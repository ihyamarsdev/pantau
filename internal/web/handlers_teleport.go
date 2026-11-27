package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
	"pantau/internal/teleport"
)

// Teleport profile API. Every route sits behind authMiddleware. A profile value is validated by
// teleport.ValidateProfile before it reaches tsh, and no response carries a credential.

// teleportBodyLimit bounds a Teleport request body.
const teleportBodyLimit = 16 * 1024

// SetTeleportDataDir enables the Teleport API. Isolated kubeconfigs go to <dir>/teleport. The tsh binary can be
// pinned with PANTAU_TSH_BIN (an absolute path); otherwise tsh is taken from PATH.
func (s *Server) SetTeleportDataDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("teleport data directory: %w", err)
	}
	m, err := teleport.NewManager(abs, os.Getenv("PANTAU_TSH_BIN"), nil)
	if err != nil {
		return err
	}
	s.teleportMgr = m
	return nil
}

// teleportManager returns the manager, or writes 503 when the Teleport API is not configured.
func (s *Server) teleportManager(w http.ResponseWriter) *teleport.Manager {
	if s.teleportMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"ok": false, "error": "teleport is not configured", "code": "TELEPORT_NOT_CONFIGURED"})
		return nil
	}
	return s.teleportMgr
}

// writeTeleportError maps a Teleport failure to a structured response. An expired session is 401 and names the
// profile, so the UI can offer a one-click Re-Login.
func writeTeleportError(w http.ResponseWriter, err error, profileID int64) {
	msg := err.Error()
	switch {
	case errors.Is(err, teleport.ErrTeleportSessionExpired):
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"ok": false, "error": "teleport session expired: log in again", "code": teleport.CodeSessionExpired,
			"teleport_profile_id": profileID,
		})
	case errors.Is(err, teleport.ErrTSHNotInstalled):
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"ok": false, "error": msg, "code": "TSH_NOT_INSTALLED"})
	case errors.Is(err, teleport.ErrLoginInProgress):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "error": msg, "code": "TELEPORT_LOGIN_IN_PROGRESS", "teleport_profile_id": profileID})
	case errors.Is(err, teleport.ErrContextNotProvisioned):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "error": msg, "code": "TELEPORT_CONTEXT_NOT_READY", "teleport_profile_id": profileID})
	case errors.Is(err, teleport.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": msg, "code": "INVALID_INPUT"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "error": msg, "code": "TELEPORT_COMMAND_FAILED"})
	}
}

// teleportSessionFailure reports whether a Kubernetes error is an expired Teleport session, and the profile it names.
func teleportSessionFailure(err error) (int64, bool) {
	if !errors.Is(err, teleport.ErrTeleportSessionExpired) {
		return 0, false
	}
	var se *inspector.TeleportSessionError
	if errors.As(err, &se) {
		return se.ProfileID, true
	}
	return 0, true
}

// teleportProfileReq is the body of POST and PUT /api/teleport/profiles.
type teleportProfileReq struct {
	Name            string `json:"name"`
	Proxy           string `json:"proxy"`
	AuthConnector   string `json:"auth_connector"`
	Username        string `json:"username"`
	TeleportCluster string `json:"teleport_cluster"`
	TeleportHome    string `json:"teleport_home"`
	DefaultPolicy   string `json:"default_policy"`
}

// validate checks the display name and the policy here, and every value that reaches tsh through teleport.
func (r teleportProfileReq) validate() error {
	name := strings.TrimSpace(r.Name)
	if name == "" || len(name) > 64 || strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("%w: name is required and must be at most 64 characters", teleport.ErrInvalidInput)
	}
	switch r.DefaultPolicy {
	case "", "read-only", "no-exec", "full":
	default:
		return fmt.Errorf("%w: default_policy must be read-only, no-exec or full", teleport.ErrInvalidInput)
	}
	return teleport.ValidateProfile(teleport.Profile{
		Proxy: r.Proxy, AuthConnector: r.AuthConnector, Username: r.Username,
		TeleportCluster: r.TeleportCluster, TeleportHome: r.TeleportHome,
	})
}

