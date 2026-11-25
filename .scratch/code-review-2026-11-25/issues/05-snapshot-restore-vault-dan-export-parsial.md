# Issue 05: Snapshot restore merusak vault & export parsial menghapus data live

Status: resolved
Severity: HIGH (kehilangan data permanen)

## Deskripsi
1. `vault_master_key` ada di `settings` dan ikut di-export/di-upsert saat import (`internal/store/snapshot.go:820`), tapi tabel `vault_secrets`, `vault_versions`, `vault_templates`, `vault_targets` tidak ikut di-export maupun di-clear (`snapshot.go:558`, `internal/store/vault.go:16`). Restore snapshot instance lain → master key tertimpa, semua secret lokal `ErrCorrupt` selamanya. Restore ke instalasi baru → vault kosong.
2. `ExportSnapshot` mengabaikan error Query/Scan/`rows.Err` (`snapshot.go:339-497`, `306-312`; `internal/store/hosts.go:165-172`). Snapshot parsial tetap di-push ke GitHub dengan status "ok"; restore kemudian `DELETE FROM ssh_keys` dsb. tanpa insert apa pun.

## Kriteria Penerimaan
- [x] Snapshot memuat baris vault terenkripsi bersama master key; import mengganti keduanya bersama. Snapshot lama tidak mengganti master key lokal.
- [x] `ExportSnapshot` mengembalikan error pada setiap Query/Scan/rows.Err yang gagal (26 titik + loop).
- [x] `ListHosts` cek `rows.Err`.
- [x] Test: `vault_snapshot_test.go` (round-trip, legacy, export gagal saat tabel tidak terbaca).
