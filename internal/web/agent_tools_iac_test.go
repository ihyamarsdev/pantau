package web

import (
	"encoding/json"
	"net/http"
	"strings"
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