func (r teleportProfileReq) toStore() store.TeleportProfile {
	return store.TeleportProfile{
		Name: strings.TrimSpace(r.Name), Proxy: r.Proxy, AuthConnector: r.AuthConnector, Username: r.Username,
		TeleportCluster: r.TeleportCluster, TeleportHome: r.TeleportHome, DefaultPolicy: r.DefaultPolicy,
	}
}

// teleportProfileOf converts a stored profile to the value the teleport package runs with.
func teleportProfileOf(p *store.TeleportProfile) teleport.Profile {
	return teleport.Profile{
		ID: p.ID, Proxy: p.Proxy, AuthConnector: p.AuthConnector, Username: p.Username,
		TeleportCluster: p.TeleportCluster, TeleportHome: p.TeleportHome,
	}
}

func decodeTeleportBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, teleportBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "bad request", "code": "INVALID_INPUT"})
		return false
	}
	return true
}

// parseTeleportID reads a positive id from a path segment.
func parseTeleportID(seg string) (int64, bool) {
	id, err := strconv.ParseInt(seg, 10, 64)
	return id, err == nil && id > 0
}

// handleTeleportProfiles serves GET (list, with tsh detection) and POST (create) on /api/teleport/profiles.
func (s *Server) handleTeleportProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.db.ListTeleportProfiles()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "list profiles failed"})
			return
		}
		tsh := map[string]interface{}{"installed": false}
		if s.teleportMgr != nil {
			if loc, err := s.teleportMgr.Locate(); err == nil {
				tsh = map[string]interface{}{"installed": true, "path": loc.Path, "source": loc.Source}
			} else {
				tsh["error"] = "tsh is not installed on the Pantau server"
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"profiles": list, "tsh": tsh})
	case http.MethodPost:
		var req teleportProfileReq
		if !decodeTeleportBody(w, r, &req) {
			return
		}
		if err := req.validate(); err != nil {
			writeTeleportError(w, err, 0)
			return
		}
		p := req.toStore()
		id, err := s.db.CreateTeleportProfile(&p)
		if errors.Is(err, store.ErrTeleportProfileNameTaken) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "error": err.Error(), "code": "NAME_TAKEN"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "create profile failed"})
			return
		}
		created, _ := s.db.GetTeleportProfile(id)
		writeJSON(w, http.StatusCreated, created)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTeleportProfileRoute dispatches /api/teleport/profiles/{id}[/status|/sync|/logout|/login/ws].
