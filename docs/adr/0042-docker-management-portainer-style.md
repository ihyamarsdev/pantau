# 0042. Manajemen Docker ala Portainer

Memperluas tab Docker yang semula hanya daftar container + log + aksi start/stop/restart menjadi pengelolaan lengkap: container, image, volume, network, stack Docker Compose, dan pembersihan.

## Konteks dan Masalah

1. Administrator masih harus membuka terminal untuk menjalankan container baru, melihat konfigurasi lengkapnya, menarik image, atau mengelola stack Compose.
2. Pantau agentless: seluruh operasi harus lewat SSH dengan akun yang sama seperti inspeksi, tanpa menyimpan kata sandi.
3. Operasi Docker sangat destruktif (hapus volume, prune); salah klik atau masukan berbahaya tidak boleh berubah menjadi injeksi shell.

## Keputusan Arsitektur

1. **Satu lapisan perintah** (`internal/inspector/docker_manage.go`). Setiap operasi adalah skrip `sh -c` yang dibungkus `dockerShell(body)`. Prelude mendefinisikan fungsi shell `docker()` yang memakai `sudo -n docker` **hanya** bila `docker ps` gagal dengan "permission denied" dan `sudo -n true` berhasil; selain itu `docker` biasa. Dengan begitu skrip lama (daftar container, statistik, volume, network) tidak berubah, tidak pernah memakai kata sandi, dan host yang sudah di grup `docker` tidak menyentuh sudo sama sekali.
2. **Validasi dan quoting**. Nama container/volume/network/stack, nama image, mode restart, port, dan batas memori/CPU divalidasi dengan pola ketat (`ErrDockerInvalid` → HTTP 400) dan setiap nilai tetap di-quote dengan `shellQuote`. Perintah container baru dirakit sebagai argumen (`buildDockerRun`), bukan string bebas; kolom *Command* dipecah oleh `splitCommandLine` (tanda kutip didukung, tanpa ekspansi shell). Nilai env disimpan apa adanya; `;`/`$()` di dalamnya tidak dieksekusi.
3. **Aksi container**: start, stop, restart, pause, unpause, kill, remove (opsi `force` dan `volumes`), rename. Menghapus container yang berjalan harus eksplisit (`force`); tanpa itu Docker menolak dan galatnya ditampilkan apa adanya.
4. **Inspect** membaca `docker inspect` dan mengirim ringkasan terstruktur (`DockerContainerDetail`). Nilai environment disembunyikan di UI sampai pengguna menekan *Show values* karena sering memuat rahasia.
5. **Stack Compose**. Daftar stack diturunkan dari label `com.docker.compose.*` pada container, bukan dari berkas di disk. Aksi `up/down/start/stop/restart/pull/recreate/update` memakai `docker compose -p NAME -f FILE …`; path berkas dari label divalidasi (absolut, `.yml/.yaml`, tanpa `..`, kutip, atau newline). Edit hanya untuk proyek satu berkas. **Deploy** menulis YAML (maks 64 KB, via base64) ke `~/.pantau/stacks/<nama>/docker-compose.yml` atau menimpa berkas tunggal proyek yang ada dengan cadangan `.pantau-bak`; sebelum `up` dijalankan `docker compose config` — YAML tidak valid ditolak tanpa menyentuh container. Nama proyek `^[a-z0-9][a-z0-9_-]{0,63}$`.
6. **Prune** hanya per target: `containers`, `images`, `images_all`, `volumes`, `networks`, `builder`. Tidak ada target "semua" (`docker system prune -a --volumes`); target tak dikenal ditolak (400). UI menampilkan Confirmation Dialog per target dengan peringatan permanen khusus untuk volume.
7. **Console** adalah tab Terminal Dock yang menjalankan `docker exec -it NAME sh -c 'bash atau sh'` (dengan `sudo docker` bila perlu), memakai ulang jembatan SSH PTY yang sudah ada alih-alih membuat WebSocket baru.
8. **UI**: semua masukan lewat satu modal formulir generik (`#dockerFormModal`) karena `prompt()` dilarang; keluaran panjang (pull, compose, prune) memakai ulang modal log Docker. Aksi sekunder berada di menu titik tiga yang mendukung item nonaktif.
9. **API**: `GET /api/hosts/{id}/docker/{images|system|stacks|inspect|stack/file}`, `POST …/{action|run|image|volume|network|stack|stack/deploy|prune}`, semua ditangani `handleDockerManage` (`internal/web/handlers_docker.go`). Hasil dicatat ke Activity Console (modul `docker`).

## Konsekuensi

- Siklus hidup container dan stack dikelola dari satu tempat; terminal hanya untuk kebutuhan lanjutan.
- Host tanpa akses socket Docker maupun sudo tanpa kata sandi menampilkan galat Docker asli.
- Aksi menyentuh host produksi: Pantau tidak menyimpan riwayat/undo. Penghapusan volume dan prune volume bersifat permanen.
- `docker compose` v2 diperlukan untuk Stacks; host dengan Compose lama hanya mendapat galat dari perintahnya.
- Stack yang dibuat Pantau berada di `~/.pantau/stacks/` akun SSH; menghapus stack lewat *Down* tidak menghapus direktori itu atau volume bernama.
- Diuji langsung terhadap Docker 29.8.1/Compose 5.5.1 dengan objek `pantau-qa-*` (lifecycle, volume, network, tag image, deploy/edit/down stack, console); prune hanya diuji unit karena destruktif.
