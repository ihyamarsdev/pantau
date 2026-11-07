package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeRegistryServer(t *testing.T) *httptest.Server {
	t.Helper()
	body := map[string]string{
		"/v1/modules/a/vpc/aws":       `{"id":"a/vpc/aws/6.1.0","namespace":"a","name":"vpc","provider":"aws","version":"6.1.0","versions":["6.0.0","6.1.0"]}`,
		"/v1/modules/a/ok/aws":        `{"id":"a/ok/aws/2.3.0","namespace":"a","name":"ok","provider":"aws","version":"2.3.0","versions":["2.3.0"]}`,
		"/v1/providers/hashicorp/aws": `{"version":"5.2.0","versions":["5.1.0","5.2.0"]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := body[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRegistryCheckInline(t *testing.T) {
	reg := fakeRegistryServer(t)
	srv, auth, id := newToolsWS(t, "inline", map[string]string{
		"main.tf": "module \"vpc\" {\n  source  = \"a/vpc/aws\"\n  version = \"~> 5.0\"\n}\n" +
			"module \"ok\" {\n  source  = \"a/ok/aws\"\n  version = \"~> 2.0\"\n}\n" +
			"module \"gone\" {\n  source = \"a/gone/aws\"\n}\n" +
			"module \"git\" {\n  source = \"git::https://example.com/x.git\"\n}\n",
		"versions.tf": "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"= 4.0.0\"\n    }\n  }\n}\n",
	})
	if err := srv.db.SetSetting(tfRegistrySetting, reg.URL); err != nil {
		t.Fatal(err)
	}
	rec := doTFReq(t, srv, auth, http.MethodGet, toolsURL(id, "registry/check"), nil)
	if rec.Code != 200 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Supported bool `json:"supported"`
		Items     []struct {
			Kind, Name, File, Latest, Status, Suggested string
			Line                                        int
		} `json:"items"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	type want struct {
		name, status, latest, suggested string
		line                            int
	}
	wants := []want{
		{"vpc", "newer_outside_constraint", "6.1.0", "~> 6.1", 3},
		{"ok", "up_to_date", "2.3.0", "~> 2.3", 7},
		{"gone", "unknown", "", "", 9},
		{"git", "unknown", "", "", 12},
		{"aws", "outdated", "5.2.0", "~> 5.2", 5},
	}
	if !out.Supported || len(out.Items) != len(wants) || len(out.Errors) != 1 {
		t.Fatalf("got %+v", out)
	}
	for i, w := range wants {
		it := out.Items[i]
		if it.Name != w.name || it.Status != w.status || it.Latest != w.latest || it.Suggested != w.suggested || it.Line != w.line {
			t.Errorf("item %d = %+v, want %+v", i, it, w)
		}
	}
	if out.Items[4].Kind != "provider" || out.Items[4].File != "versions.tf" || out.Items[0].Kind != "module" {
		t.Errorf("kind/file wrong: %+v", out.Items)
	}
}

func TestRegistryCheckGitUnsupported(t *testing.T) {
	srv, auth, id := newToolsWS(t, "git", nil)
	rec := doTFReq(t, srv, auth, http.MethodGet, toolsURL(id, "registry/check"), nil)
	var out struct {
		Supported bool  `json:"supported"`
		Items     []any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 || out.Supported || out.Items == nil || len(out.Items) != 0 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
}
