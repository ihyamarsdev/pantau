# 0089. Pusat Keamanan Terpadu (Unified Security Center UI/UX Revamp)

Menyatukan seluruh permukaan keamanan Pantau — yang sebelumnya tersebar di tab Security Host Detail, kartu CVE Exposure Radar, inventaris kunci fleet, modul firewall/akun/IaC, dan tidak adanya tampilan keamanan global — menjadi satu komponen **Security Center** tiga zona dengan sinyal tinggi (high-signal), dipakai ulang baik untuk tampilan Security global (cakupan Fleet) maupun tab Security pada Host Workspace (cakupan terkunci ke `host_id`). Backend menyediakan satu endpoint agregat `GET /api/security/overview` yang menghitung skor postur, penghitung pilar, dan antrean aksi prioritas dalam satu lintasan SQLite tanpa SSH maupun panggilan jaringan.

## Konteks dan Masalah

1. **Tidak ada tampilan keamanan global.** Sebelum ADR ini, tidak ada bagian `sectionSecurity` di `index.html`; satu-satunya permukaan keamanan berada di dalam tab Security Host Detail (`#tabContent`) dan beberapa modal terpisah (`AC.openFleet()`, kartu Exploit Intelligence, Firewall, Accounts, OSV, Exposure Radar, IaC). Administrator tidak punya satu tempat untuk melihat postur seluruh armada.
2. **Sinyal tenggelam dalam detail.** Tab Security menumpuk banyak kartu independen secara vertikal tanpa hierarki: temuan kritis (KEV/EPSS, drift FIM, rahasia bocor) tampil setara dengan catatan cakupan dan konfigurasi. Prinsip hierarki berbasis pengecualian ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)) belum diterapkan pada keamanan.
3. **Biaya baca tinggi.** Menampilkan ringkasan keamanan mengharuskan beberapa endpoint dan potensi koneksi SSH; tidak ada satu agregasi ringan yang bisa dipakai dashboard maupun komponen baru.
4. **Modal bertingkat pada evidence.** Detail temuan sebelumnya dibuka lewat modal di dalam modal workspace, melanggar preferensi satu lapisan (tanpa nested modal) dan memperumit navigasi keyboard.
5. **Konsistensi visual & airgapped.** Semua perubahan wajib memakai token desain dan sprite ikon ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)), tanpa pustaka eksternal ([ADR 0013](0013-airgapped-self-contained-web-assets.md)), serta i18n EN/ID penuh ([ADR 0009](0009-client-side-i18n-and-domain-glossary-preservation.md)).

## Keputusan Arsitektur

### 1. Endpoint agregat `GET /api/security/overview` (Go, store)

- Rute baru di `internal/web/server.go`, ditangani `handleSecurityOverview` di `internal/web/handlers_security.go`. Menerima `?host_id=` dan/atau `?group_id=` (nama Host Group); keduanya dapat dikombinasikan. `host_id` tidak valid → HTTP 400.
- Logika di `internal/store/security_overview.go`, fungsi `GetSecurityOverview(hostID int64, groupName string)`. Satu lintasan SQLite terbatas atas `security_scans`, `exposure_scans`, `actual_items` (drift `file_integrity`), `docker_security_scans`, `account_audits`, dan `iac_scans`. Tidak ada SSH, tidak ada koneksi keluar; IaC/secret (tersimpan di server Pantau) hanya dihitung pada cakupan Fleet.
- Respons:
  - `score` (0–100) + `grade` (A–F) dari `ScoreSecurityPosture`, fungsi murni yang dapat diuji: bobot KEV 8, rahasia 6, FIM/kontainer 3, firewall 2, pembaruan 1 per item (dibatasi agar satu kategori tidak mendominasi), dipotong ke [0,100].
  - `breakdown`: `kev_critical`, `fim_drift`, `leaked_secrets`, `pending_sec_updates`, `container_issues`, `firewall_warnings`.
  - `pillars`: jumlah badge per 7 pilar (`overview`, `vulnerabilities`, `network`, `containers`, `identity`, `code`, `exposure`). Pilar `exposure` dihitung dari `exposure_scans` pada cakupan Fleet dan per-host; pada cakupan host IaC tetap dikecualikan.
  - `top_actions`: maksimal 10 item, diurutkan menurun menurut prioritas (KEV/EPSS + paparan publik + drift FIM + keparahan rahasia), tiap item memuat `pillar`, `severity`, `evidence` (JSON mentah temuan), `host_id`/`host_name`, dan saran `remediation`.
  - `host_count`, `generated_at`.
