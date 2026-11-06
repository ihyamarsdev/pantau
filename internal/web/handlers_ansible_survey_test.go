package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

// createPlaybook makes a playbook and fails the test if the DB refuses it.
func createPlaybook(t *testing.T, srv *Server, name, extraVars string) int64 {
	t.Helper()
	id, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: name, Content: "---\n- hosts: all\n", ExtraVars: extraVars})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}
	return id
}

func putSurvey(t *testing.T, srv *Server, auth func(*http.Request), id int64, fields []ansible.SurveyField) {
	t.Helper()

	rec := doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d/survey", id), map[string]interface{}{"survey": fields})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT survey: %d %s", rec.Code, rec.Body.String())
	}
}

// bodyOf decodes a JSON response into a generic object via the package's decodeBody.
func bodyOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	m := map[string]interface{}{}
	decodeBody(t, rec, &m)
	return m
}

func TestAnsibleSurveyCRUD(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	id := createPlaybook(t, srv, "Survey PB", "")

	// No survey yet: GET answers an empty array, not null.
	rec := doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/survey", id), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET survey: %d %s", rec.Code, rec.Body.String())
	}
	body := bodyOf(t, rec)
	if body["has_survey"] != false || len(body["survey"].([]interface{})) != 0 {
		t.Fatalf("unexpected empty survey body: %v", body)
	}

	fields := []ansible.SurveyField{
		{Key: "env", Label: "Environment", Type: "select", Required: true, Options: []string{"dev", "prod"}, Default: "dev"},
		{Key: "replicas", Label: "Replicas", Type: "integer", Min: new(1.0), Max: new(5.0), Default: 2.0},
		{Key: "token", Label: "Token", Type: "password"},
	}
	putSurvey(t, srv, auth, id, fields)

	// GET playbook carries the parsed survey and has_survey.
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d", id), nil)
	pb := bodyOf(t, rec)["playbook"].(map[string]interface{})
	if pb["has_survey"] != true || len(pb["survey"].([]interface{})) != 3 {
		t.Fatalf("playbook GET must carry survey/has_survey: %v", pb)
	}
	rec = doRequest(t, srv, auth, http.MethodGet, "/api/ansible/playbooks", nil)
	if first := bodyOf(t, rec)["playbooks"].([]interface{})[0].(map[string]interface{}); first["has_survey"] != true {
		t.Fatalf("playbook list must carry has_survey: %v", first)
	}

	// An invalid definition is rejected with the validator's message and nothing stored.
	rec = doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d/survey", id),
		map[string]interface{}{"survey": []map[string]interface{}{{"key": "bad key", "type": "text"}}})
	if rec.Code != http.StatusBadRequest || !containsSub(rec.Body.String(), "key must match") {
		t.Fatalf("invalid survey: %d %s", rec.Code, rec.Body.String())
	}

	// Clearing removes the survey.
	rec = doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d/survey", id), map[string]interface{}{"survey": []any{}})
	if rec.Code != http.StatusOK || bodyOf(t, rec)["has_survey"] != false {
		t.Fatalf("clear survey: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnsibleSurveyGateOff(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	id := createPlaybook(t, srv, "Gate PB", "")

	for _, req := range []struct{ method, url string }{
		{http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/survey", id)},
		{http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d/survey", id)},
		{http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/sync", id)},
	} {
		rec := doRequest(t, srv, auth, req.method, req.url, map[string]interface{}{"survey": []any{}})
		if rec.Code != http.StatusConflict {
			t.Errorf("%s %s with gate off: expected 409, got %d", req.method, req.url, rec.Code)
		}
	}
}

func TestAnsibleSurveyLaunchMerge(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	hid, _ := srv.db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})

	// Answering in the runner seam captures the exact spec the runner receives, without depending on the
	// manager's goroutine timing.
	type captured struct{ vars string }
	got := make(chan captured, 1)
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		_ = log
		select {
		case got <- captured{spec.ExtraVars}:
		default:
		}
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	fields := []ansible.SurveyField{
		{Key: "env", Type: "select", Required: true, Options: []string{"dev", "prod"}, Default: "dev"},
		{Key: "replicas", Type: "integer", Min: new(1.0), Max: new(5.0), Default: 2.0},
		{Key: "token", Type: "password"},
	}
	id := createPlaybook(t, srv, "Launch PB", `{"env":"from-playbook","region":"eu"}`)
	putSurvey(t, srv, auth, id, fields)

	// Unanswered survey: defaults fill in, the playbook default is overridden by the survey default.
	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", id),
		map[string]interface{}{"host_ids": []int64{hid}, "survey_answers": map[string]interface{}{"token": "s3cret"}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
	runID := int64(bodyOf(t, rec)["run"].(map[string]interface{})["id"].(float64))
	spec := <-got
	vars := decodeVars(t, spec.vars)
	if vars["env"] != "dev" || vars["replicas"] != float64(2) || vars["region"] != "eu" || vars["token"] != "s3cret" {
		t.Fatalf("runner must get survey defaults merged over the playbook defaults and the real password: %v", vars)
	}

	// The stored record masks the password but keeps everything else.
	run, err := srv.db.GetAnsibleRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	record := decodeVars(t, run.ExtraVars)
	if record["token"] != "(hidden)" || record["env"] != "dev" {
		t.Fatalf("stored extra_vars must hide survey passwords: %v", record)
	}

	// Explicit launch extra_vars beat the survey; answers override the playbook default.
	id2 := createPlaybook(t, srv, "Launch PB 2", `{"env":"from-playbook","region":"eu"}`)
	putSurvey(t, srv, auth, id2, fields)

	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", id2), map[string]interface{}{
		"host_ids":       []int64{hid},
		"extra_vars":     `{"env":"from-launch","zone":"a"}`,
		"survey_answers": map[string]interface{}{"env": "prod", "replicas": "4"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run 2: %d %s", rec.Code, rec.Body.String())
	}
	vars = decodeVars(t, (<-got).vars)
	if vars["env"] != "from-launch" || vars["replicas"] != float64(4) || vars["region"] != "eu" || vars["zone"] != "a" {
		t.Fatalf("launch extra_vars must win over survey answers: %v", vars)
	}

	// Bad answers are a 400 and create no run.
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", id2), map[string]interface{}{
		"host_ids": []int64{hid}, "survey_answers": map[string]interface{}{"replicas": 99},
	})
	if rec.Code != http.StatusBadRequest || !containsSub(rec.Body.String(), "replicas") {
		t.Fatalf("out-of-range answer: %d %s", rec.Code, rec.Body.String())
	}
	// Unknown answer keys are a 400 too.
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", id2), map[string]interface{}{
		"host_ids": []int64{hid}, "survey_answers": map[string]interface{}{"nope": 1},
	})
	if rec.Code != http.StatusBadRequest || !containsSub(rec.Body.String(), "unknown survey answer") {
		t.Fatalf("unknown answer: %d %s", rec.Code, rec.Body.String())
	}
}

