package web

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAgentRepoTools(t *testing.T) {
	srv, call, _ := newReposServer(t)
	_ = srv.db.SetSetting("hermes_enabled", "1")
	_ = srv.db.SetSetting("agent_approval", "auto") // gate 1 passes without a card
	code, repo := cloneRepo(t, call, map[string]interface{}{"url": "git@git.example.com:team/infra.git", "auth": "none"})
	if code != http.StatusCreated {
		t.Fatalf("clone: %d %v", code, repo)
	}
	id := itoa(int64(repo["id"].(float64)))
	r := `{"repo_id":` + id

	if out := callTool(t, srv, "repo_list", `{}`); !strings.Contains(out, "infra") || strings.Contains(out, `"isError":true`) {
		t.Fatalf("repo_list: %s", out)
	}
	if out := callTool(t, srv, "repo_read_file", r+`,"path":"README.md"}`); !strings.Contains(out, "# Test") {
		t.Fatalf("repo_read_file: %s", out)
	}
	if out := callTool(t, srv, "repo_read_file", r+`,"path":"../etc/passwd"}`); !strings.Contains(out, `"isError":true`) {
		t.Fatalf("path escape must be refused: %s", out)
	}
	if out := callTool(t, srv, "repo_write_file", r+`,"path":"notes.txt","content":"password=hunter2\nhello\n"}`); strings.Contains(out, `"isError":true`) {
		t.Fatalf("repo_write_file: %s", out)
	}
	if out := callTool(t, srv, "repo_read_file", r+`,"path":"notes.txt"}`); strings.Contains(out, "hunter2") || !strings.Contains(out, "hello") {
		t.Fatalf("secret-looking values must be masked: %s", out)
	}
	if out := callTool(t, srv, "repo_status", r+`}`); !strings.Contains(out, "notes.txt") {
		t.Fatalf("repo_status must list the new file: %s", out)
	}

	// Committing pushes to the remote: gate 2 waits for the operator even in auto mode, and nothing runs unapproved.
	done := make(chan string, 1)
	go func() { done <- callTool(t, srv, "repo_commit", r+`,"message":"add notes"}`) }()
	var ap *agentApproval
	for ap == nil {
		srv.agent.mu.Lock()
		for _, p := range srv.agent.pending {
			ap = p
		}
		srv.agent.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if ap.Tool != "repo_commit" || ap.Gate != gateHigh || !strings.Contains(ap.Summary, "add notes") {
		t.Fatalf("approval card: %+v", ap)
	}
	ap.done <- true
	if out := <-done; !strings.Contains(out, "pushed") || strings.Contains(out, `"isError":true`) {
		t.Fatalf("repo_commit: %s", out)
	}
	if _, st := call(http.MethodGet, "/api/repos/"+id, nil); st["ahead"] != float64(0) || len(st["changes"].([]interface{})) != 0 {
		t.Fatalf("after commit and push: %v", st)
	}
}
