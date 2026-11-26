# Audit Sinkronisasi Upstream: `herliansyah/pantau:main`

**Tanggal audit:** 2026-11-27
**HEAD lokal:** `6a84d5e` (`main`)
**Upstream:** `upstream/main` = `f690e8b` (remote `upstream` → `https://github.com/herliansyah/pantau.git`)
**Merge-base:** `0a5a47a`
**Divergensi:** lokal 157 commit di depan, upstream 35 commit (non-merge) di depan.

Status: **audit saja. Belum ada merge atau cherry-pick.** Menunggu tinjauan Anda.

---

## 1. Ringkasan

- Dari 35 commit, **11 adalah release chore** (bump versi, CHANGELOG, `pantau_test.go`). Tidak perlu di-cherry-pick.
- Ada **1 fix SSH leak yang belum ada di fork** (`e607ff4`). Ini kandidat paling jelas untuk diambil lebih dulu. Commit ini tidak bergantung pada fitur baru.
- Sebagian besar sisa commit membawa **fitur besar yang tidak ada sama sekali di HEAD**: native terminal bridge, storage triage, Windows observability, uPlot time-series, public status page, dan netdiag Trevally. Bug fix dan hardening di area tersebut hanya berguna jika fitur induknya ikut diambil.
- **Keputusan utama untuk Anda:** apakah fitur-fitur itu memang ingin dibawa ke fork, atau hanya bug fix yang relevan.
- **Peringatan `.scratch`:** upstream masih melacak folder `.scratch/` (sudah di-untrack lokal). Setiap cherry-pick yang menyentuh `.scratch/` akan menimbulkan konflik modify/delete. Hunk `.scratch/` harus dibuang saat cherry-pick.

---

## 2. Daftar Lengkap Commit (35, non-merge, kronologis)

Keterangan kolom **Verifikasi**: apa yang saya cek di HEAD untuk menentukan kategori.

