package web

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestAgentTurnGuardHintsOnFifthKubeGet(t *testing.T) {
	t.Parallel()
	g := newAgentTurnGuard(agentGuardHintAfterKubeGet, agentGuardRepeatLimit, agentGuardTotalLimit)
	for i := 1; i <= 5; i++ {
		refuse, hint := g.before("kube_get", true)
		if refuse != "" {
			t.Fatalf("call %d refused: %s", i, refuse)
		}
		if (i == 5) != strings.Contains(hint, "[HINT") {
			t.Errorf("call %d hint = %q", i, hint)
		}
	}
}

func TestAgentTurnGuardHintsOnFourthTFCatalog(t *testing.T) {
	t.Parallel()
	g := newAgentTurnGuard(agentGuardHintAfterKubeGet, agentGuardRepeatLimit, agentGuardTotalLimit)
	for i := 1; i <= 4; i++ {
		refuse, hint := g.before("tf_catalog", true)
		if refuse != "" {
			t.Fatalf("call %d refused: %s", i, refuse)
		}
		if (i == 4) != strings.Contains(hint, "batch queries/module_ids") {
			t.Errorf("call %d hint = %q", i, hint)
		}
	}
}

func TestAgentTurnGuardRepeatLimitOnlyForReads(t *testing.T) {
	t.Parallel()
	g := newAgentTurnGuard(100, 3, 100)
	for i := 1; i <= 3; i++ {
		if refuse, _ := g.before("kube_workload_summary", true); refuse != "" {
			t.Fatalf("read %d refused: %s", i, refuse)
		}
	}
	if refuse, _ := g.before("kube_workload_summary", true); !strings.Contains(refuse, "already called 3 times") {
		t.Errorf("4th read refuse = %q, want a refusal", refuse)
	}
	// A repeated write is governed by its own approval gate, not by the read limit.
	for i := 1; i <= 4; i++ {
		if refuse, _ := g.before("kube_action", false); refuse != "" {
			t.Errorf("write %d refused by the read limit: %s", i, refuse)
		}
	}
}

func TestAgentTurnGuardTotalLimit(t *testing.T) {
	t.Parallel()
	g := newAgentTurnGuard(100, 100, 3)
	for i := 1; i <= 3; i++ {
		if refuse, _ := g.before(fmt.Sprintf("tool_%d", i), false); refuse != "" {
			t.Fatalf("call %d refused: %s", i, refuse)
		}
	}
	if refuse, _ := g.before("tool_4", false); !strings.Contains(refuse, "3 tool calls") {
		t.Errorf("4th call refuse = %q, want the total limit", refuse)
	}
}

// kubeLoopAgent is an agent whose SSH host answers every kubectl script with kubeGetFixture and counts the calls.
func kubeLoopAgent(t *testing.T, f *fakeLLM) (srv *Server, hostID int64, kubectlCalls func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	mock := sshrunner.NewMockRunner()
	mock.DefaultExec = func(string) (string, string, int, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return kubeGetFixture, "", 0, nil
	}
	srv, db, _ := setupRunnerTestServer(t, mock)
	hostID, err := db.CreateHost(&store.Host{Name: "k3s-1", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_llm_provider", "openai")
	_ = db.SetSetting("agent_llm_model", "test-model")
	_ = db.SetSetting("agent_llm_api_key", "sk-test")
	useFakeLLM(srv, f)
	return srv, hostID, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// toolResults returns the tool messages the model saw in its second call, in order.
func toolResults(f *fakeLLM) []llmMessage {
	var out []llmMessage
	for _, m := range f.call(1).Messages {
		if m.Role == llmRoleTool {
			out = append(out, m)
		}
	}
	return out
}

// A "how many workloads" question is answered with one summary call and one kubectl run.
func TestAgentWorkloadCountIsOneKubectlCall(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{}
	srv, hostID, kubectlCalls := kubeLoopAgent(t, f)
	f.results = []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "kube_workload_summary", Args: fmt.Sprintf(`{"target_id":%d,"namespace":"*"}`, hostID)}}},
		{Text: "There are 3 workloads."},
	}
	if w := chatPost(srv, "guard-count", "ada berapa workloads?"); !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("turn must finish: %s", w.Body.String())
	}
	if n := kubectlCalls(); n != 1 {
		t.Errorf("kubectl ran %d times, want 1", n)
	}
	res := toolResults(f)
	if len(res) != 1 || res[0].IsError || !strings.Contains(res[0].Text, `"total_workloads"`) {
		t.Fatalf("summary result = %+v", res)
	}
}

// A model that loops kube_get over 15 namespaces gets the hint on the fifth call, and calls 13 to 15 are refused
// before they reach kubectl.
func TestAgentKubeGetLoopIsCutOff(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{}
	srv, hostID, kubectlCalls := kubeLoopAgent(t, f)
	var batch []llmToolCall
	for i := 1; i <= 15; i++ {
		batch = append(batch, llmToolCall{ID: fmt.Sprintf("c%d", i), Name: "kube_get",
			Args: fmt.Sprintf(`{"target_id":%d,"kind":"deployments","namespace":"ns-%02d"}`, hostID, i)})
	}
	f.results = []llmResult{{ToolCalls: batch}, {Text: "I stopped after 12 lookups."}}
	if w := chatPost(srv, "guard-loop", "ada berapa workloads?"); !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("turn must finish: %s", w.Body.String())
	}
	if n := kubectlCalls(); n != agentGuardRepeatLimit {
		t.Errorf("kubectl ran %d times, want %d (calls past the limit must not run)", n, agentGuardRepeatLimit)
	}
	res := toolResults(f)
	if len(res) != 15 {
		t.Fatalf("tool results = %d, want 15", len(res))
	}
	if !strings.Contains(res[4].Text, "[HINT") {
		t.Errorf("5th result has no hint: %q", res[4].Text)
	}
	for i := agentGuardRepeatLimit; i < 15; i++ {
		if !res[i].IsError || !strings.Contains(res[i].Text, "not run") {
			t.Errorf("result %d = %+v, want a refusal", i+1, res[i])
		}
	}
}
