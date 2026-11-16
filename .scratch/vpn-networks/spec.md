# VPN & Tunnel Networks (jalur jaringan per Host)

Status: ready-for-agent

Host privat yang hanya terjangkau lewat VPN tertentu harus bisa dikelola Pantau seperti Host biasa. Satu entitas **VPN Network** mewakili satu jalur (netbird, netmaker, openvpn, wireguard, tailscale, custom); Host menunjuk jalurnya lewat `hosts.network_id`; setiap koneksi SSH ke Host itu keluar lewat **dial terikat interface** milik Network tersebut, bukan lewat tabel rute default. Keputusan arsitektur dan alasannya: ADR `docs/adr/0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md`.

## Masalah yang diselesaikan

1. Host `10.10.2.5` hanya hidup di tunnel NetBird, Host `10.10.2.5` lain (subnet sama!) hanya di tunnel OpenVPN. Tabel rute hanya memilih satu; hari ini Pantau tidak punya cara memilih.
2. Menyalakan/mematikan tunnel, menyimpan kredensialnya, dan memeriksa kesehatannya masih manual di luar Pantau.
3. Tidak ada isyarat di UI bahwa sebuah Host butuh jalur khusus, dan inspeksi Host tersebut hari ini berakhir "down" — seolah server mati, padahal tunnelnya yang mati.

## Istilah domain (tambahkan ke CONTEXT.md)

- **VPN Network**: satu definisi jalur jaringan (driver + kredensial + interface + kebijakan auto-up) yang dipakai sekumpulan Host untuk keluar dari master. _Avoid_: Gateway Host (sudah dipakai untuk Kubernetes), Tunnel entry, Connection profile
- **Network Driver**: jenis implementasi sebuah VPN Network: `netbird`, `netmaker`, `openvpn`, `wireguard`, `tailscale`, atau `custom`. _Avoid_: Connector, plugin, provider
- **Network Path**: hasil resolusi sebuah VPN Network saat koneksi dibuat: nama interface untuk bind + fungsi dial. _Avoid_: Route, gateway, hop
- **Bound Dial**: dial TCP yang diikat ke interface tertentu (`SO_BINDTODEVICE`/`IP_UNICAST_IF`/`IP_BOUND_IF`) sehingga paket keluar lewat tunnel itu tanpa mengubah tabel rute. _Avoid_: Route override, forced route
- **Tunnel State**: status satu VPN Network: `down`, `starting`, `up`, `error`. _Avoid_: Health, status link
- **Network Down**: kegagalan yang berasal dari jalur (tunnel mati/tidak bisa di-bind), bukan dari Host; Host ditandai `stale`, bukan `down`. _Avoid_: Offline, unreachable

## Non-goals (v1)

Impor peer otomatis lewat API vendor; SSO/OIDC login (`netbird login` interaktif, `netclient join -s` via browser); lebih dari satu Network per Host atau rantai VPN → gateway → Host; driver `ssh_jump`, SOCKS5, dan HTTP CONNECT; pengelolaan rute/DNS vendor oleh Pantau; pemasangan service/daemon vendor (mis. `netbird service install`), `wg-quick` di Windows; IPv6-only tunnnel; pemulihan otomatis multi-tunnel (failover antar Network); ekspos fitur ini ke AI Agent tools.

## Model data

```sql
CREATE TABLE IF NOT EXISTS vpn_networks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  driver TEXT NOT NULL,                 -- netbird|netmaker|openvpn|wireguard|tailscale|custom
  enabled INTEGER NOT NULL DEFAULT 0,
  auto_up INTEGER NOT NULL DEFAULT 1,
  iface TEXT NOT NULL DEFAULT '',       -- '' = default driver; wajib untuk custom
  config TEXT NOT NULL DEFAULT '{}',    -- JSON per driver; memuat rahasia (lihat tabel di bawah)
  last_state TEXT NOT NULL DEFAULT 'down',
  last_error TEXT NOT NULL DEFAULT '',
  last_checked DATETIME,
  last_up_at DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

- `hosts.network_id INTEGER NOT NULL DEFAULT 0` via `d.alterAddColumn("hosts","network_id","INTEGER NOT NULL DEFAULT 0")` di `migrate()` (`internal/store/db.go`), ditambahkan ke `hostSelectCols`, `CreateHost`, `UpdateHost`, `scanHost`; `0` = langsung (perilaku lama, tanpa migrasi data).
- `store.Host` mendapat `NetworkID int64 \`json:"network_id"\``.
- `SnapshotPayload` mendapat `VPNNetworks []SnapshotVPNNetwork` (semua kolom, `config` mentah) agar restore membawa jalur; urutan restore: Network sebelum Host. Berkas ini ikut terenkripsi seperti `ssh_keys.private_key`.
- Hapus Network yang masih dipakai Host: `409` dengan jumlah Host pemakai; tidak ada cascade diam-diam. `DELETE /api/vpn/networks/{id}?force=1` melepas Host (`network_id=0`) setelah konfirmasi UI.

