package store_test

import (
	"strings"
	"testing"

	"pantau/internal/store"
)

// Free text becomes a safe FTS5 query: operators and punctuation in the input are plain words.
func TestFTSMatchQuery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"gcp-dev", `"gcp" OR "dev"`},
		{`"quoted"`, `"quoted"`},
		{"foo:bar", `"foo" OR "bar"`},
		{"OR NOT", `"or" OR "not"`},
		{"crash*loop(x)", `"crash" OR "loop" OR "x"`},
		{"   ", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := store.FTSMatchQuery(tc.in); got != tc.want {
				t.Fatalf("FTSMatchQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Hostile input never breaks the search: it returns no error, whatever the operators say.
func TestSearchAgentKnowledgeSurvivesOperators(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	_, _ = db.AddAgentMemory(nil, "gcp dev project uses europe region", "agent")
	for _, q := range []string{"gcp-dev", `"quoted`, "foo:bar", "OR NOT", "NEAR(a b)", "*", "a\"b"} {
		if _, err := db.SearchAgentKnowledge(q, "", 5); err != nil {
			t.Fatalf("query %q failed: %v", q, err)
		}
	}
	hits, _ := db.SearchAgentKnowledge("gcp-dev", "", 5)
	if len(hits) != 1 || hits[0].Kind != "memory" {
		t.Fatalf("gcp-dev must find the note: %+v", hits)
	}
}

// Ranking favours the entry with more matching words, and the domain filter keeps global entries.
func TestSearchAgentKnowledgeRankingAndDomain(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	weak := store.AgentRunbook{Kind: "runbook", Title: "restart", Domain: "kubernetes", TriggerSummary: "pod restarts"}
	strong := store.AgentRunbook{Kind: "runbook", Title: "crashloopbackoff fix", Domain: "kubernetes", TriggerSummary: "crashloopbackoff backoff pod"}
	global := store.AgentRunbook{Kind: "runbook", Title: "generic crashloopbackoff note", Domain: "global", TriggerSummary: "check logs"}
	other := store.AgentRunbook{Kind: "runbook", Title: "docker crashloopbackoff", Domain: "docker", TriggerSummary: "crashloopbackoff"}
	for _, rb := range []*store.AgentRunbook{&weak, &strong, &global, &other} {
		if _, err := db.CreateAgentRunbook(rb); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := db.SearchAgentKnowledge("crashloopbackoff", "kubernetes", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("kubernetes search must hold the kubernetes and the global entry only: %+v", hits)
	}
	if hits[0].Title != "crashloopbackoff fix" {
		t.Fatalf("the stronger match must rank first: %+v", hits)
	}
	for _, h := range hits {
		if h.Domain == "docker" {
			t.Fatalf("docker leaked into a kubernetes search: %+v", h)
		}
	}
}

// The index follows memory edits and deletes in the same call.
func TestAgentKnowledgeIndexFollowsWrites(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	note, err := db.AddAgentMemory(nil, "backups run at night", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if hits, _ := db.SearchAgentKnowledge("night", "", 5); len(hits) != 1 {
		t.Fatalf("new note not indexed: %+v", hits)
	}
	if err := db.UpdateAgentMemory(note.ID, "backups run at noon"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := db.SearchAgentKnowledge("night", "", 5); len(hits) != 0 {
		t.Fatalf("old text still indexed after update: %+v", hits)
	}
	if hits, _ := db.SearchAgentKnowledge("noon", "", 5); len(hits) != 1 {
		t.Fatalf("new text not indexed after update: %+v", hits)
	}
	if err := db.DeleteAgentMemory(note.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := db.SearchAgentKnowledge("noon", "", 5); len(hits) != 0 {
		t.Fatalf("deleted note still found: %+v", hits)
	}

	rb := store.AgentRunbook{Kind: "blueprint", Title: "gcp dev workspace", Domain: "terraform", TriggerSummary: "create gcp dev"}
	id, err := db.CreateAgentRunbook(&rb)
	if err != nil {
		t.Fatal(err)
	}
	if hits, _ := db.SearchAgentKnowledge("gcp", "terraform", 5); len(hits) != 1 {
		t.Fatalf("runbook not indexed: %+v", hits)
	}
	if err := db.DeleteAgentRunbook(id); err != nil {
		t.Fatal(err)
	}
	if hits, _ := db.SearchAgentKnowledge("gcp", "terraform", 5); len(hits) != 0 {
		t.Fatalf("deleted runbook still found: %+v", hits)
	}
}

func TestAgentKnowledgeValidation(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	long := strings.Repeat("x", 4001)
	cases := []struct {
		name string
		rb   store.AgentRunbook
	}{
		{"bad kind", store.AgentRunbook{Kind: "script", Title: "t"}},
		{"bad domain", store.AgentRunbook{Title: "t", Domain: "nope"}},
		{"no title", store.AgentRunbook{Title: "  "}},
		{"invalid steps json", store.AgentRunbook{Title: "t", StepsJSON: "{oops"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rb := tc.rb
			if _, err := db.CreateAgentRunbook(&rb); err == nil {
				t.Fatal("must be refused")
			}
		})
	}
	if _, err := db.CreateAgentCustomSkill(&store.AgentCustomSkill{Name: "s", Domain: "docker", Content: long}); err == nil {
		t.Fatal("an oversize skill must be refused")
	}
	if _, err := db.CreateAgentCustomSkill(&store.AgentCustomSkill{Name: "s", Domain: "nope"}); err == nil {
		t.Fatal("an unknown skill domain must be refused")
	}
}

func TestAgentCustomSkillCRUD(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	sk := store.AgentCustomSkill{Name: "docker rules", Domain: "docker", Enabled: true, Content: "Prefer compose."}
	id, err := db.CreateAgentCustomSkill(&sk)
	if err != nil {
		t.Fatal(err)
	}
	sk.ID = id
	sk.Enabled = false
	if err := db.UpdateAgentCustomSkill(&sk); err != nil {
		t.Fatal(err)
	}
	if on, _ := db.ListAgentCustomSkills("docker", true); len(on) != 0 {
		t.Fatalf("a disabled skill must not list as enabled: %+v", on)
	}
	if all, _ := db.ListAgentCustomSkills("docker", false); len(all) != 1 || all[0].Enabled {
		t.Fatalf("skill list: %+v", all)
	}
	if err := db.DeleteAgentCustomSkill(id); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAgentCustomSkill(id); err == nil {
		t.Fatal("deleting twice must report not found")
	}
}

// Runbooks, skills and memory notes survive an export and import, and the index is rebuilt on the other side.
func TestAgentKnowledgeSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	src, c1 := setupTestDB(t)
	defer c1()
	_, _ = src.AddAgentMemory(nil, "fleet keeps logs for seven days", "agent")
	rb := store.AgentRunbook{Kind: "runbook", Title: "crashloop triage", Domain: "kubernetes", TriggerSummary: "pod crashloops", StepsJSON: `{"steps":["check logs"]}`}
	if _, err := src.CreateAgentRunbook(&rb); err != nil {
		t.Fatal(err)
	}
	if _, err := src.CreateAgentCustomSkill(&store.AgentCustomSkill{Name: "k8s", Domain: "kubernetes", Enabled: true, Content: "Use summaries first."}); err != nil {
		t.Fatal(err)
	}
	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	dst, c2 := setupTestDB(t)
	defer c2()
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	runbooks, _ := dst.ListAgentRunbooks("")
	if len(runbooks) != 1 || runbooks[0].Title != "crashloop triage" {
		t.Fatalf("runbooks not restored: %+v", runbooks)
	}
	skills, _ := dst.ListAgentCustomSkills("", false)
	if len(skills) != 1 || skills[0].Content != "Use summaries first." {
		t.Fatalf("skills not restored: %+v", skills)
	}
	if hits, _ := dst.SearchAgentKnowledge("crashloop", "", 5); len(hits) != 1 {
		t.Fatalf("restored runbook not searchable: %+v", hits)
	}
	if hits, _ := dst.SearchAgentKnowledge("seven", "", 5); len(hits) != 1 || hits[0].Kind != "memory" {
		t.Fatalf("restored memory not searchable: %+v", hits)
	}
}
