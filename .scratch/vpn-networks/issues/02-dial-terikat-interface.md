# 02 — Dial terikat interface per OS (`Path.DialContext`, `Path.Resolve`)

Status: ready-for-agent

Blocked by: 01

## Lingkup

`internal/vpnnet/dial_linux.go`, `dial_windows.go`, `dial_darwin.go`, `dial_other.go`, `path.go`:

- `Path{DialContext, Resolve}`; `Iface == ""` → dial/resolve biasa.
- Linux: `SO_BINDTODEVICE` lewat `net.Dialer{Control: …}` + `unix.SetsockoptString`.
- Windows: `IP_UNICAST_IF` (indeks interface, big-endian untuk IPv4) dan `IPV6_UNICAST_IF`, via `golang.org/x/sys/windows`.
- Darwin: `IP_BOUND_IF` dan `IPV6_BOUND_IF` dengan ifindex.
- OS lain: `ErrBindUnsupported`.
- Galat `EPERM`/`EACCES` dipetakan ke pesan yang menyebut `CAP_NET_RAW`; **tidak ada fallback** ke rute default.
- `Resolve`: `net.Resolver{PreferGo: true, Dial: func(ctx, network, _, _) { return Path.DialContext(ctx, "udp", JoinHostPort(dns, "53")) }}` bila `DNS` terisi, selain itu resolver OS.

## Penerimaan

- Tes Linux: listener `127.0.0.1:0` di `lo`, `Path{Iface:"lo"}.DialContext` sukses (skip `t.Skip` bila EPERM); `Path{Iface:"iface-yang-tidak-ada"}` gagal dengan galat yang menyebut nama interface, tanpa panic.
- `GOOS=windows go vet ./internal/vpnnet/...`, `GOOS=darwin go vet ./internal/vpnnet/...` bersih.
- Fail-closed diuji: bind gagal tidak menghasilkan koneksi.

## Catatan

`Control` menerima `syscall.RawConn`; bungkus galat setsockopt menjadi galat dial, jangan menelannya.
