package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"pantau/internal/store"
)

const importKubeconfig = `apiVersion: v1
clusters:
- name: c
  cluster: {server: "https://k8s.example.com:6443"}
contexts:
- name: ctx1
  context: {cluster: c, user: u}
current-context: ctx1
users:
- name: u
  user: {token: KUBE-USER-TOKEN-SECRET}
`

func TestTFCloudImport(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)
	const cfToken = "CF-API-TOKEN-SECRET-9"
	cfID, err := srv.db.CreateCloudflareAccount(&store.CloudflareAccount{Name: "cf1", AccountID: "acc-7", APIToken: cfToken})
	if err != nil {
		t.Fatal(err)
	}
	cfEmpty, _ := srv.db.CreateCloudflareAccount(&store.CloudflareAccount{Name: "cf-empty"})
	kID, err := srv.db.CreateKubeCluster(&store.KubeCluster{Name: "k1", Context: "ctx1", Kubeconfig: importKubeconfig})
	if err != nil {
		t.Fatal(err)
	}
	kEmpty, _ := srv.db.CreateKubeCluster(&store.KubeCluster{Name: "k-empty"})

	post := func(body map[string]interface{}) (int, string) {
		code, _, raw := cloudDo(t, srv, auth, http.MethodPost, "/api/terraform/cloud-accounts", body)
		return code, raw
	}
	imp := func(typ string, id int64) map[string]interface{} {
		return map[string]interface{}{"type": typ, "id": id}
	}

	// importable: names only, never secrets.
	code, _, raw := cloudDo(t, srv, auth, http.MethodGet, "/api/terraform/cloud-accounts/importable", nil)
	if code != http.StatusOK {
		t.Fatalf("importable: %d %s", code, raw)
	}
	for _, leak := range []string{cfToken, "KUBE-USER-TOKEN-SECRET", "k8s.example.com"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("importable leaks %q: %s", leak, raw)
		}
	}
	for _, want := range []string{`"name":"cf1"`, `"name":"k1"`, `"usable":true`, `"usable":false`} {
		if !strings.Contains(raw, want) {
			t.Errorf("importable lacks %s: %s", want, raw)
		}
	}

	// Success: cloudflare (provider overridden), request field wins when non-empty.
	code, raw = post(map[string]interface{}{"name": "cf-acct", "provider": "aws", "import_from": imp("cloudflare", cfID), "fields": map[string]string{"account_id": "mine"}})
	if code != http.StatusCreated || strings.Contains(raw, cfToken) || !strings.Contains(raw, `"provider":"cloudflare"`) || !strings.Contains(raw, `"account_id":"mine"`) {
		t.Fatalf("cf import: %d %s", code, raw)
	}
	acct, _ := srv.db.GetTFCloudAccount(1)
	if acct == nil || acct.Fields["api_token"] != cfToken {
		t.Fatalf("token not copied: %+v", acct)
	}
	// Copy, not a live reference: changing the source does not change the account.
	_ = srv.db.UpdateCloudflareAccount(&store.CloudflareAccount{ID: cfID, Name: "cf1", APIToken: "changed-token-1"})
	acct, _ = srv.db.GetTFCloudAccount(1)
	if acct.Fields["api_token"] != cfToken {
		t.Fatal("import is a live reference")
	}

	code, raw = post(map[string]interface{}{"name": "k-acct", "import_from": imp("kubernetes", kID)})
	if code != http.StatusCreated || strings.Contains(raw, "KUBE-USER-TOKEN-SECRET") || !strings.Contains(raw, `"context":"ctx1"`) || !strings.Contains(raw, `"kubeconfig":true`) {
		t.Fatalf("kube import: %d %s", code, raw)
	}

	// Failures: 400 for missing source, unusable credential, unknown type.
	for name, body := range map[string]map[string]interface{}{
		"cf missing":   {"name": "x1", "import_from": imp("cloudflare", 9999)},
		"cf unusable":  {"name": "x2", "import_from": imp("cloudflare", cfEmpty)},
		"kube missing": {"name": "x3", "import_from": imp("kubernetes", 9999)},
		"kube empty":   {"name": "x4", "import_from": imp("kubernetes", kEmpty)},
		"bad type":     {"name": "x5", "import_from": imp("aws", 1)},
	} {
		if code, raw := post(body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, code, raw)
		}
	}
}

