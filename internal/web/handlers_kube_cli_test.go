package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"pantau/internal/gwupdate"
	"pantau/internal/inspector"
)

// kubeCliRoot points gwupdate at a fresh managed directory for one test and restores it after.
func kubeCliRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the kubectl manager is Linux/macOS only")
	}
	root := t.TempDir()
	old := gwupdate.ManagedRoot()
	gwupdate.SetManagedRoot(root)
	inspector.InvalidateKubectlDetectCache()
	t.Cleanup(func() {
		gwupdate.SetManagedRoot(old)
		inspector.InvalidateKubectlDetectCache()
	})
	return root
}

// writeKubeCliStub writes a kubectl that reports the given client version.
func writeKubeCliStub(t *testing.T, path, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo 'Client Version: v" + version + "'; exit 0; fi\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

// 1. Every kubectl endpoint needs a session.
func TestKubeCliUnauthenticated(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/kube/cli/update-check"},
		{http.MethodPost, "/api/kube/cli/install"},
		{http.MethodPost, "/api/kube/cli/switch"},
		{http.MethodDelete, "/api/kube/cli/versions/1.31.4"},
	} {
		rec := doRequest(t, srv, nil, c.method, c.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", c.method, c.path, rec.Code)
		}
	}
}

// 2. Each endpoint accepts only its own method.
func TestKubeCliRejectsWrongMethod(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/kube/cli/update-check"},
		{http.MethodGet, "/api/kube/cli/install"},
		{http.MethodGet, "/api/kube/cli/switch"},
		{http.MethodGet, "/api/kube/cli/versions/1.31.4"},
	} {
		rec := doRequest(t, srv, auth, c.method, c.path, nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: expected 405, got %d", c.method, c.path, rec.Code)
		}
	}
}

// 3. A version that is not a release is refused before anything is downloaded.
func TestKubeCliInstallRefusesNonReleaseVersion(t *testing.T) {
	kubeCliRoot(t)
	srv, auth := setupAnsibleTestServer(t)
	for _, v := range []string{"latest", "1.31", "../1.31.4"} {
		rec := doRequest(t, srv, auth, http.MethodPost, "/api/kube/cli/install", map[string]string{"version": v})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("version %q: expected 400, got %d", v, rec.Code)
		}
	}
}

