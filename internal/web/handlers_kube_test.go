package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func setupKubeTestServer(t *testing.T) (*Server, func(*http.Request)) {
	dbPath := filepath.Join(t.TempDir(), "test_kube.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		mock := sshrunner.NewMockRunner()
		mock.DefaultExec = func(cmd string) (string, string, int, error) {
			return "ok", "", 0, nil
		}
		return mock, nil
	}, nil)
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
		"op":   "reboot",
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

func TestSelectKubeCandidateHostsOrder(t *testing.T) {
	primary := &store.Host{
		ID:            10,
		Name:          "Primary-Degraded",
		Status:        "degraded",
		SortOrder:     5,
		KubeClusterID: "cluster-alpha",
	}

	allHosts := []store.Host{
		{ID: 10, Name: "Primary-Degraded", Status: "degraded", SortOrder: 5, KubeClusterID: "cluster-alpha"},
		{ID: 1, Name: "Peer-Down", Status: "down", SortOrder: 1, KubeClusterID: "cluster-alpha"},
		{ID: 2, Name: "Peer-Healthy-B", Status: "healthy", SortOrder: 2, KubeClusterID: "cluster-alpha"},
		{ID: 3, Name: "Peer-Healthy-A", Status: "healthy", SortOrder: 1, KubeClusterID: "cluster-alpha"},
		{ID: 4, Name: "Peer-Degraded", Status: "degraded", SortOrder: 1, KubeClusterID: "cluster-alpha"},
		{ID: 5, Name: "Peer-Other-Cluster", Status: "healthy", SortOrder: 1, KubeClusterID: "cluster-beta"},
		{ID: 6, Name: "Peer-Standalone", Status: "healthy", SortOrder: 1, KubeClusterID: ""},
	}

	candidates := selectKubeCandidateHosts(primary, allHosts)

	// 1. Requested primary host must be first (index 0)
	if len(candidates) != 5 {
		t.Fatalf("expected 5 candidate hosts, got %d", len(candidates))
	}
	if candidates[0].ID != 10 {
		t.Errorf("expected candidate 0 to be primary ID 10, got %d", candidates[0].ID)
	}

	// 2. Peer healthy (sort_order 1 then 2)
	if candidates[1].ID != 3 {
		t.Errorf("expected candidate 1 to be healthy sort 1 (ID 3), got %d", candidates[1].ID)
	}
	if candidates[2].ID != 2 {
		t.Errorf("expected candidate 2 to be healthy sort 2 (ID 2), got %d", candidates[2].ID)
	}

	// 3. Peer degraded
	if candidates[3].ID != 4 {
		t.Errorf("expected candidate 3 to be degraded (ID 4), got %d", candidates[3].ID)
	}

	// 4. Peer down last
	if candidates[4].ID != 1 {
		t.Errorf("expected candidate 4 to be down (ID 1), got %d", candidates[4].ID)
	}
}

func TestPTYRegexPositiveNegative(t *testing.T) {
	positives := []string{
		"kubectl exec -it pod-1 -- bash",
		"kc exec my-pod -- sh",
		"k3s kubectl exec my-pod -c app -- date",
		"sh -c \"kc exec -it pod -- bash\"",
		"/usr/local/bin/k3s kubectl exec my-pod -- sh",
		"kubectl   exec pod -- ls",
		"KC EXEC -i -t foo -- bar",
	}
	for _, p := range positives {
		if !isKubeExecCommand(p) {
			t.Errorf("expected isKubeExecCommand to match %q", p)
		}
	}

	negatives := []string{
		"docker exec -it mypod bash",
		"docker-compose exec web sh",
		"kubectl get pods",
		"kc logs -f my-pod",
		"kubectl describe pod web",
		"podman exec -it c1 sh",
		"exec kc",
		"echo 'executing kubectl'",
	}
	for _, n := range negatives {
		if isKubeExecCommand(n) {
			t.Errorf("expected isKubeExecCommand NOT to match %q", n)
		}
	}
}