func TestTFCloudBuildRunConfigPlainAndAssumeRole(t *testing.T) {
	srv, _ := setupTerraformTestDataServer(t)
	db := srv.db
	old := resolveCloudEnv
	t.Cleanup(func() { resolveCloudEnv = old })
	var failFor string
	resolveCloudEnv = func(ctx context.Context, provider string, fields map[string]string, hc *http.Client) (map[string]string, error) {
		if failFor != "" && fields["role_arn"] == failFor {
			return nil, errAssume
		}
		if fields["role_arn"] != "" {
			return map[string]string{"AWS_ACCESS_KEY_ID": "ASIATMP", "AWS_SECRET_ACCESS_KEY": "tmpsecret", "AWS_SESSION_TOKEN": "tmptoken", "AWS_REGION": "eu-west-1"}, nil
		}
		return old(ctx, provider, fields, hc)
	}

	role, _ := db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "role", Provider: "aws", Fields: map[string]string{
		"access_key_id": "AKIA1", "secret_access_key": "basesecret1", "role_arn": "arn:aws:iam::1:role/r"}})
	kube, _ := db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "kube", Provider: "kubernetes", Fields: map[string]string{
		"kubeconfig": importKubeconfig, "context": "ctx1"}})
	cf, _ := db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "cf", Provider: "cloudflare", Fields: map[string]string{"api_token": "cftoken-1234", "account_id": "acc"}})
	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "ws", Source: "inline", CloudAccountIDs: []int64{role, kube, cf}})
	if err != nil {
		t.Fatal(err)
	}
	// A user env variable on a plain key overrides and un-marks it.
	if _, err := db.CreateTFVariable(&store.TFVariable{WorkspaceID: wsID, Key: "CLOUDFLARE_ACCOUNT_ID", Value: "user-acc", Category: "env"}); err != nil {
		t.Fatal(err)
	}
	ws, _ := db.GetTFWorkspace(wsID)
	mgr := NewTerraformRunManager(db, t.TempDir(), nil, nil)
	cfg, err := mgr.BuildRunConfig(ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env["AWS_SESSION_TOKEN"] != "tmptoken" || cfg.Env["AWS_SECRET_ACCESS_KEY"] != "tmpsecret" || cfg.Env["KUBE_CTX"] != "ctx1" {
		t.Fatalf("env = %v", cfg.Env)
	}
	for _, k := range []string{"AWS_ACCESS_KEY_ID", "AWS_REGION", "KUBE_CTX"} {
		if !cfg.PlainEnv[k] {
			t.Errorf("%s should be plain", k)
		}
	}
	for _, k := range []string{"AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"} {
		if cfg.PlainEnv[k] {
			t.Errorf("%s must not be plain", k)
		}
	}
	if cfg.Env["CLOUDFLARE_ACCOUNT_ID"] != "user-acc" {
		t.Errorf("user variable lost: %v", cfg.Env)
	}
	if cfg.EnvFiles["KUBECONFIG"] != importKubeconfig || cfg.EnvFiles["KUBE_CONFIG_PATH"] != importKubeconfig || cfg.Env["KUBECONFIG"] != "" {
		t.Errorf("env files = %v", cfg.EnvFiles)
	}

	// Failure aborts with an error naming the account, never the secret.
	failFor = "arn:aws:iam::1:role/r"
	_, err = mgr.BuildRunConfig(ws)
	if err == nil || !strings.Contains(err.Error(), `"role"`) || strings.Contains(err.Error(), "basesecret1") {
		t.Fatalf("err = %v", err)
	}
}

type assumeErr string

func (e assumeErr) Error() string { return string(e) }

var errAssume = assumeErr("assume role: HTTP 403 (credentials rejected): AccessDenied")
