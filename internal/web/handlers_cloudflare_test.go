package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/cloudflare"
	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func setupCFTestServer(t *testing.T) (*Server, func(*http.Request)) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_cf.db")
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
	token := "cf_test_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, authReq
}

func fakeCloudflareServer(t *testing.T, verifyOk bool) *httptest.Server {
	t.Helper()
	validZid := "0123456789abcdef0123456789abcdef"
	validRid := "fedcba9876543210fedcba9876543210"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"errors":  []map[string]interface{}{{"code": 1000, "message": "Invalid token"}},
			})
			return
		}

		path := r.URL.Path

		// Token verification endpoints
		if path == "/user/tokens/verify" || strings.HasSuffix(path, "/tokens/verify") {
			if !verifyOk {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"errors":  []map[string]interface{}{{"code": 1000, "message": "Invalid token"}},
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"errors":  []interface{}{},
				"result": map[string]interface{}{
					"id":     "token123",
					"status": "active",
				},
			})
			return
		}

		// Zones list
		if path == "/zones" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"errors":  []interface{}{},
				"result": []map[string]interface{}{
					{
						"id":           validZid,
						"name":         "example.com",
						"status":       "active",
						"paused":       false,
						"type":         "full",
						"name_servers": []string{"ns1.cloudflare.com", "ns2.cloudflare.com"},
						"plan":         map[string]interface{}{"name": "Free Plan"},
						"account":      map[string]interface{}{"name": "My Account"},
					},
				},
				"result_info": map[string]interface{}{"page": 1, "total_pages": 1},
			})
			return
		}

		// DNS records
		if path == "/zones/"+validZid+"/dns_records" {
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"errors":  []interface{}{},
					"result": []map[string]interface{}{
						{
							"id":      validRid,
							"type":    "A",
							"name":    "example.com",
							"content": "1.2.3.4",
							"ttl":     1,
						},
					},
					"result_info": map[string]interface{}{"page": 1, "total_pages": 1},
				})
				return
			case http.MethodPost:
				var in cloudflare.DNSRecordInput
				_ = json.NewDecoder(r.Body).Decode(&in)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"errors":  []interface{}{},
					"result": map[string]interface{}{
						"id":      validRid,
						"type":    in.Type,
						"name":    in.Name,
						"content": in.Content,
						"ttl":     in.TTL,
					},
				})
				return
			}
		}

		// Single DNS record
		if path == "/zones/"+validZid+"/dns_records/"+validRid {
			switch r.Method {
			case http.MethodPut:
				var in cloudflare.DNSRecordInput
				_ = json.NewDecoder(r.Body).Decode(&in)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"errors":  []interface{}{},
					"result": map[string]interface{}{
						"id":      validRid,
						"type":    in.Type,
						"name":    in.Name,
						"content": in.Content,
						"ttl":     in.TTL,
					},
				})
				return
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"errors":  []interface{}{},
					"result":  map[string]interface{}{"id": validRid},
				})
				return
			}
		}

		// Purge cache
		if path == "/zones/"+validZid+"/purge_cache" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"errors":  []interface{}{},
				"result":  map[string]interface{}{"id": validZid},
			})
			return
		}

		// Zone settings
		if path == "/zones/"+validZid+"/settings" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"errors":  []interface{}{},
				"result": []map[string]interface{}{
					{"id": "ssl", "value": "full", "editable": true},
					{"id": "always_use_https", "value": "on", "editable": true},
					{"id": "ignored_setting", "value": "some", "editable": true},
				},
			})
			return
		}

		if path == "/zones/"+validZid+"/settings/ssl" && r.Method == http.MethodPatch {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"errors":  []interface{}{},
				"result": map[string]interface{}{
					"id":       "ssl",
					"value":    body["value"],
					"editable": true,
				},
			})
			return
		}

		// Upstream error trigger
		if strings.HasSuffix(path, "/error500") {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"errors":  []map[string]interface{}{{"code": 9999, "message": "Upstream internal error"}},
			})
			return
		}

		http.NotFound(w, r)
	})

	return httptest.NewServer(handler)
}

func TestCloudflareAuthRejected(t *testing.T) {
	srv, _ := setupCFTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthorized, got %d", w.Code)
	}
}