func TestKubeModeMatrix(t *testing.T) {
	modes := []string{"full", "no-exec", "read-only"}

	for _, mode := range modes {
		t.Run("Mode_"+mode, func(t *testing.T) {
			srv, auth := setupKubeTestServer(t)
			hID, err := srv.db.CreateHost(&store.Host{
				Name:     "TestHost",
				Host:     "10.0.0.1",
				Port:     22,
				User:     "root",
				KubeMode: mode,
			})
			if err != nil {
				t.Fatalf("create host: %v", err)
			}
			hIDStr := strconv.FormatInt(hID, 10)

			// 1. POST action
			reqAction := httptest.NewRequest("POST", "/api/hosts/"+hIDStr+"/kube/action", strings.NewReader(`{"op":"restart","kind":"deployment","ns":"default","name":"web"}`))
			auth(reqAction)
			wAction := httptest.NewRecorder()
			srv.ServeHTTP(wAction, reqAction)
			if mode == "read-only" {
				if wAction.Code != http.StatusForbidden {
					t.Errorf("expected 403 for action on read-only, got %d", wAction.Code)
				}
			} else {
				if wAction.Code == http.StatusForbidden {
					t.Errorf("unexpected 403 for action on mode %s", mode)
				}
			}

			// 2. POST apply
			reqApply := httptest.NewRequest("POST", "/api/hosts/"+hIDStr+"/kube/apply", strings.NewReader(`{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm"}`))
			auth(reqApply)
			wApply := httptest.NewRecorder()
			srv.ServeHTTP(wApply, reqApply)
			if mode == "read-only" {
				if wApply.Code != http.StatusForbidden {
					t.Errorf("expected 403 for apply on read-only, got %d", wApply.Code)
				}
			} else {
				if wApply.Code == http.StatusForbidden {
					t.Errorf("unexpected 403 for apply on mode %s", mode)
				}
			}

			// 3. GET exec-cmd
			reqExec := httptest.NewRequest("GET", "/api/hosts/"+hIDStr+"/kube/exec-cmd?ns=default&pod=web-1", nil)
			auth(reqExec)
			wExec := httptest.NewRecorder()
			srv.ServeHTTP(wExec, reqExec)
			if mode == "read-only" || mode == "no-exec" {
				if wExec.Code != http.StatusForbidden {
					t.Errorf("expected 403 for exec-cmd on mode %s, got %d", mode, wExec.Code)
				}
			} else {
				if wExec.Code != http.StatusOK {
					t.Errorf("expected 200 for exec-cmd on mode %s, got %d", mode, wExec.Code)
				}
				var res map[string]interface{}
				_ = json.Unmarshal(wExec.Body.Bytes(), &res)
				if res["host_id"] == nil {
					t.Errorf("expected host_id in exec-cmd response")
				}
			}

			// 4. POST diff (allowed in all modes!)
			reqDiff := httptest.NewRequest("POST", "/api/hosts/"+hIDStr+"/kube/diff", strings.NewReader(`{"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm"}`))
			auth(reqDiff)
			wDiff := httptest.NewRecorder()
			srv.ServeHTTP(wDiff, reqDiff)
			if wDiff.Code == http.StatusForbidden {
				t.Errorf("diff should be allowed in all modes, got 403 in mode %s", mode)
			}

			// 5. GET logs (allowed in all modes!)
			reqLogs := httptest.NewRequest("GET", "/api/hosts/"+hIDStr+"/kube/logs?ns=default&pod=web-1", nil)
			auth(reqLogs)
			wLogs := httptest.NewRecorder()
			srv.ServeHTTP(wLogs, reqLogs)
			if wLogs.Code == http.StatusForbidden {
				t.Errorf("logs should be allowed in all modes, got 403 in mode %s", mode)
			}
		})
	}
}

