package terraform

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateOpsAddressValidation(t *testing.T) {
	good := []string{
		"aws_instance.web", "aws_instance.web[0]", `aws_instance.web["a-b.c"]`,
		`module.a["k"].aws_x.y[0]`, "module.net.module.sub.aws_vpc.main", "data.aws_ami.x", "terraform_data.a_b-c",
		`aws_s3_bucket.b["10.0.0.0/24"]`,
	}
	bad := []string{
		"", "-var=x", "-aws_instance.x", "aws_instance.web x", "a.b;rm -rf /", "a.b|c", "a.b&c", "a.b$(x)", "a.b`x`",
		"a.b\nc", "a..b", ".a.b", "a.b.", "a.b[", `a.b["x y"]`, `a.b["x"y]`, "a.b['x']", "a.b[x]", "1abc.x", "a.b\\c",
		"a.b>c", "a.b<c", "a.b(c)", strings.Repeat("a", 600),
	}
	for _, s := range good {
		if err := ValidateStateAddress(s); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateStateAddress(s); err == nil {
			t.Errorf("%q should be rejected", s)
		}
	}
	for _, s := range []string{"i-123", "/subscriptions/x/resourceGroups/r", "projects/p/zones/z/instances/i", "arn:aws:s3:::b", "a,b=c"} {
		if err := ValidateImportID(s); err != nil {
			t.Errorf("import id %q should be valid: %v", s, err)
		}
	}
	for _, s := range []string{"", "-x", "a b", "a;b", "a$b", "a|b", "a`b", "a\nb", "a'b"} {
		if err := ValidateImportID(s); err == nil {
			t.Errorf("import id %q should be rejected", s)
		}
	}
}

