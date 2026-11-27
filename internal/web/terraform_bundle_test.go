package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func terraformBundle(t *testing.T) string {
	t.Helper()
	b, err := buildAppBundle("terraform", ".css")
	if err != nil {
		t.Fatalf("build terraform css bundle: %v", err)
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
	t.Parallel()
	srv := NewServer(nil, nil, nil)
	for _, tc := range []struct{ path, ctype string }{
		{"/app/terraform.js", "javascript"},
		{"/app/terraform.css", "text/css"},
	} {
		plain := fetchAppAsset(t, srv, tc.path, "", "")
		if plain.Code != http.StatusOK || !strings.Contains(plain.Header().Get("Content-Type"), tc.ctype) {
			t.Fatalf("%s: code=%d type=%q", tc.path, plain.Code, plain.Header().Get("Content-Type"))
		}
		if plain.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: expected no-cache so a new build is picked up", tc.path)
		}
		etag := plain.Header().Get("ETag")
		if etag == "" || plain.Body.Len() < 1000 {
			t.Fatalf("%s: etag=%q size=%d", tc.path, etag, plain.Body.Len())
		}
		gz := fetchAppAsset(t, srv, tc.path, "gzip", "")
		if gz.Header().Get("Content-Encoding") != "gzip" || gz.Header().Get("ETag") != etag {
			t.Fatalf("%s: gzip variant must keep the ETag", tc.path)
		}
		zr, err := gzip.NewReader(bytes.NewReader(gz.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(zr)
		if !bytes.Equal(raw, plain.Body.Bytes()) {
			t.Fatalf("%s: gunzipped body differs from identity body", tc.path)
		}
		if nm := fetchAppAsset(t, srv, tc.path, "", etag); nm.Code != http.StatusNotModified || nm.Body.Len() != 0 {
			t.Fatalf("%s: expected empty 304, got %d (%d bytes)", tc.path, nm.Code, nm.Body.Len())
		}
	}
}

func TestTerraformBundleContract(t *testing.T) {
	t.Parallel()
	js := terraformJSBundle(t)
	css := terraformBundle(t)

	// 5 top-level tabs registered via Tf.registerTab and mounted with the standard panel/tab IDs.
	tabRe := regexp.MustCompile(`registerTab\(\{\s*id:\s*'(\w+)'`)
	got := map[string]bool{}
	for _, m := range tabRe.FindAllStringSubmatch(js, -1) {
		got[m[1]] = true
	}
	for _, id := range []string{"workspaces", "runs", "variables", "registry", "governance"} {
		if !got[id] {
			t.Errorf("top-level tab %q is not registered via Tf.registerTab", id)
		}
	}

	// Shell, Gate, Drawer, and CLI update card contract.
	for _, want := range []string{
		`id="tfTabs"`, `role="tablist"`, `role="tabpanel"`,
		`id="tfGateStatus"`, `id="tfGateBanner"`, `id="btnTfRefresh"`,
		`id="tfDrawerRoot"`, `id="tfDrawer"`, `id="tfDrawerWide"`, `id="tfWsTabs"`, `id="tfWsBody"`,
		`id="tfCliUpdateCard"`, `id="tfCliUpdateCheck"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("terraform.js bundle missing %q", want)
		}
	}

	// Cross-module window.Tf contract from .scratch/terraform-ui-redesign/spec.md.
	for _, want := range []string{
		"Tf.workspaces =", "Tf.overview =", "Tf.config =", "Tf.runs =",
		"Tf.state =", "Tf.variables =", "Tf.registry =", "Tf.governance =",
		"Tf.drawer =", "Tf.dlg =",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("terraform.js bundle does not provide %q", want)
		}
	}

	// No inline HTML event handlers (onclick="..."): all interactions must use event delegation.
	if m := regexp.MustCompile(`\bon(click|change|input|submit|keydown|keyup)\s*=\s*\\?["']`).FindString(js); m != "" {
		t.Errorf("terraform.js bundle contains an inline HTML handler: %s", m)
	}

	// CSS bundle: no stray semicolons, must define drawer, container queries, and mobile breakpoint.
	if strings.Contains(css, "\n;\n") {
		t.Errorf("terraform.css bundle must not contain a stray semicolon separator")
	}
	for _, want := range []string{
		".tf-drawer-root", ".tf-drawer-root.open", ".tf-drawer-backdrop", ".tf-drawer", ".tf-drawer.wide",
		".tf-drawer-head", ".tf-drawer-tabs", ".tf-drawer-body", ".tf-ov-grid",
		"container-name: tfbody", "@container tfbody", "@media (max-width: 900px)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("terraform.css missing %q", want)
		}
	}

	// i18n parity: every referenced tf_* key must be defined in BOTH EN and ID dictionaries.
	defRe := regexp.MustCompile("(?m)(?:^|[{,])\\s*['\"]?(tf_[a-z0-9_]+)['\"]?\\s*:\\s*['\"`]")
	defs := map[string]int{}
	for _, m := range defRe.FindAllStringSubmatch(js, -1) {
		defs[m[1]]++
	}
	refRe := regexp.MustCompile(`['"](tf_[a-z0-9_]+)['"]\s*[,)\]]`)
	var missing []string
	for _, m := range refRe.FindAllStringSubmatch(js, -1) {
		if defs[m[1]] < 2 {
			missing = append(missing, m[1])
		}
	}
	if len(missing) > 0 {
		t.Errorf("i18n keys without both EN and ID text (%d): %v", len(missing), uniqueStrings(missing))
	}
	for k, n := range defs {
		if n%2 != 0 {
			t.Errorf("i18n key %q is defined %d times (EN and ID must pair up)", k, n)
		}
	}
}

func TestTerraformBundleSyntax(t *testing.T) {
	t.Parallel()
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
