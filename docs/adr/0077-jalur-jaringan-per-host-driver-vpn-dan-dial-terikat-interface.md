# 0077. Jalur jaringan per Host: driver VPN/tunnel dan dial terikat interface

Host privat yang hanya bisa dijangkau lewat VPN tertentu kini dapat dikelola Pantau: satu entitas **VPN Network** per jalur (netbird, netmaker, openvpn, wireguard, tailscale, atau perintah kustom), satu kolom `hosts.network_id`, dan satu primitif koneksi — dial TCP yang **diikat ke interface** — sehingga Host yang berbeda dapat keluar lewat tunnel yang berbeda meskipun tabel rute keduanya bertumpuk.

## Konteks dan Masalah

1. **Tidak ada konsep jalur.** Seluruh akses ke Host melewati satu titik: `sshrunner.Connect` → `dialSSH` → `dialTCP` → `net.DialTimeout("tcp", "host:port")` (`internal/sshrunner/runner.go:111-175`). Satu-satunya pemanggil adalah `sshrunner.DefaultFactory`; pemakainya dua — `inspector.GetRunnerForHost` dan `transfer.getRunnerForHost`. Semua fitur (inspeksi, terminal, port forward, SFTP, transfer, tunnel web, diagnostik) duduk di atas dua pemanggil itu.
2. **Host privat butuh tunnel di sisi master.** Pantau agentless: dial dilakukan oleh proses Pantau di mesin master, bukan dari browser operator. Jadi VPN/tunnel harus hidup di master, dan tiap Host perlu tahu jalur mana yang dipakai.
3. **Rute bertumpuk adalah kasus sebenarnya.** NetBird, Netmaker, dan Tailscale umumnya memberi rute `0.0.0.0/0` atau subnet yang sama (mis. `10.0.0.0/24`), dan OpenVPN mendorong `redirect-gateway`. Tabel rute hanya punya satu pemenang; `ping`/`curl` manual dari shell sering "berhasil" hanya karena tunnel lain yang sedang aktif. Memilih tunnel lewat perubahan `ip route` global berarti Pantau merusak jaringan master dan tidak bisa melayani dua Host di dua tunnel sekaligus.
4. **Vendor berbeda bentuk eksekusi.** NetBird, Netmaker (netclient), dan Tailscale adalah daemon yang dikendalikan CLI (`netbird up`, `netclient join`, `tailscale up`), sedangkan OpenVPN dan WireGuard (`wg-quick`) adalah proses yang harus dijalankan dan diawasi. Satu model driver harus menampung keduanya.
5. **Privilese.** `SO_BINDTODEVICE` butuh `CAP_NET_RAW`; `wg-quick`/`openvpn` butuh `CAP_NET_ADMIN`; `netclient` menolak jalan tanpa root ("This program must be run with elevated privileges"). Ini selevel dengan Terraform Gate (ADR 0056) dan harus opt-in dengan peringatan.
6. **Istilah.** `Gateway Host` di CONTEXT.md sudah dipakai untuk makna Kubernetes; kata itu tidak boleh dipakai untuk VPN.

## Keputusan Arsitektur

1. **Entitas `VPN Network`** (tabel `vpn_networks`): `name`, `driver`, `enabled`, `auto_up`, `iface`, `config` (JSON per driver, memuat rahasia), `last_state`, `last_error`, `last_checked`, `last_up_at`. Host menunjuk satu jalur lewat `hosts.network_id` (`0` = langsung, perilaku lama). Satu Host = satu Network; tidak ada rantai.
2. **Primitif tunggal: dial terikat interface.** `vpnnet.Path` membawa `Iface` dan `Dial(ctx, network, addr)`, diimplementasikan per OS di balik `net.Dialer.Control`:
   - Linux: `SO_BINDTODEVICE` (level `SOL_SOCKET`, `unix`).
   - Windows: `IP_UNICAST_IF` + `IPV6_UNICAST_IF` (indeks interface, urutan byte jaringan untuk IPv4).
   - macOS: `IP_BOUND_IF` + `IPV6_BOUND_IF`.
   Bila `Iface` kosong, `Path.Dial` = dial biasa (jalur lama). Bind yang gagal **fail closed** dengan galat yang menyebut `CAP_NET_RAW` — tidak pernah jatuh kembali ke rute default, karena itu bisa menyambung ke Host yang salah.
