package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

func setupKubeTestServer(t *testing.T) (*Server, func(*http.Request)) {
	dbPath := filepath.Join(t.TempDir(), "test_kube.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ins := inspector.New(db, nil, nil)
	srv := NewServer(db, ins, nil)
	token := "kube_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, authReq
}

func TestKubeHandlersValidation(t *testing.T) {
	srv, auth := setupKubeTestServer(t)

	// 1. Invalid namespace in workloads query returns 400
	req := httptest.NewRequest("GET", "/api/hosts/1/kube/workloads?ns=UPPER_NS", nil)
	auth(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ns, got %d", w.Code)
	}
	var res map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["ok"] != false {
		t.Errorf("expected ok:false, got %v", res["ok"])
	}

	// 2. Invalid tail in logs query returns 400
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/logs?ns=default&pod=web&tail=5000", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for tail > 2000, got %d", w.Code)
	}

	// 3. Invalid kind in describe query (secret is forbidden in describe) returns 400
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/describe?kind=secret&name=my-secret&ns=default", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for secret describe, got %d", w.Code)
	}

	// 4. POST action with invalid action returns 400
	body, _ := json.Marshal(map[string]interface{}{
		"op":   "drain",
		"kind": "node",
		"name": "worker-1",
	})
	req = httptest.NewRequest("POST", "/api/hosts/1/kube/action", bytes.NewReader(body))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported action, got %d", w.Code)
	}

	// 5. POST apply with empty yaml returns 400
	body, _ = json.Marshal(map[string]interface{}{
		"yaml": "   ",
	})
	req = httptest.NewRequest("POST", "/api/hosts/1/kube/apply", bytes.NewReader(body))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty yaml, got %d", w.Code)
	}

	// 6. GET detect for nonexistent host returns 200 with ok:false (read failure contract)
	req = httptest.NewRequest("GET", "/api/hosts/999/kube/detect", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for detect failure, got %d", w.Code)
	}
	var detectRes map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &detectRes)
	if detectRes["ok"] != false {
		t.Errorf("expected ok:false for detect failure, got %v", detectRes["ok"])
	}

	// 7. GET resources with unknown kind returns 400
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/resources?kind=bogus", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown resource kind, got %d", w.Code)
	}

	// 8. GET resources with invalid namespace returns 400
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/resources?kind=ingresses&ns=UPPER_NS", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ns in resources, got %d", w.Code)
	}

	// 9. GET exec-cmd with valid params returns 200 with cmd
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/exec-cmd?ns=default&pod=web-1", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for valid exec-cmd, got %d", w.Code)
	}
	var execRes map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &execRes)
	if execRes["ok"] != true || execRes["cmd"] == nil || !strings.Contains(execRes["cmd"].(string), "exec -it") {
		t.Errorf("invalid exec-cmd response: %+v", execRes)
	}

	// 10. GET exec-cmd with invalid param returns 400
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/exec-cmd?ns=bad;ns&pod=web-1", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid exec-cmd ns, got %d", w.Code)
	}
}

func TestWSKubeLogsValidation(t *testing.T) {
	srv, _ := setupKubeTestServer(t)
	token := "kube_test_token"
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/kube/logs"

	// 1. Unauthenticated -> 401
	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"?host_id=1&ns=default&pod=pod-1", nil)
	if err == nil {
		t.Fatal("expected dial error for unauthenticated websocket")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %v", resp)
	}

	// 2. Authenticated with invalid namespace -> closes with text message
	header := http.Header{}
	header.Add("Cookie", "pantau_session="+token)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?host_id=1&ns=UPPER_NS&pod=pod-1", header)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	msgType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Errorf("expected TextMessage, got %d", msgType)
	}
	if !strings.Contains(string(data), "invalid namespace") {
		t.Errorf("expected 'invalid namespace' message, got: %s", string(data))
	}
}
