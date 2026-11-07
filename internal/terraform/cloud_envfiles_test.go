package terraform

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTerraform puts a shell script named terraform on PATH that prints env, shows the
// secret file's mode, and fails init so Plan returns right after the first invocation.
func fakeTerraform(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo "REGION=$AWS_REGION"
echo "DEFREGION=$AWS_DEFAULT_REGION"
echo "TOKEN=$HCLOUD_TOKEN"
echo "KUBE=$KUBECONFIG"
echo "MODE=$(stat -c %a "$KUBECONFIG")"
cat "$KUBECONFIG"
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "terraform"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRedactPlainEnvAndEnvFiles(t *testing.T) {
	fakeTerraform(t)
	root := t.TempDir()
	spec := PlanSpec{RunID: 1, Config: Config{
		Binary: "terraform", DataRoot: t.TempDir(), Root: root,
		Files: map[string]string{"main.tf": ""},
		Env: map[string]string{
			"AWS_REGION":         "eu-west-1",
			"AWS_DEFAULT_REGION": "user-region-1", // plain key overridden by a user variable: not in PlainEnv
			"HCLOUD_TOKEN":       "hcloud-secret-token",
		},
		PlainEnv: map[string]bool{"AWS_REGION": true},
		EnvFiles: map[string]string{"KUBECONFIG": "kubeconfig-secret-content"},
	}}
	var log bytes.Buffer
	if _, err := Plan(context.Background(), spec, &log); err != nil {
		t.Fatal(err)
	}
	out := log.String()
	for _, want := range []string{"REGION=eu-west-1", "DEFREGION=(sensitive)", "TOKEN=(sensitive)", "MODE=600", "KUBE=" + filepath.Join(root, "secrets")} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	for _, leak := range []string{"hcloud-secret-token", "user-region-1", "kubeconfig-secret-content"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q:\n%s", leak, out)
		}
	}
	// Files are removed after the invocation.
	var left []string
	_ = filepath.Walk(filepath.Join(root, "secrets"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			left = append(left, p)
		}
		return nil
	})
	if len(left) != 0 {
		t.Errorf("secret files left behind: %v", left)
	}
}

func TestEnvFilesPerms(t *testing.T) {
	root := t.TempDir()
	c := Config{Root: root, Env: map[string]string{"A": "b"}, EnvFiles: map[string]string{"KUBECONFIG": "x", "KUBE_CONFIG_PATH": "x"}}
	got, cleanup, err := c.withEnvFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"KUBECONFIG", "KUBE_CONFIG_PATH"} {
		p := got.Env[k]
		if !strings.HasPrefix(p, filepath.Join(root, "secrets")+string(os.PathSeparator)) {
			t.Fatalf("%s path %q not under secrets", k, p)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0600 {
			t.Fatalf("%s: %v %v", p, fi, err)
		}
		if fi, _ := os.Stat(filepath.Dir(p)); fi.Mode().Perm() != 0700 {
			t.Fatalf("dir perm %v", fi.Mode().Perm())
		}
	}
	if c.Env["KUBECONFIG"] != "" || got.Env["A"] != "b" {
		t.Fatal("original env mutated or lost")
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(got.Env["KUBECONFIG"])); !os.IsNotExist(err) {
		t.Fatal("secrets dir not removed")
	}
}
