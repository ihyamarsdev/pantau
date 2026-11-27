package web

// delegate_investigate: the orchestrator sends 2 to 4 read-only investigations, one per domain, and they run in
// parallel. Each investigator has its own conversation on the fast model (the primary model when no fast model is
// set), at most investigatorMaxCalls tool calls, and one timeout shared by all of them. Only its short summary
// returns to the orchestrator, so the raw tool output never fills the main context.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
)

const (
	investigatorMinTasks   = 2
	investigatorMaxTasks   = 4
	investigatorMaxCalls   = 4
	investigatorSummaryMax = 1500
)

// investigatorTimeout bounds one delegate_investigate call. A variable so the tests can make it short.
var investigatorTimeout = 20 * time.Second

// investigatorExcluded are read-looking tools that are not reads, or that must never run on a delegated path.
var investigatorExcluded = map[string]bool{
	"exec_command": true, "ssh_batch_inspect": true, "alert_ack": true, "file_action": true,
	"agent_propose_plan": true, "delegate_investigate": true, "set_tool_domain": true,
}

// investigatorReadVerbs are the words a read-only tool name carries (list_hosts, docker_logs, systemd_detail).
var investigatorReadVerbs = map[string]bool{
	"list": true, "get": true, "status": true, "detail": true, "details": true, "logs": true, "log": true,
	"info": true, "search": true, "summary": true, "state": true, "read": true, "inspect": true, "show": true,
	"check": true, "describe": true, "top": true, "events": true, "outline": true, "facts": true, "usage": true,
	"stats": true, "audit": true, "findings": true, "health": true,
}

// investigatorWriteWords win over any read verb: fleet_host_action carries a read-looking word and still writes.
var investigatorWriteWords = map[string]bool{
	"action": true, "actions": true, "run": true, "apply": true, "deploy": true, "install": true, "remove": true,
	"delete": true, "save": true, "write": true, "set": true, "create": true, "update": true, "prune": true,
	"drain": true, "restart": true, "stop": true, "start": true, "exec": true, "ack": true, "import": true,
	"export": true, "cancel": true, "discard": true, "approve": true, "unlock": true, "lock": true, "clone": true,
	"sync": true, "reboot": true, "upgrade": true, "clear": true, "rename": true, "scan": true, "refresh": true,
	"move": true, "copy": true, "enable": true, "disable": true, "kill": true, "mute": true, "forget": true,
}

// investigatorAllowed decides structurally which tools an investigator may hold. A tool is kept only when its
// name is a read verb and it is read-only by name; every write-named tool (action, run, deploy, save, ...) is
// out, whatever its arguments. Multi-action tools with any write action are out as well.
func investigatorAllowed(name string) bool {
	if investigatorExcluded[name] || strings.HasPrefix(name, "memory_") {
		return false
	}
	if _, multi := iacReadActions[name]; multi || iacWriteTools[name] {
		return false
	}
	if riskTierOfTool(name, map[string]interface{}{}) != riskReadOnly {
		return false
	}
	verb := false
	for _, word := range strings.Split(name, "_") {
		if investigatorWriteWords[word] {
			return false
		}
		verb = verb || investigatorReadVerbs[word]
	}
	return verb
}

// investigatorToolsFor lists the investigator's tools for one domain: its own read-only tools plus the core ones.
func investigatorToolsFor(domain toolDomain) []llmToolSpec {
	specs := []llmToolSpec{}
	for _, t := range agentTools {
		dom := agentToolDomain(t.Name)
		if (dom != domain && dom != domainCore) || !investigatorAllowed(t.Name) {
			continue
		}
		specs = append(specs, llmToolSpec{Name: t.Name, Description: t.Description, Schema: t.InputSchema})
	}
	return specs
}

func init() { agentTools = append(agentTools, investigateTools...) }

var investigateTools = []agentTool{
	{Name: "delegate_investigate", Description: "Investigate 2 to 4 domains in parallel, read only: each investigator checks one domain (host, kubernetes, docker, ansible, terraform or security) with at most 4 read-only tool calls and returns a short summary. Use it when the question spans domains; it changes nothing. Ask one clear question per domain.",
		InputSchema: schema([]string{"tasks"}, map[string]interface{}{
			"tasks": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{
				"domain":   map[string]interface{}{"type": "string", "enum": []string{"host", "kubernetes", "docker", "ansible", "terraform", "security"}},
				"question": str,
			}}},
		}), run: toolDelegateInvestigate},
}

// investigation is what one investigator returns to the orchestrator.
type investigation struct {
	Domain    string `json:"domain"`
	Question  string `json:"question"`
	Summary   string `json:"summary"`
	ToolCalls int    `json:"tool_calls"`
	Error     string `json:"error,omitempty"`
}

