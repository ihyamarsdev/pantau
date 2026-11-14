package inspector

import (
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestValidateFIMTarget(t *testing.T) {
	valid := []string{
		"/etc/passwd",
		"/etc/passwd,/etc/group",
		"/etc/passwd\n/etc/group",
		"preset:auth",
		"suid",
	}
	for _, v := range valid {
		if err := ValidateFIMTarget(v); err != nil {
			t.Errorf("target %q must be valid: %v", v, err)
		}
	}
	invalid := []string{
		"",
		"etc/passwd",              // relative
		"/etc/../etc/passwd",      // traversal
		"/etc/passwd; rm -rf /",   // injection
		"/etc/passwd\nid",         // newline injection
		"/etc/passwd`id`",         // backtick
		"/etc/passwd$(id)",        // expansion
		"/etc/pass*wd",            // glob
		"preset:unknown",          // unknown preset
		"/etc/passwd,/etc/group,", // trailing comma is tolerated
	}
	for _, v := range invalid {
		if v == "/etc/passwd,/etc/group," {
			continue
		}
		if err := ValidateFIMTarget(v); err == nil {
			t.Errorf("target %q must be rejected", v)
		}
	}
	// More than 20 paths is rejected.
	many := make([]string, FIMMaxPaths+1)
	for i := range many {
		many[i] = "/etc/f" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if err := ValidateFIMTarget(strings.Join(many, ",")); err == nil {
		t.Errorf("more than %d paths must be rejected", FIMMaxPaths)
	}
}

func TestFIMBaselineRoundTrip(t *testing.T) {
	files := []FIMFile{
		{Path: "/etc/group", Hash: hashB},
		{Path: "/etc/passwd", Hash: hashA},
	}
	encoded, err := EncodeFIMBaseline(files)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.HasPrefix(encoded, "sha256:") || !strings.Contains(encoded, "|m:") {
		t.Fatalf("unexpected encoding: %s", encoded)
	}
	parsed, err := ParseFIMExpected(encoded)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(parsed.Files))
	}
	// The manifest is sorted by path.
	if parsed.Files[0].Path != "/etc/group" || parsed.Files[1].Path != "/etc/passwd" {
		t.Fatalf("manifest is not sorted: %+v", parsed.Files)
	}
	// A tampered manifest must not pass the digest check.
	tampered := strings.Replace(encoded, hashA, hashB, 1)
	if _, err := ParseFIMExpected(tampered); err == nil {
		t.Fatal("tampered manifest must be rejected")
	}
	// "baseline" is not an accepted shortcut.
	if _, err := ParseFIMExpected("baseline"); err == nil {
		t.Fatal("expected 'baseline' must be rejected")
	}
	if _, err := ParseFIMExpected(""); err == nil {
		t.Fatal("empty expected must be rejected")
	}
}

func TestParseFIMOutput(t *testing.T) {
	out := "FIM:OK /etc/passwd " + hashA + "\n" +
		"FIM:OK /etc/group " + hashB + "\n" +
		"FIM:MISSING /etc/sudoers\n" +
		"FIM:DENIED /etc/shadow\n" +
		"FIM:DONE\n"
	c := ParseFIMOutput(out)
	if len(c.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(c.Files))
	}
	if c.Files["/etc/passwd"] != hashA {
		t.Fatalf("hash mismatch: %+v", c.Files)
	}
	if len(c.Missing) != 1 || c.Missing[0] != "/etc/sudoers" {
		t.Fatalf("missing = %v", c.Missing)
	}
	if len(c.Denied) != 1 || c.Denied[0] != "/etc/shadow" {
		t.Fatalf("denied = %v", c.Denied)
	}
}

func TestDiffFIM(t *testing.T) {
	base := &FIMBaseline{Files: []FIMFile{
		{Path: "/etc/passwd", Hash: hashA},
		{Path: "/etc/group", Hash: hashB},
		{Path: "/etc/shadow", Hash: hashA},
	}}
	current := map[string]string{
		"/etc/passwd": hashB, // modified
		"/etc/new":    hashA, // added
		// /etc/group removed
		// /etc/shadow unreadable -> not removed
	}
	d := DiffFIM(base, current, map[string]bool{"/etc/shadow": true})
	if len(d.Modified) != 1 || d.Modified[0].Path != "/etc/passwd" {
		t.Fatalf("modified = %+v", d.Modified)
	}
	if len(d.Added) != 1 || d.Added[0].Path != "/etc/new" {
		t.Fatalf("added = %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].Path != "/etc/group" {
		t.Fatalf("removed = %+v", d.Removed)
	}
}

// fimMockRunner answers the manifest command with a canned output.
func fimMockRunner(out string) *sshrunner.MockRunner {
	m := sshrunner.NewMockRunner()
	m.DefaultExec = func(cmd string) (string, string, int, error) {
		return out, "", 0, nil
	}
	return m
}

func TestEvaluateFIMRule_Ok(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	base, err := EncodeFIMBaseline([]FIMFile{{Path: "/etc/passwd", Hash: hashA}})
	if err != nil {
		t.Fatal(err)
	}
	runner := fimMockRunner("FIM:OK /etc/passwd " + hashA + "\nFIM:DONE\n")
	rule := &store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: base}
	curr, status, _, _ := ins.evaluateFIMRule(host, runner, rule)
	if status != "ok" || curr != "ok" {
		t.Fatalf("expected ok, got %s/%s", status, curr)
	}
}

