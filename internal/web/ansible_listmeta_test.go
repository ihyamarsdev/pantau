package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"pantau/internal/store"
)

func TestAnsiblePlaybookListCarriesLastRunSummary(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	pb, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "p", Content: "- hosts: all\n"})
	runID, _ := srv.db.CreateAnsibleRun(&store.AnsibleRun{PlaybookID: pb, PlaybookName: "p", Status: "queued", Mode: "run", Targets: `[{"id":1,"name":"a"}]`})
	_ = srv.db.StartAnsibleRun(runID)
	ec := 2
	_ = srv.db.FinishAnsibleRun(runID, "failed", &ec, "log",
		`{"a":{"ok":1,"changed":2,"unreachable":0,"failed":1,"skipped":0},"b":{"ok":4,"changed":0,"unreachable":1,"failed":0,"skipped":3}}`)

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/playbooks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Playbooks []struct {
			LastRunID     int64          `json:"last_run_id"`
			LastTargets   *int           `json:"last_targets"`
			LastRecap     map[string]int `json:"last_recap"`
			ScheduleCount int            `json:"schedule_count"`
		} `json:"playbooks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Playbooks) != 1 {
		t.Fatalf("decode: %v %s", err, rec.Body.String())
	}
	p := resp.Playbooks[0]
	if p.LastRunID != runID || p.LastTargets == nil || *p.LastTargets != 1 || p.ScheduleCount != 0 {
		t.Errorf("summary wrong: %+v", p)
	}
	want := map[string]int{"ok": 5, "changed": 2, "unreachable": 1, "failed": 1, "skipped": 3}
	for k, v := range want {
		if p.LastRecap[k] != v {
			t.Errorf("last_recap[%s] = %d, want %d (%v)", k, p.LastRecap[k], v, p.LastRecap)
		}
	}
}

func TestAnsibleRunsListFilters(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	a, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "a", Content: "- hosts: all\n"})
	b, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "b", Content: "- hosts: all\n"})
	for _, r := range []store.AnsibleRun{
		{PlaybookID: a, PlaybookName: "a", Status: "failed", Mode: "run", Targets: "[]"},
		{PlaybookID: a, PlaybookName: "a", Status: "success", Mode: "run", Targets: "[]"},
		{PlaybookID: b, PlaybookName: "b", Status: "failed", Mode: "run", Targets: "[]"},
	} {
		r := r
		_, _ = srv.db.CreateAnsibleRun(&r)
	}
	count := func(query string) (int, int) {
		rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/runs"+query, nil)
		var resp struct {
			Runs []json.RawMessage `json:"runs"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, len(resp.Runs)
	}
	for _, tc := range []struct {
		query string
		code  int
		n     int
	}{
		{"", 200, 3},
		{"?status=failed", 200, 2},
		{fmt.Sprintf("?playbook_id=%d&status=failed", a), 200, 1},
		{fmt.Sprintf("?playbook_id=%d", b), 200, 1},
		{"?status=bogus", 400, 0},
		{"?playbook_id=abc", 400, 0},
		{"?playbook_id=0", 400, 0},
	} {
		code, n := count(tc.query)
		if code != tc.code || n != tc.n {
			t.Errorf("GET /api/ansible/runs%s = %d with %d runs, want %d with %d", tc.query, code, n, tc.code, tc.n)
		}
	}
}
