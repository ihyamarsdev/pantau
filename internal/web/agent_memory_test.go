package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestAgentMemoryToolsAndPrompt(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	hostID, _ := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	call := func(name, args string) (string, bool) {
		return callTool(t, srv, name, args)
	}

	if p := srv.agentMemoryPrompt(""); !strings.Contains(p, "No notes saved yet") || !strings.Contains(p, "memory_save") {
		t.Fatalf("empty prompt must still teach the tools: %s", p)
	}

	if out, _ := call("memory_save", `{"content":"Backups run at 02:00"}`); !strings.Contains(out, "saved as note #1") {
		t.Fatalf("save: %s", out)
	}
	if out, _ := call("memory_save", `{"content":"nginx sits behind Cloudflare","host_id":`+itoa(hostID)+`}`); !strings.Contains(out, "note #2") {
		t.Fatalf("save host note: %s", out)
	}
	for _, bad := range []string{
		`{"content":"db password: hunter2"}`,
		`{"content":"-----BEGIN OPENSSH PRIVATE KEY-----"}`,
		`{"content":"x","host_id":9999}`,
		`{"content":"x","replaces":9999}`,
		`{"content":""}`,
	} {
		if out, isErr := call("memory_save", bad); !isErr {
			t.Errorf("must be refused: %s -> %s", bad, out)
		}
	}

	p := srv.agentMemoryPrompt("")
	if !strings.Contains(p, `[#1] (fleet) Backups run at 02:00`) || !strings.Contains(p, `[#2] (host "web-1" id `+itoa(hostID)+`) nginx sits behind Cloudflare`) {
		t.Fatalf("notes missing from prompt: %s", p)
	}

	if out, _ := call("memory_save", `{"content":"Backups run at 03:00","replaces":1}`); !strings.Contains(out, "rewritten") {
		t.Fatalf("replace: %s", out)
	}
	if out, _ := call("memory_list", `{"host_id":`+itoa(hostID)+`}`); !strings.Contains(out, "03:00") || !strings.Contains(out, "Cloudflare") {
		t.Fatalf("list host: %s", out)
	}
	if out, _ := call("memory_forget", `{"id":2}`); !strings.Contains(out, "deleted") {
		t.Fatalf("forget: %s", out)
	}
	if p := srv.agentMemoryPrompt(""); strings.Contains(p, "Cloudflare") || strings.Contains(p, "02:00") {
		t.Fatalf("forgotten/rewritten text still in prompt: %s", p)
	}
}

func TestAgentMemoryPromptRespectsBudget(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	for i := range 40 {
		_, _ = db.AddAgentMemory(nil, strings.Repeat("w", 400)+itoa(int64(i)), "agent")
	}
	p := srv.agentMemoryPrompt("")
	if len(p) > agentMemoryBudget+300 || !strings.Contains(p, "more notes not shown") {
		t.Fatalf("prompt not bounded (%d bytes)", len(p))
	}
}

func TestAgentMemoryAPI(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	token := "memory_api_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	if w := httptest.NewRecorder(); true {
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/api/agent/memory", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("needs a session: %d", w.Code)
		}
	}
	if w := do("POST", "/api/agent/memory", `{"content":"Staging is host b"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"operator"`) {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/agent/memory", `{"content":"x","host_id":999}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown host: %d", w.Code)
	}
	if w := do("POST", "/api/agent/memory", `{"id":1,"content":"Staging is host c"}`); w.Code != 200 {
		t.Fatalf("edit: %d", w.Code)
	}
	if w := do("GET", "/api/agent/memory", ""); !strings.Contains(w.Body.String(), "host c") {
		t.Fatalf("list: %s", w.Body.String())
	}
	if w := do("DELETE", "/api/agent/memory?id=1", ""); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := do("DELETE", "/api/agent/memory?id=1", ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", w.Code)
	}
}

