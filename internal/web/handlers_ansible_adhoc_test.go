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

// setupAdhocTestServer enables the ansible gate and installs a fake ad-hoc runner that writes a realistic log.
func setupAdhocTestServer(t *testing.T) (*Server, func(*http.Request), *adhocFakeRunner) {
	t.Helper()
	srv, auth := setupAnsibleTestServer(t)
	if err := srv.db.SetAnsibleEnabled(true); err != nil {
		t.Fatalf("enable ansible: %v", err)
	}
	fake := &adhocFakeRunner{}
	srv.adhocRunner = fake.run
	srv.ansibleAdhocMgr = NewAnsibleAdhocManager(srv.db, srv.adhocRunner)
	return srv, auth, fake
}

type adhocFakeRunner struct {
	mu    sync.Mutex
	calls []ansible.AdhocSpec
	err   error
}

func (f *adhocFakeRunner) run(ctx context.Context, spec ansible.AdhocSpec, log io.Writer) (int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, spec)
	err := f.err
	f.mu.Unlock()

	if err != nil {
		return 0, err
	}
	_, _ = io.WriteString(log, "web-1 | SUCCESS => {\n")
	_, _ = io.WriteString(log, "    \"ping\": \"pong\"\n}\n")
	return 0, nil
}

func (f *adhocFakeRunner) called() []ansible.AdhocSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ansible.AdhocSpec(nil), f.calls...)
}

