package ansible

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateAsset(t *testing.T) {
	big := func(n int) []byte { return make([]byte, n) }
	for _, tc := range []struct {
		name string
		a    Asset
		err  string // "" = valid
	}{
		{"template ok", Asset{AssetTemplate, "nginx/site.conf.j2", []byte("{{ x }}")}, ""},
		{"file ok binary", Asset{AssetFile, "certs/ca.der", []byte{0xff, 0x00, 0xfe}}, ""},
		{"hidden file ok", Asset{AssetFile, ".env.example", nil}, ""},
		{"bad kind", Asset{"role", "a", nil}, "invalid asset kind"},
		{"empty name", Asset{AssetFile, "", nil}, "asset name"},
		{"traversal", Asset{AssetFile, "../x", nil}, "asset name"},
		{"absolute", Asset{AssetFile, "/etc/passwd", nil}, "asset name"},
		{"backslash", Asset{AssetFile, `a\b`, nil}, "asset name"},
		{"git segment", Asset{AssetFile, "a/.git/b", nil}, "asset name"},
		{"unclean", Asset{AssetFile, "a//b", nil}, "asset name"},
		{"name too long", Asset{AssetFile, strings.Repeat("a", maxAssetNameLen+1), nil}, "exceeds 200"},
		{"template binary", Asset{AssetTemplate, "a.j2", []byte{0xff, 0xfe}}, "UTF-8"},
		{"template too big", Asset{AssetTemplate, "a.j2", big(MaxTemplateBytes + 1)}, "256 KiB"},
		{"file too big", Asset{AssetFile, "a", big(MaxAssetFileBytes + 1)}, "5 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAsset(tc.a)
			if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("got %v, want %q", err, tc.err)
			}
		})
	}
}

func TestRunSpecValidateAssets(t *testing.T) {
	spec := RunSpec{Playbook: "- hosts: all", Mode: "syntax", Assets: []Asset{{AssetFile, "../x", nil}}}
	if err := spec.Validate(); err == nil {
		t.Fatal("invalid asset accepted")
	}
	spec.Assets = nil
	for i := 0; i < 7; i++ {
		spec.Assets = append(spec.Assets, Asset{AssetFile, fmt.Sprint(i), make([]byte, MaxAssetFileBytes)})
	}
	if err := spec.Validate(); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("total size not enforced: %v", err)
	}
}

func TestWriteAssetsConflict(t *testing.T) {
	dir := t.TempDir()
	err := writeAssets(dir, []Asset{{AssetFile, "a", []byte("1")}, {AssetFile, "a/b", []byte("2")}})
	if err == nil {
		t.Fatal("a file and a folder with the same name must be rejected")
	}
}

// A real ansible-playbook finds templates/ and files/ next to an inline playbook and renders the Jinja template.
func TestRunAssetsRealAnsible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ansible controller is not supported on windows")
	}
	bin, err := exec.LookPath("ansible-playbook")
	if err != nil {
		t.Skip("ansible-playbook not installed")
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || !strings.Contains(string(out), "ansible") {
		t.Skip("ansible-playbook not functioning")
	}

	out := t.TempDir()
	playbook := fmt.Sprintf(`---
- hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - ansible.builtin.template:
        src: app/hello.conf.j2
        dest: %[1]s/hello.conf
    - ansible.builtin.copy:
        src: certs/ca.bin
        dest: %[1]s/ca.bin
`, out)
	spec := RunSpec{
		Playbook:  playbook,
		Mode:      "run",
		ExtraVars: `{"greeting":"halo"}`,
		Targets:   []Target{{ID: 1, Name: "web", Address: "127.0.0.1", Port: 22, User: "root"}},
		Assets: []Asset{
			{AssetTemplate, "app/hello.conf.j2", []byte("say={{ greeting }}\n")},
			{AssetFile, "certs/ca.bin", []byte{0x00, 0xff, 0x10}},
		},
	}
	var log strings.Builder
	code, err := Run(context.Background(), spec, &log)
	if err != nil || code != 0 {
		t.Fatalf("run failed (code %d, err %v):\n%s", code, err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(out, "hello.conf")); string(got) != "say=halo\n" {
		t.Fatalf("template not rendered: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "ca.bin")); string(got) != "\x00\xff\x10" {
		t.Fatalf("file not copied intact: %q", got)
	}
}
