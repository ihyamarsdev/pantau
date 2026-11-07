package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"pantau/internal/store"
)

func newToolsWS(t *testing.T, source string, files map[string]string) (*Server, func(*http.Request), int64) {
	t.Helper()
	srv, db, _, auth := setupTerraformTestServer(t)
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatal(err)
	}
	fj, _ := json.Marshal(files)
	id, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "tools-ws", Source: source, Files: string(fj), GitURL: "https://example.com/r.git"})
	if err != nil {
		t.Fatal(err)
	}
	return srv, auth, id
}

func toolsURL(id int64, tail string) string {
	return fmt.Sprintf("/api/terraform/workspaces/%d/%s", id, tail)
}

func skipNoTF(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
}

func TestTFToolsDetectVariables(t *testing.T) {
	srv, auth, id := newToolsWS(t, "inline", map[string]string{
		"variables.tf": "variable \"region\" { default = \"eu\" }\nvariable \"name\" {\n  type = string\n  sensitive = true\n}\n",
	})
	if _, err := srv.db.CreateTFVariable(&store.TFVariable{WorkspaceID: id, Key: "region", Value: "x", Category: "terraform"}); err != nil {
		t.Fatal(err)
	}
	// an env variable with the same key as a declared one does not count as existing
	if _, err := srv.db.CreateTFVariable(&store.TFVariable{WorkspaceID: id, Key: "name", Value: "x", Category: "env"}); err != nil {
		t.Fatal(err)
	}
	rec := doTFReq(t, srv, auth, http.MethodGet, toolsURL(id, "variables/detect"), nil)
	if rec.Code != 200 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Supported bool `json:"supported"`
		Variables []struct {
			Name       string `json:"name"`
			Default    string `json:"default"`
			HasDefault bool   `json:"has_default"`
			Sensitive  bool   `json:"sensitive"`
			File       string `json:"file"`
			Exists     bool   `json:"exists"`
		} `json:"variables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Supported || len(out.Variables) != 2 {
		t.Fatalf("got %+v", out)
	}
	if v := out.Variables[0]; v.Name != "region" || !v.Exists || !v.HasDefault || v.Default != `"eu"` || v.File != "variables.tf" {
		t.Fatalf("region = %+v", v)
	}
	if v := out.Variables[1]; v.Name != "name" || v.Exists || !v.Sensitive {
		t.Fatalf("name = %+v", v)
	}
}

func TestTFToolsDetectGitUnsupported(t *testing.T) {
	srv, auth, id := newToolsWS(t, "git", nil)
	rec := doTFReq(t, srv, auth, http.MethodGet, toolsURL(id, "variables/detect"), nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"supported":false`) || !strings.Contains(rec.Body.String(), `"variables":[]`) {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
}

func TestTFToolsFmt(t *testing.T) {
	skipNoTF(t)
	srv, auth, id := newToolsWS(t, "inline", map[string]string{})
	rec := doTFReq(t, srv, auth, http.MethodPost, toolsURL(id, "tools/fmt"), map[string]any{"files": map[string]string{
		"main.tf":   "resource \"terraform_data\" \"a\" {\n      input   = \"x\"\n}\n",
		"bad.tf":    "resource {{{\n",
		"notes.txt": "  keep   me ",
	}})
	if rec.Code != 200 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Files   map[string]string `json:"files"`
		Changed []string          `json:"changed"`
		Errors  map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Files["main.tf"] != "resource \"terraform_data\" \"a\" {\n  input = \"x\"\n}\n" || len(out.Changed) != 1 || out.Changed[0] != "main.tf" {
		t.Fatalf("fmt result: %+v", out)
	}
	if out.Errors["bad.tf"] == "" || out.Files["notes.txt"] != "  keep   me " {
		t.Fatalf("fmt result: %+v", out)
	}
}

func TestTFToolsGitRejected(t *testing.T) {
	srv, auth, id := newToolsWS(t, "git", nil)
	for _, p := range []string{"tools/fmt", "tools/validate"} {
		rec := doTFReq(t, srv, auth, http.MethodPost, toolsURL(id, p), map[string]any{"files": map[string]string{"a.tf": ""}})
		if rec.Code != 400 {
			t.Fatalf("%s: code %d", p, rec.Code)
		}
	}
	srv, auth, id = newToolsWS(t, "inline", nil)
	rec := doTFReq(t, srv, auth, http.MethodPost, toolsURL(id, "tools/fmt"), map[string]any{"files": map[string]string{"../a.tf": ""}})
	if rec.Code != 400 {
		t.Fatalf("bad file name: code %d", rec.Code)
	}
}

func TestTFToolsValidate(t *testing.T) {
	skipNoTF(t)
	srv, auth, id := newToolsWS(t, "inline", map[string]string{})
	post := func(content string) (ok bool, diags []map[string]any, initErr string) {
		rec := doTFReq(t, srv, auth, http.MethodPost, toolsURL(id, "tools/validate"), map[string]any{"files": map[string]string{"main.tf": content}})
		if rec.Code != 200 {
			t.Fatalf("code %d: %s", rec.Code, rec.Body)
		}
		var out struct {
			OK          bool             `json:"ok"`
			InitError   string           `json:"init_error"`
			Diagnostics []map[string]any `json:"diagnostics"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.OK, out.Diagnostics, out.InitError
	}
	if ok, d, ie := post("resource \"terraform_data\" \"a\" {\n  input = \"x\"\n}\n"); !ok || len(d) != 0 || ie != "" {
		t.Fatalf("valid config: ok=%v diags=%v init=%q", ok, d, ie)
	}
	ok, d, _ := post("resource \"terraform_data\" \"a\" {\n  input = var.missing\n}\n")
	if ok || len(d) == 0 || d[0]["file"] != "main.tf" || d[0]["line"] != float64(2) {
		t.Fatalf("invalid config: ok=%v diags=%v", ok, d)
	}
}

func TestTFToolsValidateBusy(t *testing.T) {
	srv, auth, id := newToolsWS(t, "inline", map[string]string{})
	tfValidateSem <- struct{}{}
	tfValidateSem <- struct{}{}
	defer func() { <-tfValidateSem; <-tfValidateSem }()
	rec := doTFReq(t, srv, auth, http.MethodPost, toolsURL(id, "tools/validate"), map[string]any{"files": map[string]string{"main.tf": ""}})
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
}
