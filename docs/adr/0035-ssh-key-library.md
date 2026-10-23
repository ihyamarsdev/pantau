# 0035. SSH Key Library Terpusat dan Resolusi Kunci Deterministik per Host

Menyediakan katalog penyimpanan kunci privat SSH terpusat (*SSH Key Library*) di menu Settings Pantau, memungkinkan pendaftaran kunci privat sekali untuk digunakan berulang kali oleh banyak Host melalui dropdown, didukung hirarki resolusi kunci deterministik, derivasi kunci publik otomatis, proteksi penghapusan kunci aktif, dan pencadangan terenkripsi.

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, manajemen kredensial SSH di Pantau memiliki keterbatasan signifikan:
1. **Duplikasi Kredensial & Input Manual Berulang**: Setiap Host hanya dapat diautentikasi menggunakan kunci privat global tunggal (`settings.ssh_private_key`) atau kunci kustom per-host (`custom_key`). Jika sebuah kelompok server (misal: klaster Kubernetes, VPC staging, atau server dari penyedia cloud tertentu) menggunakan pasangan kunci yang sama, administrator terpaksa menempelkan (*paste*) teks kunci privat yang panjang secara manual ke form setiap host.
2. **Ketiadaan Inventaris Kunci**: Pengguna tidak memiliki visibilitas terpusat terhadap kunci-kunci SSH yang terdaftar, nama aliasnya, sidik jari kriptografis (*fingerprint*), maupun daftar host yang mengonsumsi kunci tersebut (*used_by*).
3. **Risiko Akses Putus Akibat Penghapusan Ceroboh**: Tanpa pelacakan relasi penggunaan kunci, kunci privat yang dihapus dapat seketika memutus inspeksi telemetri, terminal web, dan transfer berkas pada host-host terkait.
4. **Kebocoran Kunci Privat Melalui API atau Log**: Kunci privat merupakan rahasia sensitif tingkat tinggi. Kunci privat tidak boleh bocor melalui endpoint REST API, tidak boleh tampak pada antarmuka GET, dan tidak boleh tercetak pada stream log aktivitas.
5. **Kunci dengan Frasa Sandi (Passphrase-Protected Keys)**: Pantau beroperasi secara non-interaktif (*agentless daemon*). Kunci privat yang diproteksi passphrase memerlukan prompt interaktif yang tidak didukung saat eksekusi inspeksi latar belakang.

## Keputusan Arsitektur

1. **Model Data dan Tabel SQLite `ssh_keys`**:
   - Menambahkan tabel basis data `ssh_keys`:
     ```sql
     CREATE TABLE IF NOT EXISTS ssh_keys (
         id INTEGER PRIMARY KEY AUTOINCREMENT,
         name TEXT NOT NULL UNIQUE,
         private_key TEXT NOT NULL,
         public_key TEXT NOT NULL,
         fingerprint TEXT NOT NULL,
         created_at DATETIME DEFAULT CURRENT_TIMESTAMP
     );
     ```
   - Menambahkan kolom `key_id INTEGER DEFAULT 0` pada tabel `hosts` melalui migrasi otomatis `alterAddColumn`. Nilai `0` menandakan host tidak menggunakan kunci dari library.
   - Model Go `store.SSHKey`:
     ```go
     type SSHKey struct {
         ID          int64     `json:"id"`
         Name        string    `json:"name"`
         PublicKey   string    `json:"public_key"`
         Fingerprint string    `json:"fingerprint"`
         CreatedAt   time.Time `json:"created_at"`
         UsedBy      int       `json:"used_by"`
         PrivateKey  string    `json:"-"`
     }
     ```
   - Properti `PrivateKey` ditandai dengan tag JSON `json:"-"` sehingga tidak pernah diserialisasi ke respons API apa pun.

2. **Hirarki Resolusi Kunci Deterministik (Strict Key Resolution)**:
   - Seluruh subsistem koneksi SSH (inspeksi berkala di `inspector`, transfer berkas di `transfer`, terminal web, tunnel port, dan diagnostik jaringan) menggunakan urutan resolusi seragam:
     1. **Library Key (`key_id > 0`)**: Sistem mencari entri kunci di tabel `ssh_keys`. Jika `key_id` yang ditunjuk tidak ditemukan di basis data, koneksi **langsung digagalkan dengan pesan error eksplisit**. Sistem dilarang melakukan fallback senyap ke kunci global untuk mencegah kebingungan operasional atau pelanggaran batas akses.
     2. **Custom Key (`custom_key` tidak kosong)**: Jika `key_id == 0` dan host memiliki `custom_key` tersimpan, sistem menggunakan kunci kustom tersebut.
     3. **Global SSH Key (`settings.ssh_private_key`)**: Jika `key_id == 0` dan `custom_key` kosong, koneksi menggunakan kunci privat global bawaan sistem.
   - Saat sebuah Host dibuat atau diperbarui dengan `key_id > 0`, nilai `custom_key` pada host tersebut otomatis dikosongkan untuk menjamin konsistensi status kredensial.

