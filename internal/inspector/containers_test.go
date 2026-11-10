package inspector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// inspectorHostContainers returns the stored container rows of a host.
func inspectorHostContainers(t *testing.T, db *store.DB, hostID int64) []store.HostContainer {
	t.Helper()
	rows, err := db.Query(`SELECT host_id, name, image, state FROM host_containers WHERE host_id = ? ORDER BY name`, hostID)
	if err != nil {
		t.Fatalf("query containers: %v", err)
	}
	defer rows.Close()
	var out []store.HostContainer
	for rows.Next() {
		var c store.HostContainer
		if err := rows.Scan(&c.HostID, &c.Name, &c.Image, &c.State); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// inspectionDockerMock answers the metrics batch and the container probe.
func inspectionDockerMock(dockerOut, dockerErr string, dockerExit int) *sshrunner.MockRunner {
	mock := sshrunner.NewMockRunner()
	mock.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "uname -r") {
			return strings.Join([]string{
				"5.15.0-generic", "---",
				"NAME=\"Ubuntu\"\nVERSION=\"22.04 LTS\"", "---",
				"up 10 days, 1 user, load average: 0.10, 0.20, 0.15", "---",
				"Mem: 8000000000 4000000000 4000000000", "---",
				"/dev/sda1 100000 50000 50000 50% /", "---",
				"0", "---",
				"1000 1000", "---",
				"time=12.5", "---",
				"1.1.1.1", "---",
				"127.0.0.1:80 127.0.0.1:12345", "---",
				"LISTEN 127.0.0.1:80", "---",
				"0", "---",
				"4", "---",
				"01/01/2022", "---",
				"KVM", "---",
				"1640995200",
			}, "\n"), "", 0, nil
		}
		if strings.Contains(cmd, "docker ps -a") {
			return dockerOut, dockerErr, dockerExit, nil
		}
		return "", "", 0, nil
	}
	return mock
}

func TestInspectHostStoresContainers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-containers-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	mock := inspectionDockerMock("db-main|postgres:13|Up 3 hours (healthy)|5432/tcp\n", "", 0)
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mock, nil
	}, nil)

	if err := ins.InspectHost(hostID); err != nil {
		t.Fatalf("inspection: %v", err)
	}

	got := inspectorHostContainers(t, db, hostID)
	if len(got) != 1 {
		t.Fatalf("expected 1 stored container, got %+v", got)
	}
	if got[0].Name != "db-main" || got[0].Image != "postgres:13" || got[0].State != "running" {
		t.Fatalf("unexpected container: %+v", got[0])
	}
}

func TestInspectHostStoresEmptyContainersWhenDockerIsMissing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-nodocker-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	// A stale listing must be cleared when docker is gone.
	if err := db.ReplaceHostContainers(hostID, []store.HostContainer{{Name: "old", Image: "nginx", State: "running"}}); err != nil {
		t.Fatal(err)
	}
	mock := inspectionDockerMock(dockerNotInstalled+"\n", "", 0)
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mock, nil
	}, nil)

	if err := ins.InspectHost(hostID); err != nil {
		t.Fatalf("inspection: %v", err)
	}
	if got := inspectorHostContainers(t, db, hostID); len(got) != 0 {
		t.Fatalf("expected no stored containers, got %+v", got)
	}
}

func TestInspectHostKeepsContainersWhenDockerFails(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-dockerfail-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceHostContainers(hostID, []store.HostContainer{{Name: "old", Image: "nginx", State: "running"}}); err != nil {
		t.Fatal(err)
	}
	mock := inspectionDockerMock("", "permission denied while trying to connect to the Docker daemon socket", 1)
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mock, nil
	}, nil)

	if err := ins.InspectHost(hostID); err != nil {
		t.Fatalf("inspection: %v", err)
	}
	got := inspectorHostContainers(t, db, hostID)
	if len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("expected the stored containers to stay untouched, got %+v", got)
	}
}
