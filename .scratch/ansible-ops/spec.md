# Ansible Ops: Recap, Task Tree, Diff, Survey, Ad-Hoc, Facts, Explorer, Git Sync, -vvvv, --limit

Status: ready-for-agent

Melengkapi ADR 0049-0055 dengan komponen Ansible yang TIDAK melanggar non-goal ADR 0049 (tanpa inventory kustom, Vault, password become, dynamic inventory, workflow, execution environment). Semuanya tetap subproses lokal `ansible`/`ansible-playbook` di balik Ansible Gate (`checkAnsibleGate`), env bersih, tanpa shell, validasi ketat, mengikuti model keamanan ADR 0049.

## Fitur

1. **Verbosity 0..4** (`-v` sampai `-vvvv`). Ganti batas 0..3 di `RunSpec.Validate`, handler run, handler schedule, UI.
2. **Limit** (`--limit`): field opsional `limit` pada launch run dan schedule. Pola Ansible host-pattern, divalidasi `^[A-Za-z0-9_.,:!&*\[\]-]{1,256}$` (tanpa spasi/`;`/`$`/backtick), dilewatkan sebagai satu argumen `--limit <v>`. Alias yang valid adalah alias dari Generated Inventory (nama Host tersanitasi / nama Group). Disimpan pada `ansible_runs.limit_pattern` dan `ansible_schedules.limit_pattern` (kolom TEXT NOT NULL DEFAULT '').
3. **ANSI log**: run memakai `ANSIBLE_FORCE_COLOR=1` (bukan 0) agar log berwarna; di DB/API log tetap menyimpan escape ANSI mentah. Hanya run playbook yang berubah (galaxy/library tetap `NOCOLOR`). UI merender ANSI ke HTML ber-escape (tanpa `innerHTML` mentah dari log).
4. **Play Recap**: paket `ansible` menyediakan parser baris `PLAY RECAP` (menyaring ANSI) -> per host `{ok,changed,unreachable,failed,skipped,rescued,ignored}`. Saat run selesai, hasil disimpan di `ansible_runs.recap` (TEXT JSON, kolom baru) dan dikembalikan pada API run.
5. **Task Tree**: parsing log -> pohon Play > Task > Host (`ok`/`changed`/`fatal`/`failed`/`skipping`/`unreachable`/`ignored`) dilakukan di sisi klien (UI) dari log tanpa ANSI; ada pencarian teks dan filter status. Tidak ada perubahan backend.
6. **Diff Viewer**: UI mem-parse blok diff (`--- before`, `+++ after`, `@@`, `+`/`-`) pada run mode `diff` dan merender per file ber-warna. Tanpa perubahan backend.
7. **Survey Form**: playbook punya definisi survey opsional. Launch menerima `survey_answers` yang divalidasi server dan digabung ke extra vars.
8. **Ad-Hoc Runner**: jalankan satu modul (`ping`, `shell`, `command`, `apt`, `service`, `setup`, dst.) ke Host/Group terpilih tanpa playbook; riwayat dan log live.
9. **Facts Viewer**: kumpulkan Ansible Facts (`setup`) satu Host, cache di DB, tampilkan ringkas (OS, CPU, memori, disk, jaringan) + JSON mentah.
10. **Host & Group Explorer**: tab UI baru di menu Ansible: pohon Host Group > Host, badge kesehatan Pantau, status konektivitas Ansible dari tombol **Ping** (ad-hoc `ping` ke seluruh/terpilih; Reachable / Unreachable / Failed), tombol Facts.
11. **Git Sync**: untuk playbook sumber Git, tombol Sync Now memverifikasi repositori (clone dangkal sementara), mengembalikan commit/branch/daftar `.yml`, menyimpan waktu sync + commit terakhir.

## Kontrak data