func TestStateOpsSpecValidation(t *testing.T) {
	base := Config{Binary: "terraform", DataRoot: "/d", Root: "/d/1", Files: map[string]string{}}
	cases := []struct {
		name string
		s    StateOpSpec
		ok   bool
	}{
		{"rm", StateOpSpec{Config: base, Op: "rm", Address: "a.b"}, true},
		{"mv", StateOpSpec{Config: base, Op: "mv", Address: "a.b", To: "a.c"}, true},
		{"mv same", StateOpSpec{Config: base, Op: "mv", Address: "a.b", To: "a.b"}, false},
		{"mv no dest", StateOpSpec{Config: base, Op: "mv", Address: "a.b"}, false},
		{"import", StateOpSpec{Config: base, Op: "import", Address: "a.b", ImportID: "i-1"}, true},
		{"import no id", StateOpSpec{Config: base, Op: "import", Address: "a.b"}, false},
		{"bad op", StateOpSpec{Config: base, Op: "taint", Address: "a.b"}, false},
	}
	for _, c := range cases {
		if err := c.s.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestStateOpsRmMvReal(t *testing.T) {
	for _, bin := range []string{"terraform", "tofu"} {
		t.Run(bin, func(t *testing.T) {
			if _, err := exec.LookPath(bin); err != nil {
				t.Skipf("%s not in PATH", bin)
			}
			dataRoot := t.TempDir()
			cfg := Config{Binary: bin, DataRoot: dataRoot, Root: filepath.Join(dataRoot, "ws"), Files: map[string]string{
				"main.tf": "resource \"terraform_data\" \"a\" {\n  input = \"x\"\n}\n\nresource \"terraform_data\" \"b\" {\n  input = \"y\"\n}\n",
			}}
			var log bytes.Buffer
			if _, err := Plan(context.Background(), PlanSpec{Config: cfg, RunID: 1, Timeout: time.Minute}, &log); err != nil {
				t.Fatalf("plan: %v\n%s", err, log.String())
			}
			ar, err := Apply(context.Background(), ApplySpec{Config: cfg, RunID: 1, Timeout: time.Minute}, &log)
			if err != nil || ar.ExitCode != 0 {
				t.Fatalf("apply: %v\n%s", err, log.String())
			}

			cfg.State = ar.State
			res, err := StateOp(context.Background(), StateOpSpec{Config: cfg, Op: "rm", Address: "terraform_data.a"})
			if err != nil || res.ExitCode != 0 {
				t.Fatalf("rm: %v %+v", err, res)
			}
			info, err := ParseState(res.State)
			if err != nil || len(info.Resources) != 1 || info.Resources[0].Address != "terraform_data.b" {
				t.Fatalf("after rm: %v %+v", err, info)
			}

			cfg.State = res.State
			res, err = StateOp(context.Background(), StateOpSpec{Config: cfg, Op: "mv", Address: "terraform_data.b", To: "terraform_data.c"})
			if err != nil || res.ExitCode != 0 {
				t.Fatalf("mv: %v %+v", err, res)
			}
			info, _ = ParseState(res.State)
			if len(info.Resources) != 1 || info.Resources[0].Address != "terraform_data.c" {
				t.Fatalf("after mv: %+v", info)
			}
			if strings.Contains(res.Output, "Deprecated") {
				t.Errorf("deprecation warning in output: %s", res.Output)
			}

			// A failing command reports the exit code and no state.
			cfg.State = res.State
			res, err = StateOp(context.Background(), StateOpSpec{Config: cfg, Op: "rm", Address: "terraform_data.nope"})
			if err != nil || res.ExitCode == 0 || res.State != nil {
				t.Fatalf("rm missing: %v %+v", err, res)
			}
		})
	}
}

func TestStateOpsImportShim(t *testing.T) {
	shimDir := t.TempDir()
	logFile := filepath.Join(shimDir, "calls.log")
	script := `#!/bin/sh
echo "CALL|$1|$*" >> '` + logFile + `'
echo "ENV|$AWS_ACCESS_KEY_ID|$AWS_SECRET_ACCESS_KEY|$TF_DATA_DIR" >> '` + logFile + `'
cat pantau_backend_override.tf >> '` + logFile + `' 2>/dev/null
echo "$1 secret=$AWS_SECRET_ACCESS_KEY"
if [ "$1" = import ]; then
  printf '{"version":4,"serial":2,"lineage":"l","outputs":{},"resources":[]}' > "$TF_DATA_DIR/../terraform.tfstate"
fi
`
	if err := os.WriteFile(filepath.Join(shimDir, "terraform"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dataRoot := t.TempDir()
	root := filepath.Join(dataRoot, "ws")
	res, err := StateOp(context.Background(), StateOpSpec{
		Config: Config{
			Binary: "terraform", DataRoot: dataRoot, Root: root,
			Files: map[string]string{"main.tf": "# x\n"},
			Env:   map[string]string{"AWS_ACCESS_KEY_ID": "AKIAPLAINID", "AWS_SECRET_ACCESS_KEY": "s3cr3tvalue"},
		},
		Op: "import", Address: `aws_instance.web["a"]`, ImportID: "i-123",
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("import: %v %+v", err, res)
	}
	if strings.Contains(res.Output, "s3cr3tvalue") || !strings.Contains(res.Output, "(sensitive)") {
		t.Errorf("output not redacted: %q", res.Output)
	}
	if len(res.State) == 0 {
		t.Error("no state returned")
	}
	b, _ := os.ReadFile(logFile)
	got := string(b)
	for _, want := range []string{
		"CALL|init|init -input=false -no-color -reconfigure",
		`CALL|import|import -input=false -no-color aws_instance.web["a"] i-123`,
		"ENV|AKIAPLAINID|s3cr3tvalue|" + filepath.Join(root, "data"),
		filepath.Join(root, "terraform.tfstate"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shim log missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-state=") {
		t.Errorf("import must not use -state:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "stateops")); err == nil {
		if ents, _ := os.ReadDir(filepath.Join(root, "stateops")); len(ents) != 0 {
			t.Errorf("work dir not cleaned up: %v", ents)
		}
	}
}
