package dockerhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		ref, ns, name, tag string
		ok                 bool
	}{
		{"nginx", "library", "nginx", "latest", true},
		{"nginx:1.27-alpine", "library", "nginx", "1.27-alpine", true},
		{"bitnami/redis:7.2", "bitnami", "redis", "7.2", true},
		{"docker.io/library/nginx:1.28", "library", "nginx", "1.28", true},
		{"docker.io/nginx", "library", "nginx", "latest", true},
		{"index.docker.io/grafana/grafana:11.0.0", "grafana", "grafana", "11.0.0", true},
		{"registry-1.docker.io/bitnami/redis", "bitnami", "redis", "latest", true},
		{"ghcr.io/herliansyah/pantau:latest", "", "", "", false},
		{"scm.example.net:5050/team/app:v1", "", "", "", false},
		{"localhost:5000/app:1", "", "", "", false},
		{"localhost/app", "", "", "", false},
		{"a/b/c:1", "", "", "", false},           // Docker Hub repositories have exactly one namespace
		{"nginx@sha256:abcd", "", "", "", false}, // pinned by digest: nothing to compare against a tag
		{"MyApp:1", "", "", "", false},           // uppercase is not a valid repository name
		{"nginx:bad/tag", "", "", "", false},
	}
	for _, c := range cases {
		ns, name, tag, ok := ParseRef(c.ref)
		if ok != c.ok || ns != c.ns || name != c.name || tag != c.tag {
			t.Errorf("ParseRef(%q) = %q %q %q %v, want %q %q %q %v", c.ref, ns, name, tag, ok, c.ns, c.name, c.tag, c.ok)
		}
	}
}

// fakeHub serves tag lookups from a table ("ns/name:tag" → JSON body); anything else is 404. status, when set,
// is answered to every request instead.
func fakeHub(t *testing.T, tags map[string]string, status int) (*Client, *int64) {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/v2/namespaces/")
		ns, rest, _ := strings.Cut(p, "/repositories/")
		name, tag, _ := strings.Cut(rest, "/tags/")
		body, ok := tags[ns+"/"+name+":"+tag]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, srv.URL), &hits
}

func TestCheckUpdates(t *testing.T) {
	c, hits := fakeHub(t, map[string]string{
		"library/nginx:1.28":  `{"name":"1.28","digest":"sha256:same","tag_last_pushed":"2026-04-07T13:50:27Z"}`,
		"library/golang:1.26": `{"name":"1.26","digest":"sha256:new","tag_last_pushed":"2026-11-06T01:22:37Z"}`,
		"bitnami/redis:7":     `{"name":"7","digest":"sha256:index","images":[{"os":"linux","architecture":"amd64","digest":"sha256:amd64"}]}`,
	}, 0)
	updates := c.CheckUpdates(context.Background(), []Local{
		// Two names for the same Docker Hub tag share one lookup.
		{ID: "sha256:same", Tags: []string{"nginx:1.28", "docker.io/library/nginx:1.28"}, Digests: []string{"nginx@sha256:same"}},
		{ID: "sha256:old", Tags: []string{"golang:1.26"}, Digests: []string{"golang@sha256:old"}},
		// Pulled by platform: the local digest is the amd64 manifest of the current index.
		{ID: "sha256:x", Tags: []string{"bitnami/redis:7"}, Digests: []string{"docker.io/bitnami/redis@sha256:amd64"}},
		{ID: "sha256:y", Tags: []string{"library/gone:1"}, Digests: []string{"gone@sha256:y"}},
		// Built locally: no registry digest, so nothing is asked of Docker Hub.
		{ID: "sha256:z", Tags: []string{"myapp:dev"}},
		// A digest recorded for another repository does not count for this one.
		{ID: "sha256:w", Tags: []string{"golang:1.26"}, Digests: []string{"mirror.local/golang@sha256:new"}},
		{ID: "sha256:v", Tags: []string{"ghcr.io/owner/app:1"}, Digests: []string{"ghcr.io/owner/app@sha256:v"}},
	})
	want := []struct{ ref, status string }{
		{"nginx:1.28", "up_to_date"},
		{"docker.io/library/nginx:1.28", "up_to_date"},
		{"golang:1.26", "update_available"},
		{"bitnami/redis:7", "up_to_date"},
		{"library/gone:1", "not_found"},
		{"myapp:dev", "unknown"},
		{"golang:1.26", "unknown"},
	}
	if len(updates) != len(want) {
		t.Fatalf("got %d updates, want %d (other registries left out): %+v", len(updates), len(want), updates)
	}
	for i, w := range want {
		if updates[i].Ref != w.ref || updates[i].Status != w.status {
			t.Errorf("update %d = %s %s (%s), want %s %s", i, updates[i].Ref, updates[i].Status, updates[i].Detail, w.ref, w.status)
		}
	}
	if updates[2].RemoteDigest != "sha256:new" || updates[2].Repo != "library/golang" {
		t.Errorf("update_available must carry the Docker Hub digest and repo: %+v", updates[2])
	}
	if n := atomic.LoadInt64(hits); n != 4 {
		t.Errorf("Docker Hub lookups = %d, want 4 (deduplicated, none for images without a registry digest)", n)
	}
}

// When Docker Hub refuses (rate limit) the check stops instead of spending the whole quota, and every image
// reports the reason.
func TestCheckUpdatesStopsAtFirstFailure(t *testing.T) {
	c, hits := fakeHub(t, nil, http.StatusTooManyRequests)
	var images []Local
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		images = append(images, Local{ID: "sha256:" + n, Tags: []string{n + ":1"}, Digests: []string{n + "@sha256:" + n}})
	}
	updates := c.CheckUpdates(context.Background(), images)
	for _, u := range updates {
		if u.Status != "error" || !strings.Contains(u.Detail, "rate limit") {
			t.Errorf("%s: %s %q, want error with the rate limit reason", u.Ref, u.Status, u.Detail)
		}
	}
	if n := atomic.LoadInt64(hits); n > checkWorkers {
		t.Errorf("Docker Hub was asked %d times after refusing, want at most the %d requests already in flight", n, checkWorkers)
	}
}

func TestTagsPastLastPageIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer srv.Close()
	c := New(srv.URL, srv.URL)
	if p, err := c.Tags(context.Background(), "library", "nginx", "", 3); err != nil || len(p.Tags) != 0 || p.HasNext {
		t.Errorf("page past the end = %+v, %v; want an empty last page", p, err)
	}
	if _, err := c.Tags(context.Background(), "library", "nope", "", 1); !IsNotFound(err) {
		t.Errorf("first page of a missing repository must be not found, got %v", err)
	}
}
