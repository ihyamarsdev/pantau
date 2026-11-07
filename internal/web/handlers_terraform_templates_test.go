package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTerraformTemplates_Endpoint(t *testing.T) {
	srv, authReq := setupTerraformCatalogTestServer(t)
	get := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/terraform/templates", nil)
		authReq(req)
		rec := httptest.NewRecorder()
		srv.handleTerraformTemplates(rec, req)
		return rec
	}

	rec := get(http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Templates []struct {
			ID        string                  `json:"id"`
			Provider  string                  `json:"provider"`
			Variables []struct{ Name string } `json:"variables"`
			Files     map[string]string       `json:"files"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Templates) != 9 || resp.Templates[0].ID != "hello-world" || resp.Templates[0].Provider != "none" {
		t.Fatalf("unexpected templates: %+v", resp.Templates)
	}
	for _, tp := range resp.Templates {
		if tp.Files["main.tf"] == "" || len(tp.Variables) == 0 {
			t.Errorf("%s: missing main.tf or variables", tp.ID)
		}
	}

	if c := get(http.MethodPost).Code; c != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d, want 405", c)
	}
	if err := srv.db.SetTerraformEnabled(false); err != nil {
		t.Fatal(err)
	}
	if c := get(http.MethodGet).Code; c != http.StatusForbidden {
		t.Errorf("gate off status %d, want 403", c)
	}
}