func TestEvaluateFIMRule_Drift(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	base, err := EncodeFIMBaseline([]FIMFile{{Path: "/etc/passwd", Hash: hashA}})
	if err != nil {
		t.Fatal(err)
	}
	runner := fimMockRunner("FIM:OK /etc/passwd " + hashB + "\nFIM:DONE\n")
	rule := &store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: base}
	curr, status, details, rootCause := ins.evaluateFIMRule(host, runner, rule)
	if status != "drift" || curr != "drift" {
		t.Fatalf("expected drift, got %s/%s", status, curr)
	}
	if !strings.Contains(details, "/etc/passwd") {
		t.Fatalf("drift must name the changed file: %s", details)
	}
	if !strings.Contains(rootCause, "/etc/passwd") {
		t.Fatalf("root cause must name the changed file: %s", rootCause)
	}
}

func TestEvaluateFIMRule_MissingFileIsDrift(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	base, err := EncodeFIMBaseline([]FIMFile{{Path: "/etc/passwd", Hash: hashA}})
	if err != nil {
		t.Fatal(err)
	}
	runner := fimMockRunner("FIM:MISSING /etc/passwd\nFIM:DONE\n")
	rule := &store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: base}
	_, status, details, _ := ins.evaluateFIMRule(host, runner, rule)
	if status != "drift" {
		t.Fatalf("a missing baseline file must be drift, got %s", status)
	}
	if !strings.Contains(details, "/etc/passwd") {
		t.Fatalf("drift must name the missing file: %s", details)
	}
}

func TestEvaluateFIMRule_UnreadableIsNotAssessed(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	base, err := EncodeFIMBaseline([]FIMFile{{Path: "/etc/shadow", Hash: hashA}})
	if err != nil {
		t.Fatal(err)
	}
	runner := fimMockRunner("FIM:DENIED /etc/shadow\nFIM:DONE\n")
	rule := &store.DesiredRule{Kind: "file_integrity", Target: "/etc/shadow", Expected: base}
	curr, status, details, _ := ins.evaluateFIMRule(host, runner, rule)
	if status != "not_assessed" || curr != "not_assessed" {
		t.Fatalf("an unreadable file must be not_assessed, got %s/%s", status, curr)
	}
	if !strings.Contains(details, "/etc/shadow") {
		t.Fatalf("details must name the unreadable file: %s", details)
	}
}

func TestEvaluateFIMRule_NoHashTool(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	base, err := EncodeFIMBaseline([]FIMFile{{Path: "/etc/passwd", Hash: hashA}})
	if err != nil {
		t.Fatal(err)
	}
	runner := fimMockRunner("FIM:NO_HASH_TOOL\n")
	rule := &store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: base}
	_, status, _, _ := ins.evaluateFIMRule(host, runner, rule)
	if status != "not_assessed" {
		t.Fatalf("a host without sha256 must be not_assessed, got %s", status)
	}
}

func TestBuildFIMManifestCommand(t *testing.T) {
	cmd := BuildFIMManifestCommand("/etc/passwd", false)
	if !strings.Contains(cmd, "/etc/passwd") {
		t.Fatalf("command must contain the path: %s", cmd)
	}
	if strings.Contains(cmd, "sudo -n") {
		t.Fatalf("plain read must not use sudo: %s", cmd)
	}
	if !strings.Contains(cmd, "sha256sum") || !strings.Contains(cmd, "shasum -a 256") {
		t.Fatalf("command must try both hash tools: %s", cmd)
	}
	sudoCmd := BuildFIMManifestCommand("/etc/passwd", true)
	if !strings.Contains(sudoCmd, "sudo -n") {
		t.Fatalf("sudo fallback must use sudo -n: %s", sudoCmd)
	}
	// A path with a shell metacharacter must never reach the command unquoted.
	inj := BuildFIMManifestCommand("/etc/passwd;touch /tmp/x", false)
	if strings.Contains(inj, "p=/etc/passwd;touch") {
		t.Fatalf("injected path must be quoted: %s", inj)
	}
	if !strings.Contains(inj, "/etc/passwd;touch /tmp/x") {
		t.Fatalf("the path must still be hashed: %s", inj)
	}
	suid := BuildFIMManifestCommand("suid", false)
	if !strings.Contains(suid, "-perm -4000") || !strings.Contains(suid, "-xdev") {
		t.Fatalf("suid command must find setuid binaries on one filesystem: %s", suid)
	}
}

// The SUID manifest must be deterministic: the same sorted list yields the same digest.
func TestSUIDHashingDeterminism(t *testing.T) {
	files := []FIMFile{
		{Path: "/usr/bin/su", Hash: hashA},
		{Path: "/usr/bin/passwd", Hash: hashB},
	}
	first, err := EncodeFIMBaseline(files)
	if err != nil {
		t.Fatal(err)
	}
	shuffled := []FIMFile{files[1], files[0]}
	second, err := EncodeFIMBaseline(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("the manifest digest must not depend on the input order:\n%s\n%s", first, second)
	}
}

func TestFIMPresetAuthPaths(t *testing.T) {
	paths := FIMTargetPaths(FIMPresetAuth)
	want := map[string]bool{"/etc/passwd": false, "/etc/group": false, "/etc/sudoers": false, "/etc/ssh/sshd_config": false}
	for _, p := range paths {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, found := range want {
		if !found {
			t.Fatalf("preset:auth must include %s", p)
		}
	}
}
