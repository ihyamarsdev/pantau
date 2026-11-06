package ansible

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// minimalSetupOutput renders the facts of the recorded real `setup` run (kept as the json-callback document in
// facts_fixtures_test.go) the way the ad-hoc `minimal` stdout callback prints a successful host.
func minimalSetupOutput(t *testing.T) []byte {
	t.Helper()
	var doc struct {
		Plays []struct {
			Tasks []struct {
				Hosts map[string]struct {
					AnsibleFacts map[string]any `json:"ansible_facts"`
				} `json:"hosts"`
			} `json:"tasks"`
		} `json:"plays"`
	}
	if err := json.Unmarshal([]byte(setupFactsJSON), &doc); err != nil {
		t.Fatal(err)
	}
	for _, h := range doc.Plays[0].Tasks[0].Hosts {
		b, err := json.MarshalIndent(map[string]any{"ansible_facts": h.AnsibleFacts, "changed": false}, "", "    ")
		if err != nil {
			t.Fatal(err)
		}
		return []byte("web-1 | SUCCESS => " + string(b) + "\n")
	}
	t.Fatal("fixture has no host")
	return nil
}

func TestParseSetupFactsRealOutput(t *testing.T) {
	facts, err := parseSetupFacts(minimalSetupOutput(t))
	if err != nil {
		t.Fatalf("parseSetupFacts: %v", err)
	}
	if _, ok := facts["ansible_architecture"]; !ok {
		t.Fatalf("parsed %d facts without ansible_architecture", len(facts))
	}
}

// The real ansible binary is the only judge of the minimal callback format and of the generated ansible.cfg
// (a duplicated [defaults] section once made every facts run fail with exit 5).
func TestGatherFactsConfigAndFormatWithRealAnsible(t *testing.T) {
	bin, err := exec.LookPath("ansible")
	if err != nil {
		t.Skip("ansible binary not found")
	}
	dir := t.TempDir()
	cfg, err := BuildConfig("")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "ansible.cfg")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "localhost", "-c", "local", "-m", "setup")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "ANSIBLE_CONFIG=" + cfgPath, "ANSIBLE_STDOUT_CALLBACK=minimal", "ANSIBLE_HOME=" + filepath.Join(dir, ".ansible")}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("real ansible rejected the generated config or setup failed: %v\n%s", err, out)
	}
	facts, err := parseSetupFacts(out)
	if err != nil {
		t.Fatalf("real minimal output not parsed: %v", err)
	}
	if facts["ansible_architecture"] == nil {
		t.Fatalf("ansible_architecture missing from %d facts", len(facts))
	}
}

func TestParseSetupFactsRejectsNoise(t *testing.T) {
	if _, err := parseSetupFacts([]byte("no result line here\n")); err == nil {
		t.Fatal("expected an error for output without a host result")
	}
	if _, err := parseSetupFacts([]byte(`web-1 | SUCCESS => {"ansible_facts":"nope"}`)); err == nil {
		t.Fatal("expected an error for malformed facts")
	}
	if _, err := parseSetupFacts([]byte(`web-1 | SUCCESS => {"ansible_facts":{},"changed":false}`)); err == nil {
		t.Fatal("expected an error for empty facts")
	}
	for _, status := range []string{"FAILED!", "UNREACHABLE!"} {
		out := `web-1 | ` + status + ` => {"msg": "Failed to connect to the host via ssh", "unreachable": true}`
		if _, err := parseSetupFacts([]byte(out)); err == nil || !strings.Contains(err.Error(), "Failed to connect") {
			t.Fatalf("%s: err = %v, want the host message", status, err)
		}
	}
}

func TestSummarizeFactsRealOutput(t *testing.T) {
	facts, err := parseSetupFacts(minimalSetupOutput(t))
	if err != nil {
		t.Fatal(err)
	}
	s := SummarizeFacts(facts)
	if s.Distribution == "" || s.Version == "" || s.Kernel == "" || s.Architecture == "" {
		t.Fatalf("OS fields missing: %+v", s)
	}
	if s.OS != strings.TrimSpace(s.Distribution+" "+s.Version) {
		t.Fatalf("OS = %q", s.OS)
	}
	if s.Hostname == "" || s.FQDN == "" {
		t.Fatalf("host fields missing: %+v", s)
	}
	if s.CPUCores <= 0 || s.CPUModel == "" {
		t.Fatalf("cpu fields missing: %+v", s)
	}
	if s.MemTotalMB <= 0 || s.SwapTotalMB < 0 || s.UptimeSeconds <= 0 {
		t.Fatalf("sizes/uptime missing: %+v", s)
	}
	if len(s.Mounts) != 1 || s.Mounts[0].Mount != "/" || s.Mounts[0].SizeTotal <= 0 || s.Mounts[0].FSType == "" {
		t.Fatalf("mounts = %+v", s.Mounts)
	}
	var loopback, wlan bool
	for _, iface := range s.Interfaces {
		switch iface.Name {
		case "lo":
			loopback = iface.IPv4 == "127.0.0.1" && len(iface.IPv6) > 0
		case "wlan0":
			wlan = iface.Active && iface.IPv4 != "" && iface.MAC != "" && len(iface.IPv6) > 0
		}
	}
	if !loopback || !wlan {
		t.Fatalf("interfaces = %+v", s.Interfaces)
	}
	if s.DefaultIPv4 == "" {
		t.Fatalf("default_ipv4 missing: %+v", s)
	}
	if s.Python == "" {
		t.Fatalf("python missing: %+v", s)
	}
}

func TestSummarizeFactsEmpty(t *testing.T) {
	s := SummarizeFacts(nil)
	if s.Mounts == nil || s.Interfaces == nil {
		t.Fatal("slices must be non-nil so the JSON stays []")
	}
	if s.OS != "" || s.CPUCores != 0 {
		t.Fatalf("unexpected values from empty facts: %+v", s)
	}
}

func TestTailWriterSummary(t *testing.T) {
	w := &tailWriter{}
	_, _ = w.Write([]byte("[WARNING]: ignoring me\n"))
	_, _ = w.Write([]byte("[ERROR]: Task failed: boom\nmore detail\n"))
	if got := w.summary(); got != "Task failed: boom" {
		t.Fatalf("summary = %q", got)
	}
	_, _ = w.Write([]byte("\x00\x01trailing"))
	if got := (&tailWriter{}).summary(); got != "no output" {
		t.Fatalf("empty summary = %q", got)
	}
}
