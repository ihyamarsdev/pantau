package web

import (
	"strings"
	"testing"

	"pantau/internal/store"
	"pantau/internal/terraform"
)

func TestOutputLinkInject(t *testing.T) {
	srv, _ := setupTerraformTestDataServer(t)
	cons := linkTestWS(t, srv, "cons", false)
	src := linkTestWS(t, srv, "src", true)
	empty := linkTestWS(t, srv, "empty", false)
	mgr := NewTerraformRunManager(srv.db, t.TempDir(), nil, nil)
	ws, _ := srv.db.GetTFWorkspace(cons)

	if err := srv.db.ReplaceTFOutputLinks(cons, []store.TFOutputLink{
		{VarName: "tags", SourceWorkspaceID: src, OutputName: "tags"},
		{VarName: "pw", SourceWorkspaceID: src, OutputName: "pw"},
		{VarName: "vpc", SourceWorkspaceID: src, OutputName: "vpc"},
	}); err != nil {
		t.Fatal(err)
	}
	// explicit variable wins over the link
	if _, err := srv.db.CreateTFVariable(&store.TFVariable{WorkspaceID: cons, Key: "vpc", Value: `"manual"`, HCL: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.BuildRunConfig(ws)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]terraform.Var{}
	for _, v := range cfg.Vars {
		got[v.Key] = v
	}
	if v := got["tags"]; !v.HCL || v.Sensitive || v.Value != `{"a":"b","n":[1,2]}` {
		t.Errorf("tags = %+v", v)
	}
	if v := got["pw"]; !v.HCL || !v.Sensitive || v.Value != `"s3cret"` {
		t.Errorf("pw = %+v", v)
	}
	if v := got["vpc"]; v.Value != `"manual"` {
		t.Errorf("explicit variable overridden: %+v", v)
	}
	n := 0
	for _, v := range cfg.Vars {
		if v.Key == "vpc" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("vpc present %d times", n)
	}

	// unavailable output (no state / unknown output) fails with the documented text
	for _, l := range []store.TFOutputLink{
		{VarName: "x", SourceWorkspaceID: empty, OutputName: "o1"},
		{VarName: "x", SourceWorkspaceID: src, OutputName: "nope"},
	} {
		_ = srv.db.ReplaceTFOutputLinks(cons, []store.TFOutputLink{l})
		_, err = mgr.BuildRunConfig(ws)
		wsName := map[int64]string{empty: "empty", src: "src"}[l.SourceWorkspaceID]
		want := `linked output "` + l.OutputName + `" of workspace "` + wsName + `" is not available (apply that workspace first)`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
}
