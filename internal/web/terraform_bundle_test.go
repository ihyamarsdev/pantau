package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func terraformBundle(t *testing.T) string {
	t.Helper()
	b, err := buildAppBundle("terraform", ".css")
	if err != nil {
		t.Fatalf("build terraform bundle: %v", err)
	}
	return string(b.body)
}

func terraformJSBundle(t *testing.T) string {
	t.Helper()
	b, err := buildAppBundle("terraform", ".js")
	if err != nil {
		t.Fatalf("build terraform js bundle: %v", err)
	}
	return string(b.body)
}

func TestTerraformBundleServing(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	plain := fetchAppAsset(t, srv, "/app/terraform.css", "", "")
	if plain.Code != http.StatusOK || !strings.Contains(plain.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("/app/terraform.css: code=%d type=%q", plain.Code, plain.Header().Get("Content-Type"))
	}
	if plain.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected no-cache so a new build is picked up")
	}
	etag := plain.Header().Get("ETag")
	if etag == "" || plain.Body.Len() < 1000 {
		t.Fatalf("etag=%q size=%d", etag, plain.Body.Len())
	}
	if !strings.Contains(plain.Body.String(), ".tf-drawer") {
		t.Errorf("bundle missing .tf-drawer rules")
	}
	gz := fetchAppAsset(t, srv, "/app/terraform.css", "gzip", "")
	if gz.Header().Get("Content-Encoding") != "gzip" || gz.Header().Get("ETag") != etag {
		t.Fatalf("gzip variant must keep the ETag")
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(zr)
	if !bytes.Equal(raw, plain.Body.Bytes()) {
		t.Fatalf("gunzipped body differs from identity body")
	}
	nm := fetchAppAsset(t, srv, "/app/terraform.css", "", etag)
	if nm.Code != http.StatusNotModified || nm.Body.Len() != 0 {
		t.Fatalf("expected empty 304, got %d (%d bytes)", nm.Code, nm.Body.Len())
	}

	plain = fetchAppAsset(t, srv, "/app/terraform.js", "", "")
	if plain.Code != http.StatusOK || !strings.Contains(plain.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("/app/terraform.js: code=%d type=%q", plain.Code, plain.Header().Get("Content-Type"))
	}
	etag = plain.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("/app/terraform.js: missing ETag")
	}
	gz = fetchAppAsset(t, srv, "/app/terraform.js", "gzip", "")
	if gz.Header().Get("Content-Encoding") != "gzip" || gz.Header().Get("ETag") != etag {
		t.Fatalf("/app/terraform.js: gzip variant must keep the ETag")
	}
	zr, err = gzip.NewReader(bytes.NewReader(gz.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(zr)
	if !bytes.Equal(raw, plain.Body.Bytes()) {
		t.Fatalf("/app/terraform.js: gunzipped body differs from identity body")
	}
	nm = fetchAppAsset(t, srv, "/app/terraform.js", "", etag)
	if nm.Code != http.StatusNotModified || nm.Body.Len() != 0 {
		t.Fatalf("/app/terraform.js: expected empty 304, got %d (%d bytes)", nm.Code, nm.Body.Len())
	}
	if !strings.Contains(plain.Body.String(), "window.TfUI") {
		t.Errorf("/app/terraform.js missing the TfUI namespace stub")
	}
}

// Contract classes the drawer markup and the Overview tab rely on.
func TestTerraformBundleContract(t *testing.T) {
	css := terraformBundle(t)
	for _, want := range []string{
		".tf-drawer-root", ".tf-drawer-root.open", ".tf-drawer-backdrop", ".tf-drawer", ".tf-drawer.wide",
		".tf-drawer-head", ".tf-drawer-tabs", ".tf-drawer-body", ".tf-drawer-wide", ".tf-ov-grid", ".tf-ov-card",
		"#tfVarsetsList", "#tfPoliciesList", "#tfPanelWorkspaces > div:first-child > div:first-child",
		".ans-section-panel", ".ans-sched-tbl", "#tfWsTableWrap",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("terraform.css missing %q", want)
		}
	}
	for _, want := range []string{"container-name: tfbody", "@container tfbody", "@media (max-width: 900px)"} {
		if !strings.Contains(css, want) {
			t.Errorf("terraform.css missing %q", want)
		}
	}
}

func TestTerraformBundleSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the JavaScript syntax check")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "terraform.js")
	if err := os.WriteFile(file, []byte(terraformJSBundle(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Fatalf("bundle has a syntax error: %v\n%s", err, out)
	}
}
