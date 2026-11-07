package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

// verifyCloud checks credentials against the provider API; tests replace it.
var verifyCloud = terraform.VerifyCloud

type cloudAccountReq struct {
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"fields"`
	// ImportFrom copies credentials from an existing Pantau account (POST only).
	ImportFrom *struct {
		Type string `json:"type"`
		ID   int64  `json:"id"`
	} `json:"import_from"`
}

// cloudAccountJSON never exposes secret values: only non-secret fields plus a secrets_set map.
func (s *Server) cloudAccountJSON(a *store.TFCloudAccount) map[string]interface{} {
	fields := map[string]string{}
	secretsSet := map[string]bool{}
	if p, ok := terraform.CloudProviderByID(a.Provider); ok {
		for _, f := range p.Fields {
			v := a.Fields[f.Key]
			if f.Secret {
				if v != "" {
					secretsSet[f.Key] = true
				}
			} else {
				fields[f.Key] = v
			}
		}
	}
	count, _ := s.db.TFCloudAccountWorkspaceCount(a.ID)
	return map[string]interface{}{
		"id":              a.ID,
		"name":            a.Name,
		"provider":        a.Provider,
		"description":     a.Description,
		"fields":          fields,
		"secrets_set":     secretsSet,
		"verified_at":     a.VerifiedAt,
		"verify_ok":       a.VerifyOK,
		"verify_message":  a.VerifyMsg,
		"workspace_count": count,
		"created_at":      a.CreatedAt,
		"updated_at":      a.UpdatedAt,
	}
}

// normalizeCloudReq trims the name and field values and validates the name length.
func normalizeCloudReq(req *cloudAccountReq) error {
	req.Name = strings.TrimSpace(req.Name)
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		return errors.New("name must be 1-100 characters")
	}
	fields := make(map[string]string, len(req.Fields))
	for k, v := range req.Fields {
		fields[k] = strings.TrimSpace(v)
	}
	req.Fields = fields
	return nil
}

// handleTerraformCloudAccounts:
//
//	GET  /api/terraform/cloud-accounts
//	POST /api/terraform/cloud-accounts
func (s *Server) handleTerraformCloudAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		accounts, err := s.db.ListTFCloudAccounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]map[string]interface{}, 0, len(accounts))
		for i := range accounts {
			out = append(out, s.cloudAccountJSON(&accounts[i]))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"accounts":  out,
			"providers": terraform.CloudProviders(),
		})

	case http.MethodPost:
		var req cloudAccountReq
		if !decodeTerraformBody(w, r, &req) {
			return
		}
		if err := normalizeCloudReq(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.ImportFrom != nil {
			if err := s.applyCloudImport(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := terraform.ValidateCloudFields(req.Provider, req.Fields); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a := &store.TFCloudAccount{Name: req.Name, Provider: req.Provider, Description: req.Description, Fields: req.Fields}
		id, err := s.db.CreateTFCloudAccount(a)
		if err != nil {
			if errors.Is(err, store.ErrTFCloudAccountNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		created, err := s.db.GetTFCloudAccount(id)
		if err != nil || created == nil {
			http.Error(w, "failed to load created account", http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", created.Name, fmt.Sprintf("Created cloud account %s (%s, id %d)", created.Name, created.Provider, id))
		writeJSON(w, http.StatusCreated, s.cloudAccountJSON(created))

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleTerraformCloudAccountRoute:
//
//	PUT/DELETE /api/terraform/cloud-accounts/{id}
//	POST       /api/terraform/cloud-accounts/{id}/test
func (s *Server) handleTerraformCloudAccountRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	if strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/terraform/cloud-accounts/"), "/") == "importable" {
		s.handleCloudImportable(w, r)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/terraform/cloud-accounts/"), "/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cloud account id"})
		return
	}
	if len(parts) > 2 || (len(parts) == 2 && parts[1] != "test") {
		http.NotFound(w, r)
		return
	}

	acct, err := s.db.GetTFCloudAccount(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if acct == nil {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 2 {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleCloudAccountTest(w, r, acct)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.cloudAccountJSON(acct))

	case http.MethodPut:
		var req cloudAccountReq
		if !decodeTerraformBody(w, r, &req) {
			return
		}
		if err := normalizeCloudReq(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// An empty secret keeps the stored one; only valid within the same provider.
		if p, ok := terraform.CloudProviderByID(req.Provider); ok && req.Provider == acct.Provider {
			for _, f := range p.Fields {
				if f.Secret && req.Fields[f.Key] == "" && acct.Fields[f.Key] != "" {
					req.Fields[f.Key] = acct.Fields[f.Key]
				}
			}
		}
		if err := terraform.ValidateCloudFields(req.Provider, req.Fields); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		acct.Name, acct.Provider, acct.Description, acct.Fields = req.Name, req.Provider, req.Description, req.Fields
		if err := s.db.UpdateTFCloudAccount(acct); err != nil {
			if errors.Is(err, store.ErrTFCloudAccountNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated, err := s.db.GetTFCloudAccount(id)
		if err != nil || updated == nil {
			http.Error(w, "failed to load updated account", http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", updated.Name, fmt.Sprintf("Updated cloud account %s (id %d)", updated.Name, id))
		writeJSON(w, http.StatusOK, s.cloudAccountJSON(updated))

	case http.MethodDelete:
		if err := s.db.DeleteTFCloudAccount(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", acct.Name, fmt.Sprintf("Deleted cloud account %s (id %d)", acct.Name, id))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleCloudAccountTest(w http.ResponseWriter, r *http.Request, a *store.TFCloudAccount) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	identity, err := verifyCloud(ctx, a.Provider, a.Fields, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		msg := err.Error()
		// Defense in depth: the verifier must not leak secrets, scrub anyway.
		if p, ok := terraform.CloudProviderByID(a.Provider); ok {
			for _, f := range p.Fields {
				if v := a.Fields[f.Key]; f.Secret && len(v) >= 4 {
					msg = strings.ReplaceAll(msg, v, "***")
				}
			}
		}
		if serr := s.db.SetTFCloudAccountVerify(a.ID, false, msg); serr != nil {
			http.Error(w, serr.Error(), http.StatusInternalServerError)
			return
		}
		logger.Warn("terraform", a.Name, fmt.Sprintf("Tested cloud account %s (%s): failed", a.Name, a.Provider))
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": msg})
		return
	}
	if serr := s.db.SetTFCloudAccountVerify(a.ID, true, identity); serr != nil {
		http.Error(w, serr.Error(), http.StatusInternalServerError)
		return
	}
	logger.Info("terraform", a.Name, fmt.Sprintf("Tested cloud account %s (%s): ok", a.Name, a.Provider))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "identity": identity})
}

// checkCloudAccountIDs returns an error when any id does not reference an existing account.
func (s *Server) checkCloudAccountIDs(ids []int64) error {
	for _, id := range ids {
		a, err := s.db.GetTFCloudAccount(id)
		if err != nil {
			return err
		}
		if a == nil {
			return fmt.Errorf("cloud account %d not found", id)
		}
	}
	return nil
}

// cloudAccountRefs returns [{id,name,provider}] for the given ids (missing ids skipped).
func (s *Server) cloudAccountRefs(ids []int64) []map[string]interface{} {
	refs := []map[string]interface{}{}
	for _, id := range ids {
		if a, err := s.db.GetTFCloudAccount(id); err == nil && a != nil {
			refs = append(refs, map[string]interface{}{"id": a.ID, "name": a.Name, "provider": a.Provider})
		}
	}
	return refs
}

// applyCloudImport copies credentials of an existing Pantau cloudflare account / kube cluster
// into req.Fields (a non-empty request field wins). The copy is not a live reference.
func (s *Server) applyCloudImport(req *cloudAccountReq) error {
	imp := req.ImportFrom
	var src map[string]string
	switch imp.Type {
	case "cloudflare":
		a, err := s.db.GetCloudflareAccount(imp.ID)
		if err != nil {
			return err
		}
		if a == nil {
			return errors.New("cloudflare account not found")
		}
		if a.APIToken == "" {
			return errors.New("cloudflare account has no API token")
		}
		src = map[string]string{"api_token": a.APIToken, "account_id": a.AccountID}
	case "kubernetes":
		c, err := s.db.GetKubeCluster(imp.ID)
		if err != nil {
			return err
		}
		if c == nil {
			return errors.New("kubernetes cluster not found")
		}
		if strings.TrimSpace(c.Kubeconfig) == "" {
			return errors.New("kubernetes cluster has no kubeconfig")
		}
		src = map[string]string{"kubeconfig": c.Kubeconfig, "context": c.Context}
	default:
		return fmt.Errorf("cannot import from %q", imp.Type)
	}
	req.Provider = imp.Type
	for k, v := range src {
		if req.Fields[k] == "" {
			req.Fields[k] = strings.TrimSpace(v)
		}
	}
	return nil
}

// handleCloudImportable: GET /api/terraform/cloud-accounts/importable (names only, no secrets).
func (s *Server) handleCloudImportable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	type item struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Usable bool   `json:"usable"`
	}
	cf, kube := []item{}, []item{}
	accts, err := s.db.ListCloudflareAccounts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, a := range accts {
		cf = append(cf, item{a.ID, a.Name, a.APIToken != ""})
	}
	clusters, err := s.db.ListKubeClusters()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range clusters {
		kube = append(kube, item{c.ID, c.Name, strings.TrimSpace(c.Kubeconfig) != ""})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"cloudflare": cf, "kubernetes": kube})
}
