# Terraform Management (Terraform Cloud-style, local CLI)

Status: ready-for-agent

Dashboard manajemen Terraform/OpenTofu terinspirasi HCP Terraform: Workspace, Run pipeline (plan → policy → cost → approval → apply), State, Variables/Variable Sets, Outputs, Private Module Registry, Drift Detection, Policy as Code + Cost Estimation, integrasi Git/webhook. Eksekusi lewat biner `terraform` / `tofu` **lokal di server Pantau** (preseden: `ansible-playbook` ADR 0049, `kubectl` ADR 0047). Tidak ada backend remote: backend `local`, state file dimaterialisasi dari DB.

## Istilah domain (tambahkan ke CONTEXT.md)

- **Terraform Workspace**: unit terisolasi (config + variabel + state + riwayat run) untuk satu environment. _Avoid_: Stack, project, environment folder
- **Terraform Run**: satu siklus eksekusi (plan → policy check → cost estimate → approval → apply) pada Workspace. _Avoid_: Job, pipeline, build
- **Approval Gate**: tahap wajib di mana Run dengan perubahan menunggu Confirm & Apply / Discard dengan komentar wajib. _Avoid_: Manual step, review
- **State Version**: salinan immutable `terraform.tfstate` yang dicatat di DB setiap apply/refresh/rollback/import. DB = sumber kebenaran; file di disk dimaterialisasi sebelum tiap run. _Avoid_: State backup, snapshot
- **Workspace Lock**: kunci Pantau yang mencegah Run baru (manual atau otomatis selama Run aktif); Force Unlock untuk darurat. _Avoid_: Mutex, state lock file
- **Variable Set**: kumpulan variabel yang dibagikan ke banyak Workspace (atau global). _Avoid_: Shared vars, env group
- **Terraform Drift**: perbedaan infrastruktur riil vs state yang terdeteksi plan `-refresh-only` (beda dari **Drift** inspeksi Host). _Avoid_: Config drift, mismatch
- **Policy Check**: evaluasi aturan bawaan (Go) atas plan JSON dengan level `advisory` / `soft` / `hard`. Bukan OPA/Sentinel. _Avoid_: Compliance scan
- **Cost Estimate**: estimasi delta biaya bulanan via biner `infracost` bila terpasang. _Avoid_: Billing
- **Terraform Gate**: saklar opt-in admin (`terraform_enabled`, default mati) sebelum Run dapat dijalankan. _Avoid_: Feature flag

## Non-goals (v1)

OPA/Rego & Sentinel (hanya aturan bawaan), backend remote/S3/consul, auto-apply, API token CLI/remote execution (trigger `api` tidak ada; trigger = `manual` | `webhook` | `drift`), OIDC/dynamic credentials (kredensial cloud lewat variabel `env` sensitif), RBAC multi-user, antrean run (>3 aktif → 429), Terraform di Windows (build tetap lolos, `Detect()` melaporkan tidak didukung), registry protocol publik (katalog modul hanya UI + dokumentasi, bukan endpoint `terraform init` source), private provider mirror, enkripsi variabel di DB (konsisten dengan token Cloudflare/Git yang plaintext di SQLite; ikut System Snapshot terenkripsi).

## Model keamanan (WAJIB di ADR dan UI)

