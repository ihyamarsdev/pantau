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

	"pantau/internal/ansible"
	"pantau/internal/store"
)

func TestAnsibleHostFactsGatherAndCache(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "web")

	var mu sync.Mutex
	calls := 0
	srv.factsGather = func(ctx context.Context, target ansible.Target, cfg, libDir string, log io.Writer) (map[string]any, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		if target.ID != hostID || target.Address != "10.0.0.9" {
			return nil, fmt.Errorf("unexpected target %+v", target)
		}
		return map[string]any{
			"ansible_distribution":         "Archlinux",
			"ansible_distribution_version": "2026.11",
			"ansible_kernel":               "6.12.1",
			"ansible_architecture":         "x86_64",
			"ansible_hostname":             "web-1",
			"ansible_memtotal_mb":          float64(16384),
			"ansible_mounts":               []any{map[string]any{"mount": "/", "fstype": "ext4", "size_total": float64(100)}},
			"ansible_interfaces":           []any{"eth0"},
			"ansible_eth0":                 map[string]any{"active": true, "ipv4": map[string]any{"address": "10.0.0.9"}, "macaddress": "aa:bb"},
		}, nil
	}

	// Nothing gathered yet.
	rec := doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET before gather = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST gather = %d: %s", rec.Code, rec.Body.String())
	}
	var gathered struct {
		GatheredAt string         `json:"gathered_at"`
		Summary    map[string]any `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &gathered); err != nil {
		t.Fatalf("decode gather: %v", err)
	}
	if gathered.GatheredAt == "" || gathered.Summary["hostname"] != "web-1" || gathered.Summary["kernel"] != "6.12.1" {
		t.Fatalf("gather response = %+v", gathered)
	}

	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET after gather = %d: %s", rec.Code, rec.Body.String())
	}
	var cached struct {
		GatheredAt string         `json:"gathered_at"`
		Summary    map[string]any `json:"summary"`
		Facts      map[string]any `json:"facts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cached); err != nil {
		t.Fatalf("decode cached: %v", err)
	}
	if cached.Facts["ansible_hostname"] != "web-1" || cached.Summary["cpu_cores"] == nil {
		t.Fatalf("cached facts = %+v", cached)
	}
	if mounts, ok := cached.Summary["mounts"].([]any); !ok || len(mounts) != 1 {
		t.Fatalf("summary mounts = %v", cached.Summary["mounts"])
	}
	if ifaces, ok := cached.Summary["interfaces"].([]any); !ok || len(ifaces) != 1 {
		t.Fatalf("summary interfaces = %v", cached.Summary["interfaces"])
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("gather calls = %d, want 1 (the second GET must hit the cache)", calls)
	}
}

func TestAnsibleHostFactsGatherFailure(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "")
	srv.factsGather = func(ctx context.Context, target ansible.Target, cfg, libDir string, log io.Writer) (map[string]any, error) {
		return nil, fmt.Errorf("ansible exited with 4: Failed to connect to the host via ssh")
	}

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST gather failure = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to connect") {
		t.Fatalf("body = %s, want the ansible reason", rec.Body.String())
	}
	// A failed gather must not poison the cache.
	if rec := doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after failed gather = %d, want 404", rec.Code)
	}
}

func TestAnsibleHostFactsNotFoundHost(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if rec := doRequest(t, srv, auth, method, "/api/ansible/hosts/9999/facts", nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s unknown host = %d", method, rec.Code)
		}
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/hosts/1/other", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unrelated sub-path = %d, want 404", rec.Code)
	}
}

func TestAnsibleHostFactsConcurrentGatherConflict(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	hostID := createAdhocHost(t, srv, "web-1", "")

	started := make(chan struct{})
	release := make(chan struct{})
	srv.factsGather = func(ctx context.Context, target ansible.Target, cfg, libDir string, log io.Writer) (map[string]any, error) {
		close(started)
		<-release
		return map[string]any{"ansible_hostname": "web-1"}, nil
	}

	first := make(chan int, 1)
	go func() {
		first <- doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil).Code
	}()
	<-started

	if rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/hosts/%d/facts", hostID), nil); rec.Code != http.StatusConflict {
		t.Fatalf("concurrent gather = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	close(release)
	if code := <-first; code != http.StatusAccepted {
		t.Fatalf("first gather = %d", code)
	}
}

