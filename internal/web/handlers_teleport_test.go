package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/teleport"
)

// teleportFakeTSH answers `tsh status`, `tsh kube ls` and `tsh kube login` the way tsh does for a live session.
// Each login writes the isolated kubeconfig named by KUBECONFIG, with current-context tc/<cluster>.
const teleportFakeTSH = `case "$1" in
status) printf '%s' '{"active":{"profile_url":"https://teleport.setoranku.com:443","cluster":"tc","username":"alice","valid_until":"2099-01-01T00:00:00Z"},"profiles":[]}' ;;
kube)
  case "$2" in
  ls) printf '%s' '[{"kube_cluster_name":"prod-eu","labels":{"env":"prod"}},{"kube_cluster_name":"dev-1"}]' ;;
  login)
    for a; do last=$a; done
    printf 'apiVersion: v1\ncurrent-context: tc/%s\ncontexts:\n- name: tc/%s\n' "$last" "$last" > "$KUBECONFIG" ;;
  esac ;;
logout) echo "logged out" ;;
esac
`

// teleportExpiredTSH fails every call the way tsh does once the certificate is gone.
const teleportExpiredTSH = `echo "ERROR: your credentials have expired, please login using 'tsh login'" >&2
exit 1
`

// setupTeleportAPI builds a test server whose tsh is a shell script with the given body, and enables the Teleport API
// on a fresh data directory.
func setupTeleportAPI(t *testing.T, tshBody string) (*Server, func(*http.Request), string) {
	t.Helper()
	srv, auth := setupClusterTestServer(t)
	tsh := filepath.Join(t.TempDir(), "tsh")
	if err := os.WriteFile(tsh, []byte("#!/bin/sh\n"+tshBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PANTAU_TSH_BIN", tsh)
	dataDir := t.TempDir()
	if err := srv.SetTeleportDataDir(dataDir); err != nil {
		t.Fatal(err)
	}
	return srv, auth, dataDir
}

func teleportCall(t *testing.T, srv *Server, auth func(*http.Request), method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	auth(req)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

func decodeTeleportJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON (%d): %q", rec.Code, rec.Body.String())
	}
	return out
}

var prodProfileBody = map[string]interface{}{
	"name": "prod", "proxy": "teleport.setoranku.com:443", "auth_connector": "local",
	"username": "alice", "teleport_cluster": "tc", "default_policy": "read-only",
}

func createProfile(t *testing.T, srv *Server, auth func(*http.Request)) int64 {
	t.Helper()
	rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles", prodProfileBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	return int64(decodeTeleportJSON(t, rec)["id"].(float64))
}

func TestTeleportProfileCRUD(t *testing.T) {
	srv, auth, dataDir := setupTeleportAPI(t, teleportFakeTSH)

	t.Run("list reports tsh detection and starts empty", func(t *testing.T) {
		rec := teleportCall(t, srv, auth, http.MethodGet, "/api/teleport/profiles", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := decodeTeleportJSON(t, rec)
		tsh := body["tsh"].(map[string]interface{})
		if tsh["installed"] != true || tsh["path"] == "" {
			t.Fatalf("tsh detection = %v", tsh)
		}
	})

	id := createProfile(t, srv, auth)

	t.Run("create refuses a duplicate name and invalid input", func(t *testing.T) {
		if rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles", prodProfileBody); rec.Code != http.StatusConflict {
			t.Fatalf("duplicate name: status = %d, want 409", rec.Code)
		}
		bad := map[string]interface{}{"name": "x", "proxy": "-oProxyCommand=id"}
		rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles", bad)
		if rec.Code != http.StatusBadRequest || decodeTeleportJSON(t, rec)["code"] != "INVALID_INPUT" {
			t.Fatalf("flag-like proxy: %d %s", rec.Code, rec.Body.String())
		}
		traversal := map[string]interface{}{"name": "y", "proxy": "a.example.com", "teleport_home": "/srv/../../etc"}
		if rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles", traversal); rec.Code != http.StatusBadRequest {
			t.Fatalf("traversal home: status = %d, want 400", rec.Code)
		}
	})

	t.Run("get, update and missing profile", func(t *testing.T) {
		path := "/api/teleport/profiles/" + tpID(id)
		if rec := teleportCall(t, srv, auth, http.MethodGet, path, nil); rec.Code != http.StatusOK {
			t.Fatalf("get: %d", rec.Code)
		}
		upd := map[string]interface{}{"name": "prod-eu", "proxy": "teleport.setoranku.com:443", "teleport_cluster": "tc", "default_policy": "no-exec"}
		rec := teleportCall(t, srv, auth, http.MethodPut, path, upd)
		if rec.Code != http.StatusOK || decodeTeleportJSON(t, rec)["default_policy"] != "no-exec" {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
		if rec := teleportCall(t, srv, auth, http.MethodGet, "/api/teleport/profiles/999", nil); rec.Code != http.StatusNotFound {
			t.Fatalf("missing profile: status = %d, want 404", rec.Code)
		}
		if rec := teleportCall(t, srv, auth, http.MethodGet, "/api/teleport/profiles/abc", nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("non-numeric id: status = %d, want 400", rec.Code)
		}
	})

	t.Run("delete removes the profile and its isolated kubeconfig", func(t *testing.T) {
		path := "/api/teleport/profiles/" + tpID(id)
		kubeFile, _ := teleport.KubeconfigPath(dataDir, id)
		if err := os.MkdirAll(filepath.Dir(kubeFile), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(kubeFile, []byte("apiVersion: v1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if rec := teleportCall(t, srv, auth, http.MethodDelete, path, nil); rec.Code != http.StatusOK {
			t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
		}
		if _, err := os.Stat(kubeFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("isolated kubeconfig survived delete: %v", err)
		}
		if p, _ := srv.db.GetTeleportProfile(id); p != nil {
			t.Fatal("profile still stored after delete")
		}
	})
}

func TestTeleportStatusSyncAndExpiry(t *testing.T) {
	srv, auth, _ := setupTeleportAPI(t, teleportFakeTSH)
	id := createProfile(t, srv, auth)
	statusPath := "/api/teleport/profiles/" + tpID(id) + "/status"
	syncPath := "/api/teleport/profiles/" + tpID(id) + "/sync"

	rec := teleportCall(t, srv, auth, http.MethodGet, statusPath, nil)
	st := decodeTeleportJSON(t, rec)
	if rec.Code != http.StatusOK || st["active"] != true || st["expired"] != false {
		t.Fatalf("status = %d %v", rec.Code, st)
	}
	if ttl, _ := st["ttl_seconds"].(float64); ttl <= 0 {
		t.Fatalf("ttl_seconds = %v, want > 0", st["ttl_seconds"])
	}

	t.Run("sync lists clusters with badges and the profile policy", func(t *testing.T) {
		rec := teleportCall(t, srv, auth, http.MethodPost, syncPath, map[string]bool{"sync_contexts": false})
		body := decodeTeleportJSON(t, rec)
		if rec.Code != http.StatusOK || len(body["added"].([]interface{})) != 2 {
			t.Fatalf("sync = %d %v", rec.Code, body)
		}
		clusters, _ := srv.db.ListKubeClusters()
		for _, c := range clusters {
			if c.Name == "prod-eu" && (c.EnvBadge != "prod" || c.KubeMode != "read-only") {
				t.Fatalf("prod-eu = %+v", c)
			}
		}
	})

	t.Run("sync with contexts stores each context name", func(t *testing.T) {
		rec := teleportCall(t, srv, auth, http.MethodPost, syncPath, map[string]bool{"sync_contexts": true})
		body := decodeTeleportJSON(t, rec)
		if rec.Code != http.StatusOK || body["contexts_ready"] != float64(2) {
			t.Fatalf("sync contexts = %d %v", rec.Code, body)
		}
		clusters, _ := srv.db.ListKubeClusters()
		for _, c := range clusters {
			if c.Name == "dev-1" && c.KubeContextName != "tc/dev-1" {
				t.Fatalf("dev-1 context = %q, want tc/dev-1", c.KubeContextName)
			}
		}
	})

	t.Run("expired session returns 401 with the profile id", func(t *testing.T) {
		expiredSrv, expiredAuth, _ := setupTeleportAPI(t, teleportExpiredTSH)
		eid := createProfile(t, expiredSrv, expiredAuth)
		rec := teleportCall(t, expiredSrv, expiredAuth, http.MethodPost, "/api/teleport/profiles/"+tpID(eid)+"/sync", nil)
		body := decodeTeleportJSON(t, rec)
		if rec.Code != http.StatusUnauthorized || body["code"] != teleport.CodeSessionExpired || body["teleport_profile_id"] != float64(eid) {
			t.Fatalf("expired sync = %d %v", rec.Code, body)
		}
	})
}

func TestTeleportLogoutAndClusterMetadata(t *testing.T) {
	srv, auth, dataDir := setupTeleportAPI(t, teleportFakeTSH)
	id := createProfile(t, srv, auth)
	teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles/"+tpID(id)+"/sync", map[string]bool{"sync_contexts": true})

	t.Run("logout skips tsh for the shared default home", func(t *testing.T) {
		rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles/"+tpID(id)+"/logout", map[string]bool{"clean_kubeconfig": false})
		body := decodeTeleportJSON(t, rec)
		if rec.Code != http.StatusOK || body["tsh_logout"] != false || body["shared_home_skipped"] != true {
			t.Fatalf("logout = %d %v", rec.Code, body)
		}
	})

	t.Run("clean logout removes the kubeconfig and forgets contexts", func(t *testing.T) {
		rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles/"+tpID(id)+"/logout", map[string]bool{"clean_kubeconfig": true})
		if rec.Code != http.StatusOK || decodeTeleportJSON(t, rec)["kubeconfig_removed"] != true {
			t.Fatalf("clean logout = %d %s", rec.Code, rec.Body.String())
		}
		kubeFile, _ := teleport.KubeconfigPath(dataDir, id)
		if _, err := os.Stat(kubeFile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("kubeconfig still present: %v", err)
		}
		clusters, _ := srv.db.ListKubeClusters()
		for _, c := range clusters {
			if c.KubeContextName != "" {
				t.Fatalf("%s kept context %q after clean logout", c.Name, c.KubeContextName)
			}
		}
	})

	var prodID int64
	clusters, _ := srv.db.ListKubeClusters()
	for _, c := range clusters {
		if c.Name == "prod-eu" {
			prodID = c.ID
		}
	}
	clusterPath := "/api/teleport/clusters/" + tpID(prodID)

	t.Run("patch updates favorite, badge and policy", func(t *testing.T) {
		fav := true
		rec := teleportCall(t, srv, auth, http.MethodPatch, clusterPath, map[string]interface{}{
			"is_favorite": fav, "env_badge": "staging", "kube_mode": "no-exec",
		})
		body := decodeTeleportJSON(t, rec)
		if rec.Code != http.StatusOK || body["is_favorite"] != true || body["env_badge"] != "staging" || body["kube_mode"] != "no-exec" {
			t.Fatalf("patch = %d %v", rec.Code, body)
		}
	})

	t.Run("patch refuses unknown badge and policy", func(t *testing.T) {
		if rec := teleportCall(t, srv, auth, http.MethodPatch, clusterPath, map[string]interface{}{"env_badge": "qa"}); rec.Code != http.StatusBadRequest {
			t.Fatalf("badge qa: status = %d, want 400", rec.Code)
		}
		if rec := teleportCall(t, srv, auth, http.MethodPatch, clusterPath, map[string]interface{}{"kube_mode": "root"}); rec.Code != http.StatusBadRequest {
			t.Fatalf("policy root: status = %d, want 400", rec.Code)
		}
	})

	t.Run("used records the switch time", func(t *testing.T) {
		if rec := teleportCall(t, srv, auth, http.MethodPost, clusterPath+"/used", nil); rec.Code != http.StatusOK {
			t.Fatalf("used: %d", rec.Code)
		}
		c, _ := srv.db.GetKubeCluster(prodID)
		if c == nil || c.LastUsedAt == nil {
			t.Fatal("last_used_at not recorded")
		}
	})
}

func TestTeleportRoutesRequireSession(t *testing.T) {
	srv, _, _ := setupTeleportAPI(t, teleportFakeTSH)
	for _, path := range []string{"/api/teleport/profiles", "/api/teleport/profiles/1/status", "/api/teleport/clusters/1"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s answered 200 without a session", path)
		}
	}
}

// A Kubernetes call that hits an expired Teleport session is answered 401 with the profile id, not the generic 500.
func TestKubeErrorMapsExpiredTeleportSession(t *testing.T) {
	t.Parallel()

	expired := &inspector.TeleportSessionError{ProfileID: 9}
	tests := []struct {
		name   string
		write  func(http.ResponseWriter, error)
		status int
	}{
		{name: "writeKubeError", write: func(w http.ResponseWriter, err error) { writeKubeError(w, err) }, status: http.StatusUnauthorized},
		{name: "writeKubeErrorOK", write: func(w http.ResponseWriter, err error) { writeKubeErrorOK(w, err) }, status: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			tt.write(rec, expired)
			body := decodeTeleportJSON(t, rec)
			if rec.Code != tt.status || body["code"] != teleport.CodeSessionExpired || body["teleport_profile_id"] != float64(9) {
				t.Fatalf("%s = %d %v", tt.name, rec.Code, body)
			}
		})
	}

	t.Run("other kube errors keep their status", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		writeKubeError(rec, inspector.ErrKubeInvalid)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid input status = %d, want 400", rec.Code)
		}
	})
}

func tpID(n int64) string { return strconv.FormatInt(n, 10) }
