# Agent IaC tools, milestone 1 (H1)

## What changed

- **Create echoes its metadata.** `tf_workspace_action` create now answers with `description`, `group_name` and `tags` too. The store and HTTP handler already saved them (checked: the INSERT's 28 columns and values match). The answer had omitted them, so the model had no evidence of the save and tended to send a follow-up `update`.
- **`tf_workspace_scaffold`** (new). Creates or rewrites an inline workspace with all files, upserts variables by key, and with `validate: true` runs the existing validate endpoint. One approval covers the batch. Refused before the gate: traversal or absolute file names (`terraform.ValidateFiles`), a sensitive variable that is not a `{{ vault.KEY }}` reference, a disabled Terraform gate, a locked workspace.
- **`tf_catalog` templates** is one compact index (`id, name, provider, category, summary, tags`, under 3 KB for the built-in set, optional `query` filter). `template_id` returns the files of one template.
- **Trace accuracy.** `riskTierOfTool` classifies IaC tools per action (`iacReadActions`, `iacWriteTools`). Reads are `read_only`, writes are `mutating_safe`, `tf_run_action` apply/destroy are `destructive`. Before, `tf_workspace_action`, `tf_workspace_data` and `tf_catalog` mutations showed as `read_only`.
- **Parallel read-only calls.** `runAgentTurn` plans every call first (events and guard in call order), then runs each run of read-only calls concurrently, at most 4 at once, and appends results in call order. Mutating calls run alone. `set_tool_domain` never runs in parallel because it writes the shared domain pointer. A panicking tool in a worker becomes an error result.

## Decisions

- The trace keeps the existing tier vocabulary (`read_only`, `mutating_safe`, `destructive`). Introducing `write` / `approval_required` would change the enum the UI and the inspector use, so it is left for a separate change.
- Template `category` is the provider, and `tags` is `[provider]`. The template data has no category or tag fields.
- `tf_workspace_scaffold` is classed `mutating_safe` in the trace, the same as other writes. Approval gating is unchanged: `gateInfra` and the approval card.

## Verification

- `go vet ./...` clean. `make sec` exit 0 (govulncheck: no vulnerabilities, gosec: 0 issues).
- `go test -race -count=1 ./...`: 2658 passed, 26 packages.
- New tests: create metadata round trip; scaffold refusals (traversal, absolute, plaintext secret, gate off) asserted with zero pending approvals; scaffold happy path with a fake `terraform` on `PATH` (one approval, 4 files, 2 variables, metadata stored); tf_catalog index size, shape, and detail mode without approval; tier table per action; parallel batch with a barrier (three read-only tools must run at once, results in call order); cancel observed after a batch returns refuses every call.
- `TestAgentLoopMidBatchCancelClosesTheBatch` now accepts "the second call ran" as well as "not run", because both calls are read-only and form one parallel segment. The test still checks that every call gets a result and the history stays valid.

## Not done / caveats

- Milestones 2 (plan-and-execute, `delegate_investigate`) and 3 (FTS5 runbooks, distill, custom skills) were not started.
- No JS changed in this milestone, so `node --check` and the E2E specs were not run.
- The new approval-path tests reuse the existing `iacApprove` helper, which polls with `time.Sleep`. The repo's helper predates this work.
- Not reproduced live: the reported "create drops description/environment" bug. Store and handler are correct; the response omission is the likely cause.
