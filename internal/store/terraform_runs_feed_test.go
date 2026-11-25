package store_test

import (
	"testing"

	"pantau/internal/store"
)

func TestListTFRunsFeed_FiltersAndJoinsWorkspace(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	web, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "web-prod", Environment: "production", GroupName: "edge"})
	api, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "api-dev", Environment: "development"})
	mk := func(ws int64, status, trigger, msg string) {
		t.Helper()
		if _, err := db.CreateTFRun(&store.TFRun{WorkspaceID: ws, Type: "plan", Status: status, TriggerKind: trigger, Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	mk(web, "awaiting_approval", "manual", "bump ami")
	mk(web, "applied", "webhook", "")
	mk(api, "errored", "drift", "fix sg")

	all, err := db.ListTFRunsFeed(store.TFRunFeedFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("all: got %d err=%v", len(all), err)
	}
	if all[0].WorkspaceName != "api-dev" || all[0].WorkspaceEnvironment != "development" {
		t.Fatalf("join fields wrong: %+v", all[0])
	}
	if all[2].WorkspaceGroupName != "edge" {
		t.Fatalf("group not joined: %+v", all[2])
	}

	approval, _ := db.ListTFRunsFeed(store.TFRunFeedFilter{Statuses: []string{"awaiting_approval", "errored"}})
	if len(approval) != 2 {
		t.Fatalf("status filter: got %d", len(approval))
	}
	byTrigger, _ := db.ListTFRunsFeed(store.TFRunFeedFilter{Trigger: "webhook"})
	if len(byTrigger) != 1 || byTrigger[0].WorkspaceName != "web-prod" {
		t.Fatalf("trigger filter: %+v", byTrigger)
	}
	byWS, _ := db.ListTFRunsFeed(store.TFRunFeedFilter{Query: "api-d"})
	if len(byWS) != 1 || byWS[0].Run.Status != "errored" {
		t.Fatalf("query by workspace name: %+v", byWS)
	}
	byMsg, _ := db.ListTFRunsFeed(store.TFRunFeedFilter{Query: "ami"})
	if len(byMsg) != 1 {
		t.Fatalf("query by message: got %d", len(byMsg))
	}
	limited, _ := db.ListTFRunsFeed(store.TFRunFeedFilter{Limit: 2})
	if len(limited) != 2 {
		t.Fatalf("limit: got %d", len(limited))
	}
}
