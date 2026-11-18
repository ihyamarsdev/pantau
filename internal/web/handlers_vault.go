package web

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/vault"
)

// vaultRedactor masks every active secret value in logs. It is registered with
// the logger as a global hook, so Ansible/Terraform/SSH/Activity Console output
// can never leak a stored plaintext value.
var vaultRedactor = vault.NewRedactor()

// refreshVaultRedactor reloads the active secret values into the redactor and
// (re)installs the logger hook. Called at server startup and after every secret
// mutation so a newly stored or changed value is masked immediately.
func (s *Server) refreshVaultRedactor() {
	if s.db == nil {
		return
	}
	vals, err := s.db.ListVaultSecretValues()
	if err != nil {
		return
	}
	vaultRedactor.SetValues(vals)
	logger.SetSecretRedactor(vaultRedactor.Redact)
}

// vaultSecretsMapDB returns key -> decrypted value for all secrets in db.
// Plaintext lives only in the returned Go map.
func vaultSecretsMapDB(db *store.DB) map[string]string {
	out := map[string]string{}
	if db == nil {
		return out
	}
	metas, err := db.ListVaultSecrets()
	if err != nil {
		return out
	}
	for _, m := range metas {
		sec, err := db.GetVaultSecret(m.ID)
		if err != nil || sec == nil {
			continue
		}
		out[sec.Key] = sec.Value
	}
	return out
}

// vaultSecretsMap returns key -> decrypted value for all stored secrets.
func (s *Server) vaultSecretsMap() map[string]string {
	return vaultSecretsMapDB(s.db)
}

func vaultErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": vaultRedactor.Redact(err.Error())})
}

// parseVaultExpiry accepts an RFC3339 timestamp or a YYYY-MM-DD date from the
// UI's date input, returning a pointer to the parsed UTC time (or nil).
func parseVaultExpiry(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		u := t.UTC()
		return &u, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		u := t.UTC()
		return &u, nil
	}
	return nil, fmt.Errorf("invalid expires_at %q (expected RFC3339 or YYYY-MM-DD)", raw)
}

