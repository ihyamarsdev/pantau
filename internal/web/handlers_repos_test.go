package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/transfer"
)

func setupReposTestServer(t *testing.T) (*Server, func(*http.Request)) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test_repos.db"))
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, inspector.New(db, nil, nil), nil)
	srv.SetReposDir(filepath.Join(dir, "repos"))
	token := "repos_test_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, func(req *http.Request) { req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token}) }
}

func doRepoReq(t *testing.T, srv *Server, authReq func(*http.Request), method, url string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, url, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	authReq(req)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

func newReposServer(t *testing.T) (*Server, func(method, url string, body interface{}) (int, map[string]interface{}), string) {
	t.Helper()
	srv, auth := setupReposTestServer(t)
	srv.gitProtocols = "file"
	bare := createTestBareRepo(t)
	srv.repoClone = func(ctx context.Context, g ansible.GitSource, dir string, log io.Writer) (string, string, error) {
		if out, err := exec.Command("git", "clone", "--branch", "main", "--", bare, dir).CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("clone: %v %s", err, out)
		}
		return "main", "", nil
	}
	call := func(method, url string, body interface{}) (int, map[string]interface{}) {
		rec := doRepoReq(t, srv, auth, method, url, body)
		m := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &m) // plain-text 404s have no JSON body
		return rec.Code, m
	}
	return srv, call, bare
}

