package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestStateOpsImportShim(t *testing.T) {
	shimDir := t.TempDir()
	logFile := filepath.Join(shimDir, "calls.log")
	script := `#!/bin/sh
echo "CALL|$*" >> '` + logFile + `'
echo "ENV|$HCLOUD_TOKEN|$TF_DATA_DIR" >> '` + logFile + `'
echo "tok=$HCLOUD_TOKEN"
if [ "$1" = import ] && [ "$5" = bad-id ]; then echo "boom" >&2; exit 1; fi
if [ "$1" = import ]; then
  printf '{"version":4,"serial":3,"lineage":"lin","outputs":{},"resources":[{"mode":"managed","type":"aws_instance","name":"web","provider":"p","instances":[]}]}' > "$TF_DATA_DIR/../terraform.tfstate"
fi
`
	if err := os.WriteFile(filepath.Join(shimDir, "terraform"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	old := resolveCloudEnv
	t.Cleanup(func() { resolveCloudEnv = old })
	resolveCloudEnv = func(ctx context.Context, provider string, fields map[string]string, hc *http.Client) (map[string]string, error) {
		return map[string]string{"HCLOUD_TOKEN": fields["token"]}, nil
	}

	srv, auth, _, id := stateOpsEnv(t, map[string]string{"main.tf": "# x\n"})
	acct, err := srv.db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "h", Provider: "hetzner", Fields: map[string]string{"token": "hcloud-secret-123"}})
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := srv.db.GetTFWorkspace(id)
	ws.CloudAccountIDs = []int64{acct}
	if err := srv.db.UpdateTFWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	code, out := stateOpsPost(t, srv, auth, id, map[string]string{"op": "import", "address": "aws_instance.web", "import_id": "i-123"})
	if code != 200 || out["ok"] != true {
		t.Fatalf("import: %d %v", code, out)
	}
	output, _ := out["output"].(string)
	if strings.Contains(output, "hcloud-secret-123") || !strings.Contains(output, "(sensitive)") {
		t.Errorf("output not redacted: %q", output)
	}
	b, _ := os.ReadFile(logFile)
	got := string(b)
	for _, want := range []string{
		"CALL|init -input=false -no-color -reconfigure",
		"CALL|import -input=false -no-color aws_instance.web i-123",
		"ENV|hcloud-secret-123|",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shim log missing %q:\n%s", want, got)
		}
	}
	latest, _ := srv.db.LatestTFStateVersion(id)
	if latest == nil || latest.Source != "import aws_instance.web" || latest.Serial != 3 || latest.Lineage != "lin" {
		t.Fatalf("saved version: %+v", latest)
	}

	// Failing import: 422, redacted output returned, no new version, lock released.
	code, out = stateOpsPost(t, srv, auth, id, map[string]string{"op": "import", "address": "aws_instance.web2", "import_id": "bad-id"})
	if code != http.StatusUnprocessableEntity || out["ok"] != false {
		t.Fatalf("failing import: %d %v", code, out)
	}
	if s, _ := out["output"].(string); strings.Contains(s, "hcloud-secret-123") || !strings.Contains(s, "boom") {
		t.Errorf("failing output: %q", s)
	}
	if vs, _ := srv.db.ListTFStateVersions(id, 10); len(vs) != 1 {
		t.Errorf("want 1 version, got %d", len(vs))
	}
	if w, _ := srv.db.GetTFWorkspace(id); w.Locked {
		t.Fatal("lock not released after failed import")
	}
}
