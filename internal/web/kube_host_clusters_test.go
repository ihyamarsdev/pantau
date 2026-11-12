package web

import (
	"net/http"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestKubeHostClusterRenameAndAgentByName(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")

	// Two SSH hosts of one k3s cluster; one is read-only, so the cluster must count as read-only.
	for i, mode := range []string{"full", "read-only"} {
		id, err := db.CreateHost(&store.Host{Name: "node" + itoa(int64(i)), Host: "10.0.0." + itoa(int64(i+1)), Port: 22, User: "root", GroupName: "prod-a", KubeMode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetHostKubeClusterID(id, "uid-1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.CreateKubeCluster(&store.KubeCluster{Name: "staging", KubeMode: "full", Provider: "generic"}); err != nil {
		t.Fatal(err)
	}

	rename := func(name string) int {
		w := newRec()
		srv.handleKubeHostCluster(w, reqJSON(http.MethodPut, "/api/kube/host-clusters/uid-1", `{"name":"`+name+`"}`))
		return w.Code
	}

	// Default name is the shared group, one row for the two hosts.
	out := callTool(t, srv, "kube_targets", `{}`)
	if strings.Count(out, "prod-a") != 1 || !strings.Contains(out, "staging") {
		t.Fatalf("expected one row per cluster: %s", out)
	}

	if rename("STAGING") != http.StatusConflict {
		t.Fatal("a name used by an external cluster must be refused")
	}
	if rename("Prod Jakarta") != http.StatusOK {
		t.Fatal("rename failed")
	}
	if out := callTool(t, srv, "kube_targets", `{}`); !strings.Contains(out, "Prod Jakarta") || strings.Contains(out, "prod-a") {
		t.Fatalf("custom name not used: %s", out)
	}

	// The agent addresses the cluster by name; the strictest member mode applies.
	out = callTool(t, srv, "kube_action", `{"cluster":"prod jakarta","op":"restart","kind":"deployment","namespace":"app","name":"web"}`)
	if !strings.Contains(out, "read-only") || pendingCount(srv) != 0 {
		t.Fatalf("read-only member must make the cluster read-only: %s", out)
	}
	if out := callTool(t, srv, "kube_get", `{"cluster":"nope","what":"nodes"}`); !strings.Contains(out, "no Kubernetes cluster named") {
		t.Fatalf("unknown name: %s", out)
	}

	// Empty name returns to the derived name.
	if rename("") != http.StatusOK {
		t.Fatal("reset failed")
	}
	if out := callTool(t, srv, "kube_targets", `{}`); !strings.Contains(out, "prod-a") {
		t.Fatalf("reset did not restore derived name: %s", out)
	}
}
