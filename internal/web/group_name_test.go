package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestCleanGroupName(t *testing.T) {
	if g, err := cleanGroupName("  Web Tier \t"); err != nil || g != "Web Tier" {
		t.Fatalf("trim: %q, %v", g, err)
	}
	if _, err := cleanGroupName(strings.Repeat("é", 65)); err == nil {
		t.Fatal("65 runes must be rejected")
	}
	if _, err := cleanGroupName(strings.Repeat("é", 64)); err != nil {
		t.Fatalf("64 runes must pass: %v", err)
	}
	if _, err := cleanGroupName("a\nb"); err == nil {
		t.Fatal("control characters must be rejected")
	}
}

func TestAnsiblePlaybookGroupNameLifecycle(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", map[string]any{"name": "pb", "content": "- hosts: all\n", "group_name": " Web "})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Playbook struct {
			ID        int64  `json:"id"`
			GroupName string `json:"group_name"`
		} `json:"playbook"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Playbook.GroupName != "Web" {
		t.Fatalf("group_name = %q, want trimmed Web", created.Playbook.GroupName)
	}
	url := "/api/ansible/playbooks/" + strconv.FormatInt(created.Playbook.ID, 10)

	// An update without the key keeps the group; an explicit empty string clears it.
	doRequest(t, srv, auth, http.MethodPut, url, map[string]any{"name": "pb", "content": "- hosts: all\n"})
	if pb, _ := srv.db.GetAnsiblePlaybook(created.Playbook.ID); pb.GroupName != "Web" {
		t.Fatalf("group after update without key = %q", pb.GroupName)
	}
	doRequest(t, srv, auth, http.MethodPut, url, map[string]any{"name": "pb", "content": "- hosts: all\n", "group_name": ""})
	if pb, _ := srv.db.GetAnsiblePlaybook(created.Playbook.ID); pb.GroupName != "" {
		t.Fatalf("group after clearing = %q", pb.GroupName)
	}
	if rec := doRequest(t, srv, auth, http.MethodPut, url, map[string]any{"name": "pb", "content": "- hosts: all\n", "group_name": strings.Repeat("x", 65)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("overlong group = %d, want 400", rec.Code)
	}
}

func TestTerraformWorkspaceGroupNameAndFilter(t *testing.T) {
	srv, db, _, auth := setupTerraformTestServer(t)
	_ = db.SetTerraformEnabled(true)

	for _, c := range []struct{ name, group string }{{"a", "Network"}, {"b", "network"}, {"c", ""}} {
		rec := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", map[string]any{"name": c.name, "group_name": c.group, "files": map[string]string{"main.tf": ""}})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", c.name, rec.Code, rec.Body.String())
		}
	}
	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/workspaces?group=NETWORK", nil)
	var list struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Workspaces) != 2 || list.Workspaces[0]["group_name"] != "Network" {
		t.Fatalf("filtered list = %+v", list.Workspaces)
	}
	if rec := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", map[string]any{"name": "d", "group_name": "x\ny"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("control char group = %d, want 400", rec.Code)
	}
}