Terraform dapat menjalankan kode lokal (`provisioner "local-exec"`, `data "external"`, `data "local_file"`, modul dari Git). Tidak bisa disandbox. Sama seperti Ansible:
1. **Terraform Gate** default mati; `PUT /api/terraform/status` + peringatan eksplisit di UI. Admin = setara eksekusi di master.
2. Env proses anak dibersihkan: hanya `PATH`, `LANG=C.UTF-8`, `HOME=<Root>/home`, `HTTP_PROXY/HTTPS_PROXY/NO_PROXY` (bila ada), `TF_IN_AUTOMATION=1`, `TF_INPUT=0`, `CHECKPOINT_DISABLE=1`, `TF_PLUGIN_CACHE_DIR=<DataRoot>/plugin-cache`, `TF_DATA_DIR=<Root>/data`, + variabel `env` workspace/varset (kredensial cloud). Env Pantau lain tidak diwariskan.
3. Variabel `terraform` non-HCL ditulis ke `pantau.auto.tfvars.json` (via `encoding/json`), yang HCL ke `pantau-hcl.auto.tfvars`; key divalidasi `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`. Tanpa shell: `exec.CommandContext`, argumen terpisah. File dibuat 0600 dalam dir 0700.
4. Nilai `sensitive` tidak pernah dikembalikan API (write-only), tidak ditulis ke log (log disaring: nilai sensitif diganti `(sensitive)`), dan output/atribut state yang sensitif di-mask di API; unduhan `.tfstate` mentah mengandung rahasia (admin-only, dicatat di UI).
5. Batas: file inline <= 256 KiB/file, <= 50 file, nama `^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$` dengan ekstensi `.tf .tf.json .tfvars .tftpl .json .yaml .yml .txt .md`; state import <= 16 MiB; log run <= 2 MiB (reuse `logBuffer` di `internal/web/ansible_runs.go`); timeout plan 30 m, apply 1 jam; 1 run aktif per workspace (409), 3 global (429); retensi 100 run & 100 state version per workspace; sisa run `queued/planning/applying` saat start -> `interrupted`.
6. Webhook publik `POST /api/terraform/webhook/{token}` tanpa session: token acak 32 byte hex per workspace + (opsional) secret HMAC (`X-Hub-Signature-256`) / `X-Gitlab-Token`; perbandingan constant-time; hanya memicu **plan** (tidak pernah apply) bila branch push cocok; body <= 1 MiB; rate-limit sederhana per token.

## Kontrak Go: paket `internal/terraform` (stdlib + `pantau/internal/ansible` untuk validator/klon Git; TANPA akses DB)