- Telemetri tetap merupakan data turunan; tidak ada tabel atau kolom baru, tidak ada perubahan skema, tidak ikut System Snapshot (selaras [ADR 0036](0036-agentless-security-scan.md), [ADR 0070](0070-iac-and-secret-scanning.md), [ADR 0078](0078-package-update-check-per-host.md)).

### 2. Satu komponen, dua cakupan (vanilla HTML/CSS/JS)

- Bundel baru `static/app/security-center/` (`00-security-center.js`, `00-security-center.css`), namespace `window.SC`, disajikan lewat `/app/security-center.js` dan `/app/security-center.css` (`serveAppBundle`).
- `SC.mountGlobal(#securityCenterRoot)` dipanggil saat `switchSideNav('security')`; `sectionSecurity` dan `navItemSecurity` baru ditambahkan ke `index.html`. `SC.mountHost(#tabContent, hostID, ctx)` dipanggil dari `renderSecurityData` pada tab Security Host Detail; cakupan terkunci ke host tersebut (pemilih scope disembunyikan, bagian global-only tidak dimuat).
- Satu komponen yang sama dipakai dua-duanya — tidak ada duplikasi markup atau logika render.

**Tiga zona:**

1. **Zona 1 — Sticky Posture Strip.** Cincin skor SVG ringkas (nilai + grade), 4 KPI pill yang dapat diklik (`KEV / Critical`, `FIM Drift`, `Secrets & IaC`, `Pending Updates`) yang berfungsi sebagai filter, pemilih cakupan (`All Fleet` / `Host Group` / `Host`; hanya pada mode global), dan satu split action button `Run Audit ▾` dengan opsi Quick Scan, OSV/KEV, Container Audit, IaC Scan. Aksi mutatif memakai konfirmasi eksplisit.
2. **Zona 2 — Sub-navigasi pilar vertikal.** Tujuh pilar: Overview & Triage, Vulnerabilities & Updates, Network & Perimeter, Containers & Images, Identity & Integrity, Code/IaC & Secrets, dan **Exposure Radar** (pilar tersendiri, dipisahkan dari Overview & Triage). Badge jumlah dari `pillars`. Menjadi bilah horizontal yang dapat digulir pada layar sempit (< 900px).
3. **Zona 3 — Main pane lazy + drawer.** Hanya pilar aktif yang di-fetch/di-render, memakai `DocumentFragment` dan event delegation (satu handler `click`/`change` pada root) agar jumlah node DOM tetap rendah. **Setiap kartu/bagian pada semua pilar dapat di-*collapse* secara independen** lewat pembungkus `.sc-section` dengan state di `S.collapsed` yang bertahan antar-render pilar. `contain: layout style` pada pane dan area evidence. Klik temuan membuka **Slide-Over Drawer** di sisi kanan (bukan modal bertingkat) dengan evidence teknis (detail CVE/EPSS, diff FIM, cuplikan kode IaC, metadata kunci) dan tombol remediasi kontekstual: Upgrade Package, Firewall Action, Accept FIM Baseline, Open Source File, Ask AI Agent to Remediate. Drawer mendukung `Esc` dan navigasi `↑`/`↓`. Semua aksi mutatif meminta konfirmasi.

### 3. Styling, aksesibilitas, i18n

