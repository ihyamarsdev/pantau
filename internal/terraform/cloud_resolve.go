package terraform

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type awsCreds struct{ AccessKeyID, SecretAccessKey, SessionToken string }

// ResolveCloudEnv is CloudEnv, except that AWS accounts with role_arn call sts:AssumeRole
// and return the temporary credentials instead of the base keys. Errors never contain secrets.
func ResolveCloudEnv(ctx context.Context, provider string, fields map[string]string, hc *http.Client) (map[string]string, error) {
	env := CloudEnv(provider, fields)
	p, _ := CloudProviderByID(provider)
	v := cloudValues(p, fields)
	if provider != "aws" || v["role_arn"] == "" {
		return env, nil
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	creds, _, err := assumeAWSRole(ctx, hc, v)
	if err != nil {
		return nil, scrubCloudErr(p, v, fmt.Errorf("assume role: %w", err))
	}
	env["AWS_ACCESS_KEY_ID"] = creds.AccessKeyID
	env["AWS_SECRET_ACCESS_KEY"] = creds.SecretAccessKey
	env["AWS_SESSION_TOKEN"] = creds.SessionToken
	return env, nil
}

// assumeAWSRole signs sts:AssumeRole with the base keys; returns the temporary credentials and the assumed role ARN.
func assumeAWSRole(ctx context.Context, hc *http.Client, v map[string]string) (awsCreds, string, error) {
	region := v["region"]
	form := url.Values{
		"Action":          {"AssumeRole"},
		"Version":         {"2011-06-15"},
		"RoleArn":         {v["role_arn"]},
		"RoleSessionName": {v["session_name"]},
		"DurationSeconds": {"3600"},
	}
	if v["external_id"] != "" {
		form.Set("ExternalId", v["external_id"])
	}
	body := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(cloudEndpoints.STS, url.PathEscape(region)), strings.NewReader(body))
	if err != nil {
		return awsCreds{}, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	signV4(req, []byte(body), v["access_key_id"], v["secret_access_key"], v["session_token"], region, "sts", time.Now())
	code, resp, err := doCloud(hc, req)
	if err != nil {
		return awsCreds{}, "", err
	}
	if code != 200 {
		var e struct {
			Code string `xml:"Error>Code"`
		}
		_ = xml.Unmarshal(resp, &e)
		return awsCreds{}, "", statusErr(code, e.Code)
	}
	var r struct {
		AccessKeyID     string `xml:"AssumeRoleResult>Credentials>AccessKeyId"`
		SecretAccessKey string `xml:"AssumeRoleResult>Credentials>SecretAccessKey"`
		SessionToken    string `xml:"AssumeRoleResult>Credentials>SessionToken"`
		Arn             string `xml:"AssumeRoleResult>AssumedRoleUser>Arn"`
	}
	if xml.Unmarshal(resp, &r) != nil || r.AccessKeyID == "" || r.SecretAccessKey == "" || r.SessionToken == "" {
		return awsCreds{}, "", errors.New("unexpected response")
	}
	return awsCreds{r.AccessKeyID, r.SecretAccessKey, r.SessionToken}, r.Arn, nil
}

// scrubCloudErr masks the secret field values of the account in err.
func scrubCloudErr(p CloudProvider, v map[string]string, err error) error {
	msg := err.Error()
	for _, f := range p.Fields {
		if sv := v[f.Key]; f.Secret && sv != "" {
			msg = strings.ReplaceAll(msg, sv, "***")
		}
	}
	return errors.New(msg)
}
