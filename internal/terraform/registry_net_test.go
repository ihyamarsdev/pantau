package terraform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryCacheHitMissTTL(t *testing.T) {
	c, hits := regServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"modules":[]}`)) })
	now := time.Now()
	regNow = func() time.Time { return now }
	t.Cleanup(func() { regNow = time.Now })
	ctx := context.Background()
	search := func(q string) {
		if _, err := c.SearchModules(ctx, q, "", 0, 5); err != nil {
			t.Fatal(err)
		}
	}
	search("a")
	search("a")
	if *hits != 1 {
		t.Fatalf("hit expected, got %d fetches", *hits)
	}
	search("b")
	if *hits != 2 {
		t.Fatalf("different URL must miss, got %d", *hits)
	}
	now = now.Add(regCacheTTL + time.Second)
	search("a")
	if *hits != 3 {
		t.Fatalf("expired entry must refetch, got %d", *hits)
	}
}

func TestRegistryCacheBounded(t *testing.T) {
	resetRegCache()
	for i := 0; i < regCacheMax+50; i++ {
		regCachePut(fmt.Sprint("k", i), []byte("x"))
	}
	if n := len(regCache.m); n > regCacheMax {
		t.Fatalf("cache grew to %d", n)
	}
}

func TestRegistryErrorsNotCached(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	c, hits := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "secret internal detail", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"modules":[]}`))
	})
	_, err := c.SearchModules(context.Background(), "x", "", 0, 5)
	var se *RegistryStatusError
	if !errors.As(err, &se) || se.Status != 500 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
	fail.Store(false)
	if _, err := c.SearchModules(context.Background(), "x", "", 0, 5); err != nil || *hits != 2 {
		t.Fatalf("retry: err=%v hits=%d", err, *hits)
	}
}

func TestRegistryNotFoundAndBadJSON(t *testing.T) {
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			http.Error(w, `{"errors":["module not found"]}`, 404)
			return
		}
		_, _ = w.Write([]byte(`<html>nope`))
	})
	_, err := c.ModuleDetail(context.Background(), "x", "missing", "z", "")
	var se *RegistryStatusError
	if !errors.As(err, &se) || se.Status != 404 {
		t.Fatalf("404: %v", err)
	}
	if _, err = c.ProviderDetail(context.Background(), "x", "y"); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("bad json: %v", err)
	}
}

func TestRegistryBodyCap(t *testing.T) {
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"modules":[],"pad":"` + strings.Repeat("a", regBodyCap) + `"}`))
	})
	if _, err := c.SearchModules(context.Background(), "x", "", 0, 5); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestRegistryRedirects(t *testing.T) {
	var otherHits int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&otherHits, 1)
		_, _ = w.Write([]byte(`{"modules":[]}`))
	}))
	defer other.Close()
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/modules/search":
			http.Redirect(w, r, other.URL+"/v1/modules/search", http.StatusFound)
		case "/v2/providers":
			http.Redirect(w, r, "/v2/elsewhere", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	})
	if _, err := c.SearchModules(context.Background(), "x", "", 0, 5); err == nil || atomic.LoadInt32(&otherHits) != 0 {
		t.Fatalf("cross-host redirect followed: err=%v hits=%d", err, otherHits)
	}
	if _, err := c.SearchProviders(context.Background(), "x", 1); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
}

func TestRegistryUnreachableErrorIsShort(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	resetRegCache()
	_, err := NewRegistryClient(url).SearchModules(context.Background(), "x", "", 0, 5)
	if err == nil || strings.Contains(err.Error(), url) || len(err.Error()) > 160 {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRegistryURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                           DefaultRegistryURL,
		"  ":                         DefaultRegistryURL,
		"https://reg.example.com/":   "https://reg.example.com",
		"https://reg.example.com/a/": "https://reg.example.com/a",
		"http://127.0.0.1:8080":      "http://127.0.0.1:8080",
		"http://localhost:9/":        "http://localhost:9",
		"http://[::1]:9":             "http://[::1]:9",
	} {
		if got, err := ValidateRegistryURL(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, in := range []string{"http://reg.example.com", "ftp://x.example.com", "https://u:p@reg.example.com", "https://u@reg.example.com",
		"https://reg.example.com?x=1", "https://reg.example.com/#f", "https://reg.example.com#", "reg.example.com", "https://", "javascript:alert(1)"} {
		if got, err := ValidateRegistryURL(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}
