package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

func TestHostsETagAndGzip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test_hosts_etag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := NewServer(db, inspector.New(db, nil, nil), nil)
	token := "hosts_etag_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	get := func(acceptEncoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	// 1. First GET: 200 with ETag
	rec1 := get("")
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}
	identity := rec1.Body.Bytes()

	// 2. Second GET with If-None-Match: 304, empty body
	req2 := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	req2.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Fatalf("expected empty 304 body, got %d bytes", rec2.Body.Len())
	}

	// 3. Gzip client: Content-Encoding gzip, same ETag, gunzipped equals identity
	rec3 := get("gzip")
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec3.Code)
	}
	if rec3.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding gzip, got %q", rec3.Header().Get("Content-Encoding"))
	}
	if got := rec3.Header().Get("ETag"); got != etag {
		t.Fatalf("gzip ETag %q != identity ETag %q", got, etag)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec3.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	unzipped, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unzipped, identity) {
		t.Fatalf("gunzipped body differs from identity body")
	}

	// 4. After adding a host the ETag differs
	if _, err := db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}
	rec4 := get("")
	if rec4.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec4.Code)
	}
	if got := rec4.Header().Get("ETag"); got == etag {
		t.Fatal("expected ETag to change after adding a host")
	}
}
