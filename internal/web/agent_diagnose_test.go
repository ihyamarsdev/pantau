package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

func TestDiagnoseToolsRegisteredAndReadOnly(t *testing.T) {
	want := []string{
		"k8s_diagnose_pod",
		"docker_diagnose_container",
		"docker_diagnose_stack",
		"ansible_diagnose_run",
		"terraform_analyze_plan",
	}
	have := map[string]agentTool{}
	for _, tl := range agentTools {
		have[tl.Name] = tl
	}
	for _, n := range want {
		tl, ok := have[n]
		if !ok {
			t.Fatalf("tool %s is not registered", n)
		}
		if tl.gate != 0 {
			t.Fatalf("read-only tool %s must have gate 0, got %d", n, tl.gate)
		}
		if tier := riskTierOfTool(n, nil); tier != riskReadOnly {
			t.Fatalf("read-only tool %s must have riskReadOnly, got %s", n, tier)
		}
	}
	if agentToolDomain("k8s_diagnose_pod") != domainKubernetes {
		t.Fatalf("k8s_diagnose_pod must belong to domainKubernetes")
	}
	if agentToolDomain("terraform_analyze_plan") != domainTerraform {
		t.Fatalf("terraform_analyze_plan must belong to domainTerraform")
	}
}

func TestTerraformAnalyzePlanToolAndRedaction(t *testing.T) {
	srv, db, _, _ := setupTerraformTestServer(t)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetTerraformEnabled(true)

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{
		Name:        "staging",
		Binary:      "terraform",
		Source:      "inline",
		Files:       `{"main.tf":"resource \"null_resource\" \"x\" {}"}`,
		Environment: "staging",
	})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := db.CreateTFRun(&store.TFRun{
		WorkspaceID: wsID,
		Type:        "plan",
		Status:      "awaiting_approval",
		TriggerKind: "manual",
		Targets:     "[]",
		Replace:     "[]",
		HasChanges:  true,
		Policy:      `{"status":"passed","violations":[]}`,
		Cost:        `{"available":true,"currency":"USD","monthly_before":10,"monthly_after":25,"delta":15}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	planFixture := `{"terraform_version":"1.9.0","resource_changes":[{"address":"aws_ssm_parameter.tok","mode":"managed","type":"aws_ssm_parameter","name":"tok","provider_name":"registry.terraform.io/hashicorp/aws","change":{"actions":["create"],"before":null,"after":{"name":"/app/token","value":"supersecret-tf-val"},"after_sensitive":{"value":true}}}]}`
	args, _ := json.Marshal(map[string]interface{}{
		"run_id":    runID,
		"plan_json": planFixture,
	})
	out, isErr := callTool(t, srv, "terraform_analyze_plan", string(args))
	if isErr {
		t.Fatalf("terraform_analyze_plan failed: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("read-only terraform_analyze_plan must not require approval")
	}
	if strings.Contains(out, "supersecret-tf-val") {
		t.Fatalf("sensitive value leaked: %s", out)
	}
	if !strings.Contains(out, "+create") || !strings.Contains(out, "(sensitive)") {
		t.Fatalf("expected +create and (sensitive) in output: %s", out)
	}
}

func TestAnsibleDiagnoseRunTool(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetSetting("agent_enabled", "1")
	_ = srv.db.SetAnsibleEnabled(true)

	hID, err := srv.db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.10", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = srv.db.SetAnsibleHostFacts(hID, `{"ansible_distribution":"Ubuntu","ansible_distribution_version":"24.04","ansible_kernel":"6.8.0"}`, `{}`)

	pid, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "deploy", Content: "---\n- hosts: all\n  tasks: []\n"})
	if err != nil {
		t.Fatal(err)
	}
	logBody := "PLAY [Deploy] ***\nTASK [Start service] ***\nfatal: [web-1]: FAILED! => {\"msg\": \"service failed\"}\nPLAY RECAP ***\nweb-1 : ok=0 changed=0 unreachable=0 failed=1\n"
	rid, err := srv.db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pid,
		PlaybookName: "deploy",
		Status:       "failed",
		Mode:         "run",
		Targets:      "all",
		Log:          logBody,
	})
	if err != nil {
		t.Fatal(err)
	}

	out, isErr := callTool(t, srv, "ansible_diagnose_run", `{"run_id":`+itoa(rid)+`}`)
	if isErr {
		t.Fatalf("ansible_diagnose_run failed: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("read-only ansible_diagnose_run must not require approval")
	}
	if !strings.Contains(out, "Start service") || !strings.Contains(out, "Ubuntu") {
		t.Fatalf("expected failed task and cached host facts in output: %s", out)
	}
}

func TestKubeApplyDryRunGate(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	srv.inspector = inspector.New(db, nil, nil)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_approval", "ask")

	// External cluster with no valid kubeconfig/kubectl: dry-run must fail BEFORE asking the operator!
	cid, err := db.CreateKubeCluster(&store.KubeCluster{Name: "staging", KubeMode: "full", Provider: "generic", Kubeconfig: "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() {
		out, isErr := callTool(t, srv, "kube_apply", `{"target_id":-`+itoa(cid)+`,"yaml":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"}`)
		done <- isErr && strings.Contains(out, "dry-run=server")
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("expected kube_apply to fail dry-run=server before asking operator")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kube_apply hung or waited for approval despite dry-run failure")
	}
	if pendingCount(srv) != 0 {
		t.Fatal("failed dry-run must never create an approval card")
	}
}
