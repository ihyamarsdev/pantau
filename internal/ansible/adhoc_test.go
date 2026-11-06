package ansible

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdhocSpecValidate(t *testing.T) {
	targets := []Target{{ID: 1, Name: "web-1", Address: "10.0.0.1"}}
	valid := AdhocSpec{Module: "ping", Targets: targets}

	tests := []struct {
		name   string
		spec   AdhocSpec
		errSub string
	}{
		{name: "valid minimal", spec: valid},
		{name: "valid qualified module", spec: AdhocSpec{Module: "ansible.builtin.shell", Args: "uptime", Targets: targets}},
		{name: "module starting with dash", spec: AdhocSpec{Module: "--foo", Targets: targets}, errSub: "invalid module"},
		{name: "module with space", spec: AdhocSpec{Module: "ping -m other", Targets: targets}, errSub: "invalid module"},
		{name: "module empty", spec: AdhocSpec{Targets: targets}, errSub: "invalid module"},
		{name: "module too long", spec: AdhocSpec{Module: strings.Repeat("a", 81), Targets: targets}, errSub: "invalid module"},
		{name: "args with NUL", spec: AdhocSpec{Module: "shell", Args: "echo a\x00b", Targets: targets}, errSub: "NUL"},
		{name: "args with newline", spec: AdhocSpec{Module: "shell", Args: "echo a\nrm -rf /", Targets: targets}, errSub: "line breaks"},
		{name: "args too long", spec: AdhocSpec{Module: "shell", Args: strings.Repeat("a", 4097), Targets: targets}, errSub: "4 KiB"},
		{name: "pattern with semicolon", spec: AdhocSpec{Module: "ping", Pattern: "web-1;rm", Targets: targets}, errSub: "invalid limit"},
		{name: "pattern with space", spec: AdhocSpec{Module: "ping", Pattern: "web 1", Targets: targets}, errSub: "invalid limit"},
		{name: "pattern with backtick", spec: AdhocSpec{Module: "ping", Pattern: "web`id`", Targets: targets}, errSub: "invalid limit"},
		{name: "valid pattern", spec: AdhocSpec{Module: "ping", Pattern: "webservers:!db-1", Targets: targets}},
		{name: "no targets", spec: AdhocSpec{Module: "ping"}, errSub: "at least one target"},
		{name: "too many targets", spec: AdhocSpec{Module: "ping", Targets: make([]Target, 201)}, errSub: "maximum of 200"},
		{name: "verbosity 5", spec: AdhocSpec{Module: "ping", Targets: targets, Verbosity: 5}, errSub: "verbosity"},
		{name: "negative forks", spec: AdhocSpec{Module: "ping", Targets: targets, Forks: -1}, errSub: "forks"},
		{name: "extra vars not json", spec: AdhocSpec{Module: "ping", Targets: targets, ExtraVars: "{oops"}, errSub: "extra_vars"},
		{name: "invalid config", spec: AdhocSpec{Module: "ping", Targets: targets, Config: "[inventory]\nlist = x\n"}, errSub: "config"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if tc.errSub == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.errSub)
			}
		})
	}
}

func TestParseAdhocRealPing(t *testing.T) {
	got := ParseAdhoc(adhocPingOutput)
	want := []AdhocResult{{Alias: "web-1", Status: AdhocSuccess}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ParseAdhoc = %+v, want %+v", got, want)
	}
}

func TestParseAdhocRealShellAndFailure(t *testing.T) {
	got := ParseAdhoc(adhocShellOutput)
	if len(got) != 1 || got[0].Alias != "web-1" || got[0].Status != AdhocChanged {
		t.Fatalf("shell results = %+v", got)
	}
	if got[0].Summary != "hellooops" {
		t.Fatalf("shell summary = %q, want merged stdout/stderr", got[0].Summary)
	}

	got = ParseAdhoc(adhocFailedOutput)
	if len(got) != 1 || got[0].Status != AdhocFailed {
		t.Fatalf("failed results = %+v", got)
	}
	if !strings.Contains(got[0].Summary, "non-zero return code") {
		t.Fatalf("failed summary = %q", got[0].Summary)
	}
}

func TestParseAdhocRealUnreachable(t *testing.T) {
	got := ParseAdhoc(adhocUnreachableOutput)
	if len(got) != 1 || got[0].Status != AdhocUnreachable || got[0].Alias != "web-1" {
		t.Fatalf("unreachable results = %+v", got)
	}
	if !strings.Contains(got[0].Summary, "Connection refused") {
		t.Fatalf("unreachable summary = %q, want the msg", got[0].Summary)
	}
}

func TestParseAdhocColoredMultiHost(t *testing.T) {
	got := ParseAdhoc(adhocColoredOutput)
	if len(got) != 4 {
		t.Fatalf("got %d results, want 4: %+v", len(got), got)
	}
	statuses := []string{AdhocSuccess, AdhocChanged, AdhocFailed, AdhocUnreachable}
	for i, st := range statuses {
		if got[i].Status != st {
			t.Fatalf("result %d status = %q, want %q (%+v)", i, got[i].Status, st, got[i])
		}
	}
	if got[1].Summary != "hello from web-2" || got[2].Summary != "boom" {
		t.Fatalf("summaries = %q / %q", got[1].Summary, got[2].Summary)
	}
	if strings.Contains(got[0].Summary, "\x1b") {
		t.Fatalf("ANSI not stripped: %q", got[0].Summary)
	}
	if c := CountAdhoc(got); c != (AdhocCounts{Success: 1, Changed: 1, Unreachable: 1, Failed: 1}) {
		t.Fatalf("CountAdhoc = %+v", c)
	}
}