func (s *Server) handleTeleportProfileRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/teleport/profiles/"), "/")
	id, ok := parseTeleportID(parts[0])
	if !ok {
		http.Error(w, "invalid profile id", http.StatusBadRequest)
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = strings.Join(parts[1:], "/")
	}

	switch sub {
	case "":
		s.handleTeleportProfile(w, r, id)
	case "status":
		s.handleTeleportStatus(w, r, id)
	case "sync":
		s.handleTeleportSync(w, r, id)
	case "logout":
		s.handleTeleportLogout(w, r, id)
	case "login/ws":
		s.handleWSTeleportLogin(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// handleTeleportProfile serves GET, PUT and DELETE for one profile.
func (s *Server) handleTeleportProfile(w http.ResponseWriter, r *http.Request, id int64) {
	existing, err := s.db.GetTeleportProfile(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "load profile failed"})
		return
	}
	if existing == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, existing)
	case http.MethodPut:
		var req teleportProfileReq
		if !decodeTeleportBody(w, r, &req) {
			return
		}
		if err := req.validate(); err != nil {
			writeTeleportError(w, err, id)
			return
		}
		p := req.toStore()
		p.ID = id
		if p.DefaultPolicy == "" {
			p.DefaultPolicy = existing.DefaultPolicy
		}
		if err := s.db.UpdateTeleportProfile(&p); errors.Is(err, store.ErrTeleportProfileNameTaken) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "error": err.Error(), "code": "NAME_TAKEN"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "update profile failed"})
			return
		}
		updated, _ := s.db.GetTeleportProfile(id)
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		// Clusters go with the profile (store), and so does its isolated kubeconfig, which holds client certificates.
		if err := s.db.DeleteTeleportProfile(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "delete profile failed"})
			return
		}
		if s.teleportMgr != nil {
			if err := s.teleportMgr.RemoveKubeconfig(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "profile deleted, but its kubeconfig could not be removed"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTeleportStatus serves GET /api/teleport/profiles/{id}/status: the session and its remaining TTL.
func (s *Server) handleTeleportStatus(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.teleportManager(w)
	if mgr == nil {
		return
	}
	p, err := s.db.GetTeleportProfile(id)
	if err != nil || p == nil {
		http.NotFound(w, r)
		return
	}
	st, err := mgr.Status(r.Context(), teleportProfileOf(p))
	if err != nil {
		writeTeleportError(w, err, id)
		return
	}
	now := time.Now()
	ttl := st.TTL(now)
	resp := map[string]interface{}{
		"profile_id": id, "active": st.Active, "username": st.Username,
		"ttl_seconds": int64(ttl.Seconds()), "expired": st.Active && ttl <= 0,
	}
	if st.Active {
		resp["valid_until"] = st.ValidUntil
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleTeleportSync serves POST /api/teleport/profiles/{id}/sync. It lists the profile's clusters into the database
// and, when sync_contexts is set, logs into each one so its context is ready before first use.
func (s *Server) handleTeleportSync(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.teleportManager(w)
	if mgr == nil {
		return
	}
	var req struct {
		SyncContexts bool `json:"sync_contexts"`
	}
	if r.ContentLength != 0 && !decodeTeleportBody(w, r, &req) {
		return
	}
	p, err := s.db.GetTeleportProfile(id)
	if err != nil || p == nil {
		http.NotFound(w, r)
		return
	}
	tp := teleportProfileOf(p)
	clusters, err := mgr.ListKubeClusters(r.Context(), tp)
	if err != nil {
		writeTeleportError(w, err, id)
		return
	}
	listed := make([]store.TeleportSyncCluster, 0, len(clusters))
	for _, c := range clusters {
		listed = append(listed, store.TeleportSyncCluster{Name: c.Name, EnvBadge: teleport.SuggestEnvBadge(c.Name, c.Labels)})
	}
	res, err := s.db.SyncTeleportClusters(id, p.DefaultPolicy, listed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "sync clusters failed"})
		return
	}
	resp := map[string]interface{}{"ok": true, "added": res.Added, "kept": res.Kept, "removed": res.Removed, "skipped": res.Skipped}

	if req.SyncContexts {
		contexts, err := mgr.SyncAllContexts(r.Context(), tp)
		if errors.Is(err, teleport.ErrTeleportSessionExpired) {
			writeTeleportError(w, err, id)
			return
		}
		ids, idErr := s.db.TeleportClusterIDs(id)
		if idErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "sync contexts failed"})
			return
		}
		for name, ctxName := range contexts {
			if cid, ok := ids[name]; ok {
				if err := s.db.SetKubeClusterContextName(cid, ctxName); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "save context name failed"})
					return
				}
			}
		}
		resp["contexts_ready"] = len(contexts)
		if err != nil {
			resp["context_errors"] = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleTeleportLogout serves POST /api/teleport/profiles/{id}/logout. tsh logout runs only for a profile with its
// own home, because the shared default home would end every other profile's session. clean_kubeconfig removes the
// isolated file and forgets its contexts.
func (s *Server) handleTeleportLogout(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.teleportManager(w)
	if mgr == nil {
		return
	}
	var req struct {
		CleanKubeconfig bool `json:"clean_kubeconfig"`
	}
	if r.ContentLength != 0 && !decodeTeleportBody(w, r, &req) {
		return
	}
	p, err := s.db.GetTeleportProfile(id)
	if err != nil || p == nil {
		http.NotFound(w, r)
		return
	}
	ran, err := mgr.Logout(r.Context(), teleportProfileOf(p))
	if err != nil {
		writeTeleportError(w, err, id)
		return
	}
	if req.CleanKubeconfig {
		if err := mgr.RemoveKubeconfig(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "remove kubeconfig failed"})
			return
		}
		if err := s.db.ClearTeleportContexts(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "clear contexts failed"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "tsh_logout": ran, "shared_home_skipped": p.TeleportHome == "",
		"kubeconfig_removed": req.CleanKubeconfig,
	})
}

