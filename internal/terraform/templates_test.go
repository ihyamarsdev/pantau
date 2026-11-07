package terraform

import (
	"context"
	"os/exec"
	"regexp"
	"testing"
)

var wantTemplateIDs = []string{"hello-world", "aws-s3-bucket", "aws-ec2-instance", "gcp-storage-bucket", "azure-resource-group", "digitalocean-droplet", "hetzner-server", "cloudflare-dns-record", "kubernetes-namespace"}

var templateSecretRe = regexp.MustCompile(`(?i)AKIA[0-9A-Z]{12}|PRIVATE KEY|(secret|token|password|access_key|api_key)\s*=`)

func TestTemplates_Shape(t *testing.T) {
	tpls := Templates()
	if len(tpls) != len(wantTemplateIDs) {
		t.Fatalf("got %d templates, want %d", len(tpls), len(wantTemplateIDs))
	}
	providers := map[string]bool{"aws": true, "gcp": true, "azure": true, "digitalocean": true, "hetzner": true, "cloudflare": true, "kubernetes": true, "none": true}
	for i, tp := range tpls {
		if tp.ID != wantTemplateIDs[i] {
			t.Errorf("template %d id = %q, want %q", i, tp.ID, wantTemplateIDs[i])
		}
		if tp.Name == "" || tp.Description == "" || !providers[tp.Provider] {
			t.Errorf("%s: bad metadata %+v", tp.ID, tp)
		}
		if err := ValidateFiles(tp.Files); err != nil {
			t.Errorf("%s: ValidateFiles: %v", tp.ID, err)
		}
		for _, f := range []string{"main.tf", "variables.tf", "outputs.tf"} {
			if tp.Files[f] == "" {
				t.Errorf("%s: missing %s", tp.ID, f)
			}
		}
		for n, c := range tp.Files {
			if m := templateSecretRe.FindString(c); m != "" {
				t.Errorf("%s/%s: possible embedded credential %q", tp.ID, n, m)
			}
		}
		if len(tp.Variables) == 0 {
			t.Errorf("%s: no variables", tp.ID)
		}
		declared := map[string]bool{}
		for _, v := range ParseVariables(tp.Files) {
			declared[v.Name] = true
		}
		if len(declared) != len(tp.Variables) {
			t.Errorf("%s: variables %d != declared %d", tp.ID, len(tp.Variables), len(declared))
		}
		for _, v := range tp.Variables {
			if !declared[v.Name] || v.Description == "" {
				t.Errorf("%s: variable %q not declared or undocumented", tp.ID, v.Name)
			}
		}
	}
}

func TestTemplates_ReturnsFreshCopies(t *testing.T) {
	a := Templates()
	a[0].Files["main.tf"] = "mutated"
	if Templates()[0].Files["main.tf"] == "mutated" {
		t.Fatal("Templates() shares state between calls")
	}
}

func TestTemplates_FormatClean(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	for _, tp := range Templates() {
		res, err := FormatFiles(context.Background(), "terraform", tp.Files)
		if err != nil {
			t.Fatalf("%s: %v", tp.ID, err)
		}
		if len(res.Errors) > 0 || len(res.Changed) > 0 {
			t.Errorf("%s: fmt errors=%v changed=%v", tp.ID, res.Errors, res.Changed)
		}
	}
}

func TestTemplates_HelloWorldValidates(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	var files map[string]string
	for _, tp := range Templates() {
		if tp.ID == "hello-world" {
			files = tp.Files
		}
	}
	res, err := ValidateConfig(context.Background(), "terraform", t.TempDir(), files)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("hello-world invalid: %+v", res)
	}
}
