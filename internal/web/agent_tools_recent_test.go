package web

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pantau/internal/cloudflare"
	"pantau/internal/store"
)

// The tools for the features after ADR 0074: Cloudflare DNS export and token verification (ADR 0075),
// VPN Networks (ADR 0077), the Package Update Check (ADR 0078), the repository branches (ADR 0079) and
// the Gateway CLI Update Check (ADR 0080/0081).

// TestAgentRecentToolsRegistered: every tool is registered with an object schema and a description.
func TestAgentRecentToolsRegistered(t *testing.T) {
	want := []string{"cf_dns_export", "cf_token_verify", "vpn_networks", "vpn_network_action", "pkg_updates", "gateway_cli", "repo_branch"}
	have := map[string]agentTool{}
	for _, tl := range agentTools {
		if _, dup := have[tl.Name]; dup {
			t.Fatalf("duplicate tool %s", tl.Name)
		}
		have[tl.Name] = tl
	}
	for _, n := range want {
		tl, ok := have[n]
		if !ok {
			t.Fatalf("tool %s is not registered", n)
		}
		if tl.run == nil || tl.InputSchema["type"] != "object" || strings.TrimSpace(tl.Description) == "" {
			t.Fatalf("tool %s has no run, no object schema or no description", n)
		}
		if tl.InputSchema["additionalProperties"] != false {
			t.Fatalf("tool %s must refuse additional properties", n)
		}
	}
	// The read tools must never carry a gate: a gate would make a read wait for the operator.
	for _, n := range []string{"cf_dns_export", "cf_token_verify", "vpn_networks", "gateway_cli"} {
		if have[n].gate != 0 {
			t.Fatalf("%s is a read tool and must not carry a gate (got %d)", n, have[n].gate)
		}
	}
}

// TestAgentCloudflareExportAndVerify: the zone file comes back as text, and a token is verified without ever
// being shown.
func TestAgentCloudflareExportAndVerify(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	fake := fakeCloudflareServer(t, true)
	defer fake.Close()
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fake.URL
		return c
	}
	if _, err := db.CreateCloudflareAccount(&store.CloudflareAccount{Name: "main", APIToken: "tok-secret-123"}); err != nil {
		t.Fatal(err)
	}

	out, isErr := callTool(t, srv, "cf_dns_export", `{"zone":"example.com"}`)
	if isErr || !strings.Contains(out, "1.2.3.4") {
		t.Fatalf("cf_dns_export: %s", out)
	}
	if out, _ := callTool(t, srv, "cf_dns_export", `{"zone":"nope.org"}`); !strings.Contains(out, "not found") {
		t.Fatalf("unknown zone must be an error: %s", out)
	}

	out, isErr = callTool(t, srv, "cf_token_verify", `{}`)
	if isErr || !strings.Contains(out, "active") || !strings.Contains(out, "main") {
		t.Fatalf("cf_token_verify: %s", out)
	}
	if strings.Contains(out, "tok-secret-123") {
		t.Fatalf("the token must never be returned: %s", out)
	}

	// A refused token is the finding this tool exists for, so it is reported rather than hidden.
	fakeBad := fakeCloudflareServer(t, false)
	defer fakeBad.Close()
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fakeBad.URL
		return c
	}
	if out, isErr := callTool(t, srv, "cf_token_verify", `{}`); !isErr || !strings.Contains(out, "refused") {
		t.Fatalf("a refused token must be reported: %s", out)
	}
}

