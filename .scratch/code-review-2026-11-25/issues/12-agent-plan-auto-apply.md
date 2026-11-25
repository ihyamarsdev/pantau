# Issue 12: "Plan" dari agent bisa ter-apply lewat auto_apply

Status: open
Severity: MEDIUM

## Deskripsi
Plan agent memakai `gateRoutine` (lolos tanpa tanya di mode approval "auto") (`internal/web/agent_tools_iac.go:888-900`). `tfWantsAutoApply` hanya mengecualikan run webhook (`internal/web/terraform_events.go:91-98`) → workspace auto_apply non-produksi tanpa policy langsung apply, padahal kartu approval bilang "nothing is changed yet".

## Kriteria Penerimaan
- [ ] TriggerKind `agent` untuk run dari agent, dikecualikan dari auto-apply; ATAU gate dinaikkan ke `gateInfra` bila workspace auto_apply dan kartu menyebutkannya.
