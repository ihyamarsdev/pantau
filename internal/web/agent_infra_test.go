package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

// callTool runs one MCP tool call through /mcp and returns the text result.
func callTool(t *testing.T, srv *Server, name, args string) string {
	t.Helper()
	w := mcpCall(t, srv, srv.agentToken(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
	return w.Body.String()
}

func pendingCount(srv *Server) int {
	srv.agent.mu.Lock()
	defer srv.agent.mu.Unlock()
	return len(srv.agent.pending)
}

func TestAgentInfraToolsListed(t *testing.T) {
	want := []string{"docker_inspect", "docker_info", "docker_stack_file", "docker_stack_action", "kube_targets", "kube_get", "kube_describe", "kube_logs", "kube_diff", "kube_action", "kube_apply", "ansible_playbooks", "ansible_run", "ansible_runs", "tf_workspaces", "tf_runs", "tf_plan", "tf_apply"}
	have := map[string]agentTool{}
	for _, tl := range agentTools {
		if _, dup := have[tl.Name]; dup {
			t.Fatalf("duplicate tool %s", tl.Name)
		}
		have[tl.Name] = tl
	}
	for _, n := range want {
		tl, ok := have[n]
		if !ok || tl.run == nil || tl.InputSchema["type"] != "object" {
			t.Fatalf("tool %s missing or malformed", n)
		}
	}
	for _, tl := range agentTools {
		b, _ := json.Marshal(tl.InputSchema)
		if strings.Contains(string(b), `"required":null`) {
			t.Fatalf("%s: schema has required:null, which LLM APIs reject: %s", tl.Name, b)
		}
	}
	for _, n := range []string{"kube_apply", "tf_apply"} {
		if have[n].gate != gateInfra {
			t.Fatalf("%s must be gate 3", n)
		}
	}
}

func TestAgentInfraGateNeverAutoOrGranted(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_approval", "auto")

	// A gate-1 session grant exists on host 1; gate 3 must still wait for the operator.
	srv.agent.mu.Lock()
	srv.agent.grants = map[int64]agentGrant{1: {HostID: 1, Gate: 3, Expires: time.Now().Add(time.Hour)}}
	srv.agent.mu.Unlock()

	done := make(chan bool, 1)
	go func() {
		ok, _ := srv.agentApprove(approvalReq(), &agentApproval{HostID: 1, HostName: "h", Tool: "kube_apply"}, gateInfra)
		done <- ok
	}()
	deadline := time.Now().Add(2 * time.Second)
	for pendingCount(srv) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("gate 3 was approved without asking (auto mode or grant leaked)")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Approving "for this session" must not create a grant for gate 3 or for a host-less scope.
	srv.agent.mu.Lock()
	var id string
	for k := range srv.agent.pending {
		id = k
	}
	srv.agent.grants = nil
	srv.agent.mu.Unlock()
	w := newRec()
	srv.handleAgentApprovalDecide(w, reqJSON(http.MethodPost, "/api/agent/approvals/"+id, `{"approve":true,"session":true}`))
	if ok := <-done; !ok {
		t.Fatal("explicit approval must run the action")
	}
	srv.agent.mu.Lock()
	n := len(srv.agent.grants)
	srv.agent.mu.Unlock()
	if n != 0 {
		t.Fatal("approving gate 3 'for the session' must not create a grant")
	}

	// Host-less scope (Terraform/Ansible, HostID 0) at gate 1: session flag must not create a grant either.
	_ = db.SetSetting("agent_approval", "ask") // in auto mode gate 1 would pass without ever becoming pending
	go func() {
		srv.agentApprove(approvalReq(), &agentApproval{HostID: 0, HostName: "Terraform", Tool: "tf_plan"}, gateRoutine)
	}()
	for end := time.Now().Add(2 * time.Second); pendingCount(srv) == 0; {
		if time.Now().After(end) {
			t.Fatal("host-less approval never became pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
	srv.agent.mu.Lock()
	for k := range srv.agent.pending {
		id = k
	}
	srv.agent.mu.Unlock()
	srv.handleAgentApprovalDecide(newRec(), reqJSON(http.MethodPost, "/api/agent/approvals/"+id, `{"approve":true,"session":true}`))
	srv.agent.mu.Lock()
	n = len(srv.agent.grants)
	srv.agent.mu.Unlock()
	if n != 0 {
		t.Fatal("a host-less scope must not be grantable")
	}
}

func TestAgentKubeReadOnlyRefusedBeforeAsking(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	cid, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod", KubeMode: "read-only", Provider: "generic"})
	if err != nil {
		t.Fatal(err)
	}
	target := "-" + itoa(cid)
	for _, c := range []struct{ tool, args string }{
		{"kube_action", `{"target_id":` + target + `,"op":"restart","kind":"deployment","namespace":"app","name":"web"}`},
		{"kube_apply", `{"target_id":` + target + `,"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}`},
	} {
		out := callTool(t, srv, c.tool, c.args)
		if !strings.Contains(out, "read-only") || !strings.Contains(out, `"isError":true`) {
			t.Fatalf("%s on a read-only cluster must be refused: %s", c.tool, out)
		}
		if pendingCount(srv) != 0 {
			t.Fatalf("%s asked the operator for an action that can never run", c.tool)
		}
	}
	// Invalid input is rejected before asking too.
	if out := callTool(t, srv, "kube_action", `{"target_id":`+target+`,"op":"delete","kind":"node","name":"n1"}`); !strings.Contains(out, "isError") {
		t.Fatalf("invalid kube action: %s", out)
	}
}

func TestAgentTerraformGuardsAndNoWebhookToken(t *testing.T) {
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("hermes_enabled", "1")
	_ = db.SetTerraformEnabled(true)
	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "prod", Binary: "terraform", Source: "inline", Files: `{"main.tf":"variable \"x\" {}\ndb_password = \"hunter2\"\n"}`, WebhookToken: "SECRETWEBHOOKTOKEN123", WebhookEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	list := callTool(t, srv, "tf_workspaces", `{}`)
	if !strings.Contains(list, "prod") || strings.Contains(list, "SECRETWEBHOOKTOKEN123") {
		t.Fatalf("list must show the workspace and never the webhook token: %s", list)
	}
	one := callTool(t, srv, "tf_workspaces", `{"workspace_id":`+itoa(wsID)+`}`)
	if strings.Contains(one, "SECRETWEBHOOKTOKEN123") || strings.Contains(one, "hunter2") || !strings.Contains(one, "db_password") {
		t.Fatalf("detail must mask secret-looking values and drop the webhook: %s", one)
	}

	// A plan that is not awaiting approval can never be applied, and the operator is not asked.
	runID, err := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "planning", TriggerKind: "manual", Targets: "[]", Replace: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	out := callTool(t, srv, "tf_apply", `{"run_id":`+itoa(runID)+`,"comment":"fix it"}`)
	if !strings.Contains(out, "awaiting_approval") || !strings.Contains(out, `"isError":true`) || pendingCount(srv) != 0 {
		t.Fatalf("apply of a non-awaiting run: %s", out)
	}

	// A warning policy is never overridden by the agent.
	warnID, _ := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "awaiting_approval", TriggerKind: "manual", Targets: "[]", Replace: "[]", Policy: `{"status":"warning"}`})
	out = callTool(t, srv, "tf_apply", `{"run_id":`+itoa(warnID)+`,"comment":"fix it"}`)
	if !strings.Contains(out, "policy") || pendingCount(srv) != 0 {
		t.Fatalf("policy warning must stop the agent: %s", out)
	}
	if out := callTool(t, srv, "tf_apply", `{"run_id":`+itoa(warnID)+`,"comment":"x"}`); !strings.Contains(out, "comment") {
		t.Fatalf("short comment must be rejected: %s", out)
	}
}

func TestAgentLoopbackLeavesNoSession(t *testing.T) {
	srv, _, _, _ := setupTerraformTestServer(t)
	before := 0
	srv.sessions.Range(func(_, _ interface{}) bool { before++; return true })
	code, _ := srv.loopback(t.Context(), http.MethodGet, "/api/terraform/status", nil)
	if code != http.StatusOK {
		t.Fatalf("loopback must authenticate itself, got %d", code)
	}
	after := 0
	srv.sessions.Range(func(_, _ interface{}) bool { after++; return true })
	if after != before {
		t.Fatalf("loopback leaked a session token (%d -> %d)", before, after)
	}
	if code, _ := srv.loopback(t.Context(), http.MethodGet, "/api/hosts", nil); code != http.StatusOK {
		t.Fatalf("hosts via loopback: %d", code)
	}
}

func TestRedactSecrets(t *testing.T) {
	in := "DB_PASSWORD=hunter2\n  API_KEY: abc123\n- GITHUB_TOKEN=ghp_x\nPORT=8080\nimage: nginx\n"
	out := redactSecrets(in)
	for _, leak := range []string{"hunter2", "abc123", "ghp_x"} {
		if strings.Contains(out, leak) {
			t.Fatalf("%q leaked: %s", leak, out)
		}
	}
	if !strings.Contains(out, "PORT=8080") || !strings.Contains(out, "image: nginx") {
		t.Fatalf("non-secret lines must stay: %s", out)
	}
}

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func reqJSON(method, url, body string) *http.Request {
	return httptest.NewRequest(method, url, strings.NewReader(body))
}

func TestAgentAnsibleThroughLoopback(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetSetting("hermes_enabled", "1")
	_ = srv.db.SetAnsibleEnabled(true)
	pid, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "web-fix", Content: "---\n- hosts: all\n  vars:\n    db_password: hunter2\n  tasks: []\n"})
	if err != nil {
		t.Fatal(err)
	}

	list := callTool(t, srv, "ansible_playbooks", `{}`)
	if !strings.Contains(list, "web-fix") {
		t.Fatalf("list: %s", list)
	}
	one := callTool(t, srv, "ansible_playbooks", `{"playbook_id":`+itoa(pid)+`}`)
	if strings.Contains(one, "hunter2") || !strings.Contains(one, "db_password") {
		t.Fatalf("playbook content must be masked: %s", one)
	}

	// A real run needs a human even in auto mode; check/syntax runs do not.
	_ = srv.db.SetSetting("agent_approval", "auto")
	res := make(chan string, 1)
	go func() {
		res <- callTool(t, srv, "ansible_run", `{"playbook_id":`+itoa(pid)+`,"mode":"run","group":"x"}`)
	}()
	for end := time.Now().Add(2 * time.Second); pendingCount(srv) == 0; {
		if time.Now().After(end) {
			t.Fatal("a real ansible run must wait for the operator even in auto mode")
		}
		time.Sleep(5 * time.Millisecond)
	}
	srv.agent.mu.Lock()
	var id string
	for k, a := range srv.agent.pending {
		id = k
		if a.Gate != gateInfra || a.HostID != 0 {
			t.Fatalf("gate/scope: %+v", a)
		}
	}
	srv.agent.mu.Unlock()
	srv.handleAgentApprovalDecide(newRec(), reqJSON(http.MethodPost, "/api/agent/approvals/"+id, `{"approve":false}`))
	if out := <-res; !strings.Contains(out, "denied") {
		t.Fatalf("denied run must report the denial: %s", out)
	}

	out := callTool(t, srv, "ansible_run", `{"playbook_id":`+itoa(pid)+`,"mode":"syntax"}`)
	if strings.Contains(out, `"isError":true`) || !strings.Contains(out, "syntax") {
		t.Fatalf("syntax run must start unprompted in auto mode: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("syntax run must not ask")
	}
	runs := callTool(t, srv, "ansible_runs", `{}`)
	if !strings.Contains(runs, "web-fix") {
		t.Fatalf("runs: %s", runs)
	}
}

// The SOUL is what teaches the agent its tools; a tool missing from it is a tool the agent will misuse or ignore.
func TestAgentSoulMentionsEveryTool(t *testing.T) {
	for _, tl := range agentTools {
		if !strings.Contains(defaultAgentSoul, "`"+tl.Name+"`") {
			t.Errorf("agent_soul.md does not mention tool %s", tl.Name)
		}
	}
}
