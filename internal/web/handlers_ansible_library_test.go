package web

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/ansible"
)

// fakeGalaxy puts an `ansible-galaxy` stand-in first in PATH. It logs its arguments and lays out what the real
// tool would: a collection under <dest>/ansible_collections, a role under <dest>. It fails while <log>.fail exists.
func fakeGalaxy(t *testing.T) (logPath string) {
	t.Helper()
	bin := t.TempDir()
	logPath = filepath.Join(bin, "calls.log")
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
if [ -e "` + logPath + `.fail" ]; then echo "ERROR: boom, no such item" >&2; exit 1; fi
if [ -e "` + logPath + `.wait" ]; then echo "step 1: downloading"; while [ -e "` + logPath + `.wait" ]; do sleep 0.05; done; fi
kind=$1; dest=; spec=
shift
while [ $# -gt 0 ]; do
  case "$1" in -p) dest=$2; shift ;; -*) ;; *) spec=$1 ;; esac
  shift
done
case "$spec" in
  /*) name=local.col; version=0.0.1 ;;
  *:*) name=${spec%%:*}; version=${spec#*:} ;;
  *,*) name=${spec%%,*}; version=${spec#*,} ;;
  *) name=$spec; version=1.0.0 ;;
esac
if [ "$kind" = collection ]; then
  d="$dest/ansible_collections/${name%%.*}/${name#*.}"
  mkdir -p "$d"; printf '{"collection_info":{"version":"%s"}}' "$version" > "$d/MANIFEST.json"
else
  mkdir -p "$dest/$name/meta"; printf 'version: %s\n' "$version" > "$dest/$name/meta/.galaxy_install_info"
fi
echo "installed $name $version"
`
	if err := os.WriteFile(filepath.Join(bin, "ansible-galaxy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

type libraryResponse struct {
	Available   bool                  `json:"available"`
	Collections []ansible.LibraryItem `json:"collections"`
	Roles       []ansible.LibraryItem `json:"roles"`
	Output      string                `json:"output"`
	Error       string                `json:"error"`
}

func (e *ansibleTestEnv) library(t *testing.T, method, url string, body any) (int, libraryResponse) {
	t.Helper()
	rec := doRequest(t, e.srv, e.auth, method, url, body)
	var out libraryResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// ndjson reads an install stream: the concatenated log, the final error ("" on success) and whether the done line
// arrived. A non-stream (plain JSON) response yields done=false.
func ndjson(rec *httptest.ResponseRecorder) (log, errMsg string, done bool) {
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/x-ndjson") {
		return "", "", false
	}
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		var line struct {
			Log   string
			Done  bool
			Error string
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil {
			log += line.Log
			if line.Done {
				done, errMsg = true, line.Error
			}
		}
	}
	return
}

type installResult struct {
	code     int
	log, err string
	done     bool
}

// install posts a JSON install request and decodes either the NDJSON stream or a plain JSON error.
func (e *ansibleTestEnv) install(t *testing.T, url string, body any) installResult {
	t.Helper()
	rec := doRequest(t, e.srv, e.auth, http.MethodPost, url, body)
	res := installResult{code: rec.Code}
	if res.log, res.err, res.done = ndjson(rec); !res.done {
		var plain libraryResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &plain)
		res.err = plain.Error
	}
	return res
}

func TestAnsibleLibraryGalaxyAndRemove(t *testing.T) {
	log := fakeGalaxy(t)
	env := newAnsibleEnv(t)
	dir := t.TempDir()
	env.srv.SetAnsibleLibraryDir(dir)

	if code, lib := env.library(t, http.MethodGet, "/api/ansible/library", nil); code != http.StatusOK || !lib.Available || len(lib.Collections)+len(lib.Roles) != 0 {
		t.Fatalf("empty library: %d %+v", code, lib)
	}
	res := env.install(t, "/api/ansible/library/galaxy", map[string]string{"kind": "collection", "name": "community.general", "version": "9.1.0"})
	if res.code != http.StatusOK || !res.done || res.err != "" || !strings.Contains(res.log, "installed community.general") {
		t.Fatalf("collection install: %+v", res)
	}
	if res := env.install(t, "/api/ansible/library/galaxy", map[string]string{"kind": "role", "name": "geerlingguy.nginx"}); res.code != http.StatusOK || !res.done || res.err != "" {
		t.Fatalf("role install: %+v", res)
	}
	if _, lib := env.library(t, http.MethodGet, "/api/ansible/library", nil); len(lib.Collections) != 1 || lib.Collections[0] != (ansible.LibraryItem{Name: "community.general", Version: "9.1.0"}) || len(lib.Roles) != 1 || lib.Roles[0].Name != "geerlingguy.nginx" {
		t.Fatalf("installed items not listed: %+v", lib)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "collection install --force -p "+ansible.LibraryCollectionsDir(dir)+" community.general:9.1.0") ||
		!strings.Contains(string(calls), "role install --force -p "+ansible.LibraryRolesDir(dir)+" geerlingguy.nginx") {
		t.Fatalf("unexpected galaxy invocations:\n%s", calls)
	}

	for _, bad := range []map[string]string{
		{"kind": "collection", "name": "--help"}, {"kind": "collection", "name": "nodot"}, {"kind": "collection", "name": "a.b", "version": "--x"},
		{"kind": "role", "name": "-x"}, {"kind": "other", "name": "a.b"}, {"kind": "role", "name": ""},
	} {
		if res := env.install(t, "/api/ansible/library/galaxy", bad); res.code != http.StatusBadRequest || res.err == "" || res.log != "" {
			t.Errorf("%v: expected a plain 400, got %+v", bad, res)
		}
	}
	if after, _ := os.ReadFile(log); string(calls) != string(after) {
		t.Fatal("rejected input must not reach ansible-galaxy")
	}

	if err := os.WriteFile(log+".fail", nil, 0o600); err != nil { // the fake fails while this file exists
		t.Fatal(err)
	}
	res = env.install(t, "/api/ansible/library/galaxy", map[string]string{"kind": "collection", "name": "no.such"})
	if res.code != http.StatusOK || !res.done || !strings.Contains(res.log, "boom") || !strings.Contains(res.err, "choose kind Role") {
		t.Fatalf("galaxy failure must surface its output and the final error: %+v", res)
	}
	os.Remove(log + ".fail")

	if code, _ := env.library(t, http.MethodDelete, "/api/ansible/library/collections/community.general", nil); code != http.StatusOK {
		t.Fatalf("delete collection: %d", code)
	}
	if code, _ := env.library(t, http.MethodDelete, "/api/ansible/library/collections/community.general", nil); code != http.StatusNotFound {
		t.Fatalf("delete twice: %d", code)
	}
	if code, _ := env.library(t, http.MethodDelete, "/api/ansible/library/roles/..", nil); code == http.StatusOK {
		t.Fatal("path traversal accepted")
	}
	if code, _ := env.library(t, http.MethodDelete, "/api/ansible/library/roles/geerlingguy.nginx", nil); code != http.StatusOK {
		t.Fatalf("delete role: %d", code)
	}
	if code, lib := env.library(t, http.MethodGet, "/api/ansible/library", nil); code != http.StatusOK || len(lib.Collections)+len(lib.Roles) != 0 {
		t.Fatalf("library not empty after deletes: %+v", lib)
	}
}

func TestAnsibleLibraryRequirementsAndLocal(t *testing.T) {
	log := fakeGalaxy(t)
	env := newAnsibleEnv(t)
	dir := t.TempDir()
	env.srv.SetAnsibleLibraryDir(dir)

	res := env.install(t, "/api/ansible/library/requirements", map[string]string{"content": "collections:\n  - name: ansible.posix\n    version: 1.5.4\nroles:\n  - geerlingguy.docker\n"})
	if res.code != http.StatusOK || !res.done || res.err != "" || !strings.Contains(res.log, "== collection ansible.posix ==") || !strings.Contains(res.log, "== role geerlingguy.docker ==") {
		t.Fatalf("requirements install: %+v", res)
	}
	if _, lib := env.library(t, http.MethodGet, "/api/ansible/library", nil); len(lib.Collections) != 1 || len(lib.Roles) != 1 {
		t.Fatalf("requirements not installed: %+v", lib)
	}
	for _, bad := range []string{"roles:\n  - src: https://github.com/x/y\n", "collections:\n  - name: a.b\n    source: https://evil\n", "", "- [nested]"} {
		if code, _ := env.library(t, http.MethodPost, "/api/ansible/library/requirements", map[string]string{"content": bad}); code != http.StatusBadRequest {
			t.Errorf("requirements %q: %d", bad, code)
		}
	}

	local := func(fields map[string]string, files ...ansibleImportFile) int {
		rec := env.post(t, "/api/ansible/library/local", fields, files)
		if rec.Code == http.StatusOK { // a stream must end with a clean done line
			if _, errMsg, done := ndjson(rec); !done || errMsg != "" {
				return http.StatusBadGateway
			}
		}
		return rec.Code
	}
	if code := local(map[string]string{"kind": "role", "name": "my_role"}, ansibleImportFile{"tasks/main.yml", "- debug: msg=hi\n"}, ansibleImportFile{"defaults/main.yml", "x: 1\n"}); code != http.StatusOK {
		t.Fatalf("local role: %d", code)
	}
	if _, err := os.Stat(filepath.Join(ansible.LibraryRolesDir(dir), "my_role", "defaults", "main.yml")); err != nil {
		t.Fatalf("local role not copied into the library: %v", err)
	}
	if code := local(map[string]string{"kind": "role", "name": "../evil"}, ansibleImportFile{"tasks/main.yml", "x"}); code != http.StatusBadRequest {
		t.Fatalf("role traversal name: %d", code)
	}
	if code := local(map[string]string{"kind": "role", "name": "empty"}, ansibleImportFile{"README.md", "x"}); code != http.StatusBadRequest {
		t.Fatalf("folder without role layout: %d", code)
	}
	if code := local(map[string]string{"kind": "collection"}, ansibleImportFile{"plugins/x.py", "x"}, ansibleImportFile{"README.md", "x"}); code != http.StatusBadRequest {
		t.Fatalf("collection folder without galaxy.yml: %d", code)
	}
	if code := local(map[string]string{"kind": "collection"}, ansibleImportFile{"galaxy.yml", "namespace: acme\nname: demo\nversion: 1.0.0\n"}, ansibleImportFile{"plugins/x.py", "x"}); code != http.StatusOK {
		t.Fatalf("local collection folder: %d", code)
	}
	if code := local(map[string]string{"kind": "collection"}, ansibleImportFile{"acme-demo-1.0.0.tar.gz", "not really a tarball"}); code != http.StatusOK {
		t.Fatalf("local collection tarball: %d", code)
	}
	if code := local(map[string]string{"kind": "collection"}, ansibleImportFile{"notes.txt", "x"}); code != http.StatusBadRequest {
		t.Fatalf("single non-tarball file: %d", code)
	}
	if code := local(map[string]string{"kind": "bogus"}, ansibleImportFile{"tasks/main.yml", "x"}); code != http.StatusBadRequest {
		t.Fatalf("unknown kind: %d", code)
	}
	calls, _ := os.ReadFile(log)
	if strings.Count(string(calls), "--no-deps") != 2 || !strings.Contains(string(calls), "acme-demo-1.0.0.tar.gz") {
		t.Fatalf("local collections must install offline from the uploaded source:\n%s", calls)
	}
}

func TestAnsibleLibraryStreamsInstallOutputLive(t *testing.T) {
	log := fakeGalaxy(t)
	env := newAnsibleEnv(t)
	env.srv.SetAnsibleLibraryDir(t.TempDir())
	ts := httptest.NewServer(env.srv)
	defer ts.Close()
	// While <log>.wait exists the fake prints "step 1" and then blocks, like a slow download.
	if err := os.WriteFile(log+".wait", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"kind":"collection","name":"community.general"}`)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/ansible/library/galaxy", body)
	if err != nil {
		t.Fatal(err)
	}
	env.auth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	seen := make(chan string, 1)
	go func() { // read lines until the fake's own output shows up
		var all string
		for {
			line, err := reader.ReadString('\n')
			all += line
			if strings.Contains(line, "step 1") || err != nil {
				seen <- all
				return
			}
		}
	}()
	select {
	case all := <-seen:
		if !strings.Contains(all, "$ ansible-galaxy collection install") || !strings.Contains(all, "step 1") {
			t.Fatalf("streamed lines: %q", all)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no output arrived while the install was still running")
	}
	if _, err := os.Stat(log + ".wait"); err != nil {
		t.Fatal("the install must still be running when its first output is seen")
	}
	os.Remove(log + ".wait")
	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), "installed community.general") || !strings.Contains(string(rest), `"done":true`) {
		t.Fatalf("rest of the stream: %q", rest)
	}
}

func TestAnsibleLibraryAndConfigGating(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	srv.SetAnsibleLibraryDir(t.TempDir())
	for _, url := range []string{"/api/ansible/library", "/api/ansible/config"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated: %d", url, rec.Code)
		}
		if rec := doRequest(t, srv, auth, http.MethodGet, url, nil); rec.Code != http.StatusConflict {
			t.Errorf("%s with the gate off: %d", url, rec.Code)
		}
	}
	if err := srv.db.SetAnsibleEnabled(true); err != nil {
		t.Fatal(err)
	}
	srv.SetAnsibleLibraryDir("")
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/library", nil); rec.Code != http.StatusConflict {
		t.Errorf("library without a directory: %d", rec.Code)
	}
	srv.SetAnsibleLibraryDir(t.TempDir())
	if rec := doRequest(t, srv, auth, http.MethodPut, "/api/ansible/library", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT library: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/library/galaxy", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET galaxy: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/library/nothing/here/at/all", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown library route: %d", rec.Code)
	}
}

func TestAnsibleConfigAPI(t *testing.T) {
	env := newAnsibleEnv(t)
	type cfgResponse struct {
		Content         string   `json:"content"`
		AllowedSections []string `json:"allowed_sections"`
		ManagedKeys     []string `json:"managed_keys"`
		Error           string   `json:"error"`
	}
	call := func(method string, body any) (int, cfgResponse) {
		rec := doRequest(t, env.srv, env.auth, method, "/api/ansible/config", body)
		var out cfgResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, got := call(http.MethodGet, nil)
	if code != http.StatusOK || got.Content != "" || len(got.AllowedSections) != 3 || len(got.ManagedKeys) == 0 {
		t.Fatalf("initial config: %d %+v", code, got)
	}
	want := "[defaults]\nforks = 10\n\n[ssh_connection]\npipelining = True\n"
	if code, got := call(http.MethodPut, map[string]string{"content": want}); code != http.StatusOK || got.Content != want {
		t.Fatalf("save: %d %+v", code, got)
	}
	if _, got := call(http.MethodGet, nil); got.Content != want {
		t.Fatalf("config not persisted: %q", got.Content)
	}
	code, got = call(http.MethodPut, map[string]string{"content": "[defaults]\nforks = 1\ninventory = /etc/hosts\n"})
	if code != http.StatusBadRequest || !strings.Contains(got.Error, "line 3") || !strings.Contains(got.Error, "inventory") {
		t.Fatalf("managed key: %d %+v", code, got)
	}
	if _, got := call(http.MethodGet, nil); got.Content != want {
		t.Fatal("a rejected save must keep the previous config")
	}
	if code, _ := call(http.MethodDelete, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", code)
	}
}
