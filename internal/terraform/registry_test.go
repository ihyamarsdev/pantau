package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

const regVMGoogleV15Fixture = `{
  "id": "terraform-google-modules/vm/google/15.0.0",
  "namespace": "terraform-google-modules",
  "name": "vm",
  "version": "15.0.0",
  "provider": "google",
  "description": "Google Compute Engine VM modules",
  "source": "https://github.com/terraform-google-modules/terraform-google-vm",
  "versions": ["14.0.0", "15.0.0"],
  "root": {
    "readme": "# VM",
    "inputs": [],
    "outputs": [],
    "dependencies": [],
    "provider_dependencies": [{"name": "google", "namespace": "hashicorp", "source": "hashicorp/google", "version": ">= 6.0"}]
  },
  "submodules": [
    {
      "path": "modules/compute_instance",
      "inputs": [
        {"name": "subnetwork", "type": "string", "description": "Subnetwork self_link", "default": null, "required": true},
        {"name": "instance_template", "type": "string", "description": "Instance template self_link", "default": null, "required": true},
        {"name": "hostname", "type": "string", "description": "Hostname", "default": "\"instance\"", "required": false},
        {"name": "num_instances", "type": "number", "description": "Number of instances", "default": 1, "required": false}
      ],
      "outputs": [{"name": "instances_self_links", "description": "Self links"}]
    }
  ]
}`

const regCloudStorageFixture = `{
  "id": "terraform-google-modules/cloud-storage/google/9.0.0",
  "namespace": "terraform-google-modules",
  "name": "cloud-storage",
  "version": "9.0.0",
  "provider": "google",
  "description": "Create GCS buckets",
  "source": "https://github.com/terraform-google-modules/terraform-google-cloud-storage",
  "versions": ["9.0.0"],
  "root": {
    "readme": "# Cloud Storage",
    "inputs": [
      {"name": "name", "type": "string", "description": "Bucket name", "default": null, "required": true},
      {"name": "project_id", "type": "string", "description": "GCP project", "default": null, "required": true},
      {"name": "location", "type": "string", "description": "Bucket location", "default": "\"US\"", "required": false}
    ],
    "outputs": [{"name": "bucket", "description": "Bucket resource"}],
    "dependencies": [],
    "provider_dependencies": []
  },
  "submodules": []
}`

func TestParseRegistryModuleID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantNS  string
		wantMod string
		wantPrv string
		wantVer string
		wantErr bool
	}{
		{name: "3 parts", input: "terraform-google-modules/vm/google", wantNS: "terraform-google-modules", wantMod: "vm", wantPrv: "google"},
		{name: "4 parts with version", input: "terraform-google-modules/vm/google/15.0.0", wantNS: "terraform-google-modules", wantMod: "vm", wantPrv: "google", wantVer: "15.0.0"},
		{name: "trimmed", input: "  hashicorp/consul/aws  ", wantNS: "hashicorp", wantMod: "consul", wantPrv: "aws"},
		{name: "empty", input: "", wantErr: true},
		{name: "two parts", input: "hashicorp/consul", wantErr: true},
		{name: "five parts", input: "a/b/c/d/e", wantErr: true},
		{name: "path traversal", input: "../etc/passwd", wantErr: true},
		{name: "dotdot in segment", input: "a/b..c/google", wantErr: true},
		{name: "flag injection", input: "-flag/vm/google", wantErr: true},
		{name: "url ssrf attempt", input: "https://evil.com/vm/google", wantErr: true},
		{name: "query injection", input: "a/b/c?x=1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ns, name, prov, ver, err := ParseRegistryModuleID(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrBadRegistryArg) {
					t.Fatalf("ParseRegistryModuleID(%q) err = %v, want ErrBadRegistryArg", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRegistryModuleID(%q) unexpected err = %v", tt.input, err)
			}
			if ns != tt.wantNS || name != tt.wantMod || prov != tt.wantPrv || ver != tt.wantVer {
				t.Fatalf("got (%q,%q,%q,%q), want (%q,%q,%q,%q)", ns, name, prov, ver, tt.wantNS, tt.wantMod, tt.wantPrv, tt.wantVer)
			}
		})
	}
}

