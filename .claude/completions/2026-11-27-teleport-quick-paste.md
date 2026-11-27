# Teleport Profiles: Quick Paste of a raw `tsh login` command

## What changed
- `internal/teleport/command.go`: `ParseLoginCommand` (pure Go, no shell). Maps `--proxy`, `--auth`, `--user`/`-u`,
  positional or `--cluster` cluster, and `TELEPORT_HOME` (prefix or `export`) into a `Profile`. Every result passes
  `ValidateProfile`. Shell syntax on the tsh line is refused. Safe Teleport flags are reported as `IgnoredFlags`.
- `POST /api/teleport/profiles/parse-command` (`handlers_teleport.go`, route in `server.go`): returns the mapping,
  `ignored_flags` and `warnings`. The raw command is never echoed or logged.
- `12-teleport.js`: Quick Paste textarea above the profile form. A client parser mirrors the Go rules. It fills
  Proxy, Auth connector, Username, Teleport cluster and TELEPORT_HOME after a 150 ms debounce, and flashes each field.
  Name is filled only while the user has not edited it. Default policy is never touched. Parse errors show inline and
  leave fields alone. EN and ID strings added to `00-state.js`.

## Not done
- `raw_command` on `POST /api/teleport/profiles` was not added. The dedicated parse endpoint covers the need.

## Verification
- `make check` exit 0 (vet, `test -race`, govulncheck, gosec).
- `FuzzParseLoginCommand` ran 5 s with no panic.
- `node --check` on both changed JS modules. JS parser parity checked against the Go table (8 accept, 28 reject).
- `e2e/tests/teleport-switcher.e2e.ts`: 4/4 pass, including the new paste-and-save case.
