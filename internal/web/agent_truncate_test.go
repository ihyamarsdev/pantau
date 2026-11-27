package web

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Tool output that does not fit the history is cut to a head, a structured notice and a tail, so the model
// keeps the beginning and the end and knows how to fetch the rest.

func TestTruncateToolResultKeepsHeadAndTail(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("H", agentToolResultMax)
	tail := strings.Repeat("T", 5000)
	long := head + strings.Repeat("M", 5000) + tail

	got := truncateToolResult("docker_logs", long)
	if len(got) >= len(long) {
		t.Fatalf("an oversized result must be cut: %d vs %d bytes", len(got), len(long))
	}
	if !strings.HasPrefix(got, "HHH") {
		t.Error("the head must be kept: it says what was asked")
	}
	if !strings.HasSuffix(got, strings.Repeat("T", 100)) {
		t.Error("the tail must be kept: the end of a log holds the error")
	}
	// The cut is never silent, and the notice says how much went and how to get it.
	if !strings.Contains(got, "tool result truncated") || !strings.Contains(got, "bytes omitted") {
		t.Fatalf("the cut must be announced: %s", got[:400])
	}
	if !strings.Contains(got, "docker logs --tail") {
		t.Errorf("the notice must carry the tool's own hint: %s", got)
	}
	// The result stays inside the history budget.
	if len(got) > agentToolResultMax+300 {
		t.Errorf("the notice must not push the result past its budget: %d bytes", len(got))
	}
}

func TestTruncateToolResultSmallResultUntouched(t *testing.T) {
	t.Parallel()
	small := "drwxr-xr-x  2 root root 4096 Jan 1 00:00 bin"
	if got := truncateToolResult("list_dir", small); got != small {
		t.Fatalf("a small result must pass through unchanged: %q", got)
	}
}

// Every tool a model can call must leave the result either whole or announced. A tool without a registered
// hint still gets a usable notice, so a new tool cannot silently truncate.
func TestTruncateToolResultUnknownToolGetsGenericHint(t *testing.T) {
	t.Parallel()
	got := truncateToolResult("some_new_tool", strings.Repeat("x", agentToolResultMax+1))
	if !strings.Contains(got, "tool result truncated") || !strings.Contains(got, "narrow the call") {
		t.Fatalf("an unknown tool must get the generic hint: %s", got[:400])
	}
}

// A registered tool that grows a hint must name the tool that actually reads it; the map is the contract the
// truncation relies on, so a key that is not a tool name is dead weight.
func TestToolResultHintsNameRegisteredTools(t *testing.T) {
	t.Parallel()
	for name := range toolResultHints {
		if findAgentTool(name) == nil {
			t.Errorf("toolResultHints names %q, which is not a registered tool", name)
		}
	}
}

// Old tool results are trimmed before a model call: the model already acted on them, so the middle is not
// worth the tokens. The newest ones stay whole, because the model is still reading them.
func TestShrinkOldToolResultsKeepsNewest(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("L", agentStaleBytes+500)
	msgs := []llmMessage{{Role: llmRoleUser, Text: "go"}}
	for i := range agentStaleKeep + 3 {
		msgs = append(msgs,
			llmMessage{Role: llmRoleAssistant, ToolCalls: []llmToolCall{{ID: "c" + itoa(int64(i)), Name: "docker_logs"}}},
			llmMessage{Role: llmRoleTool, ToolCallID: "c" + itoa(int64(i)), Text: big},
		)
	}
	out := shrinkOldToolResults(msgs)

	tools := []int{}
	for i, m := range out {
		if m.Role == llmRoleTool {
			tools = append(tools, i)
		}
	}
	if len(tools) != agentStaleKeep+3 {
		t.Fatalf("trimming must not drop a message: %d tool results", len(tools))
	}
	// The oldest ones are trimmed and say so.
	for _, i := range tools[:len(tools)-agentStaleKeep] {
		if len(out[i].Text) > agentStaleBytes+200 {
			t.Errorf("an old tool result must be trimmed: %d bytes", len(out[i].Text))
		}
		if !strings.Contains(out[i].Text, "older tool result trimmed") {
			t.Error("a trim must say so, or the model believes it saw everything")
		}
	}
	// The newest ones are untouched.
	for _, i := range tools[len(tools)-agentStaleKeep:] {
		if out[i].Text != big {
			t.Errorf("a recent tool result must stay whole: %d bytes", len(out[i].Text))
		}
	}
	// The pair is never broken: the assistant tool_call before a trimmed result is still there.
	for _, i := range tools {
		if out[i-1].Role != llmRoleAssistant || len(out[i-1].ToolCalls) == 0 {
			t.Errorf("the tool result at %d lost its tool_call", i)
		}
	}
}

