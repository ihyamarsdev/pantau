# 06 - Git playbook source

Status: resolved

Playbook `source` = `inline` | `git` (URL https/ssh/scp-like, branch/tag, path, auth none/token/SSH Key Library). Shallow clone per run into the 0700 workspace, `ansible-playbook` runs from the clone, repo `ansible.cfg` ignored, commit recorded on the run.

See [ADR 0050](../../../docs/adr/0050-ansible-git-playbooks-and-cron-schedules.md).

## Answer

- `internal/ansible/git.go` (validation, `cloneGit`), `runCmd` shared with the ansible process; `internal/web/ansible_playbook_req.go`; store columns via idempotent `ALTER TABLE`; UI source selector + git fields; `commit_sha` in run history.
- Verified: real `git clone` from a local repo (branch, symlink escape, protocol allowlist), `Run` with fake git/ansible binaries (cwd, token only via env, workspace removed), API validation matrix, real server smoke with an unreachable repo (clean failure, token absent from logs).
