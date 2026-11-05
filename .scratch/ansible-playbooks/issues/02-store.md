# 02 — Penyimpanan Playbook, Riwayat Run & Integrasi Snapshot (internal/store)

Type: task
Status: ready-for-agent
Blocked by:

## Tujuan

Menyediakan persistensi data untuk Ansible Playbook dan Playbook Run di SQLite lokal (`internal/store/ansible.go`), pengaturan Ansible Gate (`ansible_enabled`), serta integrasi snapshot cadangan terenkripsi di `internal/store/db.go`.

## Perubahan

- Migrasi skema database di `internal/store/db.go`:
  - Tabel `ansible_playbooks` (`id INTEGER PRIMARY KEY AUTOINCREMENT`, `name TEXT UNIQUE COLLATE NOCASE`, `description TEXT DEFAULT ''`, `content TEXT NOT NULL`, `extra_vars TEXT DEFAULT ''`, `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`, `updated_at DATETIME DEFAULT CURRENT_TIMESTAMP`).
  - Tabel `ansible_runs` (`id INTEGER PRIMARY KEY AUTOINCREMENT`, `playbook_id INTEGER NOT NULL`, `playbook_name TEXT NOT NULL`, `status TEXT NOT NULL`, `mode TEXT NOT NULL`, `targets TEXT NOT NULL`, `extra_vars TEXT DEFAULT ''`, `tags TEXT DEFAULT ''`, `skip_tags TEXT DEFAULT ''`, `verbosity INTEGER DEFAULT 0`, `become INTEGER DEFAULT 0`, `exit_code INTEGER`, `log TEXT DEFAULT ''`, `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`, `started_at DATETIME`, `finished_at DATETIME`, `FOREIGN KEY (playbook_id) REFERENCES ansible_playbooks(id) ON DELETE CASCADE`).
- Implementasi `internal/store/ansible.go`:
  - Model `AnsiblePlaybook` dan `AnsibleRun`.
  - Galat terdefinisi: `ErrAnsiblePlaybookNameTaken`, `ErrAnsiblePlaybookNotFound`, `ErrAnsibleRunNotFound`.
  - CRUD Playbook: `CreateAnsiblePlaybook`, `GetAnsiblePlaybook`, `ListAnsiblePlaybooks` (tanpa kolom content, menyertakan `LastStatus` dan `LastRunAt` dari run terakhir), `UpdateAnsiblePlaybook`, `DeleteAnsiblePlaybook` (beserta riwayat runs dalam transaksi).
  - Manajemen Run: `CreateAnsibleRun`, `StartAnsibleRun`, `FinishAnsibleRun`, `GetAnsibleRun` (lengkap dengan log), `ListAnsibleRuns` (tanpa log, terurut terbaru, limit default 50 maks 200), `PruneAnsibleRuns` (mempertahankan N run terbaru yang selesai), `MarkAnsibleRunsInterrupted` (mengubah status queued/running menjadi interrupted saat server start).
  - Helper pengaturan Ansible Gate: `GetAnsibleEnabled() bool` dan `SetAnsibleEnabled(bool) error` pada tabel `settings`.
- Integrasi Snapshot di `internal/store/db.go`:
  - Ekspor/impor `AnsiblePlaybooks` pada System Snapshot terenkripsi.
  - Tabel `ansible_playbooks` masuk daftar tabel restore; tabel `ansible_runs` dikosongkan bersama restore dan TIDAK disertakan dalam snapshot.
- Unit test komprehensif di `internal/store/ansible_test.go` dan pengujian snapshot.

## Acceptance

- `go test -v ./internal/store/ -run 'Ansible|Snapshot'` lolos.
- Playbook tersimpan unik case-insensitive dan terintegrasi aman dengan siklus snapshot restore.