func TestWSTerminalKubeModePolicy(t *testing.T) {
	srv, _ := setupKubeTestServer(t)
	token := "kube_test_token"
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	hID, err := srv.db.CreateHost(&store.Host{
		Name:     "NoExecHost",
		Host:     "10.0.0.1",
		Port:     22,
		User:     "root",
		KubeMode: "no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}

	wsBase := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/terminal"
	header := http.Header{}
	header.Add("Cookie", "pantau_session="+token)

	// 1. Connection with kubectl exec command should be rejected with [Pantau Policy]
	urlBlocked := fmt.Sprintf("%s?host_id=%d&cmd=kubectl+exec+-it+mypod+--+sh", wsBase, hID)
	conn, _, err := websocket.DefaultDialer.Dial(urlBlocked, header)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}
	if !strings.Contains(string(msg), "[Pantau Policy]") {
		t.Errorf("expected [Pantau Policy] message, got: %s", string(msg))
	}
}

func TestKubeFailoverReadAndWriteRules(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_kube_failover.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Cluster with primary (dead) and peer (live)
	h1ID, err := db.CreateHost(&store.Host{
		Name:          "PrimaryDead",
		Host:          "192.0.2.1",
		Port:          22,
		User:          "root",
		Status:        "degraded",
		KubeClusterID: "ha-cluster-uid",
	})
	if err != nil {
		t.Fatal(err)
	}
	h2ID, err := db.CreateHost(&store.Host{
		Name:          "PeerLive",
		Host:          "192.0.2.2",
		Port:          22,
		User:          "root",
		Status:        "healthy",
		KubeClusterID: "ha-cluster-uid",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mock runner factory: fails for 192.0.2.1 (dead), succeeds for 192.0.2.2 (live)
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		if host == "192.0.2.1" {
			return nil, errors.New("dial tcp: host unreachable")
		}
		mock := sshrunner.NewMockRunner()
		mock.DefaultExec = func(cmd string) (string, string, int, error) {
			if strings.Contains(cmd, "get nodes") {
				return `{"items":[]}`, "", 0, nil
			}
			if strings.Contains(cmd, "rollout restart") {
				return "deployment restarted", "", 0, nil
			}
			return "ok", "", 0, nil
		}
		return mock, nil
	}

	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "kube_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	auth := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}

	// 1. Read endpoint failover: request to host 1 fails over to host 2
	reqRead := httptest.NewRequest("GET", fmt.Sprintf("/api/hosts/%d/kube/nodes", h1ID), nil)
	auth(reqRead)
	wRead := httptest.NewRecorder()
	srv.ServeHTTP(wRead, reqRead)
	if wRead.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", wRead.Code, wRead.Body.String())
	}
	var resRead map[string]interface{}
	_ = json.Unmarshal(wRead.Body.Bytes(), &resRead)
	if resRead["ok"] != true {
		t.Errorf("expected ok:true, got %+v", resRead)
	}
	if resRead["failover"] != true {
		t.Errorf("expected failover:true, got %+v", resRead)
	}
	if int64(resRead["served_by_host_id"].(float64)) != h2ID {
		t.Errorf("expected served_by_host_id %d, got %v", h2ID, resRead["served_by_host_id"])
	}
	if int64(resRead["primary_host_id"].(float64)) != h1ID {
		t.Errorf("expected primary_host_id %d, got %v", h1ID, resRead["primary_host_id"])
	}
	if resRead["primary_error"] == nil || !strings.Contains(resRead["primary_error"].(string), "host unreachable") {
		t.Errorf("expected primary_error to contain 'host unreachable', got %v", resRead["primary_error"])
	}

	// 2. Write endpoint failover: SSH dial to primary failed before command ran -> fails over to live peer!
	reqWrite := httptest.NewRequest("POST", fmt.Sprintf("/api/hosts/%d/kube/action", h1ID), strings.NewReader(`{"op":"restart","kind":"deployment","ns":"default","name":"web"}`))
	auth(reqWrite)
	wWrite := httptest.NewRecorder()
	srv.ServeHTTP(wWrite, reqWrite)
	if wWrite.Code != http.StatusOK {
		t.Fatalf("expected 200 on write failover, got %d: %s", wWrite.Code, wWrite.Body.String())
	}
	var resWrite map[string]interface{}
	_ = json.Unmarshal(wWrite.Body.Bytes(), &resWrite)
	if resWrite["ok"] != true || resWrite["failover"] != true {
		t.Errorf("expected successful write failover, got %+v", resWrite)
	}

	// 3. Write endpoint when SSH dial succeeds on primary but command fails -> MUST NOT FAILOVER!
	h3ID, _ := db.CreateHost(&store.Host{
		Name:          "PrimaryConnectedCommandFails",
		Host:          "192.0.2.3",
		Port:          22,
		User:          "root",
		Status:        "healthy",
		KubeClusterID: "ha-cluster-uid",
	})
	// Factory connects to 192.0.2.3, but command returns error
	commandExecuted := false
	insCommandFail := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		if host == "192.0.2.3" {
			mock := sshrunner.NewMockRunner()
			mock.DefaultExec = func(cmd string) (string, string, int, error) {
				commandExecuted = true
				return "", "command execution failed on primary", 1, nil
			}
			return mock, nil
		}
		mock := sshrunner.NewMockRunner()
		mock.DefaultExec = func(cmd string) (string, string, int, error) {
			t.Errorf("peer should NOT have been called when primary connected!")
			return "ok", "", 0, nil
		}
		return mock, nil
	}, nil)
	srvCommandFail := NewServer(db, insCommandFail, nil)
	srvCommandFail.sessions.Store(token, time.Now().Add(time.Hour))

	reqWriteNoFailover := httptest.NewRequest("POST", fmt.Sprintf("/api/hosts/%d/kube/action", h3ID), strings.NewReader(`{"op":"restart","kind":"deployment","ns":"default","name":"web"}`))
	auth(reqWriteNoFailover)
	wWriteNoFailover := httptest.NewRecorder()
	srvCommandFail.ServeHTTP(wWriteNoFailover, reqWriteNoFailover)
	if !commandExecuted {
		t.Errorf("expected command to execute on primary host")
	}
	var resNoFailover map[string]interface{}
	_ = json.Unmarshal(wWriteNoFailover.Body.Bytes(), &resNoFailover)
	if resNoFailover["failover"] == true {
		t.Errorf("write must NOT failover after command ran, got %+v", resNoFailover)
	}
}

