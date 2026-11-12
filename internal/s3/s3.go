// Package s3 is a minimal S3-compatible client (AWS S3, MinIO, Cloudflare R2, Ceph, ...) using only the standard
// library: SigV4 signing, single PUT for small objects and multipart upload for large ones.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	minPartSize = 16 << 20 // 16 MiB; S3 minimum is 5 MiB for every part but the last
	maxParts    = 9000     // S3 allows 10000
	emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

var bucketRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,254}$`)

type Config struct {
	Endpoint  string // empty = AWS (https://s3.<region>.amazonaws.com)
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

// NewConfig normalizes the values of a stored account: trims spaces, defaults the region to us-east-1 and, when
// pathStyle is neither "1" nor "0", uses path-style addressing exactly when a custom endpoint is set.
func NewConfig(endpoint, region, bucket, accessKey, secretKey, pathStyle string) Config {
	c := Config{
		Endpoint:  strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		Region:    strings.TrimSpace(region),
		Bucket:    strings.TrimSpace(bucket),
		AccessKey: strings.TrimSpace(accessKey),
		SecretKey: strings.TrimSpace(secretKey),
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	switch pathStyle {
	case "1":
		c.PathStyle = true
	case "0":
	default:
		c.PathStyle = c.Endpoint != ""
	}
	return c
}

// Validate reports a missing or malformed setting.
func (c Config) Validate() error {
	if c.Bucket == "" || c.AccessKey == "" || c.SecretKey == "" {
		return errors.New("S3 is not configured: set bucket, access key and secret key of the S3 account")
	}
	if !bucketRe.MatchString(c.Bucket) {
		return fmt.Errorf("invalid S3 bucket name %q", c.Bucket)
	}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid S3 endpoint %q: must be http:// or https://", c.Endpoint)
		}
	}
	return nil
}

// NormalizePrefix returns "" or a key prefix without leading slash and with one trailing slash; ".." is resolved away.
func NormalizePrefix(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// Error is a non-2xx S3 response.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("S3 returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("S3 %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 2 * time.Minute // counted after the request body is fully written
	return &Client{cfg: cfg, http: &http.Client{Transport: tr}}
}

// Check proves the endpoint, bucket and credentials work (ListObjectsV2 with max-keys=1).
func (c *Client) Check(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "", url.Values{"list-type": {"2"}, "max-keys": {"1"}}, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// Upload stores r as key; size is the expected length and only sizes the multipart parts.
// shortcut: no retry of failed parts, retry the whole job when the network drops.
func (c *Client) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	partSize := int64(minPartSize)
	if need := (size + maxParts - 1) / maxParts; need > partSize {
		partSize = need
	}
	first, err := io.ReadAll(io.LimitReader(r, partSize))
	if err != nil {
		return err
	}
	if int64(len(first)) < partSize {
		if err := c.drain(c.do(ctx, http.MethodPut, key, nil, first)); err != nil {
			return err
		}
		return nil
	}

	resp, err := c.do(ctx, http.MethodPost, key, url.Values{"uploads": {""}}, nil)
	if err != nil {
		return err
	}
	var init struct {
		UploadID string `xml:"UploadId"`
	}
	err = xml.NewDecoder(resp.Body).Decode(&init)
	resp.Body.Close()
	if err != nil || init.UploadID == "" {
		return errors.New("S3 did not return a multipart upload id")
	}
	abort := func() { // best effort, with a fresh context because ctx may be the reason we are here
		actx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.drain(c.do(actx, http.MethodDelete, key, url.Values{"uploadId": {init.UploadID}}, nil))
	}

	type part struct {
		N    int
		ETag string
	}
	var parts []part
	buf, spare := first, make([]byte, partSize)
	for n := 1; len(buf) > 0; n++ {
		resp, err := c.do(ctx, http.MethodPut, key, url.Values{"partNumber": {fmt.Sprint(n)}, "uploadId": {init.UploadID}}, buf)
		if err != nil {
			abort()
			return err
		}
		parts = append(parts, part{n, resp.Header.Get("ETag")})
		resp.Body.Close()

		got, err := io.ReadFull(r, spare)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			abort()
			return err
		}
		buf = spare[:got]
	}

	var body bytes.Buffer
	body.WriteString("<CompleteMultipartUpload>")
	for _, p := range parts {
		var esc bytes.Buffer
		_ = xml.EscapeText(&esc, []byte(p.ETag))
		fmt.Fprintf(&body, "<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>", p.N, esc.String())
	}
	body.WriteString("</CompleteMultipartUpload>")
	resp, err = c.do(ctx, http.MethodPost, key, url.Values{"uploadId": {init.UploadID}}, body.Bytes())
	if err != nil {
		abort()
		return err
	}
	defer resp.Body.Close()
	// S3 may answer 200 and put the failure in the body.
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if bytes.Contains(out, []byte("<Error>")) {
		abort()
		return parseError(resp.StatusCode, out)
	}
	return nil
}

func (c *Client) drain(resp *http.Response, err error) error {
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// uriEncode is the SigV4 URI encoding: everything but A-Za-z0-9-_.~ is %XX; "/" is kept only when keepSlash.
func uriEncode(s string, keepSlash bool) string {
	var b strings.Builder
	for i := range len(s) {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == '/' && keepSlash:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, false)+"="+uriEncode(v, false))
		}
	}
	return strings.Join(parts, "&")
}

func (c *Client) objectURL(key string, q url.Values) (string, error) {
	base := c.cfg.Endpoint
	if base == "" {
		base = "https://s3." + c.cfg.Region + ".amazonaws.com"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	host, p := u.Host, basePath+"/"+uriEncode(key, true)
	if c.cfg.PathStyle {
		p = basePath + "/" + uriEncode(c.cfg.Bucket, false) + "/" + uriEncode(key, true)
	} else {
		host = c.cfg.Bucket + "." + u.Host
	}
	s := u.Scheme + "://" + host + p
	if len(q) > 0 {
		s += "?" + canonicalQuery(q)
	}
	return s, nil
}

func (c *Client) do(ctx context.Context, method, key string, q url.Values, body []byte) (*http.Response, error) {
	target, err := c.objectURL(key, q)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	sum := emptySHA256
	if len(body) > 0 {
		h := sha256.Sum256(body)
		sum = hex.EncodeToString(h[:])
	}
	c.sign(req, sum, time.Now())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, parseError(resp.StatusCode, raw)
	}
	return resp, nil
}

func parseError(status int, raw []byte) error {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(raw, &e)
	return &Error{Status: status, Code: e.Code, Message: e.Message}
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// sign adds x-amz-date, x-amz-content-sha256 and Authorization (SigV4). Every header present on req is signed.
func (c *Client) sign(req *http.Request, payloadHash string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := amzDate[:8]
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	vals := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		if lk := strings.ToLower(k); lk != "authorization" {
			vals[lk] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + vals[k] + "\n")
	}
	signed := strings.Join(names, ";")

	canonQuery := ""
	if req.URL.RawQuery != "" {
		q, _ := url.ParseQuery(req.URL.RawQuery)
		canonQuery = canonicalQuery(q)
	}
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), canonQuery, canonHeaders.String(), signed, payloadHash}, "\n")

	scope := day + "/" + c.cfg.Region + "/s3/aws4_request"
	ch := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(ch[:])

	k := hmacSHA256([]byte("AWS4"+c.cfg.SecretKey), day)
	k = hmacSHA256(k, c.cfg.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))

	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", c.cfg.AccessKey, scope, signed, sig))
}
