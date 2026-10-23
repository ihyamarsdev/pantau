package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func setupSecurityTestServer(t *testing.T) (*Server, *store.DB, *inspector.Inspector, *sshrunner.MockRunner, func(*http.Request)) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau-sec-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "pantau.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "", 0, nil
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "test_security_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	return srv, db, ins, mockRunner, authReq
}

func TestSecurityAPI_UnknownHost(t *testing.T) {
	srv, _, _, _, authReq := setupSecurityTestServer(t)

	// GET unknown host -> 404
	req := httptest.NewRequest(http.MethodGet, "/api/hosts/9999/security", nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown host GET, got %d", w.Code)
	}

	// POST scan unknown host -> 404
	reqScan := httptest.NewRequest(http.MethodPost, "/api/hosts/9999/security/scan", nil)
	authReq(reqScan)
	wScan := httptest.NewRecorder()
	srv.ServeHTTP(wScan, reqScan)
	if wScan.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown host POST scan, got %d", wScan.Code)
	}
}

func TestSecurityAPI_GetUnscanned(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)

	hostID, err := db.CreateHost(&store.Host{
		Name: "App Server",
		Host: "192.168.1.10",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed decoding json: %v", err)
	}
	if scanned, ok := res["scanned"].(bool); !ok || scanned {
		t.Fatalf("expected scanned: false, got %+v", res["scanned"])
	}
	if w.Header().Get("X-Scan-Cooldown-Remaining") != "" {
		t.Errorf("expected no cooldown header for unscanned host, got %s", w.Header().Get("X-Scan-Cooldown-Remaining"))
	}
}

