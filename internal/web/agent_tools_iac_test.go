package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

// iacApprove waits for the pending approval card of the tool call, reads its gate and decides it.
// It returns the gate and false when no card appeared.
func iacApprove(t *testing.T, srv *Server, approve bool) (int, bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for pendingCount(srv) == 0 {
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(5 * time.Millisecond)
	}
	srv.agent.mu.Lock()
	var id string
	gate := 0
	for k, a := range srv.agent.pending {
		id, gate = k, a.Gate
	}
	srv.agent.mu.Unlock()
	srv.handleAgentApprovalDecide(newRec(), reqJSON(http.MethodPost, "/api/agent/approvals/"+id, `{"approve":`+boolStr(approve)+`}`))
	return gate, true
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Every new tool must be registered with a schema, a description and a name the model can call.
func TestIacToolsRegistered(t *testing.T) {
	t.Parallel()
	want := []string{
		"ansible_playbook_action", "ansible_schedule_action", "ansible_resources",
		"tf_workspace_action", "tf_run_action", "tf_workspace_data", "tf_catalog",
		"kube_cluster_action", "repo_action", "dockerhub_search",
	}
	have := map[string]bool{}
	for _, tl := range agentTools {
		have[tl.Name] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Errorf("tool %s is not registered", n)
		}
	}
	for _, tl := range agentTools {
		if tl.InputSchema["type"] != "object" || tl.Description == "" {
			t.Errorf("tool %s has no object schema or no description", tl.Name)
		}
		if !strings.HasPrefix(tl.Name, "tf_") && !strings.HasPrefix(tl.Name, "ansible_") &&
			!strings.HasPrefix(tl.Name, "kube_") && !strings.HasPrefix(tl.Name, "repo_") &&
			!strings.HasPrefix(tl.Name, "dockerhub_") {
			continue
		}
		if tl.InputSchema["additionalProperties"] != false {
			t.Errorf("tool %s accepts unknown arguments", tl.Name)
		}
	}
}

