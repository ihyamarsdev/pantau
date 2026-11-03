package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// writeFakeKubectl writes an executable fake kubectl script into a temp dir and prepends it to PATH.
func writeFakeKubectlScript(t *testing.T, scriptContent string) {
	t.Helper()
	dir := t.TempDir()
	kubectlPath := filepath.Join(dir, "kubectl")
	fullScript := "#!/bin/sh\n" + scriptContent + "\n"
	if err := os.WriteFile(kubectlPath, []byte(fullScript), 0755); err != nil {
		t.Fatalf("failed to write fake kubectl: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
}

func setupClusterTestServer(t *testing.T) (*Server, func(*http.Request)) {
	dbPath := filepath.Join(t.TempDir(), "test_clusters.db")
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
	token := "kube_cluster_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, authReq
}

func TestKubeClustersLifecycleAndSecurity(t *testing.T) {
	fakeKubectl := `
if [ "$1" = "config" ] && [ "$2" = "view" ]; then
  # Find --kubeconfig arg
  cfg=""
  for arg in "$@"; do
    case "$arg" in
      --kubeconfig=*) cfg="${arg#*=}" ;;
    esac
  done
  if [ -z "$cfg" ]; then
    while [ $# -gt 0 ]; do
      if [ "$1" = "--kubeconfig" ]; then
        cfg="$2"
        shift 2
      else
        shift
      fi
    done
  fi
  cat "$cfg"
  exit 0
fi
if [ "$1" = "version" ]; then
  echo '{"serverVersion":{"gitVersion":"v1.31.0"}}'
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "ns" ]; then
  echo "test-cluster-uid"
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "nodes" ]; then
  cat <<'EOF'
{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "ext-node-1", "creationTimestamp": "2026-01-01T00:00:00Z"},
      "status": {
        "conditions": [{"type": "Ready", "status": "True"}],
        "nodeInfo": {"kubeletVersion": "v1.31.0"},
        "addresses": [{"type": "InternalIP", "address": "10.0.1.10"}]
      }
    }
  ]
}
EOF
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "--raw" ]; then
  echo "{}"
  exit 0
fi
echo "unhandled kubectl: $@" >&2
exit 1
`
	writeFakeKubectlScript(t, fakeKubectl)

	srv, auth := setupClusterTestServer(t)

	// 1. Unauthenticated requests are refused
	{
		req := httptest.NewRequest(http.MethodGet, "/api/kube/clusters", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated request, got %d", w.Code)
		}
	}

	// 2. Create rejects tokenFile kubeconfig (400)
	tokenFileKubeconfig := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s.example.com"}}],
  "contexts": [{"name": "ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"tokenFile": "/var/run/secrets/token"}}]
}`
	{
		body, _ := json.Marshal(map[string]string{
			"name":       "cluster-tokenfile",
			"kubeconfig": tokenFileKubeconfig,
			"kube_mode":  "full",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/kube/clusters", bytes.NewReader(body))
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for tokenFile kubeconfig, got %d (body: %s)", w.Code, w.Body.String())
		}
	}

	// 3. Successful create returns cluster without any kubeconfig field
	validEKSKubeconfig := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "eks-ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://xxx.gr7.us-east-1.eks.amazonaws.com"}}],
  "contexts": [{"name": "eks-ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"exec": {"command": "aws", "args": ["eks", "get-token"]}}}]
}`
	var clusterID int64
	{
		body, _ := json.Marshal(map[string]string{
			"name":       "prod-eks",
			"kubeconfig": validEKSKubeconfig,
			"kube_mode":  "full",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/kube/clusters", bytes.NewReader(body))
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for valid create, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			OK      bool                   `json:"ok"`
			Cluster map[string]interface{} `json:"cluster"`
			Info    map[string]interface{} `json:"info"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode create response: %v", err)
		}
		if !res.OK {
			t.Fatalf("expected ok:true in create response: %s", w.Body.String())
		}
		if _, hasKubeconfig := res.Cluster["kubeconfig"]; hasKubeconfig {
			t.Errorf("cluster response must NEVER contain kubeconfig field: %v", res.Cluster)
		}
		if _, hasKubeconfig := res.Cluster["Kubeconfig"]; hasKubeconfig {
			t.Errorf("cluster response must NEVER contain Kubeconfig field: %v", res.Cluster)
		}
		if res.Cluster["provider"] != "eks" {
			t.Errorf("expected provider 'eks', got %v", res.Cluster["provider"])
		}
		clusterID = int64(res.Cluster["id"].(float64))
	}

	// 4. Duplicate name returns 409
	{
		body, _ := json.Marshal(map[string]string{
			"name":       "PROD-EKS", // case-insensitive check
			"kubeconfig": validEKSKubeconfig,
			"kube_mode":  "full",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/kube/clusters", bytes.NewReader(body))
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Errorf("expected 409 for duplicate name, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 5. Failed verification leaves no row
	// Create another server where detect fails
	{
		failIns := inspector.New(srv.db, nil, nil)
		failSrv := NewServer(srv.db, failIns, nil)
		failSrv.sessions.Store("test_token", time.Now().Add(time.Hour))

		// We can test failed verification by pointing fake kubectl to fail for version
		failScript := `
if [ "$1" = "config" ] && [ "$2" = "view" ]; then
  cfg=""
  for arg in "$@"; do
    case "$arg" in
      --kubeconfig=*) cfg="${arg#*=}" ;;
    esac
  done
  cat "$cfg"
  exit 0
fi
echo "network connection error: dial tcp timeout" >&2
exit 1
`
		writeFakeKubectlScript(t, failScript)

		body, _ := json.Marshal(map[string]string{
			"name":       "failing-cluster",
			"kubeconfig": validEKSKubeconfig,
			"kube_mode":  "full",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/kube/clusters", bytes.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: "test_token"})
		w := httptest.NewRecorder()
		failSrv.ServeHTTP(w, req)

		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["ok"] != false {
			t.Errorf("expected ok:false for failed verification, got %v", res)
		}

		// Verify row is not in db
		clusters, err := srv.db.ListKubeClusters()
		if err != nil {
			t.Fatalf("ListKubeClusters failed: %v", err)
		}
		for _, c := range clusters {
			if c.Name == "failing-cluster" {
				t.Fatalf("failing cluster row must not be retained in database")
			}
		}

		// Restore working fake kubectl
		writeFakeKubectlScript(t, fakeKubectl)
	}

	// 6. List never contains kubeconfig
	{
		req := httptest.NewRequest(http.MethodGet, "/api/kube/clusters", nil)
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for list, got %d", w.Code)
		}
		var res struct {
			OK       bool                     `json:"ok"`
			Clusters []map[string]interface{} `json:"clusters"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode list response: %v", err)
		}
		if len(res.Clusters) == 0 {
			t.Fatalf("expected at least 1 cluster in list")
		}
		for _, c := range res.Clusters {
			if _, hasKubeconfig := c["kubeconfig"]; hasKubeconfig {
				t.Errorf("list cluster must not contain kubeconfig: %v", c)
			}
			if _, hasKubeconfig := c["Kubeconfig"]; hasKubeconfig {
				t.Errorf("list cluster must not contain Kubeconfig: %v", c)
			}
		}
	}

	// 7. /api/kube/clusters/{id}/nodes returns nodes with envelope external:true and failover:false
	{
		req := httptest.NewRequest(http.MethodGet, "/api/kube/clusters/1/nodes", nil)
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for /nodes, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode nodes response: %v", err)
		}
		if res["external"] != true {
			t.Errorf("expected external:true in envelope, got %v", res["external"])
		}
		if res["failover"] != false {
			t.Errorf("expected failover:false in envelope, got %v", res["failover"])
		}
		if res["served_by_host_name"] != "prod-eks" {
			t.Errorf("expected served_by_host_name 'prod-eks', got %v", res["served_by_host_name"])
		}
		nodes, ok := res["nodes"].([]interface{})
		if !ok || len(nodes) != 1 {
			t.Fatalf("expected 1 node, got %v", res["nodes"])
		}
	}

	// 8. exec-cmd returns the error ("pod exec is not available for external clusters")
	{
		req := httptest.NewRequest(http.MethodGet, "/api/kube/clusters/1/exec-cmd?ns=default&pod=my-pod", nil)
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for exec-cmd on cluster, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["ok"] != false || res["error"] != "pod exec is not available for external clusters" {
			t.Errorf("unexpected exec-cmd response: %v", res)
		}
	}

	// 9. read-only cluster blocks action with 403
	// Update cluster mode to read-only
	{
		body, _ := json.Marshal(map[string]interface{}{
			"name":      "prod-eks",
			"kube_mode": "read-only",
		})
		req := httptest.NewRequest(http.MethodPut, "/api/kube/clusters/1", bytes.NewReader(body))
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for PUT update mode, got %d: %s", w.Code, w.Body.String())
		}
	}
	{
		actionBody := `{"op":"restart","kind":"deployment","name":"nginx","ns":"default"}`
		req := httptest.NewRequest(http.MethodPost, "/api/kube/clusters/1/action", bytes.NewReader([]byte(actionBody)))
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 for action on read-only cluster, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 10. PUT without kubeconfig keeps existing kubeconfig
	{
		body, _ := json.Marshal(map[string]interface{}{
			"name":      "prod-eks-renamed",
			"kube_mode": "full",
		})
		req := httptest.NewRequest(http.MethodPut, "/api/kube/clusters/1", bytes.NewReader(body))
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for PUT rename, got %d: %s", w.Code, w.Body.String())
		}

		stored, err := srv.db.GetKubeCluster(1)
		if err != nil || stored == nil {
			t.Fatalf("GetKubeCluster failed: %v", err)
		}
		if stored.Name != "prod-eks-renamed" {
			t.Errorf("expected name 'prod-eks-renamed', got %q", stored.Name)
		}
		if stored.Kubeconfig == "" {
			t.Errorf("stored Kubeconfig must not be cleared when omitted in PUT")
		}
	}

	// 11. DELETE removes it
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/kube/clusters/1", nil)
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for DELETE, got %d: %s", w.Code, w.Body.String())
		}
		stored, err := srv.db.GetKubeCluster(clusterID)
		if err != nil {
			t.Fatalf("GetKubeCluster after delete failed: %v", err)
		}
		if stored != nil {
			t.Errorf("cluster should be deleted, but still found: %v", stored)
		}
	}
}

func TestKubeClustersContextsEndpoint(t *testing.T) {
	fakeKubectl := `
if [ "$1" = "config" ] && [ "$2" = "view" ]; then
  cat <<'EOF'
{
  "contexts": [{"name": "ctx-prod"}, {"name": "ctx-dev"}],
  "current-context": "ctx-prod"
}
EOF
  exit 0
fi
exit 1
`
	writeFakeKubectlScript(t, fakeKubectl)

	srv, auth := setupClusterTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"kubeconfig": "valid-dummy-yaml",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/kube/clusters/contexts", bytes.NewReader(body))
	auth(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for contexts, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		OK             bool     `json:"ok"`
		Contexts       []string `json:"contexts"`
		CurrentContext string   `json:"current_context"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode contexts response: %v", err)
	}
	if !res.OK || len(res.Contexts) != 2 || res.CurrentContext != "ctx-prod" {
		t.Errorf("unexpected contexts response: %+v", res)
	}
}
