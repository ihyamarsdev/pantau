# 07 — Port-forward Service/Pod ke browser

Type: research
Status: needs-info
Blocked by:

## Temuan riset (ResPortForward)

- Mekanisme terkecil yang aman: `kc port-forward --address 127.0.0.1 svc/NAME LOCAL:PORT` di Host (proses latar), lalu pakai `/api/tunnel/{hostId}/{LOCAL}/` yang sudah ada tanpa mengubah penjagaan anti-SSRF (yang hanya menembak `127.0.0.1` Host). Alternatif `kc proxy` rusak untuk aplikasi web berpath absolut; menembak ClusterIP langsung ditolak oleh penjagaan itu dan sengaja tidak dilonggarkan.
- ClusterIP terjangkau dari node (diverifikasi), tetapi `port-forward` sendiri **tidak dijalankan** (di luar mandat read-only).
- Butuh registri forward, pembersih idle, batas jumlah, dan `pkill` bermarkah karena `sudo -n env … kubectl` membuat sinyal tidak andal. Perkiraan ±150 baris Go.

## Mengapa ditunda (butuh keputusan)

Fitur ini mengubah Pantau dari pembaca metadata menjadi pembuka akses TCP ke service internal klaster, termasuk yang saat ini hanya ClusterIP (di klaster uji: database PostgreSQL dan Redis). Kredensialnya kubeconfig root node. Ini kenaikan permukaan serangan yang nyata, bukan sekadar UI.

Pertanyaan: (1) apakah hanya mode `full`? (2) batas idle 10 menit? (3) apakah fitur ini memang diperlukan dibanding `kubectl port-forward` manual di Terminal Dock yang sudah ada? Bangun hanya setelah dijawab.
