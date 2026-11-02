package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"pantau/internal/store"
)

func TestKubeDrainPreviewModeAndValidation(t *testing.T) {
	srv, auth := setupKubeTestServer(t)
	hID, err := srv.db.CreateHost(&store.Host{
		Name: "DrainHost", Host: "10.0.0.1", Port: 22, User: "root", KubeMode: "read-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(hID, 10)

	// Preview must be allowed in read-only (it is a read).
	req := httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/drain-preview", strings.NewReader(`{"name":"node-1"}`))
	auth(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Errorf("drain-preview must be allowed in read-only, got 403")
	}

	// Invalid node name is rejected 400 even before SSH.
	req = httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/drain-preview", strings.NewReader(`{"name":"a;rm"}`))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid drain-preview name, got %d", w.Code)
	}

	// Drain action blocked in read-only like other mutations.
	req = httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/action", strings.NewReader(`{"op":"drain","kind":"node","name":"node-1"}`))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for drain on read-only, got %d", w.Code)
	}
	var res map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["ok"] != false {
		t.Errorf("expected ok:false for blocked drain")
	}
}

func TestWSKubeLogsWorkloadValidation(t *testing.T) {
	srv, _ := setupKubeTestServer(t)
	token := "kube_test_token"
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/kube/logs"
	header := http.Header{}
	header.Add("Cookie", "pantau_session="+token)

	readErr := func(conn *websocket.Conn) string {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(data)
	}

	// previous + workload -> rejected
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?host_id=1&ns=default&workload=deployment/web&previous=1", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if msg := readErr(conn); !strings.Contains(msg, "previous logs are not supported") {
		t.Errorf("unexpected message: %s", msg)
	}

	// unknown kind -> rejected
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL+"?host_id=1&ns=default&workload=cronjob/x", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn2.Close()
	if msg := readErr(conn2); !strings.Contains(msg, "unsupported workload kind") {
		t.Errorf("unexpected message: %s", msg)
	}

	// malformed workload -> rejected
	conn3, _, err := websocket.DefaultDialer.Dial(wsURL+"?host_id=1&ns=default&workload=web", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn3.Close()
	if msg := readErr(conn3); !strings.Contains(msg, "workload must be kind/name") {
		t.Errorf("unexpected message: %s", msg)
	}

	// injection in workload name -> rejected
	conn4, _, err := websocket.DefaultDialer.Dial(wsURL+"?host_id=1&ns=default&workload=deployment%2Fa%3Brm", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn4.Close()
	if msg := readErr(conn4); !strings.Contains(msg, "invalid workload name") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestKubeYAMLRouteValidation(t *testing.T) {
	srv, auth := setupKubeTestServer(t)

	// Secret rejected 400 before any command is built.
	req := httptest.NewRequest("GET", "/api/hosts/1/kube/yaml?kind=secret&ns=default&name=x", nil)
	auth(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for secret yaml, got %d", w.Code)
	}

	// Invalid name rejected 400.
	req = httptest.NewRequest("GET", "/api/hosts/1/kube/yaml?kind=deployment&ns=default&name=a;rm", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid yaml name, got %d", w.Code)
	}
}

func TestKubeModeEnforcementMatrixFull(t *testing.T) {
	srv, auth := setupKubeTestServer(t)
	hID, err := srv.db.CreateHost(&store.Host{
		Name: "ROHost", Host: "10.0.0.1", Port: 22, User: "root", KubeMode: "read-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(hID, 10)

	// 1. read-only blocks drain (403)
	req := httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/action", strings.NewReader(`{"op":"drain","kind":"node","name":"node-1"}`))
	auth(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for drain on read-only, got %d", w.Code)
	}

	// 2. read-only blocks apply (403)
	req = httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/apply", strings.NewReader(`{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm"}`))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for apply on read-only, got %d", w.Code)
	}

	// 3. read-only allows yaml (not 403)
	req = httptest.NewRequest("GET", "/api/hosts/"+idStr+"/kube/yaml?kind=deployment&ns=kube-system&name=coredns", nil)
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Errorf("yaml must be allowed in read-only, got 403")
	}

	// 4. read-only allows drain-preview (not 403)
	req = httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/drain-preview", strings.NewReader(`{"name":"node-1"}`))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Errorf("drain-preview must be allowed in read-only, got 403")
	}

	// 5. read-only allows diff (not 403)
	req = httptest.NewRequest("POST", "/api/hosts/"+idStr+"/kube/diff", strings.NewReader(`{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm"}`))
	auth(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Errorf("diff must be allowed in read-only, got 403")
	}
}