- Hanya token CSS dari `:root` ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)); tanpa heksadesigma/RGBA literal. Ikon dari sprite yang sama; lima simbol Lucide baru (`i-layout-dashboard`, `i-file-warning`, `i-panel-left-close`, `i-file-check`, `i-sparkles`) ditambahkan ke sprite.
- Severity memakai **garis aksen kiri** (`::before` 3px) pada baris temuan, bukan latar merah penuh — mengikuti hierarki pengecualian.
- Seluruh string baru memiliki kunci EN dan ID yang di-`Object.assign` ke `I18N` di dalam IIFE modul, mengikuti pola `dockersec`/`vulnintel`/`accounts`. Atribut `data-i18n` pada bagian statis diperbarui oleh `setLanguage`.
- Komponen lama (kartu VI, OV, EC, Fw, AC, findings/hardening) tetap dipasang pada pilar terkait: kartu Exposure Radar (`EC`) kini berada di pilar **Exposure Radar**, bukan lagi di Overview & Triage; Exploit Intelligence (`VI`) di Vulnerabilities, Accounts (`AC`) di Identity, Firewall (`Fw`) di Network. Renderernya dipanggil ulang setelah lazy-mount, tanpa regresi fungsional.
- Entri navigasi pilar memakai `font-size: var(--fs-xs)`, label `flex: 1` dengan `line-height: 1.25`, dan badge count kecil (`line-height: 16px`, `font-size: 10px`, `align-self: center`) agar tidak mendominasi; ampersand pada label `Vulnerabilities & Updates` memakai *non-breaking space* (`&\u00a0`) supaya tidak menggantung di akhir baris.
- Header Security Host Detail dan kartu ringkasan/status kini digabung menjadi **satu panel collapsible** (`legacy-security`), menghilangkan border ganda dan jarak antar-panel.
- Pilar **Vulnerabilities & Updates** bukan lagi placeholder: tabel **Security Findings** dipindah ke sini dari panel Overview dengan filter teks, urutan severity/nama, toggle **KEV Only** dan **Fix Available** yang benar-benar aktif, kartu **Known vulnerabilities (OSV/Arch)**, serta kartu **Package Update Check** dari `/api/hosts/{id}/packages/updates` (tombol *Check for updates* dengan konfirmasi). Setiap baris dibuka di Slide-Over Drawer dengan bukti CVE/advisori dan aksi Upgrade/Ask AI. Cakupan Fleet menampilkan agregat aksi kerentanan dari `top_actions`.

## Konsekuensi

- Administrator memperoleh satu pandangan postur armada + antrean aksi yang dapat ditindaklanjuti, tanpa koneksi SSH tambahan untuk ringkasan.
- Skor postur adalah heuristik yang sengaja disederhanakan dan transparan (bobot di `ScoreSecurityPosture`); ia bukan pengganti penilaian risiko manual dan tidak mengubah aturan `security_updates` atau chip keamanan dashboard.
- Cakupan IaC/secret hanya tersedia di level Fleet karena sumbernya berada di server Pantau, bukan pada Host tertentu.
- Drawer remediasi untuk Upgrade/Firewall/Accept FIM saat ini meminta konfirmasi dan meneruskan ke alur yang sudah ada; tombol **Ask AI Agent** membuka sesi agen host bila tersedia ([ADR 0073](0073-in-process-ai-agent.md)).
- Modul Security Center tidak menambah dependensi Go maupun aset CDN; biner dan CSP tetap airgapped ([ADR 0013](0013-airgapped-self-contained-web-assets.md)).

## Verifikasi

- Unit test Go: `ScoreSecurityPosture` (bobot, grade, batas 0–100) dan `GetSecurityOverview` (agregasi lintas tabel, filter host/group, pengecualian IaC pada cakupan host) di `internal/store/security_overview_test.go`; test handler di `internal/web/handlers_security_overview_test.go`.
- E2E: `e2e/tests/security-center.e2e.ts` memstub `/api/security/overview` dan memverifikasi strip postur, filter KPI, perpindahan pilar lazy, drawer dengan navigasi keyboard, dan cakupan host. Regresi tab Docker tetap di `e2e/tests/docker-security.e2e.ts`.

## Addendum: Penghapusan tampilan global (2026-11-19)

Permintaan lanjutan: **tampilan Security global dihapus**. `navItemSecurity`, `sectionSecurity`, `securityCenterRoot`, cabang `switchSideNav('security')`, dan `SC.mountGlobal()` beserta seluruh jalur global-only (`loadHosts`, `S.hostMode`/`scopeType`/`scopeValue`/`hosts`, pemilih scope, cabang ringkasan fleet) dihapus. Komponen Security Center kini **hanya** dipasang di tab Security Host Detail lewat `SC.mountHost(#tabContent, hostID, ctx)`; perilaku host-mode tidak berubah. Endpoint `GET /api/security/overview` tetap dipertahankan (dipakai komponen host-scope dan agent). Kunci i18n global-only (`nav_security`, `sc_title`, `sc_subtitle`, `sc_scope_*`, `sc_recent_scans`) dihapus dari kamus inti; E2E `security-center.e2e.ts` disesuaikan ke cakupan host saja.