// A wrong action or a missing argument is an error and changes nothing.
func TestIacToolValidation(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetSetting("agent_enabled", "1")
	_ = srv.db.SetAnsibleEnabled(true)

	if out, isErr := callTool(t, srv, "ansible_playbook_action", `{"action":"nope"}`); !isErr {
		t.Fatalf("unknown action must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "ansible_playbook_action", `{"action":"create"}`); !isErr || !strings.Contains(out, "name") {
		t.Fatalf("create without a name must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "ansible_playbook_action", `{"action":"create","name":"x","content":"y","bogus":1}`); !isErr {
		t.Fatalf("an unknown argument must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "tf_run_action", `{"action":"apply","run_id":1}`); !isErr {
		t.Fatalf("apply without a comment must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "ansible_resources", `{"action":"status","kind":"bogus"}`); !isErr {
		t.Fatalf("an unknown enum value must be an error: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("a rejected call must not ask the operator")
	}
}

// Every mutating action waits for an approval card, and a denial runs nothing.
func TestIacMutationsAskAndDenial(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetSetting("agent_enabled", "1")
	_ = srv.db.SetAnsibleEnabled(true)
	_ = srv.db.SetSetting("agent_approval", "auto") // even in auto mode a gate-3 action asks

	pid, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "web-fix", Content: "---\n- hosts: all\n  tasks: []\n"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "ansible_playbook_action", `{"action":"delete","playbook_id":`+itoa(pid)+`}`)
		done <- out
	}()
	gate, ok := iacApprove(t, srv, false)
	if !ok {
		t.Fatal("the delete never became pending")
	}
	if gate != gateInfra {
		t.Fatalf("a playbook delete must be gate 3, got %d", gate)
	}
	out := <-done
	if !strings.Contains(out, "denied") {
		t.Fatalf("a denied action must report the denial: %s", out)
	}
	if _, err := srv.db.GetAnsiblePlaybook(pid); err != nil {
		t.Fatalf("a denied delete must not remove the playbook: %v", err)
	}
}

// A gate-3 action is never auto-approved and never covered by a session grant.
func TestIacGate3NeverAutoOrGranted(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	db := srv.db
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetAnsibleEnabled(true)
	_ = db.SetSetting("agent_approval", "auto")

	srv.agent.mu.Lock()
	srv.agent.grants = map[int64]agentGrant{1: {HostID: 1, Gate: 3, Expires: time.Now().Add(time.Hour)}}
	srv.agent.mu.Unlock()

	done := make(chan bool, 1)
	go func() {
		out, _ := callTool(t, srv, "ansible_playbook_action", `{"action":"create","name":"p1","content":"---\n- hosts: all\n  tasks: []\n"}`)
		done <- strings.Contains(out, "denied")
	}()
	gate, ok := iacApprove(t, srv, false)
	if !ok {
		t.Fatal("a gate-3 action must wait for the operator, even in auto mode and with a grant")
	}
	if gate != gateInfra {
		t.Fatalf("expected gate 3, got %d", gate)
	}
	<-done
}

// The happy path of the Ansible playbook and schedule tools, through the real handlers.
func TestIacAnsiblePlaybookAndSchedule(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetSetting("agent_enabled", "1")
	_ = srv.db.SetAnsibleEnabled(true)

	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "ansible_playbook_action", `{"action":"create","name":"deploy","content":"---\n- hosts: all\n  tasks: []\n"}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the create never became pending")
	}
	out := <-done
	if !strings.Contains(out, "deploy") {
		t.Fatalf("the playbook was not created: %s", out)
	}
	list, err := srv.db.ListAnsiblePlaybooks()
	if err != nil {
		t.Fatal(err)
	}
	var pb *store.AnsiblePlaybook
	for i := range list {
		if list[i].Name == "deploy" {
			pb = &list[i]
		}
	}
	if pb == nil {
		t.Fatal("the playbook is not in the store")
	}

	// format is a read-only check: it must not ask.
	fmtOut, isErr := callTool(t, srv, "ansible_playbook_action", `{"action":"format","content":"---\n- hosts: all\n  tasks: []\n"}`)
	if isErr {
		t.Fatalf("format failed: %s", fmtOut)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("format must not ask the operator")
	}

	// A schedule: create, list, then delete.
	schedDone := make(chan string, 1)
	go func() {
		o, _ := callTool(t, srv, "ansible_schedule_action", `{"action":"create","playbook_id":`+itoa(pb.ID)+`,"name":"nightly","cron":"0 2 * * *","mode":"syntax"}`)
		schedDone <- o
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the schedule create never became pending")
	}
	if out := <-schedDone; !strings.Contains(out, "nightly") {
		t.Fatalf("the schedule was not created: %s", out)
	}

	schedList, isErr := callTool(t, srv, "ansible_schedule_action", `{"action":"list"}`)
	if isErr || !strings.Contains(schedList, "nightly") {
		t.Fatalf("schedule list: %s", schedList)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("listing the schedules must not ask the operator")
	}

	// A run_now of a syntax schedule asks and starts a run.
	runDone := make(chan string, 1)
	go func() {
		o, _ := callTool(t, srv, "ansible_schedule_action", `{"action":"run_now","schedule_id":1}`)
		runDone <- o
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("run_now never became pending")
	}
	if out := <-runDone; !strings.Contains(out, "id") {
		t.Fatalf("run_now: %s", out)
	}
}

// The Terraform workspace tools: create, lock, unlock and the refusal of a locked workspace.
func TestIacTerraformWorkspace(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	createDone := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "tf_workspace_action", `{"action":"create","name":"prod-ws","environment":"staging","files":{"main.tf":"variable \"x\" {}\n"}}`)
		createDone <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the workspace create never became pending")
	}
	out := <-createDone
	if !strings.Contains(out, "prod-ws") {
		t.Fatalf("the workspace was not created: %s", out)
	}
	wsID := dbWorkspaceID(t, srv, "prod-ws")

	// auto_apply is refused for a production workspace, exactly as in the UI.
	prodDone := make(chan string, 1)
	go func() {
		o, _ := callTool(t, srv, "tf_workspace_action", `{"action":"update","workspace_id":`+itoa(wsID)+`,"environment":"production","auto_apply":true}`)
		prodDone <- o
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the update never became pending")
	}
	if out := <-prodDone; !strings.Contains(out, "production") {
		t.Fatalf("the production refusal must reach the agent: %s", out)
	}

	// lock and unlock are asked for and reported.
	lockDone := make(chan string, 1)
	go func() {
		o, _ := callTool(t, srv, "tf_workspace_action", `{"action":"lock","workspace_id":`+itoa(wsID)+`,"reason":"maintenance"}`)
		lockDone <- o
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the lock never became pending")
	}
	if out := <-lockDone; !strings.Contains(out, "locked") {
		t.Fatalf("lock: %s", out)
	}

	// A locked workspace refuses a plan, and the agent is not asked for it.
	planOut, isErr := callTool(t, srv, "tf_run_action", `{"action":"plan","workspace_id":`+itoa(wsID)+`}`)
	if !isErr || !strings.Contains(planOut, "locked") {
		t.Fatalf("a plan on a locked workspace must be refused: %s", planOut)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("a refused plan must not ask the operator")
	}

	unlockDone := make(chan string, 1)
	go func() {
		o, _ := callTool(t, srv, "tf_workspace_action", `{"action":"unlock","workspace_id":`+itoa(wsID)+`}`)
		unlockDone <- o
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the unlock never became pending")
	}
	<-unlockDone
}

// dbWorkspaceID finds a workspace id by name in the store.
func dbWorkspaceID(t *testing.T, srv *Server, name string) int64 {
	t.Helper()
	list, err := srv.db.ListTFWorkspaces("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ws := range list {
		if ws.Name == name {
			return ws.ID
		}
	}
	t.Fatalf("workspace %q not found", name)
	return 0
}

// The Terraform state read tool returns the workspace state without a secret.
func TestIacTerraformStateRead(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "prod", Binary: "terraform", Source: "inline", Files: `{"main.tf":"variable \"x\" {}\n"}`})
	if err != nil {
		t.Fatal(err)
	}
	out, isErr := callTool(t, srv, "tf_workspace_data", `{"workspace_id":`+itoa(wsID)+`,"action":"state"}`)
	if isErr {
		t.Fatalf("state read: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("reading the state must not ask the operator")
	}
	if !strings.Contains(out, "serial") {
		t.Fatalf("state: %s", out)
	}
}

// A sensitive variable value never comes back to the model.
func TestIacNoSecretInResults(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "prod", Binary: "terraform", Source: "inline", Files: `{"main.tf":"variable \"x\" {}\ndb_password = \"hunter2\"\n"}`})
	if err != nil {
		t.Fatal(err)
	}
	// A sensitive variable, stored like the UI does.
	if _, err := db.CreateTFVariable(&store.TFVariable{WorkspaceID: wsID, Key: "db_password", Value: "hunter2", Category: "terraform", Sensitive: true}); err != nil {
		t.Fatal(err)
	}
	out, isErr := callTool(t, srv, "tf_workspace_data", `{"workspace_id":`+itoa(wsID)+`,"action":"variables"}`)
	if isErr {
		t.Fatalf("variables read: %s", out)
	}
	if strings.Contains(out, "hunter2") {
		t.Fatalf("a sensitive variable value leaked: %s", out)
	}
	if !strings.Contains(out, "db_password") {
		t.Fatalf("the variable name must stay visible: %s", out)
	}
}

// The Kubernetes cluster tool: rename and delete of a registered cluster, and the refusal of an unknown one.
func TestIacKubeClusterAction(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	cid, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod", KubeMode: "full", Provider: "generic"})
	if err != nil {
		t.Fatal(err)
	}

	if out, isErr := callTool(t, srv, "kube_cluster_action", `{"action":"rename","cluster_id":999,"name":"x"}`); !isErr {
		t.Fatalf("an unknown cluster must be an error: %s", out)
	}

	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "kube_cluster_action", `{"action":"rename","cluster_id":`+itoa(cid)+`,"name":"prod-eu"}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the rename never became pending")
	}
	if out := <-done; !strings.Contains(out, "prod-eu") {
		t.Fatalf("rename: %s", out)
	}
	c, err := db.GetKubeCluster(cid)
	if err != nil || c == nil || c.Name != "prod-eu" {
		t.Fatalf("the cluster was not renamed: %v", err)
	}

	delDone := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "kube_cluster_action", `{"action":"delete","cluster_id":`+itoa(cid)+`}`)
		delDone <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the delete never became pending")
	}
	<-delDone
	if c, _ := db.GetKubeCluster(cid); c != nil {
		t.Fatal("the cluster was not deleted")
	}
}

