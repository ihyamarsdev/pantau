package terraform

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func skipNoTerraform(t *testing.T) string {
	t.Helper()
	for _, b := range []string{"terraform", "tofu"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	t.Skip("terraform/tofu not installed")
	return ""
}

func TestToolsFormatFiles(t *testing.T) {
	bin := skipNoTerraform(t)
	files := map[string]string{
		"main.tf":   "resource \"terraform_data\" \"a\" {\n      input   =   \"x\"\n}\n",
		"clean.tf":  "variable \"v\" {}\n",
		"bad.tf":    "resource \"terraform_data\" {{{\n",
		"README.md": "  not   hcl  ",
	}
	res, err := FormatFiles(context.Background(), bin, files)
	if err != nil {
		t.Fatal(err)
	}
	if want := "resource \"terraform_data\" \"a\" {\n  input = \"x\"\n}\n"; res.Files["main.tf"] != want {
		t.Fatalf("main.tf = %q", res.Files["main.tf"])
	}
	if len(res.Changed) != 1 || res.Changed[0] != "main.tf" {
		t.Fatalf("changed = %v", res.Changed)
	}
	if res.Errors["bad.tf"] == "" || res.Files["bad.tf"] != files["bad.tf"] {
		t.Fatalf("bad.tf: errors=%v files=%q", res.Errors, res.Files["bad.tf"])
	}
	if res.Files["README.md"] != files["README.md"] || res.Errors["README.md"] != "" {
		t.Fatal("non-hcl file must be untouched")
	}
}

func TestToolsFormatFilesRejectsBadInput(t *testing.T) {
	if _, err := FormatFiles(context.Background(), "rm", map[string]string{"a.tf": ""}); err == nil {
		t.Fatal("invalid binary accepted")
	}
	if _, err := FormatFiles(context.Background(), "terraform", map[string]string{"../a.tf": ""}); err == nil {
		t.Fatal("invalid file name accepted")
	}
}

func TestToolsValidateConfig(t *testing.T) {
	bin := skipNoTerraform(t)
	dataRoot := t.TempDir()
	good := map[string]string{"main.tf": "resource \"terraform_data\" \"a\" {\n  input = \"x\"\n}\n"}
	res, err := ValidateConfig(context.Background(), bin, dataRoot, good)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.InitError != "" || len(res.Diagnostics) != 0 {
		t.Fatalf("good config: %+v", res)
	}

	bad := map[string]string{"main.tf": "resource \"terraform_data\" \"a\" {\n  input = var.missing\n}\n"}
	res, err = ValidateConfig(context.Background(), bin, dataRoot, bad)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || len(res.Diagnostics) == 0 {
		t.Fatalf("bad config: %+v", res)
	}
	d := res.Diagnostics[0]
	if d.File != "main.tf" || d.Line != 2 || d.Severity != "error" || !strings.Contains(d.Summary+d.Detail, "missing") {
		t.Fatalf("diagnostic = %+v", d)
	}
}
