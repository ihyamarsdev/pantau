package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

var (
	semverRegex = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
)

func validateSemver(v string) error {
	v = strings.TrimSpace(v)
	if !semverRegex.MatchString(v) {
		return errors.New("invalid semver version format (e.g. 1.0.0 or v1.0.0)")
	}
	return nil
}

// buildModuleUsageSnippet generates:
//
//	module "<name>" {
//	  source = "<placeholder>"
//	  version = "<ver>"
//	  ...required inputs...
//	}
func buildModuleUsageSnippet(modName, ver string, inputs []terraform.ModuleInput) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("module \"%s\" {\n", modName))
	b.WriteString(fmt.Sprintf("  source  = \"<placeholder>\"\n"))
	b.WriteString(fmt.Sprintf("  version = \"%s\"\n", ver))
	for _, inp := range inputs {
		if inp.Required {
			b.WriteString(fmt.Sprintf("  %s = \"\" # required\n", inp.Name))
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// ================= POLICIES =================

// handleTerraformPolicies:
//
//	GET  /api/terraform/policies
//	POST /api/terraform/policies
func (s *Server) handleTerraformPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		policies, err := s.db.ListTFPolicies(0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if policies == nil {
			policies = []store.TFPolicy{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"policies": policies,
		})

	case http.MethodPost:
		var req struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Kind        string          `json:"kind"`
			Params      json.RawMessage `json:"params"`
			Enforcement string          `json:"enforcement"`
			Enabled     *bool           `json:"enabled"`
			WorkspaceID int64           `json:"workspace_id"`
		}
		if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
			return
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		paramsBytes := req.Params
		if len(paramsBytes) == 0 {
			paramsBytes = []byte("{}")
		}

		// Validate with terraform.ValidatePolicy
		tp := terraform.Policy{
			Name:        name,
			Kind:        req.Kind,
			Enforcement: req.Enforcement,
			Params:      paramsBytes,
		}
		if err := terraform.ValidatePolicy(tp); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid policy: %v", err)})
			return
		}

		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		policy := &store.TFPolicy{
			Name:        name,
			Description: req.Description,
			Kind:        req.Kind,
			Params:      string(paramsBytes),
			Enforcement: req.Enforcement,
			Enabled:     enabled,
			WorkspaceID: req.WorkspaceID,
		}

		id, err := s.db.CreateTFPolicy(policy)
		if err != nil {
			if errors.Is(err, store.ErrTFPolicyNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		policy.ID = id
		logger.Info("terraform", policy.Name, fmt.Sprintf("Created policy %s (id %d)", policy.Name, id))
		writeJSON(w, http.StatusCreated, policy)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleTerraformPolicyRoute:
//
//	GET/PUT/DELETE /api/terraform/policies/{id}
func (s *Server) handleTerraformPolicyRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/terraform/policies/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid policy id"})
		return
	}

	existing, err := s.db.GetTFPolicy(id)
	if err != nil {
		if errors.Is(err, store.ErrTFPolicyNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, existing)

	case http.MethodPut:
		var req struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Kind        string          `json:"kind"`
			Params      json.RawMessage `json:"params"`
			Enforcement string          `json:"enforcement"`
			Enabled     *bool           `json:"enabled"`
			WorkspaceID *int64          `json:"workspace_id"`
		}
		if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
			return
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = existing.Name
		}
		kind := req.Kind
		if kind == "" {
			kind = existing.Kind
		}
		enforcement := req.Enforcement
		if enforcement == "" {
			enforcement = existing.Enforcement
		}
		paramsBytes := req.Params
		if len(paramsBytes) == 0 {
			paramsBytes = []byte(existing.Params)
			if len(paramsBytes) == 0 {
				paramsBytes = []byte("{}")
			}
		}

		tp := terraform.Policy{
			Name:        name,
			Kind:        kind,
			Enforcement: enforcement,
			Params:      paramsBytes,
		}
		if err := terraform.ValidatePolicy(tp); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid policy: %v", err)})
			return
		}

		existing.Name = name
		existing.Description = req.Description
		existing.Kind = kind
		existing.Params = string(paramsBytes)
		existing.Enforcement = enforcement
		if req.Enabled != nil {
			existing.Enabled = *req.Enabled
		}
		if req.WorkspaceID != nil {
			existing.WorkspaceID = *req.WorkspaceID
		}

		if err := s.db.UpdateTFPolicy(existing); err != nil {
			if errors.Is(err, store.ErrTFPolicyNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("terraform", existing.Name, fmt.Sprintf("Updated policy %s (id %d)", existing.Name, id))
		writeJSON(w, http.StatusOK, existing)

	case http.MethodDelete:
		if err := s.db.DeleteTFPolicy(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", existing.Name, fmt.Sprintf("Deleted policy %s (id %d)", existing.Name, id))
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// ================= MODULES =================

// handleTerraformModules:
//
//	GET  /api/terraform/modules
//	POST /api/terraform/modules
func (s *Server) handleTerraformModules(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		modules, err := s.db.ListTFModules()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if modules == nil {
			modules = []store.TFModule{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"modules": modules,
		})

	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Provider    string `json:"provider"`
			Description string `json:"description"`
		}
		if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
			return
		}
		name := strings.TrimSpace(req.Name)
		provider := strings.TrimSpace(req.Provider)
		if name == "" || provider == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and provider are required"})
			return
		}

		mod := &store.TFModule{
			Name:        name,
			Provider:    provider,
			Description: req.Description,
		}
		id, err := s.db.CreateTFModule(mod)
		if err != nil {
			if errors.Is(err, store.ErrTFModuleNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		mod.ID = id
		logger.Info("terraform", mod.Name, fmt.Sprintf("Created module %s (id %d)", mod.Name, id))
		writeJSON(w, http.StatusCreated, mod)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleTerraformModuleRoute:
//
//	GET/PUT/DELETE /api/terraform/modules/{id}
//	POST           /api/terraform/modules/{id}/versions
//	DELETE         /api/terraform/modules/{id}/versions/{ver}
func (s *Server) handleTerraformModuleRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/terraform/modules/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid module id"})
		return
	}

	mod, err := s.db.GetTFModule(id)
	if err != nil {
		if errors.Is(err, store.ErrTFModuleNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, mod)

		case http.MethodPut:
			var req struct {
				Name        string `json:"name"`
				Provider    string `json:"provider"`
				Description string `json:"description"`
			}
			if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
				return
			}
			name := strings.TrimSpace(req.Name)
			provider := strings.TrimSpace(req.Provider)
			if name == "" || provider == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and provider are required"})
				return
			}
			mod.Name = name
			mod.Provider = provider
			mod.Description = req.Description

			if err := s.db.UpdateTFModule(mod); err != nil {
				if errors.Is(err, store.ErrTFModuleNameTaken) {
					writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			updated, _ := s.db.GetTFModule(id)
			if updated == nil {
				updated = mod
			}
			logger.Info("terraform", updated.Name, fmt.Sprintf("Updated module %s (id %d)", updated.Name, id))
			writeJSON(w, http.StatusOK, updated)

		case http.MethodDelete:
			if err := s.db.DeleteTFModule(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			logger.Info("terraform", mod.Name, fmt.Sprintf("Deleted module %s (id %d)", mod.Name, id))
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	if parts[1] == "versions" {
		if len(parts) == 2 && r.Method == http.MethodPost {
			// POST /api/terraform/modules/{id}/versions
			s.handleCreateModuleVersion(w, r, mod)
			return
		}
		if len(parts) == 3 && r.Method == http.MethodDelete {
			// DELETE /api/terraform/modules/{id}/versions/{ver}
			ver := parts[2]
			if err := s.db.DeleteTFModuleVersion(mod.ID, ver); err != nil {
				if errors.Is(err, store.ErrTFModuleVersionNotFound) {
					http.NotFound(w, r)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			logger.Info("terraform", mod.Name, fmt.Sprintf("Deleted version %s of module %s", ver, mod.Name))
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}

	http.NotFound(w, r)
}

func (s *Server) handleCreateModuleVersion(w http.ResponseWriter, r *http.Request, mod *store.TFModule) {
	var req struct {
		Version string            `json:"version"`
		Files   map[string]string `json:"files"`
		Git     *struct {
			URL    string `json:"url"`
			Ref    string `json:"ref"`
			Path   string `json:"path"`
			Token  string `json:"token"`
			Auth   string `json:"auth"` // none | token | ssh
			KeyID  int64  `json:"key_id"`
			Branch string `json:"branch"` // optional alias for ref
		} `json:"git"`
	}
	if !decodeTFBody(w, r, maxTerraformBodyLimit, &req) {
		return
	}

	ver := strings.TrimSpace(req.Version)
	if err := validateSemver(ver); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	files := req.Files
	sourceURL := ""

	if req.Git != nil && strings.TrimSpace(req.Git.URL) != "" {
		gitURL := strings.TrimSpace(req.Git.URL)
		sourceURL = gitURL
		ref := req.Git.Ref
		if ref == "" {
			ref = req.Git.Branch
		}
		if ref == "" {
			ref = "main"
		}

		gSource := terraform.GitSource{
			URL:    gitURL,
			Branch: ref,
		}

		switch req.Git.Auth {
		case "token":
			gSource.Token = req.Git.Token
		case "ssh":
			if req.Git.KeyID > 0 {
				key, err := s.db.GetSSHKey(req.Git.KeyID)
				if err != nil || key == nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("git ssh key %d not found in SSH Key Library", req.Git.KeyID)})
					return
				}
				gSource.PrivateKey = key.PrivateKey
			}
		}

		clonedFiles, err := terraform.CloneModuleFiles(r.Context(), gSource, ref, req.Git.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("failed to clone module from git: %v", err)})
			return
		}
		files = clonedFiles
	}

	if files == nil {
		files = make(map[string]string)
	}

	// Validate files
	if err := terraform.ValidateFiles(files); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid module files: %v", err)})
		return
	}

	// Parse module documentation
	doc := terraform.ParseModule(files)
	docJSON, err := json.Marshal(doc)
	if err != nil {
		docJSON = []byte("{}")
	}

	filesJSON, err := json.Marshal(files)
	if err != nil {
		filesJSON = []byte("{}")
	}

	mv := &store.TFModuleVersion{
		ModuleID:  mod.ID,
		Version:   ver,
		Files:     string(filesJSON),
		Doc:       string(docJSON),
		SourceURL: sourceURL,
	}

	id, err := s.db.CreateTFModuleVersion(mv)
	if err != nil {
		if errors.Is(err, store.ErrTFModuleVersionTaken) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	mv.ID = id

	usage := buildModuleUsageSnippet(mod.Name, ver, doc.Inputs)

	logger.Info("terraform", mod.Name, fmt.Sprintf("Published version %s of module %s", ver, mod.Name))
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"version":    mv,
		"doc":        doc,
		"usage":      usage,
		"source_url": sourceURL,
	})
}
