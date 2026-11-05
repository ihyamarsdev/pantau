package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

func setupTerraformCatalogTestServer(t *testing.T) (*Server, func(*http.Request)) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_tf_catalog.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := NewServer(db, nil, nil)
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatalf("failed to enable terraform gate: %v", err)
	}

	token := "tf_catalog_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, authReq
}

func doCatalogRequest(t *testing.T, srv *Server, authReq func(*http.Request), method, url string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authReq != nil {
		authReq(req)
	}
	rec := httptest.NewRecorder()

	// Policies routes
	if req.URL.Path == "/api/terraform/policies" {
		srv.handleTerraformPolicies(rec, req)
		return rec
	}
	if len(req.URL.Path) > len("/api/terraform/policies/") && req.URL.Path[:len("/api/terraform/policies/")] == "/api/terraform/policies/" {
		srv.handleTerraformPolicyRoute(rec, req)
		return rec
	}

	// Modules routes
	if req.URL.Path == "/api/terraform/modules" {
		srv.handleTerraformModules(rec, req)
		return rec
	}
	if len(req.URL.Path) > len("/api/terraform/modules/") && req.URL.Path[:len("/api/terraform/modules/")] == "/api/terraform/modules/" {
		srv.handleTerraformModuleRoute(rec, req)
		return rec
	}

	return rec
}

func TestTerraformPolicy_ValidationAndGate(t *testing.T) {
	srv, auth := setupTerraformCatalogTestServer(t)

	// 1. Policy validation error -> 400
	rec := doCatalogRequest(t, srv, auth, http.MethodPost, "/api/terraform/policies", map[string]interface{}{
		"name":        "invalid-policy",
		"kind":        "unknown_kind", // invalid kind
		"enforcement": "hard",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid policy kind, got %d: %s", rec.Code, rec.Body.String())
	}

	// Valid policy
	rec = doCatalogRequest(t, srv, auth, http.MethodPost, "/api/terraform/policies", map[string]interface{}{
		"name":        "deny-destroy-prod",
		"kind":        "deny_destroy",
		"enforcement": "hard",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid policy, got %d: %s", rec.Code, rec.Body.String())
	}

	// Duplicate policy name -> 409
	rec = doCatalogRequest(t, srv, auth, http.MethodPost, "/api/terraform/policies", map[string]interface{}{
		"name":        "deny-destroy-prod",
		"kind":        "deny_destroy",
		"enforcement": "soft",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate policy, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Gate-off returns 403 on top-level handlers
	_ = srv.db.SetTerraformEnabled(false)

	rec = doCatalogRequest(t, srv, auth, http.MethodGet, "/api/terraform/policies", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when gate off for policies, got %d", rec.Code)
	}

	rec = doCatalogRequest(t, srv, auth, http.MethodGet, "/api/terraform/modules", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when gate off for modules, got %d", rec.Code)
	}

	// Varsets top-level handler
	recVarsets := httptest.NewRecorder()
	reqVarsets := httptest.NewRequest(http.MethodGet, "/api/terraform/varsets", nil)
	auth(reqVarsets)
	srv.handleTerraformVarsets(recVarsets, reqVarsets)
	if recVarsets.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when gate off for varsets, got %d", recVarsets.Code)
	}
}

func TestTerraformModule_VersionAddWithInlineFilesAndDuplicate(t *testing.T) {
	srv, auth := setupTerraformCatalogTestServer(t)

	// Create module
	rec := doCatalogRequest(t, srv, auth, http.MethodPost, "/api/terraform/modules", map[string]interface{}{
		"name":        "vpc",
		"provider":    "aws",
		"description": "Standard VPC module",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for create module, got %d: %s", rec.Code, rec.Body.String())
	}
	var mod store.TFModule
	_ = json.Unmarshal(rec.Body.Bytes(), &mod)

	// Add version with inline files
	files := map[string]string{
		"variables.tf": `
variable "cidr_block" {
  type        = string
  description = "The CIDR block for the VPC"
}

variable "enable_dns" {
  type    = bool
  default = true
}
`,
		"outputs.tf": `
output "vpc_id" {
  description = "The ID of the VPC"
  value       = "vpc-12345"
}
`,
		"README.md": "# VPC Module\nProvides VPC networking.",
	}

	rec = doCatalogRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/modules/%d/versions", mod.ID), map[string]interface{}{
		"version": "1.0.0",
		"files":   files,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for module version add, got %d: %s", rec.Code, rec.Body.String())
	}

	var verResp struct {
		Doc struct {
			Readme  string                   `json:"readme"`
			Inputs  []map[string]interface{} `json:"inputs"`
			Outputs []map[string]interface{} `json:"outputs"`
		} `json:"doc"`
		Usage string `json:"usage"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &verResp)

	if verResp.Doc.Readme != "# VPC Module\nProvides VPC networking." {
		t.Errorf("readme not parsed: %q", verResp.Doc.Readme)
	}
	if len(verResp.Doc.Inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d", len(verResp.Doc.Inputs))
	}
	if len(verResp.Doc.Outputs) != 1 {
		t.Errorf("expected 1 output, got %d", len(verResp.Doc.Outputs))
	}
	// Usage snippet must contain cidr_block = "" # required
	if !bytes.Contains([]byte(verResp.Usage), []byte(`module "vpc"`)) || !bytes.Contains([]byte(verResp.Usage), []byte(`cidr_block = ""`)) {
		t.Errorf("unexpected usage snippet: %s", verResp.Usage)
	}

	// Duplicate version -> 409
	rec = doCatalogRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/modules/%d/versions", mod.ID), map[string]interface{}{
		"version": "1.0.0",
		"files":   files,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate module version, got %d: %s", rec.Code, rec.Body.String())
	}
}
