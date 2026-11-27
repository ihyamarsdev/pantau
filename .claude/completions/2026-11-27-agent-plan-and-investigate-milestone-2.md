# Agent plan-and-execute and delegated investigation, milestone 2 (H2)

## What changed

- **`agent_propose_plan`** (tool). Stores a plan in memory: a goal, steps typed `inspect` (read), `mutate` (change) or `verify` (read after a change), and a `rollback_plan` of mutate steps. Steps are checked before the card is shown: unknown tools, bad args, destructive tools, a mutate step with a read tool, a read step with a write tool, and nested plan/domain tools are refused. At most 12 steps per list.
- **Single batch approval.** `POST /api/agent/plans/{id}/approve` is the one click. While a step runs, its context carries a `planGrant` for that step's tool only; `agentGateAs` passes that one tool and asks again for anything else. Destructive steps are never in a plan.
- **Verify and rollback.** Steps run in order. A failed inspect or mutate ends the plan as `failed`; a failed verify ends it as `verify_failed`. `POST {id}/rollback` (a second click) runs the rollback steps, only from `verify_failed` or `failed`.
- **Stop.** `POST {id}/cancel` cancels the running execution's context. Reject is the same call on a proposed plan.
- **`delegate_investigate`** (tool). 2 to 4 investigations, one per domain, run in goroutines. Each has its own conversation on the fast model (primary when none is set), at most 4 tool calls, and one shared 20 s timeout. Tools are structurally read-only: `investigatorAllowed` takes a read-verb name (`list`, `get`, `logs`, `status`, ...) with no write word (`action`, `run`, `apply`, `save`, ...), and every call is re-checked at run time. The summary is redacted with `agentRedactText` before it returns.
- **UI.** The chat shows a plan card after the propose tool's done event. It lists steps with status, and offers Approve & Execute / Reject while proposed, Stop while running, Run Rollback after a failure. The card polls while the plan runs. The investigation indicator is the tool label: "Investigating 3 domains in parallel: host, docker, terraform".

## Decisions

- Plans live in memory, like pending approvals. A restart drops them, and a reloaded chat does not show old cards. Capped at 50, finished plans dropped first.
- Verify runs as the plan lists it, after the change it follows. There is no separate auto-verify hook.
- The investigator read rule is a heuristic (name verbs and write words), backed by a test that no write-named tool gets through. It is stricter than the tool's own tier, so a read tool with an unusual name is left out, not allowed by mistake.
- `alert_ack`, `ssh_batch_inspect`, `exec_command`, `file_action` and `memory_*` are excluded from investigators outright.

## Verification

- `go vet ./...` clean; `gofmt` clean on touched files; `node --check` on `00-agent.js` OK.
- `go test -race -count=1 ./internal/web/`: 861 passed. The rest of the repo passed in the full `go test -race ./...` run, and only `internal/web` changed since.
- `make sec`: exit 0, no vulnerabilities, gosec 0 issues.
- New tests: transition table; step rules table (12 cases); one approval covers the write, with no second card; verify failure leaves the rollback unrun until the click, then runs it; cancel mid-flight; state-checked actions and HTTP surface; grant covers only its own tool; investigators run concurrently (barrier); read-only and refused write; per-investigator call budget; timeout; validation table; allowlist property over every registered tool; summary redaction.

## Not done / caveats

- Milestone 3 not started.
- No E2E spec for the plan card (the spec file list is M3's). The card's click paths have no browser test yet.
- `TestDelegateInvestigateTimeout` changes the global `investigatorTimeout`, so it is not parallel.
- Plan tests add fake tools to the shared list and the write map, restored in cleanup; they are not parallel for that reason.
