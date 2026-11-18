# Pencarian Teks Lengkap Klien-Sisi pada Modal Dokumentasi

Penambahan fitur pencarian kata kunci (*full-text search*) murni di peramban atas dokumentasi yang telah di-parse dan di-cache di dalam modal Documentation (ADR 0017), tanpa endpoint backend baru maupun pustaka/vendor baru, lengkap dengan penandaan kecocokan, filter daftar isi, dan navigasi antar-kecocokan.

## Konteks dan Masalah

Modal Documentation Pantau ([ADR 0017](0017-embedded-in-app-documentation-modal.md)) telah menyematkan dokumentasi resmi (README, User Guide dwibahasa ID/EN, dan CHANGELOG) ke dalam binary serta menyajikannya melalui endpoint publik `GET /api/docs?name={readme|guide|changelog}&lang={en|id}`. Dokumen diunduh sekali, di-parse oleh micro-parser `parseMicroMarkdown(md)` menjadi `{html, toc}`, lalu di-cache di memori (`S.docsCache`) dan ditampilkan dengan daftar isi dinamis berbasis heading `h2`/`h3`.

Namun, modal tersebut belum menyediakan cara mencari kata kunci di dalam dokumen. Hal ini menimbulkan hambatan operasional:

1. **Scroll Manual lewat TOC**: Operator di lingkungan airgapped ([ADR 0002](0002-golang-single-binary-sqlite.md), [ADR 0013](0013-airgapped-self-contained-web-assets.md)) yang mencari informasi spesifik harus menggulir halaman atau menebak bagian dari daftar isi; TOC hanya membantu jika operator sudah mengetahui judul bagian yang dicari.
2. **Dokumen Panjang**: CHANGELOG dan User Guide adalah berkas satu bahasa utuh yang sangat panjang; menemukan rilis, perintah, atau topik tertentu memakan waktu dan berisiko terlewat.
3. **Kendala Dependensi & Tampilan**: Menambahkan mesin pencari atau pustaka highlight eksternal melanggar prinsip *zero-npm*/tanpa vendor baru ([ADR 0013](0013-airgapped-self-contained-web-assets.md)) dan konvensi desain token Nord ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)). Solusi harus memanfaatkan apa yang sudah ada di peramban dan di modal.

## Keputusan Arsitektur

1. **Pencarian Murni Client-Sisi atas Cache yang Sudah Ada**:
   - Pencarian dijalankan sepenuhnya di peramban terhadap hasil parse dokumentasi yang sudah tersedia di `S.docsCache[cacheKey]` dengan bentuk `{html, toc}` (kunci cache `${name}_${lang}`).
   - TIDAK menambahkan endpoint backend baru; TIDAK menambahkan pustaka, bundler, atau berkas vendor baru — selaras dengan keterbatasan airgapped pada [ADR 0013](0013-airgapped-self-contained-web-assets.md) dan sistem desain pada [ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md) (menggunakan token `var(--...)` dan ikon sprite `i-search` yang sudah ada).

2. **Antarmuka dan Perilaku Pencarian**:
   - Kolom input pencarian ditempatkan di sidebar modal Documentation, memakai pola input yang sudah ada (`oninput` dengan *debounce*).
   - *Debounce* 120 ms digunakan agar pencarian tidak dijalankan pada setiap ketukan tombol.
   - Semua kecocokan di badan dokumen ditandai dengan elemen `<mark>`; daftar isi (TOC) difilter hanya ke bagian yang mengandung kecocokan; ringkasan jumlah kecocokan ditampilkan kepada pengguna.
   - `Enter` / `Shift+Enter` berpindah ke kecocokan berikutnya / sebelumnya, dengan *scroll* ke posisi dan penandaan aktif; disediakan tombol *clear* untuk mengosongkan query.

3. **Retensi Query Lintas Dokumen/Bahasa**:
   - Query disimpan di state memori `S.docsSearchQuery` dan tetap bertahan ketika pengguna berganti dokumen atau bahasa: `applyDocsSearch()` dipanggil dari `renderDocData()` sehingga pencarian langsung diterapkan kembali ke konten dan TOC yang baru dirender.
   - State bersifat *in-memory* dan hilang ketika halaman dimuat ulang (*reload*).

