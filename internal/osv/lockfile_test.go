package osv

import (
	"testing"
)

func TestParseComposerLock(t *testing.T) {
	data := []byte(`{
		"packages": [
			{"name": "laravel/framework", "version": "v9.5.1"},
			{"name": "symfony/console", "version": "v6.0.0"}
		],
		"packages-dev": [{"name": "phpunit/phpunit", "version": "9.5.10"}]
	}`)
	pkgs, kind, err := ParseLockfile("composer.lock", "/repo/composer.lock", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if kind != "composer" {
		t.Errorf("kind = %q, want composer", kind)
	}
	if len(pkgs) != 3 {
		t.Fatalf("got %d packages, want 3: %+v", len(pkgs), pkgs)
	}
	if pkgs[0].Name != "laravel/framework" || pkgs[0].Version != "9.5.1" {
		t.Errorf("first = %+v, want laravel/framework 9.5.1 (v stripped)", pkgs[0])
	}
	if pkgs[2].Name != "symfony/console" {
		t.Errorf("sorted third = %+v, want symfony/console", pkgs[2])
	}
	for _, p := range pkgs {
		if p.Source != "/repo/composer.lock" || p.Kind != "composer" {
			t.Errorf("source/kind not set on %+v", p)
		}
	}
}

func TestParsePackageLockV2(t *testing.T) {
	data := []byte(`{
		"name": "app", "lockfileVersion": 2,
		"packages": {
			"": {"name": "app", "version": "1.0.0"},
			"node_modules/express": {"version": "4.18.1"},
			"node_modules/@scope/pkg": {"version": "2.0.0"},
			"node_modules/a/node_modules/b": {"version": "3.1.0"}
		}
	}`)
	pkgs, kind, err := ParseLockfile("package-lock.json", "app/package-lock.json", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if kind != "npm" {
		t.Errorf("kind = %q, want npm", kind)
	}
	want := map[string]string{
		"express": "4.18.1", "@scope/pkg": "2.0.0", "b": "3.1.0",
	}
	if len(pkgs) != len(want) {
		t.Fatalf("got %d packages, want %d: %+v", len(pkgs), len(want), pkgs)
	}
	for _, p := range pkgs {
		if want[p.Name] != p.Version {
			t.Errorf("package %q = %q, want %q", p.Name, p.Version, want[p.Name])
		}
	}
}

func TestParsePackageLockV1(t *testing.T) {
	data := []byte(`{"lockfileVersion": 1, "dependencies": {"lodash": {"version": "4.17.21"}}}`)
	pkgs, _, err := ParseLockfile("package-lock.json", "", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "lodash" || pkgs[0].Version != "4.17.21" {
		t.Fatalf("v1 parse = %+v", pkgs)
	}
}

func TestParsePnpmLock(t *testing.T) {
	data := []byte(`lockfileVersion: '6.0'
packages:
  /express/4.18.1:
    resolution: {integrity: sha512-x}
  /@scope/pkg/2.0.0:
    resolution: {integrity: sha512-y}
snapshots:
  /express/4.18.1: {}
`)
	pkgs, kind, err := ParseLockfile("pnpm-lock.yaml", "", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if kind != "pnpm" {
		t.Errorf("kind = %q, want pnpm", kind)
	}
	want := map[string]string{"express": "4.18.1", "@scope/pkg": "2.0.0"}
	if len(pkgs) != len(want) {
		t.Fatalf("got %d packages, want %d: %+v", len(pkgs), len(want), pkgs)
	}
	for _, p := range pkgs {
		if want[p.Name] != p.Version {
			t.Errorf("package %q = %q, want %q", p.Name, p.Version, want[p.Name])
		}
	}
}

func TestParseYarnLock(t *testing.T) {
	data := []byte(`# yarn lockfile v1
express@^4.18.0, express@4.18.1:
  version "4.18.1"
  resolved "https://registry.yarnpkg.com/express"

"@scope/pkg@npm:^2.0.0":
  version "2.0.1"
  resolved "https://registry.yarnpkg.com/@scope/pkg"
`)
	pkgs, kind, err := ParseLockfile("yarn.lock", "", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if kind != "yarn" {
		t.Errorf("kind = %q, want yarn", kind)
	}
	want := map[string]string{"express": "4.18.1", "@scope/pkg": "2.0.1"}
	if len(pkgs) != len(want) {
		t.Fatalf("got %d packages, want %d: %+v", len(pkgs), len(want), pkgs)
	}
	for _, p := range pkgs {
		if want[p.Name] != p.Version {
			t.Errorf("package %q = %q, want %q", p.Name, p.Version, want[p.Name])
		}
	}
}

func TestAppEcosystemMapping(t *testing.T) {
	cases := []struct {
		kind string
		want string
		ok   bool
	}{
		{"composer", EcoPackagist, true},
		{"npm", EcoNPM, true},
		{"pnpm", EcoNPM, true},
		{"yarn", EcoNPM, true},
		{"unknown", "", false},
	}
	for _, tc := range cases {
		eco, ok := AppEcosystem(tc.kind)
		if ok != tc.ok || eco.Name != tc.want {
			t.Errorf("AppEcosystem(%q) = (%q,%v), want (%q,%v)", tc.kind, eco.Name, ok, tc.want, tc.ok)
		}
	}
}

func TestAppPackagesGroupsByEcosystem(t *testing.T) {
	pkgs := []AppPackage{
		{Name: "laravel/framework", Version: "9.0", Kind: "composer"},
		{Name: "express", Version: "4.0", Kind: "npm"},
		{Name: "lodash", Version: "4.0", Kind: "yarn"},
	}
	byEco := AppPackages(pkgs)
	if len(byEco[EcoPackagist]) != 1 || len(byEco[EcoNPM]) != 2 {
		t.Errorf("grouping = %+v, want 1 packagist + 2 npm", byEco)
	}
}

func TestParseLockfileRejectsMalformed(t *testing.T) {
	if _, _, err := ParseLockfile("composer.lock", "", []byte("not json")); err == nil {
		t.Error("malformed composer.lock should be an error, not an empty clean list")
	}
	if _, _, err := ParseLockfile("unknown.lock", "", []byte("x")); err == nil {
		t.Error("unsupported lockfile should be an error")
	}
}
