# 11 — NetBird/Tailscale multi-akun paralel (`mode: instance`)

Status: ready-for-agent

Blocked by: 04

## Masalah

Satu daemon NetBird hanya punya **satu profil aktif** ("Only one profile is active at a time", `docs.netbird.io/client/profiles`), dan multi-akun paralel masih permintaan terbuka (`netbirdio/netbird#446`, open sejak 2022). Dua server NetBird (mis. dua self-hosted, atau cloud + self-hosted) karena itu tidak bisa dilayani `mode: shared`: `netbird up` kedua memindahkan akun dan memutus yang pertama. Tailscale sama: satu tailnet per `tailscaled`.

## Lingkup

`internal/vpnnet/driver_netbird.go`, `driver_tailscale.go`:

1. Bidang `config`: `mode` (`shared` default | `instance`), `interface_name`, `wireguard_port`, `daemon_socket` (shared) / state+cari socket di `<root>` (instance).
2. `mode: instance` (NetBird): jalankan daemon sendiri sebagai anak proses —
   `netbird service run --config <root>/config.json --daemon-addr unix://<root>/nb.sock --log-file <root>/netbird.log`,
   lalu `netbird up --daemon-addr unix://<root>/nb.sock --setup-key … --management-url … --interface-name <iface> --wireguard-port <port> --disable-dns`.
   `Down`: `netbird down --daemon-addr …` lalu hentikan proses (SIGTERM, tunggu 10 detik). `Status`: `netbird status --json --daemon-addr …` (kunci `management.connected`, `signal.connected`, `peers`).
   Verifikasi flag tepat ini ada di v0.64.1 (`netbird service run --help`: `--config`, `--daemon-addr`, `--log-file`, `--disable-profiles`).
3. `mode: instance` (Tailscale): `tailscaled --state <root>/tailscaled.state --socket <root>/ts.sock --tun tailscale0` sebagai anak proses, lalu semua CLI memakai `tailscale --socket <root>/ts.sock …`.
4. Validasi silang di API (400): maks satu `netbird/shared`; `interface_name` + `wireguard_port` unik antar Network; proses anak total ≤ 5.
5. `--disable-dns` selalu dipasang pada instance NetBird (dua instance tidak boleh menulis resolver master); `--disable-firewall` sebagai opsi config.
6. `Detect()` melaporkan versi; `Up` menolak `mode: instance` bila `netbird service run` bukan subcommand yang dikenal (versi terlalu lama) dengan pesan jelas.

## Penerimaan

- Tes (driver palsu/exec shim): dua Network `netbird` `mode: instance` dengan `iface`/port berbeda → dua proses anak, dua socket, dua `Path.Iface` berbeda; `Ensure` keduanya `up` tanpa saling mematikan.
- Tes: Network kedua `netbird/shared` ditolak `400` dengan pesan yang menyebut daemon tunggal.
- Tes: `Down` pada instance menghentikan proses anak dan menghapus socket.
- `GOOS=windows go vet ./internal/vpnnet/...` bersih (Windows memakai `npipe` untuk `--daemon-addr`; bila tidak didukung, driver mengembalikan galat jelas, bukan diam-diam `shared`).

## Catatan

Jangan memakai `netbird profile` untuk kasus ini — itu penukaran akun, bukan paralel, dan memutus koneksi Host lain.
