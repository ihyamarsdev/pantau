package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func fetchAppAsset(t *testing.T, srv *Server, path, acceptEncoding, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func ansibleBundle(t *testing.T) string {
	t.Helper()
	b, err := buildAppBundle("ansible", ".js")
	if err != nil {
		t.Fatalf("build ansible bundle: %v", err)
	}
	return string(b.body)
}

func TestAnsibleSectionIsMountedByBundle(t *testing.T) {
	html := string(embeddedHTML)
	for _, want := range []string{
		`id="sectionAnsible"`, `/app/ansible.css`, `/app/ansible.js`, `id="navItemAnsible"`, `switchSideNav('ansible')`,
		`refreshAnsibleSection()`, `"nav_ansible": "Ansible"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	// The old in-page implementation (5 modals + inline tabs) must not come back.
	for _, gone := range []string{
		`id="ansiblePlaybookModal"`, `id="ansibleRunLaunchModal"`, `id="ansibleRunModal"`, `id="ansibleScheduleModal"`,
		`id="ansibleTabs"`, `function openAnsiblePlaybookModal`, `function submitAnsibleRunLaunch`, `"ansible_tab_playbooks"`,
	} {
		if strings.Contains(html, gone) {
			t.Errorf("index.html still contains removed Ansible UI marker %q", gone)
		}
	}
}

func TestAnsibleBundleServing(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	for _, tc := range []struct{ path, ctype string }{
		{"/app/ansible.js", "javascript"},
		{"/app/ansible.css", "text/css"},
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

func TestAnsibleBundleContract(t *testing.T) {
	js := ansibleBundle(t)

	// Tabs registered by the modules and the panel/tab id scheme the shell derives from them.
	tabRe := regexp.MustCompile(`registerTab\(\{\s*id:\s*'(\w+)'`)
	got := map[string]bool{}
	for _, m := range tabRe.FindAllStringSubmatch(js, -1) {
		got[m[1]] = true
	}
	for _, id := range []string{"playbooks", "runs", "hosts", "resources", "settings"} {
		if !got[id] {
			t.Errorf("tab %q is not registered", id)
		}
	}
	for _, want := range []string{`'ansiblePanel' + `, `'tabAnsible' + `, `id="ansibleTabs"`, `role="tablist"`, `role="tabpanel"`, `id="ansibleGateBanner"`, `id="btnAnsibleRefresh"`} {
		if !strings.Contains(js, want) {
			t.Errorf("core shell is missing %q", want)
		}
	}

	// Cross-module contract from .scratch/ansible-ui-redesign/spec.md.
	for _, want := range []string{
		"Ans.playbooks =", "Ans.launch =", "Ans.editor =", "Ans.runs =", "Ans.schedules =", "Ans.hosts =",
		"openViewer", "mountHistory", "mountAll", "openFacts", "openAdhoc",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("bundle does not provide %q", want)
		}
	}

	// No inline HTML handlers (onclick="..."): they bypass the escaping and delegation rules of the modules.
	if m := regexp.MustCompile(`\bon(click|change|input|submit|keydown|keyup)\s*=\s*\\?["']`).FindString(js); m != "" {
		t.Errorf("bundle contains an inline HTML handler: %s", m)
	}
	// CSS modules are concatenated; a stray ';' between them would drop the next rule.
	if css, err := buildAppBundle("ansible", ".css"); err != nil || strings.Contains(string(css.body), "\n;\n") {
		t.Errorf("css bundle must not contain a stray semicolon separator (err=%v)", err)
	}

	// i18n parity: every referenced ans_* key is defined in BOTH dictionaries (>= 2 definitions).
	// dictionary entries only: preceded by `{`, `,` or a line start, so ternaries like `? 'ans_x' : 'ans_y'` do not count
	defRe := regexp.MustCompile("(?m)(?:^|[{,])\\s*['\"]?(ans_[a-z0-9_]+)['\"]?\\s*:\\s*['\"`]")
	defs := map[string]int{}
	for _, m := range defRe.FindAllStringSubmatch(js, -1) {
		defs[m[1]]++
	}
	refRe := regexp.MustCompile(`['"](ans_[a-z0-9_]+)['"]\s*[,)\]]`)
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

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func TestAnsibleBundleSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the JavaScript syntax check")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "ansible.js")
	if err := os.WriteFile(file, []byte(ansibleBundle(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Fatalf("bundle has a syntax error: %v\n%s", err, out)
	}
}