// The repository tool refuses a token (a secret never crosses the model) and clones with an SSH key.
func TestIacRepoAction(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")

	if out, isErr := callTool(t, srv, "repo_action", `{"action":"clone","url":"https://example.com/x.git","auth":"token"}`); !isErr {
		t.Fatalf("a token clone must be refused: %s", out)
	}
	if out, isErr := callTool(t, srv, "repo_action", `{"action":"clone","url":"git@example.com:x/y.git","auth":"ssh"}`); !isErr || !strings.Contains(out, "key_id") {
		t.Fatalf("an ssh clone without a key id must be refused: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("a refused clone must not ask the operator")
	}

	// A delete of a repository that does not exist changes nothing.
	if out, isErr := callTool(t, srv, "repo_action", `{"action":"delete","repo_id":42}`); !isErr {
		t.Fatalf("an unknown repository must be an error: %s", out)
	}
}

// The Ansible resources tool reads the status and the config, and writes an asset.
func TestIacAnsibleResources(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetSetting("agent_enabled", "1")
	_ = srv.db.SetAnsibleEnabled(true)

	out, isErr := callTool(t, srv, "ansible_resources", `{"action":"status"}`)
	if isErr || !strings.Contains(out, "available") {
		t.Fatalf("status: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("reading the status must not ask the operator")
	}

	done := make(chan string, 1)
	go func() {
		o, _ := callTool(t, srv, "ansible_resources", `{"action":"write_asset","kind":"template","name":"nginx/site.conf.j2","content":"server { listen 80; }"}`)
		done <- o
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the asset write never became pending")
	}
	if out := <-done; !strings.Contains(out, "nginx/site.conf.j2") {
		t.Fatalf("the asset was not stored: %s", out)
	}

	list, isErr := callTool(t, srv, "ansible_resources", `{"action":"assets"}`)
	if isErr || !strings.Contains(list, "nginx/site.conf.j2") {
		t.Fatalf("asset list: %s", list)
	}
}

// The Terraform catalog tool lists the policies and creates one.
func TestIacTerraformCatalog(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "tf_catalog", `{"action":"create_policy","name":"no-public-bucket","kind":"deny_resource_types","enforcement":"hard","params":{"types":["aws_s3_bucket"]}}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the policy create never became pending")
	}
	if out := <-done; !strings.Contains(out, "no-public-bucket") {
		t.Fatalf("the policy was not created: %s", out)
	}
	list, isErr := callTool(t, srv, "tf_catalog", `{"action":"policies"}`)
	if isErr || !strings.Contains(list, "no-public-bucket") {
		t.Fatalf("policy list: %s", list)
	}
}

// create stores every metadata field in its one call, and the answer echoes them, so no follow-up update is needed.
func TestIacTFWorkspaceCreateStoresAndEchoesMetadata(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	args := `{"action":"create","name":"meta-ws","description":"simple gcp dev","environment":"dev","group_name":"gcp","tags":["gcp","dev"],"files":{"main.tf":"variable \"x\" {}\n"}}`
	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "tf_workspace_action", args)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the create never became pending")
	}
	out := <-done
	for _, want := range []string{"simple gcp dev", `"environment":"dev"`, `"group_name":"gcp"`, `"dev"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("create answer lacks %q: %s", want, out)
		}
	}
	ws, err := db.GetTFWorkspace(dbWorkspaceID(t, srv, "meta-ws"))
	if err != nil {
		t.Fatal(err)
	}
	if ws.Description != "simple gcp dev" || ws.Environment != "dev" || ws.GroupName != "gcp" || !strings.Contains(ws.Tags, `"dev"`) {
		t.Fatalf("metadata not stored: %+v", ws)
	}
}

