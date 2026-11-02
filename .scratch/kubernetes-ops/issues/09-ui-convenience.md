# 09 — Kenyamanan UI

Type: task
Status: resolved
Blocked by:

## Perubahan (hanya `index.html` + panduan pengguna)

- **Events**: filter *Warning saja* (default mati), penghitung.
- **Pods**: pod bermasalah (status bukan `Running`/`Completed`/`Succeeded`, atau restart tinggi) diurutkan paling atas dan diberi penanda; filter *Hanya bermasalah*. Urutan sisanya tetap.
- **Auto-refresh opsional**: pilihan Off (bawaan) / 10 dtk / 30 dtk / 60 dtk per bagian Kubernetes; berhenti saat pindah tab/bagian atau modal log dibuka; tidak menumpuk permintaan (lewati bila yang sebelumnya belum selesai); tidak aktif bila tab browser tersembunyi (`document.hidden`). Ini melonggarkan keputusan "tanpa polling" ADR 0043 butir 7 secara eksplisit sebagai pilihan pengguna; dicatat di ADR 0045.
- i18n EN|ID; panduan pengguna (EN+ID) satu paragraf.

## Acceptance

- Smoke di browser terhadap instance throwaway dengan data nyata `odin` (read-only): filter Warning, pod bermasalah di atas (`kubernetes-bootcamp` berstatus `ImagePullBackOff` di klaster uji), auto-refresh berhenti saat tab pindah, tidak ada error konsol. Ukuran gzip: laporkan sebelum/sesudah; bila batas 220000 byte terlampaui, naikkan seminimal mungkin dan katakan eksplisit.
