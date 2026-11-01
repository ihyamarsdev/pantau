# 0039. Sistem Desain Visual Modern, Token Warna Semantik, dan Sprite Ikon Tersemat

Seluruh antarmuka web Pantau didesain ulang ke bahasa visual modern yang bersih (gaya Linear/Vercel): palet netral gelap berlapis, satu warna aksen, warna semantik khusus untuk status, tipografi Inter + JetBrains Mono, dan ikon garis Lucide. Perubahan ini murni visual/struktural; perilaku, ID elemen, nama fungsi, dan kunci i18n tidak berubah.

## Konteks dan Masalah

1. **Tidak ada hierarki visual**: setiap kartu Host menampilkan 8 baris label:value yang sama, sehingga Host bermasalah (Drift, disk 92%, Lifecycle Score rendah) nyaris tidak lebih menonjol daripada Host sehat. Padahal inti produk adalah deteksi Drift.
2. **Palet generik dan warna tidak bermakna**: biru Tailwind dipakai untuk bar, tombol, ikon, dan tautan sekaligus, sehingga biru tidak lagi menandakan apa pun. Ratusan warna heksadesimal tertanam langsung di CSS, template JS, dan atribut `style` inline.
3. **Ikon tidak konsisten**: emoji (🌐 📁 ⚠️ ✅ ⛶ …) bercampur dengan SVG inline berukuran ad-hoc; render emoji berbeda antar OS sehingga tampilan tidak dapat diprediksi.
4. **Tipografi bergantung font sistem**: angka bergeser saat nilai berubah dan tampilan berbeda antar platform.

## Keputusan Arsitektur

1. **Token desain di `:root` sebagai kontrak tunggal**:
   - Permukaan berlapis `--bg` < `--card-bg` < `--surface-2` < `--surface-3`, garis tepi tipis `--card-border` / `--border-strong`, teks `--text` / `--text-muted` / `--text-faint`.
   - Satu aksen (`--primary`, `--accent-text`, `--accent-soft`, `--accent-ring`) hanya untuk aksi utama, tab aktif, fokus, dan tautan.
   - Warna semantik `--success` / `--warning` / `--danger` / `--info` (+ varian `-soft` dan `-border`) **hanya untuk status**, tidak pernah dekorasi.
   - Skala bentuk (`--radius-*`), elevasi (`--shadow-*`), tipografi (`--font-sans`, `--font-mono`, `--fs-*`), dan gerak (`--ease`, `--dur`).
   - Kode baru **wajib hanya memakai token** (tanpa heksadesimal/`rgba` literal), sehingga tema terang kelak cukup mendefinisikan ulang token. Pengecualian: skin pustaka pihak ketiga yang membutuhkan literal (objek tema xterm.js, CodeMirror).
2. **Hierarki berbasis pengecualian pada dashboard**: Host sehat tampil ringkas dan tenang; Host bermasalah memperoleh aksen tepi kiri berwarna semantik dan area penjelasan masalah teratas. Strip KPI di atas daftar (Total, Healthy, Drift, Offline, Stale, rata-rata RAM) dapat diklik untuk memfilter, dan Grid View serta List View keduanya didesain ulang dan dapat dialihkan lewat View Mode.
3. **Font tersemat (airgapped)**: Inter dan JetBrains Mono (variabel, subset latin, lisensi OFL) disimpan di `internal/web/static/vendor/fonts/` dan disajikan oleh endpoint `/vendor/*` dengan cache permanen, selaras dengan [ADR 0013](0013-airgapped-self-contained-web-assets.md). `font-src 'self' data:` pada CSP sudah mencakupnya; tidak ada permintaan ke CDN.
4. **Sprite ikon inline (Lucide, ISC)**: seluruh ikon antarmuka berasal dari satu `<svg id="iconSprite">` di awal `<body>` (`<svg class="icon"><use href="#i-NAME"/></svg>` atau helper JS `svgIcon('NAME')`). Emoji dilarang pada chrome antarmuka; nilai kamus i18n tidak lagi memuat awalan emoji karena `setLanguage` mengisi `innerHTML` dari kamus dan akan menggandakan ikon.
5. **Komponen dasar dibekukan, area dikerjakan terpisah**: tombol, input, modal, tab, tabel, badge, dan toast didefinisikan sekali di blok CSS awal; setiap area (dashboard, Host Detail, terminal, file manager, pengaturan/dialog) memakai komponen itu dan menaruh gaya khususnya di blok tersendiri.
6. **Anggaran ukuran HTML dinaikkan**: ukuran gzip `index.html` pada `TestIndexGzipAndETagRevalidation` dinaikkan dari 150 KB ke 200 KB karena sprite ikon dan sistem gaya baru; kompresi in-memory dan ETag ([ADR 0018](0018-in-memory-gzip-compression-and-etag-caching.md)) tetap berlaku.

## Konsekuensi

- Antarmuka konsisten antar-OS (tanpa emoji, font tersemat) dan sepenuhnya bekerja pada lingkungan terisolasi.
- Penambahan tema terang menjadi pekerjaan definisi token, bukan penyisiran ulang ribuan gaya inline; sisa literal warna pada pustaka pihak ketiga dan atribut inline dinamis masih ada dan perlu dimigrasikan bertahap.
- Biner membesar sekitar 90 KB (dua font variabel + ikon) dan HTML gzip sekitar 10–20 KB.