## Kontrak Go: paket `internal/vpnnet` (stdlib + `golang.org/x/sys`; TANPA akses DB)

```go
package vpnnet

type Driver string // "netbird" | "netmaker" | "openvpn" | "wireguard" | "tailscale" | "custom"

type State string // "down" | "starting" | "up" | "error"

type Network struct {
    ID     int64
    Name   string
    Driver Driver
    Enabled, AutoUp bool
    Iface  string            // '' = DefaultIface()
    Config map[string]string // rahasia sudah didekripsi apa adanya (plaintext DB)
    Root   string            // <dir db>/vpn/<id>; dibuat 0700 (openvpn/wireguard)
}

type Status struct {
    State     State
    Iface     string
    Detail    string    // ringkas: "management connected, 45/73 peers"
    Err       string
    Peers     int       // -1 bila driver tidak melaporkan
    CheckedAt time.Time
    BindErr   string    // terisi bila bind interface tidak diizinkan (CAP_NET_RAW)
}

type DriverImpl interface {
    Detect() (bin, version string, err error)          // exec.LookPath + `<bin> version`/`--version`
    Up(ctx context.Context, n Network) error            // idempoten
    Down(ctx context.Context, n Network) error          // idempoten
    Status(ctx context.Context, n Network) (Status, error)
    DefaultIface(n Network) string                      // "" bila driver tidak tahu
}

type Path struct {
    NetworkID int64
    Name      string
    Iface     string                                        // "" = dial langsung
    DNS       []string                                      // alamat resolver jaringan, bila diketahui
}
func (p *Path) DialContext(ctx context.Context, network, addr string) (net.Conn, error)
func (p *Path) Resolve(ctx context.Context, host string) ([]string, error) // dial ke DNS Network bila ada, jika tidak resolver OS

type Manager struct { /* db, drivers, mu, inflight, backoff */ }
func NewManager(db *store.DB, opts Options) *Manager
func (m *Manager) Ensure(ctx context.Context, networkID int64) (*Path, error) // 0 => nil, nil
func (m *Manager) Refresh(ctx context.Context, networkID int64) (Status, error)
func (m *Manager) Up(ctx context.Context, networkID int64) (Status, error)
func (m *Manager) Down(ctx context.Context, networkID int64) error
func (m *Manager) Status(networkID int64) Status                   // dari cache
func (m *Manager) ForEach(fn func(Network, Status))                 // untuk loop status latar
func (m *Manager) Shutdown()                                       // matikan proses anak (openvpn/wireguard)
```

Aturan `Ensure`: bila `NetworkID == 0` → `nil, nil`; bila `!Enabled` → galat `ErrNetworkDisabled`; bila state `up` dan interface masih ada (`net.InterfaceByName`) → `Path, nil`; bila `auto_up` → jalankan `Up` (singleflight per Network, timeout 60 detik) lalu verifikasi interface; bila tidak → galat `ErrNetworkDown`. Galat jalur dibungkus `sshrunner.ErrNetworkUnavailable` di lapisan pemanggil agar UI/inspeksi membedakannya dari Host mati.

Bind per OS (satu file per platform, `//go:build`):