func vaultExpiry(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	return parseVaultExpiry(*raw)
}
func vaultIDFromPath(sub string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(sub), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (s *Server) registerVaultRoutes() {
	s.mux.HandleFunc("/api/vault/secrets", s.authMiddleware(s.handleVaultSecrets))
	s.mux.HandleFunc("/api/vault/secrets/", s.authMiddleware(s.handleVaultSecretDetail))
	s.mux.HandleFunc("/api/vault/templates", s.authMiddleware(s.handleVaultTemplates))
	s.mux.HandleFunc("/api/vault/templates/", s.authMiddleware(s.handleVaultTemplateDetail))
	s.mux.HandleFunc("/api/vault/targets", s.authMiddleware(s.handleVaultTargets))
	s.mux.HandleFunc("/api/vault/targets/", s.authMiddleware(s.handleVaultTargetDetail))
	s.mux.HandleFunc("/api/vault/audit", s.authMiddleware(s.handleVaultAudit))
	s.mux.HandleFunc("/api/vault/status", s.authMiddleware(s.handleVaultStatus))
}

// ---------------------------------------------------------------- secrets

func (s *Server) handleVaultSecrets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.db.ListVaultSecrets()
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, list)

	case http.MethodPost:
		var req struct {
			Key          string     `json:"key"`
			Value        string     `json:"value"`
			Scope        string     `json:"scope"`
			ScopeRef     string     `json:"scope_ref"`
			ExpiresAt    *string `json:"expires_at"`
			RotationDays int        `json:"rotation_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		expiresAt, err := vaultExpiry(req.ExpiresAt)
		if err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		req.Key = strings.TrimSpace(req.Key)
		if req.Key == "" || !vault.ValidScope(req.Scope) || req.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key, value and a valid scope are required"})
			return
		}
		if req.RotationDays < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rotation_days must not be negative"})
			return
		}
		if existing, _ := s.db.GetVaultSecretByKey(req.Key); existing != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a secret with that key already exists"})
			return
		}
		sec := &vault.Secret{
			SecretMeta: vault.SecretMeta{
				Key:          req.Key,
				Scope:        vault.Scope(req.Scope),
				ScopeRef:     strings.TrimSpace(req.ScopeRef),
				ExpiresAt:    expiresAt,
				RotationDays: req.RotationDays,
			},
			Value: req.Value,
		}
		id, err := s.db.CreateVaultSecret(sec)
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		created, err := s.db.GetVaultSecret(id)
		if err != nil || created == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("created secret could not be read back"))
			return
		}
		_ = s.db.AddVaultAudit("secret.create", "created secret "+created.Key)
		s.refreshVaultRedactor()
		writeJSON(w, http.StatusCreated, created.SecretMeta)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVaultSecretDetail routes /api/vault/secrets/{id}[/reveal|/versions|/rollback/{version}].
func (s *Server) handleVaultSecretDetail(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/vault/secrets/"), "/")
	parts := strings.Split(sub, "/")
	id, ok := vaultIDFromPath(parts[0])
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid secret id"})
		return
	}

	// GET /api/vault/secrets/{id}/reveal — the ONLY endpoint returning plaintext.
	if len(parts) == 2 && parts[1] == "reveal" && r.Method == http.MethodGet {
		sec, err := s.db.GetVaultSecret(id)
		if err != nil || sec == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "secret not found"})
			return
		}
		_ = s.db.AddVaultAudit("secret.reveal", "revealed secret "+sec.Key)
		writeJSON(w, http.StatusOK, struct {
			Value string `json:"value"`
		}{Value: sec.Value})
		return
	}

	// GET /api/vault/secrets/{id}/versions
	if len(parts) == 2 && parts[1] == "versions" && r.Method == http.MethodGet {
		versions, err := s.db.ListVaultVersions(id)
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, versions)
		return
	}

	// POST /api/vault/secrets/{id}/rollback/{version}
	if len(parts) == 3 && parts[1] == "rollback" && r.Method == http.MethodPost {
		version, err := strconv.Atoi(parts[2])
		if err != nil || version <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
			return
		}
		if err := s.db.RollbackVaultSecret(id, version); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		sec, err := s.db.GetVaultSecret(id)
		if err != nil || sec == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("rolled back secret could not be read back"))
			return
		}
		_ = s.db.AddVaultAudit("secret.rollback", fmt.Sprintf("rolled back secret %s to version %d", sec.Key, version))
		s.refreshVaultRedactor()
		writeJSON(w, http.StatusOK, sec.SecretMeta)
		return
	}

	if len(parts) != 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		sec, err := s.db.GetVaultSecret(id)
		if err != nil || sec == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "secret not found"})
			return
		}
		var req struct {
			Value        string     `json:"value"`
			ExpiresAt    *string `json:"expires_at"`
			RotationDays int        `json:"rotation_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		expiresAt, err := vaultExpiry(req.ExpiresAt)
		if err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		if req.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "value is required"})
			return
		}
		if req.RotationDays < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rotation_days must not be negative"})
			return
		}
		if err := s.db.UpdateVaultSecret(id, req.Value, expiresAt, req.RotationDays); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		updated, err := s.db.GetVaultSecret(id)
		if err != nil || updated == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("updated secret could not be read back"))
			return
		}
		_ = s.db.AddVaultAudit("secret.update", "updated secret "+updated.Key)
		s.refreshVaultRedactor()
		writeJSON(w, http.StatusOK, updated.SecretMeta)

	case http.MethodDelete:
		sec, err := s.db.GetVaultSecret(id)
		if err != nil || sec == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "secret not found"})
			return
		}
		if err := s.db.DeleteVaultSecret(id); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		_ = s.db.AddVaultAudit("secret.delete", "deleted secret "+sec.Key)
		s.refreshVaultRedactor()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------- templates