func decodeVars(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	vars := map[string]interface{}{}
	if raw == "" {
		return vars
	}
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		t.Fatalf("extra_vars %q: %v", raw, err)
	}
	return vars
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAnsibleGitSyncRejectsInline(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	id := createPlaybook(t, srv, "Inline PB", "")
	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/sync", id), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline sync: expected 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnsibleGitSyncRecordsResult(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	// The handler's own work is the clone, covered against a real repository in the ansible package; here the
	// seam replaces it so the persistence, the response shape and the failure path are testable.
	var calledWith ansible.GitSource
	srv.ansibleSync = func(ctx context.Context, g ansible.GitSource) (*ansible.GitSyncResult, error) {
		_ = ctx
		calledWith = g
		return &ansible.GitSyncResult{Commit: "0123456789abcdef0123456789abcdef01234567", Branch: "main", Files: []string{"site.yml"}}, nil
	}

	pb, err := srv.db.GetAnsiblePlaybook(createPlaybook(t, srv, "Git PB", ""))
	if err != nil {
		t.Fatal(err)
	}
	pb.Source, pb.GitURL, pb.GitBranch, pb.GitPath = "git", "https://example.com/acme/infra.git", "main", "site.yml"
	if err := srv.db.UpdateAnsiblePlaybook(pb); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/sync", pb.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body.String())
	}
	body := bodyOf(t, rec)
	if body["ok"] != true || body["commit"] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("unexpected sync body: %v", body)
	}
	if got := body["files"].([]interface{}); len(got) != 1 || got[0] != "site.yml" {
		t.Fatalf("files: %v", body["files"])
	}
	if calledWith.URL != "https://example.com/acme/infra.git" || calledWith.Branch != "main" {
		t.Fatalf("handler passed %+v", calledWith)
	}

	after, err := srv.db.GetAnsiblePlaybook(pb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastSyncCommit != "0123456789abcdef0123456789abcdef01234567" || after.LastSyncAt == nil {
		t.Fatalf("last_sync_* not recorded: %+v", after)
	}

	// A failed clone answers 502 and leaves the previous sync record untouched.
	srv.ansibleSync = func(ctx context.Context, g ansible.GitSource) (*ansible.GitSyncResult, error) {
		_ = ctx
		return nil, fmt.Errorf("git clone failed (exit 128)")
	}
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/sync", pb.ID), nil)
	if rec.Code != http.StatusBadGateway || !containsSub(rec.Body.String(), "git clone failed") {
		t.Fatalf("failed sync: %d %s", rec.Code, rec.Body.String())
	}
	after, _ = srv.db.GetAnsiblePlaybook(pb.ID)
	if after.LastSyncCommit != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("failed sync must not clear the recorded commit: %+v", after)
	}
}
