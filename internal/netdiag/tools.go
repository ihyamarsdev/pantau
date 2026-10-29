package netdiag

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"pantau/internal/sshrunner"
)

const (
	traceToolOK         = "PANTAU_TRACE_TOOL_OK"
	traceToolFallback   = "PANTAU_TRACE_FALLBACK"
	traceToolNoPriv     = "PANTAU_TRACE_NO_PRIV"
	traceToolNoPkgMgr   = "PANTAU_TRACE_NO_PKGMGR"
	traceToolInstallErr = "PANTAU_TRACE_INSTALL_FAILED"

	// traceInstallTimeout bounds the whole ensure/install step (package index refresh + download + install).
	traceInstallTimeout = 150 * time.Second
)

// PortableTimeoutPrelude defines a timeout function for platforms where GNU timeout
// is not installed (e.g. macOS / Darwin). It prefers timeout, falls back to gtimeout,
// then wraps via perl's alarm function, and finally falls back to running the command directly.
// It avoids using '&&' or '||' so callers asserting if/elif structure remain valid.
const PortableTimeoutPrelude = `if ! command -v timeout >/dev/null 2>&1; then ` +
	`if command -v gtimeout >/dev/null 2>&1; then ` +
	`timeout() { gtimeout "$@"; }; ` +
	`elif command -v perl >/dev/null 2>&1; then ` +
	`timeout() { perl -e 'my $d = shift; alarm($d); exec @ARGV' "$@"; }; ` +
	`else ` +
	`timeout() { shift; "$@"; }; ` +
	`fi; ` +
	`fi; `

// ensureTraceToolScript is a POSIX sh script run on the source host before a traceroute probe.
//
// traceroute is the preferred tool: it streams one line per hop through a pipe. tracepath (iputils) block-buffers
// stdout when piped, so when the probe deadline kills it nothing at all reaches Pantau. The script therefore
// installs traceroute when it is missing (root or passwordless sudo only, never a password prompt). If it cannot
// be installed but tracepath exists, the probe falls back to tracepath; if neither exists the caller gets an
// actionable error. Markers are consumed by ensureTraceTool and never forwarded to the UI.
var ensureTraceToolScript = `
fallback_or_fail() {
  if command -v tracepath >/dev/null 2>&1; then
    echo ` + traceToolFallback + `
    exit 0
  fi
  echo "$1"
  exit "$2"
}
if command -v traceroute >/dev/null 2>&1; then
  echo ` + traceToolOK + `
  exit 0
fi
if [ "$(uname -s)" = "Darwin" ]; then
  fallback_or_fail ` + traceToolNoPkgMgr + ` 4
fi
echo "[pantau] traceroute is not installed on this host; installing it before running the probe..."
if [ "$(id -u)" = "0" ]; then
  SUDO=""
elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
  SUDO="sudo -n"
else
  fallback_or_fail ` + traceToolNoPriv + ` 3
fi
if command -v apt-get >/dev/null 2>&1; then
  $SUDO env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y traceroute 2>&1 || {
    echo "[pantau] package index may be stale; refreshing and retrying..."
    $SUDO apt-get update 2>&1 && $SUDO env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y traceroute 2>&1
  }
elif command -v dnf >/dev/null 2>&1; then
  $SUDO dnf install -y traceroute 2>&1
elif command -v yum >/dev/null 2>&1; then
  $SUDO yum install -y traceroute 2>&1
elif command -v apk >/dev/null 2>&1; then
  $SUDO apk add --no-cache traceroute 2>&1
elif command -v pacman >/dev/null 2>&1; then
  $SUDO pacman -S --noconfirm --needed traceroute 2>&1
elif command -v zypper >/dev/null 2>&1; then
  $SUDO zypper --non-interactive install traceroute 2>&1
else
  fallback_or_fail ` + traceToolNoPkgMgr + ` 4
fi
if command -v traceroute >/dev/null 2>&1; then
  echo ` + traceToolOK + `
  exit 0
fi
fallback_or_fail ` + traceToolInstallErr + ` 5
`

// traceProbeCommand builds the traceroute probe. traceroute is used whenever present. tracepath is the fallback and
// is wrapped in `stdbuf -oL` (when available) so hops stream line by line instead of being lost in a full pipe buffer
// if the 15s deadline kills it. An explicit if/elif replaces the old `a && b || c` chain, which retried a missing
// traceroute whenever tracepath merely exited non-zero.
func traceProbeCommand(quotedTarget string) string {
	script := fmt.Sprintf(
		`if command -v traceroute >/dev/null 2>&1; then timeout 15 traceroute -n -m 20 -q 1 -w 1 %[1]s 2>&1; `+
			`elif command -v tracepath >/dev/null 2>&1; then `+
			`if command -v stdbuf >/dev/null 2>&1; then timeout 15 stdbuf -oL tracepath -n -m 20 %[1]s 2>&1; `+
			`else timeout 15 tracepath -n -m 20 %[1]s 2>&1; fi; `+
			`else echo "no traceroute tool available"; fi`, quotedTarget)
	return "sh -c " + shellQuote(PortableTimeoutPrelude+script)
}

// TraceProbeCommand exports traceProbeCommand for test assertions.
func TraceProbeCommand(quotedTarget string) string {
	return traceProbeCommand(quotedTarget)
}

// ensureTraceTool makes sure a traceroute-capable tool exists on the remote host, installing traceroute first when
// it is missing. Installer output is streamed to eventSink as raw lines; markers are not forwarded.
func ensureTraceTool(ctx context.Context, runner sshrunner.Runner, hostName string, eventSink func(ProbeEvent)) error {
	ctx, cancel := context.WithTimeout(ctx, traceInstallTimeout)
	defer cancel()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = runner.Close()
		case <-done:
		}
	}()

	var ok, fallback, noPriv, noPkgMgr, installErr bool
	pr, pw := io.Pipe()
	runErr := make(chan error, 1)
	go func() {
		err := runner.Stream("sh -c "+shellQuote(ensureTraceToolScript), pw)
		_ = pw.CloseWithError(err)
		runErr <- err
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case traceToolOK:
			ok = true
		case traceToolFallback:
			fallback = true
		case traceToolNoPriv:
			noPriv = true
		case traceToolNoPkgMgr:
			noPkgMgr = true
		case traceToolInstallErr:
			installErr = true
		default:
			if line != "" {
				eventSink(ProbeEvent{Type: "raw", RawLine: line})
			}
		}
	}
	<-runErr

	switch {
	case ok:
		return nil
	case fallback:
		eventSink(ProbeEvent{Type: "raw", RawLine: "[pantau] traceroute could not be installed (root or passwordless sudo needed); using tracepath instead"})
		return nil
	case ctx.Err() != nil:
		return fmt.Errorf("installing traceroute on '%s' timed out after %s", hostName, traceInstallTimeout)
	case noPriv:
		return fmt.Errorf("traceroute is not installed on '%s' and the SSH user cannot install it (root or passwordless sudo required); install it manually, e.g. `apt install traceroute` or `dnf install traceroute`", hostName)
	case noPkgMgr:
		return fmt.Errorf("traceroute is not installed on '%s' and no supported package manager (apt, dnf, yum, apk, pacman, zypper) was found; install it manually", hostName)
	case installErr:
		return fmt.Errorf("installing traceroute on '%s' failed; see the raw output above", hostName)
	}
	return errors.New("could not verify traceroute availability on '" + hostName + "'")
}
