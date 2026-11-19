package inspector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- Host fingerprint formatting ---------------------------------------------------------------

func TestFormatHostFingerprint(t *testing.T) {
	cases := []struct {
		name string
		fp   HostFingerprint
		want []string
	}{
		{
			name: "linux systemd apt",
			fp:   HostFingerprint{OSFamily: "linux", Distro: "Ubuntu 22.04 LTS", Arch: "x86_64", InitSystem: "systemd", PackageManager: "apt", SudoCapability: "sudo -n (passwordless, ADR 0037)", NetworkState: "direct"},
			want: []string{"os=linux", `distro="Ubuntu 22.04 LTS"`, "arch=x86_64", "init=systemd", "pkg=apt", "net=direct"},
		},
		{
			name: "macos launchd brew",
			fp:   HostFingerprint{OSFamily: "darwin", Distro: "macOS 26.2 (Tahoe)", Arch: "arm64", InitSystem: "launchd", PackageManager: "brew", NetworkState: "direct"},
			want: []string{"os=darwin", "arch=arm64", "init=launchd", "pkg=brew"},
		},
		{
			name: "legacy alpine openrc",
			fp:   HostFingerprint{OSFamily: "linux", Distro: "Alpine 3.18", Arch: "x86_64", InitSystem: "openrc", PackageManager: "apk", NetworkState: "direct"},
			want: []string{"init=openrc", "pkg=apk"},
		},
		{
			name: "vpn down fast fail",
			fp:   HostFingerprint{OSFamily: "linux", Distro: "Debian 12", Arch: "x86_64", InitSystem: "systemd", PackageManager: "apt", NetworkID: 7, NetworkName: "netbird", NetworkState: "down"},
			want: []string{"net=netbird:down", "VPN DOWN"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FormatHostFingerprint(c.fp)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("fingerprint %q missing %q", got, w)
				}
			}
		})
	}
}

func TestFingerprintDerivation(t *testing.T) {
	if got := initFromFamily("darwin", "macos", ""); got != "launchd" {
		t.Errorf("macOS init = %q, want launchd", got)
	}
	if got := initFromFamily("linux", "alpine", "alpine 3.18"); got != "openrc" {
		t.Errorf("Alpine init = %q, want openrc", got)
	}
	if got := initFromFamily("linux", "rhel", "CentOS release 6.10"); got != "sysv" {
		t.Errorf("CentOS 6 init = %q, want sysv", got)
	}
	if got := normalizePackageManager("softwareupdate", "macos", "darwin"); got != "brew" {
		t.Errorf("macOS package manager = %q, want brew", got)
	}
	if got := archFromMetadata("Apple M3 Ultra", ""); got != "arm64" {
		t.Errorf("Apple Silicon arch = %q, want arm64", got)
	}
}

// --- Interactive command blocking ---------------------------------------------------------------

func TestLintInteractiveCommand(t *testing.T) {
	blocked := []string{
		"vim /etc/hosts", "vi /etc/hosts", "nano /etc/hosts",
		"less /var/log/syslog", "more /var/log/syslog", "man ssh",
		"htop", "top", "top -p 123", "sudo less /etc/shadow", "cat x | less",
	}
	for _, cmd := range blocked {
		if hint := LintInteractiveCommand(cmd); hint == "" {
			t.Errorf("%q must be refused, got no hint", cmd)
		} else if !strings.Contains(hint, "TTY") && !strings.Contains(hint, "pager") && !strings.Contains(hint, "interactive") {
			t.Errorf("hint for %q must be actionable: %q", cmd, hint)
		}
	}
	allowed := []string{
		"uptime", "top -b -n 1 | head -30", "top -l 1",
		"echo top", "cat /etc/hosts", "docker logs --tail 50 web", "ps aux | grep top",
	}
	for _, cmd := range allowed {
		if hint := LintInteractiveCommand(cmd); hint != "" {
			t.Errorf("%q must be allowed, got: %s", cmd, hint)
		}
	}
}

