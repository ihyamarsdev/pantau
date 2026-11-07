package web

import (
	"net/http"
	"testing"

	"pantau/internal/logger"
)

func TestTerraformAuditSinkPersistsAcrossBroadcasters(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	useAuditBroadcaster(t, srv)

	logger.Info("terraform", "ws-1", "Created workspace ws-1")
	logger.Info("ansible", "pb", "ignored")
	logger.Warn("system", "", "ignored too")
	waitAudit(t, srv, 1)

	rows, err := srv.db.ListTFAudit("", 10)
	if err != nil || len(rows) != 1 || rows[0].Host != "ws-1" || rows[0].Message != "Created workspace ws-1" {
		t.Fatalf("persisted rows = %+v, %v", rows, err)
	}

	// A fresh (empty) broadcaster simulates a restart: the DB still serves the trail.
	logger.SetDefaultBroadcaster(logger.NewBroadcaster(10))
	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/audit", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: %d", rec.Code)
	}
	got := getAudit(t, srv, auth, "?workspace_id=ws-1")
	if len(got.Events) != 1 || got.Events[0].Message != "Created workspace ws-1" || got.Events[0].Level != "info" {
		t.Fatalf("events after fresh broadcaster: %+v", got.Events)
	}
}