```go
package terraform

const MaxConcurrentRuns = 3

type Binary struct{ Name, Path, Version, Err string } // Name: "terraform" | "tofu"
func Detect() []Binary // selalu 2 entri; Err terisi bila tak ada; cache 60s; windows: Err "terraform is not supported on windows"
func Infracost() (path, version string, err error)

type GitSource struct{ URL, Branch, Token, PrivateKey string } // validasi via ansible.ValidateGitURL/Branch
type Var struct{ Key, Value string; HCL, Sensitive bool }

type Config struct {
    Binary  string            // "terraform" | "tofu"
    DataRoot string           // <dir db>/terraform
    Root    string            // DataRoot/<workspaceID>; dibuat 0700
    Files   map[string]string // inline; nil bila Git != nil
    Git     *GitSource
    WorkDir string            // subdir relatif dalam klon ("" = root); tolak `..`/absolut
    Vars    []Var             // category terraform
    Env     map[string]string // category env
    State   []byte            // state terbaru dari DB; ditulis ke Root/terraform.tfstate sebelum run ("" = tanpa)
}
func (c Config) Validate() error

type PlanSpec struct {
    Config
    RunID   int64
    Mode    string   // "plan" | "destroy" | "refresh" (apply-able refresh-only) | "drift" (refresh-only speculative)
    Targets, Replace []string // address resource; divalidasi regex ketat
    Timeout time.Duration    // 0 => 30m
}
type AttrDiff struct{ Path string; Before, After any; Sensitive, Unknown bool } // sensitif => nilai diganti "(sensitive)"
type ResourceChange struct {
    Address, Module, Mode, Type, Name, Provider string
    Action  string   // create | update | delete | replace | read | no-op
    Reason  string   // action_reason bila ada
    Diffs   []AttrDiff
}
type PlanSummary struct{ Create, Update, Delete, Replace, Read int; Changes []ResourceChange; Drift []ResourceChange; OutputChanges []string }
type PlanResult struct {
    ExitCode   int
    HasChanges bool
    Summary    PlanSummary // Changes hanya aksi != no-op
    PlanJSON   []byte      // `show -json` plan (<= 32 MiB; untuk policy/cost) -- tidak disimpan ke DB
    Commit     string      // git HEAD bila Git
    TFVersion  string
}
// Plan: siapkan workdir Root/runs/<RunID>/work (klon/tulis file), init, (validate), plan -out=Root/runs/<RunID>/tfplan, show -json.
// Mode drift/refresh => -refresh-only. Workdir & tfplan DIPERTAHANKAN untuk Apply (kecuali Mode drift/speculative: dihapus). err != nil hanya utk gagal jalan/timeout/cancel; plan gagal => ExitCode 1 + err kosong.
func Plan(ctx context.Context, spec PlanSpec, log io.Writer) (*PlanResult, error)

type ApplySpec struct{ Config; RunID int64; Timeout time.Duration } // Config HARUS sama dgn saat Plan (env/vars dipakai ulang)
type Output struct{ Name string; Value json.RawMessage; Type json.RawMessage; Sensitive bool }
type ApplyResult struct{ ExitCode int; State []byte; Outputs []Output }
// Apply: `apply tfplan` di workdir yang dipertahankan; selalu CleanupRun di akhir; State = isi Root/terraform.tfstate sesudahnya.
func Apply(ctx context.Context, spec ApplySpec, log io.Writer) (*ApplyResult, error)
func RunDirExists(root string, runID int64) bool
func CleanupRun(root string, runID int64) // hapus Root/runs/<id>
func ForceUnlockFiles(root string)         // hapus lock info sisa (.terraform.tfstate.lock.info) -- best effort

// --- State (parser tfstate v4, TANPA CLI) ---
type StateResource struct{ Address, Module, Mode, Type, Name, Provider string; Index any; Attributes map[string]any } // sensitif di-mask "(sensitive)" via sensitive_attributes
type StateInfo struct{ Serial int64; Lineage, TFVersion string; Resources []StateResource; Outputs []Output }
func ParseState(b []byte) (*StateInfo, error) // validasi version==4, <=16MiB
type AttrChange struct{ Path string; Before, After any }
type StateDiff struct{ Added, Removed []string; Changed []struct{ Address string; Changes []AttrChange } }
func DiffStates(a, b []byte) (*StateDiff, error)
func RollbackState(old, current []byte) ([]byte, error) // isi `old` dengan serial = current.serial+1, lineage = current.lineage

// --- Policy as Code (aturan bawaan) ---
type Policy struct{ ID int64; Name, Kind, Enforcement string; Params json.RawMessage } // Enforcement: advisory|soft|hard
// Kind: deny_destroy | max_deletes{max} | max_changes{max} | deny_resource_types{types[]} | require_tags{tags[],types[]?} | deny_open_cidr
type Violation struct{ Address, Message string }
type PolicyResult struct{ PolicyID int64; Name, Kind, Enforcement string; Passed bool; Violations []Violation }
type PolicyReport struct{ Status string; Results []PolicyResult } // Status: passed | warning (advisory/soft gagal) | failed (hard gagal)
func ValidatePolicy(p Policy) error
func EvaluatePolicies(planJSON []byte, ps []Policy) (PolicyReport, error)

// --- Cost (infracost opsional) ---
type CostEstimate struct{ Available bool; Error, Currency string; MonthlyBefore, MonthlyAfter, Delta, DeltaPct float64 } // jika tak ada biner: Available=false, Error="infracost not installed"
func EstimateCost(ctx context.Context, planJSON []byte, env map[string]string) CostEstimate // `infracost breakdown --path <plan.json> --format json`; INFRACOST_API_KEY dari env

// --- Modul (private catalog) ---
type ModuleInput struct{ Name, Type, Description, Default string; Required, Sensitive bool }
type ModuleOutput struct{ Name, Description string; Sensitive bool }
type ModuleDoc struct{ Readme string; Inputs []ModuleInput; Outputs []ModuleOutput; Resources []string /* "type.name" */; Providers []string; RequiredVersion string }
func ParseModule(files map[string]string) ModuleDoc // scanner blok HCL (string/komentar/heredoc/brace aware); README.md => Readme
func ValidateFiles(files map[string]string) error
func CloneModuleFiles(ctx context.Context, g GitSource, ref, subdir string) (map[string]string, error) // batas 50 file/256KiB/file, hanya ekstensi yang diizinkan

// --- Webhook ---
// Mengembalikan branch dari push (GitHub/GitLab/Bitbucket-compatible `ref: refs/heads/x`); verifikasi HMAC/X-Gitlab-Token bila secret != "".
func ParseWebhook(h http.Header, body []byte, secret string) (branch string, err error)
```

