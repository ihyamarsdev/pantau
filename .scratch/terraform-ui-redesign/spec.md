# Analisis & Rancangan Revamp UI/UX Terraform Management

Dokumen ini berisi hasil analisis menyeluruh terhadap implementasi fitur **Terraform Management** saat ini di Pantau (frontend & keterhubungannya dengan backend), identifikasi masalah arsitektur serta UX, dan rancangan lengkap *revamp* UI/UX yang selaras dengan [ADR 0039](../../docs/adr/0039-modern-visual-design-system-and-icon-sprite.md) (*Design System & Icon Sprite*) dan pola *redesign* Ansible UI ([`.scratch/ansible-ui-redesign/spec.md`](../ansible-ui-redesign/spec.md)).

---

## 1. Analisis Kondisi Saat Ini

### 1.1 Inventaris Aset Saat Ini

- **Markup & CSS inline**: `internal/web/static/index.html` masih menyimpan seluruh kerangka `#sectionTerraform` (baris 4219–4379), `#tfDrawerRoot` (baris 4805–4821), `#tfDlg1` & `#tfDlg2` (baris 4949–4967), serta CSS legacy `.tf-diff-*`, `.tf-log`, `.badge-*` (baris 3363–3388).
- **Monolith Core JS**: `internal/web/static/app/core/12-terraform.js` (1.618 baris, ~105 KB) berada di luar folder `app/terraform/`, mengekspor >110 fungsi global.
- **Global State Coupling**: `internal/web/static/app/core/00-state.js` menampung ~35 properti `S.tf*` dan kamus `S.TF_TXT` (yang diinisialisasi di `11-cloudflare.js` baris 1009!), sementara `internal/web/static/app/core/main.js` (baris 231–263) membuat 33 bridge `Object.defineProperty(window, "tf...", ...)`.
- **Sub-modul Tambalan (*Monkey-Patching*)**: `internal/web/static/app/terraform/` berisi 41 berkas `.js` dan `.css` hasil penambahan bertahap (ADR 0056, 0058, 0060, 0061, 0081). Beberapa modul membungkus ulang (*monkey-patch*) fungsi milik `12-terraform.js`:
  - `51-stateops.js` membungkus `tfFilterResources` dan memodifikasi DOM `<details>` setelah render via `tfStDecorate()`.
  - `52-links.js` membungkus `tfRenderVariables` untuk menyuntikkan kartu `#tfLinksCard`.
  - `63-registry-settings.js` membungkus `tfGateExtras` untuk menambahkan pengaturan mirror Registry.
  - `65-cli-update.js` membungkus `tfGateExtras` **sekali lagi**, sekaligus mengekspor `window.{tfCloudRefresh, tfAuditRefresh, tfRegistryRefresh, tfGateExtras}` di baris terakhir bundel.
- **Duplikasi CSS**: `10-drawer.css` menyalin kelas legacy Ansible (`.ans-section-panel`, `.ans-sched-tbl`) dengan komentar bahwa kelas tersebut sudah dihapus dari `index.html` saat redesign Ansible.
- **Kamus i18n Tersebar**: Teks terjemahan Terraform tersebar di 9 lokasi berbeda (`00-state.js` bagian `tf_*` dan `TF_TXT`, plus `20-config-i18n.js`, `30-settings-i18n.js`, `40-cloud-i18n.js`, `50-i18n.js`, `53-i18n.js`, `60-i18n.js`, dan `65-cli-update.js`).

### 1.2 Masalah Arsitektur & UX yang Ditemukan

| Area | Temuan Saat Ini | Dampak ke Pengguna / Pemeliharaan |
| --- | --- | --- |
| **Navigasi Atas (8 Tab)** | `#tfTabs` memiliki 8 tab sejajar: `Workspaces \| Variable Sets \| Cloud Accounts \| Policies \| Module Catalog \| Registry \| Audit \| Terraform Gate`. | Terlalu padat di desktop maupun mobile; pengguna kesulitan membedakan alur operasional harian vs konfigurasi sistem, serta bingung perbedaan *Module Catalog* vs *Registry*. |
| **Tidak Ada Global Runs / Approval Queue** | Tidak ada tampilan daftar Run lintas Workspace. Pengguna harus membuka setiap Workspace satu per satu untuk mengecek Run aktif atau Run berstatus `awaiting_approval`. | Operator lambat merespons *Approval Gate*, drift, atau Run gagal pada instalasi dengan banyak Workspace. |
| **Workspace List Datar & Minim Konteks** | Tidak ada KPI strip yang dapat diklik, tidak ada *status filter chips*, tidak ada *sorting* kolom pada List view, dan tidak ada *quick action* (`Plan`, `Check Drift`) langsung dari tabel/kartu. | Melanggar prinsip *exception-first hierarchy* (ADR 0039): Workspace bermasalah (`drifted`, `errored`, `awaiting_approval`, `locked`) tidak cukup menonjol; setiap aksi butuh minimal 3–4 klik. |
| **Modal New/Edit Workspace Berat** | `openTfWorkspaceModal` membuka modal `#tfDlg1` selebar 1180px dengan `<textarea>` mentah (`tfFilesEditorHTML`) untuk edit `.tf` dan tanpa *template preview* yang kaya. | Pengalaman edit kode inkonsisten dibanding CodeMirror di tab *Configuration*; form 3-kolom + editor dalam satu modal terasa sesak. |
| **Git Edit Backend Belum Terekspos di UI** | Backend `handlers_terraform_git_edit.go` sudah menyediakan `GET .../git/files`, `GET .../git/file`, dan `POST .../git/commit` (ADR 0058). Namun `23-config-git.js` masih menampilkan kartu *read-only*. | Fitur edit/commit/push Git yang sudah ada di Ansible (`20-editor.js`) dan sudah siap di backend Terraform tidak dapat digunakan di UI Terraform. |
| **Run Detail Monolitik & Console Log Polos** | `tfRenderRuns` membagi drawer menjadi daftar 290px + 1 kolom kanan yang menumpuk metadata, policy, cost, Approval Gate, Plan Diff, dan `<pre id="tfRunLog">` sekaligus. | Scroll sangat panjang; saat *log* panjang atau aktif, pengguna harus menggulir jauh untuk mencapai *Approval Gate* atau *Plan Diff*. Log belum mendukung warna ANSI, pencarian teks, *word wrap*, salin, atau unduh `.log`. |
| **Visual Plan Diff Terbatas** | `tfDiffHTML` merender kartu `<details>` manual tanpa filter aksi (`Create`/`Update`/`Replace`/`Delete`), tanpa pencarian *resource address*, dan nilai atribut >240 karakter terpotong permanen (`slice(0, 240)`). | Sulit meninjau Plan besar dengan puluhan perubahan resource secara akurat sebelum klik *Confirm & Apply*. |
| **Tab State Kurang Terstruktur** | Daftar resource (`<details>` + injeksi DOM `tfStDecorate`), State Versions, Diff selection (2 checkbox tanpa label *Base* vs *Target*), Import State, dan Lock Card bertumpuk dalam 1 halaman panjang. | Pemilihan versi untuk Diff rawan salah pilih arah (*from* vs *to*); tombol aksi `Move`/`Remove` di dalam `<summary>` rentan terklik tidak sengaja. |
| **Tab Variables & Linked Outputs** | Tidak ada pencarian/filter variabel; `Linked Output` ditempel terpisah di bawah tabel variabel tanpa penanda visual apabila tautan tertimpa (*overridden*) oleh variabel Workspace/Varset. | Sulit melakukan audit nilai efektif yang benar-benar diterima Terraform saat Run. |
| **Tab Terraform Gate Overload** | Tab Gate menjadi tempat menampung 4 fitur berbeda lewat *monkey-patch* `tfGateExtras`: Gate toggle + CLI binaries, Notifications, Registry Mirror URL, dan CLI Update Check. | Sulit ditemukan oleh pengguna (misal pengaturan Notifikasi dan Registry URL tersembunyi di bawah tab Gate) dan rapuh secara kode. |
| **Inline Handlers & Dual Modal Stack** | Ratusan `onclick="..."`, `onchange="..."` string di HTML + `#tfDlg1`/`#tfDlg2` dengan global function bindings. | Sulit diuji unit, rawan konflik ID DOM saat dialog bertumpuk, dan bertentangan dengan pola *event delegation* bersih di `app/ansible/`. |

