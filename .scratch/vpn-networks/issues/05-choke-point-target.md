# 05 — Choke point: `sshrunner.Target`, inspector, transfer, key provisioner

Status: ready-for-agent

Blocked by: 02, 04

## Lingkup

1. `internal/sshrunner/runner.go`:
   - `Target{Host, Port, User, Key, Net *vpnnet.Path}`; `Connect(t Target, timeout)` memakai `t.Net.DialContext` (fallback `net.DialTimeout` bila `Net == nil`) — termasuk retry 4× pada timeout yang sudah ada.
   - `RunnerFactory func(Target) (Runner, error)`; `DefaultFactory()` menyesuaikan.
   - `KeyProvisioner func(t Target, password, pubKey string) error`; `DefaultKeyProvisioner` mendial bound (`ssh.NewClientConn` di atas `net.Conn` dari `Net`).
   - `ErrNetworkUnavailable`.
2. `internal/inspector/inspector.go`: `PathResolver` (`Ensure(ctx, networkID) (*vpnnet.Path, error)`) + `SetPathResolver` (nil-safe) ; `GetRunnerForHost` mengisi `Net` dan membungkus galat jalur dengan `ErrNetworkUnavailable`.
3. `internal/transfer/transfer.go:68`: sama.
4. `internal/web/server.go:1044` dan `:1498`: `s.provisioner(Target{...Net: path}, req.Password, pubKey)`.
5. Perbarui 6 factory di `internal/inspector/*_test.go` (+ tes transfer bila ada) ke `func(t sshrunner.Target)`.

## Penerimaan

- `go test ./internal/sshrunner/... ./internal/inspector/... ./internal/transfer/... ./internal/web/...` hijau.
- Tes: `Target.Net == nil` tetap memakai dial lama (tidak ada perubahan perilaku); resolver mengembalikan `ErrNetworkUnavailable` → `GetRunnerForHost` meneruskannya (`errors.Is`), dan Host tidak dicoba dial.
- Tidak ada `net.Dial*`/`ssh.Dial` baru ke Host di luar `sshrunner` (grep sebagai kriteria: hanya `internal/sshrunner` dan `internal/notify`).

## Catatan

`RunnerFactory` adalah titik tunggal; jangan menambah jalur factory kedua untuk VPN.
