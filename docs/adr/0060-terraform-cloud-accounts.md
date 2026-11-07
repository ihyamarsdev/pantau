# 0060. Terraform Cloud Accounts: Kredensial Provider Cloud yang Disuntikkan ke Run

Menambahkan **Cloud Accounts**: kredensial provider cloud (AWS, Google Cloud, Microsoft Azure, DigitalOcean, Hetzner) didaftarkan sekali di Pantau, dihubungkan ke satu atau lebih Terraform Workspace, dan disuntikkan sebagai variabel lingkungan ke setiap Run workspace tersebut.

Melengkapi [ADR 0056](0056-terraform-management-terraform-cloud-style.md). Tidak mengubah model Variable Set; hanya menambah sumber env baru dengan prioritas lebih rendah dari variabel `env`.

## Konteks dan Masalah

1. Terraform dipakai untuk layanan cloud (AWS, GCP, Azure, ...), tetapi satu-satunya cara memberi kredensial adalah menyalinnya ke variabel `env` per workspace atau Variable Set. Nama variabel provider harus dihafal operator, dan kredensial yang sama tersebar di banyak tempat.
2. Tidak ada cara memeriksa apakah kredensial valid sebelum Run gagal di tengah `plan`.
3. Kredensial tidak boleh bocor melalui API, log Run, atau DOM.

## Keputusan

1. **Disimpan di DB, seperti `cloudflare_accounts.api_token` dan `git_token`**: tabel `tf_cloud_accounts` (nama unik tanpa membedakan huruf, provider, deskripsi, `fields` JSON, hasil verifikasi terakhir). Seluruh field (rahasia dan bukan) disimpan polos di SQLite dan bertag `json:"-"`. Secret manager eksternal (Vault, AWS Secrets Manager, ...) tidak dipakai sekarang: menambah dependensi dan konfigurasi untuk binary tunggal self-hosted, sedangkan kredensial lain di Pantau sudah memakai DB dan backup snapshot sudah terenkripsi.
2. **Katalog provider di kode** (`terraform.CloudProviders()`): tiap provider mendefinisikan field (label, rahasia, wajib, multiline, default) dan variabel lingkungan yang diisi. Katalog yang sama dipakai validasi server dan form UI, sehingga menambah provider cukup satu entri. Nilai dibatasi 16 KiB; GCP `credentials_json` harus JSON valid dengan `type`, `client_email`, `private_key`.
3. **Suntikan lewat `Config.Env`**: pembangun konfigurasi Run mengisi env dengan `CloudEnv` semua akun terhubung (urut id, yang belakangan menang), LALU variabel efektif kategori `env` (menimpa). Karena memakai `Config.Env`, sensor yang sudah ada di paket `terraform` (setiap nilai `spec.Env` >= 4 karakter disensor dari log plan/apply) otomatis berlaku. Semua jalur yang membangun konfigurasi (plan, apply, drift, state, cost, scheduler, webhook) lewat pembangun yang sama.
4. **Verifikasi per akun** (`POST /api/terraform/cloud-accounts/{id}/test`, timeout 15 s): AWS `sts:GetCallerIdentity` bertanda tangan SigV4 (hanya stdlib), GCP JWT service account (RS256) → token → `GET projects/{id}`, Azure client-credentials, DigitalOcean `GET /v2/account`, Hetzner `GET /v1/servers?per_page=1`. Hasil disimpan (`verified_at`, `verify_ok`, `verify_msg`); pesan error tidak pernah memuat rahasia. Endpoint dapat diganti di tes lewat variabel paket.
5. **Tautan workspace sebagai daftar id JSON**: kolom `tf_workspaces.cloud_account_ids` (`TEXT NOT NULL DEFAULT '[]'`), bukan tabel relasi: jumlah akun per workspace kecil dan tidak ada kueri dari sisi akun selain hitungan (`workspace_count`). Menghapus akun mencabut id-nya dari semua workspace. Id yang tidak ada ditolak 400; `cloud_account_ids` yang dihilangkan pada PUT mempertahankan nilai tersimpan.
6. **API tidak pernah mengembalikan rahasia**: `GET /api/terraform/cloud-accounts` hanya memuat field bukan rahasia ditambah `secrets_set` (peta key → true). `PUT` dengan field rahasia kosong mempertahankan nilai tersimpan. Nilai field tidak pernah di-log.
7. **Snapshot**: akun dan `cloud_account_ids` ikut dalam payload snapshot terenkripsi; snapshot lama tanpa field ini tetap dapat dipulihkan.
8. **UI**: tab tingkat atas **Cloud Accounts** (daftar, dialog, uji koneksi), kartu **Cloud accounts** di tab Settings drawer workspace, dan baris di Overview. Modul `40-43-cloud-*.js` dalam bundel `/app/terraform.js`.