---

## 2. Tujuan & Prinsip Desain Revamp

1. **Exception-First & Operational Clarity (ADR 0039)**: Status kritis (`awaiting_approval`, `errored`, `drifted`, `locked`) langsung terlihat di tingkat atas (KPI strip & global Approval Queue) dengan warna semantik murni untuk status.
2. **Satu Alur Kerja Terpadu (*Workspace-Centric + Run-Centric*)**: Operator bisa bekerja dari daftar **Workspaces** maupun langsung dari antrean **Runs** global tanpa kehilangan konteks.
3. **Ekspos Penuh Kemampuan Backend**: Membuka fitur backend yang sudah ada namun belum terpakai di UI (terutama **Git file browser, CodeMirror editor, & Commit/Push** di tab Configuration) serta menata ulang fitur yang sudah ada agar mudah ditemukan.
4. **Arsitektur Modular Bersih (Standar `app/ansible/`)**:
   - `#sectionTerraform` di `index.html` menjadi mount point bersih (`<div id="sectionTerraform" class="app-section" style="display:none;"></div>`).
   - Seluruh kode JS/CSS dipindahkan ke `internal/web/static/app/terraform/` di bawah satu namespace (`window.Tf`), **tanpa `12-terraform.js` di `app/core/`**, **tanpa `S.tf*` globals di `00-state.js`**, **tanpa *monkey-patching***, dan **tanpa inline `onclick=`**.
   - Menggunakan Design Token `:root` di `index.html` dan Icon Sprite `#i-*` yang sudah tersedia (zero literal hex color kecuali tema CodeMirror, zero assets eksternal/CDN per ADR 0013).
   - Mendukung penuh i18n `en` & `id` dengan kesetaraan kunci 100%, *keyboard navigation*, dan `prefers-reduced-motion`.

---

## 3. Arsitektur Informasi Baru (Information Architecture)

### 3.1 Struktur Section & Navigasi Atas

Navigasi atas dirampingkan dari **8 tab** menjadi **5 tab** yang terorganisasi berdasarkan tugas operator:

| ID Tab | Label (EN / ID) | Segmented Sub-view / Isi | Pengganti Tab Lama |
| --- | --- | --- | --- |
| `workspaces` | **Workspaces** / **Workspace** | KPI Exception Strip + Toolbar + Dense Table (default) / Card Grid toggle | `Workspaces` |
| `runs` | **Runs** / **Run** | **Global Runs Feed & Approval Queue** lintas Workspace (filter status, trigger, workspace, search) | *(Baru — sebelumnya tidak ada)* |
| `variables` | **Variables & Cloud** / **Variabel & Cloud** | Segmented: **Variable Sets** \| **Cloud Accounts** | `Variable Sets` + `Cloud Accounts` |
| `registry` | **Modules & Registry** / **Modul & Registry** | Segmented: **Private Catalog** \| **Public Registry** | `Module Catalog` + `Registry` |
| `governance` | **Governance & Settings** / **Tata Kelola & Pengaturan** | Segmented: **Policies** \| **Audit Trail** \| **Gate, CLI & Alerts** | `Policies` + `Audit` + `Terraform Gate` |

> **Catatan Header & Gate Status**:
> - Header `#sectionTerraform` menampilkan Judul, Deskripsi singkat, **Gate Status Pill** interaktif (klik langsung lompat ke `Governance & Settings -> Gate, CLI & Alerts`), dan tombol **Refresh**.
> - Saat Terraform Gate mati (`enabled: false`) atau biner tidak tersedia (`supported: false`), **slim banner** atau *actionable empty-state* yang terintegrasi rapi di bawah header/di dalam panel memandu admin untuk mengaktifkan Gate (atau melihat status biner) tanpa merusak tata letak. *(Jika `TestTerraformGateHasNoBannerAboveTabs` dipertahankan, status gate tetap di header pill dan panel aktif menampilkan empty-state card dengan tombol langsung "Open Gate Settings").*

