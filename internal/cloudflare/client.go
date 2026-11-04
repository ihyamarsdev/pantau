package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

var idRegex = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidID reports whether s matches ^[0-9a-f]{32}$ (Cloudflare zone/record/account ID format).
func ValidID(s string) bool {
	return idRegex.MatchString(s)
}

type Client struct {
	BaseURL string       // default DefaultBaseURL; replaced in tests with httptest
	HTTP    *http.Client // Timeout 20s
	token   string
}

func New(token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
		},
		token: token,
	}
}

// APIError is returned on non-2xx or success=false Cloudflare responses.
// Error() formats Cloudflare error items as "code: message; ..." or "cloudflare: HTTP <status>".
// The API token is never included in the error string.
type APIError struct {
	Status int
	Errors []APIErrorItem
}

type APIErrorItem struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if len(e.Errors) > 0 {
		var parts []string
		for _, it := range e.Errors {
			if it.Code != 0 || it.Message != "" {
				parts = append(parts, fmt.Sprintf("%d: %s", it.Code, it.Message))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	if e.Status > 0 {
		return fmt.Sprintf("cloudflare: HTTP %d", e.Status)
	}
	return "cloudflare: api error"
}

type TokenInfo struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ExpiresOn string `json:"expires_on,omitempty"`
}

type apiEnvelope struct {
	Success    bool            `json:"success"`
	Errors     []APIErrorItem  `json:"errors"`
	Messages   []APIErrorItem  `json:"messages"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *resultInfo     `json:"result_info"`
}

type resultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

const maxResponseBody = 4 * 1024 * 1024 // 4 MiB limit

func (c *Client) do(ctx context.Context, method, relPath string, query url.Values, body interface{}, out interface{}) (*resultInfo, error) {
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	rel := "/" + strings.TrimLeft(relPath, "/")
	reqURL := baseURL + rel
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	lr := io.LimitReader(resp.Body, maxResponseBody)
	raw, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}

	var env apiEnvelope
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
		// Non-JSON response (e.g. HTML 502/503/504)
		return nil, &APIError{
			Status: resp.StatusCode,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !env.Success {
		return nil, &APIError{
			Status: resp.StatusCode,
			Errors: env.Errors,
		}
	}

	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return nil, err
		}
	}

	return env.ResultInfo, nil
}

// VerifyToken verifies the client's API token.
// accountID != "" -> GET /accounts/{id}/tokens/verify; empty -> GET /user/tokens/verify
func (c *Client) VerifyToken(ctx context.Context, accountID string) (*TokenInfo, error) {
	var path string
	if accountID != "" {
		if !ValidID(accountID) {
			return nil, errors.New("invalid account id")
		}
		path = "/accounts/" + accountID + "/tokens/verify"
	} else {
		path = "/user/tokens/verify"
	}

	var info TokenInfo
	if _, err := c.do(ctx, http.MethodGet, path, nil, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
