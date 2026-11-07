# 0061. Terraform: Backend Override, Operasi State, Linked Output, Audit, dan Auto-apply

Menambahkan beberapa kemampuan Terraform di atas [ADR 0056](0056-terraform-management-terraform-cloud-style.md) dan [ADR 0060](0060-terraform-cloud-accounts.md): backend lokal lewat berkas override (bukan flag `-state`), operasi state (`state rm`/`state mv`/import) sebagai versi state baru, Linked Output antar workspace, jejak audit permanen, notifikasi, plan terjadwal, auto-apply, dan template workspace.

## Konteks dan Masalah

1. Flag `-state` pada `plan`/`apply` sudah deprecated; selain itu backend yang dideklarasikan pengguna (`backend "s3"` dll.) dapat membuat Run menyentuh state di luar SQLite.
2. Memperbaiki state (resource yatim, pindah alamat, mengadopsi resource yang sudah ada) hanya mungkin lewat CLI manual di server, di luar riwayat State Version dan Workspace Lock.
3. Workspace sering bergantung pada output workspace lain (mis. id VPC); menyalinnya manual ke variabel mudah basi.
4. Kejadian Terraform hanya terlihat di log yang bersifat sementara; tidak ada jejak yang bertahan.
5. Operator tidak diberi tahu saat Run menunggu approval, gagal, atau terjadi drift; dan tidak ada plan terjadwal maupun jalur auto-apply untuk environment non-produksi.

## Keputusan

1. **Backend override, bukan `-state`**. Setiap Run menulis `pantau_backend_override.tf` (backend `local` dengan path di dalam root Run) dan menjalankan `init -reconfigure`. Berkas override menggantikan backend yang dideklarasikan pengguna, sehingga SQLite tetap sumber kebenaran state. Nama berkas itu dicadangkan: berkas workspace dengan nama sama ditimpa.
2. **Operasi state lewat State Version baru + Workspace Lock**. `state rm`, `state mv`, dan import (`POST /api/terraform/workspaces/{id}/state/ops`) dijalankan pada salinan state di direktori sementara, lalu hasilnya dicatat sebagai State Version **baru**; versi lama tidak diubah dan sumbernya dicatat (mis. `state rm x`), sehingga dapat di-rollback seperti versi lain. Workspace dikunci selama operasi (HTTP 423 bila sudah terkunci, 409 bila ada Run aktif). Dialog konfirmasi di UI menampilkan perintah persis yang akan dijalankan. Import menjalankan `init` dan memakai kredensial Cloud Account yang tertaut.
3. **Linked Output diselesaikan saat Run dimulai, tanpa `terraform_remote_state`**. Tautan (`GET/PUT /api/terraform/workspaces/{id}/output-links`) memetakan variabel Terraform ke output dari State Version terbaru workspace lain; nilainya disuntikkan sebagai variabel (HCL JSON), output sensitif tetap tersamarkan. Variabel workspace/varset eksplisit dengan nama sama menang atas tautan. Pemeriksaan siklus dilakukan saat menyimpan tautan (siklus ditolak). Output yang tidak tersedia menggagalkan awal Run dengan pesan jelas. Remote state tidak dipakai karena Pantau tidak menyediakan backend jarak jauh; tautan ikut dalam backup.
4. **Audit disimpan dari aliran logger, tanpa RBAC**. Event Terraform ditulis ke tabel `tf_audit` dari aliran log (5000 terbaru dipertahankan; `GET /api/terraform/audit`), sehingga semua jalur yang sudah mencatat log otomatis tercatat tanpa menyentuh tiap handler. Pantau hanya memiliki satu kata sandi admin sehingga tidak ada pemisahan peran atau identitas pelaku. Lonjakan lebih dari ~64 baris dalam satu jendela penulisan dapat membuang baris (penulisan dibatasi agar log tidak membebani DB).
5. **Notifikasi lewat kanal yang sudah ada**. Event (awaiting approval, errored, applied, drift; bawaan semua kecuali applied) dipilih di kartu Notifications tab Gate (`GET/PUT /api/terraform/notifications`, uji lewat `POST .../notifications/test`) dan dikirim lewat kanal Pantau Settings yang ada (Telegram/webhook/ntfy/Slack/Discord/SMTP). Drift hanya memberi notifikasi pada transisi tidak ada → ada agar tidak berulang.
6. **Plan terjadwal dan Auto-apply dibatasi dari produksi dan webhook**. `plan_interval_minutes` membuat Run plan bertipe `schedule`. `auto_apply` meng-apply plan yang memiliki perubahan dan berstatus kebijakan `passed` dengan komentar `auto-apply`, hanya untuk pemicu `schedule` dan `manual`. Run yang dipicu `webhook` **dikecualikan**: pemegang secret webhook tidak boleh dapat menyebabkan apply tanpa pengawasan. Environment produksi tidak boleh: ditolak 400 saat menyimpan, dan diabaikan saat runtime (flag dibaca ulang dari workspace sehingga perubahan ke produksi setelahnya menang). Run destroy/refresh dan Run dengan peringatan kebijakan tetap butuh persetujuan manual. Ini mempersempit batasan "tanpa auto-apply" pada ADR 0056; Approval Gate tetap wajib untuk produksi dan untuk semua Run yang dipicu webhook.
7. **Template workspace**. Katalog di kode (`GET /api/terraform/templates`): Hello world, AWS S3 bucket, AWS EC2 instance, GCP storage bucket, Azure resource group, DigitalOcean droplet, Hetzner server, Cloudflare DNS record, Kubernetes namespace. Template khusus provider memerlukan Cloud Account yang sesuai tertaut; sebagian punya variabel wajib tanpa default (`bucket_name`, `zone_id`, `name`, `content`).
8. **UI**: dialog Run dilebarkan ke 720px, modal dibatasi 96vw, strip tab dapat di-scroll pada layar sempit, dan tombol aksi state terlihat di setiap baris resource.

