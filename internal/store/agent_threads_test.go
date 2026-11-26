package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// The schema migration creates both tables with the columns the history needs, on a fresh database.
func TestAgentThreadSchema(t *testing.T) {
	db := openTestDB(t)
	cols := func(table string) map[string]bool {
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			out[n] = true
		}
		return out
	}
	for _, c := range []string{"id", "scope_type", "scope_id", "title", "created_at", "updated_at"} {
		if !cols("agent_threads")[c] {
			t.Errorf("agent_threads is missing %s", c)
		}
	}
	for _, c := range []string{"id", "thread_id", "role", "content", "tool_calls_json", "approval_status", "created_at"} {
		if !cols("agent_messages")[c] {
			t.Errorf("agent_messages is missing %s", c)
		}
	}
	// The migration is idempotent: a second run on the same file changes nothing.
	if err := db.migrate(); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}

func TestAgentThreadCRUD(t *testing.T) {
	db := openTestDB(t)

	th, err := db.CreateAgentThread("host", 7, "sess-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if th.ScopeType != "host" || th.ScopeID != 7 || th.Title != "" {
		t.Fatalf("thread: %+v", th)
	}
	if _, err := db.CreateAgentThread("nonsense", 0, "", ""); err == nil {
		t.Fatal("an unknown scope type must be refused")
	}

	msgs := []AgentMessage{
		{Role: "user", Content: "why is nginx down?"},
		{Role: "assistant", Content: "let me look", ToolCalls: `[{"ID":"c1","Name":"list_hosts","Args":"{}"}]`},
		{Role: "tool", Content: "one host", ToolCallID: "c1", ToolName: "list_hosts"},
		{Role: "assistant", Content: "it answered"},
	}
	if err := db.AppendAgentMessages(th.ID, msgs); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetAgentThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Messages != 4 {
		t.Fatalf("message count: %d", got.Messages)
	}
	if got.Title != "why is nginx down?" {
		t.Fatalf("the first question must title the thread: %q", got.Title)
	}

	rows, err := db.ListAgentMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[2].ToolCallID != "c1" || rows[2].ToolName != "list_hosts" {
		t.Fatalf("messages: %+v", rows)
	}
	if rows[1].ToolCalls == "" {
		t.Fatal("the tool calls of the assistant row must be stored")
	}

	// Grouping: the thread is listed under its scope only.
	list, err := db.ListAgentThreads("host", 7)
	if err != nil || len(list) != 1 {
		t.Fatalf("host scope: %v %d", err, len(list))
	}
	if list, _ := db.ListAgentThreads("host", 8); len(list) != 0 {
		t.Fatalf("another host must not see it: %d", len(list))
	}
	if list, _ := db.ListAgentThreads("docker", 0); len(list) != 0 {
		t.Fatalf("another scope type must not see it: %d", len(list))
	}

	// A session resumes its newest thread.
	th2, err := db.CreateAgentThread("global", 0, "sess-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if cur, err := db.AgentThreadBySession("sess-a"); err != nil || cur == nil || cur.ID != th2.ID {
		t.Fatalf("newest session thread: %v %+v", err, cur)
	}
	if cur, _ := db.AgentThreadBySession("nobody"); cur != nil {
		t.Fatal("an unknown session must have no thread")
	}

	if err := db.DeleteAgentThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAgentThread(th.ID); err != ErrAgentThreadNotFound {
		t.Fatalf("deleted thread must be gone: %v", err)
	}
	if rows, _ := db.ListAgentMessages(th.ID); len(rows) != 0 {
		t.Fatalf("the messages must go with the thread: %d", len(rows))
	}
	if err := db.DeleteAgentThread(th.ID); err != ErrAgentThreadNotFound {
		t.Fatalf("a second delete must say not found: %v", err)
	}

	if _, err := db.CreateAgentThread("docker", 0, "", "s3"); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ClearAgentThreads("global", 0); err != nil || n != 1 {
		t.Fatalf("clear global: %v %d", err, n)
	}
	if n, err := db.ClearAgentThreads("", 0); err != nil || n != 1 {
		t.Fatalf("clear all: %v %d", err, n)
	}
}

// The rolling pruning keeps the newest messages of a thread, and the stored history always starts at a user
// message so a resumed conversation is a valid provider history.
func TestAgentMessagesPruned(t *testing.T) {
	db := openTestDB(t)
	th, err := db.CreateAgentThread("global", 0, "s", "")
	if err != nil {
		t.Fatal(err)
	}
	var batch []AgentMessage
	for i := range 60 {
		batch = append(batch,
			AgentMessage{Role: "user", Content: fmt.Sprintf("question %d", i)},
			AgentMessage{Role: "assistant", Content: "looking", ToolCalls: fmt.Sprintf(`[{"ID":"c%d","Name":"list_hosts","Args":"{}"}]`, i)},
			AgentMessage{Role: "tool", Content: "ok", ToolCallID: fmt.Sprintf("c%d", i), ToolName: "list_hosts"},
			AgentMessage{Role: "assistant", Content: "answer"},
		)
	}
	if err := db.AppendAgentMessages(th.ID, batch); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListAgentMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) > AgentMessagesPerThread {
		t.Fatalf("at most %d messages may stay, got %d", AgentMessagesPerThread, len(rows))
	}
	if rows[0].Role != "user" {
		t.Fatalf("the stored history must start at a user message, got %q", rows[0].Role)
	}
	last := rows[len(rows)-1]
	if last.Role != "assistant" || last.Content != "answer" {
		t.Fatalf("the newest message must be kept: %+v", last)
	}
}

// The chat history is local and re-creatable: a System Snapshot must not carry it, so a restore on another
// instance cannot overwrite that instance's conversations.
func TestAgentThreadsOutOfSnapshot(t *testing.T) {
	db := openTestDB(t)
	th, err := db.CreateAgentThread("host", 1, "s", "secret topic")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AppendAgentMessages(th.ID, []AgentMessage{{Role: "user", Content: "unique-chat-marker"}}); err != nil {
		t.Fatal(err)
	}
	payload, err := db.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "unique-chat-marker") {
		t.Fatal("a System Snapshot must not carry the chat history")
	}
}

func TestAgentThreadsPruned(t *testing.T) {
	db := openTestDB(t)
	var oldest int64
	for i := range AgentThreadMax + 10 {
		th, err := db.CreateAgentThread("global", 0, "s", fmt.Sprintf("thread %d", i))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldest = th.ID
		}
	}
	// The pruning runs on a write, like the incidents retention.
	th, err := db.CreateAgentThread("global", 0, "s", "trigger")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AppendAgentMessages(th.ID, []AgentMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListAgentThreads("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) > AgentThreadMax {
		t.Fatalf("at most %d threads may stay, got %d", AgentThreadMax, len(list))
	}
	if _, err := db.GetAgentThread(oldest); err != ErrAgentThreadNotFound {
		t.Fatalf("the oldest thread must be pruned: %v", err)
	}
}