func TestAnsibleExplorerGroupsAndAliases(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	webID := createAdhocHost(t, srv, "web-1", "web")
	createAdhocHost(t, srv, "db-1", "db")
	soloID := createAdhocHost(t, srv, "solo box", "")
	// A gathered fact set must surface as facts_at for that host only.
	if _, err := srv.db.SetAnsibleHostFacts(webID, `{}`, `{}`); err != nil {
		t.Fatal(err)
	}

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/explorer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET explorer = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Groups []struct {
			Group string           `json:"group"`
			Hosts []map[string]any `json:"hosts"`
		} `json:"groups"`
		Ungrouped []map[string]any `json:"ungrouped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode explorer: %v", err)
	}
	if len(body.Groups) != 2 || body.Groups[0].Group != "web" || body.Groups[1].Group != "db" {
		t.Fatalf("groups = %+v", body.Groups)
	}
	web := body.Groups[0].Hosts[0]
	if web["ansible_alias"] != "web-1" || web["id"] != float64(webID) || web["address"] != "10.0.0.9" || web["facts_at"] == nil {
		t.Fatalf("web host = %+v", web)
	}
	if body.Groups[1].Hosts[0]["facts_at"] != nil {
		t.Fatalf("db host must have no facts: %+v", body.Groups[1].Hosts[0])
	}
	if len(body.Ungrouped) != 1 || body.Ungrouped[0]["id"] != float64(soloID) {
		t.Fatalf("ungrouped = %+v", body.Ungrouped)
	}
	// The alias is the inventory alias: spaces become underscores, exactly like the runner's inventory.
	if body.Ungrouped[0]["ansible_alias"] != "solo_box" {
		t.Fatalf("ungrouped alias = %v", body.Ungrouped[0]["ansible_alias"])
	}
}

func TestAnsibleExplorerShowsLatestPing(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	createAdhocHost(t, srv, "web-1", "web")
	createAdhocHost(t, srv, "db-1", "web")
	createAdhocHost(t, srv, "never", "web")
	finish := func(module, results string) {
		t.Helper()
		id, err := srv.db.CreateAnsibleAdhocRun(&store.AnsibleAdhocRun{Module: module, Pattern: "all", Targets: "[]", Status: "queued"})
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.db.FinishAnsibleAdhocRun(id, "success", nil, "", results); err != nil {
			t.Fatal(err)
		}
	}
	finish("ping", `[{"alias":"web-1","status":"unreachable"},{"alias":"db-1","status":"success"}]`)
	// A newer ping flips web-1; a newer non-ping run must not count as a reachability check.
	finish("ping", `[{"alias":"web-1","status":"success"}]`)
	finish("shell", `[{"alias":"db-1","status":"failed"},{"alias":"never","status":"failed"}]`)

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/explorer", nil)
	var body struct {
		Groups []struct {
			Hosts []map[string]any `json:"hosts"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, h := range body.Groups[0].Hosts {
		got[h["name"].(string)] = h
	}
	if got["web-1"]["ansible_status"] != "online" || got["web-1"]["ansible_checked_at"] == nil {
		t.Fatalf("web-1 = %+v, want online from the newest ping", got["web-1"])
	}
	if got["db-1"]["ansible_status"] != "online" {
		t.Fatalf("db-1 = %+v, a shell run must not override its ping", got["db-1"])
	}
	if got["never"]["ansible_status"] != "unknown" || got["never"]["ansible_checked_at"] != nil {
		t.Fatalf("never = %+v, want unknown without a timestamp", got["never"])
	}
}

func TestAnsibleExplorerMethodNotAllowed(t *testing.T) {
	srv, auth, _ := setupAdhocTestServer(t)
	if rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/explorer", map[string]any{}); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST explorer = %d", rec.Code)
	}
}
