package gwupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeKubectlServer serves dl.k8s.io for any version: the binary and its .sha256. The binary answers
// "version --client" with its version, and fails for any other argument (like kubectl offline).
// broken names a version whose binary reports a different version; corrupt names one whose checksum
// file does not match.
type fakeKubectlServer struct {
	srv     *httptest.Server
	broken  string
	corrupt string
}

func newFakeKubectlServer(t *testing.T) *fakeKubectlServer {
	t.Helper()
	f := &fakeKubectlServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/v"
		suffix := "/bin/" + runtime.GOOS + "/" + runtime.GOARCH + "/kubectl"
		path := r.URL.Path
		if strings.HasSuffix(path, ".sha256") {
			path = strings.TrimSuffix(path, ".sha256")
		}
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			http.NotFound(w, r)
			return
		}
		version := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
		bin := f.binary(version)
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			sum := sha256Hex(bin)
			if version == f.corrupt {
				sum = strings.Repeat("0", 64)
			}
			fmt.Fprintf(w, "%s  kubectl\n", sum)
			return
		}
		w.Write(bin)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// binary is a shell script that behaves like kubectl for the two cases the installer uses.
func (f *fakeKubectlServer) binary(version string) []byte {
	reported := version
	if version == f.broken {
		reported = "9.9.9"
	}
	return []byte(fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ] && [ "$2" = "--client" ]; then
  echo "Client Version: v%s"
  exit 0
fi
echo "The connection to the server localhost:8080 was refused" >&2
exit 1
`, reported))
}

// point sends the upstream requests of the package to the fake server.
func (f *fakeKubectlServer) point(t *testing.T) {
	t.Helper()
	old := KubectlBaseURL
	KubectlBaseURL = f.srv.URL
	t.Cleanup(func() { KubectlBaseURL = old })
}

func skipUnlessUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the kubectl install is Linux/macOS only")
	}
}

func TestLatestKubectlReadsStable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stable.txt" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("v1.31.4\n"))
	}))
	defer srv.Close()
	old := KubectlBaseURL
	KubectlBaseURL = srv.URL
	defer func() { KubectlBaseURL = old }()

	rel, err := LatestKubectl(context.Background())
	if err != nil {
		t.Fatalf("LatestKubectl: %v", err)
	}
	if rel.Version != "1.31.4" {
		t.Errorf("version = %q, want 1.31.4", rel.Version)
	}
	if !strings.Contains(rel.URL, "v1.31.4") {
		t.Errorf("url = %q, want it to name the release", rel.URL)
	}
}

func TestLatestKubectlRejectsNonRelease(t *testing.T) {
	for _, body := range []string{"latest", "v1.31.4/../x", "v1.31.4-rc.1", "1.31", ""} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		old := KubectlBaseURL
		KubectlBaseURL = srv.URL
		_, err := LatestKubectl(context.Background())
		KubectlBaseURL = old
		srv.Close()
		if err == nil {
			t.Errorf("body %q: expected an error", body)
		}
	}
}

func TestLatestKubectlReportsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	old := KubectlBaseURL
	KubectlBaseURL = srv.URL
	defer func() { KubectlBaseURL = old }()

	if _, err := LatestKubectl(context.Background()); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestLatestKubectlChannelReadsMinor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stable-1.31.txt" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("v1.31.5\n"))
	}))
	defer srv.Close()
	old := KubectlBaseURL
	KubectlBaseURL = srv.URL
	defer func() { KubectlBaseURL = old }()

	rel, err := LatestKubectlChannel(context.Background(), "1.31")
	if err != nil {
		t.Fatalf("LatestKubectlChannel: %v", err)
	}
	if rel.Version != "1.31.5" {
		t.Errorf("version = %q, want 1.31.5", rel.Version)
	}
	if _, err := LatestKubectlChannel(context.Background(), "1.31.5"); err == nil {
		t.Error("a full version is not a minor channel")
	}
	if _, err := LatestKubectlChannel(context.Background(), "../1.31"); err == nil {
		t.Error("a path is not a minor channel")
	}
}

func TestCheckKubectlReportsUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("v1.32.1\n"))
	}))
	defer srv.Close()
	old := KubectlBaseURL
	KubectlBaseURL = srv.URL
	defer func() { KubectlBaseURL = old }()

	res := Check(context.Background(), TargetKubectl, "1.30.8")
	if res.Status != StatusUpdate || res.Latest != "1.32.1" {
		t.Errorf("status = %q latest = %q, want update 1.32.1", res.Status, res.Latest)
	}
}

func TestInstallKubectlActivatesVersion(t *testing.T) {
	skipUnlessUnix(t)
	f := newFakeKubectlServer(t)
	f.point(t)
	root := t.TempDir()

	if err := Install(context.Background(), TargetKubectl, "v1.31.4", root, nil); err != nil {
		t.Fatalf("Install: %v", err)
	}
	m := ManagedDir{Root: root}
	target, err := os.Readlink(filepath.Join(m.BinDir(), "kubectl"))
	if err != nil {
		t.Fatalf("bin/kubectl is not a symlink: %v", err)
	}
	if want := filepath.Join(m.VersionsDir(), "kubectl-1.31.4"); target != want {
		t.Errorf("symlink = %q, want %q", target, want)
	}
	if got := m.ActiveKubectlVersion(); got != "1.31.4" {
		t.Errorf("active = %q, want 1.31.4", got)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestInstallKubectlRefusesChecksumMismatch(t *testing.T) {
	skipUnlessUnix(t)
	f := newFakeKubectlServer(t)
	f.corrupt = "1.31.4"
	f.point(t)
	root := t.TempDir()

	if err := Install(context.Background(), TargetKubectl, "1.31.4", root, nil); err == nil {
		t.Fatal("expected a checksum error")
	}
	m := ManagedDir{Root: root}
	if _, err := os.Lstat(filepath.Join(m.BinDir(), "kubectl")); !os.IsNotExist(err) {
		t.Errorf("no symlink must be written after a checksum error, got %v", err)
	}
	entries, _ := os.ReadDir(m.VersionsDir())
	if len(entries) != 0 {
		t.Errorf("nothing must be written after a checksum error, found %d entries", len(entries))
	}
}

func TestInstallKubectlKeepsWorkingVersionWhenNewOneDoesNotRun(t *testing.T) {
	skipUnlessUnix(t)
	f := newFakeKubectlServer(t)
	f.point(t)
	root := t.TempDir()
	if err := Install(context.Background(), TargetKubectl, "1.31.4", root, nil); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	f.broken = "1.31.5"
	if err := Install(context.Background(), TargetKubectl, "1.31.5", root, nil); err == nil {
		t.Fatal("expected the smoke test to refuse a binary that reports another version")
	}
	if got := (ManagedDir{Root: root}).ActiveKubectlVersion(); got != "1.31.4" {
		t.Errorf("active = %q, want the working 1.31.4", got)
	}
}

func TestInstallKubectlSwitchesToCachedVersionOffline(t *testing.T) {
	skipUnlessUnix(t)
	f := newFakeKubectlServer(t)
	f.point(t)
	root := t.TempDir()
	for _, v := range []string{"1.31.4", "1.31.5"} {
		if err := Install(context.Background(), TargetKubectl, v, root, nil); err != nil {
			t.Fatalf("Install %s: %v", v, err)
		}
	}

	// The upstream is unreachable now: switching back to a cached version must still work.
	KubectlBaseURL = "http://127.0.0.1:1"
	if err := Install(context.Background(), TargetKubectl, "1.31.4", root, nil); err != nil {
		t.Fatalf("offline switch: %v", err)
	}
	if got := (ManagedDir{Root: root}).ActiveKubectlVersion(); got != "1.31.4" {
		t.Errorf("active = %q, want 1.31.4", got)
	}
}

func TestUseSystemKubectlRemovesOnlyTheLink(t *testing.T) {
	skipUnlessUnix(t)
	f := newFakeKubectlServer(t)
	f.point(t)
	root := t.TempDir()
	m := ManagedDir{Root: root}
	if err := Install(context.Background(), TargetKubectl, "1.31.4", root, nil); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := m.UseSystemKubectl(); err != nil {
		t.Fatalf("UseSystemKubectl: %v", err)
	}
	if got := m.ActiveKubectlVersion(); got != "" {
		t.Errorf("active = %q, want none after reverting to the system copy", got)
	}
	if _, err := os.Stat(filepath.Join(m.VersionsDir(), "kubectl-1.31.4")); err != nil {
		t.Errorf("the cached version must be kept: %v", err)
	}
	// A second revert is a no-op, not an error.
	if err := m.UseSystemKubectl(); err != nil {
		t.Errorf("second UseSystemKubectl: %v", err)
	}
}

func TestDeleteKubectlVersionRefusesActiveVersion(t *testing.T) {
	skipUnlessUnix(t)
	f := newFakeKubectlServer(t)
	f.point(t)
	root := t.TempDir()
	m := ManagedDir{Root: root}
	for _, v := range []string{"1.31.4", "1.31.5"} {
		if err := Install(context.Background(), TargetKubectl, v, root, nil); err != nil {
			t.Fatalf("Install %s: %v", v, err)
		}
	}

	if err := m.DeleteKubectlVersion("1.31.5"); err != ErrKubectlVersionActive {
		t.Fatalf("delete active: err = %v, want ErrKubectlVersionActive", err)
	}
	if err := m.DeleteKubectlVersion("1.31.4"); err != nil {
		t.Fatalf("delete inactive: %v", err)
	}
	if err := m.DeleteKubectlVersion("../etc"); err == nil {
		t.Error("a path is not a version and must be refused")
	}

	versions, active, err := m.ListKubectlVersions()
	if err != nil {
		t.Fatalf("ListKubectlVersions: %v", err)
	}
	if len(versions) != 1 || versions[0] != "1.31.5" || active != "1.31.5" {
		t.Errorf("versions = %v active = %q, want [1.31.5] and 1.31.5", versions, active)
	}
}
