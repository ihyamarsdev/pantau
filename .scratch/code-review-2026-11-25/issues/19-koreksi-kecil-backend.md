# Issue 19: Koreksi kecil backend

Status: open
Severity: LOW

## Deskripsi
- PUT kube cluster tanpa `kube_mode` mereset read-only → full — `internal/web/handlers_kube_clusters.go:202-205` (pakai `existing.KubeMode`).
- FIM `preset:webshell` lolos validasi tapi tidak memonitor apa pun (`BuildWebshellManifestCommand` tak dipanggil) — `internal/inspector/fim.go:88-91`, `323-344`.
- Backup rule memotong nama file berspasi (`fields[2]`) → drift palsu — `internal/inspector/rules_backup.go:200-202` (pakai `SplitN(out, " ", 3)`).
- Mode `starttls` diam-diam kirim cleartext bila STARTTLS tidak diiklankan — `internal/notify/smtp.go:161-168` (return error).
- Fallback GeoIP `http://ip-api.com` membocorkan IP fleet tanpa TLS — `internal/globe/resolver.go:24` (HTTPS atau opt-in).

## Kriteria Penerimaan
- [ ] Kelima poin diperbaiki, masing-masing dengan test kecil bila menyentuh logika (kube_mode, FIM preset, parsing backup).
