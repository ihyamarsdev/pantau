package store_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestAnsiblePlaybook_GitSourceAndTokenHandling(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	id, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name: "repo-pb", Source: "git", GitURL: "https://github.com/acme/infra.git", GitBranch: "main",
		GitPath: "site.yml", GitAuth: "token", GitToken: "ghp_secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetAnsiblePlaybook(id)
	if got.Source != "git" || got.GitToken != "ghp_secret" || !got.GitTokenSet {
		t.Fatalf("stored playbook wrong: %+v", got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "ghp_secret") {
		t.Fatalf("git token leaked in JSON: %s", b)
	}

	// Empty token on update keeps the stored one.
	got.GitToken = ""
	got.GitBranch = "release"
	if err := db.UpdateAnsiblePlaybook(got); err != nil {
		t.Fatal(err)
	}
	again, _ := db.GetAnsiblePlaybook(id)
	if again.GitToken != "ghp_secret" || again.GitBranch != "release" {
		t.Fatalf("token not kept / field not updated: %+v", again)
	}

	// Switching auth away from token drops the token; ssh_key keeps the key id only for ssh_key.
	again.GitAuth, again.GitKeyID = "ssh_key", 7
	if err := db.UpdateAnsiblePlaybook(again); err != nil {
		t.Fatal(err)
	}
	sw, _ := db.GetAnsiblePlaybook(id)
	if sw.GitToken != "" || sw.GitTokenSet || sw.GitKeyID != 7 {
		t.Fatalf("auth switch did not clear token: %+v", sw)
	}
	sw.GitAuth = "none"
	_ = db.UpdateAnsiblePlaybook(sw)
	if n, _ := db.GetAnsiblePlaybook(id); n.GitKeyID != 0 {
		t.Fatalf("key id kept for auth=none: %+v", n)
	}

	// List includes the git fields but never the content; a new inline playbook defaults sensibly.
	inl, _ := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "inline-pb", Content: "x"})
	g, _ := db.GetAnsiblePlaybook(inl)
	if g.Source != "inline" || g.GitAuth != "none" {
		t.Fatalf("defaults wrong: %+v", g)
	}
}

func TestAnsibleRun_TriggerAndCommit(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	pid, _ := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "p", Content: "x"})
	r := &store.AnsibleRun{PlaybookID: pid, PlaybookName: "p", Trigger: "schedule", ScheduleID: 5}
	rid, err := db.CreateAnsibleRun(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAnsibleRunCommit(rid, "abc123"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetAnsibleRun(rid)
	if got.Trigger != "schedule" || got.ScheduleID != 5 || got.CommitSHA != "abc123" {
		t.Fatalf("run meta lost: %+v", got)
	}
	m, _ := db.CreateAnsibleRun(&store.AnsibleRun{PlaybookID: pid, PlaybookName: "p"})
	if g, _ := db.GetAnsibleRun(m); g.Trigger != "manual" {
		t.Fatalf("default trigger = %q", g.Trigger)
	}
}

func TestAnsibleSchedule_CRUDFireAndCascade(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	pid, _ := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "nightly", Content: "x"})

	next := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	sid, err := db.CreateAnsibleSchedule(&store.AnsibleSchedule{
		PlaybookID: pid, Name: "n", Cron: "0 3 * * *", Enabled: true, Mode: "check", HostIDs: []int64{1, 2}, Group: "web",
		ExtraVars: `{"a":1}`, Tags: "t", Verbosity: 1, Become: true, NextRunAt: &next,
	})
	if err != nil || sid == 0 {
		t.Fatalf("create: %v id=%d", err, sid)
	}
	got, err := db.GetAnsibleSchedule(sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlaybookName != "nightly" || !got.Enabled || !got.Become || len(got.HostIDs) != 2 || got.Group != "web" ||
		got.NextRunAt == nil || !got.NextRunAt.Equal(next) {
		t.Fatalf("round trip wrong: %+v", got)
	}

	got.Cron, got.Enabled, got.HostIDs, got.NextRunAt = "*/5 * * * *", false, nil, nil
	if err := db.UpdateAnsibleSchedule(got); err != nil {
		t.Fatal(err)
	}
	upd, _ := db.GetAnsibleSchedule(sid)
	if upd.Cron != "*/5 * * * *" || upd.Enabled || upd.NextRunAt != nil || upd.HostIDs == nil || len(upd.HostIDs) != 0 {
		t.Fatalf("update wrong: %+v", upd)
	}

	fired := time.Now().UTC()
	n2 := fired.Add(5 * time.Minute).Truncate(time.Second)
	if err := db.RecordAnsibleScheduleFire(sid, &fired, 42, "", &n2); err != nil {
		t.Fatal(err)
	}
	f, _ := db.GetAnsibleSchedule(sid)
	if f.LastRunID != 42 || f.LastRunAt == nil || f.NextRunAt == nil || !f.NextRunAt.Equal(n2) {
		t.Fatalf("fire not recorded: %+v", f)
	}

	if l, _ := db.ListAnsibleSchedules(pid); len(l) != 1 {
		t.Fatalf("list by playbook = %d", len(l))
	}
	if l, _ := db.ListAnsibleSchedules(pid + 99); len(l) != 0 {
		t.Fatalf("list by other playbook = %d", len(l))
	}

	// Snapshot round trip keeps schedules (and the git token of playbooks).
	gid, _ := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "g", Source: "git", GitURL: "https://h/r.git", GitPath: "a.yml", GitAuth: "token", GitToken: "tok"})
	payload, err := db.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.AnsibleSchedules) != 1 {
		t.Fatalf("snapshot schedules = %d", len(payload.AnsibleSchedules))
	}
	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()
	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if r, err := db2.GetAnsibleSchedule(sid); err != nil || r.Cron != "*/5 * * * *" || r.PlaybookID != pid {
		t.Fatalf("schedule not restored: %+v %v", r, err)
	}
	if g, _ := db2.GetAnsiblePlaybook(gid); g.GitToken != "tok" || g.Source != "git" {
		t.Fatalf("git playbook not restored: %+v", g)
	}

	// Deleting the playbook removes its schedules.
	if err := db.DeleteAnsiblePlaybook(pid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAnsibleSchedule(sid); !errors.Is(err, store.ErrAnsibleScheduleNotFound) {
		t.Fatalf("schedule survived playbook delete: %v", err)
	}
	if err := db.DeleteAnsibleSchedule(sid); !errors.Is(err, store.ErrAnsibleScheduleNotFound) {
		t.Fatalf("delete missing = %v", err)
	}
}