func TestReposValidation(t *testing.T) {
	srv, auth := setupReposTestServer(t)

	for name, body := range map[string]map[string]interface{}{
		"local path url":  {"url": "/etc"},
		"file url":        {"url": "file:///tmp/x"},
		"bad name":        {"url": "https://h.example/o/r.git", "name": "../x"},
		"bad branch":      {"url": "https://h.example/o/r.git", "branch": "--x"},
		"token missing":   {"url": "https://h.example/o/r.git", "auth": "token"},
		"token over ssh":  {"url": "git@h.example:o/r.git", "auth": "token", "token": "t"},
		"ssh without key": {"url": "git@h.example:o/r.git", "auth": "ssh"},
		"unknown auth":    {"url": "https://h.example/o/r.git", "auth": "magic"},
	} {
		if rec := doRepoReq(t, srv, auth, http.MethodPost, "/api/repos", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
}

func TestReposLifecycle(t *testing.T) {
	_, call, bare := newReposServer(t)

	code, repo := cloneRepo(t, call, map[string]interface{}{
		"url": "git@git.example.com:team/infra.git", "auth": "none",
	})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, repo)
	}
	if repo["name"] != "infra" {
		t.Errorf("derived name = %v, want infra", repo["name"])
	}
	id := int64(repo["id"].(float64))
	base := fmt.Sprintf("/api/repos/%d", id)
	dir := repo["dir"].(string)
	if filepath.Base(dir) != "infra" || filepath.Base(filepath.Dir(dir)) != "repos" {
		t.Errorf("clone dir = %s", dir)
	}
	if code, _ := call(http.MethodPost, "/api/repos", map[string]interface{}{"url": "https://h.example/o/INFRA.git"}); code != http.StatusConflict {
		t.Errorf("duplicate name: got %d, want 409", code)
	}

	// Detail: status; folders and search are separate calls.
	code, d := call(http.MethodGet, base, nil)
	if code != http.StatusOK || d["branch"] != "main" || len(d["changes"].([]interface{})) != 0 {
		t.Fatalf("detail: %d %v", code, d)
	}

	if code, r := call(http.MethodGet, base+"/tree?path=", nil); code != http.StatusOK || len(r["entries"].([]interface{})) == 0 {
		t.Errorf("root tree: %d %v", code, r)
	}
	if code, r := call(http.MethodGet, base+"/tree?path=../x", nil); code != http.StatusBadRequest {
		t.Errorf("unsafe tree path: %d %v", code, r)
	}
	if code, r := call(http.MethodGet, base+"/search?q=readme", nil); code != http.StatusOK || len(r["files"].([]interface{})) != 1 {
		t.Errorf("search: %d %v", code, r)
	}

	// Edit -> status shows the change -> file reads back.
	if code, r := call(http.MethodPut, base+"/file", map[string]string{"path": "env/prod/main.tf", "content": "x\n"}); code != http.StatusOK {
		t.Fatalf("write: %d %v", code, r)
	}
	if code, r := call(http.MethodPut, base+"/file", map[string]string{"path": "../escape", "content": "x"}); code != http.StatusBadRequest {
		t.Errorf("traversal write: %d %v", code, r)
	}
	if code, r := call(http.MethodPut, base+"/file", map[string]string{"path": "bad.bin", "content": "a\x00b"}); code != http.StatusUnsupportedMediaType {
		t.Errorf("binary write: %d %v", code, r)
	}
	if code, r := call(http.MethodGet, base+"/file?path=env/prod/main.tf", nil); code != http.StatusOK || r["content"] != "x\n" {
		t.Errorf("read: %d %v", code, r)
	}
	if code, r := call(http.MethodGet, base+"/file?path=bad_bin.dat", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("read binary: %d %v", code, r)
	}
	if code, r := call(http.MethodGet, base+"/file?path=nope.txt", nil); code != http.StatusNotFound {
		t.Errorf("read missing: %d %v", code, r)
	}
	_, d = call(http.MethodGet, base, nil)
	if ch := d["changes"].([]interface{}); len(ch) != 1 || ch[0].(map[string]interface{})["status"] != "new" {
		t.Fatalf("changes = %v", ch)
	}

	// Discard removes the new file.
	if code, r := call(http.MethodPost, base+"/discard", map[string]string{"path": "env/prod/main.tf"}); code != http.StatusOK {
		t.Fatalf("discard: %d %v", code, r)
	}
	if _, d = call(http.MethodGet, base, nil); len(d["changes"].([]interface{})) != 0 {
		t.Fatalf("discard left changes: %v", d["changes"])
	}

	// Commit + push reaches the remote; empty commit is a 400.
	call(http.MethodPut, base+"/file", map[string]string{"path": "README.md", "content": "# edited\n"})
	if code, r := call(http.MethodPost, base+"/commit", map[string]interface{}{"message": "docs", "author_name": "A", "author_email": "a@x.io", "paths": []string{"README.md"}}); code != http.StatusOK || r["pushed"] != true || r["branch"] != "main" {
		t.Fatalf("commit: %d %v", code, r)
	}
	if out, err := exec.Command("git", "--git-dir", bare, "log", "-1", "--format=%s", "main").Output(); err != nil || strings.TrimSpace(string(out)) != "docs" {
		t.Errorf("remote head = %q, %v", out, err)
	}
	if code, r := call(http.MethodPost, base+"/commit", map[string]interface{}{"message": "again"}); code != http.StatusBadRequest {
		t.Errorf("empty commit: %d %v", code, r)
	}
	if code, r := call(http.MethodPost, base+"/commit", map[string]interface{}{"message": ""}); code != http.StatusBadRequest {
		t.Errorf("empty message: %d %v", code, r)
	}
	if code, r := call(http.MethodPost, base+"/sync", nil); code != http.StatusOK || r["behind"] != float64(0) {
		t.Errorf("sync: %d %v", code, r)
	}
	if code, r := call(http.MethodPost, base+"/push", nil); code != http.StatusOK {
		t.Errorf("push: %d %v", code, r)
	}

	// List carries the working-tree summary.
	if _, l := call(http.MethodGet, "/api/repos", nil); len(l["repos"].([]interface{})) != 1 {
		t.Errorf("list = %v", l)
	}

	// Credentials: token is write-only.
	if code, r := call(http.MethodPut, base, map[string]interface{}{"auth": "token", "token": "s3cret"}); code != http.StatusBadRequest {
		t.Errorf("token over scp url: %d %v", code, r) // repo URL is ssh-style: tokens need https
	}

	// Delete removes the folder and the row.
	if code, _ := call(http.MethodDelete, base, nil); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("clone folder still exists after delete")
	}
	if code, _ := call(http.MethodGet, base, nil); code != http.StatusNotFound {
		t.Errorf("get after delete: %d", code)
	}
}

