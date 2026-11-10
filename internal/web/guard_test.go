package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuard(t *testing.T) {
	handler := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name         string
		method       string
		path         string
		host         string
		secFetchSite string
		origin       string
		expectedCode int
	}{
		{"hostname host blocked", http.MethodGet, "/", "evil.example", "", "", http.StatusForbidden},
		{"localhost with port", http.MethodGet, "/", "localhost:8080", "", "", http.StatusOK},
		{"ipv4 loopback", http.MethodGet, "/", "127.0.0.1:8080", "", "", http.StatusOK},
		{"ipv6 loopback", http.MethodGet, "/", "[::1]:8080", "", "", http.StatusOK},
		{"ipv4 lan address", http.MethodGet, "/", "192.168.1.5:8080", "", "", http.StatusOK},
		{"uppercase localhost", http.MethodGet, "/", "LOCALHOST", "", "", http.StatusOK},
		{"same-site post blocked", http.MethodPost, "/", "localhost:8080", "same-site", "", http.StatusForbidden},
		{"foreign origin post blocked", http.MethodPost, "/", "localhost:8080", "", "http://localhost:3000", http.StatusForbidden},
		{"same-origin post allowed", http.MethodPost, "/", "localhost:8080", "same-origin", "", http.StatusOK},
		{"curl post allowed", http.MethodPost, "/", "localhost:8080", "", "", http.StatusOK},
		{"webhook bypasses guard", http.MethodPost, "/api/terraform/webhook/x", "evil.example", "cross-site", "", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = tc.host
			if tc.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetchSite)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.expectedCode {
				t.Errorf("expected code %d, got %d (body: %s)", tc.expectedCode, rec.Code, rec.Body.String())
			}
		})
	}
}
