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

sec:
	@test -x "$(GOVULNCHECK)" || { echo "govulncheck not found. Install: go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	@test -x "$(GOSEC)" || { echo "gosec not found. Install: go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	$(GOVULNCHECK) ./...
	$(GOSEC) -exclude-generated ./...

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o pantau .

check: vet test sec

install-hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks enabled from .githooks/"