## Konsekuensi

- Satu tempat untuk kredensial cloud; workspace baru cukup mencentang akun. Kredensial yang salah ketahuan lewat **Test connection** sebelum Run.
- Kredensial tersimpan **polos di SQLite** (seperti token Cloudflare dan Git); perlindungan at-rest bergantung pada izin berkas DB dan enkripsi backup snapshot.
- Hanya kredensial statis yang didukung. Token berumur pendek yang dipertukarkan per Run, OIDC, dan workload identity tidak didukung; `AWS_SESSION_TOKEN` yang diisi manual kedaluwarsa dan harus diperbarui operator.
- Akun cloud yang dihubungkan memberi semua Run workspace (termasuk kode Terraform/`local-exec`) akses penuh ke kredensial itu; batas kepercayaan sama dengan Terraform Gate ([ADR 0056](0056-terraform-management-terraform-cloud-style.md)).
- Variabel `env` workspace atau Variable Set dengan nama sama menimpa nilai akun cloud, sehingga override per workspace tetap dimungkinkan.

## Addendum: Cloudflare, Kubernetes, Assume Role, dan Penyensoran Log

1. **Provider Cloudflare dan Kubernetes**. Cloudflare: `api_token` (rahasia) dan `account_id` opsional → `CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID`, diverifikasi lewat API verifikasi token Cloudflare. Kubernetes: kubeconfig (rahasia, multiline) dan `context` opsional → `KUBE_CTX`; verifikasi hanya mem-parse kubeconfig, tanpa jaringan.
2. **`EnvFiles` / `FileEnv` untuk kubeconfig**. Katalog provider mendapat pemetaan field → berkas: kubeconfig ditulis sebagai berkas sementara `0600` per Run, variabel `KUBE_CONFIG_PATH` dan `KUBECONFIG` menunjuk ke path-nya, dan berkas dihapus setelah Run selesai. Isi kubeconfig tetap disensor dari log.
3. **Aturan penyensoran log `PlainEnv`**. Sebelumnya semua nilai env akun cloud disensor. Sekarang default tetap **sensor semua**; hanya field non-rahasia yang ditandai eksplisit sebagai `PlainEnv` (region, access key id, tenant/subscription/client id, project id) yang tampil polos di log. Rahasia dan variabel `env` pengguna tetap disensor. Field baru harus ditandai `PlainEnv` secara sengaja; lupa menandai berarti aman (tersensor).
4. **Impor dari akun Pantau yang ada**. Akun cloud dapat dibuat dari akun Cloudflare atau cluster Kubernetes Pantau (`import_from: {type, id}` pada `POST`; `GET /api/terraform/cloud-accounts/importable` hanya mengembalikan nama). Kredensial **disalin saat impor**, bukan tautan langsung, sehingga perubahan atau penghapusan sumber tidak memengaruhi akun cloud.
5. **AWS assume role diselesaikan per Run**. Field opsional `role_arn`, `external_id` (rahasia), `session_name` (default `pantau`). Di awal tiap Run Pantau memanggil STS `AssumeRole` (1 jam) dengan kunci dasar dan menyuntikkan kredensial sementara; gagal berarti Run gagal dengan error jelas. Test connection juga meng-assume role. Ini mengisi celah "token berumur pendek per Run" pada Konsekuensi di atas untuk AWS; OIDC dan workload identity tetap tidak didukung.
6. **Kredensial tetap polos di SQLite**. Enkripsi at-rest akun cloud (termasuk kunci dasar, `external_id`, kubeconfig) **belum dikerjakan**; perlindungan tetap bergantung pada izin berkas DB dan enkripsi backup snapshot.
