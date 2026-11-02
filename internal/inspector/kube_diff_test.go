package inspector

import (
	"strings"
	"testing"
)

func TestParseKubeDetectClusterID(t *testing.T) {
	out := `===DISTRO===
k3s
===VERSION===
{"serverVersion":{"gitVersion":"v1.36.5+k3s1"}}
===CLUSTER_ID===
f52cb567-728a-4c51-819a-e501ea09a451
`
	info, err := parseKubeDetect(out)
	if err != nil {
		t.Fatalf("parseKubeDetect failed: %v", err)
	}
	if info.Distro != "k3s" {
		t.Errorf("expected distro k3s, got %s", info.Distro)
	}
	if info.Version != "v1.36.5+k3s1" {
		t.Errorf("expected version v1.36.5+k3s1, got %s", info.Version)
	}
	if info.Role != "server" {
		t.Errorf("expected role server, got %s", info.Role)
	}
	if info.ClusterID != "f52cb567-728a-4c51-819a-e501ea09a451" {
		t.Errorf("expected cluster_id f52cb567-728a-4c51-819a-e501ea09a451, got %s", info.ClusterID)
	}
}

func TestParseKubeDiffExitCodesAndTruncation(t *testing.T) {
	// Exit 0: no diff
	out, changed, err := parseKubeDiff("", "", 0)
	if err != nil {
		t.Errorf("exit 0 should not return error, got %v", err)
	}
	if changed {
		t.Errorf("exit 0 should have changed=false")
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}

	// Exit 1: diff found (not an error!)
	diffSample := "--- live\n+++ merged\n@@ -1,3 +1,3 @@"
	out, changed, err = parseKubeDiff(diffSample, "", 1)
	if err != nil {
		t.Errorf("exit 1 should not return error, got %v", err)
	}
	if !changed {
		t.Errorf("exit 1 should have changed=true")
	}
	if out != diffSample {
		t.Errorf("expected output %q, got %q", diffSample, out)
	}

	// Exit 2: error
	_, changed, err = parseKubeDiff("", "Dry-run failed: schema error", 2)
	if err == nil {
		t.Errorf("exit 2 should return an error")
	}
	if changed {
		t.Errorf("exit 2 should have changed=false")
	}
	if !strings.Contains(err.Error(), "Dry-run failed") {
		t.Errorf("expected error message to contain Dry-run failed, got %v", err)
	}

	// Truncation: 300 KB output cut to 256 KB
	largeOutput := strings.Repeat("A", 300*1024)
	out, changed, err = parseKubeDiff(largeOutput, "", 1)
	if err != nil {
		t.Errorf("unexpected error on large output: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true")
	}
	if len(out) != KubeDiffOutputLimit {
		t.Errorf("expected output length %d, got %d", KubeDiffOutputLimit, len(out))
	}
}