---

## 4. Rancangan Detail per Layar & Komponen

### 4.1 Tab 1: `Workspaces`

#### A. Clickable KPI Strip (Exception-First)
Baris kartu ringkas di atas toolbar (klik untuk toggle filter status cepat):
1. **Total Workspaces** (netral)
2. **Awaiting Approval** (`--primary` / `--accent-text` — prioritas aksi manusia)
3. **Drifted** (`--warning` — deviasi infrastruktur)
4. **Errored** (`--danger` — run terakhir gagal / policy failed)
5. **Running** (`--info` — plan/apply sedang berjalan, dengan jumlah slot aktif `X / 3`)
6. **Locked** (ikon kunci — terkunci manual atau oleh run)

#### B. Toolbar
- **Search input** (`/` untuk fokus cepat): cari berdasarkan nama, deskripsi, tag, atau grup.
- **Status filter chips**: `All`, `Approval`, `Drifted`, `Errored`, `Running`, `Locked`, `Never run`.
- **Dropdown filter**: `Environment` (`production`, `staging`, `development`, custom), `Tag`, `Group`.
- **Group-by toggle** (`Group by workspace group`, tersimpan di `localStorage` `pantau_tf_groupby`).
- **View Mode Toggle**: **Dense Table** (default baru untuk kepadatan informasi lebih baik) dan **Card Grid** (dipertahankan untuk pengguna yang menyukai tampilan kartu).
- **Primary Action**: Split-button **New Workspace** (utama: buka drawer/wizard; dropdown: *From Template*).

#### C. Dense Table View (Kolom & Aksi)
1. **Status Dot**: Indikator warna semantik + animasi pulse saat `running`/`planning`/`applying`.
2. **Workspace**: Nama (`font-weight: 600`) + deskripsi singkat + badge ringkas (`git`/`inline`, `env`, ikon `lock` jika terkunci, badge `drifted` jika ada drift, badge `auto-apply` jika aktif).
3. **Group**: `group_name` (atau `—`).
4. **Binary & Resources**: `terraform 1.x` / `tofu 1.x` · `N resources` (font mono).
5. **Last Run**: Status badge + `#id` + waktu relatif (`ago`, tooltip timestamp penuh) + ringkasan delta `+C ~U -D -/+R` bila ada.
6. **Actions**:
   - Tombol cepat kontekstual:
     - Jika `awaiting_approval`: tombol **Review** (aksen primary, langsung buka drawer -> Run Viewer pada run yang menunggu approval).
     - Selain itu: tombol **Plan** (langsung buka Launch Run pre-selected `plan` atau konfirmasi cepat).
   - Overflow menu (`...`): `Check Drift`, `Configuration`, `State`, `Variables`, `Settings`, `Lock / Unlock`, `Delete`.
- **Keyboard & Interaksi**: Baris tabel `tabindex="0"`, `Enter` membuka Workspace Drawer, `p` memicu Plan, `d` memicu Check Drift.

#### D. New Workspace Flow (Drawer / 2-Step Wizard)
Menggantikan modal raksasa 1180px:
- Dibuka di Side Drawer (atau Action Dialog terstruktur 2 langkah) dengan **Template Gallery** yang lebih visual:
  - Pilihan **Blank Workspace** atau kartu **Template** (`Hello World`, `AWS S3`, `AWS EC2`, `GCP Storage`, `Azure RG`, `DO Droplet`, `Hetzner Server`, `Cloudflare DNS`, `K8s Namespace`) yang langsung menampilkan badge provider, daftar variabel yang diperlukan, dan indikator apakah Cloud Account terkait sudah tersedia.
  - Pengaturan Metadata (`Name`, `Group`, `Environment`, `Tags`, `Binary`, `Drift Interval`, `Plan Interval`, `Auto-apply` non-prod) dan Source (`Inline` dengan starter files atau `Git` dengan URL/Branch/Path/Auth + tombol **Test Connection** langsung saat pembuatan).

---

### 4.2 Tab 2: `Runs` (Global Runs & Approval Queue)

Tab baru yang memberikan visibilitas penuh lintas semua Workspace:
- **Banner Antrean Approval (Contextual)**: Jika ada >= 1 Run berstatus `awaiting_approval`, tampilkan strip ringkas di atas tabel (*"N run(s) awaiting approval"*) dengan tombol filter satu klik.
- **Toolbar**: Search (`#id`, message, commit SHA, workspace), Filter `Workspace`, Filter `Status` chips (`All`, `Awaiting Approval`, `Running`, `Failed`, `Applied`, `No Changes`), Filter `Type` (`plan`, `destroy`, `refresh`, `drift`), Filter `Trigger` (`manual`, `webhook`, `drift`, `schedule`).
- **Tabel Global Runs**:
  - Kolom: `Run #ID` | `Workspace` (+ env badge) | `Type & Trigger` | `Status` (+ live elapsed timer bila aktif) | `Changes` (`+C ~U -D -/+R`) | `Policy` (`pass`/`warn`/`fail`) | `Cost Delta` | `Created / Duration` | `Action` (`Inspect` / `Review`).
- **Klik Baris**: Membuka **Run Viewer** langsung di Side Drawer (`Tf.runs.openViewer(runId)`), lengkap dengan header Workspace + tombol *"Open Workspace"* untuk melompat ke drawer Workspace terkait.

---

### 4.3 Workspace Side Drawer (`#tfDrawer`)

Dipertahankan sebagai *docked right-side drawer* (`width: min(980px, 94vw)`, mode lebar `wide`: `min(1320px, 96vw)`, full-screen `< 900px`), dengan **Stack Navigation** (`push` / `back` ala `Ans.drawer`) untuk membuka *Run Viewer* atau *Registry Detail* tanpa kehilangan state tab Workspace.