func TestCloudflareAccountLifecycleAndSecurity(t *testing.T) {
	srv, authReq := setupCFTestServer(t)
	fakeCF := fakeCloudflareServer(t, true)
	defer fakeCF.Close()

	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fakeCF.URL
		return c
	}

	// 1. POST fails when token verification fails
	fakeCFBad := fakeCloudflareServer(t, false)
	defer fakeCFBad.Close()
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fakeCFBad.URL
		return c
	}

	createPayload := `{"name":"Acct 1","api_token":"bad_token"}`
	req := httptest.NewRequest(http.MethodPost, "/api/cloudflare/accounts", strings.NewReader(createPayload))
	authReq(req)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 bad request on bad token verification, got %d: %s", w.Code, w.Body.String())
	}

	// Verify nothing was stored
	accounts, err := srv.db.ListCloudflareAccounts()
	if err != nil {
		t.Fatalf("db list failed: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("expected 0 accounts stored after failed verification, got %d", len(accounts))
	}

	// 2. POST succeeds with valid token
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fakeCF.URL
		return c
	}

	createPayload = `{"name":"Production Acct","api_token":"secret_token_123"}`
	req = httptest.NewRequest(http.MethodPost, "/api/cloudflare/accounts", strings.NewReader(createPayload))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 created, got %d: %s", w.Code, w.Body.String())
	}

	if strings.Contains(w.Body.String(), "secret_token_123") || strings.Contains(w.Body.String(), "api_token") {
		t.Fatalf("secret token leaked in create response: %s", w.Body.String())
	}

	var createResp struct {
		Account store.CloudflareAccount `json:"account"`
		Token   cloudflare.TokenInfo    `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp failed: %v", err)
	}
	if createResp.Account.ID <= 0 || createResp.Account.Name != "Production Acct" {
		t.Fatalf("unexpected account in create resp: %+v", createResp.Account)
	}
	if createResp.Token.Status != "active" {
		t.Fatalf("expected active token status, got: %+v", createResp.Token)
	}

	acctID := createResp.Account.ID

	// 3. GET /api/cloudflare/accounts -> token never returned
	req = httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret_token_123") || strings.Contains(w.Body.String(), "api_token") {
		t.Fatalf("secret token leaked in list response: %s", w.Body.String())
	}

	// 4. PUT without api_token keeps stored token
	putPayload := `{"name":"Production Acct Updated"}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/cloudflare/accounts/%d", acctID), strings.NewReader(putPayload))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok on put, got %d: %s", w.Code, w.Body.String())
	}

	stored, err := srv.db.GetCloudflareAccount(acctID)
	if err != nil || stored == nil {
		t.Fatalf("failed to reload stored account: %v", err)
	}
	if stored.Name != "Production Acct Updated" {
		t.Fatalf("expected updated name, got %s", stored.Name)
	}
	if stored.APIToken != "secret_token_123" {
		t.Fatalf("expected stored token to be preserved, got %q", stored.APIToken)
	}

	// 5. Unknown account -> 404
	req = httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts/999999/zones", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 not found, got %d", w.Code)
	}

	// 6. Invalid method -> 405
	req = httptest.NewRequest(http.MethodPatch, "/api/cloudflare/accounts", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 method not allowed, got %d", w.Code)
	}

	// 7. DELETE /api/cloudflare/accounts/{id}
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/cloudflare/accounts/%d", acctID), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok on delete, got %d: %s", w.Code, w.Body.String())
	}

	storedAfterDelete, err := srv.db.GetCloudflareAccount(acctID)
	if err != nil {
		t.Fatalf("get account failed: %v", err)
	}
	if storedAfterDelete != nil {
		t.Fatalf("expected account to be deleted")
	}
}

