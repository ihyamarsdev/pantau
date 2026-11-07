package terraform

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// cloudEndpoints holds the provider API URLs; tests point them at httptest servers.
// %s verbs: STS region, GCP project, Azure tenant.
var cloudEndpoints = struct {
	STS, GCPToken, GCPProject, AzureLogin, DOAccount, HetznerServers, CloudflareVerify string
}{
	STS:              "https://sts.%s.amazonaws.com/",
	GCPToken:         "https://oauth2.googleapis.com/token",
	GCPProject:       "https://cloudresourcemanager.googleapis.com/v1/projects/%s",
	AzureLogin:       "https://login.microsoftonline.com/%s/oauth2/v2.0/token",
	DOAccount:        "https://api.digitalocean.com/v2/account",
	HetznerServers:   "https://api.hetzner.cloud/v1/servers?per_page=1",
	CloudflareVerify: "https://api.cloudflare.com/client/v4/user/tokens/verify",
}

// VerifyCloud checks the credentials against the provider API and returns a short identity.
// Returned errors never contain credential values.
func VerifyCloud(ctx context.Context, provider string, fields map[string]string, hc *http.Client) (string, error) {
	if err := ValidateCloudFields(provider, fields); err != nil {
		return "", err
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	p, _ := CloudProviderByID(provider)
	v := cloudValues(p, fields)
	var (
		id  string
		err error
	)
	switch provider {
	case "aws":
		id, err = verifyAWS(ctx, hc, v)
	case "gcp":
		id, err = verifyGCP(ctx, hc, v)
	case "azure":
		id, err = verifyAzure(ctx, hc, v)
	case "digitalocean":
		id, err = verifyDO(ctx, hc, v)
	case "hetzner":
		id, err = verifyHetzner(ctx, hc, v)
	case "cloudflare":
		id, err = verifyCloudflare(ctx, hc, v)
	case "kubernetes":
		var host string
		host, err = kubeServerHost(v["kubeconfig"], v["context"])
		id = "server " + host
	}
	if err != nil {
		return "", scrubCloudErr(p, v, err)
	}
	return id, nil
}

// doCloud performs the request and returns status and (bounded) body.
func doCloud(hc *http.Client, req *http.Request) (int, []byte, error) {
	resp, err := hc.Do(req)
	if err != nil {
		// url.Error repeats the URL only; keep host, drop query.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

func statusErr(code int, detail string) error {
	msg := fmt.Sprintf("HTTP %d", code)
	if code == 401 || code == 403 {
		msg += " (credentials rejected)"
	}
	if detail != "" {
		msg += ": " + detail
	}
	return errors.New(msg)
}

func bearerGet(ctx context.Context, hc *http.Client, urlStr, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doCloud(hc, req)
}

func verifyDO(ctx context.Context, hc *http.Client, v map[string]string) (string, error) {
	code, body, err := bearerGet(ctx, hc, cloudEndpoints.DOAccount, v["token"])
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", statusErr(code, "")
	}
	var r struct {
		Account struct {
			Email string `json:"email"`
		} `json:"account"`
	}
	if json.Unmarshal(body, &r) != nil || r.Account.Email == "" {
		return "", errors.New("unexpected response")
	}
	return r.Account.Email, nil
}

func verifyHetzner(ctx context.Context, hc *http.Client, v map[string]string) (string, error) {
	code, _, err := bearerGet(ctx, hc, cloudEndpoints.HetznerServers, v["token"])
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", statusErr(code, "")
	}
	return "ok", nil
}

func verifyAzure(ctx context.Context, hc *http.Client, v map[string]string) (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {v["client_id"]},
		"client_secret": {v["client_secret"]},
		"scope":         {"https://management.azure.com/.default"},
	}
	u := fmt.Sprintf(cloudEndpoints.AzureLogin, url.PathEscape(v["tenant_id"]))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	code, body, err := doCloud(hc, req)
	if err != nil {
		return "", err
	}
	if code != 200 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return "", statusErr(code, e.Error)
	}
	return v["tenant_id"], nil
}

