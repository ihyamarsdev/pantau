package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGetDockerOverviewAggregatesFleet(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h1, _ := db.CreateHost(&Host{Name: "web-1", Host: "10.0.0.1", GroupName: "prod"})
	h2, _ := db.CreateHost(&Host{Name: "web-2", Host: "10.0.0.2", GroupName: "prod"})
	_, _ = db.CreateHost(&Host{Name: "db-1", Host: "10.0.0.3", GroupName: "data"})

	// h1: 3 containers (2 running, 1 exited), 2 stacks, one critical misconfig finding.
	now := time.Now().UTC()
	if err := db.ReplaceHostContainers(h1, []HostContainer{
		{HostID: h1, Name: "nginx", Image: "nginx:1", State: "running", SeenAt: now},
		{HostID: h1, Name: "app", Image: "app:1", State: "running", SeenAt: now},
		{HostID: h1, Name: "old", Image: "old:1", State: "exited", SeenAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDockerStack(h1, "web", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDockerStack(h1, "worker", "{}"); err != nil {
		t.Fatal(err)
	}
	sum, _ := json.Marshal(dockerSecuritySummary{Status: "ok", Counts: DockerSeverityCounts{Critical: 1, Total: 1}})
	if _, err := db.Exec(`INSERT INTO docker_security_scans(host_id, kind, subject, scanned_at, summary_json, findings_json) VALUES (?, 'misconfig', '', ?, ?, '[]')`, h1, now, string(sum)); err != nil {
		t.Fatal(err)
	}

	// h2: 1 running container, no security scan. h3: no Docker at all.
	if err := db.ReplaceHostContainers(h2, []HostContainer{{HostID: h2, Name: "redis", Image: "redis:1", State: "running", SeenAt: now}}); err != nil {
		t.Fatal(err)
	}

	ov, err := db.GetDockerOverview(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if ov.HostCount != 3 {
		t.Errorf("host_count = %d, want 3", ov.HostCount)
	}
	if ov.HostsWithDocker != 2 || ov.HostsWithoutDocker != 1 {
		t.Errorf("with/without docker = %d/%d, want 2/1", ov.HostsWithDocker, ov.HostsWithoutDocker)
	}
	if ov.TotalContainers != 4 || ov.RunningContainers != 3 || ov.StoppedContainers != 1 {
		t.Errorf("containers = %d running=%d stopped=%d, want 4/3/1", ov.TotalContainers, ov.RunningContainers, ov.StoppedContainers)
	}
	if ov.TotalStacks != 2 {
		t.Errorf("total_stacks = %d, want 2", ov.TotalStacks)
	}
	if ov.TotalSecurityIssues != 1 {
		t.Errorf("total_security_issues = %d, want 1", ov.TotalSecurityIssues)
	}
	if len(ov.Hosts) != 3 || ov.Hosts[0].HostID != h1 {
		t.Errorf("hosts sorted wrong: %+v", ov.Hosts)
	}
	if ov.Hosts[0].Running != 2 || ov.Hosts[0].StackCount != 2 || ov.Hosts[0].SecurityIssues != 1 || ov.Hosts[0].AuditStatus != "ok" {
		t.Errorf("h1 summary = %+v", ov.Hosts[0])
	}
	if ov.Hosts[2].ContainerCount != 0 {
		t.Errorf("h3 should have no containers: %+v", ov.Hosts[2])
	}
}

func TestGetDockerOverviewScopeFilters(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	prod, _ := db.CreateHost(&Host{Name: "prod-1", Host: "10.0.0.1", GroupName: "prod"})
	dev, _ := db.CreateHost(&Host{Name: "dev-1", Host: "10.0.0.2", GroupName: "dev"})
	for _, id := range []int64{prod, dev} {
		if err := db.ReplaceHostContainers(id, []HostContainer{{HostID: id, Name: "c", Image: "i", State: "running", SeenAt: time.Now().UTC()}}); err != nil {
			t.Fatal(err)
		}
	}

	byHost, err := db.GetDockerOverview(prod, "")
	if err != nil {
		t.Fatal(err)
	}
	if byHost.HostCount != 1 || byHost.TotalContainers != 1 {
		t.Errorf("host scope = %+v", byHost)
	}

	byGroup, err := db.GetDockerOverview(0, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if byGroup.HostCount != 1 || byGroup.TotalContainers != 1 {
		t.Errorf("group scope = %+v", byGroup)
	}
}
