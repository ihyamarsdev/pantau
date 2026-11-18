package gwupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The upstream sources are read from fixed URLs, so the tests point the package at a fake server by
// swapping the package-level clients and URLs.

func TestLatestAnsibleReadsVersionWithoutHistory(t *testing.T) {
	var served string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A PyPI document: "info" first with "version" as its first field, then the release history.
		served = `{"info":{"version":"2.22.0","summary":"x"},"releases":{"1.0.0":[],"2.22.0":[]}}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(served))
	}))
	defer srv.Close()

	old := PyPIURL
	PyPIURL = srv.URL
	defer func() { PyPIURL = old }()

	rel, err := LatestAnsible(context.Background())
	if err != nil {
		t.Fatalf("LatestAnsible: %v", err)
	}
	if rel.Version != "2.22.0" {
		t.Errorf("version = %q, want 2.22.0", rel.Version)
	}
	if !strings.Contains(rel.URL, "2.22.0") {
		t.Errorf("url = %q, want it to name the version", rel.URL)
	}
}

func TestLatestTerraformReadsVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.17.2","timestamp":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	rel, err := LatestTerraform(context.Background())
	if err != nil {
		t.Fatalf("LatestTerraform: %v", err)
	}
	if rel.Version != "1.17.2" {
		t.Errorf("version = %q, want 1.17.2", rel.Version)
	}
	if !strings.Contains(rel.URL, "v1.17.2") {
		t.Errorf("url = %q, want it to name the tag", rel.URL)
	}
}

func TestLatestOpenTofuReadsRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://github.com/opentofu/opentofu/releases/tag/v1.13.9")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	old := OpenTofuURL
	OpenTofuURL = srv.URL
	defer func() { OpenTofuURL = old }()

	rel, err := LatestOpenTofu(context.Background())
	if err != nil {
		t.Fatalf("LatestOpenTofu: %v", err)
	}
	if rel.Version != "v1.13.9" {
		t.Errorf("version = %q, want v1.13.9", rel.Version)
	}
	if !strings.Contains(rel.URL, "v1.13.9") {
		t.Errorf("url = %q, want it to name the tag", rel.URL)
	}
}

// A redirect that does not name a version is a failure, not a silent empty version.
func TestLatestOpenTofuRejectsBadRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.com/not-a-version")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	old := OpenTofuURL
	OpenTofuURL = srv.URL
	defer func() { OpenTofuURL = old }()

	if _, err := LatestOpenTofu(context.Background()); err == nil {
		t.Fatal("expected an error for a redirect without a version")
	}
}

// An upstream that answers with an error status must fail, not report an empty version.
func TestLatestTerraformRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	if _, err := LatestTerraform(context.Background()); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestVersionFromTagPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/opentofu/opentofu/releases/tag/v1.13.9", "v1.13.9"},
		{"https://github.com/opentofu/opentofu/releases/tag/1.13.9", "1.13.9"},
		{"https://example.com/releases/tag/not-a-version", ""},
		{"", ""},
		{"https://example.com/", ""},
	}
	for _, c := range cases {
		if got := versionFromTagPath(c.in); got != c.want {
			t.Errorf("versionFromTagPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLatestUnknownTarget(t *testing.T) {
	if _, err := Latest(context.Background(), "nope"); err == nil {
		t.Fatal("expected an error for an unknown target")
	}
}
