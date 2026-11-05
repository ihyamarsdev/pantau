# 07 - Cron schedules

Status: resolved

`ansible_schedules` (playbook + 5-field cron + fixed targets + run options + enabled). One scheduler goroutine (15 s tick) fires due schedules through the same launcher as manual runs; missed occurrences (> 5 min late) are skipped; busy playbook / run limit -> recorded as `not started`, not queued.

See [ADR 0050](../../../docs/adr/0050-ansible-git-playbooks-and-cron-schedules.md).

## Answer

- `internal/ansible/cron.go` (parser + `Next`, table-tested incl. leap day and Vixie day semantics), `internal/store/ansible_schedules.go`, `internal/web/handlers_ansible_schedules.go`, `internal/web/ansible_scheduler.go`, `StartAnsibleScheduler(ctx)` from `main.go`; UI schedules table + dialog.
- Verified: unit tests for tick (due / missed / future / fresh / disabled / busy / gate off), API validation matrix, and a real server run where a `* * * * *` syntax-mode schedule fired a real `ansible-playbook` run (`trigger=schedule`).

## Not done

Month/weekday names in cron, per-schedule time zone, a Repository entity shared between playbooks, Git webhooks.