#### Header Drawer (Sticky)
- Kiri: Ikon Terraform + Nama Workspace + Badges (`status`, `drifted`, `locked`, `environment`, `group_name`).
- Kanan:
  - **Split-Button Aksi Utama**:
    - Tombol utama: **Plan** (membuka dialog/popover Launch Run dengan opsi target, replace, speculative, message).
    - Dropdown: `Speculative Plan`, `Refresh-only`, `Check Drift`, `Destroy` (danger), `Lock / Unlock`.
  - Tombol `Refresh`, `Expand/Collapse Drawer` (`wide` toggle), dan `Close` (`Esc` dilindungi *unsaved-changes guard*).

#### Tab dalam Workspace Drawer (7 Tab)
`Overview` | `Configuration` | `Runs` | `State` | `Variables` | `Drift` | `Settings`

---

#### Tab Drawer 1: `Overview`
1. **Actionable Status Banner (Top Priority)**:
   - Jika `awaiting_approval`: Banner aksen dengan jumlah perubahan, status policy, cost delta, dan tombol **Review & Approve Run #N**.
   - Jika `locked`: Banner lock (`manual` atau `run:<id>`) dengan alasan dan tombol `Unlock` / `Force Unlock`.
   - Jika `drift_detected`: Banner peringatan drift dengan waktu cek terakhir dan tombol `View Drift` / `Re-plan`.
2. **Grid Kartu (2–3 Kolom adaptif via `@container`)**:
   - **Recent Runs & 10-Run Pip Timeline**: Baris 10 kotak status (*pips*) yang bisa diklik langsung ke Run Viewer + ringkasan Run terakhir (durasi, trigger, commit, `+C ~U -D`).
   - **Workspace Outputs**: Tabel `Name`, `Value` (sensitif disamarkan dengan tombol toggle *Reveal Sensitive* + *Copy* per baris), plus pencarian cepat jika output banyak.
   - **Configuration & Security Summary**: Environment, Binary & TF Version, Source (`Inline N files` atau `Git URL @ branch` + commit terakhir), Linked Cloud Accounts badges, Jadwal (`Drift interval`, `Plan interval`, `Auto-apply`), serta **IaC Security Scan** (`window.IacScan` chip + tombol `Security Scan`).

---

#### Tab Drawer 2: `Configuration` (Inline Editor + Git Repository Editor)
Mengubah arsitektur menjadi dua mode yang setara kapabilitasnya:

1. **Mode Inline (`source === 'inline'`)**:
   - Pertahankan *two-pane CodeMirror layout* (`220px` file tree + editor) yang sudah baik, dengan perbaikan:
     - **File Sidebar**: Daftar berkas dengan indikator *dirty dot*, tombol tambah berkas inline, hapus berkas, dan indikator ukuran total berkas (`X / 50 files`).
     - **Toolbar Editor Terorganisir**:
       - Kiri: Nama berkas aktif + ukuran (`KiB / 256 KiB`) + badge `Unsaved`.
       - Grup Tools: `Format` (`Shift+Ctrl/Cmd+F`), `Validate` (`terraform validate`), `Registry` (buka Registry browser untuk *Insert snippet* / *Add file*), `Check Versions`.
       - Grup Save: `Discard`, `Save` (`Ctrl/Cmd+S`), `Save & Plan` (primary).
     - **Panel Diagnostics & Registry Check**: Tetap collapsible di atas CodeMirror dengan tombol *jump-to-file:line* dan tombol *one-click Apply version constraint*.
     - **Integrasi Detect Variables**: Tombol pintasan *"Detect Variables"* (atau toast/link setelah menyimpan `variables.tf`) untuk langsung mengekstrak blok `variable` ke tab Variables.

2. **Mode Git (`source === 'git'`) — BARU (Mengekspos `handlers_terraform_git_edit.go`)**:
   - Menggantikan kartu *read-only* `23-config-git.js` dengan **Git Repository Browser & Editor**:
     - **Top Info Bar**: Repo URL, Branch badge, Working Dir (`git_path`), Commit SHA terbaru, tombol **Test VCS Connection**, **Sync / Reload Tree**, dan tombol **Edit Git Config** (lompat ke Settings).
     - **Two-Pane Browser + CodeMirror**:
       - Sidebar kiri: Pohon direktori & berkas dari `GET /api/terraform/workspaces/{id}/git/files`, menandai folder `git_path` aktif, filter pencarian berkas, serta indikator berkas yang dimodifikasi (*dirty dot*).
       - Panel kanan: CodeMirror editor untuk membaca dan menyunting berkas `.tf` / `.tfvars` / `.hcl` / `.md` dari `GET /api/terraform/workspaces/{id}/git/file?path=...`.
     - **Commit & Push Bar**: Saat ada perubahan lokal di editor Git, muncul footer/bar *Commit & Push* dengan input `Commit message` dan daftar berkas berubah, memanggil `POST /api/terraform/workspaces/{id}/git/commit` (dengan penanganan error jelas bila kredensial bersifat read-only).

---

#### Tab Drawer 3: `Runs` & **Run Viewer** (Drawer Stack)
Mengubah layout lama (daftar sempit + 1 kolom scroll raksasa) menjadi alur 2 tingkat yang lega:

1. **Workspace Runs List**:
   - Header bar: Filter status (`All`, `Awaiting Approval`, `Applied`, `Errored`, `Planned`), filter tipe, dan tombol **Start Run** (`Plan`, `Speculative`, `Refresh`, `Destroy` dengan input `Message`, `Targets`, `Replace`).
   - Tabel/List padat: `#ID`, `Status` + live elapsed, `Type` + `Trigger`, `Changes` (`+C ~U -D -/+R`), `Policy`, `Cost Delta`, `Commit`, `Started`, `Duration`.
   - Klik baris mendorong (**push**) tampilan **Run Viewer** ke dalam `#tfDrawer` (dengan tombol `← Back to Runs` di header drawer).