func TestParseAdhocNoResults(t *testing.T) {
	for _, log := range []string{"", "ansible: error\n", "[WARNING]: Could not match\nweb-1 | BOGUS | x\n"} {
		if got := ParseAdhoc(log); len(got) != 0 {
			t.Fatalf("ParseAdhoc(%q) = %+v, want none", log, got)
		}
	}
}

// TestRunAdhocFakeAnsible checks the argument assembly and env without touching SSH.
func TestRunAdhocFakeAnsible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell script test on windows")
	}
	binDir := t.TempDir()
	script := `#!/bin/sh
echo "FORCE_COLOR=$ANSIBLE_FORCE_COLOR"
echo "CONFIG=$ANSIBLE_CONFIG"
[ -n "$ANSIBLE_CONFIG" ] && grep -E 'host_key_checking' "$ANSIBLE_CONFIG"
for arg in "$@"; do echo "ARG:$arg"; done
[ -e "$3" ] && echo "INVENTORY_EXISTS"
[ -e keys/web-1.key ] && echo "KEY_MODE:$(stat -c %a keys/web-1.key 2>/dev/null || echo ?)"
echo "web-1 | SUCCESS | rc=0 >>"
echo "pong"
`
	bin := filepath.Join(binDir, "ansible")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var log strings.Builder
	exitCode, err := RunAdhoc(context.Background(), AdhocSpec{
		Module: "ping", Pattern: "web-1", Become: true, Verbosity: 4, Forks: 3,
		ExtraVars: `{"a":1}`,
		Targets:   []Target{{ID: 1, Name: "web-1", Address: "10.0.0.1", User: "root", PrivateKey: "KEYDATA"}},
	}, &log)
	if err != nil || exitCode != 0 {
		t.Fatalf("RunAdhoc exit=%d err=%v", exitCode, err)
	}
	out := log.String()
	for _, want := range []string{
		"FORCE_COLOR=1", "host_key_checking = False", "ARG:web-1", "ARG:-m", "ARG:ping",
		"ARG:-vvvv", "ARG:--become", "ARG:-f", "ARG:3", "ARG:-e", "INVENTORY_EXISTS", "KEY_MODE:600",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ARG:-a") {
		t.Fatalf("empty args must not add -a:\n%s", out)
	}
	if got := ParseAdhoc(out); len(got) != 1 || got[0].Summary != "pong" {
		t.Fatalf("parsed fake run = %+v", got)
	}
	// Workspace cleanup
	if extra := strings.TrimSpace(strings.Join(strings.Fields(out), " ")); !strings.Contains(extra, "INVENTORY_EXISTS") {
		t.Fatal("inventory missing")
	}
}

func fakeAnsibleOnPath(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell script test on windows")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "ansible"), []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGatherFactsPinsMinimalCallback(t *testing.T) {
	fakeAnsibleOnPath(t, `printf 'web-1 | SUCCESS => {"ansible_facts": {"ansible_architecture": "%s"}, "changed": false}\n' "$ANSIBLE_STDOUT_CALLBACK"
`)
	facts, err := GatherFacts(context.Background(), Target{ID: 1, Name: "web-1"}, "[defaults]\nforks = 3\n", "", nil)
	if err != nil {
		t.Fatalf("GatherFacts: %v", err)
	}
	if facts["ansible_architecture"] != "minimal" {
		t.Fatalf("stdout callback env = %v, want minimal", facts["ansible_architecture"])
	}
}

func TestGatherFactsReportsHostFailureAndNoise(t *testing.T) {
	fakeAnsibleOnPath(t, `echo 'web-1 | UNREACHABLE! => {"msg": "Failed to connect to the host via ssh", "unreachable": true}'
exit 4
`)
	if _, err := GatherFacts(context.Background(), Target{ID: 1, Name: "web-1"}, "", "", nil); err == nil || !strings.Contains(err.Error(), "Failed to connect") {
		t.Fatalf("err = %v, want the host message", err)
	}
	fakeAnsibleOnPath(t, `echo "facts without any host result line"
`)
	if _, err := GatherFacts(context.Background(), Target{ID: 1, Name: "web-1"}, "", "", nil); err == nil || !strings.Contains(err.Error(), "no per-host result") {
		t.Fatalf("err = %v, want the no-result error", err)
	}
}

func TestAdhocSummarySkipsNestedObjectFields(t *testing.T) {
	body := []string{
		"    \"msg\": \"boom\",",
		"    \"rc\": 2",
		"}",
	}
	if got := adhocSummary(body); got != "boom" {
		t.Fatalf("adhocSummary = %q, want the msg", got)
	}
	if got := adhocSummary([]string{"uptime output"}); got != "uptime output" {
		t.Fatalf("adhocSummary = %q, want raw output", got)
	}
}
