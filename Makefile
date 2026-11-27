.PHONY: vet test test-fast sec build check install-hooks

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOBIN_DIR := $(shell go env GOPATH)/bin
GOVULNCHECK := $(shell command -v govulncheck 2>/dev/null || echo $(GOBIN_DIR)/govulncheck)
GOSEC := $(shell command -v gosec 2>/dev/null || echo $(GOBIN_DIR)/gosec)

vet:
	go vet ./...

test:
	go test -race -count=1 ./...

# Fast local iteration: pure-Go SQLite is slow under the race detector.
test-fast:
	go test -count=1 ./...

# gosec policy: report Medium+ severity at Medium+ confidence. Excluded rules are accepted by design for an
# SSH/Ansible/Terraform management tool (see docs/security/gosec-policy.md for the per-rule rationale):
#   G204 subprocess with variable args  - ansible/terraform/git/ssh are run with admin-configured args, fixed binaries
#   G304 file read via variable path    - paths come from admin-configured library/workspace dirs
#   G301/G302/G306 file permissions     - repo/work files need group/world read; secrets are written 0600 separately
# Everything else stays enabled, including G702 command injection, G704 SSRF, G705 XSS, G710 open redirect and crypto rules.
GOSEC_FLAGS := -exclude-generated -severity medium -confidence medium -exclude G204,G304,G301,G302,G306

sec:
	@test -x "$(GOVULNCHECK)" || { echo "govulncheck not found. Install: go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	@test -x "$(GOSEC)" || { echo "gosec not found. Install: go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	$(GOVULNCHECK) ./...
	$(GOSEC) $(GOSEC_FLAGS) ./...

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o pantau .

check: vet test sec

install-hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks enabled from .githooks/"