## Konsekuensi

- Run tidak lagi bergantung pada flag deprecated dan tidak bisa menulis state ke backend jarak jauh yang dideklarasikan pengguna.
- Perbaikan state dapat ditelusuri dan dibalik karena selalu menjadi State Version baru di bawah Workspace Lock.
- Nilai Linked Output adalah snapshot saat Run dimulai; bila workspace sumber berubah setelahnya, Run berjalan tetap memakai nilai lama.
- Audit tidak memiliki identitas pelaku dan dapat kehilangan baris saat lonjakan log.
- Auto-apply memperluas jangkauan perubahan otomatis pada plan terjadwal dan manual di workspace non-produksi yang mengaktifkannya; produksi dan Run yang dipicu webhook tetap lewat Approval Gate.

## Batasan yang Diketahui

- **Kredensial masih polos di penyimpanan** (SQLite): enkripsi at-rest Cloud Account dan nilai variabel belum dikerjakan (lihat addendum ADR 0060).
- **Import belum diuji terhadap provider nyata**: operasi sudah diimplementasikan dan diuji dengan skrip biner tiruan (shim), bukan dengan provider cloud sungguhan.
- **Template belum divalidasi `init`** selain Hello world: unduhan provider membutuhkan jaringan pada Run pertama, dan versi/atribut provider dapat berubah.
- Tidak ada RBAC pada audit, state operation, maupun auto-apply (satu kata sandi admin).

## Addendum: Terraform Registry

Integrasi penelusuran Terraform Registry publik (modul dan provider) ditambahkan ke tab Registry, tombol Registry di tab Configuration, dan kartu Registry di tab Gate.

### Keputusan

1. **Proksi sisi server dengan cache, bukan panggilan dari browser**. Browser tidak memanggil registry langsung (`GET /api/terraform/registry/modules`, `/providers`): registry tidak menjamin CORS, semua egress terpusat di satu titik (Pantau), dan base URL dapat diganti (`tf_registry_url`, `GET/PUT /api/terraform/registry/settings`, uji lewat `POST .../settings/test`) untuk mirror atau proxy air-gapped. Hasil di-cache di memori selama 5 menit.
2. **Snippet dibuat di sisi server**. Konstrain versi `~> MAJOR.MINOR` dan placeholder per tipe input dihitung di Go (diuji unit), sehingga UI hanya menyisipkan teks (di kursor, atau sebagai berkas `module-<name>.tf` / `provider-<type>.tf`, opsional dengan variabel input wajib ditambahkan ke `variables.tf`).
3. **Check versions membaca berkas yang tersimpan**. `GET /api/terraform/workspaces/{id}/registry/check` hanya untuk workspace inline dan membaca berkas tersimpan, bukan isi editor yang belum disimpan, sehingga hasilnya konsisten dengan yang akan dijalankan Run. Status: `up_to_date`, `outdated` (pin eksak di bawah versi terbaru), `newer_outside_constraint`, `unpinned`, `unknown` (sumber bukan registry). Tombol Apply hanya menulis ulang literal versi di editor; belum tersimpan sampai pengguna menekan Save.
4. **Kontrol SSRF**. Base URL harus `https` (kecuali `http` pada loopback) dan tanpa kredensial di URL; fetch memakai timeout 15 detik, batas respons 4 MiB, redirect hanya ke host yang sama, dan argumen path divalidasi. Teks dari registry dianggap tidak tepercaya dan ditampilkan dengan di-escape. Pencarian memerlukan kueri tidak kosong.

### Konsekuensi dan Batasan

- Pencarian provider memakai endpoint pencarian v2 registry yang tidak terdokumentasi dan dapat berubah.
- Hanya protokol Terraform Registry publik: tanpa token registry privat dan tanpa pencarian registry khusus OpenTofu.
- Run tetap mengunduh modul/provider sendiri saat `terraform init` dan membutuhkan akses jaringan dari server.
- Workspace Git tidak diperiksa; versi prerelease dibandingkan sebagai semver biasa.