// waitCalls blocks until the runner has been called at least n times (the run is started from a goroutine).
func (f *adhocFakeRunner) waitCalls(t *testing.T, n int) []ansible.AdhocSpec {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if calls := f.called(); len(calls) >= n {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner called %d times, want %d", len(f.called()), n)
	return nil
}

func createAdhocHost(t *testing.T, srv *Server, name, group string) int64 {
	t.Helper()
	id, err := srv.db.CreateHost(&store.Host{Name: name, Host: "10.0.0.9", Port: 22, User: "root", GroupName: group})
	if err != nil {
		t.Fatalf("CreateHost(%s): %v", name, err)
	}
	return id
}

func waitAdhocFinished(t *testing.T, srv *Server, id int64) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := srv.db.GetAnsibleAdhocRun(id)
		if err == nil && (run.Status == "success" || run.Status == "failed" || run.Status == "cancelled") {
			return map[string]interface{}{"status": run.Status}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %d did not finish", id)
	return nil
}

func TestAnsibleAdhocGateDisabled(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/adhoc", nil); rec.Code != http.StatusConflict {
		t.Fatalf("GET adhoc with gate disabled = %d, want 409", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/explorer", nil); rec.Code != http.StatusConflict {
		t.Fatalf("GET explorer with gate disabled = %d, want 409", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/hosts/1/facts", nil); rec.Code != http.StatusConflict {
		t.Fatalf("GET facts with gate disabled = %d, want 409", rec.Code)
	}
}

func TestAnsibleAdhocRequiresAuth(t *testing.T) {
	srv, _, _ := setupAdhocTestServer(t)
	if rec := doRequest(t, srv, nil, http.MethodGet, "/api/ansible/adhoc", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated adhoc = %d, want 401", rec.Code)
	}
}

func TestAnsibleAdhocCreateRunAndStreamLog(t *testing.T) {
	srv, auth, fake := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "web")

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", map[string]interface{}{
		"module": "ping", "host_ids": []int64{hostID}, "verbosity": 2,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST adhoc = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Run struct {
			ID      int64  `json:"id"`
			Status  string `json:"status"`
			Pattern string `json:"pattern"`
		} `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Run.ID == 0 || created.Run.Pattern != "all" {
		t.Fatalf("created run = %+v", created.Run)
	}

	calls := fake.waitCalls(t, 1)
	if calls[0].Module != "ping" || len(calls[0].Targets) != 1 || calls[0].Targets[0].Name != "web-1" || calls[0].Verbosity != 2 {
		t.Fatalf("spec = %+v", calls[0])
	}

	// waitAdhocFinished checks the DB; the manager records the in-memory log/status a hair later, so poll the
	// endpoint until it reports the finished run instead of racing it.
	waitAdhocFinished(t, srv, created.Run.ID)

	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/adhoc/%d", created.Run.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET adhoc run = %d: %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Run struct {
			Status  string                `json:"status"`
			Targets []targetSnapshot      `json:"targets"`
			Results []ansible.AdhocResult `json:"results"`
			Counts  ansible.AdhocCounts   `json:"counts"`
		} `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Run.Status != "success" || len(detail.Run.Targets) != 1 || detail.Run.Targets[0].Name != "web-1" {
		t.Fatalf("detail = %+v", detail.Run)
	}
	if len(detail.Run.Results) != 1 || detail.Run.Results[0].Alias != "web-1" || detail.Run.Results[0].Status != ansible.AdhocSuccess {
		t.Fatalf("results = %+v", detail.Run.Results)
	}
	if detail.Run.Counts.Success != 1 {
		t.Fatalf("counts = %+v", detail.Run.Counts)
	}

	type logResponse struct {
		Log      string `json:"log"`
		Offset   int    `json:"offset"`
		Status   string `json:"status"`
		Finished bool   `json:"finished"`
	}
	var logResp logResponse
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/adhoc/%d/log", created.Run.ID), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET adhoc log = %d: %s", rec.Code, rec.Body.String())
		}
		logResp = logResponse{}
		if err := json.Unmarshal(rec.Body.Bytes(), &logResp); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		if logResp.Finished {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !logResp.Finished || !strings.Contains(logResp.Log, "web-1 | SUCCESS") {
		t.Fatalf("log = %+v", logResp)
	}

	rec = doRequest(t, srv, auth, http.MethodGet, "/api/ansible/adhoc?limit=10", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"runs"`) {
		t.Fatalf("list adhoc = %d: %s", rec.Code, rec.Body.String())
	}

	// Cancelling a finished run is a conflict, not a silent success.
	if rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/adhoc/%d/cancel", created.Run.ID), nil); rec.Code != http.StatusConflict {
		t.Fatalf("cancel finished run = %d, want 409", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/adhoc/999", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run = %d, want 404", rec.Code)
	}
}

func TestAnsibleAdhocValidation(t *testing.T) {
	srv, auth, fake := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "")

	tests := []struct {
		name string
		body map[string]interface{}
		code int
	}{
		{"flag module", map[string]interface{}{"module": "--force", "host_ids": []int64{hostID}}, http.StatusBadRequest},
		{"bad pattern", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}, "pattern": "web 1"}, http.StatusBadRequest},
		{"newline args", map[string]interface{}{"module": "shell", "args": "echo a\nid", "host_ids": []int64{hostID}}, http.StatusBadRequest},
		{"no targets", map[string]interface{}{"module": "ping"}, http.StatusBadRequest},
		{"unknown host", map[string]interface{}{"module": "ping", "host_ids": []int64{424242}}, http.StatusBadRequest},
		{"unknown group", map[string]interface{}{"module": "ping", "group": "nope"}, http.StatusBadRequest},
		{"extra vars", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}, "extra_vars": "{bad"}, http.StatusBadRequest},
		{"verbosity", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}, "verbosity": 9}, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", tc.body); rec.Code != tc.code {
				t.Fatalf("POST = %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
		})
	}
	if calls := fake.called(); len(calls) != 0 {
		t.Fatalf("runner must not be called for invalid requests, got %d calls", len(calls))
	}
}

func TestAnsibleAdhocGroupSelection(t *testing.T) {
	srv, auth, fake := setupAdhocTestServer(t)
	createAdhocHost(t, srv, "web-1", "web")
	createAdhocHost(t, srv, "web-2", "web")
	createAdhocHost(t, srv, "db-1", "db")

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", map[string]interface{}{
		"module": "setup", "args": "gather_subset=min", "group": "web", "pattern": "web-*",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body.String())
	}
	calls := fake.waitCalls(t, 1)
	if len(calls[0].Targets) != 2 {
		t.Fatalf("spec targets = %+v", calls)
	}
	if calls[0].Pattern != "web-*" || calls[0].Args != "gather_subset=min" {
		t.Fatalf("spec = %+v", calls[0])
	}
}

func TestAnsibleAdhocCancelActiveRun(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "")

	block := make(chan struct{})
	srv.adhocRunner = func(ctx context.Context, spec ansible.AdhocSpec, log io.Writer) (int, error) {
		close(block)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	srv.ansibleAdhocMgr = NewAnsibleAdhocManager(srv.db, srv.adhocRunner)

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	<-block
	if rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/adhoc/%d/cancel", created.Run.ID), nil); rec.Code != http.StatusOK {
		t.Fatalf("cancel = %d: %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := srv.db.GetAnsibleAdhocRun(created.Run.ID)
		if err == nil && run.Status == "cancelled" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run was not marked cancelled")
}

func TestAnsibleAdhocRunnerErrorMarksFailed(t *testing.T) {
	srv, auth, fake := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "")
	fake.err = fmt.Errorf("ansible executable not found")

	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}})
	var created struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := srv.db.GetAnsibleAdhocRun(created.Run.ID)
		if err == nil && run.Status == "failed" {
			if !strings.Contains(run.Log, "ansible executable not found") {
				t.Fatalf("log = %q, want the runner error", run.Log)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run was not marked failed")
}

func TestAnsibleAdhocConcurrencyLimit(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "")

	release := make(chan struct{})
	srv.adhocRunner = func(ctx context.Context, spec ansible.AdhocSpec, log io.Writer) (int, error) {
		<-release
		return 0, nil
	}
	srv.ansibleAdhocMgr = NewAnsibleAdhocManager(srv.db, srv.adhocRunner)
	defer close(release)

	for i := 0; i < MaxConcurrentAdhocRuns; i++ {
		if rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}}); rec.Code != http.StatusCreated {
			t.Fatalf("run %d = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/adhoc", map[string]interface{}{"module": "ping", "host_ids": []int64{hostID}})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit run = %d, want 429: %s", rec.Code, rec.Body.String())
	}

	// The rejected run must not linger as queued.
	runs, err := srv.db.ListAnsibleAdhocRuns(0)
	if err != nil {
		t.Fatal(err)
	}
	var failed int
	for _, r := range runs {
		if r.Status == "failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("rejected run status: %+v", runs)
	}
}
