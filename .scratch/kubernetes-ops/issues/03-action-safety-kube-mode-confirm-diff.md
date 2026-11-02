# 03 — Keamanan aksi: `kube_mode`, konfirmasi, pratinjau diff

Type: task
Status: resolved
Blocked by:

## Tujuan

Operator dapat membatasi apa yang boleh dilakukan Pantau pada klaster suatu Host, aksi berbahaya meminta konfirmasi berdampak jelas, dan Apply menampilkan perbedaan sebelum menerapkan. Rancangan: [spec.md](../spec.md) bagian C.

## Perubahan

Store (`internal/store/db.go`)
- Kolom `hosts.kube_mode TEXT NOT NULL DEFAULT 'full'` (`full|no-exec|read-only`) via `alterAddColumn`; `Host.KubeMode`; `hostSelectCols` (`COALESCE`), `scanHost`, `CreateHost`/`UpdateHost`, `ImportSnapshot` (kosong → `full`). Validasi nilai di handler host (tolak selain tiga nilai).

Backend
- `handlers_kube.go`: `read-only` → 403 `{"ok":false,"error":…}` untuk `action`, `apply`; `no-exec` dan `read-only` → 403 untuk `exec-cmd`. `GET logs` dan `/ws/kube/logs` tetap diizinkan.
- `server.go handleWSTerminal`: bila Host `no-exec`/`read-only`, tolak koneksi yang `cmd`-nya (atau perintah preset) cocok `(kubectl|kc|k3s\s+kubectl).*\bexec\b`, dengan pesan `[Pantau Policy] …`.
- `kube.go`: `KubeDiff(hostID, yaml) (output string, changed bool, err error)` — validasi sama dengan Apply (≤64 KB), `kc apply --dry-run=server` lalu `kc diff -f`; exit 0 = tak ada beda, **exit 1 = ada beda (bukan galat)**, >1 = galat. Potong output 256 KB. Rute `POST …/kube/diff`; diizinkan pada semua mode (read-only).

Frontend (`index.html`, dialog Host)
- Dropdown *Kubernetes Mode* di dialog tambah/ubah Host (`host_kube_mode`), nilai dikirim pada `POST/PUT /api/hosts`.
- Menu aksi: item nonaktif + `title` (tooltip) bila diblokir; tambahkan dukungan `title` pada `dockerFloatingMenu`. Badge *Read-Only* di tab Apply.
- `showConfirm` dengan dampak konkret:
  - Scale ke 0: menyebut jumlah pod yang berhenti; StatefulSet menyebut PVC tetap ada.
  - Restart rollout: menyebut jumlah replika yang dibuat ulang bertahap.
  - Hapus Pod: menyebut node dan bahwa pod tak terkelola hilang permanen.
  - Cordon: workload tetap berjalan, tak ada pod baru dijadwalkan.
- Tab Apply: tombol utama *Preview diff*; *Apply* terkunci sampai diff untuk isi YAML **saat ini** selesai, kembali terkunci bila teks berubah; hasil: diff berwarna / "tidak ada perubahan" / galat; *Apply* tetap lewat `showConfirm`.
- i18n EN|ID. UI menyatakan jujur: `no-exec` adalah pagar aplikasi; batas keamanan sebenarnya adalah izin kubeconfig/sudoers/RBAC.

## Acceptance

- Unit test: matriks mode × endpoint (200/403), regex pemblokir `/ws/terminal` (cocok dan tidak cocok, tanpa false-positive untuk kata "exec" di tempat lain seperti `docker exec`), `KubeDiff` pada exit 0/1/2 dan pemotongan output, migrasi + snapshot lama.
- Smoke di browser: tiap mode di Host uji; diff pada manifest ConfigMap yang belum ada di `odin` (read-only; pastikan tidak ada objek terbuat). Mutasi **tidak** dijalankan di `odin`.
- `go build ./... && go vet ./internal/... && go test ./internal/inspector/ ./internal/store/ ./internal/web/`.

## Catatan

ADR 0044 (bersama 01) mencatat batas penegakan PTY. CONTEXT.md: **Kube Mode**.
