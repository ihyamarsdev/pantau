package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/cloudflare"
	"pantau/internal/logger"
	"pantau/internal/store"
)

const cfBodyLimit = 1 * 1024 * 1024 // 1 MiB

func decodeCFBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, cfBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) writeCFError(w http.ResponseWriter, err error) {
	var apiErr *cloudflare.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": apiErr.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": apiErr.Error()})
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *Server) getCFClient(account *store.CloudflareAccount) *cloudflare.Client {
	if s.cfClient != nil {
		return s.cfClient(account.APIToken)
	}
	return cloudflare.New(account.APIToken)
}

// handleCloudflareAccounts serves:
//
//	GET  /api/cloudflare/accounts -> {"accounts":[...]}
//	POST /api/cloudflare/accounts -> 201 {"account":..., "token":TokenInfo}
func (s *Server) handleCloudflareAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		accounts, err := s.db.ListCloudflareAccounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if accounts == nil {
			accounts = []store.CloudflareAccount{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"accounts": accounts,
		})

	case http.MethodPost:
		var req struct {
			Name      string `json:"name"`
			AccountID string `json:"account_id"`
			APIToken  string `json:"api_token"`
		}
		if !decodeCFBody(w, r, &req) {
			return
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		apiToken := strings.TrimSpace(req.APIToken)
		if apiToken == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_token is required"})
			return
		}

		accountID := strings.TrimSpace(req.AccountID)
		if accountID != "" && !cloudflare.ValidID(accountID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account_id"})
			return
		}

		// Verify token before save
		cf := s.getCFClient(&store.CloudflareAccount{APIToken: apiToken})
		tokenInfo, err := cf.VerifyToken(r.Context(), accountID)
		if err != nil {
			var apiErr *cloudflare.APIError
			if errors.As(err, &apiErr) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": apiErr.Error()})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		acct := &store.CloudflareAccount{
			Name:      name,
			AccountID: accountID,
			APIToken:  apiToken,
		}

		id, err := s.db.CreateCloudflareAccount(acct)
		if err != nil {
			if errors.Is(err, store.ErrCloudflareAccountNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		acct.ID = id

		logger.Info("cloudflare", acct.Name, fmt.Sprintf("Cloudflare account %s created", acct.Name))
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"account": acct,
			"token":   tokenInfo,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCloudflareAccountRoute dispatches:
//
//	/api/cloudflare/accounts/{id}
//	/api/cloudflare/accounts/{id}/zones
//	/api/cloudflare/accounts/{id}/zones/{zid}/dns
//	/api/cloudflare/accounts/{id}/zones/{zid}/dns/{rid}
//	/api/cloudflare/accounts/{id}/zones/{zid}/purge
//	/api/cloudflare/accounts/{id}/zones/{zid}/settings
func (s *Server) handleCloudflareAccountRoute(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/cloudflare/accounts/")
	parts := strings.Split(subpath, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "account id required", http.StatusBadRequest)
		return
	}

	accountID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || accountID <= 0 {
		http.Error(w, "invalid account id", http.StatusBadRequest)
		return
	}

	account, err := s.db.GetCloudflareAccount(accountID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if account == nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	// Route dispatch based on path depth
	if len(parts) == 1 {
		s.handleAccountDetail(w, r, account)
		return
	}

	if parts[1] != "zones" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// /api/cloudflare/accounts/{id}/zones
	if len(parts) == 2 {
		s.handleAccountZones(w, r, account)
		return
	}

	zid := parts[2]
	if !cloudflare.ValidID(zid) {
		http.Error(w, "invalid zone id", http.StatusBadRequest)
		return
	}

	if len(parts) == 4 {
		switch parts[3] {
		case "dns":
			s.handleZoneDNS(w, r, account, zid)
			return
		case "purge":
			s.handleZonePurge(w, r, account, zid)
			return
		case "settings":
			s.handleZoneSettings(w, r, account, zid)
			return
		}
	} else if len(parts) == 5 && parts[3] == "dns" {
		rid := parts[4]
		if !cloudflare.ValidID(rid) {
			http.Error(w, "invalid record id", http.StatusBadRequest)
			return
		}
		s.handleZoneDNSRecord(w, r, account, zid, rid)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleAccountDetail(w http.ResponseWriter, r *http.Request, account *store.CloudflareAccount) {
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name      string `json:"name"`
			AccountID string `json:"account_id"`
			APIToken  string `json:"api_token"`
		}
		if !decodeCFBody(w, r, &req) {
			return
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}

		accountID := strings.TrimSpace(req.AccountID)
		if accountID != "" && !cloudflare.ValidID(accountID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account_id"})
			return
		}

		apiToken := strings.TrimSpace(req.APIToken)
		if apiToken != "" {
			// Verify new token
			cf := s.getCFClient(&store.CloudflareAccount{APIToken: apiToken})
			_, err := cf.VerifyToken(r.Context(), accountID)
			if err != nil {
				var apiErr *cloudflare.APIError
				if errors.As(err, &apiErr) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": apiErr.Error()})
					return
				}
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			account.APIToken = apiToken
		} else {
			account.APIToken = "" // signals store to keep stored token
		}

		account.Name = name
		account.AccountID = accountID

		if err := s.db.UpdateCloudflareAccount(account); err != nil {
			if errors.Is(err, store.ErrCloudflareAccountNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		updated, err := s.db.GetCloudflareAccount(account.ID)
		if err != nil || updated == nil {
			http.Error(w, "account reloaded failed", http.StatusInternalServerError)
			return
		}

		logger.Info("cloudflare", updated.Name, fmt.Sprintf("Cloudflare account %s updated", updated.Name))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"account": updated,
		})

	case http.MethodDelete:
		if err := s.db.DeleteCloudflareAccount(account.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("cloudflare", account.Name, fmt.Sprintf("Cloudflare account %s deleted", account.Name))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAccountZones(w http.ResponseWriter, r *http.Request, account *store.CloudflareAccount) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cf := s.getCFClient(account)
	zones, err := cf.ListZones(r.Context(), account.AccountID)
	if err != nil {
		s.writeCFError(w, err)
		return
	}
	if zones == nil {
		zones = []cloudflare.Zone{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"zones": zones,
	})
}

func (s *Server) handleZoneDNS(w http.ResponseWriter, r *http.Request, account *store.CloudflareAccount, zid string) {
	cf := s.getCFClient(account)
	switch r.Method {
	case http.MethodGet:
		records, err := cf.ListDNSRecords(r.Context(), zid)
		if err != nil {
			s.writeCFError(w, err)
			return
		}
		if records == nil {
			records = []cloudflare.DNSRecord{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"records": records,
		})

	case http.MethodPost:
		var in cloudflare.DNSRecordInput
		if !decodeCFBody(w, r, &in) {
			return
		}
		if err := in.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		rec, err := cf.CreateDNSRecord(r.Context(), zid, in)
		if err != nil {
			s.writeCFError(w, err)
			return
		}

		logger.Info("cloudflare", account.Name, fmt.Sprintf("Created DNS record %s %s in zone %s", in.Type, in.Name, zid))
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"record": rec,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleZoneDNSRecord(w http.ResponseWriter, r *http.Request, account *store.CloudflareAccount, zid, rid string) {
	cf := s.getCFClient(account)
	switch r.Method {
	case http.MethodPut:
		var in cloudflare.DNSRecordInput
		if !decodeCFBody(w, r, &in) {
			return
		}
		if err := in.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		rec, err := cf.UpdateDNSRecord(r.Context(), zid, rid, in)
		if err != nil {
			s.writeCFError(w, err)
			return
		}

		logger.Info("cloudflare", account.Name, fmt.Sprintf("Updated DNS record %s (%s %s) in zone %s", rid, in.Type, in.Name, zid))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"record": rec,
		})

	case http.MethodDelete:
		if err := cf.DeleteDNSRecord(r.Context(), zid, rid); err != nil {
			s.writeCFError(w, err)
			return
		}

		logger.Info("cloudflare", account.Name, fmt.Sprintf("Deleted DNS record %s in zone %s", rid, zid))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleZonePurge(w http.ResponseWriter, r *http.Request, account *store.CloudflareAccount, zid string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req cloudflare.PurgeRequest
	if !decodeCFBody(w, r, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	cf := s.getCFClient(account)
	if err := cf.PurgeCache(r.Context(), zid, req); err != nil {
		s.writeCFError(w, err)
		return
	}

	logger.Info("cloudflare", account.Name, fmt.Sprintf("Purged cache for zone %s", zid))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}

func (s *Server) handleZoneSettings(w http.ResponseWriter, r *http.Request, account *store.CloudflareAccount, zid string) {
	cf := s.getCFClient(account)
	switch r.Method {
	case http.MethodGet:
		settings, err := cf.GetZoneSettings(r.Context(), zid)
		if err != nil {
			s.writeCFError(w, err)
			return
		}
		if settings == nil {
			settings = []cloudflare.Setting{}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"settings": settings,
			"options":  cloudflare.EditableSettings,
		})

	case http.MethodPut:
		var req struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		}
		if !decodeCFBody(w, r, &req) {
			return
		}

		if err := cloudflare.ValidateSetting(req.ID, req.Value); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		setting, err := cf.SetZoneSetting(r.Context(), zid, req.ID, req.Value)
		if err != nil {
			s.writeCFError(w, err)
			return
		}

		logger.Info("cloudflare", account.Name, fmt.Sprintf("Updated setting %s=%s in zone %s", req.ID, req.Value, zid))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"setting": setting,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
