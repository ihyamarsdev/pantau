# Percepatan test `store`, `snapshot`, `web`

## Penyebab (dari profil CPU)

- **`store`**: ~75% CPU di keygen RSA-4096 dalam `initDefaults`, dipicu setiap `store.Open` pada file baru. Migrasi DDL ~13%.
- **`snapshot`**: ~80% CPU di keygen RSA-4096 (lewat `store.Open`). PBKDF2 hanya ~4%, bcrypt ~10%.
- **`web`**: ~41% CPU di `prepareHTML` (gzip BestCompression HTML embed) yang dijalankan ulang di setiap `NewServer`. Migrasi ~40%.
- Hipotesis PBKDF2 sebagai bottleneck utama **tidak terbukti**.

## Perubahan

1. `internal/web/server.go`: hasil gzip `prepareHTML` di-cache per sha256 isi (`htmlGzCache`). Output produksi identik.
2. `internal/store/*_test.go`: fresh `Open` diganti `TemplateCopy(t)` kecuali test migrasi legacy, test fresh-boot (`TestOpenUsesProductionKeyParams`, `TestOpenRestrictsDBFilePermissions`).
3. `internal/snapshot/export_test.go` (baru): template DB sekali per binary, `TemplateCopy(t)`. Dipakai `snapshot_test.go` dan `verify_test.go`.
4. Isolasi test (race nyata / flake):
   - `TestTFToolsValidateBusy`: tidak paralel, karena mengisi semaphore global `tfValidateSem`.
   - `TestKubeRunReadFailoverBudget`: tidak paralel, karena memutasi global `kubeFailoverBudget` (race terdeteksi).
   - `TestPortForwardCarriesBytes`: poll sampai counter kedua arah terhitung (counter produksi ditambah setelah `Write`).

## Hasil (`go test ./...`, semua paket, dijalankan bersamaan)

| Paket | Race sebelum | Race sesudah | Non-race sebelum | Non-race sesudah |
|---|---|---|---|---|
| internal/store | 58.8s | 47.4s | 11.9s | 7.1s |
| internal/snapshot | 33.1s | 14.6s | 12.4s | 1.7s |
| internal/web | 92.7s | 73.6s | gagal (flake 429) | 8.5s |
| Total wall | 160s (go test) | 129s (`make check`, termasuk vet + govulncheck + gosec) | 39s | 38s |

Non-race total hanya turun sedikit karena paket lain (vpnnet ~14s, notify ~12s) mendominasi wall.

## Verifikasi

- `make check`: exit 0, No vulnerabilities, gosec bersih.
- `make test-fast`: lulus.
- `go test -race -count=3 ./internal/store ./internal/snapshot ./internal/web`: lulus (exit 0).
- Jumlah test lulus paket target sama dengan baseline (881).

## Belum dikerjakan (perlu keputusan)

- **Migrasi per `Open`** adalah bottleneck berikutnya di bawah race. Butuh gerbang versi schema di `migrate()` agar template copy melewati DDL. Ini mengubah perilaku migrasi produksi, jadi tidak dikerjakan tanpa persetujuan.
- `PRAGMA synchronous=OFF` untuk test: butuh hook di produksi. fsync bukan bottleneck di profil, jadi dilewati.
- 51 `time.Sleep` di test web belum diganti dengan sinkronisasi deterministik.
- Override PBKDF2 khusus test: hanya ~4% CPU snapshot, dilewati.
