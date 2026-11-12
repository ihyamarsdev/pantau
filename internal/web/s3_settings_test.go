package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func s3Req(t *testing.T, srv *Server, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		_ = json.NewEncoder(&body).Encode(payload)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, &body)
	switch {
	case path == "/api/s3/accounts":
		srv.handleS3Accounts(w, r)
	case strings.HasPrefix(path, "/api/s3/accounts/"):
		srv.handleS3AccountRoute(w, r)
	default:
		srv.handleS3Upload(w, r)
	}
	return w
}

func TestS3AccountsMultipleAndSecretIsWriteOnly(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()

	create := func(name, bucket, secret string) *httptest.ResponseRecorder {
		return s3Req(t, srv, http.MethodPost, "/api/s3/accounts", map[string]string{
			"name": name, "endpoint": " https://minio.example.com/ ", "bucket": bucket, "access_key": "AKIA",
			"secret_key": secret, "prefix": "/a//b", "path_style": "garbage",
		})
	}
	if w := create("aws", "bkt-one", "TOPSECRET"); w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if w := create("r2", "bkt-two", "OTHERSECRET"); w.Code != http.StatusCreated {
		t.Fatalf("second account: %d %s", w.Code, w.Body.String())
	}
	if w := create("AWS", "bkt-three", "x"); w.Code != http.StatusConflict {
		t.Fatalf("duplicate name must be 409, got %d", w.Code)
	}

	a, _ := db.GetS3Account(1)
	if a.Endpoint != "https://minio.example.com" || a.Prefix != "a/b/" || a.PathStyle != "" {
		t.Fatalf("not normalized: %+v", a)
	}

	list := s3Req(t, srv, http.MethodGet, "/api/s3/accounts", nil)
	if strings.Contains(list.Body.String(), "SECRET") {
		t.Fatal("list leaked a secret key")
	}
	var got struct {
		Accounts []s3AccountJSON `json:"accounts"`
	}
	_ = json.Unmarshal(list.Body.Bytes(), &got)
	if len(got.Accounts) != 2 || !got.Accounts[0].SecretKeySet || got.Accounts[0].Name != "aws" || got.Accounts[1].Bucket != "bkt-two" {
		t.Fatalf("unexpected list: %+v", got.Accounts)
	}

	// Update without a secret keeps the stored one; with a secret replaces it.
	if w := s3Req(t, srv, http.MethodPut, "/api/s3/accounts/1", map[string]string{"name": "aws", "bucket": "renamed", "access_key": "AKIA2"}); w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	a, _ = db.GetS3Account(1)
	if a.SecretKey != "TOPSECRET" || a.Bucket != "renamed" || a.AccessKey != "AKIA2" {
		t.Fatalf("secret lost or fields not saved: %+v", a)
	}
	s3Req(t, srv, http.MethodPut, "/api/s3/accounts/1", map[string]string{"name": "aws", "bucket": "renamed", "access_key": "AKIA2", "secret_key": "NEW"})
	if a, _ = db.GetS3Account(1); a.SecretKey != "NEW" {
		t.Fatalf("secret not replaced: %q", a.SecretKey)
	}

	if w := s3Req(t, srv, http.MethodDelete, "/api/s3/accounts/2", nil); w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
	if rest, _ := db.ListS3Accounts(); len(rest) != 1 {
		t.Fatalf("expected 1 account left, got %d", len(rest))
	}
	if w := s3Req(t, srv, http.MethodDelete, "/api/s3/accounts/2", nil); w.Code != http.StatusNotFound {
		t.Fatalf("deleted account must be 404, got %d", w.Code)
	}
}

func TestS3AccountValidation(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	for name, p := range map[string]map[string]string{
		"no name":       {"bucket": "bkt1", "access_key": "a", "secret_key": "s"},
		"no secret":     {"name": "x", "bucket": "bkt1", "access_key": "a"},
		"bad bucket":    {"name": "x", "bucket": "bad/bucket", "access_key": "a", "secret_key": "s"},
		"bad endpoint":  {"name": "x", "bucket": "bkt1", "access_key": "a", "secret_key": "s", "endpoint": "ftp://x"},
		"no access key": {"name": "x", "bucket": "bkt1", "secret_key": "s"},
	} {
		if w := s3Req(t, srv, http.MethodPost, "/api/s3/accounts", p); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", name, w.Code)
		}
	}
	if list, _ := db.ListS3Accounts(); len(list) != 0 {
		t.Fatalf("invalid accounts were stored: %d", len(list))
	}
}

func TestS3UploadRequiresAccount(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	for name, body := range map[string]string{
		"no account":      `{"source_host_id":1,"source_path":"/etc/hosts"}`,
		"unknown account": `{"source_host_id":1,"source_path":"/etc/hosts","account_id":42}`,
	} {
		w := s3Req(t, srv, http.MethodPost, "/api/transfers/s3", json.RawMessage(body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := s3Req(t, srv, http.MethodPost, "/api/s3/accounts/42/test", nil); w.Code != http.StatusNotFound {
		t.Fatalf("test of unknown account: %d", w.Code)
	}
}