2. **Run Viewer (Sub-view di Drawer)**:
   - **Header Ringkas + Pipeline Stepper**:
     - Judul `Run #ID` + badge status + type/speculative + trigger + commit SHA + duration/elapsed + tombol `Cancel Run` (jika aktif).
     - **Visual Pipeline Stepper (6 Tahap)**:
       `Queued` → `Plan` → `Policy Check` → `Cost Estimate` → `Approval` → `Apply`
       Setiap tahap menampilkan status ikon (`done`, `active`, `skipped`, `warn`, `failed`, `pending`).
   - **Sticky Approval Gate Card** (hanya muncul saat `status === 'awaiting_approval'`):
     - Ditempatkan di atas tab Run Viewer agar **selalu terlihat tanpa scroll**:
       - Ringkasan dampak: `+X create · ~Y update · -/+Z replace · -W delete`, status Policy, dan Cost Delta.
       - Jika Policy `warning` (soft/advisory): checkbox wajib *"Override policy warnings"*.
       - Input komentar wajib (min 3 karakter) + tombol **Confirm & Apply** dan **Discard Run**.
   - **Sub-Tabs di Dalam Run Viewer**:
     - **Tab `Plan Diff`** (aktif default bila run sudah selesai plan / `awaiting_approval`):
       - Toolbar: Filter aksi (`All`, `+ Create`, `~ Update`, `-/+ Replace`, `- Delete`, `<= Read`), kotak cari *resource address*, serta tombol `Expand All` / `Collapse All`.
       - Kartu Diff: Border semantik per aksi, header menampilkan simbol aksi, *resource address* (mono), tipe/provider, dan `action_reason` (mis. `tainted`, `cannot be updated in-place`).
       - Atribut Diff: Tampilan terformat untuk `+`, `~ before → after`, `-`, dengan badge `(known after apply)` atau `(sensitive)`. Nilai yang melebihi 240 karakter dapat diklik *"Show full value"* dalam modal/expand inline.
     - **Tab `Policy & Cost`**:
       - **Policy Check Report**: Badge status keseluruhan (`Passed` / `Warning` / `Failed`) dan daftar kebijakan yang dievaluasi beserta detail pelanggaran (`address` + `message` + level `advisory`/`soft`/`hard`).
       - **Cost Estimate (Infracost)**: Tampilan Before → After bulanan, Delta (`+$45.20/mo (+12%)`), atau penjelasan singkat cara mengaktifkan jika `infracost` belum terpasang / `INFRACOST_API_KEY` belum disetel.
     - **Tab `Console Log`** (aktif default saat run sedang berjalan `queued`/`planning`/`applying`):
       - Mengadopsi penuh **Run Viewer Log** milik Ansible (`ansible/30-runs.js`):
         - Toolbar: Search-in-log + tombol `Prev`/`Next` (`i/N` match counter), toggle `Follow` (auto-scroll cerdas), toggle `Wrap`, tombol `Copy`, dan tombol `Download .log`.
         - Rendering log dengan **ANSI SGR parser** (HTML-escape terlebih dahulu, lalu bungkus span warna `.tf-fg-*`) dan highlight baris hasil pencarian.

---

#### Tab Drawer 4: `State`
Dipecah menjadi 3 sub-navigasi (*segmented view*) di bawah **Workspace Lock Card** agar fokus dan tidak menumpuk:

1. **Workspace Lock Banner** (selalu di atas tab State):
   - Status Lock jelas (`Unlocked`, `Manually locked: <reason>`, atau `Locked by Run #<id>`).
   - Tombol `Lock Workspace` (dengan input alasan), `Unlock` (jika manual), atau `Force Unlock` (jika terkunci oleh run/bermasalah, dengan Confirmation Dialog destruktif).
2. **Sub-tab `Resources` (Inventory & State Ops)**:
   - Toolbar: Filter search *resource address*, filter `Provider` / `Mode` (`managed` vs `data`), dan tombol **Import Resource** (`terraform import`).
   - Tabel / Accordion Resource: Menampilkan `Address` (mono), `Type`, `Provider`, `Mode`, dan aksi per baris (`Inspect Attributes`, `Move (mv)`, `Remove (rm)`) yang ditempatkan di kolom aksi terpisah—**bukan** disisipkan di dalam `<summary>` accordion sehingga tidak bentrok saat klik expand!
   - Inspector Atribut: Menampilkan JSON atribut terformat dengan nilai sensitif bertanda `(sensitive)` dan tombol salin atribut/JSON.
   - Semua *State Operations* (`import`, `mv`, `rm`) tetap melewati dialog konfirmasi yang menampilkan **perintah CLI persisnya** serta spinner progres dan penampil error inline (sesuai ADR 0061).
3. **Sub-tab `Versions & Diff`**:
   - Toolbar: Tombol **Import `.tfstate`**, dan bar pembanding versi: dropdown/seleksi eksplisit **Base (From)** vs **Target (To)** + tombol **Compare Versions** (jauh lebih intuitif daripada 2 checkbox tanpa arah).
   - Peringatan keamanan unduhan `.tfstate` mentah (mengandung rahasia).
   - Tabel Riwayat State Versions: `#Serial` (`current` badge pada versi terbaru), `Source` (`apply`/`refresh`/`rollback`/`import`/`state rm ...`), `Resources`, `Size`, `SHA-256` (truncated + copy), `Run #ID` (klik lompat ke Run Viewer), `Created`, serta tombol `Download` & `Rollback` (dengan Confirmation Dialog + komentar wajib).
   - Hasil State Diff: Ringkasan `+ Added`, `- Removed`, `~ Changed` (collapsible per resource dengan `before → after`).

---

#### Tab Drawer 5: `Variables`
Dipecah menjadi 2 sub-navigasi (*segmented view*) yang terintegrasi:

1. **Sub-tab `Variables` (Direct & Effective)**:
   - Switcher: `Workspace Only` vs `Effective Resolution` (menampilkan gabungan Global Varsets + Linked Varsets + Linked Outputs + Workspace Vars).
   - Toolbar: Search key/description, filter Category (`All`, `terraform`, `env`), tombol **Detect from Configuration** (untuk inline workspace), dan **Add Variable**.
   - Tabel Variabel: `Key` (mono), `Value` (badge `••••••` untuk write-only sensitive), `Category` (`terraform`/`env`), `Flags` (`HCL`, `Sensitive`), `Origin` (`workspace`, `varset:<name>`, `linked:<ws>`), dan status **Overridden** (jika sebuah nilai dari Varset atau Linked Output tertimpa oleh variabel Workspace dengan nama sama).
   - Dialog Edit Variabel: Memperjelas perilaku `Sensitive` (biarkan kosong saat edit untuk mempertahankan nilai lama) dan otomatis menonaktifkan `HCL` ketika kategori = `env`.
2. **Sub-tab `Linked Outputs` (Cross-Workspace Injection)**:
   - Visual mapping per baris: `Target Variable (terraform)` `←` `Source Workspace` `::` `Output Name` + status badge (`Available`, `Sensitive`, `Unavailable`, atau `Overridden by explicit var`).
   - Penjelasan singkat bahwa nilai diselesaikan saat Run dimulai (bukan `terraform_remote_state`) dan validasi anti-siklus.

---

#### Tab Drawer 6: `Drift`
- **Status Banner**: Indikator `Drift Detected` / `No Drift` + waktu pengecekan terakhir + jadwal interval (`drift_interval_minutes`, dengan link ke Settings untuk mengubah interval) + tombol **Check Drift Now**.
- **Drift Diff Viewer**: Menggunakan komponen **Plan Diff Viewer** yang sama dengan Run Viewer (filter aksi, search resource address, expand/collapse all, full-value inspect) untuk konsistensi penuh.

---

#### Tab Drawer 7: `Settings`
Tata letak 2 kolom responsif dengan sticky footer (`Unsaved changes` indicator, `Reset`, `Save`):
- **Kolom Kiri**:
  1. **General & Automation**: Name, Group (autocomplete datalist), Environment (autocomplete), Tags, Description, Binary (`terraform` / `tofu`), Drift Interval (menit), Scheduled Plan Interval (menit), dan toggle **Auto-apply** (otomatis nonaktif + pesan peringatan jelas bila Environment = `production`).
  2. **Source & VCS**:
     - Jika `inline`: Ringkasan jumlah berkas + tombol *"Open Configuration Editor"*.
     - Jika `git`: Git URL, Branch, Working Dir, Auth (`none`/`token`/`ssh` dengan SSH Key Library dropdown), dan tombol **Test VCS Connection**.
  3. **Cloud Accounts**: Daftar centang Cloud Accounts yang tertaut ke Workspace ini + tombol *"Manage Cloud Accounts"*.
- **Kolom Kanan**:
  1. **Webhook Trigger**: Webhook URL (read-only + Copy), tombol **Rotate Secret** (dengan tampilan *one-time secret* + copy), dan ringkasan cara pakai header GitHub (`X-Hub-Signature-256`) / GitLab (`X-Gitlab-Token`) beserta catatan keamanan bahwa webhook hanya memicu *Plan* dan tidak pernah *Auto-apply*.
  2. **Danger Zone**: Tombol **Delete Workspace** (disabled/peringatan jika ada Run aktif).

---

### 4.4 Tab 3, 4, dan 5 (Level Section Utama)

#### Tab 3: `Variables & Cloud`
Segmented control di bagian atas panel:
- **`Variable Sets`**:
  - Toolbar: Search, Filter (`All`, `Global`, `Workspace-scoped`), tombol **New Variable Set**.
  - Dense Table / Card Grid: Nama, badge `Global` atau jumlah Workspace tertaut, jumlah variabel, deskripsi, aksi (`Edit`, `Delete`).
  - Editor Variable Set dalam **Drawer / Modal terstruktur** (Metadata + Workspace Picker searchable + Tabel Variabel langsung di dalamnya).
- **`Cloud Accounts`**:
  - Mempertahankan desain kartu provider yang sudah bagus (`40-cloud.css` / `41-cloud-list.js`) + tambah search/filter provider (`AWS`, `GCP`, `Azure`, `DigitalOcean`, `Hetzner`, `Cloudflare`, `Kubernetes`), status verifikasi (`Verified`, `Failed`, `Unverified`), jumlah workspace pemakai, aksi `Test`, `Edit`, `Delete`.

#### Tab 4: `Modules & Registry`
Segmented control di bagian atas panel:
- **`Private Catalog`** (dari `tf_modules` DB internal):
  - Toolbar: Search modul, filter provider, tombol **New Module**.
  - Grid / List modul privat; klik membuka **Module Detail View** di Drawer (memilih versi semver, **Usage Snippet** + Copy, **README preview** dengan Markdown renderer aman `64-registry-md.js`, tabel **Inputs** / **Outputs**, daftar **Resources** & **Providers**, serta manajemen versi Inline/Git).
- **`Public Registry`** (proksi server-side dari `tf_registry_url`):
  - Mempertahankan browser 2-kolom (`tf-reg`) yang sudah solid (`60-registry.js`, `61-registry-detail.js`), dengan penyempurnaan responsivitas mobile (list → detail dengan tombol Back) dan tetap terhubung ke modal/drawer saat dipanggil dari tab *Configuration* untuk menyisipkan snippet (`Insert at cursor` / `Add as file` + opsi deklarasi variabel wajib).

#### Tab 5: `Governance & Settings`
Sub-navigasi terstruktur (bukan tempelan *monkey-patch*):
- **`Policies`**:
  - Toolbar: Search, Filter Enforcement (`All`, `hard`, `soft`, `advisory`), Filter Scope (`All`, `Global`, workspace tertentu), tombol **New Policy**.
  - Daftar/Tabel Policy dengan toggle cepat `Enabled/Disabled`, badge jenis aturan (`deny_destroy`, `max_deletes`, `max_changes`, `deny_resource_types`, `require_tags`, `deny_open_cidr`), parameter ringkas, dan aksi `Edit`/`Delete`.
- **`Audit Trail`**:
  - Toolbar: Filter Workspace, Filter Level (`All`, `INFO`, `WARN`, `ERROR`), kotak pencarian pesan audit, dan tombol `Refresh`.
  - Tabel ringkas timestamp, level badge, workspace, dan pesan audit (maks 200–5000 terbaru dari `GET /api/terraform/audit`).
