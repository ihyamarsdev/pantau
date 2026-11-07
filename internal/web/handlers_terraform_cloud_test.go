package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"pantau/internal/store"
)

const cloudTestSecret = "SUPERSECRETKEY1234567890"

func cloudDo(t *testing.T, srv *Server, auth func(*http.Request), method, path string, body interface{}) (int, map[string]interface{}, string) {
	t.Helper()
	rec := doTFReq(t, srv, auth, method, path, body)
	var m map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, rec.Body.String()
}

func awsBody(name, secret string) map[string]interface{} {
	return map[string]interface{}{
		"name": name, "provider": "aws", "description": "d",
		"fields": map[string]string{"access_key_id": "AKIAEXAMPLE", "secret_access_key": secret, "region": "eu-west-1"},
	}
}

func TestTFCloudAccounts_CreateListNoLeak(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)

	code, created, raw := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("prod-aws", cloudTestSecret))
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, raw)
	}
	if strings.Contains(raw, cloudTestSecret) {
		t.Fatalf("create response leaks secret: %s", raw)
	}

	code, _, raw = cloudDo(t, srv, auth, http.MethodGet, "/api/terraform/cloud-accounts", nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, raw)
	}
	if strings.Contains(raw, cloudTestSecret) {
		t.Fatalf("list leaks secret: %s", raw)
	}
	var list struct {
		Accounts []struct {
			Name       string            `json:"name"`
			Fields     map[string]string `json:"fields"`
			SecretsSet map[string]bool   `json:"secrets_set"`
		} `json:"accounts"`
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Accounts) != 1 || len(list.Providers) != 7 {
		t.Fatalf("unexpected list: %s", raw)
	}
	a := list.Accounts[0]
	if a.Fields["access_key_id"] != "AKIAEXAMPLE" || a.Fields["region"] != "eu-west-1" {
		t.Fatalf("non-secret fields missing: %v", a.Fields)
	}
	if _, ok := a.Fields["secret_access_key"]; ok {
		t.Fatalf("secret key must not appear in fields")
	}
	if !a.SecretsSet["secret_access_key"] || a.SecretsSet["session_token"] {
		t.Fatalf("secrets_set wrong: %v", a.SecretsSet)
	}
	if created["id"] == nil {
		t.Fatal("missing id")
	}
}

func TestTFCloudAccounts_CreateValidation(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"unknown provider", map[string]interface{}{"name": "x", "provider": "nope", "fields": map[string]string{}}, 400},
		{"missing required", map[string]interface{}{"name": "x", "provider": "aws", "fields": map[string]string{"access_key_id": "A"}}, 400},
		{"unknown key", map[string]interface{}{"name": "x", "provider": "hetzner", "fields": map[string]string{"token": "t", "bogus": "1"}}, 400},
		{"empty name", map[string]interface{}{"name": "  ", "provider": "hetzner", "fields": map[string]string{"token": "t"}}, 400},
		{"long name", map[string]interface{}{"name": strings.Repeat("a", 101), "provider": "hetzner", "fields": map[string]string{"token": "t"}}, 400},
		{"ok", map[string]interface{}{"name": "dup", "provider": "hetzner", "fields": map[string]string{"token": "tok-1234"}}, 201},
		{"duplicate nocase", map[string]interface{}{"name": "DUP", "provider": "hetzner", "fields": map[string]string{"token": "tok-1234"}}, 409},
	}
	for _, c := range cases {
		code, _, raw := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", c.body)
		if code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.name, code, c.want, raw)
		}
	}
}

func TestTFCloudAccounts_UpdateSecretKeepAndReplace(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	_, created, _ := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("acct", cloudTestSecret))
	id := int64(created["id"].(float64))
	path := fmt.Sprintf("/api/terraform/cloud-accounts/%d", id)

	// Empty secret keeps the stored one; non-secret changes apply.
	body := awsBody("acct-renamed", "")
	body["fields"].(map[string]string)["region"] = "us-west-2"
	code, _, raw := cloudDo(t, srv, auth, http.MethodPut, path, body)
	if code != http.StatusOK {
		t.Fatalf("put keep: %d %s", code, raw)
	}
	got, _ := srv.db.GetTFCloudAccount(id)
	if got.Fields["secret_access_key"] != cloudTestSecret || got.Fields["region"] != "us-west-2" || got.Name != "acct-renamed" {
		t.Fatalf("keep failed: %+v", got)
	}

	// New secret replaces.
	code, _, raw = cloudDo(t, srv, auth, http.MethodPut, path, awsBody("acct-renamed", "NEWSECRET9999"))
	if code != http.StatusOK || strings.Contains(raw, "NEWSECRET9999") {
		t.Fatalf("put replace: %d %s", code, raw)
	}
	got, _ = srv.db.GetTFCloudAccount(id)
	if got.Fields["secret_access_key"] != "NEWSECRET9999" {
		t.Fatalf("replace failed: %+v", got.Fields)
	}

	// Switching provider must not inherit the old secret.
	code, _, _ = cloudDo(t, srv, auth, http.MethodPut, path, map[string]interface{}{"name": "acct", "provider": "digitalocean", "fields": map[string]string{}})
	if code != http.StatusBadRequest {
		t.Fatalf("provider switch without secret: %d", code)
	}

	// Duplicate name on update.
	cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("other", cloudTestSecret))
	code, _, _ = cloudDo(t, srv, auth, http.MethodPut, path, awsBody("OTHER", ""))
	if code != http.StatusConflict {
		t.Fatalf("dup on update: %d", code)
	}
}

