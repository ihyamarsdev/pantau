package web

import (
	"context"
	"strconv"
)

// Provider-neutral types of the in-process AI agent. agent_llm.go implements llmClient for Anthropic and for
// OpenAI-compatible endpoints; agent_loop.go drives the tool-call loop on top of it.

const (
	llmRoleUser      = "user"
	llmRoleAssistant = "assistant"
	llmRoleTool      = "tool" // the result of one llmToolCall
)

type llmToolCall struct {
	ID   string
	Name string
	Args string // raw JSON object, "{}" when the model sent no arguments
}

type llmMessage struct {
	Role       string
	Text       string        // user text, assistant text, or tool result text
	ToolCalls  []llmToolCall // assistant only
	ToolCallID string        // tool only: the llmToolCall.ID this answers
	ToolName   string        // tool only: which tool produced this result (used by compaction)
	IsError    bool          // tool only: the call failed
}

type llmToolSpec struct {
	Name        string
	Description string
	Schema      map[string]interface{} // JSON Schema object, as in agentTool.InputSchema
}

type llmRequest struct {
	Model     string
	System    string
	Messages  []llmMessage
	Tools     []llmToolSpec // empty: the model cannot call tools (used for summaries)
	MaxTokens int           // 0 = the client's default
}

type llmResult struct {
	Text         string
	ToolCalls    []llmToolCall
	InputTokens  int // prompt size of this call, as the provider counted it (0 when unknown)
	OutputTokens int
}

// llmClient runs one model call. onText receives each text delta as it streams (may be nil). A returned error
// is final: the client has already retried what is safe to retry (429, 5xx and network errors before the first
// delta was delivered).
type llmClient interface {
	Stream(ctx context.Context, req llmRequest, onText func(string)) (llmResult, error)
}

// llmError is a non-2xx answer of the provider. Status 0 means the request never got an answer.
type llmError struct {
	Status  int
	Message string
}

func (e *llmError) Error() string {
	if e.Status == 0 {
		return "LLM provider unreachable: " + e.Message
	}
	return "LLM provider answered HTTP " + strconv.Itoa(e.Status) + ": " + e.Message
}