func TestCloudflareProxyEndpoints(t *testing.T) {
	srv, authReq := setupCFTestServer(t)
	fakeCF := fakeCloudflareServer(t, true)
	defer fakeCF.Close()

	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fakeCF.URL
		return c
	}

	acct := &store.CloudflareAccount{
		Name:     "Test Acct",
		APIToken: "token123",
	}
	acctID, err := srv.db.CreateCloudflareAccount(acct)
	if err != nil {
		t.Fatalf("create account failed: %v", err)
	}

	validZid := "0123456789abcdef0123456789abcdef"
	validRid := "fedcba9876543210fedcba9876543210"

	// 1. Invalid zone ID -> 400 without hitting upstream
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/invalid-zid/dns", acctID), nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 bad request for invalid zid, got %d", w.Code)
	}

	// Invalid record ID -> 400
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns/invalid-rid", acctID, validZid), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 bad request for invalid rid, got %d", w.Code)
	}

	// 2. GET /zones happy path
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones", acctID), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for zones, got %d: %s", w.Code, w.Body.String())
	}
	var zonesResp struct {
		Zones []cloudflare.Zone `json:"zones"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &zonesResp); err != nil {
		t.Fatalf("unmarshal zones failed: %v", err)
	}
	if len(zonesResp.Zones) != 1 || zonesResp.Zones[0].Name != "example.com" {
		t.Fatalf("unexpected zones response: %+v", zonesResp)
	}

	// 3. DNS records: GET, POST, PUT, DELETE
	// GET
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns", acctID, validZid), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for dns list, got %d: %s", w.Code, w.Body.String())
	}

	// POST
	dnsBody := `{"type":"A","name":"sub.example.com","content":"1.1.1.1","ttl":1}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns", acctID, validZid), strings.NewReader(dnsBody))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 created for dns create, got %d: %s", w.Code, w.Body.String())
	}

	// PUT
	dnsPutBody := `{"type":"A","name":"sub.example.com","content":"1.1.1.2","ttl":120}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns/%s", acctID, validZid, validRid), strings.NewReader(dnsPutBody))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for dns update, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns/%s", acctID, validZid, validRid), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for dns delete, got %d: %s", w.Code, w.Body.String())
	}

	// 4. Purge cache happy path
	purgeBody := `{"purge_everything":true}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/purge", acctID, validZid), strings.NewReader(purgeBody))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for purge cache, got %d: %s", w.Code, w.Body.String())
	}

	// Purge cache invalid body (multiple modes) -> 400
	badPurgeBody := `{"purge_everything":true,"files":["https://example.com/a"]}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/purge", acctID, validZid), strings.NewReader(badPurgeBody))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 bad request for bad purge, got %d", w.Code)
	}

	// 5. Settings: GET, PUT allowlist and PUT outside allowlist
	// GET
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/settings", acctID, validZid), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for settings, got %d: %s", w.Code, w.Body.String())
	}
	var setResp struct {
		Settings []cloudflare.Setting `json:"settings"`
		Options  map[string][]string  `json:"options"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &setResp); err != nil {
		t.Fatalf("unmarshal settings failed: %v", err)
	}
	if len(setResp.Settings) != 2 || len(setResp.Options) == 0 {
		t.Fatalf("unexpected settings response: %+v", setResp)
	}

	// PUT outside allowlist -> 400
	badSettingBody := `{"id":"ssl","value":"bogus_mode"}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/settings", acctID, validZid), strings.NewReader(badSettingBody))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 bad request for setting outside allowlist, got %d", w.Code)
	}

	// PUT valid allowlist -> 200
	goodSettingBody := `{"id":"ssl","value":"strict"}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/settings", acctID, validZid), strings.NewReader(goodSettingBody))
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ok for valid setting, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Upstream error mapping:
	// Upstream 500 -> 502 Bad Gateway
	// Upstream 401/403 -> 400 Bad Request
	errCF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "dns_records") {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"errors":  []map[string]interface{}{{"code": 10000, "message": "Authentication error"}},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"errors":  []map[string]interface{}{{"code": 9999, "message": "Cloudflare internal failure"}},
		})
	}))
	defer errCF.Close()

	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = errCF.URL
		return c
	}

	// Upstream 403 mapped to 400
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns", acctID, validZid), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected upstream 403 to be mapped to 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Authentication error") {
		t.Fatalf("expected Cloudflare error message in body, got: %s", w.Body.String())
	}

	// Upstream 500 mapped to 502
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones", acctID), nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected upstream 500 to be mapped to 502, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Cloudflare internal failure") {
		t.Fatalf("expected Cloudflare error message in body, got: %s", w.Body.String())
	}
}