func TestRepoFolderSetting(t *testing.T) {
	_, call, _ := newReposServer(t)
	if code, _ := call(http.MethodPut, "/api/repos/folder", map[string]string{"folder": "relative/dir"}); code != http.StatusBadRequest {
		t.Errorf("relative folder: got %d, want 400", code)
	}
	if code, _ := call(http.MethodPut, "/api/repos/folder", map[string]string{"folder": "/"}); code != http.StatusBadRequest {
		t.Errorf("root folder: got %d, want 400", code)
	}
	custom := filepath.Join(t.TempDir(), "my", "repos")
	if code, r := call(http.MethodPut, "/api/repos/folder", map[string]string{"folder": custom}); code != http.StatusOK || r["folder"] != custom || r["custom"] != true {
		t.Fatalf("set folder: %d %v", code, r)
	}
	code, repo := cloneRepo(t, call, map[string]interface{}{"url": "https://h.example/o/web.git"})
	if code != http.StatusCreated || repo["dir"] != filepath.Join(custom, "web") {
		t.Fatalf("clone into custom folder: %d %v", code, repo)
	}
	if code, r := call(http.MethodPut, "/api/repos/folder", map[string]string{"folder": ""}); code != http.StatusOK || r["custom"] != false {
		t.Errorf("reset folder: %d %v", code, r)
	}
}

type repoCall func(method, url string, body interface{}) (int, map[string]interface{})

