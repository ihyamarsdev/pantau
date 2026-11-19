package store_test

import (
	"testing"

	"pantau/internal/store"
)

// The exact reported bug: the host "macmini-4" must be found when the operator asks with a space
// ("adakah host macmini 4"). RecallContext must surface it as a host fact even when the question is
// made of stopwords and no memory note matches.
func TestRecallContextFindsHostWithLooseSeparator(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.CreateHost(&store.Host{Name: "macmini-4", Host: "10.0.0.4", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateHost(&store.Host{Name: "macmini-40", Host: "10.0.0.40", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"adakah host macmini 4", "is there a host macmini-4", "status MacMini_4"} {
		recall, err := db.RecallContext(store.AgentMemoryScope{}, q, 12)
		if err != nil {
			t.Fatal(err)
		}
		var found []string
		for _, f := range recall.Facts {
			if f.Kind == "host" {
				found = append(found, f.Name)
			}
		}
		if !contains(found, "macmini-4") {
			t.Errorf("question %q: host macmini-4 not recalled, got %v", q, found)
		}
		if contains(found, "macmini-40") {
			t.Errorf("question %q: macmini-40 must not match a query for macmini-4, got %v", q, found)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
