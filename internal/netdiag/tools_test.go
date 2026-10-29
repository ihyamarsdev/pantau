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

func TestPortableTimeoutPrelude(t *testing.T) {
	if !strings.Contains(PortableTimeoutPrelude, "command -v timeout") {
		t.Errorf("expected presence check for timeout")
	}
	if !strings.Contains(PortableTimeoutPrelude, "gtimeout") {
		t.Errorf("expected fallback to gtimeout")
	}
	if !strings.Contains(PortableTimeoutPrelude, "perl") || !strings.Contains(PortableTimeoutPrelude, "alarm") {
		t.Errorf("expected perl alarm fallback")
	}
	if strings.Contains(PortableTimeoutPrelude, "&&") || strings.Contains(PortableTimeoutPrelude, "||") {
		t.Errorf("prelude must not use && or || to avoid interfering with parent scripts")
	}
}

func TestPingProbeCommand_DarwinAndLinux(t *testing.T) {
	cmd := PingProbeCommand("'1.1.1.1'")
	if !strings.Contains(cmd, PortableTimeoutPrelude) {
		t.Errorf("expected command to include PortableTimeoutPrelude")
	}
	if !strings.Contains(cmd, "uname -s") || !strings.Contains(cmd, "Darwin") {
		t.Fatalf("expected uname -s check for Darwin: %s", cmd)
	}

	// Split by Darwin branch
	parts := strings.Split(cmd, "Darwin")
	if len(parts) < 2 {
		t.Fatalf("unexpected command structure: %s", cmd)
	}
	darwinBranch := parts[1]
	if idxElse := strings.Index(darwinBranch, "else"); idxElse > 0 {
		darwinPart := darwinBranch[:idxElse]
		linuxPart := darwinBranch[idxElse:]

		// Darwin ping must use -t (deadline in seconds) and NOT -W 2
		if !strings.Contains(darwinPart, "-t 14") {
			t.Errorf("Darwin ping must use -t 14, got: %s", darwinPart)
		}
		if strings.Contains(darwinPart, "-W 2") {
			t.Errorf("Darwin ping must not use -W 2: %s", darwinPart)
		}

		// Linux ping must use -W 2
		if !strings.Contains(linuxPart, "-W 2") {
			t.Errorf("Linux ping must keep -W 2: %s", linuxPart)
		}
	} else {
		t.Fatalf("expected else in ping command: %s", cmd)
	}
}

func TestEnsureTraceTool_DarwinNoPkgManager(t *testing.T) {
	idxDarwin := strings.Index(ensureTraceToolScript, `"$(uname -s)" = "Darwin"`)
	if idxDarwin < 0 {
		t.Fatalf("ensureTraceToolScript must contain Darwin check")
	}
	idxApt := strings.Index(ensureTraceToolScript, "apt-get install")
	if idxDarwin > idxApt {
		t.Fatalf("Darwin check (%d) must precede apt-get install (%d)", idxDarwin, idxApt)
	}
	if strings.Contains(ensureTraceToolScript, "brew install") {
		t.Errorf("ensureTraceToolScript must never attempt brew installation")
	}
}

func TestDNSProbeCommand_DarwinDscacheutil(t *testing.T) {
	cmd := DNSProbeCommand("'google.com'")
	if !strings.Contains(cmd, PortableTimeoutPrelude) {
		t.Errorf("expected DNS command to include PortableTimeoutPrelude")
	}
	if !strings.Contains(cmd, "dscacheutil -q host -a name") {
		t.Errorf("expected DNS command to contain dscacheutil: %s", cmd)
	}
	if !strings.Contains(cmd, "getent hosts") {
		t.Errorf("expected DNS command to contain getent hosts: %s", cmd)
	}
}

func TestPortProbeCommand_PortableTimeout(t *testing.T) {
	cmd := PortProbeCommand("1.1.1.1", 443)
	if !strings.Contains(cmd, PortableTimeoutPrelude) {
		t.Errorf("expected Port command to include PortableTimeoutPrelude")
	}
	if !strings.Contains(cmd, "</dev/tcp/1.1.1.1/443") {
		t.Errorf("expected /dev/tcp port probe: %s", cmd)
	}
}