func TestTFCloudAccounts_DeleteUnlinksWorkspaces(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	_, created, _ := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("acct", cloudTestSecret))
	id := int64(created["id"].(float64))

	wsID, err := srv.db.CreateTFWorkspace(&store.TFWorkspace{Name: "ws", CloudAccountIDs: []int64{id}})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := srv.db.TFCloudAccountWorkspaceCount(id); n != 1 {
		t.Fatalf("workspace count = %d", n)
	}
	_, list, _ := cloudDo(t, srv, auth, http.MethodGet, "/api/terraform/cloud-accounts", nil)
	if list["accounts"].([]interface{})[0].(map[string]interface{})["workspace_count"].(float64) != 1 {
		t.Fatalf("workspace_count not reported: %v", list)
	}

	code, _, raw := cloudDo(t, srv, auth, http.MethodDelete, fmt.Sprintf("/api/terraform/cloud-accounts/%d", id), nil)
	if code != http.StatusOK {
		t.Fatalf("delete: %d %s", code, raw)
	}
	ws, _ := srv.db.GetTFWorkspace(wsID)
	if len(ws.CloudAccountIDs) != 0 {
		t.Fatalf("workspace still linked: %v", ws.CloudAccountIDs)
	}
	code, _, _ = cloudDo(t, srv, auth, http.MethodDelete, fmt.Sprintf("/api/terraform/cloud-accounts/%d", id), nil)
	if code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
}

func TestTFCloudAccounts_WorkspaceLinking(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	_, created, _ := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("acct", cloudTestSecret))
	id := int64(created["id"].(float64))

	code, _, raw := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", map[string]interface{}{"name": "bad", "cloud_account_ids": []int64{9999}})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown id on create: %d %s", code, raw)
	}

	code, res, raw := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", map[string]interface{}{"name": "ws", "cloud_account_ids": []int64{id}})
	if code != http.StatusCreated {
		t.Fatalf("create ws: %d %s", code, raw)
	}
	if strings.Contains(raw, cloudTestSecret) {
		t.Fatalf("workspace JSON leaks secret")
	}
	ws := res["workspace"].(map[string]interface{})
	wsID := int64(ws["id"].(float64))
	refs := ws["cloud_accounts"].([]interface{})
	if len(refs) != 1 || refs[0].(map[string]interface{})["name"] != "acct" || refs[0].(map[string]interface{})["provider"] != "aws" {
		t.Fatalf("cloud_accounts wrong: %v", ws["cloud_accounts"])
	}
	wsPath := fmt.Sprintf("/api/terraform/workspaces/%d", wsID)

	// Omitted on PUT keeps the link.
	code, res, raw = cloudDo(t, srv, auth, http.MethodPut, wsPath, map[string]interface{}{"name": "ws", "description": "x"})
	if code != http.StatusOK {
		t.Fatalf("put keep: %d %s", code, raw)
	}
	if ids := res["workspace"].(map[string]interface{})["cloud_account_ids"].([]interface{}); len(ids) != 1 {
		t.Fatalf("link lost on omitted PUT: %v", ids)
	}

	// Unknown id on PUT is rejected and keeps stored links.
	code, _, _ = cloudDo(t, srv, auth, http.MethodPut, wsPath, map[string]interface{}{"name": "ws", "cloud_account_ids": []int64{id, 4242}})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown id on PUT: %d", code)
	}

	// Explicit empty list unlinks, and JSON is never null.
	code, res, raw = cloudDo(t, srv, auth, http.MethodPut, wsPath, map[string]interface{}{"name": "ws", "cloud_account_ids": []int64{}})
	if code != http.StatusOK {
		t.Fatalf("put unlink: %d %s", code, raw)
	}
	if !strings.Contains(raw, `"cloud_account_ids":[]`) || !strings.Contains(raw, `"cloud_accounts":[]`) {
		t.Fatalf("expected empty arrays: %s", raw)
	}
	_ = res
}