func TestRegistryBatchSearchAndInspect(t *testing.T) {
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/modules/search":
			q := r.URL.Query().Get("q")
			switch q {
			case "gcp network":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/network/google/10.0.0","namespace":"terraform-google-modules","name":"network","provider":"google","version":"10.0.0","description":"VPC"}]}`))
			case "gcp vm":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/vm/google/15.0.0","namespace":"terraform-google-modules","name":"vm","provider":"google","version":"15.0.0","description":"VM"},{"id":"terraform-google-modules/network/google/10.0.0","namespace":"terraform-google-modules","name":"network","provider":"google","version":"10.0.0","description":"VPC"}]}`))
			default:
				_, _ = w.Write([]byte(`{"modules":[]}`))
			}
		case "/v1/modules/terraform-google-modules/vm/google":
			_, _ = w.Write([]byte(regVMGoogleV15Fixture))
		case "/v1/modules/terraform-google-modules/cloud-storage/google":
			_, _ = w.Write([]byte(regCloudStorageFixture))
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	mods, err := c.BatchSearchModules(ctx, []string{"gcp network", "gcp vm"}, "google", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 {
		t.Fatalf("expected 2 deduplicated modules, got %d: %+v", len(mods), mods)
	}

	vmDetail, err := c.ModuleDetail(ctx, "terraform-google-modules", "vm", "google", "")
	if err != nil {
		t.Fatal(err)
	}
	schema := FormatModuleSchema(vmDetail, "")
	if schema.Version != "15.0.0" || len(schema.Submodules) != 1 {
		t.Fatalf("unexpected schema: %+v", schema)
	}
	if schema.Submodules[0].Source != "terraform-google-modules/vm/google//modules/compute_instance" {
		t.Fatalf("unexpected submodule source: %q", schema.Submodules[0].Source)
	}
	if len(schema.Submodules[0].RequiredInputs) != 2 {
		t.Fatalf("expected 2 required inputs on compute_instance, got %+v", schema.Submodules[0].RequiredInputs)
	}
	b, err := json.Marshal(schema)
	if err != nil || len(b) >= 4096 {
		t.Fatalf("schema size %d >= 4096 or marshal err %v", len(b), err)
	}

	subSchema := FormatModuleSchema(vmDetail, "compute_instance")
	if len(subSchema.Submodules) != 1 || len(subSchema.Submodules[0].OptionalInputs) != 2 || len(subSchema.Submodules[0].Outputs) != 1 {
		t.Fatalf("expected full optional inputs and outputs for compute_instance: %+v", subSchema)
	}
}

func TestNormalizeModuleSearchQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		wantNS    string
		wantQuery string
	}{
		{name: "namespace prefix multiword", input: "terraform-google-modules vm compute", wantNS: "terraform-google-modules", wantQuery: "vm"},
		{name: "aws namespace prefix", input: "terraform-aws-modules vpc", wantNS: "terraform-aws-modules", wantQuery: "vpc"},
		{name: "slash namespace", input: "hashicorp/consul", wantNS: "hashicorp", wantQuery: "consul"},
		{name: "plain query unchanged", input: "gcp network", wantNS: "", wantQuery: "gcp network"},
		{name: "special chars unchanged", input: "vpc & co", wantNS: "", wantQuery: "vpc & co"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ns, q := normalizeModuleSearchQuery(tt.input)
			if ns != tt.wantNS || q != tt.wantQuery {
				t.Fatalf("normalizeModuleSearchQuery(%q) = (%q, %q), want (%q, %q)", tt.input, ns, q, tt.wantNS, tt.wantQuery)
			}
		})
	}
}

func TestRegistryInspect_LatestVersionProviderDepsAndAllSubmodules(t *testing.T) {
	t.Parallel()
	const multiSubFixture = `{
  "id": "terraform-google-modules/vm/google/18.1.0",
  "namespace": "terraform-google-modules",
  "name": "vm",
  "version": "18.1.0",
  "provider": "google",
  "description": "VM modules",
  "versions": ["17.0.0", "18.1.0"],
  "root": {
    "inputs": [],
    "outputs": [],
    "provider_dependencies": [{"name": "google", "namespace": "hashicorp", "version": ">= 7.0, < 8.0"}]
  },
  "submodules": [
    {
      "path": "modules/compute_instance",
      "inputs": [{"name": "subnetwork", "type": "string", "required": true}],
      "outputs": [],
      "provider_dependencies": [{"name": "google", "namespace": "hashicorp", "version": ">= 7.0"}]
    },
    {
      "path": "modules/instance_template",
      "inputs": [{"name": "machine_type", "type": "string", "required": true}, {"name": "source_image", "type": "string", "required": true}],
      "outputs": [],
      "provider_dependencies": [{"name": "google", "namespace": "hashicorp", "version": ">= 7.0"}]
    }
  ]
}`
	c, _ := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/modules/search" {
			q := r.URL.Query()
			if q.Get("namespace") != "terraform-google-modules" || q.Get("q") != "vm" {
				t.Errorf("expected namespace=terraform-google-modules q=vm, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/vm/google/18.1.0","namespace":"terraform-google-modules","name":"vm","provider":"google","version":"18.1.0"}]}`))
			return
		}
		_, _ = w.Write([]byte(multiSubFixture))
	})

	sr, err := c.SearchModules(context.Background(), "terraform-google-modules vm compute", "google", 0, 10)
	if err != nil || len(sr.Modules) != 1 || sr.Modules[0].LatestVersion != "18.1.0" {
		t.Fatalf("SearchModules = %+v, err=%v", sr, err)
	}

	d, err := c.ModuleDetail(context.Background(), "terraform-google-modules", "vm", "google", "")
	if err != nil {
		t.Fatal(err)
	}
	sc := FormatModuleSchema(d, "")
	if sc.LatestVersion != "18.1.0" {
		t.Fatalf("LatestVersion = %q, want 18.1.0", sc.LatestVersion)
	}
	if len(sc.Root.ProviderDependencies) != 1 || sc.Root.ProviderDependencies[0].Version != ">= 7.0, < 8.0" {
		t.Fatalf("Root.ProviderDependencies = %+v", sc.Root.ProviderDependencies)
	}
	if len(sc.Submodules) != 2 {
		t.Fatalf("expected both compute_instance and instance_template submodules in 1 call, got %d", len(sc.Submodules))
	}
	if sc.Submodules[1].Path != "modules/instance_template" || len(sc.Submodules[1].RequiredInputs) != 2 || len(sc.Submodules[1].ProviderDependencies) != 1 {
		t.Fatalf("unexpected second submodule: %+v", sc.Submodules[1])
	}
}

func TestRegistrySingleflightConcurrent(t *testing.T) {
	gate := make(chan struct{})
	c, hits := regServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		_, _ = w.Write([]byte(regCloudStorageFixture))
	})

	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	ready := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready <- struct{}{}
			d, err := c.ModuleDetail(context.Background(), "terraform-google-modules", "cloud-storage", "google", "")
			if err != nil {
				errs <- err
				return
			}
			if d.Module.Version != "9.0.0" {
				errs <- errors.New("unexpected version: " + d.Module.Version)
			}
		}()
	}
	for i := 0; i < n; i++ {
		<-ready
	}
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected singleflight to coalesce into 1 HTTP request, got %d", got)
	}
}
