# 03 — Driver vendor: netbird, netmaker, openvpn, wireguard, tailscale, custom

Status: ready-for-agent

Blocked by: 01, 02

## Lingkup

`internal/vpnnet/drivers.go` + satu berkas per driver. Kontrak `DriverImpl` sesuai spec (Detect/Up/Down/Status/DefaultIface). Bidang `config` dan perintah **persis** tabel di spec; jangan mengarang flag.

- `Detect()`: `exec.LookPath` + versi (`netbird version`, `netclient version`, `tailscale version`, `openvpn --version` dibaris pertama, `wg --version`); cache 60 detik; di Windows `wg-quick` dilaporkan tidak didukung.
- File-backed driver (`openvpn`, `wireguard`): tulis `<root>/client.ovpn`, `<root>/auth.txt`, `<root>/<iface>.conf`, 0600 di dir 0700; proses anak via `exec.CommandContext` dengan `PATH`,`HOME`,`LANG` saja; `Down` = SIGTERM + tunggu 10 detik + SIGKILL.
- `openvpn` interface dibaca dari log `TUN/TAP device (tun\d+) opened`; `wireguard` dari nama `[Interface]`; `netbird` dari `interface_name`; `netmaker` dari `interface`/nama jaringan; `tailscale` `tailscale0`.
- `custom`: `sh -c` (`cmd /c` di Windows), status = exit code 0.
- Semua keluaran perintah dipotong 4 KiB dan disaring nilai rahasia → `(redacted)` sebelum masuk `last_error`/`Detail`.

## Penerimaan

- Tes parser status dari **fixture JSON nyata** (disimpan sebagai testdata): NetBird (`management.connected`, `signal.connected`, `peers.connected`, `dnsServers`) dan Tailscale (`BackendState`). Fixture diambil dari mesin ini, bukan dikarang.
- Tes `custom`: `up=false` → `Up` mengembalikan galat, `last_error` tersaring (setup key tidak muncul).
- Tes file-backed: berkas 0600, dir 0700, `iface` dari isi berkas; tidak ada `sh -c` untuk driver selain `custom`.

## Catatan

`netclient` menolak jalan tanpa root ("This program must be run with elevated privileges"); driver harus meneruskan pesan itu apa adanya agar UI bisa menjelaskan.