| OS | Implementasi | Catatan |
| --- | --- | --- |
| linux | `unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)` di `net.Dialer.Control` | butuh `CAP_NET_RAW`; galat → `BindErr` menyebut CAP_NET_RAW |
| windows | `IP_UNICAST_IF` (IPPROTO_IP, indeks interface, big-endian IPv4) + `IPV6_UNICAST_IF` | indeks via `net.InterfaceByName` → `windows.IfAnsiToIndex`/`Interface.Index` |
| darwin | `IP_BOUND_IF` (IPPROTO_IP) + `IPV6_BOUND_IF` dengan `ifindex` | |
| lain | `ErrBindUnsupported` | fitur tetap bisa dipakai tanpa `iface` (hanya driver process/daemon) |

## Driver: bidang `config` dan perintah (terverifikasi pada binary lokal)

| Driver | Bidang `config` (rahasia ✱) | Up / Down / Status | Interface |
| --- | --- | --- | --- |
| `netbird` | `mode` (`shared` default \| `instance`), `setup_key`✱, `management_url`, `hostname`, `interface_name` (default `wt0`), `wireguard_port` (default `51820`), `daemon_socket` (default `/var/run/netbird.sock`, hanya `shared`) | **shared**: `netbird up --setup-key … [--management-url …] [--hostname …] [--interface-name …]` / `netbird down` — **hanya satu Network `netbird/shared` yang boleh ada** (daemon tunggal, `up` dengan akun lain memindahkan akun). **instance**: Pantau menjalankan daemon sendiri per Network, `netbird service run --config <root>/config.json --daemon-addr unix://<root>/nb.sock --log-file <root>/netbird.log`, lalu `netbird up --daemon-addr unix://<root>/nb.sock --setup-key … --management-url … --interface-name <iface> --wireguard-port <port> --disable-dns`; `down` = `netbird down --daemon-addr …` lalu hentikan proses. Status keduanya: `netbird status --json [--daemon-addr …]`: `management.connected && signal.connected` → `up`; `peers.connected`/`peers.total` → `Peers`; `fqdn`, `netbirdIp` → `Detail`; `dnsServers` → `Path.DNS` | `interface_name` (`wt0`, wajib unik antar Network) |
| `netmaker` | `token`✱, `network`, `server`, `interface` | `netclient join -t <token> [-n net] [-s server] [-I iface]` / `netclient disconnect <net>` (delete → `netclient leave <net>`) / `netclient list` (root) **dan** verifikasi interface punya alamat → `up`; `peers` tidak dilaporkan (`-1`) | `interface` atau nama jaringan (`nm-…`) |
| `openvpn` | `config`✱ (isi `.ovpn`), `username`✱, `password`✱ | proses anak: `openvpn --config <root>/client.ovpn --auth-nocache [--auth-user-pass <root>/auth.txt] --writepid <root>/openvpn.pid` / kirim `SIGTERM` + tunggu 10 detik / interface ada + punya alamat; device dibaca dari baris log `TUN/TAP device (tun\d+) opened` | dari log, atau bidang `iface` bila diisi |
| `wireguard` | `config`✱ (isi `.conf`) | `wg-quick up <root>/<iface>.conf` / `wg-quick down <root>/<iface>.conf` / interface ada + punya alamat | nama `[Interface]` di berkas |
| `tailscale` | `authkey`✱, `login_server`, `hostname`, `socket` (default `/var/run/tailscale/tailscaled.sock`) | `tailscale --socket <s> up --authkey … [--login-server …] [--hostname …]` / `tailscale --socket <s> down` / `tailscale --socket <s> status --json`: `BackendState == "Running"` → `up`; `Peer` → `Peers`; `TailscaleIPs` → `Detail`. Satu tailnet per instance: dua tailnet = dua `tailscaled` (state+socket sendiri, sama pola `instance` NetBird) | `tailscale0` (unik per instance) |
| `custom` | `up`✱, `down`✱, `status`✱ (string perintah) | `sh -c <cmd>` (Windows: `cmd /c`) dengan `PATH`+`HOME`+`LANG` saja / status = exit 0 / — | `iface` **wajib** |

Berkas `config`/`auth.txt`/`.conf` ditulis 0600 dalam direktori 0700, dihapus saat Network dihapus. Keluaran perintah dipotong 4 KiB, disaring nilai rahasia → `(redacted)`, dan hanya baris terakhir disimpan di `last_error`/`Detail`.

