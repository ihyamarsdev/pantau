# 08 — Add/Edit Host + Import: pilih Network

Status: ready-for-agent

Blocked by: 06, 07

## Lingkup

1. `#hostModal`: select `#hostNetwork` (opsi pertama "Direct (tanpa VPN)", lalu daftar Network) di antara *Host Group* dan *Kubernetes Mode*; terisi saat `openHostModal`/edit; `saveHost()` mengirim `network_id`.
2. Grid daftar Host: badge nama Network (merah bila `last_state != up`); data dari `GET /api/hosts` (`network_id`) + `GET /api/vpn/networks` (peta id→nama/state).
3. Import (`#importHostsModal`): select `#importNetworkId` (default Direct) di samping `#importKeyId`, dikirim oleh `importSubmit()`; kolom Network di `#importPreviewBody` dapat diubah per baris (pola `.import-group-input`). `importHostInput` + `handleHostImportCommit` menerima `network_id` (divalidasi: Network ada dan `enabled`, jika tidak → baris `invalid`).
4. `POST /api/hosts` menerima `network_id` (validasi Network ada; `0` boleh).

## Penerimaan

- e2e/nyata: buat Host dengan Network → badge muncul; edit ke Direct → badge hilang; import 2 baris dengan Network berbeda → tersimpan sesuai baris.
- Host dengan `network_id` yang sudah dihapus tidak mungkin ada (delete memakai `force`).

## Catatan

Duplikat import (`importEndpointKey`) tetap berdasarkan host|port|user, bukan Network.
