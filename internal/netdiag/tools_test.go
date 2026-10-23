package netdiag

import (
	"context"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
)

func runEnsure(t *testing.T, remoteOutput string) (error, []ProbeEvent) {
	t.Helper()
	m := sshrunner.NewMockRunner()
	m.DefaultExec = func(cmd string) (string, string, int, error) {
		return remoteOutput, "", 0, nil
	}
	var evts []ProbeEvent
	err := ensureTraceTool(context.Background(), m, "web-1", func(e ProbeEvent) { evts = append(evts, e) })
	return err, evts
}

func TestEnsureTraceToolOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr string // substring; empty means success
	}{
		{"already installed", traceToolOK + "\n", ""},
		{"installed during step", "[pantau] traceroute is not installed on this host; installing it before running the probe...\nSetting up traceroute\n" + traceToolOK + "\n", ""},
		{"cannot install but tracepath exists", "[pantau] traceroute is not installed on this host; installing it before running the probe...\n" + traceToolFallback + "\n", ""},
		{"no privileges and no tracepath", "[pantau] traceroute is not installed on this host; installing it before running the probe...\n" + traceToolNoPriv + "\n", "root or passwordless sudo"},
		{"no package manager and no tracepath", traceToolNoPkgMgr + "\n", "no supported package manager"},
		{"install failed and no tracepath", "E: Unable to locate package traceroute\n" + traceToolInstallErr + "\n", "failed"},
		{"no marker at all", "something unexpected\n", "could not verify"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err, evts := runEnsure(t, tc.output)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
			for _, e := range evts {
				if strings.Contains(e.RawLine, "PANTAU_TRACE_") {
					t.Errorf("internal marker leaked to the raw stream: %q", e.RawLine)
				}
			}
		})
	}
}

func TestEnsureTraceToolTellsOperatorWhenFallingBackToTracepath(t *testing.T) {
	err, evts := runEnsure(t, traceToolFallback+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) != 1 || !strings.Contains(evts[0].RawLine, "using tracepath") {
		t.Fatalf("expected a raw note about the tracepath fallback, got %+v", evts)
	}
}

func TestEnsureTraceToolStreamsInstallerOutput(t *testing.T) {
	err, evts := runEnsure(t, "Reading package lists...\nSetting up traceroute (2.1.0)\n"+traceToolOK+"\n")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, e := range evts {
		if e.Type == "raw" {
			lines = append(lines, e.RawLine)
		}
	}
	if len(lines) != 2 || lines[1] != "Setting up traceroute (2.1.0)" {
		t.Fatalf("expected installer output forwarded as raw lines, got %v", lines)
	}
}

func TestEnsureTraceToolScriptChecksBeforeInstalling(t *testing.T) {
	idxCheck := strings.Index(ensureTraceToolScript, "command -v traceroute")
	idxInstall := strings.Index(ensureTraceToolScript, "apt-get install")
	if idxCheck < 0 || idxInstall < 0 || idxCheck > idxInstall {
		t.Fatalf("presence check must precede installation (check=%d install=%d)", idxCheck, idxInstall)
	}
	if !strings.Contains(ensureTraceToolScript, "sudo -n") {
		t.Errorf("non-root installs must use non-interactive sudo only")
	}
}

func TestTraceProbeCommandPrefersTracerouteAndStreamsTracepath(t *testing.T) {
	cmd := traceProbeCommand("'1.1.1.1'")
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, "||") {
		t.Fatalf("probe command must use if/elif, not && / ||, got %s", cmd)
	}
	iTraceroute := strings.Index(cmd, "timeout 15 traceroute")
	iTracepath := strings.Index(cmd, "tracepath -n")
	if iTraceroute < 0 || iTracepath < 0 || iTraceroute > iTracepath {
		t.Fatalf("traceroute must be preferred over tracepath: %s", cmd)
	}
	// tracepath block-buffers a pipe; when the deadline kills it every hop is lost unless it is line-buffered.
	if !strings.Contains(cmd, "stdbuf -oL tracepath") {
		t.Errorf("tracepath must run line-buffered via stdbuf when available: %s", cmd)
	}
}
