package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

// useAuditBroadcaster routes slog output into a fresh ring buffer and starts the persistence sink.
func useAuditBroadcaster(t *testing.T, srv *Server) {
	t.Helper()
	logger.SetDefaultBroadcaster(logger.NewBroadcaster(600))
	prev := slog.Default()
	logger.SetupGlobalLogger(io.Discard, slog.LevelDebug)
	ctx, cancel := context.WithCancel(context.Background())
	srv.StartTerraformAuditSink(ctx)
	t.Cleanup(func() { cancel(); slog.SetDefault(prev) })
}

// waitAudit blocks until n rows are persisted (the sink is asynchronous).
func waitAudit(t *testing.T, srv *Server, n int) {
	t.Helper()
	for range 200 {
		if rows, _ := srv.db.ListTFAudit("", 1000); len(rows) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("audit rows never reached %d", n)
}

type auditResp struct {
	Events []struct {
		Time      string `json:"time"`
		Level     string `json:"level"`
		Message   string `json:"message"`
		Workspace string `json:"workspace"`
	} `json:"events"`
}

func getAudit(t *testing.T, srv *Server, auth func(*http.Request), query string) auditResp {
	t.Helper()
	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/audit"+query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body.String())
	}
	var out auditResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTerraformAuditAuthRequired(t *testing.T) {
	srv, _ := setupTerraformTestDataServer(t)
	rec := doTFReq(t, srv, nil, http.MethodGet, "/api/terraform/audit", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestTerraformAuditOrderFilterLimit(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	useAuditBroadcaster(t, srv)
	wsID, err := srv.db.CreateTFWorkspace(&store.TFWorkspace{Name: "alpha"})
	if err != nil {
		t.Fatal(err)
	}

	logger.Info("terraform", "alpha", "alpha first")
	logger.Info("ansible", "alpha", "not terraform")
	logger.Warn("terraform", "beta", "beta second")
	logger.Error("terraform", "alpha", "alpha third")

	waitAudit(t, srv, 3)
	all := getAudit(t, srv, auth, "")
	if len(all.Events) != 3 {
		t.Fatalf("want 3 terraform events, got %+v", all.Events)
	}
	if all.Events[0].Message != "alpha third" || all.Events[2].Message != "alpha first" {
		t.Fatalf("not newest first: %+v", all.Events)
	}
	if all.Events[0].Level != "error" || all.Events[0].Workspace != "alpha" || all.Events[0].Time == "" {
		t.Fatalf("bad event fields: %+v", all.Events[0])
	}

	byID := getAudit(t, srv, auth, fmt.Sprintf("?workspace_id=%d", wsID))
	byName := getAudit(t, srv, auth, "?workspace_id=alpha")
	if len(byID.Events) != 2 || len(byName.Events) != 2 {
		t.Fatalf("workspace filter: id=%d name=%d", len(byID.Events), len(byName.Events))
	}

	if got := getAudit(t, srv, auth, "?limit=1"); len(got.Events) != 1 || got.Events[0].Message != "alpha third" {
		t.Fatalf("limit=1: %+v", got.Events)
	}
	if rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/audit?limit=x", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", rec.Code)
	}
}

func TestTerraformAuditLimitCap(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	for i := range 520 {
		if err := srv.db.AppendTFAudit(time.Now(), "INFO", "w", fmt.Sprintf("line %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if got := getAudit(t, srv, auth, "?limit=9999"); len(got.Events) != 500 {
		t.Fatalf("cap: got %d", len(got.Events))
	}
}

func TestTerraformAuditMutationsLoggedWithoutSecrets(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	useAuditBroadcaster(t, srv)
	const varSecret = "VARSECRETVALUE987654"
	wsID, err := srv.db.CreateTFWorkspace(&store.TFWorkspace{Name: "ws-audit"})
	if err != nil {
		t.Fatal(err)
	}

	// Cloud account create / update / delete.
	_, created, raw := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("audit-aws", cloudTestSecret))
	if created["id"] == nil {
		t.Fatalf("create account: %s", raw)
	}
	acctPath := fmt.Sprintf("/api/terraform/cloud-accounts/%d", int64(created["id"].(float64)))
	cloudDo(t, srv, auth, http.MethodPut, acctPath, awsBody("audit-aws", cloudTestSecret+"X"))
	cloudDo(t, srv, auth, http.MethodDelete, acctPath, nil)

	// Workspace variable create / update / delete.
	varBody := map[string]interface{}{"key": "db_pass", "value": varSecret, "category": "terraform", "sensitive": true}
	_, v, raw := cloudDo(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/variables", wsID), varBody)
	if v["id"] == nil {
		t.Fatalf("create variable: %s", raw)
	}
	varPath := fmt.Sprintf("/api/terraform/workspaces/%d/variables/%d", wsID, int64(v["id"].(float64)))
	varBody["value"] = varSecret + "2"
	cloudDo(t, srv, auth, http.MethodPut, varPath, varBody)
	cloudDo(t, srv, auth, http.MethodDelete, varPath, nil)

	waitAudit(t, srv, 6)
	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/audit?limit=500", nil)
	body := rec.Body.String()
	for _, secret := range []string{cloudTestSecret, varSecret} {
		if strings.Contains(body, secret) {
			t.Fatalf("audit leaks secret %q: %s", secret, body)
		}
	}
	for _, want := range []string{
		"Created cloud account audit-aws", "Updated cloud account audit-aws", "Deleted cloud account audit-aws",
		"Created variable db_pass in workspace ws-audit", "Updated variable db_pass in workspace ws-audit", "Deleted variable db_pass from workspace ws-audit",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("audit missing %q in %s", want, body)
		}
	}
}
