# 05 — Dokumentasi & Istilah Arsitektur Ansible Playbooks

Type: task
Status: ready-for-agent
Blocked by:

## Tujuan

Menyusun dokumentasi teknis, operasional, dan arsitektur untuk fitur Ansible Playbooks: Architectural Decision Record (ADR 0049), penambahan istilah domain di `CONTEXT.md`, catatan rilis di `CHANGELOG.md`, ringkasan fitur di `README.md` dan `README.id.md`, panduan lengkap di `docs/user-guide.md` dan `docs/user-guide.id.md`, penambahan paket pada `Dockerfile`, serta tiket-tiket pelacakan isu di `.scratch/ansible-playbooks/issues/`.

## Perubahan

- `docs/adr/0049-ansible-playbooks-semaphore-style.md`:
  - Format mengikuti struktur ADR 0047/0048 dalam Bahasa Indonesia: Konteks dan Masalah, Keputusan Arsitektur, Model Keamanan (eksekusi di master dengan hak Pantau, ketiadaan sandbox, opt-in Ansible Gate, pembersihan env anak, penulisan kunci privat 0600 di direktori 0700 dan pembersihan otomatis, penonaktifan host_key_checking, penanganan run terinterupsi), Alternatif yang Dipertimbangkan (Semaphore sidecar, implementasi executor YAML sendiri di Go, repositori Git sebagai sumber), dan Konsekuensi (ukuran image bertambah, python di master, Windows server tidak didukung, batasan koneksi SSH lama).
- `CONTEXT.md`:
  - Penambahan 4 istilah domain kanonikal: `Ansible Playbook`, `Playbook Run`, `Generated Inventory`, dan `Ansible Gate` dengan definisi dan baris `_Avoid_:`.
- `CHANGELOG.md`:
  - Penambahan entri fitur baru pada bagian `[Unreleased] -> Added` merujuk ke ADR-0049.
- `README.md` & `README.id.md`:
  - Penambahan seksi fitur bernomor baru (30) mengenai Ansible Playbooks (Semaphore-style) mencakup inventory otomatis dari Host/Host Group, mode eksekusi, log streaming, dan model keamanan.
- `docs/user-guide.md` & `docs/user-guide.id.md`:
  - Penambahan panduan operasional langkah-demi-langkah: aktivasi Ansible Gate, pembuatan playbook, seleksi target host/group, mode eksekusi (run/check/diff/syntax), extra vars, pemantauan log live & pembatalan, retensi riwayat (100 run per playbook, 2 MiB log), batasan, dan perilaku snapshot.
- `Dockerfile`:
  - Penambahan paket `ansible-core openssh-client` pada baris `apk add` stage final.
- `.scratch/ansible-playbooks/issues/`:
  - Berkas pelacakan `01-runner.md` hingga `05-docs.md` berstatus `ready-for-agent`.

## Acceptance

- Semua berkas dokumentasi valid dan tautan relatif terselesaikan.
- Pengujian embedding dokumen `go test ./internal/web/ -run 'Doc|Changelog|Readme'` dan `go test -run 'Doc|Changelog|Readme' .` lolos.