### Multi-akun paralel (dua server NetBird, dua tailnet)

Fakta yang diukur di mesin ini (`netbird` 0.64.1) dan di tracker resmi:

- Satu daemon NetBird = **satu profil aktif**. `netbird profile` hanya menukar akun (`select`), bukan menjalankan dua akun bersamaan (`docs.netbird.io/client/profiles`: "Only one profile is active at a time"). Permintaan multi-akun paralel masih terbuka sejak 2022 (`netbirdio/netbird#446`).
- Karena itu dua server NetBird **tidak** dilayani oleh dua Network `mode: shared`; `up` kedua akan memindahkan akun dan memutus yang pertama.
- Jalur yang didukung (dan disarankan tim NetBird di issue tersebut): satu daemon **per** akun — config/state, socket `--daemon-addr`, `--interface-name`, dan `--wireguard-port` sendiri. Itulah `mode: instance`: Pantau menjalankan/mengawasi daemon itu sebagai proses anak, bukan memakai service sistem.
- **Netmaker tidak butuh ini**: satu `netclient` boleh join banyak network (`join -n <net>`, `list`, `disconnect <network>`, `leave <network>`) dan tiap network dapat interface sendiri. Jadi dua server Netmaker cukup dua Network `netmaker` di daemon yang sama.
- **Tailscale**: satu tailnet per instance; dua tailnet = dua `tailscaled` dengan state dir + `--socket` sendiri (pola sama dengan `instance`).

Aturan yang ditegakkan API (400 dengan pesan jelas):

1. Paling banyak **satu** Network `netbird` `mode: shared`; Network `netbird` `mode: instance` tanpa batas selain limit proses anak.
2. `interface_name` dan `wireguard_port` unik di antara semua Network (bind ke iface yang sama = dua jalur bertabrakan).
3. `mode: instance` selalu menambahkan `--disable-dns` (dua instance tidak boleh sama-sama menulis resolver master); `--disable-firewall` disediakan sebagai opsi bila admin mengelola aturan sendiri.
4. Jumlah proses anak total (`openvpn`, `netbird service run` mode `instance`, `tailscaled` instance, `custom`) maksimal 5.


Fakta yang diukur di mesin ini (`netbird` 0.64.1) dan di tracker resmi:

- Satu daemon NetBird = **satu profil aktif**. `netbird profile` hanya menukar akun (`select`), bukan menjalankan dua akun bersamaan (`docs.netbird.io/client/profiles`: "Only one profile is active at a time"). Permintaan multi-akun paralel masih terbuka sejak 2022 (`netbirdio/netbird#446`).
- Karena itu dua server NetBird **tidak** dilayani oleh dua Network `mode: shared`; `up` kedua akan memindahkan akun dan memutus yang pertama.
- Jalur yang didukung (dan disarankan tim NetBird di issue tersebut): satu daemon **per** akun — config/state, socket `--daemon-addr`, `--interface-name`, dan `--wireguard-port` sendiri. Itulah `mode: instance`: Pantau menjalankan/mengawasi daemon itu sebagai proses anak, bukan memakai service sistem.
- **Netmaker tidak butuh ini**: satu `netclient` boleh join banyak network (`join -n <net>`, `list`, `disconnect <network>`, `leave <network>`) dan tiap network dapat interface sendiri. Jadi dua server Netmaker cukup dua Network `netmaker` di daemon yang sama.
- **Tailscale**: satu tailnet per instance; dua tailnet = dua `tailscaled` dengan state dir + `--socket` sendiri (pola sama dengan `instance`).

Aturan yang ditegakkan API (400 dengan pesan jelas):

1. Paling banyak **satu** Network `netbird` `mode: shared`; Network `netbird` `mode: instance` tanpa batas selain limit proses anak.
2. `interface_name` dan `wireguard_port` unik di antara semua Network (bind ke iface yang sama = dua jalur bertabrakan).
3. `mode: instance` selalu menambahkan `--disable-dns` (dua instance tidak boleh sama-sama menulis resolver master); `--disable-firewall` disediakan sebagai opsi bila admin mengelola aturan sendiri.
4. Jumlah proses anak total (`openvpn`, `netbird service run` mode `instance`, `tailscaled` instance, `custom`) maksimal 5.

