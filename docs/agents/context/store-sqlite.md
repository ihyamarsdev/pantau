# Store, SQLite & Snapshot

Domain reference for SQLite storage, migrations, snapshots and fleet-wide persisted search. Read this when working in `internal/store/` on persistence, schema or snapshot code.

**System Snapshot**:
Arsip mandiri terenkripsi yang memuat seluruh basis data Pantau (konfigurasi Host, kredensial SSH, aturan Desired State, dan pengaturan aplikasi).
_Avoid_: Database dump, backup archive, config export

**Snapshot Passphrase**:
Kunci rahasia berbasis frasa sandi dari pengguna yang digunakan untuk derivasi kunci enkripsi (AES-256-GCM) sebelum System Snapshot disimpan atau dipulihkan.
_Avoid_: Master password, backup pin, encryption key

**Remote Storage Provider**:
Konektor penyimpanan eksternal (GitHub Private Repository) untuk sinkronisasi System Snapshot secara berkala melalui protokol HTTPS API.
_Avoid_: Cloud storage, backup destination, git sync

**Changelog**:
Catatan historis kronologis terstruktur (berbasis standar Keep a Changelog) yang mendokumentasikan penambahan fitur, perubahan perilaku, dan perbaikan bug di setiap rilis Pantau, disematkan langsung ke dalam biner aplikasi untuk akses in-app pada lingkungan terisolasi (airgapped).
_Avoid_: Release notes, commit log, version summary, update history

**Self-Contained Web Assets**:
Koleksi seluruh pustaka antarmuka web Pantau (stylesheet CSS, modul terminal xterm, dan editor teks CodeMirror) yang disematkan langsung ke dalam binary aplikasi melalui sistem file tersemat (embedded filesystem) dan disajikan secara lokal, menghilangkan seluruh ketergantungan jaringan eksternal ke CDN publik untuk menjamin operasional penuh pada lingkungan terisolasi (airgapped).
_Avoid_: CDN dependencies, online scripts, external assets

**Fleet Search**:
Pencarian paket dan container lintas seluruh Host dari data yang sudah tersimpan hasil scan terakhir (tanpa koneksi SSH saat mencari), lengkap dengan umur data per Host.
_Avoid_: Global grep, inventory query, live search

**Disk Usage Explorer**:
Penelusuran pemakaian disk per folder (satu tingkat, satu filesystem, seperti `ncdu`) yang dijalankan on-demand dari File Manager untuk menemukan folder penyebab disk penuh.
_Avoid_: du scan, storage audit, disk report

**Host Group**:
Pengelompokan logis untuk satu atau lebih Host pada dashboard Pantau berdasarkan fungsi server, lingkungan (environment), atau lokasi (misal: "Server Utama", "Testing", "Database Cluster"). Nama Host Group tidak membedakan huruf besar/kecil: nama yang hanya berbeda huruf besar/kecil dari grup yang sudah ada memakai penulisan grup tersebut.
_Avoid_: Server cluster, host tag, machine folder

**Notification Channel**:
Saluran pengiriman peringatan saat terdeteksi Drift (seperti Telegram bot, webhook, atau in-app dashboard).
_Avoid_: Alert sink, message publisher
