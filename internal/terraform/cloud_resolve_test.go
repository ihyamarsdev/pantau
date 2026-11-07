package terraform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKubeconfig = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: c1
  cluster: {server: "https://other.example:6443"}
- name: c2
  cluster: {server: "https://k8s.example.com:6443"}
contexts:
- name: dev
  context: {cluster: c1, user: u}
- name: prod
  context: {cluster: c2, user: u}
users:
- name: u
  user: {token: kubetoken-very-secret}
`

func TestCloudAssumeRole(t *testing.T) {
	const secret, ext = "BASE-SECRET-KEY-123", "EXTERNAL-ID-XYZ"
	var fail bool
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm.Encode()
		if fail {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`<ErrorResponse><Error><Code>AccessDenied</Code><Message>` + secret + `</Message></Error></ErrorResponse>`))
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIA1/") || !strings.Contains(r.Header.Get("Authorization"), "/sts/aws4_request") {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`<AssumeRoleResponse><AssumeRoleResult><Credentials><AccessKeyId>ASIATEMP</AccessKeyId><SecretAccessKey>tempsecret</SecretAccessKey><SessionToken>temptoken</SessionToken></Credentials><AssumedRoleUser><Arn>arn:aws:sts::999:assumed-role/tf/pantau</Arn></AssumedRoleUser></AssumeRoleResult></AssumeRoleResponse>`))
	}))
	defer srv.Close()
	setEndpoints(t, srv)

	fields := map[string]string{"access_key_id": "AKIA1", "secret_access_key": secret, "region": "eu-west-1",
		"role_arn": "arn:aws:iam::999:role/tf", "external_id": ext}
	env, err := ResolveCloudEnv(context.Background(), "aws", fields, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"AWS_ACCESS_KEY_ID": "ASIATEMP", "AWS_SECRET_ACCESS_KEY": "tempsecret", "AWS_SESSION_TOKEN": "temptoken", "AWS_REGION": "eu-west-1", "AWS_DEFAULT_REGION": "eu-west-1"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, env[k], v)
		}
	}
	for _, sub := range []string{"Action=AssumeRole", "ExternalId=" + ext, "RoleSessionName=pantau", "DurationSeconds=3600", "RoleArn=arn%3Aaws%3Aiam%3A%3A999%3Arole%2Ftf"} {
		if !strings.Contains(form, sub) {
			t.Errorf("request form %q lacks %s", form, sub)
		}
	}

	id, err := VerifyCloud(context.Background(), "aws", fields, srv.Client())
	if err != nil || id != "999 (arn:aws:sts::999:assumed-role/tf/pantau)" {
		t.Fatalf("verify: %q %v", id, err)
	}

	// Without role_arn ResolveCloudEnv equals CloudEnv and makes no call.
	plain := map[string]string{"access_key_id": "AKIA1", "secret_access_key": secret}
	env, err = ResolveCloudEnv(context.Background(), "aws", plain, nil)
	if err != nil || env["AWS_ACCESS_KEY_ID"] != "AKIA1" || env["AWS_SESSION_TOKEN"] != "" {
		t.Fatalf("no-role env = %v %v", env, err)
	}

	fail = true
	for _, f := range []func() error{
		func() error { _, err := ResolveCloudEnv(context.Background(), "aws", fields, srv.Client()); return err },
		func() error { _, err := VerifyCloud(context.Background(), "aws", fields, srv.Client()); return err },
	} {
		err := f()
		if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
			t.Fatalf("err = %v", err)
		}
		for _, s := range []string{secret, ext, "ASIATEMP"} {
			if strings.Contains(err.Error(), s) {
				t.Errorf("error leaks %q: %v", s, err)
			}
		}
	}
}

func TestCloudCloudflareAndKubernetes(t *testing.T) {
	const token = "CF-TOKEN-SECRET-1"
	var status = "active"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cf" || r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{"id":"tok-id","status":"` + status + `"}}`))
	}))
	defer srv.Close()
	setEndpoints(t, srv)

	cf := map[string]string{"api_token": token, "account_id": "acc1"}
	if id, err := VerifyCloud(context.Background(), "cloudflare", cf, srv.Client()); err != nil || id != "token tok-id (active)" {
		t.Fatalf("cf verify: %q %v", id, err)
	}
	status = "disabled"
	if _, err := VerifyCloud(context.Background(), "cloudflare", cf, srv.Client()); err == nil {
		t.Fatal("expected inactive token error")
	}
	if _, err := VerifyCloud(context.Background(), "cloudflare", map[string]string{"api_token": "other-token"}, srv.Client()); err == nil || strings.Contains(err.Error(), "other-token") {
		t.Fatalf("rejected token err = %v", err)
	}
	env := CloudEnv("cloudflare", cf)
	if env["CLOUDFLARE_API_TOKEN"] != token || env["CLOUDFLARE_ACCOUNT_ID"] != "acc1" {
		t.Fatalf("cf env = %v", env)
	}

	k := map[string]string{"kubeconfig": testKubeconfig}
	// No network client needed: the verifier must not call out.
	if id, err := VerifyCloud(context.Background(), "kubernetes", k, &http.Client{Transport: failTransport{}}); err != nil || id != "server k8s.example.com:6443" {
		t.Fatalf("kube verify: %q %v", id, err)
	}
	if id, _ := VerifyCloud(context.Background(), "kubernetes", map[string]string{"kubeconfig": testKubeconfig, "context": "dev"}, nil); id != "server other.example:6443" {
		t.Fatalf("kube ctx verify: %q", id)
	}
	if env := CloudEnv("kubernetes", map[string]string{"kubeconfig": testKubeconfig, "context": "dev"}); len(env) != 1 || env["KUBE_CTX"] != "dev" {
		t.Fatalf("kube env = %v", env)
	}
	files := CloudEnvFiles("kubernetes", k)
	if len(files) != 2 || files["KUBE_CONFIG_PATH"] != testKubeconfig || files["KUBECONFIG"] != testKubeconfig {
		t.Fatalf("kube files = %v", files)
	}

	for name, f := range map[string]map[string]string{
		"missing":    {},
		"bad yaml":   {"kubeconfig": "a: [unclosed"},
		"no cluster": {"kubeconfig": "apiVersion: v1\nclusters: []\n"},
		"bad ctx":    {"kubeconfig": testKubeconfig, "context": "nope"},
	} {
		if err := ValidateCloudFields("kubernetes", f); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	if err := ValidateCloudFields("aws", map[string]string{"access_key_id": "a", "secret_access_key": "b", "role_arn": "role/x"}); err == nil {
		t.Error("role_arn without arn: prefix accepted")
	}
	if err := ValidateCloudFields("aws", map[string]string{"access_key_id": "a", "secret_access_key": "b", "session_name": "x"}); err == nil {
		t.Error("short session_name accepted")
	}
	if got := strings.Join(CloudPlainEnv("kubernetes"), ","); got != "KUBE_CTX" {
		t.Errorf("plain kube = %s", got)
	}
}

type failTransport struct{}

func (failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("unexpected network call")
}