// A refused scaffold never reaches the operator: traversal, absolute paths, plaintext secrets and the disabled
// gate are all refused before the approval gate.
func TestIacTFWorkspaceScaffoldRefusesBeforeApproval(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		disabled bool
		args     string
		want     string
	}{
		{"traversal", false, `{"name":"x","files":{"../evil.tf":"output \"a\" {}"}}`, "invalid file name"},
		{"absolute", false, `{"name":"x","files":{"/etc/evil.tf":"output \"a\" {}"}}`, "invalid file name"},
		{"plaintext secret", false, `{"name":"x","files":{"main.tf":"output \"a\" {}"},"variables":[{"key":"token","value":"s3cr3t","sensitive":true}]}`, "reference"},
		{"gate disabled", true, `{"name":"x","files":{"main.tf":"output \"a\" {}"}}`, "disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, db, _, _ := setupTerraformTestServer(t)
			_ = db.SetSetting("agent_enabled", "1")
			_ = db.SetTerraformEnabled(!tc.disabled)
			out, isErr := callTool(t, srv, "tf_workspace_scaffold", tc.args)
			if !isErr || !strings.Contains(out, tc.want) {
				t.Fatalf("want a refusal mentioning %q, got %q (error %v)", tc.want, out, isErr)
			}
			if n := pendingCount(srv); n != 0 {
				t.Fatalf("a refused scaffold asked the operator (%d pending)", n)
			}
		})
	}
}

// fakeTerraform puts a stub terraform on PATH: init succeeds and validate prints a clean result. It sets the
// environment, so the test that uses it must not run in parallel.
func fakeTerraform(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  validate) echo '{\"valid\":true,\"error_count\":0,\"warning_count\":0,\"diagnostics\":[]}' ;;\n  *) exit 0 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "terraform"), []byte(script), 0o700); err != nil { // #nosec G306 -- a test stub that must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// A scaffold writes four files, two variables and runs validation for one approval.
func TestIacTFWorkspaceScaffoldOneApproval(t *testing.T) {
	fakeTerraform(t)
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	args := `{"name":"gcp-dev","environment":"dev","description":"simple gcp workspace",` +
		`"files":{"versions.tf":"terraform {}\n","variables.tf":"variable \"region\" {}\n",` +
		`"main.tf":"resource \"terraform_data\" \"x\" {}\n","outputs.tf":"output \"id\" { value = 1 }\n"},` +
		`"variables":[{"key":"region","value":"asia-southeast2"},{"key":"gcp_token","value":"{{ vault.GCP_TOKEN }}","sensitive":true}],` +
		`"validate":true}`
	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "tf_workspace_scaffold", args)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the scaffold never became pending")
	}
	out := <-done
	if strings.Contains(out, "validate_error") || !strings.Contains(out, "outputs.tf") {
		t.Fatalf("scaffold answer: %s", out)
	}
	if n := pendingCount(srv); n != 0 {
		t.Fatalf("the scaffold asked more than once (%d still pending)", n)
	}
	ws, err := db.GetTFWorkspace(dbWorkspaceID(t, srv, "gcp-dev"))
	if err != nil {
		t.Fatal(err)
	}
	if ws.Description != "simple gcp workspace" || ws.Environment != "dev" {
		t.Fatalf("metadata not stored: %+v", ws)
	}
	if !strings.Contains(ws.Files, "outputs.tf") || !strings.Contains(ws.Files, "versions.tf") {
		t.Fatalf("files not stored: %s", ws.Files)
	}
	vars, err := db.ListTFVariables(ws.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 2 {
		t.Fatalf("two variables expected, got %d", len(vars))
	}
}