func TestHostCreateUpdateKubeModeValidation(t *testing.T) {
	srv, auth := setupKubeTestServer(t)

	// 1. POST /api/hosts with invalid kube_mode -> 400
	req := httptest.NewRequest("POST", "/api/hosts", strings.NewReader(`{"name":"H1","host":"10.0.0.1","kube_mode":"invalid_mode"}`))
	auth(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid kube_mode, got %d", w.Code)
	}

	// 2. POST /api/hosts with valid kube_mode "no-exec" -> 201, kube_cluster_id ignored
	reqValid := httptest.NewRequest("POST", "/api/hosts", strings.NewReader(`{"name":"H1","host":"10.0.0.1","kube_mode":"no-exec","kube_cluster_id":"client_injected"}`))
	auth(reqValid)
	wValid := httptest.NewRecorder()
	srv.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid host create, got %d: %s", wValid.Code, wValid.Body.String())
	}
	var createdHost store.Host
	_ = json.Unmarshal(wValid.Body.Bytes(), &createdHost)
	if createdHost.KubeMode != "no-exec" {
		t.Errorf("expected kube_mode 'no-exec', got %s", createdHost.KubeMode)
	}
	if createdHost.KubeClusterID != "" {
		t.Errorf("expected client kube_cluster_id to be ignored, got %q", createdHost.KubeClusterID)
	}

	// Set cluster ID server-side
	_ = srv.db.SetHostKubeClusterID(createdHost.ID, "server_cluster_id")

	// 3. PUT /api/hosts/{id} with invalid kube_mode -> 400
	reqPutInvalid := httptest.NewRequest("PUT", fmt.Sprintf("/api/hosts/%d", createdHost.ID), strings.NewReader(`{"name":"H1-Updated","host":"10.0.0.1","kube_mode":"bogus"}`))
	auth(reqPutInvalid)
	wPutInvalid := httptest.NewRecorder()
	srv.ServeHTTP(wPutInvalid, reqPutInvalid)
	if wPutInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid PUT kube_mode, got %d", wPutInvalid.Code)
	}

	// 4. PUT /api/hosts/{id} with valid kube_mode "read-only" -> preserves cluster ID
	reqPutValid := httptest.NewRequest("PUT", fmt.Sprintf("/api/hosts/%d", createdHost.ID), strings.NewReader(`{"name":"H1-Updated","host":"10.0.0.1","kube_mode":"read-only"}`))
	auth(reqPutValid)
	wPutValid := httptest.NewRecorder()
	srv.ServeHTTP(wPutValid, reqPutValid)
	if wPutValid.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid PUT, got %d: %s", wPutValid.Code, wPutValid.Body.String())
	}
	updatedHost, _ := srv.db.GetHost(createdHost.ID)
	if updatedHost.KubeMode != "read-only" {
		t.Errorf("expected kube_mode 'read-only', got %s", updatedHost.KubeMode)
	}
	if updatedHost.KubeClusterID != "server_cluster_id" {
		t.Errorf("expected preserved kube_cluster_id 'server_cluster_id', got %q", updatedHost.KubeClusterID)
	}
}

// TestKubeRunReadFailoverBudget verifies the failover loop stops trying candidates once
// the overall budget is spent instead of stacking per-candidate exec timeouts.
func TestKubeRunReadFailoverBudget(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_kube_budget.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ids := make([]int64, 0, 3)
	for _, name := range []string{"h1", "h2", "h3"} {
		id, err := db.CreateHost(&store.Host{Name: name, Host: name, KubeClusterID: "cluster-a"})
		if err != nil {
			t.Fatalf("create host: %v", err)
		}
		ids = append(ids, id)
	}

	var mu sync.Mutex
	execCounts := map[string]int{}
	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		m := sshrunner.NewMockRunner()
		h := host
		m.DefaultExec = func(cmd string) (string, string, int, error) {
			mu.Lock()
			execCounts[h]++
			mu.Unlock()
			switch h {
			case "h1":
				return "", "", 1, errors.New("primary down")
			case "h2":
				time.Sleep(60 * time.Millisecond) // overshoots the shrunk budget
				return "", "", 1, errors.New("secondary down")
			default:
				return "ok", "", 0, nil
			}
		}
		return m, nil
	}, nil)
	srv := NewServer(db, ins, nil)
	token := "kube_budget_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	oldBudget := kubeFailoverBudget
	kubeFailoverBudget = 40 * time.Millisecond
	defer func() { kubeFailoverBudget = oldBudget }()

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/hosts/%d/kube/namespaces", ids[0]), nil)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for runRead failure envelope, got %d: %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if execCounts["h1"] != 1 || execCounts["h2"] != 1 {
		t.Errorf("expected one exec on h1 and h2, got %v", execCounts)
	}
	if execCounts["h3"] != 0 {
		t.Errorf("expected failover budget to stop before h3, got %d execs", execCounts["h3"])
	}
}