3. **Driver menampung dua bentuk eksekusi.** `netbird`/`netmaker`/`tailscale` = **daemon-driven**: perintah CLI idempoten (`up`/`login`, `join`, `up`) + status JSON vendor. `openvpn`/`wireguard` = **process-driven**: berkas konfigurasi ditulis ke `<root>/vpn/<id>/`, proses anak dijalankan dan diawasi (`exec.CommandContext`, kill saat Down/restart). `custom` = perintah `up`/`down`/`status` milik admin. Deteksi biner memakai `exec.LookPath` seperti `terraform.Detect()`.
4. **Supervisor `internal/vpnnet`** menjaga state machine per Network (`down`/`starting`/`up`/`error`) dengan singleflight per Network (satu bring-up aktif), backoff 30 detik setelah galat, dan `Ensure(ctx, networkID) (*Path, error)` yang dipanggil sebelum setiap dial. `auto_up=false` membuat Network hanya dinaikkan lewat tombol UI.
5. **Satu choke point tetap satu.** `sshrunner.RunnerFactory` berubah menjadi `func(Target) (Runner, error)` dengan `Target{Host, Port, User, Key, Net *NetPath}`; `inspector.GetRunnerForHost` dan `transfer.getRunnerForHost` mengisi `Net` dari resolver yang disuntikkan. `KeyProvisioner` (dua pemanggil: `server.go:1044`, `server.go:1498`) menerima `Target` pula, karena ia mendial sendiri dengan kata sandi. Tidak ada jalur SSH kedua yang boleh dibuat.
6. **Fail-closed yang terbaca.** Galat resolusi jalur dibungkus `sshrunner.ErrNetworkUnavailable`; inspeksi menandai Host `stale` dengan alasan `network down: <nama>`, bukan `down` (Host sehat, jalurnya yang mati), sehingga alarm "server mati" tidak salah bunyi.
7. **Gate `vpn_enabled` default mati**, sejajar Terraform Gate: driver menjalankan biner vendor dengan privilese master, dan perintah `custom` menjalankan string shell. UI wajib menampilkan peringatan sebelum mengaktifkan.
8. **Rahasia mengikuti aturan yang sudah ada** (ADR 0076): nilai di `config` tersimpan plaintext di SQLite (sama seperti `custom_key`, token Cloudflare, kunci S3), tidak pernah dikembalikan API (write-only, diganti penanda `*_set`), tidak pernah masuk log (keluaran perintah dipangkas 4 KiB dan disaring), dan ikut System Snapshot terenkripsi.

## Batasan yang Disengaja

1. **Tanpa API vendor.** Tidak ada impor peer otomatis dari dashboard NetBird/Netmaker, tidak ada pembuatan setup key, tidak ada OIDC/SSO login (yang butuh browser). Kredensial masuk lewat setup key/token/berkas yang dibuat admin.
2. **Satu Network per Host**, tanpa rantai (VPN → gateway SSH → Host). Jump host dan proxy SOCKS/HTTP di luar cakupan v1; keduanya dapat menyusul sebagai driver tambahan tanpa mengubah kontrak.
3. **Pantau tidak mengelola rute/DNS vendor.** NetBird/Netmaker/Tailscale mengurus rute dan resolver mereka sendiri; itu justru alasan pemilihan bind-interface. Nama Host di-resolve lewat resolver jaringan hanya bila driver melaporkan alamat DNS-nya (NetBird `dnsServers`, Tailscale `100.100.100.100`, Netmaker dari konfigurasi); selain itu resolver OS dipakai.
4. **Butuh privilese.** Bind interface butuh `CAP_NET_RAW`; daemon/berkas VPN butuh root. Tanpa itu fitur menolak dengan pesan jelas, bukan diam-diam memakai rute default.
5. **Windows/macOS hanya dial.** Bind per-OS didukung, tetapi pemasangan service vendor, `wg-quick` di Windows, dan driver TUN non-Linux tidak diuji otomatis di CI Linux; dinyatakan sebagai batas dukungan.

## Alternatif yang Dipertimbangkan

1. **Proxy per jaringan (SOCKS5/HTTP CONNECT) yang diekspos vendor.** Ditolak: tidak semua vendor mengekspos proxy, dan menambah satu lapisan proses yang harus diawasi.
2. **Namespace jaringan per Host (`netns` + veth).** Paling bersih secara isolasi, tetapi butuh root penuh, menduplikasi tumpukan jaringan per Host, dan merusak port forwarding/browser bridge yang mengandalkan loopback master.
3. **Menyunting tabel rute global sebelum tiap koneksi** (`ip route replace`): serial, merusak koneksi lain yang sedang berjalan, dan gagal pada dua Host di dua tunnel bersamaan. Ditolak.
4. **Memakai SDK/klien WireGuard userspace sendiri** (`wireguard-go` sebagai library): menyalin fungsi yang sudah disediakan vendor, dan tetap tidak menyelesaikan konflik rute tanpa bind.
5. **SSH jump host** (tunnel hidup di mesin lain): ditunda — sebagai driver `ssh_jump` ia cocok dengan kontrak `Path` yang sama.

## Verifikasi

1. JSON status NetBird diukur langsung pada mesin ini (`netbird` 0.64.1): kunci tingkat atas `management.connected`, `signal.connected`, `fqdn`, `netbirdIp`, `dnsServers`, `usesKernelInterface`; interface default `wt0` (flag `--interface-name`). Tidak ada kunci `status`/`wireguardInterface` tingkat atas — spesifikasi tidak boleh mengarang keduanya.
2. `netclient` (Netmaker): `join -t <token> [-n net] [-I interface] [-s server]`, `disconnect <network>`, `leave <network>`; `list` menolak tanpa root.
3. Tailscale: `tailscale status --json` → `BackendState` (`Running`), `TailscaleIPs`, `Self`, `TUN`; interface `tailscale0`.
4. Rencana uji: unit (parser status dari fixture JSON nyata, supervisor dengan driver palsu, bind dial ke `lo` bila privilese ada, `go vet` lintas-OS untuk windows/darwin), dan e2e (CRUD Network, Host dengan Network yang tunnel-nya mati harus melaporkan `network down`, gate default mati).
