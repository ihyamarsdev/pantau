package terraform

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRegistryScanRefs(t *testing.T) {
	files := map[string]string{
		"main.tf": `# module "ghost" { source = "a/b/c" }
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0" # pinned
  name    = "x"
}

module "bare" {
  source = "hashicorp/consul/aws"
}

module "git" {
  source  = "git::https://example.com/x.git?ref=v1"
  version = "1.0.0"
}
module "local" { source = "./modules/x" }
module "http" { source = "https://example.com/x.zip" }
module "gh" { source = "github.com/a/b" }
module "sub" { source = "a/b/c//sub" }
module "hosted" {
  source = "registry.terraform.io/a/b/c"
  version = "= 1.0.0"
}
module "dyn" {
  source  = "a/b/c"
  version = var.v
}
`,
		"providers.tf": `terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.1, < 6.0"
    }
    random = "~> 3.0"
    null   = { source = "hashicorp/null", version = "3.2.1" }
    custom = {
      source = "example.com/acme/custom"
      version = "1.0"
    }
    google = {
      source = "hashicorp/google"
    }
    kube = { version = "~> 2.0" }
  }
}
`,
		"notes.txt": `module "x" { source = "a/b/c" }`,
	}
	want := []RegistryRef{
		{Kind: "provider", Name: "aws", Source: "hashicorp/aws", Constraint: ">= 5.1, < 6.0", File: "providers.tf", Line: 6, Coord: "hashicorp/aws"},
		{Kind: "provider", Name: "random", Source: "hashicorp/random", Constraint: "~> 3.0", File: "providers.tf", Line: 8, Coord: "hashicorp/random"},
		{Kind: "provider", Name: "null", Source: "hashicorp/null", Constraint: "3.2.1", File: "providers.tf", Line: 9, Coord: "hashicorp/null"},
		{Kind: "provider", Name: "custom", Source: "example.com/acme/custom", Constraint: "1.0", File: "providers.tf", Line: 12},
		{Kind: "provider", Name: "google", Source: "hashicorp/google", File: "providers.tf", Line: 14, Coord: "hashicorp/google"},
		{Kind: "provider", Name: "kube", Source: "hashicorp/kube", Constraint: "~> 2.0", File: "providers.tf", Line: 17, Coord: "hashicorp/kube"},
	}
	main := []RegistryRef{
		{Kind: "module", Name: "vpc", Source: "terraform-aws-modules/vpc/aws", Constraint: "~> 5.0", File: "main.tf", Line: 4, Coord: "terraform-aws-modules/vpc/aws"},
		{Kind: "module", Name: "bare", Source: "hashicorp/consul/aws", File: "main.tf", Line: 8, Coord: "hashicorp/consul/aws"},
		{Kind: "module", Name: "git", Source: "git::https://example.com/x.git?ref=v1", Constraint: "1.0.0", File: "main.tf", Line: 14},
		{Kind: "module", Name: "local", Source: "./modules/x", File: "main.tf", Line: 16},
		{Kind: "module", Name: "http", Source: "https://example.com/x.zip", File: "main.tf", Line: 17},
		{Kind: "module", Name: "gh", Source: "github.com/a/b", File: "main.tf", Line: 18},
		{Kind: "module", Name: "sub", Source: "a/b/c//sub", File: "main.tf", Line: 19},
		{Kind: "module", Name: "hosted", Source: "registry.terraform.io/a/b/c", Constraint: "= 1.0.0", File: "main.tf", Line: 22, Coord: "a/b/c"},
	}
	got := ScanRegistryRefs(files, "registry.terraform.io")
	want = append(main, want...)
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("ref %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func TestRegistryScanMalformed(t *testing.T) {
	for _, src := range []string{"module \"a\" {", "terraform { required_providers {", "module {", "module \"a\" { source = \"a/b/c\"", "terraform {\n required_providers {\n aws = {\n"} {
		_ = ScanRegistryRefs(map[string]string{"x.tf": src}, "h") // must not panic or hang
	}
}

func TestRegistryCheckStatus(t *testing.T) {
	tests := []struct{ c, latest, want string }{
		{"~> 5.0", "5.9.0", RegUpToDate},
		{"~> 4.0", "5.9.0", RegNewerOut},
		{">= 1.0, < 2.0", "2.1.0", RegNewerOut},
		{"= 1.0.0", "1.2.0", RegOutdated},
		{"1.0.0", "1.2.0", RegOutdated},
		{"1.2.0", "1.2.0", RegUpToDate},
		{"2.0.0", "1.2.0", RegUpToDate},
		{"", "1.2.0", RegUnpinned},
		{"~> 1.0", "", RegUnknown},
		{"~> junk", "1.0.0", RegUnknown},
		{"1.x", "1.0.0", RegUnknown},
		{">= 1.0", "3.0.0", RegUpToDate},
	}
	for _, tc := range tests {
		if got := RegistryStatus(tc.c, tc.latest); got != tc.want {
			t.Errorf("RegistryStatus(%q, %q) = %s, want %s", tc.c, tc.latest, got, tc.want)
		}
	}
}

type fakeLatest map[string]string

func (f fakeLatest) get(k string) (string, error) {
	if v, ok := f[k]; ok {
		return v, nil
	}
	return "", errors.New("HTTP 404")
}

func (f fakeLatest) LatestModuleVersion(_ context.Context, ns, name, p string) (string, error) {
	return f.get("m:" + ns + "/" + name + "/" + p)
}

func (f fakeLatest) LatestProviderVersion(_ context.Context, ns, typ string) (string, error) {
	return f.get("p:" + ns + "/" + typ)
}

func TestRegistryCheckRefs(t *testing.T) {
	refs := []RegistryRef{
		{Kind: "module", Name: "vpc", Constraint: "~> 5.0", Coord: "a/vpc/aws"},
		{Kind: "provider", Name: "aws", Constraint: "= 4.0.0", Coord: "hashicorp/aws"},
		{Kind: "module", Name: "local", Constraint: "1.0.0"},
		{Kind: "provider", Name: "gone", Coord: "x/gone"},
	}
	items, errs := CheckRegistryRefs(context.Background(), fakeLatest{"m:a/vpc/aws": "6.1.0", "p:hashicorp/aws": "5.2.0"}, refs)
	wantStatus := []string{RegNewerOut, RegOutdated, RegUnknown, RegUnknown}
	for i, w := range wantStatus {
		if items[i].Status != w {
			t.Errorf("item %d status %s, want %s", i, items[i].Status, w)
		}
	}
	if items[0].Latest != "6.1.0" || items[0].Suggested != "~> 6.1" || items[2].Latest != "" {
		t.Errorf("items: %+v", items)
	}
	if len(errs) != 1 {
		t.Errorf("errs = %v", errs)
	}
}

func TestRegistryCheckCap(t *testing.T) {
	refs := make([]RegistryRef, 45)
	items, errs := CheckRegistryRefs(context.Background(), fakeLatest{}, refs)
	if len(items) != 40 || len(errs) != 1 {
		t.Fatalf("items %d errs %v", len(items), errs)
	}
}