### Paket `internal/ansible` (tanpa DB)
```go
// RunSpec bertambah: Limit string. Verbosity 0..4. Validate: Limit regex di atas (jika != "").
// Run(): bila spec.Limit != "" tambahkan "--limit", spec.Limit. ANSIBLE_FORCE_COLOR=1 hanya di Run playbook.

type RecapHost struct{ OK, Changed, Unreachable, Failed, Skipped, Rescued, Ignored int }
func ParseRecap(log string) map[string]RecapHost // alias host -> angka; kosong bila tidak ada PLAY RECAP; ANSI disaring

// Ad-hoc
type AdhocSpec struct {
    Module   string        // ^[A-Za-z0-9_.]{1,80}$ (mis. "ping", "ansible.builtin.shell")
    Args     string        // <= 4 KiB, satu argumen `-a`; boleh ""
    Pattern  string        // host pattern `ansible <pattern>`; default "all"; regex yang sama dengan Limit
    Targets  []Target      // 1..200
    Become   bool
    Forks    int
    Verbosity int          // 0..4
    ExtraVars string       // JSON objek <= 64 KiB -> -e @file
    Timeout  time.Duration // 0 => 10m
    Config, LibraryDir string // seperti RunSpec
}
func (s AdhocSpec) Validate() error // modul tidak boleh berawalan "-"; daftar blok: tidak ada (Gate + hak admin), tetapi Args tidak boleh mengandung NUL
func RunAdhoc(ctx context.Context, spec AdhocSpec, log io.Writer) (exitCode int, err error) // `ansible <pattern> -m <module> [-a <args>] -i inventory ...`, env/workspace/cleanup identik Run
type AdhocResult struct{ Alias, Status, Summary string } // Status: success|changed|unreachable|failed ; dari baris `alias | SUCCESS | rc=0 >>` dst
func ParseAdhoc(log string) []AdhocResult

// Facts
func GatherFacts(ctx context.Context, t Target, cfg, libDir string, log io.Writer) (map[string]any, error) // `ansible <alias> -m setup` pada Generated Inventory satu host; parse JSON hasil (callback json/`-o` sesuai yang berfungsi di ansible-core terpasang; verifikasi dengan biner nyata); timeout 90 dtk; ukuran hasil <= 2 MiB
func SummarizeFacts(f map[string]any) FactsSummary // OS (distribution, version, kernel, arch), hostname, fqdn, CPU (count, model), memtotal_mb, swaptotal_mb, mounts[], interfaces[{name,ipv4,ipv6,mac}], default_ipv4, uptime_seconds, python, virtualization
type FactsSummary struct {...} // json tags snake_case

// Survey
type SurveyField struct {
    Key         string   `json:"key"`         // ^[A-Za-z_][A-Za-z0-9_]{0,63}$ (nama variabel extra var)
    Label       string   `json:"label"`
    Type        string   `json:"type"`        // text | textarea | number | integer | select | multiselect | boolean | password
    Description string   `json:"description"`
    Default     any      `json:"default"`
    Required    bool     `json:"required"`
    Options     []string `json:"options"`     // select/multiselect, 1..50
    Min, Max    *float64 `json:"min,omitempty"` // number/integer; untuk text/textarea/password = panjang
}
func ValidateSurvey(fields []SurveyField) error                       // maks 30 field, key unik, aturan per type
func ApplySurvey(fields []SurveyField, answers map[string]any) (map[string]any, error) // validasi jawaban (required, tipe, options, min/max), isi default, kembalikan objek extra vars; key tak dikenal ditolak
```
`password` pada survey: nilai tidak pernah dicatat ke `ansible_runs.extra_vars`; diteruskan ke runner saja (di record diganti `"(hidden)"`) dan log tidak disaring (peringatkan di UI/ADR bahwa task bisa mencetaknya).

### Store
- `ansible_runs` + kolom `limit_pattern TEXT NOT NULL DEFAULT ''`, `recap TEXT NOT NULL DEFAULT ''`; struct `AnsibleRun` + `Limit string json:"limit"`, `Recap string json:"-"` (API mengirim `recap` sebagai objek via handler).
- `ansible_schedules` + `limit_pattern`; struct `AnsibleSchedule.Limit json:"limit"`.
- `ansible_playbooks` + `survey TEXT NOT NULL DEFAULT ''` (JSON array SurveyField), `last_sync_at DATETIME NULL`, `last_sync_commit TEXT NOT NULL DEFAULT ''`.
- `ansible_adhoc_runs` (id, module, args, pattern, targets JSON snapshot, become, verbosity, status `queued|running|success|failed|cancelled|interrupted`, exit_code, results JSON (ParseAdhoc), log, created_at, started_at, finished_at); retensi 100 terbaru; `MarkAnsibleAdhocInterrupted()` saat start.
- `ansible_host_facts` (host_id PK -> hosts ON DELETE CASCADE, facts JSON, summary JSON, gathered_at).
- Snapshot: `survey` ikut playbook; `limit` ikut schedule; riwayat ad-hoc dan facts TIDAK ikut snapshot. Migrasi `alterAddColumn` seperti kolom ansible yang ada; restore snapshot lama tetap jalan.