// handleTeleportClusterRoute serves PATCH /api/teleport/clusters/{id} (favorite, env badge, policy) and
// POST /api/teleport/clusters/{id}/used (records a switch to the cluster).
func (s *Server) handleTeleportClusterRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/teleport/clusters/"), "/")
	id, ok := parseTeleportID(parts[0])
	if !ok {
		http.Error(w, "invalid cluster id", http.StatusBadRequest)
		return
	}
	c, err := s.db.GetKubeCluster(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "load cluster failed"})
		return
	}
	if c == nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "ensure" && r.Method == http.MethodPost:
		s.handleTeleportEnsure(w, r, c)
	case len(parts) == 2 && parts[1] == "used" && r.Method == http.MethodPost:
		if err := s.db.TouchKubeClusterUsed(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "record use failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	case len(parts) == 1 && r.Method == http.MethodPatch:
		var req struct {
			IsFavorite *bool   `json:"is_favorite"`
			EnvBadge   *string `json:"env_badge"`
			KubeMode   *string `json:"kube_mode"`
		}
		if !decodeTeleportBody(w, r, &req) {
			return
		}
		fav, badge, mode := c.IsFavorite, c.EnvBadge, c.KubeMode
		if req.IsFavorite != nil {
			fav = *req.IsFavorite
		}
		if req.EnvBadge != nil {
			switch *req.EnvBadge {
			case "", "prod", "staging", "dev":
				badge = *req.EnvBadge
			default:
				writeTeleportError(w, fmt.Errorf("%w: env_badge must be prod, staging, dev or empty", teleport.ErrInvalidInput), 0)
				return
			}
		}
		if req.KubeMode != nil {
			if err := validateClusterKubeMode(*req.KubeMode); err != nil {
				writeTeleportError(w, fmt.Errorf("%w: %v", teleport.ErrInvalidInput, err), 0)
				return
			}
			mode = *req.KubeMode
		}
		if err := s.db.UpdateKubeClusterMeta(id, fav, badge, mode); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "update cluster failed"})
			return
		}
		updated, _ := s.db.GetKubeCluster(id)
		writeJSON(w, http.StatusOK, updated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTeleportEnsure serves POST /api/teleport/clusters/{id}/ensure. It logs the profile into one cluster when that
// cluster has no context yet, and stores the context name. The UI calls it before it loads a Teleport cluster.
func (s *Server) handleTeleportEnsure(w http.ResponseWriter, r *http.Request, c *store.KubeCluster) {
	if c.SourceType != store.KubeSourceTeleport {
		writeTeleportError(w, fmt.Errorf("%w: cluster is not a teleport cluster", teleport.ErrInvalidInput), 0)
		return
	}
	mgr := s.teleportManager(w)
	if mgr == nil {
		return
	}
	p, err := s.db.GetTeleportProfile(c.TeleportProfileID)
	if err != nil || p == nil {
		writeTeleportError(w, fmt.Errorf("%w: the profile of this cluster no longer exists", teleport.ErrInvalidInput), c.TeleportProfileID)
		return
	}
	ctxName, err := mgr.EnsureKubeContext(r.Context(), teleportProfileOf(p), c.TeleportKubeName)
	if err != nil {
		writeTeleportError(w, err, p.ID)
		return
	}
	if err := s.db.SetKubeClusterContextName(c.ID, ctxName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "save context name failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "kube_context_name": ctxName})
}
