# Issue 06: Terraform apply — state hilang saat gagal/cancel & double apply

Status: resolved
Severity: HIGH (resource orphan)

## Deskripsi
1. Cabang cancel/error `executeApply` mengabaikan `applyRes.State` (`internal/web/terraform_runs.go:420-438`). Plan berikutnya menimpa `terraform.tfstate` dengan salinan DB lama (`internal/terraform/plan.go:130`) → resource yang sudah dibuat tidak terlacak.
2. Setelah apply sukses, `_, _ = m.db.CreateTFStateVersion(sv)` (`terraform_runs.go:470-471`; `:448` juga abaikan error `LatestTFStateVersion`) — run tetap "applied" walau state gagal disimpan.
3. Dua apply bisa jalan di plan yang sama (double-click, dua operator, auto-apply vs manual): `StartApply` hanya menolak run *lain* yang aktif; `DiscardRun` bisa menghapus plan dir di bawah apply yang sedang build (`internal/web/handlers_terraform.go:1159-1221`, `terraform_runs.go:368-394`).

## Kriteria Penerimaan
- [x] State parsial dari apply gagal atau dibatalkan disimpan (`saveRunState`).
- [x] Error simpan state tidak diabaikan; run jadi `errored`, file state di disk tetap.
- [x] `StartApply` menolak run yang sedang apply; `DiscardRun` menolak run yang sedang apply.
- [ ] Transisi `awaiting_approval -> applying` lewat conditional DB update: BELUM. Penjagaan di memori menutup race di satu proses.
- [x] Test: `terraform_partial_state_test.go`.