- **`Gate, CLI & Alerts`**:
  - **Card 1: Terraform Gate & Security**: Switch/tombol Enable/Disable Terraform Gate dengan peringatan keamanan lokal tanpa sandbox.
  - **Card 2: CLI Binaries & Updates**: Status `terraform`, `tofu`, dan `infracost` (versi & path) + fitur **Check for Updates** & **Update Now** dengan live streaming install log (dari `65-cli-update.js`).
  - **Card 3: Notifications**: Pilihan event (`awaiting_approval`, `errored`, `applied`, `drift`) + tombol `Save` & `Send Test Notification` (dari `54-notify.js`).
  - **Card 4: Registry Mirror URL**: Konfigurasi `tf_registry_url` + tombol `Save` & `Test Connection` (dari `63-registry-settings.js`).

---

## 5. Rancangan Arsitektur Teknis Frontend (`static/app/terraform/`)

Mengadopsi penuh arsitektur **Ansible UI Redesign** agar konsisten di seluruh repositori Pantau:

### 5.1 Pembersihan File Core (`index.html`, `00-state.js`, `main.js`, `11-cloudflare.js`)
1. **`internal/web/static/index.html`**:
   - `#sectionTerraform` disederhanakan menjadi `<div id="sectionTerraform" class="app-section" style="display:none;"></div>`.
   - Hapus `#tfDrawerRoot`, `#tfDlg1`, `#tfDlg2` statis dari `index.html` (di-render dan dikelola oleh `00-core.js` di dalam `#sectionTerraform` / portal root agar lifecycle bersih).
   - Hapus blok CSS legacy `/* --- Terraform Management Styles --- */` (baris 3363–3388).
2. **Hapus `internal/web/static/app/core/12-terraform.js`**.
3. **Bersihkan `00-state.js`, `main.js`, `08-metrics-systemd-docker.js`, `11-cloudflare.js`**:
   - Hapus 35+ properti `tf*` dan `TF_TXT` dari `00-state.js`.
   - Hapus 33 `Object.defineProperty(window, "tf...", ...)` dari `main.js`.
   - Hapus loop `S.TF_TXT` dari `11-cloudflare.js`.
   - `08-metrics-systemd-docker.js` cukup memanggil `if (window.Tf && window.Tf.refresh) window.Tf.refresh();` (pola yang sama seperti `refreshAnsibleSection()`).

### 5.2 Struktur Modul `internal/web/static/app/terraform/`
Server tetap menyajikan bundel tunggal terurut leksikal: `/app/terraform.js` dan `/app/terraform.css`.

| Berkas | Tanggung Jawab |
| --- | --- |
| `00-core.js` / `00-core.css` | Namespace `window.Tf`, `Tf.api`, `Tf.state`, `Tf.on/off/emit`, `Tf.addI18n` / `Tf.t`, `Tf.esc`, `Tf.icon`, `Tf.fmt` (`dur`, `ago`, `dt`, `bytes`, `counts`), `Tf.ui` (`badge`, `dot`, `empty`, `skeleton`, `error`), `Tf.store` (`pantau_tf_*`), `Tf.drawer` (stackable `open`/`push`/`back`/`close` + `canClose` guard), `Tf.dlg` (modal promise tunggal), `Tf.registerTab`, `Tf.every`, section shell + Gate pill. |
| `10-workspaces.js` / `10-workspaces.css` | Tab `Workspaces` (KPI strip, toolbar, dense table & card grid, sorting, grouping, overflow menu), **New Workspace** drawer + Template Picker, serta shell **Workspace Drawer** (header, split-button, tab switcher). |
| `15-overview-drift.js` / `15-overview-drift.css` | Drawer tab `Overview` (action banners, run pips timeline, outputs + reveal/copy, metadata + IaC scan chip) dan drawer tab `Drift`. |
| `20-config.js` / `20-config.css` | Drawer tab `Configuration`: **Inline CodeMirror Editor** (`fmt`, `validate`, jump-to-line diagnostics, `Registry` insert, `Check versions`) dan **Git Repository Browser & Editor** (`git/files`, `git/file`, `git/commit` + push). |
| `30-runs.js` / `30-runs.css` | Tab global `Runs`, drawer tab `Runs` (riwayat workspace + Start Run dialog), dan **Run Viewer** (Pipeline Stepper, Sticky Approval Gate, filterable **Plan Diff Viewer**, Policy & Cost card, **ANSI Console Log Viewer** dengan search/follow/wrap/copy/download). |
| `40-state.js` / `40-state.css` | Drawer tab `State`: Workspace Lock card, sub-tab `Resources` + State Ops (`mv`, `rm`, `import`), sub-tab `Versions & Diff` (explicit From/To selector, StateDiff, Rollback, Import JSON, Download). |
| `50-variables.js` / `50-variables.css` | Drawer tab `Variables` (Direct/Effective + Overridden badge + Detect Variables + `Linked Outputs`) dan Global tab `Variables & Cloud` (`Variable Sets` + `Cloud Accounts`). |
| `60-registry.js` / `60-registry.css` | Global tab `Modules & Registry`: `Private Catalog` (modul + versi + README/inputs/outputs) dan `Public Registry` browser (pencarian, detail, snippet inserter ke editor). |
| `70-governance.js` / `70-governance.css` | Global tab `Governance & Settings` (`Policies`, `Audit Trail`, `Gate, CLI & Alerts`) dan Drawer tab `Settings` (General, Source/Git, Cloud links, Webhook, Danger Zone). |

