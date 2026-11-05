# 03 — HTTP Handlers & Run Manager Ansible Playbooks (internal/web)

Type: task
Status: ready-for-agent
Blocked by: 01, 02

## Tujuan

Menyediakan endpoint REST API di bawah `/api/ansible/...` terautentikasi (`authMiddleware`) dan manager eksekusi background (`ansible_runs.go`) untuk status ketersediaan/gate, manajemen playbook CRUD, eksekusi run asinkron, streaming log real-time polling dengan offset, dan pembatalan run.

## Perubahan

- Implementasi manager eksekusi `internal/web/ansible_runs.go`:
  - Pelacakan goroutine run aktif dengan `context.CancelFunc` untuk pembatalan.
  - Batas konkurensi: 1 run aktif per playbook (409) dan maksimal 3 run aktif global (429).
  - Buffer log memori per run aktif hingga batas 2 MiB dengan pemotongan bagian awal dan penyisipan penanda `[pantau] log truncated`.
  - Penyelesaian run: memanggil `store.FinishAnsibleRun`, dilanjutkan `store.PruneAnsibleRuns(id, 100)`.
- Implementasi handler HTTP di `internal/web/handlers_ansible.go`:
  - `GET /api/ansible/status`: cek `available`, `version`, `error`, `enabled`, dan `max_concurrent`.
  - `PUT /api/ansible/status`: ubah status `enabled`; tolak 409 jika biner tidak tersedia.
  - `GET /api/ansible/playbooks`: daftar playbook.
  - `POST /api/ansible/playbooks`: tambah playbook baru (validasi nama 1..64, content 1..256 KiB, extra_vars objek JSON valid).
  - `GET /api/ansible/playbooks/{id}`: ambil detail playbook lengkap dengan content.
  - `PUT /api/ansible/playbooks/{id}`: ubah playbook.
  - `DELETE /api/ansible/playbooks/{id}`: hapus playbook (tolak 409 bila ada run yang sedang aktif).
  - `POST /api/ansible/playbooks/{id}/run`: picu eksekusi baru (202 Accepted); resolusi target dari gabungan `host_ids` dan `group`, pengambilan kunci privat efektif via `store.ResolveHostPrivateKey`.
  - `GET /api/ansible/playbooks/{id}/runs`: riwayat run per playbook.
  - `GET /api/ansible/runs`: riwayat run lintas semua playbook.
  - `GET /api/ansible/runs/{id}`: detail run (tanpa log).
  - `GET /api/ansible/runs/{id}/log?offset=N`: ambil log potongan byte sejak offset N; dari buffer memori jika aktif atau dari DB jika selesai.
  - `POST /api/ansible/runs/{id}/cancel`: batalkan run aktif.
  - Penegakan gerbang: tolak 409 `{"error":"ansible is disabled"}` bila Ansible Gate mati atau biner tidak tersedia untuk seluruh rute selain `status`.
- Integrasi server di `internal/web/server.go`:
  - Field `ansibleRun` dan `ansibleAvail` untuk injeksi dependency/mocking saat pengujian.
  - Pemanggilan `store.MarkAnsibleRunsInterrupted` pada inisialisasi server.
  - Pencatatan aktivitas ke logger terstruktur (tanpa memaparkan kunci/extra_vars).
- Unit test di `internal/web/handlers_ansible_test.go` dan `internal/web/ansible_runs_test.go`.

## Acceptance

- `go test -v ./internal/web/ -run 'Ansible'` lolos.
- Seluruh endpoint mematuhi kontrak status HTTP, validasi muatan, dan manajemen konkurensi.
