# Issue 01: Shell injection pada target desired-state rule (service batch & disk)

Status: resolved
Severity: HIGH (RCE di host terpantau)

## Deskripsi
`shellQuote(target)` disisipkan ke dalam script `sh -c '...'` yang sudah di-single-quote, sehingga quote dari `shellQuote` justru menutup quote luar dan target jadi tidak ter-quote di login shell remote. Blocklist di web layer (`internal/web/server.go:1754`, blok `;&|`$\n\r`) tidak menahan process substitution `<( )`/`>( )`.

- `internal/inspector/desired_rules.go:64` — `serviceBatchCmd` (dipakai saat host Linux punya ≥2 service rule)
- `internal/inspector/desired_rules.go:271-276` — disk rule, bug yang sama; path dengan spasi juga rusak

Skenario: target service `nginx >(touch /tmp/pwned)` → setiap inspeksi menjalankan `touch` sebagai user SSH (sering root). Bisa juga masuk lewat tool LLM `fleet_rule_action`.

## Kriteria Penerimaan
- [x] Target dikirim sebagai argumen posisional: `sh -c '... "$@" ...' -- <quoted...>` dan `"$1"` untuk disk.
- [x] Target disk divalidasi sebagai path absolut (`inspector.ValidateDiskTarget`, dipanggil dari `internal/web/server.go`).
- [x] Test: `TestServiceBatchCmd_TargetIsNotShellCode` (gagal di kode lama, lulus sekarang). Test disk belum ada (lihat catatan).