### 5.3 Kontrak Internal (`Tf` Core Contract)
```text
Tf.t(key, fallback, vars)             i18n via global t(); {name} interpolation
Tf.addI18n({en, id})                  merge ke global I18N (semua key prefix tf_, wajib EN + ID)
Tf.esc(s) / Tf.icon(name, cls)        escapeHtml / SVG sprite <use href="#i-...">
Tf.api.get|post|put|del(path, body, opts) -> JSON; throws Tf.ApiError{status, message, data}
Tf.toast(msg, type) / Tf.confirm(opts)    membungkus showToast / showConfirm
Tf.on(evt, fn) / Tf.off / Tf.emit     events: status, workspaces, runs, varsets, cloud, policies, modules, lang
Tf.state = { status, workspaces, varsets, cloud, policies, modules, tab, ws }
Tf.load.status()|workspaces()|varsets()|cloud()|policies()|modules()
Tf.fmt.dur(ms) .ago(iso) .dt(iso) .bytes(n) .counts(plan) .currency(v, cur)
Tf.ui.badge(status) .dot(status) .empty(opts) .skeleton(rows) .error(msg, retryAttr)
Tf.drawer.open(view) / push(view) / back() / close() / current()
Tf.dlg.open({title, body, footer, width, onMount}) / close()
Tf.registerTab({id, icon, labelKey, order, render(panel), onShow(panel), onHide()})
Tf.every(fn, ms) -> stop()            polling hanya aktif saat section Terraform terlihat
Tf.refresh()                          muat ulang status + tab aktif
```

---

## 6. Penambahan Backend Minimal yang Diperlukan

Sebagian besar API sudah sangat lengkap. Hanya diperlukan penambahan ringan agar tab **Runs** global dan **Workspaces** dense table bebas dari *N+1 requests*:

1. **`GET /api/terraform/runs`** *(Baru di `internal/web/handlers_terraform.go` & `internal/store/terraform*.go`)*:
   - Query opsional: `workspace_id`, `status` (termasuk comma-separated mis. `queued,planning,applying,awaiting_approval`), `type`, `trigger`, `q`, `limit` (default 100, max 500).
   - Mengembalikan `runs`: array `RunSummary` + `workspace_name`, `workspace_environment`, `workspace_group_name`.
   - Berguna untuk: Global Runs Feed, badge jumlah *Awaiting Approval* di tab `Runs`, dan KPI Strip tanpa melakukan *fetch* per workspace.
2. **Enrich `GET /api/terraform/workspaces` (`last_run`)** *(Opsional, sangat dianjurkan)*:
   - Field `last_run` pada respons workspace saat ini `{id, type, status, finished_at, has_changes}`; diperkaya dengan `counts` (`{create, update, delete, replace}`), `duration_ms`, `trigger_kind`, dan `policy_status` agar tabel daftar Workspaces bisa langsung menampilkan delta resource dan durasi terakhir.

---

## 7. Rencana Migrasi Bertahap & Strategi Verifikasi

### 7.1 Fase Implementasi
1. **Fase 1 — Backend Endpoint & Core Shell**:
   - Tambahkan `GET /api/terraform/runs` + unit test store/handler (`go test ./internal/store ./internal/web`).
   - Buat `00-core.js` dan `00-core.css` di `app/terraform/`, ubah `#sectionTerraform` di `index.html` menjadi mount point, dan siapkan kompatibilitas sementara untuk transisi modul.
2. **Fase 2 — Workspaces, New Workspace & Workspace Drawer Shell**:
   - Implementasikan `10-workspaces.js/.css` dan `15-overview-drift.js/.css`.
   - Hadirkan KPI strip, dense table + card toggle, New Workspace template picker, dan Overview + Drift drawer tabs.
3. **Fase 3 — Global Runs & Run Viewer Baru**:
   - Implementasikan `30-runs.js/.css`: tab Global Runs + Approval Queue, Workspace Runs list, Pipeline Stepper, Sticky Approval Gate, Filterable Plan Diff Viewer, Policy/Cost card, dan ANSI Console Log Viewer.
4. **Fase 4 — Configuration (Inline + Git Edit), State, & Variables**:
   - Implementasikan `20-config.js/.css` (satukan CodeMirror inline + **Git Browser/Editor/Commit & Push** mengekspos `handlers_terraform_git_edit.go`).
   - Implementasikan `40-state.js/.css` (Lock, Resources + StateOps, Explicit From/To Versions Diff).
   - Implementasikan `50-variables.js/.css` (Variables + Detect + Linked Outputs + Global Variable Sets & Cloud Accounts).
5. **Fase 5 — Modules/Registry, Governance & Cleanup Legacy**:
   - Implementasikan `60-registry.js/.css` (`Private Catalog` + `Public Registry`) dan `70-governance.js/.css` (`Policies`, `Audit Trail`, `Gate, CLI & Alerts`, Workspace `Settings`).
   - Hapus `internal/web/static/app/core/12-terraform.js`, properti `S.tf*` / `S.TF_TXT` di `00-state.js`, bridge `main.js`, hack `11-cloudflare.js`, dan CSS legacy di `index.html`.

### 7.2 Verifikasi & Pengujian
- **Unit & Contract Tests (`internal/web/terraform_ui_test.go`, `terraform_bundle_test.go`)**:
  - Perbarui assertion dari kontrak legacy (`#tfTabs` di `index.html`, `#tfDlg1`/`#tfDlg2`, `S.tfTabRenderers`) ke kontrak baru ala `ansible_ui_test.go`:
    - Mount point `#sectionTerraform` di `index.html` bersih tanpa modal/tab inline legacy.
    - `/app/terraform.js` dan `/app/terraform.css` tersaji dengan gzip + ETag/304.
    - `node --check` pada bundel `/app/terraform.js`.
    - **Parity i18n EN & ID** untuk setiap kunci `tf_*` pada seluruh modul.
    - **Zero inline `onclick=`/`onchange=`/`oninput=`** pada bundel `/app/terraform.js`.
    - Pengujian helper murni (ANSI parser, plan diff filter, version selector) via `node` jika `node` tersedia.
- **Targeted Go & E2E Verification**:
  - `go test ./internal/store ./internal/web`
  - Perbarui selektor pada `e2e/tests/terraform*.e2e.ts` bila ID/tab berubah, lalu jalankan `cd e2e && E2E_TELEMETRY_DISABLED=1 npx e2e run tests/terraform*.e2e.ts` (setelah seluruh implementasi selesai, sesuai *Deferred & Targeted Testing Policy*).
