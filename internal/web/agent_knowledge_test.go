package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/store"
)

// A runbook of the matching domain is injected into the prompt, counts one use, and stays out of other domains.
func TestAgentKnowledgePromptInjectsAndCounts(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	rb := store.AgentRunbook{Kind: "runbook", Title: "crashloopbackoff fix", Domain: "kubernetes", TriggerSummary: "pod crashloopbackoff restarts"}
	id, err := db.CreateAgentRunbook(&rb)
	if err != nil {
		t.Fatal(err)
	}
	prompt := srv.agentKnowledgePrompt("my pod is in crashloopbackoff", domainKubernetes)
	if !strings.Contains(prompt, "[runbook #") || !strings.Contains(prompt, "crashloopbackoff fix") {
		t.Fatalf("the matching runbook must be injected: %q", prompt)
	}
	if srv.agentKnowledgePrompt("my pod is in crashloopbackoff", domainHost) != "" {
		t.Fatal("a kubernetes runbook must not reach the host domain")
	}
	list, _ := db.ListAgentRunbooks("")
	if len(list) != 1 || list[0].ID != id || list[0].UsageCount != 1 {
		t.Fatalf("the injected runbook must count one use: %+v", list)
	}
}

// The search tool answers in one call and asks nobody.
func TestSearchKnowledgeTool(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	if _, err := db.CreateAgentRunbook(&store.AgentRunbook{Kind: "blueprint", Title: "gcp dev workspace", Domain: "terraform", TriggerSummary: "gcp dev"}); err != nil {
		t.Fatal(err)
	}
	out, isErr := callTool(t, srv, "search_knowledge", `{"query":"gcp dev workspace"}`)
	if isErr || !strings.Contains(out, "gcp dev workspace") {
		t.Fatalf("search answer: %s", out)
	}
	if _, isErr := callTool(t, srv, "search_knowledge", `{"query":"  "}`); !isErr {
		t.Fatal("a blank query must be refused")
	}
}

// Enabled custom skills reach only their own domain (and the global scope); a disabled skill never does.
func TestCustomSkillsInjectedPerDomain(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	_, _ = db.CreateAgentCustomSkill(&store.AgentCustomSkill{Name: "docker rules", Domain: "docker", Enabled: true, Content: "Prefer compose files."})
	_, _ = db.CreateAgentCustomSkill(&store.AgentCustomSkill{Name: "off", Domain: "docker", Enabled: false, Content: "DISABLED-RULE"})
	_, _ = db.CreateAgentCustomSkill(&store.AgentCustomSkill{Name: "everywhere", Domain: "global", Enabled: true, Content: "Always write a summary."})
	docker := srv.agentCustomSkillsPrompt(domainDocker)
	if !strings.Contains(docker, "Prefer compose files.") || !strings.Contains(docker, "Always write a summary.") {
		t.Fatalf("docker skills missing: %q", docker)
	}
	if strings.Contains(docker, "DISABLED-RULE") {
		t.Fatal("a disabled skill reached the prompt")
	}
	if strings.Contains(srv.agentCustomSkillsPrompt(domainHost), "Prefer compose") {
		t.Fatal("a docker skill reached the host domain")
	}
	if srv.agentCustomSkillsPrompt(domainCore) != "" {
		t.Fatal("the core domain carries no domain skills")
	}
}

