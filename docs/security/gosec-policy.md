# gosec policy

`make sec` runs `govulncheck ./...` and then `gosec` with `-severity medium -confidence medium`. The build fails on any
remaining finding, so a new Medium/High issue cannot slip in silently.

## Excluded rules (accepted by design)

Pantau is an SSH, Ansible, Terraform and Git management tool. Its job is to run fixed binaries with administrator-
configured arguments and to read and write files inside administrator-configured directories. These rules flag that
purpose everywhere, so they are excluded globally rather than suppressed line by line.

| Rule | Why it is excluded | What still guards the risk |
| --- | --- | --- |
| G204 subprocess with variable arguments | ansible, terraform, git, ssh and gwupdate run fixed binaries with admin-configured args | G702 (command injection taint) stays enabled; git option injection is blocked with `--` and a leading-`-` check |
| G304 file read via variable path | Paths come from admin-configured library, workspace and repo dirs | Path inputs are validated at the handlers that accept them |
| G301 / G302 / G306 file permissions | Repo and work files need group/world read; work dirs are chmod 0700; secrets are written 0600 | Secrets (keys, vault, snapshots) are written 0600 and covered by tests |

## Rules kept enabled and how each finding was resolved

- **G710 open redirect** (`handlers_tunnel.go`): the first redirect is a relative path under the fixed `/api/tunnel` prefix.
  The second now refuses any Host header that is neither `localhost` nor an IP literal. Both are annotated.
- **G704 SSRF** (`globe/resolver.go`): `httpLookup` rejects anything that is not an IP literal before building either
  lookup URL. The API key is query-escaped.
- **G702 command injection** (`handlers_terraform.go`): the git URL and branch are rejected if they start with `-`, and
  `--` ends option parsing before the URL. Without this, `--upload-pack=<cmd>` would run a command.
- **G702** (`updater.go`): `os.Executable()`, the running binary itself.
- **G108 / G114 pprof** (`main.go`): the blank import is opt-in (`-pprof`), bound to `127.0.0.1`, and served from its own
  `http.Server` with a read-header timeout. The main server never uses `DefaultServeMux`.
- **G124 cookies**: session and tunnel cookies set `Secure` when the request arrived over TLS (directly or via
  `X-Forwarded-Proto`), so plain-HTTP LAN installs keep working.
- **G705 XSS**: each remaining site writes gzip, an attachment, or a static asset with a fixed content type.
- **G118 / G122**: a background updater goroutine that must outlive requests, and filesystem reads inside admin-owned
  trees.
- **G202, G115, G404, G505**: a constant table list, a non-negative TOTP step, retry jitter, and RFC 6238's default SHA-1.

Each inline suppression has the form `// #nosec <rule> -- <reason>` so a reviewer can check the reason at the line.
