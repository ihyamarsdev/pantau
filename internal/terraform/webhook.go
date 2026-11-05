package terraform

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ParseWebhook parses webhook payloads from GitHub, GitLab, or generic webhooks,
// verifies HMAC or token if secret is provided, and extracts the git branch name.
func ParseWebhook(h http.Header, body []byte, secret string) (branch string, err error) {
	if len(body) > 1024*1024 {
		return "", errors.New("webhook body exceeds 1 MiB")
	}

	// Secret verification if provided
	if secret != "" {
		if ghSig := h.Get("X-Hub-Signature-256"); ghSig != "" {
			// GitHub: "sha256=<hex>"
			sig := strings.TrimPrefix(ghSig, "sha256=")
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			expectedSig := hex.EncodeToString(mac.Sum(nil))
			if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
				return "", errors.New("invalid github signature")
			}
		} else if glToken := h.Get("X-Gitlab-Token"); glToken != "" {
			// GitLab: token header
			if subtle.ConstantTimeCompare([]byte(glToken), []byte(secret)) != 1 {
				return "", errors.New("invalid gitlab token")
			}
		} else {
			return "", errors.New("missing webhook authentication header")
		}
	}

	// Extract branch from json body
	var payload struct {
		Ref string `json:"ref"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to parse webhook json: %w", err)
	}

	if payload.Ref == "" {
		return "", errors.New("payload missing 'ref' field")
	}

	// Handle "refs/heads/main" or "refs/tags/v1.0" or plain "main"
	ref := payload.Ref
	if strings.HasPrefix(ref, "refs/heads/") {
		branch = strings.TrimPrefix(ref, "refs/heads/")
	} else if strings.HasPrefix(ref, "refs/tags/") {
		branch = strings.TrimPrefix(ref, "refs/tags/")
	} else {
		branch = ref
	}

	return branch, nil
}
