package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func codeOf(h http.HandlerFunc, r *http.Request) int {
	w := httptest.NewRecorder()
	h(w, r)
	return w.Code
}

func TestTunnelAuth_RejectsSessionAndOtherTunnelCookies(t *testing.T) {
	t.Parallel()
	s := &Server{}
	h := s.tunnelAuth(okHandler)

	// The admin session cookie must not open a tunnel.
	r := httptest.NewRequest("GET", "/api/tunnel/7/2375/containers", nil)
	r.AddCookie(&http.Cookie{Name: "pantau_session", Value: "anything"})
	if got := codeOf(h, r); got != http.StatusUnauthorized {
		t.Fatalf("session cookie accepted on tunnel origin: %d", got)
	}

	// A capability for port 8080 must not open port 2375.
	r = httptest.NewRequest("GET", "/api/tunnel/7/2375/containers", nil)
	r.AddCookie(&http.Cookie{Name: tunnelCookieName, Value: tunnelToken(7, 8080)})
	if got := codeOf(h, r); got != http.StatusUnauthorized {
		t.Fatalf("capability for another port accepted: %d", got)
	}

	// The matching capability opens the tunnel.
	r = httptest.NewRequest("GET", "/api/tunnel/7/2375/containers", nil)
	r.AddCookie(&http.Cookie{Name: tunnelCookieName, Value: tunnelToken(7, 2375)})
	if got := codeOf(h, r); got != http.StatusOK {
		t.Fatalf("valid capability rejected: %d", got)
	}
}

func TestTunnelRedirect_RefusesCrossSiteAndIssuesScopedCookie(t *testing.T) {
	t.Parallel()
	s := &Server{tunnelPort: 9999}

	r := httptest.NewRequest("GET", "/api/tunnel/7/2375/", nil)
	r.Header.Set("Sec-Fetch-Site", "same-site")
	if got := codeOf(s.handleTunnelRedirect, r); got != http.StatusForbidden {
		t.Fatalf("same-site fetch allowed to mint a tunnel cookie: %d", got)
	}

	r = httptest.NewRequest("GET", "/api/tunnel/7/2375/", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	s.handleTunnelRedirect(w, r)
	var got *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == tunnelCookieName {
			got = c
		}
	}
	if got == nil || got.Value != tunnelToken(7, 2375) || got.Path != "/api/tunnel/7/2375" {
		t.Fatalf("unexpected tunnel cookie: %+v", got)
	}
}