func (s *Server) handleVaultTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.db.ListVaultTemplates()
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, list)

	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		t := &vault.Template{Name: req.Name, Body: req.Body}
		id, err := s.db.CreateVaultTemplate(t)
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		created, err := s.db.GetVaultTemplate(id)
		if err != nil || created == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("created template could not be read back"))
			return
		}
		writeJSON(w, http.StatusCreated, created)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleVaultTemplateDetail(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/vault/templates/"), "/")
	parts := strings.Split(sub, "/")
	id, ok := vaultIDFromPath(parts[0])
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid template id"})
		return
	}

	// GET /api/vault/templates/{id}/render — values are masked, never plaintext.
	if len(parts) == 2 && parts[1] == "render" && r.Method == http.MethodGet {
		t, err := s.db.GetVaultTemplate(id)
		if err != nil || t == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		values := s.vaultSecretsMap()
		if _, err := vault.Render(t.Body, values); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		// Replace every placeholder with [REDACTED] without ever emitting the
		// decrypted value: render against an all-redacted value map.
		masked := make(map[string]string, len(values))
		for k := range values {
			masked[k] = "[REDACTED]"
		}
		rendered, err := vault.Render(t.Body, masked)
		if err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"body": t.Body, "rendered": rendered})
		return
	}

	if len(parts) != 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		existing, err := s.db.GetVaultTemplate(id)
		if err != nil || existing == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		var req struct {
			Name string `json:"name"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		existing.Name, existing.Body = req.Name, req.Body
		if err := s.db.UpdateVaultTemplate(existing); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		updated, err := s.db.GetVaultTemplate(id)
		if err != nil || updated == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("updated template could not be read back"))
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := s.db.DeleteVaultTemplate(id); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------- targets

func (s *Server) handleVaultTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.db.ListVaultTargets()
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, list)

	case http.MethodPost:
		var t vault.SyncTarget
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		t.ID = 0
		if !s.validVaultTarget(&t, w) {
			return
		}
		id, err := s.db.CreateVaultTarget(&t)
		if err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		created, err := s.db.GetVaultTarget(id)
		if err != nil || created == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("created target could not be read back"))
			return
		}
		writeJSON(w, http.StatusCreated, created)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) validVaultTarget(t *vault.SyncTarget, w http.ResponseWriter) bool {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return false
	}
	if t.Kind != "host" && t.Kind != "kube" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind must be host or kube"})
		return false
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(t.TargetRef), 10, 64); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_ref must be an integer id"})
		return false
	}
	if t.TemplateID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id is required"})
		return false
	}
	if t.Kind == "host" && strings.TrimSpace(t.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required for host targets"})
		return false
	}
	if t.Kind == "kube" {
		if strings.TrimSpace(t.Namespace) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace is required for kube targets"})
			return false
		}
		if strings.TrimSpace(t.Path) == "" {
			t.Path = t.Name // used as the Kubernetes Secret name
		}
	}
	return true
}

func (s *Server) handleVaultTargetDetail(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/vault/targets/"), "/")
	parts := strings.Split(sub, "/")
	id, ok := vaultIDFromPath(parts[0])
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid target id"})
		return
	}

	// POST /api/vault/targets/{id}/sync — performs the real sync.
	if len(parts) == 2 && parts[1] == "sync" && r.Method == http.MethodPost {
		s.syncVaultTarget(id, w)
		return
	}

	if len(parts) != 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		existing, err := s.db.GetVaultTarget(id)
		if err != nil || existing == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found"})
			return
		}
		var t vault.SyncTarget
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			vaultErr(w, http.StatusBadRequest, err)
			return
		}
		t.ID = id
		t.LastSyncSHA = existing.LastSyncSHA
		t.LastSyncAt = existing.LastSyncAt
		t.DriftStatus = existing.DriftStatus
		t.CreatedAt = existing.CreatedAt
		if !s.validVaultTarget(&t, w) {
			return
		}
		if err := s.db.UpdateVaultTarget(&t); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		updated, err := s.db.GetVaultTarget(id)
		if err != nil || updated == nil {
			vaultErr(w, http.StatusInternalServerError, fmt.Errorf("updated target could not be read back"))
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := s.db.DeleteVaultTarget(id); err != nil {
			vaultErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------- sync

// renderVaultTarget renders the target's template to plaintext .env bytes. The
// plaintext exists only in memory for the duration of the sync.
func (s *Server) renderVaultTarget(t *vault.SyncTarget) ([]byte, error) {
	tpl, err := s.db.GetVaultTemplate(t.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("load template: %w", err)
	}
	if tpl == nil {
		return nil, fmt.Errorf("template %d not found", t.TemplateID)
	}
	rendered, err := vault.Render(tpl.Body, s.vaultSecretsMap())
	if err != nil {
		return nil, err
	}
	return []byte(rendered), nil
}

// vaultDrift compares freshly rendered content with what was last synced.
func vaultDrift(t *vault.SyncTarget, sha string) string {
	if t.LastSyncSHA == "" || t.LastSyncSHA == sha {
		return "in-sync"
	}
	return "drift"
}

func (s *Server) syncVaultTarget(id int64, w http.ResponseWriter) {
	t, err := s.db.GetVaultTarget(id)
	if err != nil || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found"})
		return
	}

	content, err := s.renderVaultTarget(t)
	if err != nil {
		s.markVaultTargetError(t, err)
		vaultErr(w, http.StatusBadRequest, err)
		return
	}
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	drift := vaultDrift(t, sha)

	var output string
	switch t.Kind {
	case "host":
		output, err = s.syncVaultToHost(t, content)
	case "kube":
		output, err = s.syncVaultToKube(t, content)
	default:
		err = fmt.Errorf("unknown target kind %q", t.Kind)
	}
	if err != nil {
		s.markVaultTargetError(t, err)
		vaultErr(w, http.StatusBadGateway, err)
		return
	}

	if hook := strings.TrimSpace(t.PostSyncHook); hook != "" {
		hookOut, hookErr := s.runVaultPostSyncHook(t, hook)
		if hookErr != nil {
			s.markVaultTargetError(t, hookErr)
			vaultErr(w, http.StatusBadGateway, hookErr)
			return
		}
		if hookOut != "" {
			output += "\n" + hookOut
		}
	}

	now := time.Now().UTC()
	if err := s.db.UpdateVaultTargetSync(id, sha, "in-sync", now); err != nil {
		s.markVaultTargetError(t, err)
		vaultErr(w, http.StatusInternalServerError, err)
		return
	}
	_ = s.db.AddVaultAudit("target.sync", "synced target "+t.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"target_id": id,
		"sha256":    sha,
		"drift":     drift,
		"output":    vaultRedactor.Redact(output),
	})
}

func (s *Server) markVaultTargetError(t *vault.SyncTarget, syncErr error) {
	_ = s.db.UpdateVaultTargetSync(t.ID, t.LastSyncSHA, "error", time.Now().UTC())
	_ = s.db.AddVaultAudit("target.sync", fmt.Sprintf("sync failed for target %s: %v", t.Name, syncErr))
}

func (s *Server) syncVaultToHost(t *vault.SyncTarget, content []byte) (string, error) {
	hostID, err := strconv.ParseInt(strings.TrimSpace(t.TargetRef), 10, 64)
	if err != nil || hostID <= 0 {
		return "", fmt.Errorf("invalid host target_ref %q", t.TargetRef)
	}
	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		return "", fmt.Errorf("host %d not found", hostID)
	}
	privKey, err := s.db.ResolveHostPrivateKey(host)
	if err != nil {
		return "", fmt.Errorf("resolve private key for host %s: %w", host.Name, err)
	}
	target, err := s.hostTarget(host)
	if err != nil {
		return "", err
	}
	target.Key = privKey

	runner, err := sshrunner.Connect(target, 0)
	if err != nil {
		return "", fmt.Errorf("connect to host %s: %w", host.Name, err)
	}
	defer runner.Close()

	client, err := runner.SFTP()
	if err != nil {
		return "", fmt.Errorf("open SFTP on host %s: %w", host.Name, err)
	}
	defer client.Close()

	path := t.Path
	f, err := client.Create(path)
	if err != nil {
		return "", fmt.Errorf("create %s on host %s: %w", path, host.Name, err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("write %s on host %s: %w", path, host.Name, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close %s on host %s: %w", path, host.Name, err)
	}
	if err := client.Chmod(path, 0o600); err != nil {
		// Fallback for servers that reject SFTP chmod: use the shell.
		if _, _, _, execErr := runner.ExecTimeout("chmod 600 "+shellQuote(path), 30*time.Second); execErr != nil {
			return "", fmt.Errorf("chmod %s on host %s: %w", path, host.Name, err)
		}
	}
	return fmt.Sprintf("wrote %d bytes to %s on %s (mode 0600)", len(content), path, host.Name), nil
}

// syncVaultToKube builds a v1/Secret from the rendered .env lines and applies
// it to the cluster whose negative id is stored in target_ref.
func (s *Server) syncVaultToKube(t *vault.SyncTarget, content []byte) (string, error) {
	clusterID, err := strconv.ParseInt(strings.TrimSpace(t.TargetRef), 10, 64)
	if err != nil || clusterID >= 0 {
		return "", fmt.Errorf("invalid kube cluster target_ref %q (expected a negative cluster id)", t.TargetRef)
	}

	// Parse KEY=VALUE lines from the rendered .env and base64-encode each value.
	data := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		data[key] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	if len(data) == 0 {
		return "", fmt.Errorf("rendered template has no KEY=VALUE entries to store in the Secret")
	}

	name := strings.TrimSpace(t.Path)
	if name == "" {
		name = strings.TrimSpace(t.Name)
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\ntype: Opaque\ndata:\n", kubeYAMLValue(name), kubeYAMLValue(t.Namespace))
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %s\n", kubeYAMLValue(k), data[k])
	}

	out, err := s.inspector.KubeApply(clusterID, b.String())
	if err != nil {
		return "", fmt.Errorf("apply Secret to cluster %d: %w", clusterID, err)
	}
	return fmt.Sprintf("applied Secret %s/%s to cluster %d", t.Namespace, name, clusterID) + "\n" + out, nil
}

// kubeYAMLValue quotes a string as a YAML scalar. Names and keys are validated
// downstream by Kubernetes, but quoting keeps them from breaking the manifest.
func kubeYAMLValue(s string) string {
	return strconv.Quote(s)
}

// runVaultPostSyncHook performs the optional post-sync action.
func (s *Server) runVaultPostSyncHook(t *vault.SyncTarget, hook string) (string, error) {
	parts := strings.Split(hook, ":")
	switch {
	case len(parts) == 3 && parts[0] == "systemd" && (parts[2] == "restart" || parts[2] == "reload"):
		hostID, err := strconv.ParseInt(strings.TrimSpace(t.TargetRef), 10, 64)
		if err != nil || hostID <= 0 {
			return "", fmt.Errorf("systemd hook needs a host target")
		}
		res, err := s.inspector.SystemdAction(hostID, parts[1], parts[2])
		if err != nil {
			return "", fmt.Errorf("systemd %s %s: %w", parts[2], parts[1], err)
		}
		return fmt.Sprintf("systemd %s %s: ok=%v", parts[2], parts[1], res.Succeeded()), nil

	case len(parts) == 3 && parts[0] == "docker" && parts[2] == "restart":
		hostID, err := strconv.ParseInt(strings.TrimSpace(t.TargetRef), 10, 64)
		if err != nil || hostID <= 0 {
			return "", fmt.Errorf("docker hook needs a host target")
		}
		out, err := s.inspector.DockerContainerOp(hostID, parts[1], "restart", inspector.DockerContainerOpts{})
		if err != nil {
			return "", fmt.Errorf("docker restart %s: %w", parts[1], err)
		}
		return "docker restart " + parts[1] + ": " + strings.TrimSpace(out), nil

	default:
		return "", fmt.Errorf("invalid post_sync_hook %q", hook)
	}
}

// ---------------------------------------------------------------- audit + status

func (s *Server) handleVaultAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := s.db.ListVaultAudit()
	if err != nil {
		vaultErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleVaultStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secrets, err := s.db.ListVaultSecrets()
	if err != nil {
		vaultErr(w, http.StatusInternalServerError, err)
		return
	}
	targets, err := s.db.ListVaultTargets()
	if err != nil {
		vaultErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now()
	expiring := 0
	for _, sec := range secrets {
		if sec.ExpiresAt != nil && sec.ExpiresAt.Before(now.Add(30*24*time.Hour)) {
			expiring++
		}
	}
	masterKeySet := false
	if s.db != nil {
		_, err := s.db.VaultMasterKey()
		masterKeySet = err == nil
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key_count":      len(secrets),
		"expiring_count": expiring,
		"target_count":   len(targets),
		"master_key_set": masterKeySet,
	})
}
