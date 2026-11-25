# Issue 13: Stored XSS di Activity Console & Exposed CVEs

Status: open
Severity: MEDIUM

## Deskripsi
1. `getActivityMetaString()` menggabungkan `attrs.tool/.domain/.risk_tier/.status/.port` tanpa escape lalu masuk `tbody.innerHTML` (`internal/web/static/app/core/06-modal-docs-activity.js:1127`, sink :1066). Nama tool dari model dilog apa adanya (`internal/web/agent_tools.go:247`) → tool name `<img src=x onerror=…>` jalan di origin Pantau.
2. `onclick="EC.toggle('${key}',${idx})"` dengan `key` hanya escape `'` (`internal/web/static/app/exposedcves/00-exposedcves.js:315-317`). `f.id` berisi path config nginx dari host (`internal/inspector/webproxy_nginx_hardening.go:184`) / nama paket OSV → keluar dari atribut.

## Kriteria Penerimaan
- [ ] `escapeHtml(parts.join(' • '))` di meta activity; opsional sanitasi nama tool tak dikenal sebelum dilog.
- [ ] `onclick="EC.toggle(${idx})"` (argumen `key` tidak dipakai) atau `jsArg(f.id)`.
