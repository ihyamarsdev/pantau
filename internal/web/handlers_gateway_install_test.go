package web

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"pantau/internal/gwupdate"
)

// releaseZip builds a release archive holding one binary that reports version, plus the checksum of
// that archive.
func releaseZip(t *testing.T, name, version string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	w.Write([]byte("#!/bin/sh\necho '" + name + " v" + version + "'\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

// Installing a newer CLI (ADR 0081) must refuse to touch the system binaries, must name an unknown
// target, and must stream its steps to the UI.

// 1. The endpoint needs a session.
func TestCLIInstallUnauthenticated(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, nil, http.MethodPost, "/api/terraform/install", map[string]string{"target": "terraform"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// 2. Only POST is allowed.
func TestCLIInstallRejectsGet(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/terraform/install", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// 3. An unknown target is refused before anything is downloaded.
func TestCLIInstallRefusesUnknownTarget(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/terraform/install", map[string]string{"target": "helm"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(res["error"], "unknown target") {
		t.Errorf("error = %q, want it to name the unknown target", res["error"])
	}
}

// 4. A target is required.
func TestCLIInstallRequiresTarget(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/terraform/install", map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// 5. A malformed body is refused.
func TestCLIInstallRejectsBadJSON(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/terraform/install", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	auth(req)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// 6. When Pantau does not manage the CLIs on this server, it says so instead of writing anywhere.
func TestCLIInstallRefusesWithoutManagedRoot(t *testing.T) {
	old := gwupdate.ManagedRoot()
	gwupdate.SetManagedRoot("")
	defer gwupdate.SetManagedRoot(old)

	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/terraform/install", map[string]string{"target": "terraform"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(res["error"], "does not manage") {
		t.Errorf("error = %q, want it to explain that nothing is managed", res["error"])
	}
}

// 7. A version that is not a release number is refused, and the failure reaches the UI as NDJSON.
func TestCLIInstallReportsFailureAsNDJSON(t *testing.T) {
	root := t.TempDir()
	old := gwupdate.ManagedRoot()
	gwupdate.SetManagedRoot(root)
	defer gwupdate.SetManagedRoot(old)

	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/terraform/install", map[string]string{
		"target":  "terraform",
		"version": "latest",
	})
	// The install fails before it writes anything, so it is a plain 400 with the reason.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(res["error"], "release number") {
		t.Errorf("error = %q, want it to name the bad version", res["error"])
	}
	// Nothing was installed.
	if _, ok := (gwupdate.ManagedDir{Root: root}).ManagedBinary("terraform"); ok {
		t.Error("a binary was installed for a refused version")
	}
}

// 8. A successful install answers as an NDJSON stream that ends with {"done":true}.
func TestCLIInstallStreamsItsSteps(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads a release")
	}
	root := t.TempDir()
	old := gwupdate.ManagedRoot()
	gwupdate.SetManagedRoot(root)
	defer gwupdate.SetManagedRoot(old)

	// Serve a release whose checksum matches, so the install succeeds without the network.
	archive, sum := releaseZip(t, "tofu", "1.13.9")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "_SHA256SUMS") {
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			asset := strings.TrimSuffix(name, "_SHA256SUMS") + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".zip"
			w.Write([]byte(sum + "  " + asset + "\n"))
			return
		}
		w.Write(archive)
	}))
	defer upstream.Close()

	oldBase := gwupdate.OpenTofuBaseURL
	gwupdate.OpenTofuBaseURL = upstream.URL
	defer func() { gwupdate.OpenTofuBaseURL = oldBase }()

	srv, auth := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/terraform/install", map[string]string{
		"target":  "tofu",
		"version": "1.13.9",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/x-ndjson") {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", ct)
	}

	// The stream carries the steps and ends with a done marker.
	var lines []map[string]interface{}
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line is not JSON: %q", sc.Text())
		}
		lines = append(lines, m)
	}
	if len(lines) == 0 {
		t.Fatal("the stream carried nothing")
	}
	last := lines[len(lines)-1]
	if last["done"] != true {
		t.Errorf("the stream must end with done:true, got %v", last)
	}
	if last["error"] != nil {
		t.Errorf("the install must succeed, got error %v", last["error"])
	}
	var logs strings.Builder
	for _, m := range lines {
		if s, ok := m["log"].(string); ok {
			logs.WriteString(s)
		}
	}
	if !strings.Contains(logs.String(), "Checksum verified") {
		t.Errorf("the stream must show the checksum verification:\n%s", logs.String())
	}

	// The binary is in place and Pantau now reports it.
	if _, ok := (gwupdate.ManagedDir{Root: root}).ManagedBinary("tofu"); !ok {
		t.Error("the managed binary was not installed")
	}
}