func TestTFCloudAccounts_TestEndpoint(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	_, created, _ := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", awsBody("acct", cloudTestSecret))
	id := int64(created["id"].(float64))
	path := fmt.Sprintf("/api/terraform/cloud-accounts/%d/test", id)

	orig := verifyCloud
	t.Cleanup(func() { verifyCloud = orig })

	verifyCloud = func(ctx context.Context, provider string, fields map[string]string, hc *http.Client) (string, error) {
		if provider != "aws" || fields["secret_access_key"] != cloudTestSecret {
			return "", errors.New("wrong inputs")
		}
		return "123456789012", nil
	}
	code, res, raw := cloudDo(t, srv, auth, http.MethodPost, path, nil)
	if code != http.StatusOK || res["ok"] != true || res["identity"] != "123456789012" {
		t.Fatalf("ok case: %d %s", code, raw)
	}
	got, _ := srv.db.GetTFCloudAccount(id)
	if !got.VerifyOK || got.VerifiedAt == nil || got.VerifyMsg != "123456789012" {
		t.Fatalf("verify not stored: %+v", got)
	}

	// Failure: a (buggy) verifier echoing the secret must still not leak it.
	verifyCloud = func(ctx context.Context, provider string, fields map[string]string, hc *http.Client) (string, error) {
		return "", errors.New("denied for " + fields["secret_access_key"])
	}
	code, res, raw = cloudDo(t, srv, auth, http.MethodPost, path, nil)
	if code != http.StatusOK || res["ok"] != false || strings.Contains(raw, cloudTestSecret) {
		t.Fatalf("failure case: %d %s", code, raw)
	}
	got, _ = srv.db.GetTFCloudAccount(id)
	if got.VerifyOK || strings.Contains(got.VerifyMsg, cloudTestSecret) || got.VerifyMsg == "" {
		t.Fatalf("failure not stored safely: %+v", got)
	}
	_, _, raw = cloudDo(t, srv, auth, http.MethodGet, "/api/terraform/cloud-accounts", nil)
	if strings.Contains(raw, cloudTestSecret) {
		t.Fatalf("list leaks secret via verify_message")
	}

	if code, _, _ = cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts/999/test", nil); code != http.StatusNotFound {
		t.Fatalf("missing account: %d", code)
	}
}

func TestTFCloudAccounts_GateOff(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	if err := srv.db.SetTerraformEnabled(false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/api/terraform/cloud-accounts", "/api/terraform/cloud-accounts/1"} {
		if code, _, _ := cloudDo(t, srv, auth, http.MethodGet, p, nil); code != http.StatusForbidden {
			t.Fatalf("%s: got %d want 403", p, code)
		}
	}
}

func TestTFCloudAccounts_RunConfigEnv(t *testing.T) {
	srv, _ := setupTerraformTestDataServer(t)
	db := srv.db

	id1, err := db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "a1", Provider: "aws", Fields: map[string]string{
		"access_key_id": "AKIA1", "secret_access_key": cloudTestSecret, "region": "eu-west-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "a2", Provider: "hetzner", Fields: map[string]string{"token": "hcloud-token-1"}})
	if err != nil {
		t.Fatal(err)
	}
	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "ws", CloudAccountIDs: []int64{id1, id2}})
	if err != nil {
		t.Fatal(err)
	}

	// A workspace env variable with the same key must beat the account value.
	if _, err := db.CreateTFVariable(&store.TFVariable{WorkspaceID: wsID, Key: "AWS_DEFAULT_REGION", Value: "override-region", Category: "env"}); err != nil {
		t.Fatalf("create var: %v", err)
	}

	ws, _ := db.GetTFWorkspace(wsID)
	mgr := NewTerraformRunManager(db, t.TempDir(), nil, nil)
	cfg, err := mgr.BuildRunConfig(ws)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA1",
		"AWS_SECRET_ACCESS_KEY": cloudTestSecret,
		"AWS_REGION":            "eu-west-1",
		"AWS_DEFAULT_REGION":    "override-region",
		"HCLOUD_TOKEN":          "hcloud-token-1",
	}
	for k, v := range want {
		if cfg.Env[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, cfg.Env[k], v)
		}
	}
}
