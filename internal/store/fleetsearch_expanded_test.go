package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// findGroup returns the first group of the given kind in a result.
func findGroup(res *FleetSearchResult, kind string) *FleetSearchGroup {
	for i := range res.Groups {
		if res.Groups[i].Kind == kind {
			return &res.Groups[i]
		}
	}
	return nil
}

func TestSearchFleetFolders(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)
	fleetSearchHost(t, db, "alpha", "Production")
	fleetSearchHost(t, db, "beta", "Production")
	fleetSearchHost(t, db, "gamma", "Staging")

	res, err := db.SearchFleet("folder:Production", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Scope != FleetSearchFolder || res.Kind != FleetSearchFolder {
		t.Fatalf("expected folder scope, got scope=%q kind=%q", res.Scope, res.Kind)
	}
	g := findGroup(res, FleetSearchFolder)
	if g == nil || g.Name != "Production" {
		t.Fatalf("expected Production folder, got %+v", res.Groups)
	}
	if g.GroupID != "Production" || g.TargetTab != "hosts" || g.Category != FleetSearchFolder {
		t.Fatalf("missing deep-link metadata: %+v", g)
	}
	if g.HostCount != 2 {
		t.Errorf("expected 2 members, got %d", g.HostCount)
	}
	if !strings.Contains(g.Subtitle, "2") {
		t.Errorf("expected member count in subtitle, got %q", g.Subtitle)
	}
}

func TestSearchFleetHostAttributes(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)
	id := fleetSearchHost(t, db, "web-01", "prod")
	if err := db.UpdateHostInspection(&Host{
		ID: id, Name: "web-01", Host: "web-01.example.net", Port: 22, User: "root",
		PublicIP: "203.0.113.5", PrivateIPs: `["10.0.0.5"]`, OSInfo: "Ubuntu 24.04",
		HardwareModel: "Dell R640", Notes: "edge proxy", GroupName: "prod",
	}); err != nil {
		t.Fatalf("update inspection: %v", err)
	}
	res, err := db.SearchFleet("host:Ubuntu", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	g := findGroup(res, FleetSearchHostKind)
	if g == nil || g.Name != "web-01" {
		t.Fatalf("expected host match, got %+v", res.Groups)
	}
	if g.EntityID != strconvItoa64(id) || g.TargetTab != "overview" || g.Category != FleetSearchHostKind {
		t.Fatalf("missing host deep-link metadata: %+v", g)
	}
	if !strings.Contains(g.Subtitle, "Ubuntu 24.04") {
		t.Errorf("expected OS in subtitle, got %q", g.Subtitle)
	}
}

func TestSearchFleetPortsAndServices(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)
	id := fleetSearchHost(t, db, "alpha", "")
	if err := db.UpdateHostInspection(&Host{
		ID: id, Name: "alpha", Host: "alpha.example.net", Port: 22, User: "root",
		ListeningPorts: `[{"proto":"tcp","port":"8080","address":"0.0.0.0","process":"nginx","risk":"high"}]`,
	}); err != nil {
		t.Fatalf("update inspection: %v", err)
	}
	if err := db.UpsertActualItem(&ActualItem{HostID: id, Kind: "service", Target: "nginx.service", Current: "active", Status: "ok"}); err != nil {
		t.Fatalf("upsert service: %v", err)
	}

	res, err := db.SearchFleet("port:8080", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("port search: %v", err)
	}
	g := findGroup(res, FleetSearchPort)
	if g == nil || !strings.Contains(g.Name, "8080") {
		t.Fatalf("expected port match, got %+v", res.Groups)
	}
	if g.TargetTab != "network" || g.StatusBadge != "high" {
		t.Errorf("unexpected port metadata: %+v", g)
	}

	res, err = db.SearchFleet("service:nginx", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("service search: %v", err)
	}
	g = findGroup(res, FleetSearchService)
	if g == nil || g.Name != "nginx.service" {
		t.Fatalf("expected service match, got %+v", res.Groups)
	}
	if g.TargetTab != "systemd" {
		t.Errorf("expected systemd tab, got %q", g.TargetTab)
	}
}

func TestSearchFleetInfraAndAutomation(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)

	if _, err := db.CreateKubeCluster(&KubeCluster{Name: "prod-eu", Context: "ctx-eu", Server: "https://kube.example", Provider: "eks"}); err != nil {
		t.Fatalf("create kube: %v", err)
	}
	if _, err := db.CreateVPNNetwork(&VPNNetwork{Name: "tailscale-main", Driver: "tailscale", Iface: "tailscale0"}); err != nil {
		t.Fatalf("create vpn: %v", err)
	}
	if _, err := db.CreateCloudflareAccount(&CloudflareAccount{Name: "acme-cf", AccountID: "abc123"}); err != nil {
		t.Fatalf("create cf: %v", err)
	}
	if _, err := db.CreateGitRepo(&GitRepo{Name: "pantau-web", URL: "https://git.example/pantau-web.git", Branch: "main", Group: "apps"}); err != nil {
		t.Fatalf("create git repo: %v", err)
	}
	if _, err := db.CreateAnsiblePlaybook(&AnsiblePlaybook{Name: "deploy-web", Description: "Deploy the web tier", GroupName: "apps"}); err != nil {
		t.Fatalf("create playbook: %v", err)
	}
	if _, err := db.CreateTFWorkspace(&TFWorkspace{Name: "infra-prod", Description: "Production infra", Environment: "production", GroupName: "core"}); err != nil {
		t.Fatalf("create tf workspace: %v", err)
	}

	cases := []struct {
		query    string
		kind     string
		wantName string
		wantTab  string
	}{
		{"kube:prod-eu", FleetSearchKube, "prod-eu", "kube"},
		{"vpn:tailscale", FleetSearchVPN, "tailscale-main", "networks"},
		{"cf:acme", FleetSearchCF, "acme-cf", "cloudflare"},
		{"git:pantau-web", FleetSearchGit, "pantau-web", "repos"},
		{"ansible:deploy", FleetSearchAnsible, "deploy-web", "ansible"},
		{"tf:infra-prod", FleetSearchTF, "infra-prod", "terraform"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			res, err := db.SearchFleet(tc.query, FleetSearchAll, 200)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			g := findGroup(res, tc.kind)
			if g == nil || g.Name != tc.wantName {
				t.Fatalf("expected %s, got %+v", tc.wantName, res.Groups)
			}
			if g.TargetTab != tc.wantTab || g.EntityID == "" || g.Category != tc.kind {
				t.Errorf("unexpected metadata: %+v", g)
			}
		})
	}
}

