# 01 — Runner Ansible Playbook Lokal (internal/ansible)

Type: task
Status: ready-for-agent
Blocked by:

## Tujuan

Menyediakan paket Go independen `internal/ansible` (hanya standar library Go, tanpa dependensi DB) untuk mendeteksi ketersediaan biner `ansible-playbook`, memvalidasi konfigurasi eksekusi (`RunSpec`), menyusun inventory JSON sementara dari Host dan Host Group Pantau, serta menjalankan playbook sebagai subproses terisolasi dengan streaming log stdout/stderr langsung dan pembatalan bersih (`context.Context`).

## Perubahan

- Implementasi `internal/ansible/ansible.go`:
  - Konstanta `MaxConcurrentRuns = 3`.
  - Fungsi `Available() (path, version string, err error)` dengan `exec.LookPath`, baris pertama versi, caching, dan timeout 5 detik. Di Windows mengembalikan error "ansible controller is not supported on windows".
  - Model `Target` (`ID`, `Name`, `Address`, `Port`, `User`, `Group`, `PrivateKey`).
  - Model `RunSpec` (`Playbook`, `Targets`, `Mode`, `ExtraVars`, `Tags`, `SkipTags`, `Verbosity`, `Become`, `Forks`, `Timeout`) beserta metode `Validate() error` (validasi mode `run|check|diff|syntax`, panjang tags regex `^[A-Za-z0-9_.,:-]{1,256}$`, verbosity 0..3, forks 1..20 default 5, timeout default 1h, isi playbook <= 256 KiB, extra_vars objek JSON valid <= 64 KiB, target 1..200 kecuali mode `syntax` boleh 0).
  - Fungsi `InventoryJSON(targets []Target, keyPath func(alias string) string) (inv []byte, aliases map[string]int64, err error)` untuk membangun inventori JSON dengan format grup standar Ansible (`all.hosts` dan `all.children.<group>.hosts`) beserta sanitasi alias dan dedup.
  - Fungsi `Run(ctx context.Context, spec RunSpec, log io.Writer) (exitCode int, err error)` membuat workspace sementara (`os.MkdirTemp`), menulis berkas `playbook.yml`, `inventory.json`, `ansible.cfg` (`host_key_checking = False`), `extra.json`, kunci privat SSH (`0600`), menjalankan `ansible-playbook` dengan lingkungan bersih (`PATH`, `LANG`, `HOME`, proxy, `ANSIBLE_*`), mengalirkan log langsung, dan selalu membersihkan workspace seusai run.
- File platform `internal/ansible/proc_unix.go` (`Setpgid`, pembunuhan process group SIGTERM lalu SIGKILL) dan `internal/ansible/proc_windows.go` (stub build tag).
- Unit test komprehensif di `internal/ansible/*_test.go`.

## Acceptance

- `go test -v ./internal/ansible/...` lolos.
- Proyek tetap lolos kompilasi silang `GOOS=windows go build ./...` dan `GOOS=darwin go build ./...`.
