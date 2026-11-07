package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

func gitPlaybookBody(extra map[string]interface{}) map[string]interface{} {
	b := map[string]interface{}{
		"name": "from-git", "source": "git", "git_url": "https://github.com/acme/infra.git",
		"git_branch": "main", "git_path": "site.yml", "git_auth": "token", "git_token": "ghp_TOPSECRET",
	}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func TestAnsibleGitPlaybookValidationAndTokenSecrecy(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", gitPlaybookBody(nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create git playbook: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "TOPSECRET") {
		t.Fatalf("token leaked in create response: %s", rec.Body.String())
	}
	var created struct {
		Playbook store.AnsiblePlaybook `json:"playbook"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	pb := created.Playbook
	if pb.Source != "git" || !pb.GitTokenSet || pb.Content != "" {
		t.Fatalf("unexpected playbook: %+v", pb)
	}
	for _, path := range []string{"/api/ansible/playbooks", fmt.Sprintf("/api/ansible/playbooks/%d", pb.ID)} {
		if r := doRequest(t, srv, auth, http.MethodGet, path, nil); strings.Contains(r.Body.String(), "TOPSECRET") {
			t.Fatalf("token leaked by GET %s", path)
		}
	}

	// PUT without a token keeps the stored one.
	rec = doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d", pb.ID),
		gitPlaybookBody(map[string]interface{}{"git_token": "", "git_branch": "dev"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if stored, _ := srv.db.GetAnsiblePlaybook(pb.ID); stored.GitToken != "ghp_TOPSECRET" || stored.GitBranch != "dev" {
		t.Fatalf("token not kept: %+v", stored)
	}

	bad := map[string]map[string]interface{}{
		"file url":           {"git_url": "file:///etc"},
		"option url":         {"git_url": "--upload-pack=id"},
		"embedded creds":     {"git_url": "https://u:p@github.com/a/b.git"},
		"token over ssh":     {"git_url": "git@github.com:a/b.git"},
		"traversal path":     {"git_path": "../x.yml"},
		"non yaml path":      {"git_path": "x.sh"},
		"bad branch":         {"git_branch": "--x"},
		"unknown auth":       {"git_auth": "password"},
		"token missing":      {"git_token": "", "name": "t2"},
		"ssh key missing":    {"git_auth": "ssh_key", "git_url": "git@github.com:a/b.git", "git_key_id": 999},
		"https with ssh key": {"git_auth": "ssh_key", "git_key_id": 1},
	}
	for name, over := range bad {
		over["name"] = "bad-" + strings.ReplaceAll(name, " ", "-")
		if r := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", gitPlaybookBody(over)); r.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, r.Code, r.Body.String())
		}
	}
	// Unknown source value.
	if r := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", map[string]interface{}{"name": "x", "source": "svn", "content": "y"}); r.Code != http.StatusBadRequest {
		t.Errorf("unknown source: %d", r.Code)
	}
}

func TestAnsibleGitRunUsesRepoSourceAndRecordsCommit(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name: "g", Source: "git", GitURL: "https://github.com/acme/infra.git", GitBranch: "main", GitPath: "site.yml",
		GitAuth: "token", GitToken: "tok123",
	})

	var mu sync.Mutex
	var got ansible.RunSpec
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		mu.Lock()
		got = spec
		mu.Unlock()
		spec.OnCommit("0123456789abcdef0123456789abcdef01234567")
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{"host_ids": []int64{hid}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Run store.AnsibleRun `json:"run"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	waitRunFinished(t, srv, res.Run.ID)

	mu.Lock()
	defer mu.Unlock()
	if got.Git == nil || got.Git.URL != "https://github.com/acme/infra.git" || got.Git.Token != "tok123" || got.Git.Path != "site.yml" {
		t.Fatalf("git spec wrong: %+v", got.Git)
	}
	if got.Playbook != "" {
		t.Fatalf("inline content must not be used for git source: %q", got.Playbook)
	}
	if r, _ := srv.db.GetAnsibleRun(res.Run.ID); r.CommitSHA != "0123456789abcdef0123456789abcdef01234567" || r.Trigger != "manual" {
		t.Fatalf("commit/trigger not recorded: %+v", r)
	}
}

func waitRunFinished(t *testing.T, srv *Server, runID int64) *store.AnsibleRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, err := srv.db.GetAnsibleRun(runID)
		if err == nil && r.FinishedAt != nil {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %d did not finish", runID)
	return nil
}

func scheduleBody(pid int64, hostIDs []int64, over map[string]interface{}) map[string]interface{} {
	b := map[string]interface{}{"playbook_id": pid, "name": "nightly", "cron": "*/5 * * * *", "enabled": true, "mode": "check", "host_ids": hostIDs}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func TestAnsibleScheduleCRUDAndValidation(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY", GroupName: "web"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "pb", Content: "x"})

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/schedules", scheduleBody(pid, []int64{hid}, nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Schedule store.AnsibleSchedule `json:"schedule"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	sc := res.Schedule
	if sc.NextRunAt == nil || !sc.NextRunAt.After(time.Now()) || sc.PlaybookName != "pb" {
		t.Fatalf("next_run_at / name wrong: %+v", sc)
	}

	// Disabling clears next_run_at; re-enabling recomputes it.
	url := fmt.Sprintf("/api/ansible/schedules/%d", sc.ID)
	rec = doRequest(t, srv, auth, http.MethodPut, url, scheduleBody(pid, []int64{hid}, map[string]interface{}{"enabled": false}))
	var upd struct {
		Schedule store.AnsibleSchedule `json:"schedule"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &upd)
	if rec.Code != http.StatusOK || upd.Schedule.Enabled || upd.Schedule.NextRunAt != nil {
		t.Fatalf("disable: %d %+v", rec.Code, upd.Schedule)
	}

	if rec = doRequest(t, srv, auth, http.MethodGet, "/api/ansible/schedules?playbook_id="+fmt.Sprint(pid), nil); !strings.Contains(rec.Body.String(), `"nightly"`) {
		t.Fatalf("list: %s", rec.Body.String())
	}

	bad := map[string]map[string]interface{}{
		"bad cron":       {"cron": "61 * * * *"},
		"six fields":     {"cron": "* * * * * *"},
		"impossible":     {"cron": "0 0 31 2 *"},
		"bad mode":       {"mode": "destroy"},
		"bad tags":       {"tags": "a b;rm"},
		"bad extra vars": {"extra_vars": "[1]"},
		"unknown host":   {"host_ids": []int64{9999}},
		"unknown group":  {"host_ids": []int64{}, "group": "nope"},
		"no targets":     {"host_ids": []int64{}},
		"unknown pb":     {"playbook_id": 9999},
	}
	for name, over := range bad {
		if r := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/schedules", scheduleBody(pid, []int64{hid}, over)); r.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, r.Code, r.Body.String())
		}
	}
	// Syntax mode needs no targets; group-only targeting is accepted.
	if r := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/schedules", scheduleBody(pid, nil, map[string]interface{}{"mode": "syntax"})); r.Code != http.StatusCreated {
		t.Errorf("syntax w/o targets: %d %s", r.Code, r.Body.String())
	}
	if r := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/schedules", scheduleBody(pid, nil, map[string]interface{}{"group": "web"})); r.Code != http.StatusCreated {
		t.Errorf("group targeting: %d %s", r.Code, r.Body.String())
	}

	if r := doRequest(t, srv, auth, http.MethodDelete, url, nil); r.Code != http.StatusOK {
		t.Fatalf("delete: %d", r.Code)
	}
	if r := doRequest(t, srv, auth, http.MethodGet, url, nil); r.Code != http.StatusNotFound {
		t.Fatalf("deleted schedule still served: %d", r.Code)
	}
	// Gate off -> 409.
	_ = srv.db.SetAnsibleEnabled(false)
	if r := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/schedules", nil); r.Code != http.StatusConflict {
		t.Fatalf("gate off: %d", r.Code)
	}
}

func TestAnsibleSchedulerTick(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "pb", Content: "x"})

	var mu sync.Mutex
	var specs []ansible.RunSpec
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	now := time.Now().UTC().Truncate(time.Second)
	mk := func(next *time.Time, over func(*store.AnsibleSchedule)) int64 {
		sc := &store.AnsibleSchedule{PlaybookID: pid, Cron: "*/5 * * * *", Enabled: true, Mode: "check", HostIDs: []int64{hid}, NextRunAt: next}
		if over != nil {
			over(sc)
		}
		id, err := srv.db.CreateAnsibleSchedule(sc)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	due := now.Add(-30 * time.Second)
	missed := now.Add(-ansibleMissedGrace - time.Minute)
	future := now.Add(10 * time.Minute)
	dueID := mk(&due, nil)
	missedID := mk(&missed, nil)
	futureID := mk(&future, nil)
	freshID := mk(nil, nil)
	disabledID := mk(&due, func(s *store.AnsibleSchedule) { s.Enabled = false })

	srv.ansibleSchedulerTick(now)
	waitRunFinished(t, srv, func() int64 { s, _ := srv.db.GetAnsibleSchedule(dueID); return s.LastRunID }())

	mu.Lock()
	n := len(specs)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("expected exactly 1 fired run (due), got %d", n)
	}
	d, _ := srv.db.GetAnsibleSchedule(dueID)
	run, _ := srv.db.GetAnsibleRun(d.LastRunID)
	if d.LastError != "" || d.NextRunAt == nil || !d.NextRunAt.After(now) || run.Trigger != "schedule" || run.ScheduleID != dueID || run.Mode != "check" {
		t.Fatalf("fired schedule/run wrong: %+v %+v", d, run)
	}
	m, _ := srv.db.GetAnsibleSchedule(missedID)
	if m.LastRunID != 0 || !strings.Contains(m.LastError, "missed") || m.NextRunAt == nil || !m.NextRunAt.After(now) {
		t.Fatalf("missed occurrence not skipped+advanced: %+v", m)
	}
	if f, _ := srv.db.GetAnsibleSchedule(futureID); f.LastRunID != 0 || !f.NextRunAt.Equal(future) {
		t.Fatalf("future schedule touched: %+v", f)
	}
	if f, _ := srv.db.GetAnsibleSchedule(freshID); f.NextRunAt == nil || f.LastRunID != 0 {
		t.Fatalf("fresh schedule should only get a next time: %+v", f)
	}
	if x, _ := srv.db.GetAnsibleSchedule(disabledID); x.LastRunID != 0 {
		t.Fatalf("disabled schedule fired: %+v", x)
	}

	// A busy playbook is reported, not queued, and the schedule still advances.
	block := make(chan struct{})
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		<-block
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)
	if _, lerr := srv.launchAnsibleRun(mustPlaybook(t, srv, pid), ansibleRunParams{Mode: "check", HostIDs: []int64{hid}}); lerr != nil {
		t.Fatalf("seed run: %v", lerr)
	}
	again := now.Add(-10 * time.Second)
	_ = srv.db.SetAnsibleScheduleNext(dueID, &again)
	srv.ansibleSchedulerTick(now)
	b, _ := srv.db.GetAnsibleSchedule(dueID)
	if !strings.Contains(b.LastError, "not started") || b.NextRunAt == nil || !b.NextRunAt.After(now) {
		t.Fatalf("busy playbook not reported: %+v", b)
	}
	close(block)

	// Gate off: nothing fires.
	_ = srv.db.SetAnsibleEnabled(false)
	off := now.Add(-5 * time.Second)
	_ = srv.db.SetAnsibleScheduleNext(dueID, &off)
	before := countRuns(t, srv)
	srv.ansibleSchedulerTick(now)
	if countRuns(t, srv) != before {
		t.Fatal("scheduler fired while the Ansible Gate is off")
	}
}

func mustPlaybook(t *testing.T, srv *Server, id int64) *store.AnsiblePlaybook {
	t.Helper()
	pb, err := srv.db.GetAnsiblePlaybook(id)
	if err != nil {
		t.Fatal(err)
	}
	return pb
}

func countRuns(t *testing.T, srv *Server) int {
	t.Helper()
	l, err := srv.db.ListAnsibleRuns(0, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	return len(l)
}