// waitJob polls a clone job until done(job) holds.
func waitJob(t *testing.T, call repoCall, id string, done func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	for i := 0; i < 200; i++ {
		code, j := call(http.MethodGet, "/api/repos/clone/"+id, nil)
		if code != http.StatusOK {
			t.Fatalf("job status: %d %v", code, j)
		}
		if done(j) {
			return j
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("clone job did not reach the expected state")
	return nil
}

// cloneRepo starts a clone and waits for it: 201 with the repository, or 500 with the job when it failed.
func cloneRepo(t *testing.T, call repoCall, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	code, r := call(http.MethodPost, "/api/repos", body)
	if code != http.StatusAccepted {
		return code, r
	}
	j := waitJob(t, call, r["job"].(string), func(j map[string]interface{}) bool { return j["state"] != "running" })
	if j["state"] != "done" {
		return http.StatusInternalServerError, j
	}
	return http.StatusCreated, j["repo"].(map[string]interface{})
}

func TestRepoCloneJobProgressCancelAndRedaction(t *testing.T) {
	srv, call, _ := newReposServer(t)
	srv.repoClone = func(ctx context.Context, g ansible.GitSource, dir string, log io.Writer) (string, string, error) {
		fmt.Fprint(log, "Cloning into 'x'...\nReceiving objects:  50%\rReceiving objects: 100%\n")
		fmt.Fprint(log, "fatal: header Authorization: Basic Zm9vOmJhcg==\n")
		fmt.Fprint(log, "Resolving deltas:  10%\rResolving deltas:  60%") // unfinished progress line stays visible
		<-ctx.Done()
		return "", "", ctx.Err()
	}
	code, r := call(http.MethodPost, "/api/repos", map[string]interface{}{"url": "https://h.example/o/slow.git"})
	if code != http.StatusAccepted {
		t.Fatalf("start: %d %v", code, r)
	}
	id := r["job"].(string)
	j := waitJob(t, call, id, func(j map[string]interface{}) bool { return len(j["lines"].([]interface{})) >= 4 })
	var lines []string
	for _, l := range j["lines"].([]interface{}) {
		lines = append(lines, l.(string))
	}
	want := []string{"Cloning into 'x'...", "Receiving objects: 100%", "fatal: header Authorization: <redacted>", "Resolving deltas:  60%"}
	if strings.Join(lines, "|") != strings.Join(want, "|") || j["state"] != "running" {
		t.Errorf("progress = %q state %v, want %q running (in-place updates collapsed, credentials redacted)", lines, j["state"], want)
	}
	if code, _ := call(http.MethodPost, "/api/repos", map[string]interface{}{"url": "https://h.example/o/slow.git"}); code != http.StatusConflict {
		t.Errorf("same repository twice: got %d, want 409", code)
	}

	if code, _ := call(http.MethodDelete, "/api/repos/clone/"+id, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	j = waitJob(t, call, id, func(j map[string]interface{}) bool { return j["state"] != "running" })
	if j["state"] != "canceled" {
		t.Errorf("state after cancel = %v", j["state"])
	}
	if _, l := call(http.MethodGet, "/api/repos", nil); len(l["repos"].([]interface{})) != 0 {
		t.Errorf("a canceled clone must not register a repository: %v", l)
	}
	if code, _ := call(http.MethodGet, "/api/repos/clone/nope", nil); code != http.StatusNotFound {
		t.Errorf("unknown job: got %d, want 404", code)
	}
}

func TestRepoFoldersGroupRepositoriesAndMoveThem(t *testing.T) {
	srv, call, _ := newReposServer(t)
	base := srv.reposDir
	create := func(name, group string) map[string]interface{} {
		t.Helper()
		code, r := cloneRepo(t, call, map[string]interface{}{"url": "https://h.example/o/" + name + ".git", "group": group})
		if code != http.StatusCreated {
			t.Fatalf("create %s in %q: %d %v", name, group, code, r)
		}
		return r
	}
	a := create("a", "Torcons")
	b := create("b", "torcons") // same folder, other spelling: reuses the existing one
	c := create("c", "")
	if a["dir"] != filepath.Join(base, "Torcons", "a") || b["group"] != "Torcons" || b["dir"] != filepath.Join(base, "Torcons", "b") || c["dir"] != filepath.Join(base, "c") || c["group"] != "" {
		t.Fatalf("dirs/groups: a=%v b=%v c=%v", a["dir"], b, c["dir"])
	}
	_, l := call(http.MethodGet, "/api/repos", nil)
	counts := map[string]int{}
	for _, r := range l["repos"].([]interface{}) {
		counts[r.(map[string]interface{})["group"].(string)]++
	}
	if counts["Torcons"] != 2 || counts[""] != 1 {
		t.Errorf("list groups = %v", counts)
	}

	for _, g := range []string{"../x", "a b", ".hidden", "x/y"} {
		if code, _ := call(http.MethodPost, "/api/repos", map[string]interface{}{"url": "https://h.example/o/z.git", "group": g}); code != http.StatusBadRequest {
			t.Errorf("folder %q: got %d, want 400", g, code)
		}
	}
	// A folder named like an existing clone would put a repository inside another one.
	if code, _ := call(http.MethodPost, "/api/repos", map[string]interface{}{"url": "https://h.example/o/z.git", "group": "c"}); code != http.StatusBadRequest {
		t.Errorf("folder that is a repository: got %d, want 400", code)
	}

	cid := int64(c["id"].(float64))
	if code, r := call(http.MethodPut, fmt.Sprintf("/api/repos/%d/group", cid), map[string]string{"group": "TORCONS"}); code != http.StatusOK || r["dir"] != filepath.Join(base, "Torcons", "c") {
		t.Fatalf("move c into folder: %d %v", code, r)
	}
	if _, err := os.Stat(filepath.Join(base, "c")); err == nil {
		t.Error("old directory still exists after the move")
	}
	if code, r := call(http.MethodGet, fmt.Sprintf("/api/repos/%d", cid), nil); code != http.StatusOK || r["branch"] != "main" {
		t.Errorf("moved clone must still open: %d %v", code, r)
	}

	// Moving the last repositories out removes the emptied folder; deleting the last one does too.
	for _, r := range []map[string]interface{}{a, b} {
		if code, x := call(http.MethodPut, fmt.Sprintf("/api/repos/%d/group", int64(r["id"].(float64))), map[string]string{"group": "Other"}); code != http.StatusOK {
			t.Fatalf("move to Other: %d %v", code, x)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "Torcons", "c")); err != nil {
		t.Fatalf("c must stay in Torcons: %v", err)
	}
	if code, _ := call(http.MethodPut, fmt.Sprintf("/api/repos/%d/group", cid), map[string]string{"group": ""}); code != http.StatusOK {
		t.Fatalf("move c out: %d", code)
	}
	if _, err := os.Stat(filepath.Join(base, "Torcons")); err == nil {
		t.Error("emptied folder was not removed after the last repository left")
	}
	if code, _ := call(http.MethodDelete, fmt.Sprintf("/api/repos/%d", int64(a["id"].(float64))), nil); code != http.StatusOK {
		t.Fatal("delete a")
	}
	if _, err := os.Stat(filepath.Join(base, "Other", "b")); err != nil {
		t.Errorf("b must survive deleting a: %v", err)
	}
	if code, _ := call(http.MethodDelete, fmt.Sprintf("/api/repos/%d", int64(b["id"].(float64))), nil); code != http.StatusOK {
		t.Fatal("delete b")
	}
	if _, err := os.Stat(filepath.Join(base, "Other")); err == nil {
		t.Error("folder left behind after its last repository was deleted")
	}
}

func TestRepoCopyToHost(t *testing.T) {
	srv, call, _ := newReposServer(t)
	mock := sshrunner.NewMockRunner()
	srv.SetTransferManager(transfer.NewManager(srv.db, func(string, int, string, string) (sshrunner.Runner, error) { return mock, nil }))
	hostID, err := srv.db.CreateHost(&store.Host{Name: "web1", Host: "10.0.0.1", Port: 22, User: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	_, repo := cloneRepo(t, call, map[string]interface{}{"url": "https://h.example/o/site.git", "group": "Torcons"})
	url := fmt.Sprintf("/api/repos/%d/copy", int64(repo["id"].(float64)))

	for name, body := range map[string]map[string]interface{}{
		"relative path":   {"host_id": hostID, "path": "srv/site"},
		"protected path":  {"host_id": hostID, "path": "/etc/nginx"},
		"filesystem root": {"host_id": hostID, "path": "/"},
		"no host":         {"path": "/srv/site"},
		"unknown host":    {"host_id": 9999, "path": "/srv/site"},
	} {
		if code, r := call(http.MethodPost, url, body); code != http.StatusBadRequest {
			t.Errorf("%s: got %d %v, want 400", name, code, r)
		}
	}

	code, r := call(http.MethodPost, url, map[string]interface{}{"host_id": hostID, "path": "/srv/site"})
	if code != http.StatusAccepted || r["files"].(float64) < 1 {
		t.Fatalf("copy: %d %v", code, r)
	}
	job := r["job"].(map[string]interface{})
	if job["kind"] != "repo" || job["source_path"] != "Torcons/site" || job["dest_host_name"] != "web1" || job["dest_path"] != "/srv/site" {
		t.Errorf("job = %v", job)
	}
	id := int64(job["id"].(float64))
	for i := 0; i < 200; i++ {
		_, j := call(http.MethodGet, fmt.Sprintf("/api/transfers/%d", id), nil)
		if j["status"] == "completed" {
			break
		}
		if j["status"] == "failed" {
			t.Fatalf("copy failed: %v", j["error"])
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, j := call(http.MethodGet, fmt.Sprintf("/api/transfers/%d", id), nil); j["status"] != "completed" {
		t.Fatalf("copy did not finish: %v", j)
	}
	if !strings.Contains(strings.Join(mock.ExecHistory, "\n"), "mkdir -p '/srv/site'") {
		t.Errorf("host commands = %v", mock.ExecHistory)
	}
}
