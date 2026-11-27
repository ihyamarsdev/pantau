package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/store"
)

// llmFunc is a scripted llmClient that answers each call with a function.
type llmFunc func(ctx context.Context, req llmRequest) (llmResult, error)

func (f llmFunc) Stream(ctx context.Context, req llmRequest, _ func(string)) (llmResult, error) {
	return f(ctx, req)
}

// investigatorServer prepares a server whose fast model is the scripted client.
func investigatorServer(t *testing.T, client llmFunc) (*Server, *store.DB) {
	t.Helper()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_llm_provider", "openai")
	_ = db.SetSetting("agent_llm_model", "test-model")
	_ = db.SetSetting("agent_llm_api_key", "sk-test")
	srv.agent.llmFactory = func(provider, baseURL, apiKey string) (llmClient, error) { return client, nil }
	return srv, db
}

// countToolResults counts the tool results the investigator has received so far.
func countToolResults(msgs []llmMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == llmRoleTool {
			n++
		}
	}
	return n
}

// The investigators run at the same time, hold only read-only tools, and a write named by the model is refused.
func TestDelegateInvestigateParallelReadOnly(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	up := 0
	allUp := make(chan struct{})
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		if len(req.Messages) == 1 {
			// The first call of each investigator waits until both are running: a sequential run would time out.
			mu.Lock()
			up++
			if up == 2 {
				close(allUp)
			}
			mu.Unlock()
			select {
			case <-allUp:
			case <-time.After(5 * time.Second):
				return llmResult{}, errors.New("the investigators did not run in parallel")
			}
		}
		if strings.Contains(req.System, "host domain") {
			if len(req.Messages) == 1 {
				return llmResult{ToolCalls: []llmToolCall{{ID: "h1", Name: "list_hosts", Args: "{}"}}}, nil
			}
			return llmResult{Text: "host summary ok"}, nil
		}
		// The docker investigator tries a write. It must come back as "not available", never as a run.
		if len(req.Messages) == 1 {
			return llmResult{ToolCalls: []llmToolCall{{ID: "d1", Name: "systemd_action", Args: `{"action":"restart","unit":"x"}`}}}, nil
		}
		for _, m := range req.Messages {
			if m.Role == llmRoleTool && m.IsError && strings.Contains(m.Text, "not available") {
				return llmResult{Text: "refused-ok"}, nil
			}
		}
		return llmResult{Text: "refused-missing"}, nil
	})

	out, isErr := callTool(t, srv, "delegate_investigate", `{"tasks":[
		{"domain":"host","question":"is the host up?"},
		{"domain":"docker","question":"are the containers fine?"}]}`)
	if isErr {
		t.Fatalf("delegate failed: %s", out)
	}
	var got struct {
		Investigations []investigation `json:"investigations"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Investigations) != 2 {
		t.Fatalf("answer: %s", out)
	}
	host, docker := got.Investigations[0], got.Investigations[1]
	if host.Domain != "host" || host.Summary != "host summary ok" || host.ToolCalls != 1 || host.Error != "" {
		t.Fatalf("host investigation: %+v", host)
	}
	if docker.Domain != "docker" || docker.Summary != "refused-ok" || docker.ToolCalls != 0 {
		t.Fatalf("docker investigation must refuse the write: %+v", docker)
	}
}

// An investigator makes at most investigatorMaxCalls tool calls; the next one is refused, and the answer still comes.
func TestDelegateInvestigateCallBudget(t *testing.T) {
	t.Parallel()
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		if countToolResults(req.Messages) >= investigatorMaxCalls+1 {
			return llmResult{Text: "budget-done"}, nil
		}
		return llmResult{ToolCalls: []llmToolCall{{ID: "x", Name: "list_hosts", Args: "{}"}}}, nil
	})
	out, isErr := callTool(t, srv, "delegate_investigate", `{"tasks":[
		{"domain":"host","question":"a"},{"domain":"host","question":"b"}]}`)
	if isErr {
		t.Fatalf("delegate failed: %s", out)
	}
	var got struct {
		Investigations []investigation `json:"investigations"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	for _, inv := range got.Investigations {
		if inv.ToolCalls != investigatorMaxCalls || inv.Summary != "budget-done" {
			t.Fatalf("budget not held: %+v", inv)
		}
	}
}