3. **Derivasi Kunci Publik & Fingerprint Server-Side**:
   - Saat pengguna menambahkan kunci privat baru melalui API atau UI:
     - Format baris baru (*trailing newline*) dinormalisasi secara otomatis.
     - Kunci diparsing menggunakan `golang.org/x/crypto/ssh` (`ssh.ParsePrivateKey`).
     - Jika parsing gagal karena kunci korup atau membutuhkan passphrase (`*ssh.PassphraseMissingError`), sistem menolak dengan status `400 Bad Request` disertai pesan kesalahan yang jelas.
     - Kunci publik OpenSSH (`authorized_keys` format) dan sidik jari SHA-256 (`SHA256:...` via `ssh.FingerprintSHA256`) diekstraksi dan disimpan ke basis data bersama kunci privat.

4. **REST API SSH Key Library & Proteksi Penghapusan**:
   - Seluruh endpoint dilindungi oleh `authMiddleware`:
     - `GET /api/ssh-keys`: Mengembalikan daftar seluruh kunci yang tersimpan (`[SSHKey...]`), lengkap dengan metadata `used_by` (jumlah host aktif yang merujuk pada `key_id` tersebut). Field `private_key` dijamin tidak ada di payload JSON.
     - `POST /api/ssh-keys`: Menerima payload `{"name":"...","private_key":"..."}`. Mengembalikan status `201 Created` beserta metadata `SSHKey`. Menghasilkan `400 Bad Request` jika nama kosong, format kunci salah, atau kunci ber-passphrase. Menghasilkan `409 Conflict` jika nama kunci sudah digunakan.
     - `DELETE /api/ssh-keys/{id}`: Menghapus kunci dari library. Mengembalikan `404 Not Found` jika ID tidak ada. Jika `used_by > 0` (masih ada host yang menggunakannya), penghapusan **ditolak dengan status `409 Conflict`**. Sistem menolak menghapus dan tidak melakukan detach sepihak agar koneksi host tidak putus mendadak.
   - Endpoint Host (`POST /api/hosts` dan rute pembaruan host) menerima parameter `key_id`. Jika `key_id` yang dikirimkan tidak valid atau tidak ada, sistem mengembalikan `400 Bad Request`.

5. **One-Time Password Provisioning & Injeksi Kunci Kontekstual**:
   - Saat pengguna menambahkan host baru dengan autentikasi password satu-kali (*Key Provisioning*), atau memicu aksi `inject-key` pada Host Detail:
     - Jika host mengonfigurasi `key_id > 0`, sistem menyuntikkan *public key* milik Library Key terpilih ke berkas `~/.ssh/authorized_keys` di target.
     - Jika host tidak memilih library key (`key_id == 0`), sistem menyuntikkan public key global bawaan sistem.
     - Password SSH sementara langsung dibersihkan dari RAM setelah injeksi selesai.

6. **Pencadangan & Pemulihan Bencana (Encrypted System Snapshot)**:
   - Seluruh rekaman tabel `ssh_keys` disertakan dalam payload `SnapshotPayload` yang dienkripsi menggunakan AES-256-GCM.
   - Impor snapshot mendukung kompatibilitas mundur dengan snapshot versi lama yang belum memiliki entri `ssh_keys`.
   - Proses impor memulihkan ID kunci apa adanya sehingga asosiasi `hosts.key_id` tetap utuh.

7. **Antarmuka Pengguna Terintegrasi**:
   - **Tab Pengaturan `🔑 SSH Keys`**: Menampilkan tabel daftar kunci terdaftar (Nama, Fingerprint, Jumlah Host Terhubung, Tanggal Dibuat, dan tombol Hapus), formulir penambahan kunci (nama, textarea PEM, dan impor dari berkas).
   - **Form Tambah & Edit Host**: Dilengkapi pilihan dropdown **SSH Key** yang menampilkan daftar Library Key terdaftar, dengan opsi default menggunakan Global Key atau opsi menginput Custom Key secara manual.

## Konsekuensi

- **Efisiensi & Skalabilitas Operasional**: Administrator cukup mendaftarkan kunci privat sekali dan memilihnya dari dropdown saat mengonfigurasi puluhan server dalam satu VPC/klaster.
- **Pemberantasan Kegagalan Koneksi Akibat Human Error**: Validasi integritas kunci saat input, penolakan penghapusan kunci aktif, dan penolakan fallback senyap pada `key_id` yang hilang mencegah downtime inspeksi yang membingungkan.
- **Zero-Credential-Leakage**: Kunci privat tidak pernah keluar melalui respons HTTP API, disensor secara deterministik dari log sistem (`[REDACTED]`), dan terlindungi dalam arsip snapshot terenkripsi.
- **Kompatibilitas Penuh**: Tidak mengubah arsitektur agentless SSH, mempertahankan kompatibilitas dengan custom key manual dan global key yang sudah berjalan.
