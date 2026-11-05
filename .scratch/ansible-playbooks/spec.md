# Ansible Playbooks (Semaphore-style, agentless)

Status: ready-for-agent

Terinspirasi Semaphore UI (https://semaphoreui.com/): menyimpan Playbook Ansible, menjalankannya terhadap server, melihat log langsung, dan riwayat run. Disesuaikan dengan Pantau: **inventory dihasilkan otomatis dari Host + Host Group + SSH Key Library** (tidak ada inventory/key store terpisah), eksekusi lewat biner `ansible-playbook` lokal di server Pantau (preseden: `kubectl` lokal pada ADR 0047).

## Istilah domain (tambahkan ke CONTEXT.md)

- **Ansible Playbook**: playbook YAML (nama, deskripsi, isi, extra vars bawaan) yang tersimpan di basis data Pantau. _Avoid_: Script, template task, job definition
- **Playbook Run**: satu eksekusi Ansible Playbook terhadap sekumpulan Host (status, mode, target, exit code, log, durasi). _Avoid_: Task, job, build
- **Generated Inventory**: inventory Ansible sementara yang dibangun otomatis dari Host terpilih (alamat, port, user, kunci SSH efektif) dan Host Group sebagai group Ansible; dihapus setelah run. _Avoid_: Static inventory, hosts file
- **Ansible Gate**: saklar opt-in administrator (`ansible_enabled`, default mati) yang wajib aktif sebelum Playbook dapat dijalankan; dipasangkan dengan deteksi biner `ansible-playbook`. _Avoid_: Feature flag, ansible switch

## Non-goals (v1)

Repositori Git sebagai sumber playbook, jadwal/cron, Ansible Vault & secret environment, role/collection install (`ansible-galaxy`), autentikasi password SSH / become password (hanya kunci SSH + `sudo` tanpa password), notifikasi hasil run, RBAC multi-user, inventory kustom, dukungan server Pantau di Windows (tidak ada controller Ansible native), eksekusi paralel antrian (>`MaxConcurrentRuns` ditolak 429, tidak diantrikan).

## Model keamanan (WAJIB dituliskan di ADR dan UI)

Playbook dijalankan **di server Pantau dengan hak proses Pantau**. Ansible tidak bisa disandbox: `connection: local`, `delegate_to: localhost`, `local_action`, `lookup('pipe'|'file')` mengeksekusi/membaca sesuatu di mesin master. Karena itu:
1. **Ansible Gate** default mati; pengaktifan lewat `PUT /api/ansible/status` dengan peringatan eksplisit di UI. Sesi admin (+2FA bila aktif) = setara akses eksekusi pada master; ini diterima secara sadar (admin sudah memegang terminal ke semua Host).
2. Lingkungan proses anak **dibersihkan**: hanya `PATH`, `LANG=C.UTF-8`, `HOME=<tmp>`, `HTTP_PROXY/HTTPS_PROXY/NO_PROXY` (jika ada) + variabel `ANSIBLE_*` buatan Pantau. Env Pantau lainnya tidak diwariskan. `ANSIBLE_CONFIG` menunjuk ke `ansible.cfg` sementara (cegah pemuatan `/etc/ansible`, `~/.ansible.cfg`, atau cfg di cwd).
3. Kunci privat SSH efektif Host ditulis ke file `0600` di direktori kerja `0700` (`os.MkdirTemp`), dihapus seusai run (juga saat gagal/cancel/panic). Kunci tidak pernah ditulis ke log/DB/API.
4. Semua nilai yang masuk ke inventory dibangun dengan `encoding/json` (inventory JSON), bukan penggabungan string -> tanpa injeksi. Alias host/group disanitasi `[A-Za-z0-9_.-]`, dedup dengan akhiran `_2`, `_3`.
5. Argumen CLI dari pengguna divalidasi ketat (`tags`/`skip_tags` regex `^[A-Za-z0-9_.,:-]{1,256}$`, `verbosity` 0..3, `forks` 1..20, `extra_vars` objek JSON <= 64 KiB ditulis ke file dan diberikan lewat `-e @file`). Perintah dijalankan dengan `exec.CommandContext` tanpa shell, argumen terpisah.
6. Batas: isi playbook <= 256 KiB; log run <= 2 MiB (potong bagian awal, sisipkan penanda `[pantau] log truncated`); waktu maks default 1 jam (`Timeout`); satu run aktif per playbook (409); maks 3 run aktif global (429); maks 200 target.
7. Host key checking dimatikan untuk run Ansible (`host_key_checking=False`) -- konsisten dengan koneksi SSH agentless Pantau yang tidak memakai known_hosts; dicatat di ADR sebagai risiko.
8. Run yang masih `queued/running` saat Pantau start ulang ditandai `interrupted`.

## Kontrak

### 1. Paket `internal/ansible` (stdlib saja; TANPA akses DB)

```go
package ansible

const MaxConcurrentRuns = 3

// Available melaporkan biner ansible-playbook di PATH (exec.LookPath) dan baris pertama `ansible-playbook --version`
// (di-cache; timeout 5s). Di Windows: selalu error "ansible controller is not supported on windows".
func Available() (path, version string, err error)

type Target struct {
    ID         int64
    Name       string // nama Host di Pantau -> basis alias
    Address    string // ansible_host
    Port       int
    User       string
    Group      string // Host Group (boleh kosong)
    PrivateKey string // PEM efektif (store.ResolveHostPrivateKey)
}

type RunSpec struct {
    Playbook  string        // isi YAML
    Targets   []Target
    Mode      string        // "run" | "check" (--check) | "diff" (--check --diff) | "syntax" (--syntax-check)
    ExtraVars string        // "" atau objek JSON
    Tags      string        // koma
    SkipTags  string
    Verbosity int           // 0..3 -> -v..-vvv
    Become    bool          // --become (sudo tanpa password)
    Forks     int           // 0 => 5
    Timeout   time.Duration // 0 => 1h
}
func (s RunSpec) Validate() error // semua aturan nomor 5-6 di atas + Mode valid + Targets 1..200 (kecuali Mode=="syntax" boleh 0 target)

// InventoryJSON membangun inventory JSON (format YAML-inventory ansible: all.hosts + all.children.<group>.hosts)
// dengan ansible_host/port/user/ansible_ssh_private_key_file=keyPaths[alias]/ansible_python_interpreter=auto_silent.
// Mengembalikan juga peta alias -> Target.ID.
func InventoryJSON(targets []Target, keyPath func(alias string) string) (inv []byte, aliases map[string]int64, err error)

// Run membuat workspace sementara, menulis playbook.yml, inventory.json, ansible.cfg, extra.json, key files,
// menjalankan `ansible-playbook`, menyalurkan stdout+stderr gabungan ke log secara live, dan SELALU membersihkan
// workspace. ctx dibatalkan -> seluruh process group dibunuh (SIGTERM lalu SIGKILL setelah 5s) dan err = ctx.Err().
// exitCode = kode keluar proses (0 sukses, 2 = ada host gagal, dst.). err != nil hanya untuk kegagalan menjalankan/validasi/timeout/cancel.
func Run(ctx context.Context, spec RunSpec, log io.Writer) (exitCode int, err error)
```
File platform: `proc_unix.go` (`Setpgid`, kill `-pgid`) dan `proc_windows.go` (stub; build tag). Proyek harus tetap lolos `GOOS=windows go build ./...` dan `GOOS=darwin go build ./...`.

### 2. Store (`internal/store/ansible.go`, tabel `ansible_playbooks`, `ansible_runs`)

```go
type AnsiblePlaybook struct {
    ID          int64      `json:"id"`
    Name        string     `json:"name"`
    Description string     `json:"description"`
    Content     string     `json:"content"`
    ExtraVars   string     `json:"extra_vars"`          // JSON objek bawaan, "" bila kosong
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
    LastStatus  string     `json:"last_status,omitempty"` // dihitung saat List/Get dari run terakhir
    LastRunAt   *time.Time `json:"last_run_at,omitempty"`
}
type AnsibleRun struct {
    ID           int64      `json:"id"`
    PlaybookID   int64      `json:"playbook_id"`
    PlaybookName string     `json:"playbook_name"`   // snapshot saat run dibuat
    Status       string     `json:"status"`          // queued|running|success|failed|cancelled|interrupted
    Mode         string     `json:"mode"`
    Targets      string     `json:"targets"`         // JSON: [{"id":1,"name":"web-1"}] snapshot
    ExtraVars    string     `json:"extra_vars"`
    Tags         string     `json:"tags"`
    SkipTags     string     `json:"skip_tags"`
    Verbosity    int        `json:"verbosity"`
    Become       bool       `json:"become"`
    ExitCode     *int       `json:"exit_code"`
    Log          string     `json:"-"`               // hanya via endpoint log
    CreatedAt    time.Time  `json:"created_at"`
    StartedAt    *time.Time `json:"started_at"`
    FinishedAt   *time.Time `json:"finished_at"`
}
var ErrAnsiblePlaybookNameTaken = errors.New("an ansible playbook with this name already exists")
var ErrAnsiblePlaybookNotFound  = errors.New("ansible playbook not found")
var ErrAnsibleRunNotFound       = errors.New("ansible run not found")

func (d *DB) CreateAnsiblePlaybook(p *AnsiblePlaybook) (int64, error)  // nama unik case-insensitive
func (d *DB) GetAnsiblePlaybook(id int64) (*AnsiblePlaybook, error)
func (d *DB) ListAnsiblePlaybooks() ([]AnsiblePlaybook, error)          // tanpa Content (hemat), dengan LastStatus/LastRunAt
func (d *DB) UpdateAnsiblePlaybook(p *AnsiblePlaybook) error            // name, description, content, extra_vars, updated_at
func (d *DB) DeleteAnsiblePlaybook(id int64) error                      // hapus juga runs-nya (transaksi)

func (d *DB) CreateAnsibleRun(r *AnsibleRun) (int64, error)             // status awal "queued"
func (d *DB) StartAnsibleRun(id int64) error                            // status=running, started_at=now
func (d *DB) FinishAnsibleRun(id int64, status string, exitCode *int, log string) error // finished_at=now, simpan log
func (d *DB) GetAnsibleRun(id int64) (*AnsibleRun, error)               // termasuk Log
func (d *DB) ListAnsibleRuns(playbookID int64, limit int) ([]AnsibleRun, error) // playbookID 0 = semua; tanpa Log; terbaru dulu; limit default 50 maks 200
func (d *DB) PruneAnsibleRuns(playbookID int64, keep int) error         // sisakan `keep` run terbaru yang sudah selesai
func (d *DB) MarkAnsibleRunsInterrupted() (int64, error)                // queued/running -> interrupted (dipanggil saat start)
```
Setting gate: kunci `ansible_enabled` ("1"/"0") di tabel `settings` yang ada -> `GetAnsibleEnabled() bool` / `SetAnsibleEnabled(bool) error`. Snapshot: ikut `ansible_playbooks` saja (`SnapshotAnsiblePlaybook`, field `AnsiblePlaybooks json:"ansible_playbooks,omitempty"`, masuk daftar tabel restore); runs TIDAK ikut snapshot (dikosongkan saat restore bersama tabel).

### 3. HTTP API (`internal/web/handlers_ansible.go` + `internal/web/ansible_runs.go` manager; semua `s.authMiddleware`)

| Method & path | Body | Respons |
|---|---|---|
| `GET /api/ansible/status` | – | `{"available":bool,"version":"","error":"","enabled":bool,"max_concurrent":3}` |
| `PUT /api/ansible/status` | `{"enabled":bool}` | status baru; `enabled:true` ditolak 409 bila `available=false` |
| `GET /api/ansible/playbooks` | – | `{"playbooks":[...]}` |
| `POST /api/ansible/playbooks` | `{name,description,content,extra_vars}` | 201 `{"playbook":...}` (409 nama dobel) |
| `GET/PUT/DELETE /api/ansible/playbooks/{id}` | PUT sama dgn POST | `{"playbook":...}` / `{"ok":true}`; DELETE saat ada run aktif -> 409 |
| `POST /api/ansible/playbooks/{id}/run` | `{mode,host_ids:[],group,extra_vars,tags,skip_tags,verbosity,become}` | 202 `{"run":...}`; `extra_vars` kosong -> pakai bawaan playbook; target = union `host_ids` + semua Host ber-`group_name == group` |
| `GET /api/ansible/playbooks/{id}/runs?limit=` | – | `{"runs":[...]}` |
| `GET /api/ansible/runs?limit=` | – | `{"runs":[...]}` semua playbook |
| `GET /api/ansible/runs/{id}` | – | `{"run":...}` |
| `GET /api/ansible/runs/{id}/log?offset=N` | – | `{"data":"<teks sejak byte N>","offset":<next>,"status":"","finished":bool}`; run aktif dibaca dari buffer memori, selesai dari DB; offset melewati panjang -> data kosong |
| `POST /api/ansible/runs/{id}/cancel` | – | `{"ok":true}`; 409 bila sudah selesai |

Aturan: semua endpoint selain `status` mengembalikan 409 `{"error":"ansible is disabled"}` bila Gate mati atau 409 bila biner tidak tersedia. Validasi playbook (nama 1..64, content 1..256 KiB, extra_vars objek JSON valid). Run: tolak 400 bila tidak ada target (kecuali mode `syntax`), host_id tak dikenal, group tak punya Host; 409 bila playbook sudah punya run aktif; 429 bila 3 run global aktif. Manager: goroutine per run; `context.WithCancel` disimpan untuk cancel; buffer log memori dibatasi 2 MiB (potong awal + penanda); saat selesai `FinishAnsibleRun` lalu `PruneAnsibleRuns(id, 100)`; status akhir: exit 0 -> success, `ctx` dibatalkan user -> cancelled, lainnya -> failed (error eksekusi ditambahkan ke log). Server memanggil `MarkAnsibleRunsInterrupted` saat inisialisasi. Mutasi & start/finish run dicatat ke logger aktivitas (tanpa kunci/extra_vars). Field `server.ansibleRun func(ctx, ansible.RunSpec, io.Writer) (int, error)` (default `ansible.Run`) dan `server.ansibleAvail func() (string, string, error)` (default `ansible.Available`) agar test tanpa Ansible sungguhan.

### 4. UI (`internal/web/static/index.html`, i18n id/en)

Entri "Ansible" di side panel sejajar Docker/Kubernetes/Cloudflare; Workspace Modal / Action Dialog / Toast / Confirmation Dialog. Isi: (a) banner Ansible Gate: biner tidak tersedia -> pesan; tersedia tapi mati -> peringatan keamanan + tombol Aktifkan (konfirmasi); (b) daftar Playbook (nama, deskripsi, status run terakhir badge, waktu) dengan Run / Edit / Riwayat / Hapus; (c) editor playbook (Action/Workspace dialog, CodeMirror mode YAML yang sudah di-vendor `mode/yaml.min.js` -- ikuti cara editor file memakai CodeMirror; field nama, deskripsi, extra vars bawaan JSON); (d) dialog Run: pemilih target (daftar Host dengan cari + chip Host Group untuk pilih sekelompok), mode (Run/Check/Diff/Syntax), extra vars, tags, skip tags, verbosity, switch become (sudo tanpa password); (e) viewer Run (Workspace Modal): badge status, durasi, tombol Cancel/Tutup, panel log monospace polling `GET .../log?offset=` tiap 1 dtk dengan smart auto-scroll (berhenti saat run selesai; log tidak diwarnai), tab Riwayat per playbook (tabel run; klik -> viewer). Semua string dari server di-escape; tanpa CDN/skrip eksternal baru.

### 5. Dokumen & Docker

ADR `docs/adr/0049-ansible-playbooks-semaphore-style.md` (bahasa Indonesia, struktur seperti 0047: Konteks, Keputusan Arsitektur, Model Keamanan, Alternatif [mis. Semaphore sebagai sidecar ditolak: servis & DB kedua; library Go ansible tidak ada; eksekusi YAML sendiri di Go ditolak], Konsekuensi), istilah di `CONTEXT.md`, `CHANGELOG.md`, `README.md`, `README.id.md`, `docs/user-guide.md`, `docs/user-guide.id.md`, `Dockerfile` (tambahkan `ansible-core openssh-client` pada `apk add` image akhir; sebutkan konsekuensi ukuran image di ADR).
