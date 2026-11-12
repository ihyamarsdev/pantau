# 0065. Upload ke S3 dari File Manager

Menambahkan unggahan file dan folder dari sebuah Host ke bucket S3-compatible, dengan beberapa akun S3 yang kredensialnya disimpan di **Settings > S3 Storage**. Melengkapi [ADR 0003](0003-piped-streaming-cross-host-transfer.md) (relay stream tanpa disk) dan [ADR 0035](0035-ssh-key-library.md) (pola penyimpanan kredensial).

## Keputusan Arsitektur

1. **Klien S3 mandiri di `internal/s3`, tanpa SDK**: hanya `net/http` dan paket crypto standar (SigV4, single PUT, multipart). `go.mod` tidak bertambah dependensi; SDK AWS akan menambah puluhan modul untuk empat operasi. Diuji terhadap vektor tanda-tangan resmi AWS dan server S3 nyata (Garage). Mendukung path-style dan virtual-hosted; tidak ada rantai kredensial AWS (env/IAM role).
2. **Streaming lewat Pantau, tanpa file sementara**: perintah `cat` (file) atau `tar -czf -` (folder) di Host dialirkan ke `Upload` lewat `io.Pipe`. RAM yang dipakai satu part (16 MB, otomatis lebih besar jika objek butuh lebih dari 9000 part). Folder disimpan sebagai `<nama>.tar.gz`; `tar` yang keluar dengan kode 1 (file berubah saat dibaca) tetap dianggap berhasil.
3. **Memakai ulang Transfer Manager**: job S3 memakai antrean, progres, kecepatan, ETA, dan pembatalan yang sama dengan Cross-Host Transfer (`kind: "s3"`), sehingga muncul di daftar yang sudah ada.
4. **Akun S3 di tabel `s3_accounts`** (pola yang sama dengan `cloudflare_accounts`): `name` unik tanpa memedulikan huruf besar/kecil, `endpoint`, `region`, `bucket`, `access_key`, `secret_key`, `prefix`, `path_style`. Jumlah akun tidak dibatasi; dialog unggah memilih akun lewat `account_id`. Secret key *write-only*: API hanya mengembalikan `secret_key_set`, dan nilai kosong saat mengubah akun tidak menghapus yang tersimpan. Disimpan plaintext seperti ADR 0035. Akun ikut System Snapshot. Pengaturan `s3_*` tunggal dari versi pertama dimigrasikan menjadi akun `Default` saat startup atau restore snapshot lama, lalu kuncinya dihapus dari tabel `settings`.
5. **Multipart yang gagal dibatalkan** (`AbortMultipartUpload`, konteks baru) agar part yatim tidak ditagih. Keterbatasan: tidak ada retry per-part; jalankan ulang job.

## Konsekuensi

- Objek dengan key sama ditimpa tanpa peringatan.
- Folder besar dikompres oleh CPU host sumber; tidak ada verifikasi checksum end-to-end selain yang dilakukan S3 (ETag per part).
- Endpoint internal/LAN diizinkan (MinIO lokal adalah kasus pakai utama); hanya skema http/https yang divalidasi.
