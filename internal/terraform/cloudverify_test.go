package terraform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// setEndpoints points the verifiers at srv for the duration of the test.
func setEndpoints(t *testing.T, srv *httptest.Server) {
	old := cloudEndpoints
	t.Cleanup(func() { cloudEndpoints = old })
	cloudEndpoints.STS = srv.URL + "/sts/%s"
	cloudEndpoints.GCPToken = srv.URL + "/token"
	cloudEndpoints.GCPProject = srv.URL + "/project/%s"
	cloudEndpoints.AzureLogin = srv.URL + "/azure/%s/token"
	cloudEndpoints.DOAccount = srv.URL + "/do"
	cloudEndpoints.HetznerServers = srv.URL + "/hz"
}

// Documented AWS SigV4 example (iam ListUsers, 2015-08-30).
func TestSignV4KnownAnswer(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	ts := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	signV4(req, nil, "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "", "us-east-1", "iam", ts)
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, " +
		"SignedHeaders=content-type;host;x-amz-date, " +
		"Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n got %s\nwant %s", got, want)
	}
}

func testGCPKey(t *testing.T) (saJSON string) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	pk := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	b, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "sa@p.iam", "private_key": pk})
	return string(b)
}

func TestVerifyCloud(t *testing.T) {
	const secret = "TOPSECRET-value-123"
	saJSON := testGCPKey(t)
	// fail=true makes every endpoint reject with the status below.
	var fail bool
	var failStatus int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(failStatus)
			// echo the secret to prove it gets scrubbed
			w.Write([]byte(`{"error":"denied ` + secret + `"}`))
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/sts/"):
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIA1/") || r.Header.Get("X-Amz-Security-Token") != "tok" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`<GetCallerIdentityResponse><GetCallerIdentityResult><Arn>arn:aws:iam::123:user/x</Arn><Account>123</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`))
		case r.URL.Path == "/token":
			r.ParseForm()
			if parts := strings.Split(r.PostForm.Get("assertion"), "."); len(parts) != 3 {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"access_token":"gtok"}`))
		case strings.HasPrefix(r.URL.Path, "/project/"):
			if r.Header.Get("Authorization") != "Bearer gtok" || r.URL.Path != "/project/proj-1" {
				w.WriteHeader(403)
				return
			}
			w.Write([]byte(`{"projectId":"proj-1"}`))
		case r.URL.Path == "/azure/tenant-1/token":
			r.ParseForm()
			if r.PostForm.Get("client_secret") != secret || r.PostForm.Get("grant_type") != "client_credentials" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"access_token":"x"}`))
		case r.URL.Path == "/do":
			if r.Header.Get("Authorization") != "Bearer "+secret {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"account":{"email":"me@example.com"}}`))
		case r.URL.Path == "/hz":
			if r.Header.Get("Authorization") != "Bearer "+secret {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"servers":[]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	setEndpoints(t, srv)

	cases := []struct {
		provider string
		fields   map[string]string
		want     string
	}{
		{"aws", map[string]string{"access_key_id": "AKIA1", "secret_access_key": secret, "session_token": "tok"}, "123 (arn:aws:iam::123:user/x)"},
		{"gcp", map[string]string{"project_id": "proj-1", "credentials_json": saJSON}, "proj-1"},
		{"azure", map[string]string{"subscription_id": "s", "tenant_id": "tenant-1", "client_id": "c", "client_secret": secret}, "tenant-1"},
		{"digitalocean", map[string]string{"token": secret}, "me@example.com"},
		{"hetzner", map[string]string{"token": secret}, "ok"},
	}
	for _, c := range cases {
		fail = false
		id, err := VerifyCloud(context.Background(), c.provider, c.fields, srv.Client())
		if err != nil || id != c.want {
			t.Errorf("%s success: id=%q err=%v, want %q", c.provider, id, err, c.want)
		}
		for _, st := range []int{401, 403} {
			fail, failStatus = true, st
			id, err := VerifyCloud(context.Background(), c.provider, c.fields, srv.Client())
			if err == nil || id != "" {
				t.Errorf("%s %d: expected error, got id=%q", c.provider, st, id)
				continue
			}
			if c.provider != "gcp" && strings.Contains(err.Error(), secret) { // gcp fields do not hold the secret
				t.Errorf("%s %d: error leaks secret: %v", c.provider, st, err)
			}
			if !strings.Contains(err.Error(), "HTTP") {
				t.Errorf("%s %d: error lacks status: %v", c.provider, st, err)
			}
		}
	}
}

func TestVerifyCloudErrorsHideSecret(t *testing.T) {
	const secret = "TOPSECRET-value-123"
	// Unreachable endpoint with secret embedded in the URL: error must not echo it.
	old := cloudEndpoints
	t.Cleanup(func() { cloudEndpoints = old })
	cloudEndpoints.DOAccount = "http://127.0.0.1:1/?k=" + url.QueryEscape(secret)
	_, err := VerifyCloud(context.Background(), "digitalocean", map[string]string{"token": secret}, &http.Client{Timeout: time.Second})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
	// Validation failure and bad GCP PEM.
	if _, err := VerifyCloud(context.Background(), "digitalocean", nil, nil); err == nil {
		t.Fatal("expected validation error")
	}
	_, err = VerifyCloud(context.Background(), "gcp", map[string]string{"project_id": "p", "credentials_json": testSAJSON}, nil)
	if err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Fatalf("err = %v", err)
	}
}
