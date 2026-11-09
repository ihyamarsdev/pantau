# 0063. Integrasi Docker Hub pada Tab Docker

Menghubungkan manajemen Docker ([ADR 0042](0042-docker-management-portainer-style.md)) dengan Docker Hub: mencari repositori, membaca ringkasan (README) dan daftar tag, menarik (*pull*) atau menjalankan tag tertentu di Host, serta **Image Update Check** yang membandingkan image lokal dengan digest yang kini disajikan Docker Hub untuk tag yang sama.

## Konteks dan Masalah

1. Formulir *Pull image* hanya menerima nama image yang diketik. Administrator membuka hub.docker.com di peramban lain untuk mencari nama repositori yang benar, tag yang tersedia, dan platform (amd64/arm64) yang didukung.
2. Tag seperti `latest` atau `1.27-alpine` bergerak: Docker Hub menerbitkan ulang tag yang sama dengan isi baru (patch keamanan). Pantau tidak memberi tahu bahwa image lokal sudah tertinggal dari tag yang sama di Docker Hub.
3. Docker Hub membatasi *pull* anonim (100 per 6 jam per IP) dan API Hub (~180 permintaan per menit per IP, header `x-ratelimit-*`). Cek pembaruan tidak boleh menghabiskan kuota pull Host.
4. Pantau harus tetap berfungsi di lingkungan *airgapped* ([ADR 0013](0013-airgapped-self-contained-web-assets.md)); fitur yang butuh internet harus gagal secara jujur, bukan merusak tab Docker.

## Keputusan Arsitektur

1. **Permintaan dari server Pantau, data publik saja.** Paket `internal/dockerhub` memanggil Docker Hub dari server Pantau (pola sama dengan Terraform Registry): pencarian lewat API pencarian registry yang dipakai `docker search` (`index.docker.io/v1/search`), repositori dan tag lewat Hub API `hub.docker.com/v2/namespaces/{ns}/repositories/{repo}[/tags[/{tag}]]`. Tanpa kredensial, tanpa penyimpanan ke DB; respons di-cache di memori 5 menit (maks 512 entri), timeout 15 detik, body maks 4 MB, README dipotong 100 KB. Bila Docker Hub tidak terjangkau, UI menampilkan galat "Docker Hub is unreachable from the Pantau server" dan sisa tab Docker tetap bekerja.
2. **Validasi sebelum keluar.** Namespace/nama repositori mengikuti tata bahasa referensi Docker (`[a-z0-9]+([._-]+[a-z0-9]+)*`, maks 255), tag `[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}`, filter tag `[A-Za-z0-9_.-]{0,128}`, kueri 1–100 karakter, halaman 1–100. Masukan lain ditolak HTTP 400 tanpa permintaan ke Docker Hub.
3. **API**: `GET /api/dockerhub/search?q=&page=`, `GET /api/dockerhub/repo/{ns}/{name}`, `GET /api/dockerhub/tags/{ns}/{name}?name=&page=` (urut push terbaru, 50 per halaman; 404 setelah halaman terakhir dibaca sebagai halaman kosong). Galat: 400 masukan ditolak, 404 tidak ada di Docker Hub, 429 batas laju Docker Hub, 502 galat lain di hulu.
4. **Pull dan Run tetap lewat jalur ADR 0042.** Tombol *Pull* memakai `POST /api/hosts/{id}/docker/image` (`docker pull` di Host, keluaran mengalir ke log viewer, Docker Privilege Fallback). Tombol *Run* membuka formulir *Add container* yang sudah ada dengan image terisi. Pull memakai jaringan dan kuota Docker Hub **Host**, bukan server Pantau.
5. **Image Update Check** (`GET /api/hosts/{id}/docker/images/updates`, tombol *Check updates* di sub-tab Images, hanya saat diminta):
   - Host menjalankan `docker image inspect --format '{{.Id}}|{{join .RepoTags ","}}|{{join .RepoDigests ","}}'` untuk semua image; `docker images` yang ditolak menggagalkan cek (bukan dianggap "tidak ada image").
   - Setiap `repo:tag` yang berasal dari Docker Hub (tanpa host registry, atau `docker.io`/`index.docker.io`/`registry-1.docker.io`) dibandingkan dengan `GET …/tags/{tag}`. Status: `up_to_date` bila digest lokal untuk repositori itu sama dengan digest index tag atau digest manifest salah satu platformnya; `update_available` bila berbeda; `not_found` bila tag tidak ada di Docker Hub; `unknown` bila engine tidak mencatat digest registry (image dibangun/di-*load* lokal, tanpa permintaan ke Docker Hub); `error` dengan alasannya. Image dari registry lain tidak diikutkan.
   - Permintaan di-deduplikasi per `ns/name:tag`, 4 paralel, maksimal 100 tag per cek, dan berhenti pada kegagalan pertama selain 404 (tak terjangkau, 429, timeout); alasan itu dilaporkan untuk sisa image. Hub API tidak memotong kuota pull.
   - Hasil disimpan di memori peramban per Host (tidak di DB). Badge di kolom Status, tombol *Pull update*, dan ringkasan jumlah pembaruan tampil di tabel Images. Setelah *Pull update* berhasil, badge menjadi *Up to date* dan pengguna diingatkan bahwa container yang berjalan tetap memakai image lama sampai dibuat ulang (Compose: Stacks → Update).
6. **UI**: modul `static/app/dockerhub/` (bundel `/app/dockerhub.js` + `/app/dockerhub.css`, namespace `Hub`, i18n EN/ID sendiri) membuat modal *Docker Hub* (pencarian → detail repositori dengan tab Tags dan Overview) dan menyuplai tombol, badge, serta ringkasan ke sub-tab Images. README dirender dengan renderer Markdown aman milik Terraform Registry (`TfUI.reg.md`: HTML di-escape, gambar tidak dimuat, tautan hanya http(s)). Hasil pencarian resmi (*Official*) ditampilkan lebih dulu dalam satu halaman.

## Konsekuensi

- Mencari dan menarik image tidak lagi butuh peramban kedua; tag dan platform terlihat sebelum pull.
- Administrator melihat image yang tertinggal dari tag-nya di Docker Hub tanpa menarik apa pun.
- Server Pantau perlu akses HTTPS keluar ke `hub.docker.com` dan `index.docker.io` untuk fitur ini; Host perlu akses ke registry Docker Hub untuk pull. Bila hanya salah satu yang punya internet, hanya bagian itu yang bekerja.
- Tidak ada dukungan repositori privat, `docker login`, atau registry lain (ghcr.io, quay.io, registry sendiri); image dari registry tersebut tidak dicek.
- Cek pembaruan tidak berjalan otomatis dan tidak mengirim notifikasi; pull tidak membuat ulang container.
- Image lokal bernama sama dengan repositori Docker Hub (mis. hasil build `nginx:latest`) dilaporkan `update_available` karena digest-nya berbeda; *Pull update* akan menimpa tag lokal itu.