// The catalog index comes back in one answer, is compact, asks nobody, and the detail mode returns one template.
func TestIacTerraformCatalogTemplateIndex(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	out, isErr := callTool(t, srv, "tf_catalog", `{"action":"templates"}`)
	if isErr {
		t.Fatalf("index failed: %s", out)
	}
	if len(out) > 3*1024 {
		t.Fatalf("index is %d bytes, want under 3 KB", len(out))
	}
	var idx struct {
		Count     int                      `json:"count"`
		Templates []map[string]interface{} `json:"templates"`
	}
	if err := json.Unmarshal([]byte(out), &idx); err != nil {
		t.Fatalf("index is not JSON: %v", err)
	}
	if idx.Count == 0 || idx.Count != len(idx.Templates) {
		t.Fatalf("bad count: %d for %d entries", idx.Count, len(idx.Templates))
	}
	for _, key := range []string{"id", "name", "provider", "category", "summary", "tags"} {
		if _, ok := idx.Templates[0][key]; !ok {
			t.Fatalf("index entry lacks %q: %v", key, idx.Templates[0])
		}
	}
	if strings.Contains(out, `"files"`) {
		t.Fatal("the index must not carry template files")
	}
	detail, isErr := callTool(t, srv, "tf_catalog", `{"action":"templates","template_id":"hello-world"}`)
	if isErr || !strings.Contains(detail, `"files"`) {
		t.Fatalf("detail of one template: %s", detail)
	}
	if n := pendingCount(srv); n != 0 {
		t.Fatalf("reads asked the operator (%d pending)", n)
	}
}

// The Docker Hub tool and the update check are reads: they never ask the operator.
func TestIacDockerHubAndUpdateCheck(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	_ = srv.db.SetSetting("agent_enabled", "1")

	out, isErr := callTool(t, srv, "dockerhub_search", `{"action":"update"}`)
	if isErr {
		t.Fatalf("the update check failed: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("the update check must not ask the operator")
	}
	if out, isErr := callTool(t, srv, "dockerhub_search", `{"action":"search","query":"nginx"}`); isErr {
		t.Fatalf("the Docker Hub search failed: %s", out)
	}
}

// The tool result of a catalog list is valid JSON, so the model can read it.
func TestIacToolResultIsJSON(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	if _, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "prod", Binary: "terraform", Source: "inline"}); err != nil {
		t.Fatal(err)
	}
	out, isErr := callTool(t, srv, "tf_catalog", `{"action":"policies"}`)
	if isErr {
		t.Fatalf("policy list: %s", out)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("the result is not JSON: %v (%s)", err, out)
	}
}

// The new Terraform actions ask the operator and refuse a bad argument before anything runs.
func TestIacTerraformDriftAndStateImport(t *testing.T) {
	t.Parallel()
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "prod", Binary: "terraform", Source: "inline", Files: `{"main.tf":"variable \"x\" {}\n"}`})
	if err != nil {
		t.Fatal(err)
	}

	// state_import without a state file is refused, and nothing is asked.
	if out, isErr := callTool(t, srv, "tf_workspace_data", `{"workspace_id":`+itoa(wsID)+`,"action":"state_import"}`); !isErr {
		t.Fatalf("state_import without a state must be an error: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("a refused call must not ask the operator")
	}

	// A drift check asks.
	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "tf_workspace_action", `{"action":"drift_check","workspace_id":`+itoa(wsID)+`}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the drift check never became pending")
	}
	<-done
}

// The ad-hoc Ansible command asks the operator and refuses an empty target selection.
func TestIacAnsibleAdhoc(t *testing.T) {
	t.Parallel()
	if _, _, err := ansible.Available(); err != nil {
		t.Skip("ansible is required: the ad-hoc tool answers 409 without it")
	}
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetAnsibleEnabled(true)

	// Without a module the tool reads the ad-hoc run history: no approval.
	out, isErr := callTool(t, srv, "ansible_resources", `{"action":"adhoc"}`)
	if isErr {
		t.Fatalf("the ad-hoc run list failed: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("reading the ad-hoc runs must not ask the operator")
	}

	// A command with no target is refused by the handler, after the approval.
	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "ansible_resources", `{"action":"adhoc","module":"ping","args":"-c 2"}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("the ad-hoc run never became pending")
	}
	if out := <-done; !strings.Contains(out, "ERROR") {
		t.Fatalf("an ad-hoc run without a target must fail: %s", out)
	}
	_ = db
}

