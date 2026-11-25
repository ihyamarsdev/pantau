# Issue 20: Error handling fetch di frontend

Status: open
Severity: LOW

## Deskripsi
- Edit rule: DELETE rule lama tidak dicek, toast sukses tetap muncul → rule lama tetap aktif — `internal/web/static/app/core/03-workspace-files-editor.js:2065`.
- Scan exposure gagal terlihat seperti hasil kosong dan menghapus temuan sebelumnya — `internal/web/static/app/exposedcves/00-exposedcves.js:194-196`.
- Ack alert & logout mengabaikan kegagalan request — `internal/web/static/app/core/02-hosts.js:925,930`; `core/01-i18n-auth.js:235`.

## Kriteria Penerimaan
- [ ] Cek `res.ok` dan tampilkan toast error; scan gagal mempertahankan `state.data` lama; logout reload hanya setelah sukses.