## Safety dan batas

1. **Gate `vpn_enabled`** (setting, default `0`) harus aktif sebelum `up`/`down`/`Ensure` boleh menjalankan biner vendor; sejajar Terraform Gate. `PUT /api/vpn/gate {enabled}`; UI menampilkan peringatan: driver menjalankan biner dengan privilese master, `custom` menjalankan string shell, `netclient`/`wg-quick` menolak tanpa root.
2. Validasi: `name` `^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`; `driver` salah satu nilai; `iface` `^[A-Za-z0-9_.-]{0,15}$`; `config` ≤ 64 KiB (`.ovpn`/`.conf` ≤ 64 KiB), perintah `custom` ≤ 512 byte; maksimal 20 Network dan 3 proses anak (`openvpn`/`wireguard`) bersamaan (409 bila lewat).
3. `sh -c` hanya untuk driver `custom` (perintah ditulis admin, di balik gate); driver lain **wajib** `exec.CommandContext` dengan argumen terpisah, tanpa shell.
4. `Env` proses anak: `PATH`, `HOME=<root>/home`, `LANG=C.UTF-8` (Windows: `PATH`, `USERPROFILE`, `SYSTEMROOT`). Env Pantau lain tidak diwariskan.
5. Timeout: `Up` 60 detik (openvpn 90 detik, karena handshake), `Down` 20 detik, `Status` 10 detik. Backoff: setelah galat, `Ensure` tidak mencoba lagi sebelum 30 detik; 3 galat berturut → state `error` (auto-up berhenti sampai `Up` manual atau 5 menit).
6. Rahasia tidak pernah dikembalikan API: `GET /api/vpn/networks` mengganti setiap nilai rahasia dengan `""` dan menambahkan `<key>_set: true` (pola `login_pass` ADR 0076). `PUT` dengan bidang rahasia kosong mempertahankan nilai lama.
7. Bind gagal **fail closed**: tidak ada fallback ke rute default (galat menyebut `CAP_NET_RAW`). Perilaku ini diuji.
8. Loop status latar: `Status` disegarkan tiap 30 detik untuk Network `enabled` (tanpa `Up`), hasil ke cache untuk badge UI; loop berhenti saat `Shutdown`.

## Integrasi: satu choke point

```go
// internal/sshrunner
type Target struct {
    Host string
    Port int
    User string
    Key  string
    Net  *NetPath   // nil = dial langsung (perilaku lama)
}
type NetPath = vpnnet.Path   // atau interface kecil: DialContext + Name
type RunnerFactory func(Target) (Runner, error)   // dari func(host,port,user,key)
func Connect(t Target, timeout time.Duration) (*LiveSSHRunner, error)
type KeyProvisioner func(t Target, password, pubKey string) error
var ErrNetworkUnavailable = errors.New("network unavailable")
```

Perubahan pemanggil (semua, tidak ada yang boleh dilewatkan):

1. `internal/inspector/inspector.go:89` `GetRunnerForHost`: `path, err := ins.paths.Ensure(ctx, h.NetworkID)` → `ins.factory(Target{…, Net: path})`; galat jalur dibungkus `ErrNetworkUnavailable`. Suntikkan lewat `func (ins *Inspector) SetPathResolver(r PathResolver)` (nil-safe; tanpa resolver → `Net: nil`, semua tes lama tetap jalan).
2. `internal/transfer/transfer.go:68` `getRunnerForHost`: sama.
3. `internal/web/server.go:1044` dan `:1498` `s.provisioner(...)` → `s.provisioner(Target{Host, Port, User, Net: path}, password, pubKey)`; `DefaultKeyProvisioner` mendial bound socket dengan `ssh.Dial` atas `net.Conn` dari `Target.Net` (bila `nil` → `net.DialTimeout` seperti sekarang).
4. `POST /api/hosts/{id}/test` (`server.go:1438`) tidak berubah karena memakai `inspector.GetRunnerForHost`; galatnya harus tampil apa adanya (`network down: netbird-prod`).
5. Enam factory di tes (`internal/inspector/*_test.go`) diperbarui ke `func(t sshrunner.Target)`.