// TestAgentVPNNetworks: the three reads, the host count of a network, the redacted config and the refusal
// of an unknown what.
func TestAgentVPNNetworks(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")

	// No network yet: a valid, empty answer, never an error the model would read as "VPN is broken".
	if out, isErr := callTool(t, srv, "vpn_networks", `{}`); isErr || !strings.Contains(out, "networks") {
		t.Fatalf("empty list: %s", out)
	}

	id, err := db.CreateVPNNetwork(&store.VPNNetwork{Name: "office", Driver: "wireguard", Enabled: true, AutoUp: true, Iface: "wg0", Config: `{"config":"s3cret"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.9.0.5", Port: 22, User: "root", NetworkID: id}); err != nil {
		t.Fatal(err)
	}

	out, isErr := callTool(t, srv, "vpn_networks", `{"what":"networks"}`)
	if isErr || !strings.Contains(out, "office") || !strings.Contains(out, "wireguard") {
		t.Fatalf("networks: %s", out)
	}
	// The config holds the driver's secrets: it is redacted, and the raw value never appears.
	if strings.Contains(out, "s3cret") {
		t.Fatalf("the network config must be redacted: %s", out)
	}
	if !strings.Contains(out, `"hosts_count":1`) {
		t.Fatalf("hosts_count must show the host behind the network: %s", out)
	}

	if out, isErr := callTool(t, srv, "vpn_networks", `{"what":"status"}`); isErr || !strings.Contains(out, "drivers") {
		t.Fatalf("status: %s", out)
	}
	if out, isErr := callTool(t, srv, "vpn_networks", `{"what":"interfaces"}`); isErr || out == "" {
		t.Fatalf("interfaces: %s", out)
	}
	if out, isErr := callTool(t, srv, "vpn_networks", `{"what":"nope"}`); !isErr || !strings.Contains(out, "what must be") {
		t.Fatalf("bad what: %s", out)
	}

	if out, isErr := callTool(t, srv, "vpn_networks", `{"what":"telemetry"}`); isErr || !strings.Contains(out, "telemetry") || !strings.Contains(out, "office") {
		t.Fatalf("telemetry: %s", out)
	} else {
		// The enriched runtime fields must be present, and the config secret must never leak.
		for _, key := range []string{"virtual_ip", "probe_ms", "degraded", "breaker", "hosts_count", "last_error"} {
			if !strings.Contains(out, `"`+key+`"`) {
				t.Fatalf("telemetry is missing %s: %s", key, out)
			}
		}
		if strings.Contains(out, "s3cret") {
			t.Fatalf("telemetry must not leak the config secret: %s", out)
		}
	}
}

// TestAgentVPNNetworkAction: probe is a read that needs no manager (graceful when one is absent); the schema
// refuses unknown actions and a missing id before any request. No real VPN is needed for these read paths.
func TestAgentVPNNetworkAction(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")

	// With no manager wired, probe reports the condition rather than panicking or hanging on a gate.
	if out, isErr := callTool(t, srv, "vpn_network_action", `{"network_id":1,"action":"probe"}`); !isErr || !strings.Contains(out, "vpn manager unavailable") {
		t.Fatalf("probe without manager: %s", out)
	}
	// Bad action is refused by the schema before any request (no approval, no manager needed).
	if out, isErr := callTool(t, srv, "vpn_network_action", `{"network_id":1,"action":"destroy"}`); !isErr {
		t.Fatalf("bad action must be refused: %s", out)
	}
	// Missing id is refused by the tool.
	if out, isErr := callTool(t, srv, "vpn_network_action", `{"action":"probe"}`); !isErr || !strings.Contains(out, "network_id") {
		t.Fatalf("missing network id: %s", out)
	}
	// The action tool is a controlled change and carries a gate (unlike the read tools).
	have := map[string]agentTool{}
	for _, tl := range agentTools {
		have[tl.Name] = tl
	}
	if have["vpn_network_action"].gate != gateRoutine {
		t.Fatalf("vpn_network_action must carry the routine gate")
	}
	if agentToolDomain("vpn_network_action") != domainNetwork {
		t.Fatalf("vpn_network_action must be in the network domain")
	}
}

// TestAgentPkgUpdates: the stored result is read without approval, the check asks first, and the list is
// cut while the counts stay.
func TestAgentPkgUpdates(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_approval", "") // the default mode asks for every change, gate 1 included
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h := strconv.FormatInt(hostID, 10)

	// Never checked: the tool says so instead of inventing "up to date".
	out, isErr := callTool(t, srv, "pkg_updates", `{"host_id":`+h+`}`)
	if isErr || !strings.Contains(out, `"checked":false`) {
		t.Fatalf("no check yet: %s", out)
	}

	pkgs := make([]store.PackageUpdate, 0, secCap+5)
	for i := range secCap + 5 {
		pkgs = append(pkgs, store.PackageUpdate{Name: "pkg" + strconv.Itoa(i), From: "1.0", To: "2.0"})
	}
	pkgs[0].Security = true
	if err := db.SavePackageUpdateCheck(&store.PackageUpdateCheck{
		HostID: hostID, CheckedAt: time.Now().Add(-2 * time.Minute), Supported: true, Manager: "apt",
		PendingTotal: len(pkgs), PendingSecurity: 1, Packages: pkgs, RepoCacheAgeHours: 200,
	}); err != nil {
		t.Fatal(err)
	}

	out, isErr = callTool(t, srv, "pkg_updates", `{"host_id":`+h+`}`)
	if isErr || !strings.Contains(out, `"pending_security":1`) || !strings.Contains(out, `"repo_cache_age_hours":200`) || !strings.Contains(out, "apt") {
		t.Fatalf("stored check: %s", out)
	}
	// The severities the model needs stay readable: the cut is reported, never silent.
	if !strings.Contains(out, `"packages_total":`+strconv.Itoa(len(pkgs))) {
		t.Fatalf("the cut of the package list must be counted: %s", out)
	}

	// The check runs a command over SSH and writes its result, so it asks. Denied: nothing runs.
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "pkg_updates", `{"host_id":`+h+`,"check":true}`); !strings.Contains(out, "denied") {
		t.Fatalf("a denied check must not run: %s", out)
	}
}

// TestAgentGatewayCLI: the comparison reaches the tool, a target filter narrows it, and an unknown target
// is refused by the schema before any request.
func TestAgentGatewayCLI(t *testing.T) {
	fakeUpstream(t, "2.22.0")
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	// The installed CLI is reported by the seam, so the test does not depend on the machine.
	srv.ansibleAvail = func() (string, string, error) {
		return "/usr/bin/ansible-playbook", "ansible-playbook [core 2.9.0]", nil
	}

	out, isErr := callTool(t, srv, "gateway_cli", `{"target":"ansible"}`)
	if isErr || !strings.Contains(out, `"target":"ansible"`) || !strings.Contains(out, "2.22.0") {
		t.Fatalf("ansible: %s", out)
	}
	// An older installed version is an update, and the installed version is reported beside it.
	if !strings.Contains(out, `"status":"update"`) || !strings.Contains(out, "2.9.0") {
		t.Fatalf("an older version must read as an update: %s", out)
	}

	// all always carries ansible, even when no Terraform binary is installed.
	out, isErr = callTool(t, srv, "gateway_cli", `{"target":"all"}`)
	if isErr || !strings.Contains(out, `"target":"ansible"`) {
		t.Fatalf("all: %s", out)
	}

	if out, isErr := callTool(t, srv, "gateway_cli", `{"target":"helm"}`); !isErr || !strings.Contains(out, "must be one of") {
		t.Fatalf("the enum must be enforced by the schema: %s", out)
	}
}

// TestAgentRepoBranch: the branch listing, a create and a switch, and the refusal of a delete of an
// unmerged branch. A real clone is needed, so it is built from a bare repository on disk.
func TestAgentRepoBranch(t *testing.T) {
	bare := createTestBareRepo(t)
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_approval", "auto") // gate 1 is skipped; a delete (gate 2) must still ask

	dir := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", bare, dir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	repoID, err := db.CreateGitRepo(&store.GitRepo{Name: "infra", URL: "https://h.example/o/infra.git", Branch: "main", Dir: dir, Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(repoID, 10)

	out, isErr := callTool(t, srv, "repo_branch", `{"repo_id":`+id+`,"action":"list"}`)
	if isErr || !strings.Contains(out, "main") {
		t.Fatalf("list: %s", out)
	}

	// switch and create lose nothing, so auto mode runs them without a card.
	if out, isErr := callTool(t, srv, "repo_branch", `{"repo_id":`+id+`,"action":"create","name":"feature/a"}`); isErr || !strings.Contains(out, "feature/a") {
		t.Fatalf("create: %s", out)
	}
	if out, isErr := callTool(t, srv, "repo_branch", `{"repo_id":`+id+`,"action":"switch","name":"main"}`); isErr || !strings.Contains(out, "main") {
		t.Fatalf("switch: %s", out)
	}

	// A name that git would read as an option never reaches git.
	if out, isErr := callTool(t, srv, "repo_branch", `{"repo_id":`+id+`,"action":"create","name":"--force"}`); !isErr || pendingCount(srv) != 0 {
		t.Fatalf("an option-shaped branch name must be refused before asking: %s", out)
	}

	// delete always asks. Denied: the branch is still there.
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "repo_branch", `{"repo_id":`+id+`,"action":"delete","name":"feature/a"}`); !strings.Contains(out, "denied") {
		t.Fatalf("a denied delete must not run: %s", out)
	}
	if out, _ := callTool(t, srv, "repo_branch", `{"repo_id":`+id+`,"action":"list"}`); !strings.Contains(out, "feature/a") {
		t.Fatalf("the branch must survive a denied delete: %s", out)
	}
}
