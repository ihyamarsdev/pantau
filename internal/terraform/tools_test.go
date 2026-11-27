package terraform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

func installFakeTerraform(t *testing.T, scriptBody string) string {
	t.Helper()
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "terraform")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"+scriptBody), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	InvalidateDetectCache()
	t.Cleanup(InvalidateDetectCache)
	return binDir
}

func TestValidateConfig_MultiModuleAndPluginCacheMutex(t *testing.T) {
	dataRoot := t.TempDir()
	lockProbe := filepath.Join(t.TempDir(), "init-active")
	installFakeTerraform(t, `
case "$1" in
  init)
    if [ -z "$TF_PLUGIN_CACHE_DIR" ] || [ "$TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE" != "true" ]; then
      echo "missing plugin cache env: TF_PLUGIN_CACHE_DIR=$TF_PLUGIN_CACHE_DIR BREAK=$TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE" >&2
      exit 1
    fi
    if ! mkdir "`+lockProbe+`" 2>/dev/null; then
      echo "concurrent init detected in TF_PLUGIN_CACHE_DIR!" >&2
      exit 1
    fi
    echo "Initializing modules..."
    echo "- network in .terraform/modules/network"
    echo "- vm in .terraform/modules/vm"
    echo "- storage in .terraform/modules/storage"
    rmdir "`+lockProbe+`"
    exit 0
    ;;
  validate)
    echo '{"valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}'
    exit 0
    ;;
esac
exit 0
`)

	files := map[string]string{
		"main.tf": `
module "network" { source = "terraform-google-modules/network/google" }
module "vm"      { source = "terraform-google-modules/vm/google//modules/compute_instance" }
module "storage" { source = "terraform-google-modules/cloud-storage/google" }
`,
	}

	const workers = 4
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := ValidateConfig(context.Background(), "terraform", dataRoot, files)
			if err != nil {
				errs <- err
				return
			}
			if !res.OK || res.InitError != "" {
				errs <- os.ErrInvalid
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent ValidateConfig failed: %v", err)
	}
}

func TestValidateConfig_InitErrorTailPreserved(t *testing.T) {
	dataRoot := t.TempDir()
	installFakeTerraform(t, `
if [ "$1" = "init" ]; then
  i=1
  while [ $i -le 200 ]; do
    echo "Downloading registry.terraform.io/terraform-google-modules/network/google 10.0.0 for module_$i..."
    i=$((i + 1))
  done
  echo "Error: Failed to query available provider packages"
  echo "  on versions.tf line 3: Could not retrieve the list of available versions for provider hashicorp/google"
  echo "  Authorization: Bearer supersecrettoken12345"
  exit 1
fi
exit 0
`)

	res, err := ValidateConfig(context.Background(), "terraform", dataRoot, map[string]string{"main.tf": ""})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected OK=false on init failure")
	}
	if !strings.Contains(res.InitErrorTail, "Error: Failed to query available provider packages") ||
		!strings.Contains(res.InitErrorTail, "Could not retrieve the list of available versions") {
		t.Fatalf("expected full trailing Error block in InitErrorTail, got: %q", res.InitErrorTail)
	}
	if strings.Contains(res.InitErrorTail, "supersecrettoken12345") {
		t.Fatalf("expected secret to be redacted in InitErrorTail: %q", res.InitErrorTail)
	}
}

func TestValidateConfig_StructuredDiagnostics(t *testing.T) {
	dataRoot := t.TempDir()
	installFakeTerraform(t, `
case "$1" in
  init) exit 0 ;;
  validate)
    cat <<'EOF'
{"valid":false,"error_count":2,"warning_count":1,"diagnostics":[
  {"severity":"error","summary":"Missing required argument","detail":"The argument \"subnetwork\" is required, but no definition was found.","range":{"filename":"main.tf","start":{"line":12,"column":3,"byte":120}}},
  {"severity":"error","summary":"Unsupported argument","detail":"An argument named \"bogus\" is not expected here.","range":{"filename":"main.tf","start":{"line":15,"column":3,"byte":180}}},
  {"severity":"warning","summary":"Deprecated attribute","detail":"Use new_attr instead.","range":{"filename":"variables.tf","start":{"line":4,"column":1,"byte":40}}}
]}
EOF
    exit 1
    ;;
esac
`)

	res, err := ValidateConfig(context.Background(), "terraform", dataRoot, map[string]string{"main.tf": ""})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.ErrorCount != 2 || res.WarningCount != 1 || len(res.Diagnostics) != 3 {
		t.Fatalf("unexpected ValidateResult: %+v", res)
	}
	if res.Diagnostics[0].File != "main.tf" || res.Diagnostics[0].Line != 12 || res.Diagnostics[0].Severity != "error" {
		t.Fatalf("unexpected first diagnostic: %+v", res.Diagnostics[0])
	}
}