func TestShrinkOldToolResultsShortHistoryUntouched(t *testing.T) {
	t.Parallel()
	msgs := []llmMessage{
		{Role: llmRoleUser, Text: "u"},
		{Role: llmRoleTool, Text: strings.Repeat("x", agentStaleBytes+1)},
	}
	out := shrinkOldToolResults(msgs)
	if out[1].Text != msgs[1].Text {
		t.Error("a history at or below the keep count must not be trimmed")
	}
}

// A tool result must reach the truncation whole: cutting it earlier (with a silent marker) would drop the
// end of a log before the model ever sees it, and the byte count in the notice would be a lie.
func TestCallAgentToolDoesNotCutBeforeTruncation(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	big := strings.Repeat("H", agentToolResultMax) + strings.Repeat("T", 5000)
	const name = "test_oversized_result"
	agentTools = append(agentTools, agentTool{
		Name:        name,
		Description: "a tool that returns a very large result",
		InputSchema: schema(nil, nil),
		run:         func(*Server, *http.Request, map[string]interface{}) (string, error) { return big, nil },
	})
	defer func() {
		for i, tl := range agentTools {
			if tl.Name == name {
				agentTools = append(agentTools[:i], agentTools[i+1:]...)
				break
			}
		}
	}()

	out, isErr := callTool(t, srv, name, `{}`)
	if isErr {
		t.Fatalf("the tool must not fail: %s", out)
	}
	if len(out) != len(big) {
		t.Fatalf("the result must reach the truncation whole: %d of %d bytes", len(out), len(big))
	}
	// What the model gets is the cut with the tail and the notice.
	got := truncateToolResult(name, out)
	if !strings.Contains(got, "tool result truncated") || !strings.Contains(got, strings.Repeat("T", 100)) {
		t.Fatalf("the cut must keep the tail and say so: %s", got[:200])
	}
	if !strings.Contains(got, strconv.Itoa(len(big)-agentToolResultMax)+" of "+strconv.Itoa(len(big))+" bytes omitted") {
		t.Errorf("the notice must count the real bytes: %s", got[:300])
	}
}

// A host list is foundational: trimming an old list_hosts result to 2 KB would silently drop the hosts at
// the end, so a later turn could not see that a host like "macmini-4" exists. Keep these results whole.
func TestShrinkOldToolResultsKeepsHostLists(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("H", agentStaleBytes+500)
	msgs := []llmMessage{{Role: llmRoleUser, Text: "go"}}
	for i := range agentStaleKeep + 2 {
		name := "docker_logs"
		if i == 0 {
			name = "list_hosts"
		}
		msgs = append(msgs,
			llmMessage{Role: llmRoleAssistant, ToolCalls: []llmToolCall{{ID: "c" + itoa(int64(i)), Name: name}}},
			llmMessage{Role: llmRoleTool, ToolCallID: "c" + itoa(int64(i)), ToolName: name, Text: big},
		)
	}
	out := shrinkOldToolResults(msgs)
	if out[2].Text != big {
		t.Error("an old list_hosts result must not be trimmed")
	}
	if !strings.Contains(out[4].Text, "older tool result trimmed") {
		t.Error("an old non-host tool result must still be trimmed")
	}
}