4. **Sanitasi dan Keamanan**:
   - Query di-escape terlebih dahulu sebelum dipakai sebagai pola *regex*, sehingga karakter khusus regex tidak dapat mengubah perilaku pencarian maupun memicu kesalahan.
   - Penandaan hanya membungkus *text node* yang cocok — tidak pernah menyuntikkan HTML berasal dari dokumen ke dalam markup hasil highlight.
   - Mekanisme ini mengikuti sanitasi XSS yang sudah diterapkan pada `parseMicroMarkdown`/`sanitizeHtml` ([ADR 0017](0017-embedded-in-app-documentation-modal.md)); konten dokumen tetap diperlakukan sebagai teks, bukan markup aktif.

5. **Pencarian Berbasis Token dan Kamus Sinonim Dwibahasa (EN/ID)**:
   - Query dan teks dokumen dinormalisasi menjadi token (huruf kecil, tanpa diakritik, dengan pembuangan akhiran jamak sederhana) sebelum dicocokkan, sehingga "secrets" juga cocok dengan "secret" dan "rahasia".
   - Kamus sinonim `DOCS_SYNONYMS` memetakan token kanonik ke daftar sinonim Inggris + Indonesia — misalnya `secret`/`rahasia`/`vault`/`brankas`/`kunci`/`credential`, `sync`/`sinkron`/`sinkronisasi`, `docker`/`kontainer`, `kubernetes`/`k8s`/`klaster`. Kata kunci pemula dalam satu bahasa kini menemukan konten yang ditulis dengan kata lain.
   - Seluruh normalisasi, perluasan sinonim, dan penilaian kecocokan (`normalizeDocsToken`, `docsTokens`, `docsSynonyms`, `searchDocsScore`) berjalan di peramban dengan JavaScript murni — tanpa pustaka, *stemmer* eksternal, *vendor*, maupun *endpoint* baru; tetap selaras dengan batasan airgapped ([ADR 0013](0013-airgapped-self-contained-web-assets.md)).
   - Dengan didokumentasikannya fitur Secrets Vault ([ADR 0084](0084-universal-secrets-vault-and-env-sync.md)) di README dan User Guide dwibahasa, pencarian "secret", "secrets", "rahasia", "vault", maupun "brankas" kini menemukan bagian tersebut — langsung menutup celah yang melatarbelakangi ADR ini.

## Konsekuensi & Batasan

- **Tanpa beban jaringan tambahan**: seluruh pencarian berlangsung atas dokumen yang sudah ada di memori; tidak ada request baru ke `/api/docs`.
- **Beban CPU linear**: biaya pencarian dan penandaan bertumbuh linier terhadap panjang dokumen yang sudah dimuat; *debounce* 120 ms membatasi frekuensi eksekusi agar tetap responsif pada dokumen panjang.
- **Batasan cakupan**: pencarian bersifat per-dokumen aktif — tidak mencari lintas README, User Guide, dan CHANGELOG sekaligus; pengguna harus berpindah tab dokumen untuk mencari di dokumen lain (query tetap bertahan berkat poin 3).
- **Blok kode di-skip**: isi blok kode tidak disertakan dalam pencarian/highlight untuk menghindari kecocokan palsu pada contoh perintah atau keluaran; hanya teks naratif dan heading yang dicari.
- **State tidak persisten**: `S.docsSearchQuery` berada di memori dan hilang saat halaman dimuat ulang; tidak disimpan ke `localStorage`, URL, maupun backend.
- **Penemuan Konten yang Sebelumnya Tersembunyi**: berkat pencocokan token + sinonim dwibahasa, bagian Secrets Vault yang baru didokumentasikan dapat ditemukan lewat kata kunci pemula apa pun (`secret`, `rahasia`, `vault`, `brankas`, `kunci`, `credential`) tanpa mengharuskan pengguna tahu judul atau istilah resminya. Ini tetap merupakan pencarian klien-sisi murni dengan **nol dependensi baru** — biaya tambahan hanya berupa normalisasi string dan pencarian kamus kecil di peramban, tanpa beban jaringan maupun perubahan pada kontrak API `/api/docs`.
