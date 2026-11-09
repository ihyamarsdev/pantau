package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/dockerhub"
)

// The browser endpoints map Docker Hub answers to HTTP statuses the UI can explain: bad input never leaves
// Pantau (400), a missing repository is 404, a rate limit stays 429, anything else upstream is 502.
func TestDockerHubEndpoints(t *testing.T) {
	var upstream []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream = append(upstream, r.URL.Path)
		switch {
		case r.URL.Path == "/v1/search":
			_, _ = w.Write([]byte(`{"num_pages":3,"num_results":60,"page":1,"results":[{"name":"nginx","description":"Official build of Nginx.","star_count":21419,"pull_count":13567421202,"is_official":true},{"name":"bitnami/nginx","star_count":200,"pull_count":9}]}`))
		case r.URL.Path == "/v2/namespaces/library/repositories/nginx":
			_, _ = w.Write([]byte(`{"description":"Official build of Nginx.","full_description":"# Quick reference","star_count":1,"pull_count":2,"last_updated":"2026-11-08T16:52:12Z"}`))
		case strings.HasSuffix(r.URL.Path, "/busy/tags"):
			w.WriteHeader(http.StatusTooManyRequests)
		case strings.HasSuffix(r.URL.Path, "/broken/tags"):
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	srv, auth := setupCFTestServer(t)
	srv.hub = dockerhub.New(fake.URL, fake.URL)
	get := func(path string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		auth(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}

	code, m := get("/api/dockerhub/search?q=nginx")
	res, _ := m["results"].([]any)
	if code != 200 || len(res) != 2 || m["num_pages"] != float64(3) {
		t.Fatalf("search = %d %v", code, m)
	}
	if first := res[0].(map[string]any); first["name"] != "nginx" || first["namespace"] != "library" || first["official"] != true {
		t.Errorf("official image must keep its pull name and library namespace: %v", first)
	}

	code, m = get("/api/dockerhub/repo/library/nginx")
	if code != 200 || m["readme"] != "# Quick reference" || m["url"] != "https://hub.docker.com/_/nginx" || m["official"] != true {
		t.Errorf("repo = %d %v", code, m)
	}

	before := len(upstream)
	for path, want := range map[string]int{
		"/api/dockerhub/search?q=":                     400,
		"/api/dockerhub/tags/library/Bad..Name":        400,
		"/api/dockerhub/tags/library/nginx?name=a%20b": 400,
		"/api/dockerhub/repo/library/missing":          404,
		"/api/dockerhub/tags/library/busy":             429,
		"/api/dockerhub/tags/library/broken":           502,
		"/api/dockerhub/nothing":                       404,
	} {
		if code, m := get(path); code != want || (code != 404 && m["error"] == nil) {
			t.Errorf("%s = %d %v, want %d with an error message", path, code, m, want)
		}
	}
	if got := len(upstream) - before; got != 3 {
		t.Errorf("Docker Hub was asked %d times, want 3 (rejected input and unknown routes never leave Pantau)", got)
	}
}