## Kontrak Store (`internal/store/terraform*.go`, tabel di `db.go`; pola sama `ansible.go`)

Tabel: `tf_workspaces` (id, name unik NOCASE, description, environment, tags (JSON array), binary, source `inline|git`, files (JSON), git_url, git_branch, git_path, git_auth `none|token|ssh`, git_token, git_key_id, webhook_token (unik, hex), webhook_secret, drift_interval_minutes, lock_reason, locked_by (`manual|run:<id>`), locked_at, drift_detected, drift_checked_at, tf_version, created_at, updated_at); `tf_variables` (id, workspace_id|0, varset_id|0, key, value, category `terraform|env`, hcl, sensitive, description; unik (owner,key,category)); `tf_varsets` (id, name unik, description, global) + `tf_varset_workspaces`; `tf_runs` (id, workspace_id, type, speculative, status, trigger_kind, message, targets, replace, commit_sha, plan_summary JSON, drift JSON, policy JSON, cost JSON, has_changes, exit_code, log, approved_by_comment, decision (`applied|discarded|""`), decision_comment, created_at, started_at, finished_at); `tf_state_versions` (id, workspace_id, run_id, serial, lineage, source `apply|refresh|rollback|import`, size, resource_count, sha256, state BLOB, created_at); `tf_policies` (id, name unik, description, kind, params JSON, enforcement, enabled, workspace_id|0 = semua); `tf_modules` (id, name, provider, description) + `tf_module_versions` (id, module_id, version semver, files JSON, doc JSON, source_url, created_at; unik (module_id, version)).
Status run: `queued | planning | awaiting_approval | applying | applied | no_changes | planned (speculative selesai) | policy_failed | errored | discarded | canceled | interrupted`.
Metode wajib: CRUD tiap entitas, `ListTFRuns(wsID, limit)`, `UpdateTFRun…`, `MarkTFRunsInterrupted()` (awaiting_approval TIDAK disentuh), `PruneTFRuns/PruneTFStateVersions(wsID, keep)`, `LatestTFStateVersion(wsID)`, `EffectiveTFVariables(wsID)` (varset global + varset terhubung + milik workspace; workspace menang), `SetTFWorkspaceLock/Unlock`, `TryLockTFWorkspace(id, by)` atomik, kolom sensitif tidak pernah dikembalikan lewat JSON (`json:"-"`). Snapshot: `tf_workspaces`, variabel, varset(+membership), policies, modules(+versions) dan **state terbaru per workspace** (dibawa sebagai versi `import`) ikut System Snapshot; runs & log tidak; restore mengosongkan semuanya dan memasukkan ulang; `go test ./internal/store` untuk round-trip snapshot.

## Kontrak REST (semua di bawah `authMiddleware` kecuali webhook; semua kecuali `status` memeriksa Terraform Gate → 403 `{"error":"terraform is disabled"}` bila mati). Error: `http.Error` teks seperti handler Ansible.

