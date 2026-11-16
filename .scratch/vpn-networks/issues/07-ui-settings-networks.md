# 07 — UI Settings → Networks

Status: ready-for-agent

Blocked by: 06

## Lingkup

`internal/web/static/index.html` (settings ada di monolith; CSS/JS baru boleh ikut pola `app/` bila perlu):

1. Tab baru `#tabSettingsNetworks` di `#settingsTabs` + panel `#settingsPanelNetworks` + cabang `switchSettingsTab('networks')`.
2. Kartu per Network: nama, badge driver, badge **Tunnel State** (`up` hijau / `starting` kuning / `down` abu / `error` merah), interface, jumlah Host, tombol Up/Down/Edit/Hapus, ringkas `last_error` + tombol "Log".
3. Dialog tambah/edit: select driver → bidang `config` sesuai driver (rahasia `type=password`, tampil `•••• sudah disimpan` bila `*_set`), select `iface` dari `GET /api/vpn/interfaces` **atau** dropdown yang diisi `net.Interfaces()` via endpoint `GET /api/vpn/status` (pilih salah satu, jangan keduanya), checkbox `enabled` + `auto_up`.
4. Peringatan gate di atas panel bila `vpn_enabled` mati, dengan tombol aktifkan + teks risiko privilese (pola Terraform Gate).
5. i18n EN/ID untuk semua string baru (`vpn_*`).

## Penerimaan

- Di browser: membuat Network `custom` dengan `iface` valid tampil di daftar; badge state berubah setelah `Up`/`Down`; rahasia tidak pernah tampil di DOM setelah simpan (input kosong + hint "sudah disimpan").
- Tidak ada `console.error` saat tab dibuka/ditutup berulang.

## Catatan

Ikuti `.card`/`.badge`/`.modal` yang sudah ada; jangan menulis CSS komponen baru.