// The operator can file a note under a domain or a cluster too, not only under a host: the panel offers the
// same scopes the agent uses.
func TestAgentMemoryAPIScopes(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	token := "memory_scope_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	clusterID, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Context: "prod", Server: "https://k8s"})
	if err != nil {
		t.Fatal(err)
	}

	w := do("POST", "/api/agent/memory", `{"content":"the ingress is traefik","cluster_id":`+itoa(clusterID)+`}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cluster_id":`+itoa(clusterID)) {
		t.Fatalf("cluster scope: %d %s", w.Code, w.Body.String())
	}
	w = do("POST", "/api/agent/memory", `{"content":"fail2ban bans after 5 tries","domain":"security"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"domain":"security"`) {
		t.Fatalf("domain scope: %d %s", w.Code, w.Body.String())
	}
	// A scope that does not exist is refused, and two scopes at once are refused.
	if w := do("POST", "/api/agent/memory", `{"content":"x","cluster_id":9999}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown cluster: %d", w.Code)
	}
	if w := do("POST", "/api/agent/memory", `{"content":"x","domain":"nonsense"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown domain: %d", w.Code)
	}
	if w := do("POST", "/api/agent/memory", `{"content":"x","host_id":1,"domain":"network"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("two scopes: %d", w.Code)
	}
}

// A note can be filed under a domain or a cluster, and the prompt carries only the notes the question is about
// instead of the whole table.
func TestAgentMemoryScopedToolsAndRecall(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	hostID, _ := db.CreateHost(&store.Host{Name: "db-1", Host: "10.0.0.1", Port: 22, User: "root"})
	clusterID, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Context: "prod", Server: "https://k8s"})
	if err != nil {
		t.Fatal(err)
	}

	// A domain note, a cluster note and a host note, each through the tool.
	if out, isErr := callTool(t, srv, "memory_save", `{"content":"fail2ban bans an IP after 5 failed logins","domain":"security"}`); isErr {
		t.Fatalf("domain note: %s", out)
	}
	if out, isErr := callTool(t, srv, "memory_save", `{"content":"the ingress is traefik behind the NLB","cluster_id":`+itoa(clusterID)+`}`); isErr {
		t.Fatalf("cluster note: %s", out)
	}
	if out, isErr := callTool(t, srv, "memory_save", `{"content":"postgres replication lags during backups","host_id":`+itoa(hostID)+`}`); isErr {
		t.Fatalf("host note: %s", out)
	}

	// Unknown scopes, and two scopes at once, are refused before anything is stored.
	for _, bad := range []string{
		`{"content":"x","domain":"nonsense"}`,
		`{"content":"x","host_id":` + itoa(hostID) + `,"domain":"network"}`,
		`{"content":"x","cluster_id":9999}`,
	} {
		if out, isErr := callTool(t, srv, "memory_save", bad); !isErr {
			t.Errorf("must be refused: %s -> %s", bad, out)
		}
	}

	// A question about host db-1 carries that host's note, not the security or the cluster one.
	p := srv.agentMemoryPrompt("why does postgres replication lag on db-1?")
	if !strings.Contains(p, "postgres replication") {
		t.Fatalf("the host note must be recalled: %s", p)
	}
	if strings.Contains(p, "fail2ban") || strings.Contains(p, "traefik") {
		t.Errorf("an unrelated note must not reach the prompt: %s", p)
	}
	if !strings.Contains(p, `(host "db-1" id `) {
		t.Errorf("the scope label is missing: %s", p)
	}

	// A security question carries the domain note.
	if p := srv.agentMemoryPrompt("how does the security stack block brute force?"); !strings.Contains(p, "fail2ban") {
		t.Fatalf("the domain note must be recalled by a domain question: %s", p)
	}
	// A question that matches nothing says so instead of dumping the whole table.
	if p := srv.agentMemoryPrompt("how is the weather today?"); !strings.Contains(p, "No note matches") {
		t.Fatalf("an unrelated question must not dump every note: %s", p)
	}
	// With no question at all (the first turn of a chat) the newest notes are shown.
	if p := srv.agentMemoryPrompt(""); !strings.Contains(p, "fail2ban") || !strings.Contains(p, "traefik") {
		t.Fatalf("an empty question must show the saved notes: %s", p)
	}
}

// The prompt lists the stored inventory the question mentions, so the agent does not have to call fleet_search
// for the first fact.
func TestAgentMemoryPromptCarriesFleetFacts(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	hostID, _ := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.9", Port: 22, User: "root"})
	if err := db.ReplaceHostPackages(hostID, []*store.HostPackage{{HostID: hostID, Name: "nginx", Version: "1.24.0", LastScannedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{}, "nginx is the edge proxy", "agent"); err != nil {
		t.Fatal(err)
	}

	p := srv.agentMemoryPrompt("which hosts run nginx?")
	if !strings.Contains(p, "nginx") {
		t.Fatalf("the note must be recalled: %s", p)
	}
	if !strings.Contains(p, "stored inventory") || !strings.Contains(p, "alpha") {
		t.Fatalf("the stored inventory must be summarised with its hosts: %s", p)
	}
}