func TestTFCatalogBatchSearchAndInspect(t *testing.T) {
	t.Parallel()
	var hits int32
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		switch r.URL.Path {
		case "/v1/modules/search":
			q := r.URL.Query().Get("q")
			switch q {
			case "gcp network":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/network/google/10.0.0","namespace":"terraform-google-modules","name":"network","provider":"google","version":"10.0.0"}]}`))
			case "gcp vm":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/vm/google/15.0.0","namespace":"terraform-google-modules","name":"vm","provider":"google","version":"15.0.0"}]}`))
			case "gcp storage":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/cloud-storage/google/9.0.0","namespace":"terraform-google-modules","name":"cloud-storage","provider":"google","version":"9.0.0"}]}`))
			default:
				_, _ = w.Write([]byte(`{"modules":[]}`))
			}
		case "/v1/modules/terraform-google-modules/vm/google":
			_, _ = w.Write([]byte(`{"id":"terraform-google-modules/vm/google/15.0.0","namespace":"terraform-google-modules","name":"vm","provider":"google","version":"15.0.0","description":"VM modules","root":{"inputs":[],"outputs":[]},"submodules":[{"path":"modules/compute_instance","inputs":[{"name":"subnetwork","type":"string","required":true},{"name":"instance_template","type":"string","required":true},{"name":"hostname","type":"string","default":"\"vm\"","required":false}],"outputs":[{"name":"instances_self_links"}]}]}`))
		case "/v1/modules/terraform-google-modules/cloud-storage/google":
			_, _ = w.Write([]byte(`{"id":"terraform-google-modules/cloud-storage/google/9.0.0","namespace":"terraform-google-modules","name":"cloud-storage","provider":"google","version":"9.0.0","description":"GCS buckets","root":{"inputs":[{"name":"name","type":"string","required":true},{"name":"project_id","type":"string","required":true}],"outputs":[{"name":"bucket"}]},"submodules":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(reg.Close)

	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	_ = db.SetSetting("tf_registry_url", reg.URL)

	// 1. Batch search in 1 call
	out, isErr := callTool(t, srv, "tf_catalog", `{"action":"registry_modules","provider":"google","queries":["gcp network","gcp vm","gcp storage"]}`)
	if isErr {
		t.Fatalf("batch search failed: %s", out)
	}
	var searchResp struct {
		Count   int `json:"count"`
		Modules []struct {
			Name string `json:"name"`
		} `json:"modules"`
	}
	if err := json.Unmarshal([]byte(out), &searchResp); err != nil || searchResp.Count != 3 {
		t.Fatalf("unexpected batch search response: err=%v out=%s", err, out)
	}

	// 2. Batch inspect in 1 call
	out, isErr = callTool(t, srv, "tf_catalog", `{"action":"inspect","module_ids":["terraform-google-modules/vm/google","terraform-google-modules/cloud-storage/google"]}`)
	if isErr {
		t.Fatalf("batch inspect failed: %s", out)
	}
	if !strings.Contains(out, "terraform-google-modules/vm/google//modules/compute_instance") || !strings.Contains(out, "cloud-storage") {
		t.Fatalf("missing expected submodule/module in inspect output: %s", out)
	}

	// 3. Anti-SSRF / path traversal / >5 module_ids rejected before hitting registry
	hitsBefore := atomic.LoadInt32(&hits)
	for _, badArgs := range []string{
		`{"action":"inspect","module_ids":["../etc/passwd"]}`,
		`{"action":"inspect","module_ids":["-flag/vm/google"]}`,
		`{"action":"inspect","module_ids":["https://evil.com/vm/google"]}`,
		`{"action":"inspect","module_ids":["a/b/c","a/b/d","a/b/e","a/b/f","a/b/g","a/b/h"]}`,
	} {
		if _, isErr := callTool(t, srv, "tf_catalog", badArgs); !isErr {
			t.Fatalf("expected error for %s", badArgs)
		}
	}
	if atomic.LoadInt32(&hits) != hitsBefore {
		t.Fatalf("invalid inspect args must not hit upstream registry")
	}
}

func TestTFWorkspaceScaffold_PreservesVarsAndReturnsSummary(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	// 1. Create workspace with initial variables (including a sensitive vault ref).
	done := make(chan string, 1)
	go func() {
		out, _ := callTool(t, srv, "tf_workspace_scaffold", `{
			"name": "torcons",
			"files": {"main.tf": "variable \"project_id\" {}\n"},
			"variables": [
				{"key": "project_id", "value": "my-gcp-proj", "category": "terraform", "sensitive": false},
				{"key": "gcp_creds", "value": "{{ vault.GCP_CREDS }}", "category": "env", "sensitive": true}
			]
		}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("initial scaffold never became pending")
	}
	firstOut := <-done
	var firstResp struct {
		WorkspaceID      int64 `json:"workspace_id"`
		VariablesSummary struct {
			Count     int `json:"count"`
			Variables []struct {
				Key       string `json:"key"`
				Sensitive bool   `json:"sensitive"`
			} `json:"variables"`
		} `json:"variables_summary"`
		FilesWritten []struct {
			Path string `json:"path"`
			Size int    `json:"size"`
		} `json:"files_written"`
	}
	if err := json.Unmarshal([]byte(firstOut), &firstResp); err != nil || firstResp.WorkspaceID < 1 {
		t.Fatalf("unexpected first scaffold output: err=%v out=%s", err, firstOut)
	}
	if firstResp.VariablesSummary.Count != 2 || len(firstResp.FilesWritten) != 1 {
		t.Fatalf("unexpected summary: %+v", firstResp)
	}
	if strings.Contains(firstOut, "my-gcp-proj") {
		t.Fatalf("variables_summary must not leak variable values: %s", firstOut)
	}

	// 2. Rewrite workspace files without passing variables -> existing variables MUST be preserved.
	go func() {
		out, _ := callTool(t, srv, "tf_workspace_scaffold", `{
			"workspace_id": `+itoa(firstResp.WorkspaceID)+`,
			"files": {"main.tf": "variable \"project_id\" {}\n", "outputs.tf": "output \"ok\" { value = true }\n"}
		}`)
		done <- out
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("second scaffold never became pending")
	}
	secondOut := <-done
	var secondResp struct {
		VariablesSummary struct {
			Count int `json:"count"`
		} `json:"variables_summary"`
		FilesWritten []struct {
			Path string `json:"path"`
		} `json:"files_written"`
	}
	if err := json.Unmarshal([]byte(secondOut), &secondResp); err != nil {
		t.Fatalf("unmarshal second scaffold: %v (%s)", err, secondOut)
	}
	if secondResp.VariablesSummary.Count != 2 || len(secondResp.FilesWritten) != 2 {
		t.Fatalf("expected 2 preserved variables and 2 written files, got: %s", secondOut)
	}
}

func TestTFRuns_ErroredIncludesErrorExcerpt(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "err-ws", Source: "inline", Binary: "terraform", Files: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "errored", Message: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	var logLines []string
	for i := 0; i < 60; i++ {
		logLines = append(logLines, "Initializing module "+itoa(int64(i))+"...")
	}
	logLines = append(logLines, "╷", "│ Error: Invalid value for input variable", "│   on main.tf line 9: subnetwork must not be empty", "╵")
	exitCode := 1
	if err := db.FinishTFRun(runID, "errored", &exitCode, strings.Join(logLines, "\n")); err != nil {
		t.Fatal(err)
	}

	out, isErr := callTool(t, srv, "tf_runs", `{"run_id":`+itoa(runID)+`}`)
	if isErr {
		t.Fatalf("tf_runs failed: %s", out)
	}
	var resp struct {
		ErrorExcerpt string `json:"error_excerpt"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal tf_runs: %v (%s)", err, out)
	}
	if !strings.Contains(resp.ErrorExcerpt, "Error: Invalid value for input variable") {
		t.Fatalf("expected ErrorExcerpt to contain the Error block, got: %q", resp.ErrorExcerpt)
	}
}

// End-to-end simulation of the "torcons" 3-module GCP workspace flow:
// fake LLM + mock Registry + fake terraform CLI -> <= 4 tool calls, inspects vm/google submodule,
// and passes validate in a single tf_workspace_scaffold call.
func TestAgentTerraformTorconsEndToEndSimulation(t *testing.T) {
	binDir := t.TempDir()
	fakeTF := filepath.Join(binDir, "terraform")
	if err := os.WriteFile(fakeTF, []byte(`#!/bin/sh
case "$1" in
  version) echo "Terraform v1.11.0"; exit 0 ;;
  init)
    if [ -z "$TF_PLUGIN_CACHE_DIR" ] || [ "$TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE" != "true" ]; then
      echo "missing plugin cache env" >&2; exit 1
    fi
    echo "Initializing modules..."
    exit 0
    ;;
  validate)
    echo '{"valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}'
    exit 0
    ;;
esac
exit 0
`), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/modules/search":
			switch r.URL.Query().Get("q") {
			case "gcp vpc":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/network/google/10.0.0","namespace":"terraform-google-modules","name":"network","provider":"google","version":"10.0.0"}]}`))
			case "gcp compute":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/vm/google/15.0.0","namespace":"terraform-google-modules","name":"vm","provider":"google","version":"15.0.0"}]}`))
			case "gcp storage":
				_, _ = w.Write([]byte(`{"modules":[{"id":"terraform-google-modules/cloud-storage/google/9.0.0","namespace":"terraform-google-modules","name":"cloud-storage","provider":"google","version":"9.0.0"}]}`))
			default:
				_, _ = w.Write([]byte(`{"modules":[]}`))
			}
		case "/v1/modules/terraform-google-modules/network/google":
			_, _ = w.Write([]byte(`{"id":"terraform-google-modules/network/google/10.0.0","namespace":"terraform-google-modules","name":"network","provider":"google","version":"10.0.0","root":{"inputs":[{"name":"project_id","type":"string","required":true},{"name":"network_name","type":"string","required":true}],"outputs":[{"name":"subnets_self_links"}]},"submodules":[]}`))
		case "/v1/modules/terraform-google-modules/vm/google":
			_, _ = w.Write([]byte(`{"id":"terraform-google-modules/vm/google/15.0.0","namespace":"terraform-google-modules","name":"vm","provider":"google","version":"15.0.0","root":{"inputs":[],"outputs":[]},"submodules":[{"path":"modules/compute_instance","inputs":[{"name":"subnetwork","type":"string","required":true},{"name":"instance_template","type":"string","required":true}],"outputs":[{"name":"instances_self_links"}]}]}`))
		case "/v1/modules/terraform-google-modules/cloud-storage/google":
			_, _ = w.Write([]byte(`{"id":"terraform-google-modules/cloud-storage/google/9.0.0","namespace":"terraform-google-modules","name":"cloud-storage","provider":"google","version":"9.0.0","root":{"inputs":[{"name":"name","type":"string","required":true},{"name":"project_id","type":"string","required":true}],"outputs":[{"name":"bucket"}]},"submodules":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(reg.Close)

	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.tfMgr = NewTerraformRunManager(db, t.TempDir(), nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	_ = db.SetSetting("agent_llm_provider", "openai")
	_ = db.SetSetting("agent_llm_model", "test-model")
	_ = db.SetSetting("agent_llm_api_key", "sk-test")
	_ = db.SetSetting("tf_registry_url", reg.URL)

	f := &fakeLLM{results: []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "tf_catalog", Args: `{"action":"registry_modules","provider":"google","queries":["gcp vpc","gcp compute","gcp storage"]}`}}},
		{ToolCalls: []llmToolCall{{ID: "c2", Name: "tf_catalog", Args: `{"action":"inspect","module_ids":["terraform-google-modules/network/google","terraform-google-modules/vm/google","terraform-google-modules/cloud-storage/google"]}`}}},
		{ToolCalls: []llmToolCall{{ID: "c3", Name: "tf_workspace_scaffold", Args: `{"name":"torcons","files":{"main.tf":"module \"vpc\" { source = \"terraform-google-modules/network/google\" }\nmodule \"vm\" { source = \"terraform-google-modules/vm/google//modules/compute_instance\" }\nmodule \"gcs\" { source = \"terraform-google-modules/cloud-storage/google\" }\n"},"variables":[{"key":"project_id","value":"torcons-prod","category":"terraform","sensitive":false}],"validate":true}`}}},
		{Text: "Workspace torcons berhasil dibuat dan tervalidasi."},
	}}
	useFakeLLM(srv, f)

	done := make(chan int, 1)
	go func() {
		rec := chatPostPage(srv, "s1", "buatkan workspace torcons menggunakan provider gcp dan module compute, vpc, storage", "terraform")
		done <- rec.Code
	}()
	if _, ok := iacApprove(t, srv, true); !ok {
		t.Fatal("tf_workspace_scaffold never asked for approval")
	}
	if code := <-done; code != http.StatusOK {
		t.Fatalf("chat status = %d", code)
	}

	// Count total tool calls sent by LLM across all steps: must be <= 4 (here 3).
	totalCalls := 0
	var scaffoldToolResult string
	var sawComputeSubmodule bool
	for _, req := range f.calls {
		for _, m := range req.Messages {
			if m.Role == llmRoleTool {
				totalCalls++
				if strings.Contains(m.Text, "terraform-google-modules/vm/google//modules/compute_instance") {
					sawComputeSubmodule = true
				}
				if strings.Contains(m.Text, `"workspace_id"`) {
					scaffoldToolResult = m.Text
				}
			}
		}
	}
	// Note: f.calls[last] contains all 3 tool results; count unique tool calls from steps instead:
	totalCalls = 0
	for i := 0; i < len(f.calls)-1; i++ {
		// Each step in f.steps emitted 1 tool call; verify from f.calls[last] tool messages.
	}
	lastMsgs := f.calls[len(f.calls)-1].Messages
	for _, m := range lastMsgs {
		if m.Role == llmRoleTool {
			totalCalls++
		}
	}
	if totalCalls > 4 || totalCalls != 3 {
		t.Fatalf("expected <=4 (specifically 3) tool calls, got %d", totalCalls)
	}
	if !sawComputeSubmodule {
		t.Fatal("expected agent to inspect vm/google and observe modules/compute_instance before scaffolding")
	}
	if !strings.Contains(scaffoldToolResult, `"ok":true`) {
		t.Fatalf("expected tf_workspace_scaffold validation.ok=true, got: %s", scaffoldToolResult)
	}
}
