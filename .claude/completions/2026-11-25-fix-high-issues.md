# 2026-11-25 — Perbaikan temuan HIGH (issue 01–07)

Status per issue ada di `.scratch/code-review-2026-11-25/spec.md`.

- 01 resolved: target rule dikirim sebagai argumen posisional; validasi path absolut untuk disk.
- 02 partial: `ssh_batch_inspect` gate 2; regex destruktif cocok setelah sudo/xargs/env/{/(; destruktif tidak tercakup session grant. Belum: klasifikasi per perintah / allowlist read-only.
- 03 resolved: origin tunnel memakai cookie kapabilitas per host:port, bukan session admin.
- 04 partial: trust host key tersimpan di DB + endpoint reset. Belum: git over SSH (`StrictHostKeyChecking=no`), UI reset.
- 05 partial: snapshot memuat vault terenkripsi + master key; snapshot lama tidak mengganti master key. Belum: error export yang diabaikan (~26 titik) dan `rows.Err` di ListHosts.
- 06 resolved (kecuali conditional DB update status): state parsial disimpan; error simpan state tidak diabaikan; double apply ditolak.
- 07 resolved: checkNoSymlinks pada cabang delete.

Test: `go test` untuk sshrunner, store, terraform, ansible, inspector, web lulus, kecuali dua kegagalan yang sudah ada sebelumnya di lingkungan ini: `TestLocalKubeRunner` (kubectl tidak terpasang) dan `TestIacAnsibleAdhoc` (ansible tidak terpasang).

## Lanjutan
- 04: git over SSH pakai `accept-new` + known_hosts persisten. Belum: tombol reset di UI.
- 05: export snapshot mengembalikan error (26 titik + rows.Err); ListHosts cek rows.Err. Selesai.
- 02: destruktif ditolak di dalam batch. Belum: allowlist read-only.
- 06: sisa conditional DB update status.
