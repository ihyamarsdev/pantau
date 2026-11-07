package terraform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func resetRegCache() {
	regCache.Lock()
	regCache.m = map[string]regCacheEntry{}
	regCache.Unlock()
}

// regServer starts a fake registry and returns a client for it plus a hit counter.
func regServer(t *testing.T, h http.HandlerFunc) (*RegistryClient, *int32) {
	t.Helper()
	resetRegCache()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewRegistryClient(srv.URL), &hits
}

const regModuleFixture = `{"id":"terraform-aws-modules/vpc/aws/6.7.3","namespace":"terraform-aws-modules","name":"vpc","version":"6.7.3","provider":"aws",
"description":"VPC module","source":"https://github.com/terraform-aws-modules/terraform-aws-vpc","published_at":"2026-09-18T19:52:23Z","downloads":229420746,"verified":false,
"providers":["aws"],"versions":["6.7.1","6.7.2","6.7.3"],
"root":{"name":"vpc","readme":"# VPC","inputs":[{"name":"cidr","type":"string","description":"CIDR block","default":"\"10.0.0.0/16\"","required":false},
{"name":"azs","type":"list(string)","description":"AZs","default":null,"required":true}],
"outputs":[{"name":"vpc_id","description":"The ID"}],"dependencies":[],
"provider_dependencies":[{"name":"aws","namespace":"hashicorp","source":"hashicorp/aws","version":">= 6.28","aliases":[]}]}}`

func TestRegistryArgValidation(t *testing.T) {
	c, hits := regServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(regModuleFixture)) })
	ctx := context.Background()
	for _, bad := range []string{"../x", "a b", "a/b", "", "..", ".x", "a..b", "x%2fy", strings.Repeat("a", 129)} {
		if _, err := c.ModuleDetail(ctx, bad, "vpc", "aws", ""); !errors.Is(err, ErrBadRegistryArg) {
			t.Errorf("ModuleDetail ns=%q: %v", bad, err)
		}
		if _, err := c.ProviderDetail(ctx, "hashicorp", bad); !errors.Is(err, ErrBadRegistryArg) {
			t.Errorf("ProviderDetail type=%q: %v", bad, err)
		}
	}
	if _, err := c.ModuleDetail(ctx, "a", "b", "c", "1 2"); !errors.Is(err, ErrBadRegistryArg) {
		t.Errorf("bad version: %v", err)
	}
	if _, err := c.SearchModules(ctx, "vpc", "../aws", 0, 5); !errors.Is(err, ErrBadRegistryArg) {
		t.Errorf("bad provider filter: %v", err)
	}
	if *hits != 0 {
		t.Fatalf("rejected args reached the registry: %d hits", *hits)
	}
}

func TestRegistrySearchModules(t *testing.T) {
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/modules/search" || q.Get("q") != "vpc & co" || q.Get("provider") != "aws" ||
			q.Get("offset") != "20" || q.Get("limit") != "50" || r.Header.Get("User-Agent") != "pantau" {
			t.Errorf("unexpected request %s UA=%q", r.URL, r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"meta":{"limit":50,"current_offset":20,"next_offset":70},"modules":[` +
			`{"id":"a/b/c/1.0.0","owner":"","namespace":"a","name":"b","version":"1.0.0","provider":"c","description":"d","source":"s","published_at":"p","downloads":5,"verified":true}]}`))
	})
	res, err := c.SearchModules(context.Background(), "vpc & co", "aws", 20, 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Modules) != 1 || res.Modules[0].Name != "b" || !res.Modules[0].Verified || res.Modules[0].Downloads != 5 ||
		res.NextOffset != 70 || res.TotalHint != 21 {
		t.Fatalf("decoded: %+v", res)
	}
}

func TestRegistryModuleDetail(t *testing.T) {
	big := strings.Repeat("é", 30<<10) // 60 KiB of 2-byte runes
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/modules/terraform-aws-modules/vpc/aws/6.7.2" {
			_, _ = w.Write([]byte(strings.Replace(regModuleFixture, `"readme":"# VPC"`, `"readme":"`+big+`"`, 1)))
			return
		}
		if r.URL.Path != "/v1/modules/terraform-aws-modules/vpc/aws" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(regModuleFixture))
	})
	ctx := context.Background()
	d, err := c.ModuleDetail(ctx, "terraform-aws-modules", "vpc", "aws", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Module.Version != "6.7.3" || strings.Join(d.Versions, ",") != "6.7.3,6.7.2,6.7.1" || len(d.Inputs) != 2 || !d.Inputs[1].Required ||
		d.Outputs[0].Name != "vpc_id" || d.Providers[0].Source != "hashicorp/aws" || d.Dependencies == nil || d.Readme != "# VPC" || d.ReadmeTruncated {
		t.Fatalf("decoded: %+v", d)
	}
	d, err = c.ModuleDetail(ctx, "terraform-aws-modules", "vpc", "aws", "6.7.2")
	if err != nil || !d.ReadmeTruncated || len(d.Readme) > regReadmeCap || !strings.HasSuffix(d.Readme, "é") {
		t.Fatalf("readme cap: err=%v truncated=%v len=%d", err, d.ReadmeTruncated, len(d.Readme))
	}
	if v, err := c.LatestModuleVersion(ctx, "terraform-aws-modules", "vpc", "aws"); err != nil || v != "6.7.3" {
		t.Fatalf("latest: %q %v", v, err)
	}
}

func TestRegistryProviders(t *testing.T) {
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/providers":
			q := r.URL.Query()
			if q.Get("filter[name]") != "aws" || q.Get("page[size]") != "20" || q.Get("page[number]") != "2" || q.Get("sort") != "-downloads" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"type":"providers","id":"1","attributes":{"name":"aws","namespace":"aaronfeng","full-name":"aaronfeng/aws","description":"x","downloads":7,"tier":"community","source":"https://github.com/a/b"}}],` +
				`"meta":{"pagination":{"page-size":20,"current-page":2,"next-page":3}}}`))
		case "/v1/providers/hashicorp/aws":
			_, _ = w.Write([]byte(`{"id":"hashicorp/aws/6.72.0","namespace":"hashicorp","name":"aws","version":"6.72.0","description":"d","source":"https://github.com/hashicorp/terraform-provider-aws","published_at":"2026-11-04T21:21:09Z","downloads":8,"tier":"official","versions":["6.70.0","6.71.0","6.72.0"],"docs":[]}`))
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	s, err := c.SearchProviders(ctx, "aws", 2)
	if err != nil || len(s.Providers) != 1 || s.Providers[0].FullName != "aaronfeng/aws" || s.Providers[0].Tier != "community" || s.NextPage != 3 {
		t.Fatalf("search: %+v %v", s, err)
	}
	d, err := c.ProviderDetail(ctx, "hashicorp", "aws")
	if err != nil || d.Latest != "6.72.0" || strings.Join(d.Versions, ",") != "6.72.0,6.71.0,6.70.0" || d.Provider.Tier != "official" ||
		d.DocsURL != c.BaseURL+"/providers/hashicorp/aws/latest/docs" {
		t.Fatalf("detail: %+v %v", d, err)
	}
	if v, err := c.LatestProviderVersion(ctx, "hashicorp", "aws"); err != nil || v != "6.72.0" {
		t.Fatalf("latest: %q %v", v, err)
	}
}
