package inspector

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"pantau/internal/gwupdate"
)

// writeStubKubectl writes a kubectl that reports Client Version 1.31.4 for "version --client" and
// prints label for anything else, so a test can tell which copy ran.
func writeStubKubectl(t *testing.T, dir, label string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "kubectl")
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo 'Client Version: v1.31.4'; exit 0; fi\necho " + label + "\n"
	if err := os.WriteFile(p, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// setStubManagedRoot points gwupdate at a managed directory for the test only.
func setStubManagedRoot(t *testing.T, root string) {
	t.Helper()
	gwupdate.SetManagedRoot(root)
	t.Cleanup(func() {
		gwupdate.SetManagedRoot("")
		InvalidateKubectlDetectCache()
	})
}

func TestExternalClusterPrefersManagedKubectl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	sysDir := t.TempDir()
	sysBin := writeStubKubectl(t, sysDir, "system")
	t.Setenv("PATH", sysDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	managed := writeStubKubectl(t, filepath.Join(root, "bin"), "managed")
	setStubManagedRoot(t, root)

	p, err := resolveKubectlBinary()
	if err != nil || p != managed {
		t.Fatalf("resolve = %q, %v; want the managed %q", p, err, managed)
	}

	// The commands the runner starts see the managed copy first on PATH.
	r := &localKubeRunner{kubeconfigPath: "unused-kubeconfig"}
	out, _, _, err := r.ExecTimeout("kubectl", 10*time.Second)
	if err != nil || strings.TrimSpace(out) != "managed" {
		t.Fatalf("ExecTimeout = %q, %v; want managed", out, err)
	}

	// Reverting (no managed binary) falls back to the system copy, with no restart.
	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	p, err = resolveKubectlBinary()
	if err != nil || p != sysBin {
		t.Fatalf("after revert resolve = %q, %v; want the system %q", p, err, sysBin)
	}
	out, _, _, err = r.ExecTimeout("kubectl", 10*time.Second)
	if err != nil || strings.TrimSpace(out) != "system" {
		t.Fatalf("after revert ExecTimeout = %q, %v; want system", out, err)
	}
}

func TestDetectLocalKubectlReportsSourceAndSystem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	sysDir := t.TempDir()
	writeStubKubectl(t, sysDir, "system")
	t.Setenv("PATH", sysDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	managed := writeStubKubectl(t, filepath.Join(root, "bin"), "managed")
	setStubManagedRoot(t, root)

	InvalidateKubectlDetectCache()
	s := DetectLocalKubectl()
	if s.Source != "managed" || s.Path != managed || s.Version != "1.31.4" {
		t.Errorf("managed status = %+v", s)
	}
	if !s.SystemAvailable || s.SystemVersion != "1.31.4" {
		t.Errorf("the system copy must be reported while a managed one is active: %+v", s)
	}

	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	InvalidateKubectlDetectCache()
	if s := DetectLocalKubectl(); s.Source != "system" {
		t.Errorf("after revert source = %q, want system", s.Source)
	}
}

func TestDetectLocalKubectlReportsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	gwupdate.SetManagedRoot("")
	InvalidateKubectlDetectCache()
	defer InvalidateKubectlDetectCache()

	s := DetectLocalKubectl()
	if s.Source != "missing" || s.SystemAvailable {
		t.Errorf("status = %+v, want missing with no system copy", s)
	}
}