| Method + path | Body → respons |
|---|---|
| `GET /api/terraform/status` | `{enabled, supported, binaries:[{name,path,version,err}], infracost:{available,version,err}}` |
| `PUT /api/terraform/status` | `{enabled}` → status |
| `GET /api/terraform/workspaces?env=&tag=&q=` | `{workspaces:[WS]}` |
| `POST /api/terraform/workspaces` | WS input → `{workspace:WS}` (201); nama duplikat 409 |
| `GET/PUT/DELETE /api/terraform/workspaces/{id}` | `{workspace:WS}` (detail menyertakan `files`); DELETE hapus run/state/dir (409 bila run aktif) |
| `POST /api/terraform/workspaces/{id}/vcs/test` | `{ok, commit, branch, error}` (git ls-remote) |
| `POST /api/terraform/workspaces/{id}/webhook/rotate` | `{webhook_url, webhook_secret}` (secret hanya tampil sekali) |
| `GET /api/terraform/workspaces/{id}/runs?limit=` | `{runs:[RunSummary]}` |
| `POST /api/terraform/workspaces/{id}/runs` | `{type:"plan"\|"destroy"\|"refresh", speculative, message, targets[], replace[]}` → `{run:Run}` 201; 423 locked; 409 run aktif; 429 penuh |
| `GET /api/terraform/runs/{id}` | `{run:Run}` |
| `GET /api/terraform/runs/{id}/log?offset=N` | `{log, next_offset, done, status}` (sama `handleAnsibleRunLog`) |
| `POST /api/terraform/runs/{id}/apply` | `{comment (wajib, >=3 char), override_policy}` → `{run}`; 409 bukan awaiting_approval; 422 policy hard / soft tanpa override |
| `POST /api/terraform/runs/{id}/discard` | `{comment (wajib)}` → `{run}` |
| `POST /api/terraform/runs/{id}/cancel` | → `{run}` |
| `GET /api/terraform/workspaces/{id}/state` | `{serial, lineage, terraform_version, resources:[StateResource]}` (kosong bila belum ada state) |
| `POST /api/terraform/workspaces/{id}/state/import` | `{state: "<json tfstate>"}` → version |
| `GET /api/terraform/workspaces/{id}/state/versions` | `{versions:[{id,serial,run_id,source,size,resource_count,sha256,created_at}]}` |
| `GET /api/terraform/workspaces/{id}/state/versions/{vid}` | JSON tfstate mentah (`Content-Disposition: attachment; filename=<ws>-<serial>.tfstate`) |
| `GET /api/terraform/workspaces/{id}/state/diff?from=&to=` | `StateDiff` (JSON) |
| `POST /api/terraform/workspaces/{id}/state/versions/{vid}/rollback` | `{comment}` → `{version}`; 423 locked |
| `POST /api/terraform/workspaces/{id}/lock` / `unlock` / `force-unlock` | `{reason}` / – / `{comment}` → `{workspace:WS}`; force-unlock membatalkan run aktif + `ForceUnlockFiles` |
| `GET /api/terraform/workspaces/{id}/outputs?reveal=1` | `{outputs:[{name,value,type,sensitive}]}`; value sensitif dihilangkan (`null`, `value_hidden:true`) tanpa `reveal=1` |
| `GET /api/terraform/workspaces/{id}/drift` | `{detected, checked_at, interval_minutes, resources:[ResourceChange]}`; `POST .../drift/check` memicu run `drift` → `{run}` |
| `GET/POST /api/terraform/workspaces/{id}/variables` | `GET ?effective=1` → `{variables:[Var]}` dgn `origin:"workspace"\|"varset:<name>"`; POST `Var` input |
| `PUT/DELETE /api/terraform/workspaces/{id}/variables/{vid}` | sensitif: `value` kosong pada PUT = pertahankan nilai lama |
| `GET/POST /api/terraform/varsets`, `GET/PUT/DELETE /api/terraform/varsets/{id}` | `{varsets:[{id,name,description,global,variable_count,workspace_ids}]}` |
| `GET/POST /api/terraform/varsets/{id}/variables`, `PUT/DELETE …/{vid}` | idem variabel; `PUT /api/terraform/varsets/{id}/workspaces` `{workspace_ids:[]}` |
| `GET/POST /api/terraform/policies`, `PUT/DELETE /api/terraform/policies/{id}` | `{policies:[Policy]}`; validasi `ValidatePolicy` |
| `GET/POST /api/terraform/modules`, `GET/PUT/DELETE /api/terraform/modules/{id}` | `{modules:[{id,name,provider,description,latest_version,version_count}]}`; detail menyertakan `versions` |
| `POST /api/terraform/modules/{id}/versions` | `{version, files{}}` ATAU `{version, git:{url,ref,path,token,auth,key_id}}` (klon saat request lalu simpan file) → `{version}` dgn `doc:ModuleDoc` dan `usage` snippet `module "x" {…}`; `DELETE …/versions/{ver}` |
| `POST /api/terraform/webhook/{token}` | publik; 202 `{run_id}` / 200 `{ignored:"branch"}` / 401 / 404 |