Inspeksi: saat `Ensure` gagal, `InspectHostWithTimeout` mencatat Host `status="stale"` + pesan `network down: <name>` (bukan `down`) dan melewati sisa pemeriksaan; metrik lama dipertahankan.

## API

```
GET    /api/vpn/status                -> {enabled, drivers:[{driver, bin, version, err}]}   # deteksi biner
GET    /api/vpn/interfaces            -> [{name, index, addrs:[...]}]                       # net.Interfaces() untuk dropdown iface
PUT    /api/vpn/gate                  -> {enabled}
GET    /api/vpn/networks              -> [{id,name,driver,enabled,auto_up,iface,config(redacted),last_state,last_error,last_checked,last_up_at,hosts_count}]
POST   /api/vpn/networks              -> {id}
PUT    /api/vpn/networks/{id}         -> ok
DELETE /api/vpn/networks/{id}[?force=1]
POST   /api/vpn/networks/{id}/up      -> Status     # manual bring-up
POST   /api/vpn/networks/{id}/down    -> Status
GET    /api/vpn/networks/{id}/log     -> {lines:[...]}   # ring 200 baris terakhir per Network
```
Semua di balik `authMiddleware`; `up`/`down` menolak (`409`) bila gate mati. `hosts_count` dihitung dengan satu query agregat, bukan N+1.

## UI

1. **Settings → Networks** (tab baru di `#settingsTabs`, pola `switchSettingsTab`): daftar kartu Network (nama, driver, badge `Tunnel State`, interface, jumlah Host, tombol Up/Down/Edit/Hapus, log terakhir). Dialog tambah/edit: pilih driver → bidang `config` yang sesuai (rahasia write-only, tampil `•••• sudah disimpan`), pilih `iface` dari `GET /api/vpn/interfaces` (opsi "default driver" = kosong), `enabled`, `auto_up`. Peringatan gate di atas tab bila `vpn_enabled` mati.
2. **Add/Edit Host**: select **Network** di antara *Host Group* dan *Kubernetes Mode* (`#hostNetwork`), opsi pertama "Direct (tanpa VPN)". Badge kecil di kartu Host pada grid yang menampilkan nama Network; badge merah bila `last_state != up`.
3. **Baris diagnostik**: bila `POST /api/hosts/{id}/test` gagal karena jalur, toast menampilkan `network down: <name>` + tombol ke tab Networks.
4. **Import** (`#importHostsModal`): select `#importNetworkId` "Network untuk host yang diimpor" (default Direct), di samping `#importKeyId`, dikirim pada `importSubmit()` commit; tabel `#importPreviewBody` menampilkan kolom Network dan mengizinkannya diubah per baris (pola input `import-group-input` yang sudah ada). `importHostInput` mendapat `network_id`.
5. i18n: semua string lewat `t()`/`tx()` dengan entri `vpn_*` di kedua bahasa (EN/ID), bukan teks keras.

## Tiket

Lihat `.scratch/vpn-networks/issues/` (01–10). Urutan: 01 (skema+CRUD) → 02 (dial per OS) → 03 (driver+Detect) → 04 (supervisor+gate) → 05 (choke point) → 06 (API) → 07 (UI settings) → 08 (host form+import) → 09 (inspeksi/status) → 10 (e2e).

## Verifikasi yang diharapkan

- `go test ./internal/vpnnet/... ./internal/sshrunner/... ./internal/inspector/... ./internal/store/...`
- Uji unit wajib: parser status dari fixture JSON nyata (NetBird `management.connected`, Tailscale `BackendState`); supervisor dengan driver palsu (singleflight, backoff, state `error`); `Path.Dial` ke `lo` bila `CAP_NET_RAW` ada (skip bila tidak); `custom` perintah gagal → `last_error` tersaring.
- `GOOS=windows go vet ./internal/vpnnet/...` dan `GOOS=darwin go vet ./internal/vpnnet/...`.
- e2e (`cd e2e && npm test`): CRUD Network, gate default mati menolak `up`, Host dengan Network yang tunnel-nya mati → `/test` menjawab `network down: …`, badge Host menampilkan nama Network.