func TestSudoNoInteractive(t *testing.T) {
	cases := map[string]string{
		"sudo systemctl restart nginx": "sudo -n systemctl restart nginx",
		"sudo -n true":                 "sudo -n true",
		"echo sudo here":               "echo sudo here",
		"cmd; sudo cat /etc/shadow":    "cmd; sudo -n cat /etc/shadow",
	}
	for in, want := range cases {
		if got := sudoNoInteractive(in); got != want {
			t.Errorf("sudoNoInteractive(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- Exit-code hints ----------------------------------------------------------------------------

func TestExitHint(t *testing.T) {
	cases := []struct {
		code int
		err  error
		want string
	}{
		{127, nil, "command not found"},
		{126, nil, "permission denied"},
		{124, nil, "timed out"},
		{1, nil, "command failed"},
		{-1, context.DeadlineExceeded, "deadline"},
	}
	for _, c := range cases {
		got := exitHint(c.code, c.err, "some-command")
		if !strings.Contains(strings.ToLower(got), c.want) {
			t.Errorf("exitHint(%d, %v) = %q, want to contain %q", c.code, c.err, got, c.want)
		}
	}
}

// --- Log dedup / truncation ---------------------------------------------------------------------

func TestCompressAgentOutput(t *testing.T) {
	input := "line A\nline A\nline A\nline B\nline B\nAPI_TOKEN=supersecret123\n"
	got := CompressAgentOutput(input, 3000, 50)
	if !strings.Contains(got, "[repeated 3x] line A") {
		t.Errorf("3x duplicate must collapse: %q", got)
	}
	if !strings.Contains(got, "[repeated 2x] line B") {
		t.Errorf("2x duplicate must collapse: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") || strings.Contains(got, "supersecret123") {
		t.Errorf("secret must be redacted: %q", got)
	}
	if !strings.Contains(got, "lines") || !strings.Contains(got, "bytes") {
		t.Errorf("metadata must be present: %q", got)
	}
}

func TestCompressAgentOutputTruncates(t *testing.T) {
	var sb strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&sb, "unique-line-%04d\n", i)
	}
	got := CompressAgentOutput(sb.String(), 500, 50)
	if len(got) > 700 {
		t.Errorf("compressed output must stay near cap: %d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation must be announced: %q", got[:200])
	}
}

// --- cwd state tracking -------------------------------------------------------------------------

func TestAgentSessionStateCWD(t *testing.T) {
	st := NewAgentSessionState()
	if got := st.GetCWD(1); got != "" {
		t.Errorf("new session cwd = %q, want empty", got)
	}
	if err := st.SetCWD(1, "/var/log"); err != nil {
		t.Fatalf("SetCWD: %v", err)
	}
	if got := st.GetCWD(1); got != "/var/log" {
		t.Errorf("cwd = %q, want /var/log", got)
	}
	if got := st.GetCWD(2); got != "" {
		t.Errorf("cwd must be per host: host 2 = %q", got)
	}
	if err := st.SetCWD(1, "relative/path"); err == nil {
		t.Error("relative cwd must be refused")
	}
	st.ClearCWD(1)
	if got := st.GetCWD(1); got != "" {
		t.Errorf("cleared cwd = %q", got)
	}
}

func TestExtractCD(t *testing.T) {
	cases := map[string]string{
		"cd /var/log && ls": "cd /var/log && ls",
		"cd '/var/log'":     "cd '/var/log'",
	}
	for in := range cases {
		if got := extractCD(in); got == "" {
			t.Errorf("extractCD(%q) returned empty", in)
		}
	}
	if got := extractCD("uptime"); got != "" {
		t.Errorf("non-cd command must return empty, got %q", got)
	}
}

func TestCwdPrefix(t *testing.T) {
	st := NewAgentSessionState()
	_ = st.SetCWD(1, "/var/log")
	prefix := st.cwdPrefix(1)
	if !strings.Contains(prefix, "cd ") || !strings.Contains(prefix, "/var/log") {
		t.Errorf("cwd prefix must cd into the stored dir: %q", prefix)
	}
	if got := st.cwdPrefix(2); got != "" {
		t.Errorf("no cwd must give empty prefix, got %q", got)
	}
}

// --- Batch script build/parse --------------------------------------------------------------------

func TestBatchScriptRoundTrip(t *testing.T) {
	script, names := BuildBatchScript([]string{"uptime", "df -h"}, []string{"LOAD", "DISK"})
	if !strings.Contains(script, "uptime") || !strings.Contains(script, "df -h") {
		t.Fatalf("script must contain both commands: %q", script)
	}
	out := "\n===PB9F3A_BEGIN_LOAD===\n 12:00 up 1 day\n===PB9F3A_END_LOAD_RC=0===\n" +
		"\n===PB9F3A_BEGIN_DISK===\n/dev/sda1 80%\n===PB9F3A_END_DISK_RC=1===\n"
	sections := ParseBatchOutput(out)
	if len(sections) != 2 {
		t.Fatalf("parsed %d sections, want 2: %+v", len(sections), sections)
	}
	if sections["LOAD"].ExitCode != 0 || !strings.Contains(sections["LOAD"].Stdout, "up 1 day") {
		t.Errorf("LOAD section wrong: %+v", sections["LOAD"])
	}
	if sections["DISK"].ExitCode != 1 || !strings.Contains(sections["DISK"].Stdout, "80%") {
		t.Errorf("DISK section wrong: %+v", sections["DISK"])
	}
	if len(names) != 2 || names[0] != "LOAD" || names[1] != "DISK" {
		t.Errorf("names = %v", names)
	}
}

// --- VPN fast-fail (no DB) ----------------------------------------------------------------------

func TestCheckVPNDownNoDB(t *testing.T) {
	ins := &Inspector{}
	if err := ins.checkVPNDown(1); err != nil {
		t.Errorf("no DB must be a no-op, got %v", err)
	}
}

func TestVPNDownError(t *testing.T) {
	err := &vpnDownError{Name: "netbird", State: "down"}
	if !strings.Contains(err.Error(), "netbird") || !strings.Contains(err.Error(), "down") {
		t.Errorf("diagnostic must name the network and state: %q", err.Error())
	}
}

// --- Destructive gate still intact after refactor -----------------------------------------------

func TestExecShellContextDestructiveStillGated(t *testing.T) {
	ins := &Inspector{}
	_, _, _, err := ins.ExecShellContext(context.Background(), 1, "rm -rf /var/log", time.Second, DefaultAgentExecLimits())
	if err == nil || !strings.Contains(err.Error(), "explicit operator approval") {
		t.Fatalf("destructive command must be blocked: %v", err)
	}
}

func TestInteractiveCommandRefusedBeforeDial(t *testing.T) {
	ins := &Inspector{} // no runner: the lint must fire first
	_, _, _, err := ins.ExecShellContext(context.Background(), 1, "vim /etc/hosts", time.Second, DefaultAgentExecLimits())
	if err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Fatalf("interactive command must be refused pre-dial: %v", err)
	}
}
