package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/cloudflare"
	"pantau/internal/store"
)

func cfDo(srv *Server, authReq func(*http.Request), method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	authReq(req)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	return w
}

func setupCFFake(t *testing.T, acct store.CloudflareAccount) (*Server, func(*http.Request), int64) {
	t.Helper()
	srv, authReq := setupCFTestServer(t)
	fake := fakeCloudflareServer(t, true)
	t.Cleanup(fake.Close)
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fake.URL
		return c
	}
	acct.APIToken = "token123"
	id, err := srv.db.CreateCloudflareAccount(&acct)
	if err != nil {
		t.Fatal(err)
	}
	return srv, authReq, id
}

func TestCloudflareAccessRulesEndpoints(t *testing.T) {
	t.Parallel()
	srv, authReq, id := setupCFFake(t, store.CloudflareAccount{Name: "acc"})
	const zid, rid = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	base := fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/access-rules", id, zid)

	w := cfDo(srv, authReq, http.MethodGet, base, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Rules   []cloudflare.AccessRule `json:"rules"`
		Modes   []string                `json:"modes"`
		Targets []string                `json:"targets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Rules) != 1 || list.Rules[0].Value != "203.0.113.7" || len(list.Modes) != 5 || len(list.Targets) != 4 {
		t.Fatalf("list body: %s", w.Body.String())
	}

	// Value is normalized and the rule is created.
	w = cfDo(srv, authReq, http.MethodPost, base, `{"mode":"block","target":"asn","value":"13335","notes":"abuse"}`)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), "AS13335") {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	// Invalid input never reaches Cloudflare.
	for _, body := range []string{`{"mode":"block","target":"ip","value":"not-an-ip"}`, `{"mode":"nuke","target":"ip","value":"1.2.3.4"}`, `{`} {
		if w := cfDo(srv, authReq, http.MethodPost, base, body); w.Code != http.StatusBadRequest {
			t.Fatalf("create %s: want 400, got %d", body, w.Code)
		}
	}

	if w := cfDo(srv, authReq, http.MethodDelete, base+"/"+rid, ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w := cfDo(srv, authReq, http.MethodDelete, base+"/not-an-id", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("delete with bad id: %d", w.Code)
	}
	if w := cfDo(srv, authReq, http.MethodGet, base+"/"+rid, ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on a rule: %d", w.Code)
	}
	if w := cfDo(srv, authReq, http.MethodPut, base, "{}"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT on the collection: %d", w.Code)
	}
}

func TestCloudflareDNSExportEndpoint(t *testing.T) {
	t.Parallel()
	srv, authReq, id := setupCFFake(t, store.CloudflareAccount{Name: "acc"})
	const zid = "0123456789abcdef0123456789abcdef"

	w := cfDo(srv, authReq, http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns/export", id, zid), "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "1.2.3.4") {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, zid+".zone") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if w := cfDo(srv, authReq, http.MethodPost, fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns/export", id, zid), ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST export: %d", w.Code)
	}
}

func TestCloudflareTunnelsEndpoint(t *testing.T) {
	t.Parallel()
	// The Cloudflare account id is stored with the account ...
	srv, authReq, id := setupCFFake(t, store.CloudflareAccount{Name: "with-id", AccountID: cfFakeAccountID})
	w := cfDo(srv, authReq, http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/tunnels", id), "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"edge-tunnel"`) || !strings.Contains(w.Body.String(), `"healthy"`) {
		t.Fatalf("tunnels with stored id: %d %s", w.Code, w.Body.String())
	}
	if w := cfDo(srv, authReq, http.MethodPost, fmt.Sprintf("/api/cloudflare/accounts/%d/tunnels", id), ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST tunnels: %d", w.Code)
	}

	// ... or it is derived from the zones of the token.
	srv, authReq, id = setupCFFake(t, store.CloudflareAccount{Name: "no-id"})
	w = cfDo(srv, authReq, http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/tunnels", id), "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"edge-tunnel"`) {
		t.Fatalf("tunnels with derived id: %d %s", w.Code, w.Body.String())
	}
}

func TestCloudflareForbiddenIsFlagged(t *testing.T) {
	t.Parallel()
	srv, authReq := setupCFTestServer(t)
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"code": 10000, "message": "Authentication error"}}})
	}))
	defer denied.Close()
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = denied.URL
		return c
	}
	id, err := srv.db.CreateCloudflareAccount(&store.CloudflareAccount{Name: "acc", AccountID: cfFakeAccountID, APIToken: "token123"})
	if err != nil {
		t.Fatal(err)
	}
	const zid = "0123456789abcdef0123456789abcdef"
	for _, path := range []string{
		fmt.Sprintf("/api/cloudflare/accounts/%d/tunnels", id),
		fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/access-rules", id, zid),
		fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s/dns/export", id, zid),
	} {
		w := cfDo(srv, authReq, http.MethodGet, path, "")
		var body struct {
			Error     string `json:"error"`
			Forbidden bool   `json:"forbidden"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v (%s)", path, err, w.Body.String())
		}
		// 400, not 401/403: the browser must not read it as an expired Pantau session.
		if w.Code != http.StatusBadRequest || !body.Forbidden || body.Error == "" || strings.Contains(w.Body.String(), "token123") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
