package ansible

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGalaxyAPI serves the catalogue endpoints the client reads; any other path answers 404 as Galaxy does.
func fakeGalaxyAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v3/plugin/ansible/content/published/collections/index/community/general/":
			io.WriteString(w, `{"highest_version":{"version":"14.0.1"}}`)
		case "/api/v3/plugin/ansible/content/published/collections/index/community/broken/":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/api/v1/roles/":
			if q.Get("namespace") == "geerlingguy" && q.Get("name") == "nginx" {
				io.WriteString(w, `{"count":1,"results":[{"id":10962,"username":"geerlingguy","name":"nginx"}]}`)
				return
			}
			io.WriteString(w, `{"count":0,"results":[]}`)
		case "/api/v1/roles/10962/versions/":
			io.WriteString(w, `{"next":null,"results":[{"name":"3.0.0"},{"name":"3.3.1"}]}`)
		case "/api/v1/roles/7/versions/":
			io.WriteString(w, `{"next":null,"results":[]}`)
		case "/api/v3/plugin/ansible/search/collection-versions/":
			if q.Get("keywords") == "broken" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			io.WriteString(w, `{"meta":{"count":171},"data":[{"collection_version":{"namespace":"community","name":"general","version":"14.0.1","description":"General plugins"}}]}`)
		case "/api/v1/search/roles/":
			io.WriteString(w, `{"count":832,"results":[{"id":10962,"name":"nginx","username":"geerlingguy","description":"Nginx role"},{"id":7,"name":"nginx","username":"acme","description":""}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewestGalaxyVersion(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  string
	}{
		{[]string{"1.0.0", "1.10.0", "1.9.9"}, "1.10.0"},
		{[]string{"2.0.0-rc1", "1.9.0"}, "1.9.0"}, // pre-releases are not candidates
		{[]string{"2.0.0-rc1"}, ""},
		{[]string{"v1.0", "1.0.1"}, "1.0.1"},
		{[]string{"main", "abc1234", "1.0.0"}, "1.0.0"}, // branches and commits are not versions
		{[]string{"main"}, ""},
		{nil, ""},
	} {
		if got := newestGalaxyVersion(tc.names); got != tc.want {
			t.Errorf("newestGalaxyVersion(%q) = %q, want %q", tc.names, got, tc.want)
		}
	}
}

func TestUpdateStatus(t *testing.T) {
	for _, tc := range []struct {
		installed, latest, want string
	}{
		{"9.1.0", "14.0.1", statusUpdate},
		{"3.3.1", "3.3.1", statusCurrent},
		{"15.0.0", "14.0.1", statusCurrent}, // installed ahead of Galaxy is not an update
		{"main", "3.3.1", statusUnknown},    // a role installed from git records its branch
		{"", "3.3.1", statusUnknown},        // no version recorded
		{"3.3.1", "", statusUnknown},        // Galaxy listed nothing comparable
	} {
		if got := updateStatus(tc.installed, tc.latest); got != tc.want {
			t.Errorf("updateStatus(%q, %q) = %q, want %q", tc.installed, tc.latest, got, tc.want)
		}
	}
}

func TestCheckUpdatesReportsEachItem(t *testing.T) {
	c := NewGalaxyClient(fakeGalaxyAPI(t).URL)
	lib := Library{
		Collections: []LibraryItem{{"community.general", "9.1.0"}, {"community.gone", "1.0.0"}, {"community.broken", "1.0.0"}},
		Roles:       []LibraryItem{{"geerlingguy.nginx", "3.0.0"}, {"geerlingguy.gone", "1.0.0"}, {"localrole", ""}},
	}
	items := c.CheckUpdates(context.Background(), lib)
	want := []struct{ name, status, latest string }{
		{"community.general", statusUpdate, "14.0.1"},
		{"community.gone", statusNotOnGalaxy, ""},
		{"community.broken", statusError, ""},
		{"geerlingguy.nginx", statusUpdate, "3.3.1"},
		{"geerlingguy.gone", statusNotOnGalaxy, ""},
		{"localrole", statusNotOnGalaxy, ""},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(items), len(want), items)
	}
	for i, w := range want {
		if items[i].Name != w.name || items[i].Status != w.status || items[i].Latest != w.latest {
			t.Errorf("item %d = %+v, want %+v", i, items[i], w)
		}
	}
	if !strings.Contains(items[2].Error, "HTTP 500") {
		t.Errorf("broken collection error = %q, want the HTTP 500", items[2].Error)
	}
}

func TestSearchReturnsNewestVersions(t *testing.T) {
	c := NewGalaxyClient(fakeGalaxyAPI(t).URL)
	res, err := c.Search(context.Background(), "nginx")
	if err != nil {
		t.Fatal(err)
	}
	if res.CollectionsTotal != 171 || res.RolesTotal != 832 {
		t.Errorf("totals = %d/%d, want 171/832", res.CollectionsTotal, res.RolesTotal)
	}
	want := GalaxyHit{Kind: "collection", Name: "community.general", Version: "14.0.1", Description: "General plugins"}
	if len(res.Collections) != 1 || res.Collections[0] != want {
		t.Errorf("collections = %+v, want [%+v]", res.Collections, want)
	}
	if len(res.Roles) != 2 || res.Roles[0].Name != "geerlingguy.nginx" || res.Roles[0].Version != "3.3.1" || res.Roles[1].Version != "" {
		t.Errorf("roles = %+v", res.Roles)
	}
	if _, err := c.Search(context.Background(), "broken"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("search failure not reported: %v", err)
	}
}