func verifyGCP(ctx context.Context, hc *http.Client, v map[string]string) (string, error) {
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.Unmarshal([]byte(v["credentials_json"]), &sa); err != nil {
		return "", errors.New("credentials_json is not valid JSON")
	}
	key, err := parseRSAKey(sa.PrivateKey)
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims, _ := json.Marshal(map[string]any{
		"iss": sa.ClientEmail, "scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud": cloudEndpoints.GCPToken, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	b64 := base64.RawURLEncoding.EncodeToString
	signing := b64([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + b64(claims)
	h := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", errors.New("cannot sign JWT with the service account key")
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signing + "." + b64(sig)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudEndpoints.GCPToken, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	code, body, err := doCloud(hc, req)
	if err != nil {
		return "", err
	}
	if code != 200 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return "", statusErr(code, e.Error)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return "", errors.New("token endpoint returned no access token")
	}
	code, _, err = bearerGet(ctx, hc, fmt.Sprintf(cloudEndpoints.GCPProject, url.PathEscape(v["project_id"])), tok.AccessToken)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", statusErr(code, "project "+v["project_id"])
	}
	return v["project_id"], nil
}

func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	blk, _ := pem.Decode([]byte(pemStr))
	if blk == nil {
		return nil, errors.New("private_key is not valid PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("private_key is not an RSA key")
	}
	if rk, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return rk, nil
	}
	return nil, errors.New("private_key cannot be parsed")
}

func verifyAWS(ctx context.Context, hc *http.Client, v map[string]string) (string, error) {
	if v["role_arn"] != "" {
		_, arn, err := assumeAWSRole(ctx, hc, v)
		if err != nil {
			return "", fmt.Errorf("assume role: %w", err)
		}
		acct := ""
		if parts := strings.Split(arn, ":"); len(parts) > 4 {
			acct = parts[4]
		}
		return acct + " (" + arn + ")", nil
	}
	region := v["region"]
	body := "Action=GetCallerIdentity&Version=2011-06-15"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(cloudEndpoints.STS, url.PathEscape(region)), strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	signV4(req, []byte(body), v["access_key_id"], v["secret_access_key"], v["session_token"], region, "sts", time.Now())
	code, resp, err := doCloud(hc, req)
	if err != nil {
		return "", err
	}
	if code != 200 {
		var e struct {
			Code string `xml:"Error>Code"`
		}
		_ = xml.Unmarshal(resp, &e)
		return "", statusErr(code, e.Code)
	}
	var r struct {
		Account string `xml:"GetCallerIdentityResult>Account"`
		Arn     string `xml:"GetCallerIdentityResult>Arn"`
	}
	if xml.Unmarshal(resp, &r) != nil || r.Account == "" {
		return "", errors.New("unexpected response")
	}
	return r.Account + " (" + r.Arn + ")", nil
}

// signV4 adds AWS Signature V4 headers. Signed headers: host, x-amz-date,
// plus content-type and x-amz-security-token when present.
func signV4(req *http.Request, body []byte, ak, sk, token, region, service string, t time.Time) {
	t = t.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := amzDate[:8]
	req.Header.Set("X-Amz-Date", amzDate)
	if token != "" {
		req.Header.Set("X-Amz-Security-Token", token)
	}
	hdrs := map[string]string{"host": req.URL.Host, "x-amz-date": amzDate}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		hdrs["content-type"] = ct
	}
	if token != "" {
		hdrs["x-amz-security-token"] = token
	}
	names := make([]string, 0, len(hdrs))
	for k := range hdrs {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHdrs strings.Builder
	for _, k := range names {
		canonHdrs.WriteString(k + ":" + strings.TrimSpace(hdrs[k]) + "\n")
	}
	signed := strings.Join(names, ";")
	uri := req.URL.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	query := strings.ReplaceAll(req.URL.Query().Encode(), "+", "%20")
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	canon := strings.Join([]string{req.Method, uri, query, canonHdrs.String(), signed, sum(body)}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sum([]byte(canon))
	mac := func(key []byte, s string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(s))
		return m.Sum(nil)
	}
	k := mac(mac(mac(mac([]byte("AWS4"+sk), day), region), service), "aws4_request")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+ak+"/"+scope+
		", SignedHeaders="+signed+", Signature="+hex.EncodeToString(mac(k, toSign)))
}

func verifyCloudflare(ctx context.Context, hc *http.Client, v map[string]string) (string, error) {
	code, body, err := bearerGet(ctx, hc, cloudEndpoints.CloudflareVerify, v["api_token"])
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", statusErr(code, "")
	}
	var r struct {
		Success bool `json:"success"`
		Result  struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &r) != nil || !r.Success {
		return "", errors.New("unexpected response")
	}
	if r.Result.Status != "active" {
		return "", fmt.Errorf("token status is %q", r.Result.Status)
	}
	return "token " + r.Result.ID + " (active)", nil
}
