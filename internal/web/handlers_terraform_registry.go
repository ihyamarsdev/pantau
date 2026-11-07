package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/terraform"
)

const tfRegistrySetting = "tf_registry_url"

// registryClient builds a client for the configured registry URL (default when unset or invalid).
func (s *Server) registryClient() *terraform.RegistryClient {
	raw, _ := s.db.GetSetting(tfRegistrySetting)
	base, err := terraform.ValidateRegistryURL(raw)
	if err != nil {
		base = terraform.DefaultRegistryURL
	}
	return terraform.NewRegistryClient(base)
}

func registryFail(w http.ResponseWriter, err error) {
	var se *terraform.RegistryStatusError
	switch {
	case errors.Is(err, terraform.ErrBadRegistryArg):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.As(err, &se) && se.Status == http.StatusNotFound:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found in registry"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}

// handleTerraformRegistry serves /api/terraform/registry/*.
func (s *Server) handleTerraformRegistry(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/terraform/registry/"), "/"), "/")
	method := r.Method
	switch {
	case len(parts) == 1 && parts[0] == "settings" && (method == http.MethodGet || method == http.MethodPut):
		s.registrySettings(w, r)
	case len(parts) == 2 && parts[0] == "settings" && parts[1] == "test" && method == http.MethodPost:
		s.registrySettingsTest(w, r)
	case method != http.MethodGet:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	case len(parts) == 1 && parts[0] == "modules":
		s.registryModules(w, r)
	case len(parts) == 4 && parts[0] == "modules":
		s.registryModule(w, r, parts[1], parts[2], parts[3])
	case len(parts) == 1 && parts[0] == "providers":
		s.registryProviders(w, r)
	case len(parts) == 3 && parts[0] == "providers":
		s.registryProvider(w, r, parts[1], parts[2])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) registrySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req struct {
			BaseURL string `json:"base_url"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}
		base, err := terraform.ValidateRegistryURL(req.BaseURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		stored := base
		if base == terraform.DefaultRegistryURL {
			stored = "" // empty = default
		}
		if err := s.db.SetSetting(tfRegistrySetting, stored); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", "", "Registry URL updated")
	}
	writeJSON(w, http.StatusOK, map[string]string{"base_url": s.registryClient().BaseURL, "default_url": terraform.DefaultRegistryURL})
}

// registrySettingsTest probes the saved URL, or the one in an optional {base_url} body.
func (s *Server) registrySettingsTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
	}
	if r.ContentLength != 0 && !decodeTerraformBody(w, r, &req) {
		return
	}
	c := s.registryClient()
	if strings.TrimSpace(req.BaseURL) != "" {
		base, err := terraform.ValidateRegistryURL(req.BaseURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		c = terraform.NewRegistryClient(base)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	err := c.Ping(ctx)
	res := map[string]any{"ok": err == nil, "latency_ms": time.Since(start).Milliseconds()}
	if err != nil {
		res["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) registryModules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	res, err := s.registryClient().SearchModules(r.Context(), q.Get("q"), q.Get("provider"), offset, limit)
	if err != nil {
		registryFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) registryModule(w http.ResponseWriter, r *http.Request, ns, name, provider string) {
	q := r.URL.Query()
	d, err := s.registryClient().ModuleDetail(r.Context(), ns, name, provider, q.Get("version"))
	if err != nil {
		registryFail(w, err)
		return
	}
	declare := q.Get("declare_vars") == "1"
	main, vars := terraform.ModuleSnippet(d, q.Get("version"), q.Get("module_name"), declare)
	writeJSON(w, http.StatusOK, map[string]any{
		"module": d.Module, "versions": d.Versions, "inputs": d.Inputs, "outputs": d.Outputs,
		"providers": d.Providers, "dependencies": d.Dependencies,
		"readme": d.Readme, "readme_truncated": d.ReadmeTruncated,
		"snippet": map[string]string{"main": main, "variables": vars},
	})
}

func (s *Server) registryProviders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	res, err := s.registryClient().SearchProviders(r.Context(), q.Get("q"), page)
	if err != nil {
		registryFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) registryProvider(w http.ResponseWriter, r *http.Request, ns, typ string) {
	c := s.registryClient()
	d, err := c.ProviderDetail(r.Context(), ns, typ)
	if err != nil {
		registryFail(w, err)
		return
	}
	ver := r.URL.Query().Get("version")
	if ver == "" {
		ver = d.Latest
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": d.Provider, "versions": d.Versions, "latest": d.Latest, "docs_url": d.DocsURL,
		"snippet": terraform.ProviderSnippet(ns, typ, ver),
	})
}