type investigationTask struct {
	domain   toolDomain
	question string
}

func toolDelegateInvestigate(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	raw, _ := args["tasks"].([]interface{})
	if len(raw) < investigatorMinTasks || len(raw) > investigatorMaxTasks {
		return "", fmt.Errorf("tasks needs %d to %d investigations", investigatorMinTasks, investigatorMaxTasks)
	}
	tasks := make([]investigationTask, 0, len(raw))
	for i, item := range raw {
		obj, _ := item.(map[string]interface{})
		d := canonicalDomain(argStr(obj, "domain"))
		if d == "" || d == domainCore || d == domainAll {
			return "", fmt.Errorf("tasks[%d]: unknown domain %q", i, argStr(obj, "domain"))
		}
		q := argStr(obj, "question")
		if q == "" {
			return "", fmt.Errorf("tasks[%d]: question is required", i)
		}
		tasks = append(tasks, investigationTask{domain: d, question: q})
	}
	c := s.agentConfig()
	client, model, err := s.agentFastLLMClient(c)
	if err != nil {
		return "", err
	}
	if model == "" {
		model = c.Model
	}
	ctx, cancel := context.WithTimeout(r.Context(), investigatorTimeout)
	defer cancel()

	results := make([]investigation, len(tasks))
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("agent", string(t.domain), fmt.Sprintf("investigator panicked: %v", rec))
					results[i] = investigation{Domain: string(t.domain), Question: clipTo(t.question, 300), Error: "the investigator failed unexpectedly"}
				}
			}()
			results[i] = s.runInvestigator(ctx, r, client, model, t)
		}()
	}
	wg.Wait()
	return jsonText(map[string]interface{}{"investigations": results})
}

// runInvestigator holds one read-only conversation. Its tool list is fixed by investigatorAllowed, and each call
// is checked again at run time, so a model that names another tool gets an error result, not a run.
func (s *Server) runInvestigator(ctx context.Context, r *http.Request, client llmClient, model string, t investigationTask) investigation {
	out := investigation{Domain: string(t.domain), Question: clipTo(t.question, 300)}
	specs := investigatorToolsFor(t.domain)
	allowed := map[string]bool{}
	for _, sp := range specs {
		allowed[sp.Name] = true
	}
	system := fmt.Sprintf("You are a read-only investigator for the %s domain of Pantau. You can only read: answer from the tool results. "+
		"Use at most %d tool calls. Reply in under 120 words: the findings, the evidence, and what you could not check. Never suggest that you changed anything.",
		t.domain, investigatorMaxCalls)
	msgs := []llmMessage{{Role: llmRoleUser, Text: t.question}}
	calls := 0
	for round := 0; round <= investigatorMaxCalls+1; round++ {
		req := llmRequest{Model: model, System: system, Messages: msgs}
		if calls < investigatorMaxCalls {
			req.Tools = specs
		}
		res, err := client.Stream(ctx, req, nil)
		if err != nil {
			out.ToolCalls = calls
			out.Error = clipTo(err.Error(), 200)
			if ctx.Err() != nil {
				out.Error = "timed out: the investigation did not finish in time"
			}
			return out
		}
		if len(res.ToolCalls) == 0 {
			out.ToolCalls = calls
			out.Summary = agentRedactText(clipTo(strings.TrimSpace(res.Text), investigatorSummaryMax))
			return out
		}
		msgs = append(msgs, llmMessage{Role: llmRoleAssistant, Text: res.Text, ToolCalls: res.ToolCalls})
		for _, tc := range res.ToolCalls {
			if tc.ID == "" {
				tc.ID = "call_" + randHex(6)
			}
			args, argErr := agentToolArgs(tc.Args)
			text, isErr := "", false
			switch {
			case argErr != nil:
				text, isErr = "the tool arguments are not valid JSON", true
			case !allowed[tc.Name]:
				text, isErr = "not available to this investigator: it only reads, with its own domain's read-only tools", true
			case calls >= investigatorMaxCalls:
				text, isErr = fmt.Sprintf("not run: this investigator already made %d tool calls; answer from what you have", investigatorMaxCalls), true
			case riskTierOfTool(tc.Name, args) != riskReadOnly:
				text, isErr = "not run: only read-only calls are allowed here", true
			default:
				calls++
				text, isErr = s.callAgentTool(r.WithContext(ctx), tc.Name, args)
			}
			msgs = append(msgs, llmMessage{Role: llmRoleTool, ToolCallID: tc.ID, ToolName: tc.Name, Text: truncateToolResult(tc.Name, text), IsError: isErr})
		}
	}
	out.ToolCalls = calls
	out.Error = "no answer within the tool budget"
	return out
}
