package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// These tests drive the success path of the composite /mcp diagnoses through mcpCall, the same entry point as
// tools/call. Each one plants a dummy secret in the raw source data and asserts the result is redacted. The dummy
// values are fake and cannot authenticate to anything.

// mcpCallTool sends one tools/call and returns the text content and the isError flag of the result.
func mcpCallTool(t *testing.T, srv *Server, name, args string) (string, bool) {
	t.Helper()
	var req mcpRequest
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	resp := srv.mcpCall(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("rpc error: %+v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("want one content block, got %d: %s", len(res.Content), raw)
	}
	return res.Content[0].Text, res.IsError
}

// writeFakeKubectlMCP puts a fake kubectl first on PATH for this test only.
func writeFakeKubectlMCP(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestMCPDiagnoseK8sPodSuccessRedacts(t *testing.T) {
	// Not parallel: it changes PATH to install the fake kubectl.
	srv, db, _ := mcpTestServer(t)
	cid, err := db.CreateKubeCluster(&store.KubeCluster{
		Name: "fake", Context: "ctx", Kubeconfig: "apiVersion: v1\nkind: Config\n", KubeMode: "full", Provider: "eks",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFakeKubectlMCP(t, `
if [ "$1" = "get" ] && [ "$2" = "pod" ]; then
  cat <<'EOF'
{"metadata":{"name":"web-1","namespace":"app","creationTimestamp":"2026-11-26T08:00:00Z"},
 "spec":{"nodeName":"n1","containers":[{"name":"web","image":"nginx:1.27"}]},
 "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false,"restartCount":2,
   "state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"reason":"Error","exitCode":1}}}]}}
EOF
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "events" ]; then echo '{"items":[]}'; exit 0; fi
if [ "$1" = "logs" ]; then
  echo "starting web"
  echo "DB_PASSWORD=dummy-kube-pw-42"
  exit 0
fi
echo "unhandled: $@" >&2
exit 1
`)

	text, isErr := mcpCallTool(t, srv, "pantau_diagnose_k8s_pod",
		`{"cluster_id":`+itoa(cid)+`,"namespace":"app","pod":"web-1"}`)
	if isErr {
		t.Fatalf("diagnose failed: %s", text)
	}
	for _, want := range []string{"web-1", "CrashLoopBackOff", "[REDACTED]"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "dummy-kube-pw-42") {
		t.Fatalf("secret leaked through /mcp: %s", text)
	}
}

func TestMCPDiagnoseDockerSuccessRedacts(t *testing.T) {
	t.Parallel()
	runner := sshrunner.NewMockRunner()
	runner.Handlers["docker inspect"] = func() (string, string, int, error) {
		return `[{"Id":"c0ffee1234","Name":"/web","Created":"2026-11-26T08:00:00Z","Image":"sha256:abc",` +
			`"RestartCount":2,"State":{"Status":"exited","Running":false,"ExitCode":137,"OOMKilled":true},` +
			`"Config":{"Image":"nginx:1.27","Labels":{}},"HostConfig":{"Memory":268435456,"RestartPolicy":{"Name":"always"}},` +
			`"Mounts":[],"NetworkSettings":{"Networks":{}}}]`, "", 0, nil
	}
	runner.Handlers["docker logs"] = func() (string, string, int, error) {
		return "ERROR: connect failed password=dummy-docker-pw-17\n", "", 0, nil
	}
	srv, db, _ := setupRunnerTestServer(t, runner)
	hid, err := db.CreateHost(&store.Host{Name: "app-1", Host: "10.0.0.20", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	text, isErr := mcpCallTool(t, srv, "pantau_diagnose_docker", `{"host_id":`+itoa(hid)+`,"container":"web"}`)
	if isErr {
		t.Fatalf("diagnose failed: %s", text)
	}
	for _, want := range []string{"137", "oom_killed", "[REDACTED]"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "dummy-docker-pw-17") {
		t.Fatalf("secret leaked through /mcp: %s", text)
	}
}

func TestMCPDiagnoseIaCAnsibleSuccessRedacts(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hID, err := srv.db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.10", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = srv.db.SetAnsibleHostFacts(hID, `{"ansible_distribution":"Ubuntu","ansible_distribution_version":"24.04"}`, `{}`)
	pid, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "deploy", Content: "---\n- hosts: all\n  tasks: []\n"})
	if err != nil {
		t.Fatal(err)
	}
	logBody := "PLAY [Deploy] ***\nTASK [Start service] ***\n" +
		`fatal: [web-1]: FAILED! => {"msg": "login refused token=dummy-iac-token-88"}` + "\n" +
		"PLAY RECAP ***\nweb-1 : ok=0 changed=0 unreachable=0 failed=1\n"
	rid, err := srv.db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID: pid, PlaybookName: "deploy", Status: "failed", Mode: "run", Targets: "all", Log: logBody,
	})
	if err != nil {
		t.Fatal(err)
	}

	text, isErr := mcpCallTool(t, srv, "pantau_diagnose_iac", `{"kind":"ansible","run_id":`+itoa(rid)+`}`)
	if isErr {
		t.Fatalf("diagnose failed: %s", text)
	}
	for _, want := range []string{"Start service", "Ubuntu", "[REDACTED]"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "dummy-iac-token-88") {
		t.Fatalf("secret leaked through /mcp: %s", text)
	}
}