// 4. Deleting a version that is not a release number is refused.
func TestKubeCliDeleteRefusesNonReleaseVersion(t *testing.T) {
	kubeCliRoot(t)
	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodDelete, "/api/kube/cli/versions/latest", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// 5. A switch names either a mode or a version, not both.
func TestKubeCliSwitchRejectsModeAndVersion(t *testing.T) {
	kubeCliRoot(t)
	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/kube/cli/switch",
		map[string]string{"mode": "system", "version": "1.31.4"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// 6. Reverting to the system kubectl is refused when there is no system kubectl: External Cluster
// operations would be left with no kubectl at all.
func TestKubeCliSwitchToSystemRefusedWithoutSystemKubectl(t *testing.T) {
	root := kubeCliRoot(t)
	writeKubeCliStub(t, filepath.Join(root, "bin", "kubectl"), "1.31.4")
	t.Setenv("PATH", t.TempDir())
	inspector.InvalidateKubectlDetectCache()

	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/kube/cli/switch", map[string]string{"mode": "system"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "kubectl")); err != nil {
		t.Errorf("the managed binary must be left in place: %v", err)
	}
}

// 7. The version in use cannot be deleted (409); an idle cached version can (200); an unknown one is
// 404.
func TestKubeCliDeleteVersion(t *testing.T) {
	root := kubeCliRoot(t)
	versions := filepath.Join(root, "versions")
	if err := os.MkdirAll(versions, 0755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(versions, "kubectl-1.31.4")
	idle := filepath.Join(versions, "kubectl-1.30.8")
	for _, p := range []string{active, idle} {
		if err := os.WriteFile(p, []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(active, filepath.Join(root, "bin", "kubectl")); err != nil {
		t.Fatal(err)
	}

	srv, auth := setupAnsibleTestServer(t)

	rec := doRequest(t, srv, auth, http.MethodDelete, "/api/kube/cli/versions/1.31.4", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("active version: expected 409, got %d", rec.Code)
	}
	if _, err := os.Stat(active); err != nil {
		t.Errorf("the active version must be kept: %v", err)
	}

	rec = doRequest(t, srv, auth, http.MethodDelete, "/api/kube/cli/versions/1.30.8", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("idle version: expected 200, got %d", rec.Code)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Errorf("the idle version must be removed, got %v", err)
	}

	rec = doRequest(t, srv, auth, http.MethodDelete, "/api/kube/cli/versions/1.29.0", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown version: expected 404, got %d", rec.Code)
	}
}

// 8. An upstream that cannot be reached is reported as an error with HTTP 200, never as up to date,
// and the cached versions are still listed.
func TestKubeCliUpdateCheckReportsUpstreamError(t *testing.T) {
	root := kubeCliRoot(t)
	writeKubeCliStub(t, filepath.Join(root, "bin", "kubectl"), "1.31.4")
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "versions", "kubectl-1.30.8"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	old := gwupdate.KubectlBaseURL
	gwupdate.KubectlBaseURL = "http://127.0.0.1:1"
	defer func() { gwupdate.KubectlBaseURL = old }()

	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/kube/cli/update-check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res kubeCliUpdateCheck
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.OK || res.Status != gwupdate.StatusError || res.Error == "" {
		t.Errorf("status = %q ok = %v error = %q, want an error status", res.Status, res.OK, res.Error)
	}
	if res.Installed != "1.31.4" || res.Source != "managed" {
		t.Errorf("installed = %q source = %q, want 1.31.4 managed", res.Installed, res.Source)
	}
	if len(res.ManagedVersions) != 1 || res.ManagedVersions[0] != "1.30.8" {
		t.Errorf("managed versions = %v, want [1.30.8]", res.ManagedVersions)
	}
	if len(res.Channels) != 0 {
		t.Errorf("channels = %v, want none while the upstream is unreachable", res.Channels)
	}
}

// 9. With the upstream reachable the check reports the update and the newest patch of the last
// three minors.
func TestKubeCliUpdateCheckReportsUpdateAndChannels(t *testing.T) {
	root := kubeCliRoot(t)
	writeKubeCliStub(t, filepath.Join(root, "bin", "kubectl"), "1.31.4")

	upstream := map[string]string{
		"/stable.txt":      "v1.32.1\n",
		"/stable-1.32.txt": "v1.32.1\n",
		"/stable-1.31.txt": "v1.31.5\n",
		"/stable-1.30.txt": "v1.30.9\n",
	}
	srvUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := upstream[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	defer srvUp.Close()
	old := gwupdate.KubectlBaseURL
	gwupdate.KubectlBaseURL = srvUp.URL
	defer func() { gwupdate.KubectlBaseURL = old }()

	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/kube/cli/update-check", nil)
	var res kubeCliUpdateCheck
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Status != gwupdate.StatusUpdate || res.Latest != "1.32.1" {
		t.Errorf("status = %q latest = %q, want update 1.32.1", res.Status, res.Latest)
	}
	got := map[string]string{}
	for _, c := range res.Channels {
		got[c.Minor] = c.Latest
	}
	want := map[string]string{"1.32": "1.32.1", "1.31": "1.31.5", "1.30": "1.30.9"}
	if len(got) != len(want) {
		t.Fatalf("channels = %v, want %v", got, want)
	}
	for minor, latest := range want {
		if got[minor] != latest {
			t.Errorf("channel %s = %q, want %q", minor, got[minor], latest)
		}
	}
	if !strings.Contains(rec.Body.String(), `"managed_versions":[]`) {
		t.Errorf("managed_versions must be an empty list, not null: %s", rec.Body.String())
	}
}