func TestSearchFleetSecurityAndInventory(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)
	h := fleetSearchHost(t, db, "alpha", "")

	if err := db.SavePackageUpdateCheck(&PackageUpdateCheck{
		HostID: h, CheckedAt: time.Now(),
		Packages: []PackageUpdate{{Name: "openssl", From: "1.1.1", To: "3.0.2", Security: true}},
	}); err != nil {
		t.Fatalf("save update check: %v", err)
	}
	if err := db.SaveDockerSecurityScan(&DockerSecurityScan{
		HostID: h, Kind: "image", Subject: "nginx:1.25", ScannedAt: time.Now(),
		Vulns: []DockerImageVuln{{ID: "CVE-2024-1234", Package: "libssl", Severity: "HIGH", Title: "buffer overflow"}},
	}); err != nil {
		t.Fatalf("save docker scan: %v", err)
	}
	if _, err := db.CreateSSHKey(&SSHKey{Name: "deploy-key", Fingerprint: "SHA256:abc", PublicKey: "ssh-ed25519 AAAA", PrivateKey: "SECRET"}); err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	if err := db.SaveAccountAudit(&AccountAudit{
		HostID: h, ScannedAt: time.Now(),
		Users: []AccountUser{{Name: "deploy", Shell: "/bin/bash", PasswordState: "locked"}},
		Keys:  []AccountKey{{User: "root", Comment: "admin@laptop", Fingerprint: "SHA256:key1"}},
	}); err != nil {
		t.Fatalf("save account audit: %v", err)
	}

	cases := []struct {
		query string
		kind  string
	}{
		{"pkg:openssl", FleetSearchPkg},
		{"cve:CVE-2024-1234", FleetSearchCVE},
		{"sshkey:deploy-key", FleetSearchSSHKey},
		{"account:deploy", FleetSearchAccount},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			res, err := db.SearchFleet(tc.query, FleetSearchAll, 200)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			g := findGroup(res, tc.kind)
			if g == nil {
				t.Fatalf("expected a %s group, got %+v", tc.kind, res.Groups)
			}
			if g.Category != tc.kind || g.TargetTab == "" {
				t.Errorf("unexpected metadata: %+v", g)
			}
		})
	}
	// The SSH private key must never appear in the result.
	res, _ := db.SearchFleet("sshkey:deploy-key", FleetSearchAll, 200)
	encoded, _ := json.Marshal(res)
	if strings.Contains(strings.ToLower(string(encoded)), "secret") {
		t.Error("SSH private key must never be searchable or returned")
	}
}

func TestSearchFleetRanking(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)
	for _, name := range []string{"nginx-exporter", "mynginx", "nginx", "notnginx"} {
		hostID := fleetSearchHost(t, db, name, "")
		if err := db.ReplaceHostPackages(hostID, []*HostPackage{{HostID: hostID, Name: name, Version: "1"}}); err != nil {
			t.Fatalf("replace packages: %v", err)
		}
	}
	res, err := db.SearchFleet("nginx", FleetSearchPackage, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(res.Groups))
	}
	if res.Groups[0].Name != "nginx" {
		t.Errorf("exact match must rank first, got %q", res.Groups[0].Name)
	}
	if res.Groups[1].Name != "nginx-exporter" {
		t.Errorf("prefix match must rank second, got %q", res.Groups[1].Name)
	}
}

func TestSearchFleetPrefixNarrowsKind(t *testing.T) {
	t.Parallel()
	db := fleetSearchDB(t)
	h := fleetSearchHost(t, db, "alpha", "prod")
	if err := db.ReplaceHostPackages(h, []*HostPackage{{HostID: h, Name: "openssl", Version: "1"}}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	if _, err := db.CreateAnsiblePlaybook(&AnsiblePlaybook{Name: "openssl-rotate"}); err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// Without a prefix both categories match.
	res, err := db.SearchFleet("openssl", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if findGroup(res, FleetSearchPackage) == nil || findGroup(res, FleetSearchAnsible) == nil {
		t.Fatalf("expected package and ansible groups, got %+v", res.Groups)
	}
	// The prefix restricts to one category even with kind=all.
	res, err = db.SearchFleet("ansible:openssl", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Kind != FleetSearchAnsible {
		t.Fatalf("expected only ansible results, got %+v", res.Groups)
	}
}

func TestFleetKindValid(t *testing.T) {
	t.Parallel()
	if !FleetKindValid(FleetSearchAll) || !FleetKindValid(FleetSearchFolder) || FleetKindValid("nope") {
		t.Fatal("kind validation is wrong")
	}
	if len(FleetKinds()) != len(fleetCategories) {
		t.Fatalf("expected %d categories, got %d", len(fleetCategories), len(FleetKinds()))
	}
}