// The distiller's draft is redacted: a bearer token is gone, a vault placeholder stays, and nothing is saved yet.
func TestDistillDraftIsRedactedAndNotSaved(t *testing.T) {
	t.Parallel()
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		return llmResult{Text: "Here is the draft:\n{\"kind\":\"runbook\",\"title\":\"Fix crashloop\",\"domain\":\"kubernetes\"," +
			"\"trigger_summary\":\"pod crashloops\",\"steps\":[\"use password {{ vault.DB_PASS }}\",\"header Authorization: Bearer abcdef0123456789abcdef0123\"]," +
			"\"verify\":[\"pod is Running\"],\"rollback\":[],\"tags\":[\"k8s\"]}\n"}, nil
	})
	th, err := srv.db.CreateAgentThread("global", 0, "s-distill", "crashloop work")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.db.AppendAgentMessages(th.ID, []store.AgentMessage{{Role: "user", Content: "the pod crashloops"}, {Role: "assistant", Content: "restarted it"}}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.handleAgentThread(rec, httptest.NewRequest(http.MethodPost, "/api/agent/threads/"+itoa(th.ID)+"/distill", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("distill: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "abcdef0123456789abcdef0123") || !strings.Contains(body, "{{ vault.DB_PASS }}") {
		t.Fatalf("the draft must be redacted and keep vault placeholders: %s", body)
	}
	if list, _ := srv.db.ListAgentRunbooks(""); len(list) != 0 {
		t.Fatalf("a distilled draft must not be saved by itself: %+v", list)
	}
}

// A model answer without a JSON object is refused, not stored.
func TestDistillRefusesAnswerWithoutDraft(t *testing.T) {
	t.Parallel()
	srv, _ := investigatorServer(t, func(ctx context.Context, req llmRequest) (llmResult, error) {
		return llmResult{Text: "I cannot help with that."}, nil
	})
	th, _ := srv.db.CreateAgentThread("global", 0, "s-nodraft", "x")
	_ = srv.db.AppendAgentMessages(th.ID, []store.AgentMessage{{Role: "user", Content: "hi"}})
	rec := httptest.NewRecorder()
	srv.handleAgentThread(rec, httptest.NewRequest(http.MethodPost, "/api/agent/threads/"+itoa(th.ID)+"/distill", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502: %s", rec.Code, rec.Body.String())
	}
}

// The reviewed draft is saved through the knowledge endpoint; an unknown domain is refused.
func TestSaveDraftAndSearchOverHTTP(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	good := `{"kind":"runbook","title":"Fix crashloop","domain":"kubernetes","trigger_summary":"pod crashloops","steps":["read logs"],"verify":["Running"],"rollback":[],"tags":["k8s"]}`
	rec := httptest.NewRecorder()
	srv.handleAgentKnowledge(rec, httptest.NewRequest(http.MethodPost, "/api/agent/knowledge", strings.NewReader(good)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	bad := httptest.NewRecorder()
	srv.handleAgentKnowledge(bad, httptest.NewRequest(http.MethodPost, "/api/agent/knowledge", strings.NewReader(`{"title":"x","domain":"nope"}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown domain: %d", bad.Code)
	}
	search := httptest.NewRecorder()
	srv.handleAgentKnowledge(search, httptest.NewRequest(http.MethodGet, "/api/agent/knowledge?q=crashloop", nil))
	var got struct {
		Hits []store.KnowledgeHit `json:"hits"`
	}
	_ = json.Unmarshal(search.Body.Bytes(), &got)
	if len(got.Hits) != 1 {
		t.Fatalf("search: %s", search.Body.String())
	}
	list, _ := db.ListAgentRunbooks("")
	if len(list) != 1 {
		t.Fatalf("stored: %+v", list)
	}
	del := httptest.NewRecorder()
	srv.handleAgentKnowledge(del, httptest.NewRequest(http.MethodDelete, "/api/agent/knowledge/runbooks/"+itoa(list[0].ID), nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", del.Code)
	}
}

// Custom skills CRUD over HTTP: create, list, update, delete; a bad domain is refused.
func TestCustomSkillsCRUDOverHTTP(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	create := httptest.NewRecorder()
	srv.handleAgentCustomSkills(create, httptest.NewRequest(http.MethodPost, "/api/agent/skills/custom",
		strings.NewReader(`{"name":"compose","domain":"docker","enabled":true,"content_md":"Prefer compose."}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	var made struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(create.Body.Bytes(), &made)
	list := httptest.NewRecorder()
	srv.handleAgentCustomSkills(list, httptest.NewRequest(http.MethodGet, "/api/agent/skills/custom", nil))
	if !strings.Contains(list.Body.String(), "Prefer compose.") {
		t.Fatalf("list: %s", list.Body.String())
	}
	upd := httptest.NewRecorder()
	srv.handleAgentCustomSkills(upd, httptest.NewRequest(http.MethodPut, "/api/agent/skills/custom/"+itoa(made.ID),
		strings.NewReader(`{"name":"compose","domain":"docker","enabled":false,"content_md":"Prefer compose v2."}`)))
	if upd.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", upd.Code, upd.Body.String())
	}
	badUpd := httptest.NewRecorder()
	srv.handleAgentCustomSkills(badUpd, httptest.NewRequest(http.MethodPut, "/api/agent/skills/custom/"+itoa(made.ID),
		strings.NewReader(`{"name":"x","domain":"nope"}`)))
	if badUpd.Code != http.StatusBadRequest {
		t.Fatalf("bad domain: %d", badUpd.Code)
	}
	del := httptest.NewRecorder()
	srv.handleAgentCustomSkills(del, httptest.NewRequest(http.MethodDelete, "/api/agent/skills/custom/"+itoa(made.ID), nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", del.Code)
	}
}