`WS` = `{id,name,description,environment,tags[],binary,source,git_url,git_branch,git_path,git_auth,git_token_set,git_key_id,webhook_enabled,webhook_url,drift_interval_minutes,status,locked,lock_reason,locked_by,drift_detected,drift_checked_at,tf_version,resource_count,last_run:{id,type,status,finished_at,has_changes}|null,created_at,updated_at}`; `status` turunan dengan prioritas: `locked` bila lock manual → `running` (run aktif) → `awaiting_approval` → `errored` (run terakhir gagal) → `drifted` → `applied` → `never`. `RunSummary` = `{id,workspace_id,type,speculative,status,trigger_kind,message,has_changes,counts:{create,update,delete,replace},policy_status,cost_delta,created_at,started_at,finished_at}`. `Run` = RunSummary + `{commit_sha,targets,replace,plan:PlanSummary,drift:[ResourceChange],policy:PolicyReport,cost:CostEstimate,exit_code,decision,decision_comment}`.

## Alur Run (web, `AnsibleRunManager`-style `TerraformRunManager`)
`queued → planning` (Plan) → bila exit 1/err `errored`; speculative/`drift` → `planned`; tanpa perubahan → `no_changes`; selain itu evaluasi Policy lalu Cost (gagal/ tak tersedia tidak memblokir) → `policy_failed` (hard) atau `awaiting_approval`. `apply` (komentar wajib; soft/advisory gagal butuh `override_policy:true`) → `applying` → `applied`/`errored`; sesudah apply/refresh sukses simpan State Version baru (bila sha256 beda dari versi terakhir), reset `drift_detected`. Workspace otomatis di-lock `run:<id>` sejak queued sampai terminal (awaiting_approval TETAP lock sampai apply/discard). Sebelum Plan dan Apply: tulis `LatestTFStateVersion` ke `Config.State`. Scheduler drift (tiap 60 dtk, pola `ansible_scheduler.go`): workspace `drift_interval_minutes>0`, tidak locked, `drift_checked_at` lewat → run `drift` trigger `drift`; hasil menyetel `drift_detected`/`drift_checked_at` dan notifikasi lewat jalur notify yang ada bila `drift_detected` baru muncul (reuse notifier existing bila ada hook yang sama dengan Ansible; jika tidak ada, lewati dan catat).

## UI (single `internal/web/static/index.html`, i18n id+en, pola ADR 0007/0009/0039/0043; ikuti entry/side-panel Ansible & Cloudflare)
Mapping 9 bagian permintaan ke layar: (1) daftar Workspace tabel/kartu + status badge + filter env/tag + pilih binary; (2) tab Runs: antrean & riwayat, Visual Plan Diff collapsible (+ hijau, ~ kuning, -/+ & - merah, before→after), Real-time Console Log (pola log Ansible/xterm-less `<pre>` auto-scroll), Approval Gate (Confirm & Apply / Discard + komentar wajib, checkbox override policy); (3) tab State: inventory resource + atribut, riwayat versi + diff + rollback + unduh, indikator Lock + Force Unlock (Confirmation Dialog); (4) tab Variables: tabel key/value, tipe terraform/env, HCL, Sensitive toggle; halaman Variable Sets; (5) panel Outputs dengan Copy (sensitif tersembunyi + reveal); (6) katalog Modules (versi, README, inputs/outputs/resources, snippet usage); (7) badge Drift Detected + interval + detail atribut; (8) banner Policy Check (Pass / Warning / Failed) & Cost Delta Card (`+$45.20/bln (+12%)`; "Infracost belum terpasang" bila tidak ada) di antara plan dan apply + halaman Policies; (9) pengaturan VCS: url/branch/working dir/auth, tombol Test, webhook URL + rotate. Terraform Gate dengan peringatan keamanan. Hanya aset yang sudah di-embed (airgapped, ADR 0013) — tidak ada CDN.
