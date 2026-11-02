package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/store"
)

func setupSettingsTestServer(t *testing.T) (*Server, *store.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "pantau-server-settings-test-*")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatal(err)
	}
	disp := notify.New(db)
	ins := inspector.New(db, nil, disp)
	srv := NewServer(db, ins, disp)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	return srv, db, cleanup
}

func TestSettings_SecretHandlingAndRoundTrip(t *testing.T) {
	srv, db, cleanup := setupSettingsTestServer(t)
	defer cleanup()

	// 1. Initial GET - secrets should be unset
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	srv.handleSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var getResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatal(err)
	}

	if getResp["ntfy_token_set"] != false {
		t.Errorf("expected ntfy_token_set=false initially, got %v", getResp["ntfy_token_set"])
	}
	if getResp["smtp_pass_set"] != false {
		t.Errorf("expected smtp_pass_set=false initially, got %v", getResp["smtp_pass_set"])
	}
	if _, exists := getResp["ntfy_token"]; exists {
		t.Errorf("ntfy_token must never be returned in GET response")
	}
	if _, exists := getResp["smtp_pass"]; exists {
		t.Errorf("smtp_pass must never be returned in GET response")
	}

	// 2. POST setting secrets and settings
	postBody := map[string]string{
		"ntfy_url":            "https://ntfy.sh/my-topic",
		"ntfy_token":          "secret-ntfy-token-123",
		"smtp_host":           "smtp.example.com",
		"smtp_pass":           "super-secret-smtp-pass",
		"alert_debounce_runs": "3",
	}
	bodyBytes, _ := json.Marshal(postBody)
	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	srv.handleSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on post, got %d", w.Code)
	}

	// Verify in DB
	tok, _ := db.GetSetting("ntfy_token")
	if tok != "secret-ntfy-token-123" {
		t.Errorf("expected ntfy_token in DB, got %q", tok)
	}
	pass, _ := db.GetSetting("smtp_pass")
	if pass != "super-secret-smtp-pass" {
		t.Errorf("expected smtp_pass in DB, got %q", pass)
	}

	// 3. GET should now reflect secrets as set, without leaking values
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w = httptest.NewRecorder()
	srv.handleSettings(w, req)

	getResp = nil
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatal(err)
	}
	if getResp["ntfy_token_set"] != true {
		t.Errorf("expected ntfy_token_set=true, got %v", getResp["ntfy_token_set"])
	}
	if getResp["smtp_pass_set"] != true {
		t.Errorf("expected smtp_pass_set=true, got %v", getResp["smtp_pass_set"])
	}
	if _, exists := getResp["ntfy_token"]; exists {
		t.Errorf("ntfy_token must not be returned in GET")
	}
	if _, exists := getResp["smtp_pass"]; exists {
		t.Errorf("smtp_pass must not be returned in GET")
	}
	if getResp["alert_debounce_runs"] != "3" {
		t.Errorf("expected alert_debounce_runs=3, got %v", getResp["alert_debounce_runs"])
	}

	// 4. POST without secret keys (omitted) -> secrets MUST BE PRESERVED
	postOmit := map[string]string{
		"ntfy_url": "https://ntfy.sh/other-topic",
	}
	bodyBytes, _ = json.Marshal(postOmit)
	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	srv.handleSettings(w, req)

	tok, _ = db.GetSetting("ntfy_token")
	if tok != "secret-ntfy-token-123" {
		t.Errorf("expected ntfy_token preserved when omitted, got %q", tok)
	}

	// 5. POST with empty secret string -> secrets MUST BE CLEARED
	postClear := map[string]string{
		"ntfy_token": "",
		"smtp_pass":  "",
	}
	bodyBytes, _ = json.Marshal(postClear)
	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	srv.handleSettings(w, req)

	tok, _ = db.GetSetting("ntfy_token")
	if tok != "" {
		t.Errorf("expected ntfy_token cleared when empty, got %q", tok)
	}
	pass, _ = db.GetSetting("smtp_pass")
	if pass != "" {
		t.Errorf("expected smtp_pass cleared when empty, got %q", pass)
	}
}

func TestSettings_URLValidation(t *testing.T) {
	srv, _, cleanup := setupSettingsTestServer(t)
	defer cleanup()

	invalidURLs := []string{
		"ftp://example.com/webhook",
		"javascript:alert(1)",
		"not-a-url",
		"file:///etc/passwd",
	}

	for _, bad := range invalidURLs {
		for _, field := range []string{"ntfy_url", "slack_webhook_url", "discord_webhook_url"} {
			payload := map[string]string{field: bad}
			bodyBytes, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()
			srv.handleSettings(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for field %s with value %q, got %d", field, bad, w.Code)
			}
		}
	}

	// Valid URLs should succeed
	validURLs := []string{
		"http://localhost:8080/hook",
		"https://hooks.slack.com/services/xxx",
		"https://discord.com/api/webhooks/xxx",
	}
	for _, good := range validURLs {
		payload := map[string]string{"slack_webhook_url": good}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(bodyBytes))
		w := httptest.NewRecorder()
		srv.handleSettings(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for valid URL %q, got %d", good, w.Code)
		}
	}
}
