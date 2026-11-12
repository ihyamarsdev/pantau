package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pantau/internal/s3"
	"pantau/internal/store"
)

// s3AccountJSON is the API shape of an account. The secret key is write-only: only secret_key_set is returned.
type s3AccountJSON struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	AccessKey    string `json:"access_key"`
	SecretKeySet bool   `json:"secret_key_set"`
	Prefix       string `json:"prefix"`
	PathStyle    string `json:"path_style"`
}

func toS3AccountJSON(a store.S3Account) s3AccountJSON {
	return s3AccountJSON{a.ID, a.Name, a.Endpoint, a.Region, a.Bucket, a.AccessKey, a.SecretKey != "", a.Prefix, a.PathStyle}
}

type s3AccountRequest struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"` // empty on update = keep the stored one
	Prefix    string `json:"prefix"`
	PathStyle string `json:"path_style"`
}

// normalize trims and validates the request into an account. needSecret is true when creating.
func (req s3AccountRequest) normalize(needSecret bool) (*store.S3Account, error) {
	a := &store.S3Account{
		Name:      strings.TrimSpace(req.Name),
		Endpoint:  strings.TrimRight(strings.TrimSpace(req.Endpoint), "/"),
		Region:    strings.TrimSpace(req.Region),
		Bucket:    strings.TrimSpace(req.Bucket),
		AccessKey: strings.TrimSpace(req.AccessKey),
		SecretKey: strings.TrimSpace(req.SecretKey),
		Prefix:    s3.NormalizePrefix(req.Prefix),
		PathStyle: req.PathStyle,
	}
	if a.PathStyle != "1" && a.PathStyle != "0" {
		a.PathStyle = ""
	}
	if a.Name == "" || len(a.Name) > 64 {
		return nil, errors.New("name is required (max 64 characters)")
	}
	if a.AccessKey == "" || (needSecret && a.SecretKey == "") {
		return nil, errors.New("access key and secret key are required")
	}
	if a.Endpoint != "" {
		if u, err := url.Parse(a.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("invalid endpoint %q: must be http:// or https://", a.Endpoint)
		}
	}
	// Bucket and endpoint are validated by Config.Validate; the secret is checked above, so give Validate a stand-in.
	secret := a.SecretKey
	if secret == "" {
		secret = "x"
	}
	if err := s3.NewConfig(a.Endpoint, a.Region, a.Bucket, a.AccessKey, secret, a.PathStyle).Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

func decodeS3AccountRequest(r *http.Request) (s3AccountRequest, error) {
	var req s3AccountRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

// handleS3Accounts serves GET (list) and POST (create) on /api/s3/accounts.
func (s *Server) handleS3Accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.db.ListS3Accounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]s3AccountJSON, 0, len(list))
		for _, a := range list {
			out = append(out, toS3AccountJSON(a))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"accounts": out})
	case http.MethodPost:
		req, err := decodeS3AccountRequest(r)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		a, err := req.normalize(true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		id, err := s.db.CreateS3Account(a)
		if errors.Is(err, store.ErrS3AccountNameTaken) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.ID = id
		writeJSON(w, http.StatusCreated, map[string]interface{}{"account": toS3AccountJSON(*a)})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleS3AccountRoute serves /api/s3/accounts/{id} (PUT, DELETE) and /api/s3/accounts/{id}/test (POST).
func (s *Server) handleS3AccountRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/s3/accounts/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 || len(parts) > 2 {
		http.Error(w, "invalid account id", http.StatusBadRequest)
		return
	}
	acct, err := s.db.GetS3Account(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if acct == nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost:
		s.testS3Account(w, r, acct)
	case len(parts) == 1 && r.Method == http.MethodPut:
		req, err := decodeS3AccountRequest(r)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		upd, err := req.normalize(false)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		upd.ID = id
		if err := s.db.UpdateS3Account(upd); errors.Is(err, store.ErrS3AccountNameTaken) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fresh, _ := s.db.GetS3Account(id)
		writeJSON(w, http.StatusOK, map[string]interface{}{"account": toS3AccountJSON(*fresh)})
	case len(parts) == 1 && r.Method == http.MethodDelete:
		if err := s.db.DeleteS3Account(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// testS3Account checks endpoint, bucket and credentials of a saved account (lists one key).
func (s *Server) testS3Account(w http.ResponseWriter, r *http.Request, a *store.S3Account) {
	cfg := s3.NewConfig(a.Endpoint, a.Region, a.Bucket, a.AccessKey, a.SecretKey, a.PathStyle)
	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s3.New(cfg).Check(ctx); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleS3Upload starts an upload of a file or folder on a host to the bucket of an S3 account.
func (s *Server) handleS3Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SourceHostID int64   `json:"source_host_id"`
		SourcePath   string  `json:"source_path"`
		AccountID    int64   `json:"account_id"`
		Prefix       *string `json:"prefix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.SourceHostID <= 0 || req.SourcePath == "" || req.AccountID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "source host, path and S3 account are required"})
		return
	}
	job, err := s.transferMgr.StartS3Upload(req.SourceHostID, req.SourcePath, req.AccountID, req.Prefix)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