func TestSecurityAPI_GetScanned(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)

	hostID, err := db.CreateHost(&store.Host{
		Name: "Database Server",
		Host: "192.168.1.20",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Stored scan from 20 seconds ago -> cooldown header should be ~40s
	scannedAt := time.Now().Add(-20 * time.Second)
	scan := &store.SecurityScan{
		HostID:            hostID,
		ScannedAt:         scannedAt,
		Status:            "ok",
		PackageManager:    "apt",
		OSFamily:          "debian",
		PendingSecurity:   1,
		PendingTotal:      3,
		Severity:          store.SecuritySeverity{Critical: 1},
		RebootRequired:    false,
		RepoCacheAgeHours: 5,
		Findings:          nil, // intentionally nil to verify JSON serialization is []
		Hardening:         nil, // intentionally nil
		Notes:             nil, // intentionally nil
	}
	if err := db.SaveSecurityScan(scan); err != nil {
		t.Fatalf("failed saving scan: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/security", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// Verify cooldown header is set (> 0)
	cdHeader := w.Header().Get("X-Scan-Cooldown-Remaining")
	if cdHeader == "" {
		t.Errorf("expected X-Scan-Cooldown-Remaining header to be set")
	} else {
		rem, err := strconv.Atoi(cdHeader)
		if err != nil || rem <= 0 || rem > 60 {
			t.Errorf("unexpected cooldown remaining value: %s", cdHeader)
		}
	}

	// Raw JSON verification: findings, hardening, notes must never be null
	rawJSON := w.Body.String()
	if strings.Contains(rawJSON, `"findings":null`) {
		t.Errorf("findings should never be null in JSON: %s", rawJSON)
	}
	if strings.Contains(rawJSON, `"hardening":null`) {
		t.Errorf("hardening should never be null in JSON: %s", rawJSON)
	}
	if strings.Contains(rawJSON, `"notes":null`) {
		t.Errorf("notes should never be null in JSON: %s", rawJSON)
	}

	var res struct {
		Scanned bool               `json:"scanned"`
		Scan    store.SecurityScan `json:"scan"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &res); err != nil {
		t.Fatalf("failed decoding json: %v", err)
	}
	if !res.Scanned {
		t.Errorf("expected scanned: true")
	}
	if res.Scan.PendingSecurity != 1 || res.Scan.PackageManager != "apt" {
		t.Errorf("unexpected scan content: %+v", res.Scan)
	}
	if res.Scan.Findings == nil || res.Scan.Hardening == nil || res.Scan.Notes == nil {
		t.Errorf("slices must not be nil after decode: findings=%v, hardening=%v, notes=%v",
			res.Scan.Findings, res.Scan.Hardening, res.Scan.Notes)
	}
}

func TestSecurityAPI_PostScan_Cooldown(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)

	hostID, err := db.CreateHost(&store.Host{
		Name: "Web Server",
		Host: "192.168.1.30",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Scan performed 15s ago -> cooldown active (45s left)
	if err := db.SaveSecurityScan(&store.SecurityScan{
		HostID:    hostID,
		ScannedAt: time.Now().Add(-15 * time.Second),
		Status:    "ok",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/security/scan", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed decoding 429 response json: %v", err)
	}
	if _, ok := res["error"]; !ok {
		t.Errorf("expected error field in 429 response: %+v", res)
	}
	rem, ok := res["cooldown_remaining"].(float64)
	if !ok || rem <= 0 || rem > 60 {
		t.Errorf("unexpected cooldown_remaining value: %+v", res["cooldown_remaining"])
	}
}

func TestSecurityAPI_PostScan_NilInspector(t *testing.T) {
	srv, db, _, _, authReq := setupSecurityTestServer(t)
	srv.inspector = nil

	hostID, err := db.CreateHost(&store.Host{
		Name: "Web Server",
		Host: "192.168.1.40",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/security/scan", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when inspector is nil, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSecurityAPI_PostScan_Success(t *testing.T) {
	srv, db, _, mockRunner, authReq := setupSecurityTestServer(t)

	// Set up mock runner output for security scan commands
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		out := "===OS_RELEASE===\nID=debian\nID_LIKE=debian\n" +
			"===CACHE_AGE===\n1700000000\n---APT---\n1699990000\n" +
			"===REBOOT===\n" +
			"===UPDATES===\nMGR:apt\nInst libssl3 [3.0.11] (3.0.13 Debian-Security:12/stable-security [amd64])\nConf libssl3\n" +
			"===HARDENING===\n---SSHD---\nPermitRootLogin prohibit-password\nPasswordAuthentication no\n---AUTO_UPDATE---\nACTIVE:false\n---FIREWALL---\nUFW:inactive\n"
		return out, "", 0, nil
	}

	hostID, err := db.CreateHost(&store.Host{
		Name: "Scannable Server",
		Host: "192.168.1.50",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/security/scan", hostID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on successful scan, got %d: %s", w.Code, w.Body.String())
	}

	cdHeader := w.Header().Get("X-Scan-Cooldown-Remaining")
	if cdHeader != "60" {
		t.Errorf("expected X-Scan-Cooldown-Remaining: 60, got %q", cdHeader)
	}

	var res struct {
		Scanned bool               `json:"scanned"`
		Scan    store.SecurityScan `json:"scan"`
	}
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed decoding success response: %v", err)
	}
	if !res.Scanned {
		t.Errorf("expected scanned: true")
	}
	if res.Scan.HostID != hostID {
		t.Errorf("expected scan host_id %d, got %d", hostID, res.Scan.HostID)
	}
	if res.Scan.Findings == nil || res.Scan.Hardening == nil || res.Scan.Notes == nil {
		t.Errorf("scan slices must never be nil: findings=%v, hardening=%v, notes=%v",
			res.Scan.Findings, res.Scan.Hardening, res.Scan.Notes)
	}
}

func TestSecurityAPI_PostScan_InProgress_409(t *testing.T) {
	srv, db, _, mockRunner, authReq := setupSecurityTestServer(t)

	hostID, err := db.CreateHost(&store.Host{
		Name: "Slow Server",
		Host: "192.168.1.60",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	scanStarted := make(chan struct{})
	scanProceed := make(chan struct{})

	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		select {
		case <-scanStarted:
		default:
			close(scanStarted)
		}
		<-scanProceed
		return "===OS_RELEASE===\nID=debian\nID_LIKE=debian\n===UPDATES===\nMGR:apt\n", "", 0, nil
	}

	// Launch first scan in background
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		req1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/security/scan", hostID), nil)
		authReq(req1)
		w1 := httptest.NewRecorder()
		srv.ServeHTTP(w1, req1)
	}()

	<-scanStarted

	// Attempt concurrent second scan while first is in-flight
	req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/security/scan", hostID), nil)
	authReq(req2)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	// Release first scan
	close(scanProceed)
	<-firstDone

	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for in-progress scan, got %d: %s", w2.Code, w2.Body.String())
	}
	var res map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&res); err != nil {
		t.Fatalf("failed decoding 409 response: %v", err)
	}
	if errStr, ok := res["error"].(string); !ok || !strings.Contains(strings.ToLower(errStr), "already in progress") {
		t.Errorf("expected 'already in progress' error, got %v", res["error"])
	}
}

func TestSecuritySettings(t *testing.T) {
	srv, _, _, _, authReq := setupSecurityTestServer(t)

	// 1. Default GET: security_scan_interval_hours should be "24"
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/settings failed: %d", w.Code)
	}
	var settings map[string]string
	if err := json.NewDecoder(w.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings["security_scan_interval_hours"] != "24" {
		t.Errorf("expected default interval '24', got %q", settings["security_scan_interval_hours"])
	}

	postSetting := func(val string) string {
		payload, _ := json.Marshal(map[string]string{
			"security_scan_interval_hours": val,
		})
		postReq := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(payload))
		authReq(postReq)
		postW := httptest.NewRecorder()
		srv.ServeHTTP(postW, postReq)
		if postW.Code != http.StatusOK {
			t.Fatalf("POST /api/settings failed for value %q: %d", val, postW.Code)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		authReq(getReq)
		getW := httptest.NewRecorder()
		srv.ServeHTTP(getW, getReq)
		var s map[string]string
		_ = json.NewDecoder(getW.Body).Decode(&s)
		return s["security_scan_interval_hours"]
	}

	// 2. Setting to "48" (valid 1..720)
	if got := postSetting("48"); got != "48" {
		t.Errorf("expected '48', got %q", got)
	}

	// 3. Setting to "0" (disables background scans)
	if got := postSetting("0"); got != "0" {
		t.Errorf("expected '0', got %q", got)
	}

	// 4. Setting to "1000" (should clamp to 720)
	if got := postSetting("1000"); got != "720" {
		t.Errorf("expected clamped '720', got %q", got)
	}

	// 5. Setting to negative "-10" (invalid -> default 24)
	if got := postSetting("-10"); got != "24" {
		t.Errorf("expected invalid negative to default to '24', got %q", got)
	}

	// 6. Setting to non-numeric "abc" (invalid -> default 24)
	if got := postSetting("abc"); got != "24" {
		t.Errorf("expected non-numeric to default to '24', got %q", got)
	}
}