### REST (semua di balik `authMiddleware` + `checkAnsibleGate`; error seperti handler Ansible)
| Method + path | Body -> respons |
|---|---|
| `POST /api/ansible/playbooks/{id}/run` (ada) | tambah `limit` (string), `survey_answers` (objek); `verbosity` 0..4. Respons run kini menyertakan `limit` |
| `GET /api/ansible/runs/{id}` (ada) | `run` + `recap`: `{ "<alias>": {ok,changed,unreachable,failed,skipped,rescued,ignored} }` atau `{}`; run list ringkas ikut `recap_totals:{ok,changed,unreachable,failed,skipped}` bila ada |
| `POST/PUT /api/ansible/schedules` (ada) | tambah `limit`; verbosity 0..4 |
| `GET/PUT /api/ansible/playbooks/{id}/survey` | `{survey:[SurveyField]}` / PUT `{survey:[...]}` -> `{survey}`; `GET /api/ansible/playbooks/{id}` dan list juga memuat `survey` + `has_survey` |
| `POST /api/ansible/playbooks/{id}/git/sync` | hanya `source=git`, selain itu 400 -> `{ok, commit, branch, files:[".yml paths"], synced_at}`; menyimpan `last_sync_*`; `GET playbook` memuat `last_sync_at`, `last_sync_commit` |
| `POST /api/ansible/adhoc` | `{module, args, host_ids:[], group:"", pattern:"", become, verbosity, extra_vars, forks}` -> `{run:{id,...}}` 201; 409/429 seperti run: maks 3 ad-hoc aktif global (manager tersendiri `AnsibleAdhocManager`) |
| `GET /api/ansible/adhoc?limit=` | `{runs:[{id,module,args,pattern,status,exit_code,counts:{success,changed,unreachable,failed},created_at,finished_at}]}` |
| `GET /api/ansible/adhoc/{id}` | `{run:{..., targets:[{id,name}], results:[{alias,status,summary}]}}` |
| `GET /api/ansible/adhoc/{id}/log?offset=N` | `{log,next_offset,done,status}` (sama `handleAnsibleRunLog`); `POST /api/ansible/adhoc/{id}/cancel` |
| `GET /api/ansible/hosts/{hostId}/facts` | `{gathered_at, summary:FactsSummary, facts:{...}}` atau 404 bila belum pernah |
| `POST /api/ansible/hosts/{hostId}/facts` | kumpulkan sinkron (<= 90 dtk) -> sama dengan GET; 502 dengan pesan ringkas bila ansible gagal/unreachable; satu pengumpulan per host sekaligus (409) |
| `GET /api/ansible/explorer` | `{groups:[{name, hosts:[{id,name,address,port,status (status inspeksi Pantau), ansible_alias, facts_at|null}]}], ungrouped:[hosts...]}` — alias sama dengan yang dipakai Generated Inventory (fungsi inventori yang ada), supaya UI dapat mencocokkan hasil ad-hoc ping ke Host |

## UI (index.html, id+en i18n, pola ADR 0007/0009/0043/0039; sole owner agent UI)
- Tab baru **Hosts** (Explorer) di menu Ansible: pohon Group > Host (collapsible), badge Pantau, badge Ansible (Reachable/Unreachable/Failed/belum dicek) dari tombol **Ping** (membuat ad-hoc `ping`, polling log/hasil), tombol Facts per host membuka modal Facts Viewer (ringkasan kartu + tab JSON mentah dengan pencarian; tombol Refresh facts).
- Tab baru **Ad-Hoc**: form (modul dari daftar umum + input bebas, argumen, target Host/Group/pattern, become, verbosity), live log, tabel hasil per host berwarna (success hijau / changed oranye / unreachable merah tua / failed merah), riwayat.
- Run viewer: ANSI renderer, toggle **Log | Task Tree | Recap | Diff**; Play Recap bar berwarna (ok hijau, changed oranye, unreachable merah tua, failed merah terang, skipped abu/biru) per host + total; Task Tree collapsible Play > Task > Host dengan pencarian dan filter status; Diff Viewer per file (tambah hijau, hapus merah, hunk header). Riwayat run menampilkan mini recap bar.
- Launch dialog: verbosity 0..4, field Limit, form Survey dinamis bila playbook punya survey (validasi sisi klien + server); editor Survey di editor playbook (tambah/hapus/urut field, tipe, opsi, default, required). Schedule dialog: limit + verbosity 0..4.
- Playbook Git: tombol **Sync Now** + info commit/waktu terakhir; hasil sync menampilkan daftar `.yml`.
- Semua string ber-escape, tanpa CDN, tanpa aset baru.

## Keamanan
Ad-hoc `shell`/`command`/`raw` mengeksekusi di host target dengan akses SSH Pantau (setara terminal, yang admin sudah punya). `connection: local`/`delegate_to`/`local_action` tidak ada pada ad-hoc karena pola host selalu dibatasi ke alias Generated Inventory, tetapi `-e`/args tetap bisa memuat lookup; risiko yang sama dengan ADR 0049 dan diterima. Nilai survey `password` tidak masuk record. Log dapat memuat ANSI: UI wajib meng-escape HTML sebelum mewarnai.
