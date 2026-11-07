package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAnsiblePlaybookListSummary(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	idle, _ := db.CreateAnsiblePlaybook(&AnsiblePlaybook{Name: "idle", Content: "- hosts: all\n"})
	busy, _ := db.CreateAnsiblePlaybook(&AnsiblePlaybook{Name: "busy", Content: "- hosts: all\n"})

	runID, err := db.CreateAnsibleRun(&AnsibleRun{
		PlaybookID: busy, PlaybookName: "busy", Status: "queued", Mode: "run",
		Targets: `[{"id":1,"name":"a"},{"id":2,"name":"b"},{"id":3,"name":"c"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StartAnsibleRun(runID); err != nil {
		t.Fatal(err)
	}
	ec := 0
	recap := `{"a":{"ok":3,"changed":1,"unreachable":0,"failed":0,"skipped":0},"b":{"ok":2,"changed":0,"unreachable":1,"failed":1,"skipped":4}}`
	if err := db.FinishAnsibleRun(runID, "success", &ec, "log", recap); err != nil {
		t.Fatal(err)
	}

	soon := time.Now().Add(time.Hour).UTC()
	later := time.Now().Add(48 * time.Hour).UTC()
	for _, s := range []AnsibleSchedule{
		{PlaybookID: busy, Name: "later", Cron: "0 3 * * *", Enabled: true, Mode: "run", NextRunAt: &later},
		{PlaybookID: busy, Name: "soon", Cron: "0 * * * *", Enabled: true, Mode: "run", NextRunAt: &soon},
		{PlaybookID: busy, Name: "off", Cron: "* * * * *", Enabled: false, Mode: "run", NextRunAt: &time.Time{}},
	} {
		s := s
		if _, err := db.CreateAnsibleSchedule(&s); err != nil {
			t.Fatal(err)
		}
	}

	list, err := db.ListAnsiblePlaybooks()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]AnsiblePlaybook{}
	for _, p := range list {
		byID[p.ID] = p
	}

	if p := byID[idle]; p.LastRunID != 0 || p.LastDurationMs != nil || p.LastTargets != nil || p.LastRecap != "" || p.ScheduleCount != 0 || p.NextRunAt != nil {
		t.Errorf("playbook without runs/schedules must carry no summary, got %+v", p)
	}
	p := byID[busy]
	if p.LastRunID != runID || p.LastDurationMs == nil || *p.LastDurationMs < 0 {
		t.Errorf("last run id/duration wrong: id=%d dur=%v", p.LastRunID, p.LastDurationMs)
	}
	if p.LastTargets == nil || *p.LastTargets != 3 {
		t.Errorf("last_targets = %v, want 3", p.LastTargets)
	}
	if p.LastRecap != recap {
		t.Errorf("raw last recap not carried: %q", p.LastRecap)
	}
	if p.ScheduleCount != 3 {
		t.Errorf("schedule_count = %d, want 3 (disabled schedules still count)", p.ScheduleCount)
	}
	if p.NextRunAt == nil || p.NextRunAt.Sub(soon).Abs() > time.Second {
		t.Errorf("next_run_at = %v, want the earliest ENABLED schedule %v", p.NextRunAt, soon)
	}

	// GetAnsiblePlaybook returns the same summary.
	got, err := db.GetAnsiblePlaybook(busy)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScheduleCount != 3 || got.LastTargets == nil || *got.LastTargets != 3 {
		t.Errorf("Get summary differs from List: %+v", got)
	}
}

func TestListAnsibleRunsStatusAndPlaybookFilter(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := db.CreateAnsiblePlaybook(&AnsiblePlaybook{Name: "a", Content: "- hosts: all\n"})
	b, _ := db.CreateAnsiblePlaybook(&AnsiblePlaybook{Name: "b", Content: "- hosts: all\n"})
	for _, r := range []AnsibleRun{
		{PlaybookID: a, PlaybookName: "a", Status: "failed", Mode: "run", Targets: "[]"},
		{PlaybookID: a, PlaybookName: "a", Status: "success", Mode: "run", Targets: "[]"},
		{PlaybookID: b, PlaybookName: "b", Status: "failed", Mode: "run", Targets: "[]"},
	} {
		r := r
		if _, err := db.CreateAnsibleRun(&r); err != nil {
			t.Fatal(err)
		}
	}
	count := func(pid int64, status string) int {
		l, err := db.ListAnsibleRuns(pid, status, 50)
		if err != nil {
			t.Fatal(err)
		}
		return len(l)
	}
	if n := count(0, ""); n != 3 {
		t.Errorf("unfiltered = %d, want 3", n)
	}
	if n := count(0, "failed"); n != 2 {
		t.Errorf("failed = %d, want 2", n)
	}
	if n := count(a, "failed"); n != 1 {
		t.Errorf("playbook a + failed = %d, want 1", n)
	}
	if n := count(b, "success"); n != 0 {
		t.Errorf("playbook b + success = %d, want 0", n)
	}
}
