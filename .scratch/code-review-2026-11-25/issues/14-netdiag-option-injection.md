# Issue 14: Option injection pada target netdiag

Status: open
Severity: MEDIUM

## Deskripsi
`validTargetRegex` `^[a-zA-Z0-9_.:-]+$` mengizinkan awalan `-` (`internal/netdiag/service.go:21`, dipakai :193, :224); port probe menyisipkan target tanpa quote (:119). Contoh: dig dengan target `-f.netrc` + DNS server penyerang → isi `~/.netrc` bocor sebagai query DNS.

## Kriteria Penerimaan
- [ ] `ValidateTarget` menolak awalan `-` (juga untuk DNS server).
- [ ] `--` sebelum operand bila tool mendukung; target port probe di-quote.