| # | Commit | Judul | Kategori | Verifikasi di HEAD |
|---|--------|-------|----------|--------------------|
| 1 | `dc11404` | Trevally (#67): netdiag triage matrix, hybrid observability | B | `internal/netdiag/` ada (`parser.go`, `tools.go`, `local.go`), tapi `triage.go` dan `types.go` tidak ada. Desain lokal berbeda. |
| 2 | `287b9ff` | chore(release) v0.26.0 | C (ambil hunk `IsClosed`) | Hanya `sshrunner/runner.go` +6 baris (`MockRunner.IsClosed`) yang dibutuhkan test `e607ff4`. Sisanya release. |
| 3 | `e607ff4` | **fix(ssh): prevent remote process and connection leaks** | **A** | HEAD masih `inWriter.Close()` / `outWriter.Close()` tanpa `runner.Close()` saat WebSocket putus. Reaper idle tunnel juga belum ada. |
| 4 | `97a6c42` | chore(release) v0.27.0 | C | Release. |
| 5 | `fd54f36` | feat(terminal): native terminal bridge | B (induk) | `handlers_native_terminal.go` **tidak ada** di HEAD. Dibutuhkan oleh #8, #10, #11, #13. |
| 6 | `12d93bc` | docs: sinkronisasi dokumentasi | B (docs) | Mengubah README/user-guide. Konflik tinggi dengan README lokal. |
| 7 | `94a0fe6` | feat(terminal): active path navigation | B | Bergantung #5. |
| 8 | `f59f7f6` | **fix(terminal): string host IDs & split pane** | **A (bersyarat #5)** | Bug fix, tapi file target tidak ada di HEAD. |
| 9 | `2fa78f6` | feat(terminal): home paths & track active dir | B | Bergantung #5. |
| 10 | `1d33792` | chore(release) v0.28.0 | C | Release. |
| 11 | `fffc28d` | **fix(terminal): avoid semicolon splitting on Windows** | **A (bersyarat #5)** | Perbaikan pemisahan perintah (`;`) saat meluncurkan terminal native di Windows. Bergantung #5. |
| 12 | `a3455c7` | chore(release) v0.28.1 | C | Release. |
| 13 | `06da716` | Pacu (#77): note preview UI | B | `note_preview_ui_test.go` belum ada. Hanya UI. |
| 14 | `00b2629` | feat(observability): time-series, app telemetry, uPlot | B (besar) | `internal/store/metrics_test.go` dan `vendor/uPlot*` belum ada. `db.go` +296 baris, `index.html` +2268. Konflik sangat tinggi. |
| 15 | `6576d09` | chore(release) v0.29.0 | C | Release. |
| 16 | `065d838` | Tarpon (#79): public status page | B (**perlu tinjauan keamanan**) | `handlers_status_public.go`, `rate_limiter.go`, `status_page.go` belum ada. Endpoint publik: wajib review auth/isolasi sebelum adopsi. |
| 17 | `59e3956` | chore(release) v0.30.0 | C | Release. |
| 18 | `c149e23` | feat(storage-triage): agentless triage & safe reclaim (#80) | B (induk) | `internal/storagetriage/` **tidak ada** di HEAD. Induk #19–#22, #23, #24. |
| 19 | `eba8134` | chore(release) v0.31.0 | C | Release. |
| 20 | `2002c18` | Host switcher & 1-click metrics shortcut (#81) | B | `host_switcher_ui_test.go` belum ada, tidak ada "host switcher" di HEAD. |
| 21 | `a2ac81a` | chore(release) v0.31.1 | C | Release. |
| 22 | `e4be6de` | Feat windows support (#82) | B (besar) | `inspector.go` +656, `sshrunner` +75, `handlers_native_terminal.go` +168. Konflik tinggi dengan `inspector.go` lokal yang sudah berkembang. |
| 23 | `25549c6` | feat(storage-triage): agentless Windows triage (#83) | B | Bergantung #18, #22. |
| 24 | `07de90f` | docs: agentless Windows observability (#84) | B (docs, ada `.scratch/`) | Menyentuh `.scratch/` dan README. |
| 25 | `1c9a17f` | **Improve Windows storage triage reliability & process cleanup (#85)** | **A (bersyarat #18)** | Bug fix proses dan `sshrunner/runner.go` -48 baris. Bergantung #18. |
| 26 | `67b912d` | chore(release) v0.32.0 | C | Release. |
| 27 | `1882101` | feat(web): batch file deletion (#86) | B (**perlu tinjauan keamanan**) | Menambah penghapusan file massal. Risiko data loss. Tinjau sebelum adopsi. |
| 28 | `b55b5c7` | feat: manual storage path monitoring (#87) | B | Bergantung #18. |
| 29 | `f93d5a5` | **Harden storage triage monitoring and file deletion handling (#88)** | **A (bersyarat #18, #27, #28)** | Hardening keamanan penghapusan file. Bergantung rantai storage triage. |
| 30 | `f073122` | chore(release) v0.32.1 | C | Release. |
| 31 | `2075d8d` | Windows drive switcher & ignore exec paths in terminal cd (#89) | B | Bergantung #5. Menambah 402 baris. |
| 32 | `e6d7f4f` | chore(release) v0.32.2 | C | Release. |
| 33 | `f690e8b` | **fix(web): native terminal launch & path tracking on Windows (#90)** | **A (bersyarat #5)** | Bug fix. Bergantung #5. |
| 34 | `648f6dd` | Wobbegong (#69): group filter UI | C (kemungkinan tergantikan) | Fork lokal sudah punya group filter (8 file menyebut group filter) dan docker stack grouping (commit `9e4fa7f`). **Verifikasi manual** sebelum dibuang. |
| 35 | `92dce42` | refs/heads/herliansyah/albatross (#74) | B (docs) | Menulis ulang README (±350 baris). **Jangan** ambil utuh: akan menimpa README lokal. |

> Catatan: urutan tabel di atas adalah urutan kronologis `git log --reverse`. Nomor 1–35 bukan hasil hitung ulang commit lokal. Angka ini hanya untuk referensi di dokumen.

---

## 3. Pengelompokan

### (a) Security / Bug Fix Penting: wajib di-cherry-pick

| Prioritas | Commit | Alasan | Prasyarat |
|-----------|--------|--------|-----------|
| **1** | `287b9ff` (hanya hunk `IsClosed` di `internal/sshrunner/runner.go`) + `e607ff4` | Mencegah proses jarak jauh (`top`, `tail`, `docker logs -f`, `zip`) tetap jalan setelah client putus. Juga reaper idle koneksi tunnel. Tidak bergantung fitur baru. | Tidak ada. Konflik kemungkinan di `server.go` (fork sudah banyak berubah). |
| 2 | `fffc28d`, `f59f7f6`, `f690e8b` | Perbaikan pemisahan perintah di Windows (`;`), ID host string, peluncuran terminal Windows. | `fd54f36` (native terminal) |
| 3 | `1c9a17f` | Perbaikan proses dan reliabilitas storage triage di Windows. | `c149e23` (storage triage) |
| 4 | `f93d5a5` | Hardening monitoring storage dan penghapusan file. Terkait keamanan. | `c149e23`, `b55b5c7`, `1882101` |

Catatan untuk kelompok (a) bersyarat: membawa commit 2–4 berarti membawa fitur induknya juga. Keputusan ada di bagian 4.

### (b) Fitur / Refactor relevan, berpotensi konflik

Diurutkan dari yang paling mudah ke yang paling berat. Setiap blok adalah rantai dependensi.

- **Native terminal bridge:** `fd54f36` → `94a0fe6` → `2fa78f6` → `2075d8d`. Opsional: `12d93bc`, `92dce42` (docs).
- **Storage triage:** `c149e23` → `25549c6` → `b55b5c7` → `1882101` → `f93d5a5`. Didukung `e4be6de` (Windows) dan `07de90f` (docs).
- **Windows support:** `e4be6de`. Konflik tinggi di `inspector.go`. Dibutuhkan storage triage Windows.
- **Netdiag Trevally:** `dc11404`. Konflik tinggi. Fork punya `netdiag` sendiri (`parser.go`, `tools.go`, `local.go`), jadi perlu penyatuan desain, bukan cherry-pick biasa.
- **Observability time-series + uPlot:** `00b2629`. Konflik sangat tinggi: `db.go` +296, `index.html` +2268, plus vendor JS.
- **Host switcher:** `2002c18`. Mandiri, UI saja.
- **Note preview:** `06da716`. Mandiri, UI saja.
- **Public status page:** `065d838`. Ukuran besar (27 file). **Wajib tinjauan keamanan**: endpoint publik, rate limiter baru, isolasi zero-trust.

### (c) Sudah tergantikan / tidak perlu

- **Release chore (11 commit):** `287b9ff` (kecuali hunk `IsClosed`), `97a6c42`, `1d33792`, `a3455c7`, `59e3956`, `6576d09`, `eba8134`, `a2ac81a`, `67b912d`, `f073122`, `e6d7f4f`. Hanya bump versi, CHANGELOG, dan `pantau_test.go`. Konflik di file itu hampir pasti. Lewati.
- **`648f6dd` Wobbegong (group filter):** kemungkinan sudah tergantikan oleh group filter dan docker stack grouping lokal. Verifikasi manual dulu.

---

## 4. Rencana Cherry-Pick (belum dieksekusi)

Tujuan: aman, bertahap, bisa dibatalkan per langkah.

**Langkah 0. Persiapan**
- Buat branch baru dari `main`, misalnya `sync/upstream-ssh-leak`. Jangan kerjakan langsung di `main`.
- Pastikan `.scratch/` sudah di-untrack (sudah dilakukan, belum di-commit).

**Langkah 1. Fix SSH leak (rekomendasi pertama)**
- `git cherry-pick -n 287b9ff` lalu ambil **hanya** hunk `IsClosed` (`git checkout -p`), buang sisanya.
- `git cherry-pick e607ff4`. Selesaikan konflik di `internal/web/server.go` dan `handlers_tunnel.go`.
- Jika ada hunk `.scratch/` atau CHANGELOG/`pantau_test.go`, buang.
- Verifikasi hanya paket yang disentuh (sesuai `AGENTS.md`): `go test ./internal/sshrunner ./internal/web`.

**Langkah 2. Keputusan Anda** (lihat bagian 5). Setelah itu, rantai fitur dijalankan per blok (native terminal, storage triage, dst.) dengan urutan dependensi di bagian 3(b).

**Langkah 3. Setiap cherry-pick**
- Buang hunk `.scratch/` (`git checkout --ours` atau edit manual, karena `.scratch/` sudah ignored).
- Abaikan commit release.
- Verifikasi per paket yang berubah saja. Tidak ada `go test ./...` penuh kecuali diminta.

---

## 5. Keputusan yang Perlu Anda Ambil

1. **Apakah Anda ingin fitur-fitur upstream (native terminal, storage triage, Windows, uPlot, status page, Trevally) dibawa ke fork?**
   - Jika **tidak**: ambil hanya kelompok (a) yang mandiri (`e607ff4` + hunk `IsClosed`). Bug fix terminal dan storage triage dilewati.
   - Jika **ya**: rantai fitur di bagian 3(b) dikerjakan satu per satu. Estimasi konflik tinggi di `server.go`, `index.html`, `inspector.go`, dan `db.go`.
2. **Status page publik (`065d838`):** setuju ditunda sampai ada tinjauan keamanan?
3. **Batch delete file (`1882101`):** perlu kebijakan keamanan dulu sebelum diambil?
4. **Wobbegong (`648f6dd`):** tolong konfirmasi apakah group filter lokal sudah cukup.
5. **`.scratch/` di-untrack:** upstream dan `origin` masih menyimpan `.scratch/`. Clone lain tidak lagi menerima issue tracker ini lewat git. Pastikan ada cadangan lokal jika diperlukan.

---

## 6. Hasil Verifikasi Lokal

**Perubahan pada repo ini (belum di-commit):**
- `.gitignore`: ditambah `.scratch/` (baris komentar + entri).
- `.claudeignore`: ditambah `.scratch/**`.
- `git rm -r --cached .scratch`: 102 file dilepas dari index, staged sebagai deletion. File di disk tetap ada.
- `git remote add upstream https://github.com/herliansyah/pantau.git` dan `git fetch upstream`. Tidak ada perubahan pada `main`.

**`go test ./...`: tidak bisa selesai sebagai satu perintah.**
- Gagal di tahap setup: `pattern ./...: open data/vpn: permission denied`. Penyebab: `data/vpn/` dimiliki `root:root` dengan mode `700`. Ini masalah lingkungan, bukan kode.
- Saya menjalankan `go test -count=1` pada semua paket yang bisa diakses (`./internal/...` dan root):
  - **Lulus:** 23 dari 25 paket.
  - **Gagal:** `internal/web` (5 test) dan root `pantau` (2 test).

| Test yang gagal | Paket |
|-----------------|-------|
| `TestAgentChatNativeOverHTTP` | internal/web |
| `TestIacAnsibleAdhoc` | internal/web |
| `TestGatewayUpdateCheckUIInBundles` | internal/web |
| `TestGatewayUpdateCheckI18nKeys` | internal/web |
| `TestGatewayCLIUpdateControlsUI` | internal/web |
| `TestDiskUsageDriftNoDu` | pantau (root) |
| `TestUIPackageTabAndProcessTelemetryPresence` | pantau (root) |

**Ketujuh test ini sudah gagal di HEAD bersih** (dicek di worktree sementara dari `6a84d5e`, sudah dihapus). Jadi kegagalan ini bukan dari perubahan audit ini. Pada test UI, string/elemen yang diharapkan memang belum ada di fork (misalnya `tfcli_hint`, `top-resource-consumers`).

Untuk menjalankan `go test ./...` penuh, perbaiki izin `data/vpn` (butuh `sudo`; saya tidak mengubahnya tanpa izin Anda).

---

## 7. Keputusan Final dan Eksekusi

Keputusan pengguna (diterapkan):
- **Diambil:** hanya `e607ff4` (fix SSH leak), beserta hunk `IsClosed` dari `287b9ff` sebagai prasyarat test.
- **Di-SKIP:** fitur besar (native terminal, storage triage, Windows, uPlot, Trevally), `065d838` (status page), dan `1882101` (batch delete).
- **`648f6dd` (Wobbegong) di-SKIP.** Hasil cek:
  - Opsi "No Grouping" (`filter_no_grouping`, `value="none"`) **tidak** ada di HEAD. Jadi fungsinya **tidak** tercakup implementasi lokal. Kriteria "sudah tercakup" tidak terpenuhi.
  - Isinya bukan murni bug fix: ada restyle tabel host (kolom, badge, dropdown), opsi grouping baru, dan test baru (`group_filter_ui_test.go`, `TestHostListDropdownPositioning`).
  - Alasan SKIP: di luar keputusan "hanya e607ff4", dan merupakan perubahan UI yang berpotensi konflik dengan UI host lokal. Jika ingin "No Grouping" kelak, ambil sebagai fitur terpisah.

`data/` sudah tercantum di `.gitignore` (baris 2). Tidak perlu entri tambahan.
