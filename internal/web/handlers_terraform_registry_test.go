package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const regTestModule = `{"id":"ns/vpc/aws/2.1.0","namespace":"ns","name":"vpc","version":"2.1.0","provider":"aws","description":"d","source":"https://example.com/r","published_at":"2026-01-01T00:00:00Z","downloads":9,"verified":true,
"providers":["aws"],"versions":["1.0.0","2.0.0","2.1.0"],
"root":{"readme":"# <b>hi</b>","inputs":[{"name":"name","type":"string","description":"The name","default":null,"required":true},{"name":"opt","type":"bool","description":"","default":"false","required":false}],
"outputs":[{"name":"id","description":"ID"}],"dependencies":[],"provider_dependencies":[{"name":"aws","namespace":"hashicorp","source":"hashicorp/aws","version":">= 5"}]}}`

// regEnv returns a gate-enabled server pointed (through the DB setting) at a fake registry.
func regEnv(t *testing.T) (*Server, func(*http.Request), func(method, url string, body any) *httptest.ResponseRecorder) {
	t.Helper()
	srv, db, _, auth := setupTerraformTestServer(t)
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/modules/search" && r.URL.Query().Get("q") == "boom":
			http.Error(w, "internal secret", 500)
		case r.URL.Path == "/v1/modules/search":
			if r.URL.Query().Get("limit") == "1" && r.URL.Query().Get("q") == "" {
				_, _ = w.Write([]byte(`{"modules":[]}`))
				return
			}
			if r.URL.Query().Get("q") != "vpc" || r.URL.Query().Get("provider") != "aws" || r.URL.Query().Get("limit") != "50" {
				t.Errorf("search query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"meta":{"next_offset":50},"modules":[{"id":"ns/vpc/aws/2.1.0","namespace":"ns","name":"vpc","provider":"aws","version":"2.1.0","downloads":9}]}`))
		case r.URL.Path == "/v1/modules/ns/vpc/aws":
			_, _ = w.Write([]byte(regTestModule))
		case r.URL.Path == "/v1/providers/hashicorp/aws":
			_, _ = w.Write([]byte(`{"namespace":"hashicorp","name":"aws","version":"6.72.0","tier":"official","versions":["6.71.0","6.72.0"]}`))
		case r.URL.Path == "/v2/providers":
			_, _ = w.Write([]byte(`{"data":[{"attributes":{"name":"aws","namespace":"hashicorp","full-name":"hashicorp/aws","tier":"official"}}],"meta":{"pagination":{"next-page":null}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	do := func(method, url string, body any) *httptest.ResponseRecorder {
		return doTFReq(t, srv, auth, method, url, body)
	}
	if w := do("PUT", "/api/terraform/registry/settings", map[string]string{"base_url": fake.URL + "/"}); w.Code != 200 {
		t.Fatalf("PUT settings: %d %s", w.Code, w.Body)
	}
	return srv, auth, do
}

func regJSON(t *testing.T, w *httptest.ResponseRecorder, code int) map[string]any {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status %d (want %d): %s", w.Code, code, w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("json: %v: %s", err, w.Body)
	}
	return m
}

func TestRegistrySettingsEndpoints(t *testing.T) {
	srv, db, _, auth := setupTerraformTestServer(t)
	do := func(method, url string, body any) *httptest.ResponseRecorder {
		return doTFReq(t, srv, auth, method, url, body)
	}
	if w := do("GET", "/api/terraform/registry/settings", nil); w.Code != http.StatusForbidden {
		t.Fatalf("gate off: %d", w.Code)
	}
	_ = db.SetTerraformEnabled(true)
	m := regJSON(t, do("GET", "/api/terraform/registry/settings", nil), 200)
	if m["base_url"] != "https://registry.terraform.io" || m["default_url"] != "https://registry.terraform.io" {
		t.Fatalf("defaults: %v", m)
	}
	for _, bad := range []string{"http://registry.example.com", "https://u:p@registry.example.com", "https://r.example.com/?a=1", "ftp://r.example.com", "nonsense"} {
		if m := regJSON(t, do("PUT", "/api/terraform/registry/settings", map[string]string{"base_url": bad}), 400); m["error"] == "" {
			t.Errorf("%q: no error message", bad)
		}
	}
	if v, _ := db.GetSetting("tf_registry_url"); v != "" {
		t.Fatalf("invalid PUT stored %q", v)
	}
	m = regJSON(t, do("PUT", "/api/terraform/registry/settings", map[string]string{"base_url": "https://mirror.example.com/"}), 200)
	if m["base_url"] != "https://mirror.example.com" {
		t.Fatalf("trailing slash: %v", m)
	}
	if v, _ := db.GetSetting("tf_registry_url"); v != "https://mirror.example.com" {
		t.Fatalf("stored %q", v)
	}
	m = regJSON(t, do("PUT", "/api/terraform/registry/settings", map[string]string{"base_url": ""}), 200)
	if v, _ := db.GetSetting("tf_registry_url"); v != "" || m["base_url"] != "https://registry.terraform.io" {
		t.Fatalf("reset: %q %v", v, m)
	}
	if w := do("DELETE", "/api/terraform/registry/settings", nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", w.Code)
	}
}

func TestRegistrySettingsTestEndpoint(t *testing.T) {
	_, _, do := regEnv(t)
	m := regJSON(t, do("POST", "/api/terraform/registry/settings/test", nil), 200)
	if m["ok"] != true || m["error"] != nil {
		t.Fatalf("ok case: %v", m)
	}
	if _, ok := m["latency_ms"]; !ok {
		t.Fatal("no latency_ms")
	}
	m = regJSON(t, do("POST", "/api/terraform/registry/settings/test", map[string]string{"base_url": "http://127.0.0.1:1"}), 200)
	if m["ok"] != false || m["error"] == "" {
		t.Fatalf("unreachable: %v", m)
	}
	regJSON(t, do("POST", "/api/terraform/registry/settings/test", map[string]string{"base_url": "http://example.com"}), 400)
	if w := do("GET", "/api/terraform/registry/settings/test", nil); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET test: %d", w.Code)
	}
}

func TestRegistryBrowseEndpoints(t *testing.T) {
	_, _, do := regEnv(t)
	m := regJSON(t, do("GET", "/api/terraform/registry/modules?q=vpc&provider=aws&limit=500", nil), 200)
	if mods := m["modules"].([]any); len(mods) != 1 || m["next_offset"] != float64(50) || m["total_hint"] != float64(1) {
		t.Fatalf("modules: %v", m)
	}
	m = regJSON(t, do("GET", "/api/terraform/registry/modules/ns/vpc/aws?module_name=net&declare_vars=1", nil), 200)
	mod := m["module"].(map[string]any)
	snip := m["snippet"].(map[string]any)
	if mod["version"] != "2.1.0" || mod["verified"] != true || m["versions"].([]any)[0] != "2.1.0" || len(m["inputs"].([]any)) != 2 ||
		m["readme"] != "# <b>hi</b>" || m["readme_truncated"] != false || len(m["providers"].([]any)) != 1 {
		t.Fatalf("detail: %v", m)
	}
	if main := snip["main"].(string); !strings.Contains(main, `module "net" {`) || !strings.Contains(main, `source  = "ns/vpc/aws"`) ||
		!strings.Contains(main, `version = "~> 2.1"`) || strings.Contains(main, "opt") {
		t.Fatalf("snippet main:\n%s", main)
	}
	if v := snip["variables"].(string); !strings.Contains(v, `variable "name" {`) {
		t.Fatalf("snippet vars: %q", v)
	}
	if snip = regJSON(t, do("GET", "/api/terraform/registry/modules/ns/vpc/aws", nil), 200)["snippet"].(map[string]any); snip["variables"] != "" {
		t.Fatalf("variables without declare_vars: %v", snip)
	}
	m = regJSON(t, do("GET", "/api/terraform/registry/providers?q=aws", nil), 200)
	if ps := m["providers"].([]any); len(ps) != 1 || ps[0].(map[string]any)["full_name"] != "hashicorp/aws" || m["next_page"] != float64(0) {
		t.Fatalf("providers: %v", m)
	}
	m = regJSON(t, do("GET", "/api/terraform/registry/providers/hashicorp/aws?version=6.0.1", nil), 200)
	if m["latest"] != "6.72.0" || m["versions"].([]any)[0] != "6.72.0" || !strings.HasSuffix(m["docs_url"].(string), "/providers/hashicorp/aws/latest/docs") ||
		!strings.Contains(m["snippet"].(string), `version = "~> 6.0"`) || !strings.Contains(m["snippet"].(string), `source  = "hashicorp/aws"`) {
		t.Fatalf("provider: %v", m)
	}
}

func TestRegistryBrowseErrors(t *testing.T) {
	_, _, do := regEnv(t)
	regJSON(t, do("GET", "/api/terraform/registry/modules/ns/nope/aws", nil), 404)
	regJSON(t, do("GET", "/api/terraform/registry/modules/ns/a%20b/aws", nil), 400)
	regJSON(t, do("GET", "/api/terraform/registry/modules/ns/vpc/aws?version=1%202", nil), 400)
	regJSON(t, do("GET", "/api/terraform/registry/modules?q=vpc&provider=..%2Fx", nil), 400)
	m := regJSON(t, do("GET", "/api/terraform/registry/modules?q=boom", nil), 502)
	if strings.Contains(m["error"].(string), "secret") {
		t.Fatalf("upstream body leaked: %v", m)
	}
	if w := do("POST", "/api/terraform/registry/modules", nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST modules: %d", w.Code)
	}
	if w := do("GET", "/api/terraform/registry/unknown", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", w.Code)
	}
}