// A shared timeout ends every investigation that is still running, and each reports it.
func TestDelegateInvestigateTimeout(t *testing.T) {
	saved := investigatorTimeout
	investigatorTimeout = 10 * time.Millisecond
	t.Cleanup(func() { investigatorTimeout = saved })
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		<-ctx.Done()
		return llmResult{}, ctx.Err()
	})
	out, isErr := callTool(t, srv, "delegate_investigate", `{"tasks":[
		{"domain":"host","question":"a"},{"domain":"security","question":"b"}]}`)
	if isErr {
		t.Fatalf("delegate failed: %s", out)
	}
	if strings.Count(out, "timed out") != 2 {
		t.Fatalf("both investigations must report the timeout: %s", out)
	}
}

func TestDelegateInvestigateValidation(t *testing.T) {
	t.Parallel()
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		return llmResult{Text: "ok"}, nil
	})
	two := func(d1, d2, q string) string {
		return `{"tasks":[{"domain":"` + d1 + `","question":"` + q + `"},{"domain":"` + d2 + `","question":"x"}]}`
	}
	cases := []struct {
		name string
		args string
	}{
		{"one task", `{"tasks":[{"domain":"host","question":"a"}]}`},
		{"five tasks", `{"tasks":[{"domain":"host","question":"a"},{"domain":"host","question":"a"},{"domain":"host","question":"a"},{"domain":"host","question":"a"},{"domain":"host","question":"a"}]}`},
		{"core is not a domain", two("core", "host", "a")},
		{"unknown domain", two("nope", "host", "a")},
		{"empty question", two("host", "docker", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, isErr := callTool(t, srv, "delegate_investigate", tc.args)
			if !isErr {
				t.Fatalf("must be refused: %s", out)
			}
		})
	}
}

// Only read verbs pass, and no write-named tool ever reaches an investigator, whatever its arguments.
func TestInvestigatorAllowedIsReadOnly(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"list_hosts": true, "get_host_state": true, "docker_logs": true, "systemd_detail": true, "security_status": true,
		"systemd_action": false, "docker_run": false, "exec_command": false, "memory_save": false,
		"alert_ack": false, "tf_workspace_action": false, "tf_workspace_data": false, "kube_apply": false,
		"security_scan": false, "agent_propose_plan": false, "delegate_investigate": false,
	}
	for name, want := range cases {
		if got := investigatorAllowed(name); got != want {
			t.Errorf("investigatorAllowed(%s) = %v, want %v", name, got, want)
		}
	}
	for _, tool := range agentTools {
		if investigatorAllowed(tool.Name) && (riskTierOfTool(tool.Name, map[string]interface{}{}) != riskReadOnly || strings.Contains(tool.Name, "_action")) {
			t.Errorf("%s is allowed to an investigator but is not a read", tool.Name)
		}
	}
}

// The summary of an investigator is redacted before it returns to the orchestrator.
func TestInvestigatorSummaryIsRedacted(t *testing.T) {
	t.Parallel()
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		return llmResult{Text: "the header was Authorization: Bearer abcdef0123456789abcdef0123 and it is stale"}, nil
	})
	out, isErr := callTool(t, srv, "delegate_investigate", `{"tasks":[
		{"domain":"security","question":"a"},{"domain":"host","question":"b"}]}`)
	if isErr {
		t.Fatalf("delegate failed: %s", out)
	}
	if strings.Contains(out, "abcdef0123456789abcdef0123") || !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("the secret must not reach the orchestrator: %s", out)
	}
}
