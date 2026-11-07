package terraform

import (
	"strings"
	"testing"
)

const testSAJSON = `{"type":"service_account","client_email":"a@b.iam","private_key":"k"}`

func TestCloudProvidersOrder(t *testing.T) {
	var ids []string
	for _, p := range CloudProviders() {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, ","); got != "aws,gcp,azure,digitalocean,hetzner,cloudflare,kubernetes" {
		t.Fatalf("order = %s", got)
	}
	if _, ok := CloudProviderByID("nope"); ok {
		t.Fatal("unknown provider found")
	}
}

func TestValidateCloudFields(t *testing.T) {
	aws := map[string]string{"access_key_id": "AKIA1", "secret_access_key": "s3cret"}
	cases := []struct {
		name, provider string
		fields         map[string]string
		wantErr        string // "" = ok
	}{
		{"aws ok", "aws", aws, ""},
		{"unknown provider", "oracle", aws, "unknown cloud provider"},
		{"missing required", "aws", map[string]string{"access_key_id": "AKIA1"}, "required"},
		{"blank required", "aws", map[string]string{"access_key_id": "  ", "secret_access_key": "x"}, "required"},
		{"unknown key", "aws", map[string]string{"access_key_id": "a", "secret_access_key": "b", "bogus": "1"}, "unknown field"},
		{"oversize", "digitalocean", map[string]string{"token": strings.Repeat("a", maxCloudFieldBytes+1)}, "too large"},
		{"max size ok", "digitalocean", map[string]string{"token": strings.Repeat("a", maxCloudFieldBytes)}, ""},
		{"gcp ok", "gcp", map[string]string{"project_id": "p", "credentials_json": testSAJSON}, ""},
		{"gcp bad json", "gcp", map[string]string{"project_id": "p", "credentials_json": "{nope"}, "not valid JSON"},
		{"gcp missing key", "gcp", map[string]string{"project_id": "p", "credentials_json": `{"type":"service_account","client_email":"x"}`}, "private_key"},
		{"gcp missing project", "gcp", map[string]string{"credentials_json": testSAJSON}, "required"},
		{"azure missing tenant", "azure", map[string]string{"subscription_id": "s", "client_id": "c", "client_secret": "x"}, "required"},
		{"hetzner ok", "hetzner", map[string]string{"token": "t"}, ""},
	}
	for _, c := range cases {
		err := ValidateCloudFields(c.provider, c.fields)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestCloudEnv(t *testing.T) {
	cases := []struct {
		provider string
		fields   map[string]string
		want     map[string]string
	}{
		{"aws", map[string]string{"access_key_id": "AK", "secret_access_key": "SK", "session_token": ""}, map[string]string{
			"AWS_ACCESS_KEY_ID": "AK", "AWS_SECRET_ACCESS_KEY": "SK", "AWS_DEFAULT_REGION": "us-east-1", "AWS_REGION": "us-east-1"}},
		{"aws", map[string]string{"access_key_id": " AK ", "secret_access_key": "SK", "session_token": "TOK", "region": "eu-west-1"}, map[string]string{
			"AWS_ACCESS_KEY_ID": "AK", "AWS_SECRET_ACCESS_KEY": "SK", "AWS_SESSION_TOKEN": "TOK", "AWS_DEFAULT_REGION": "eu-west-1", "AWS_REGION": "eu-west-1"}},
		{"gcp", map[string]string{"project_id": "proj", "credentials_json": testSAJSON}, map[string]string{
			"GOOGLE_PROJECT": "proj", "GOOGLE_CLOUD_PROJECT": "proj", "GOOGLE_CREDENTIALS": testSAJSON}},
		{"azure", map[string]string{"subscription_id": "s", "tenant_id": "t", "client_id": "c", "client_secret": "x"}, map[string]string{
			"ARM_SUBSCRIPTION_ID": "s", "ARM_TENANT_ID": "t", "ARM_CLIENT_ID": "c", "ARM_CLIENT_SECRET": "x"}},
		{"digitalocean", map[string]string{"token": "dop"}, map[string]string{"DIGITALOCEAN_TOKEN": "dop"}},
		{"hetzner", map[string]string{"token": "hc"}, map[string]string{"HCLOUD_TOKEN": "hc"}},
		{"nope", map[string]string{"token": "x"}, map[string]string{}},
	}
	for _, c := range cases {
		got := CloudEnv(c.provider, c.fields)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.provider, got, c.want)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q", c.provider, k, got[k], v)
			}
		}
		for k, v := range got {
			if v == "" {
				t.Errorf("%s: empty value for %s", c.provider, k)
			}
		}
	}
}
