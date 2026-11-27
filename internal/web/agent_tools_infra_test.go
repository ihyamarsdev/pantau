package web

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// kubeGetFixture is a three-object list: one healthy and one unhealthy Deployment, and one failed Job.
const kubeGetFixture = `{"kind":"List","items":[
{"kind":"Deployment","metadata":{"name":"web","namespace":"shop","creationTimestamp":"2026-11-20T00:00:00Z"},"spec":{"replicas":2},"status":{"readyReplicas":2}},
{"kind":"Deployment","metadata":{"name":"api","namespace":"shop","creationTimestamp":"2026-11-20T00:00:00Z"},"spec":{"replicas":2},"status":{"readyReplicas":0}},
{"kind":"Job","metadata":{"name":"migrate","namespace":"ops","creationTimestamp":"2026-11-20T00:00:00Z"},"status":{"conditions":[{"type":"Failed","status":"True"}]}}
]}`

// newKubeGetServer builds a server whose SSH runner answers every command with kubeGetFixture and records them.
func newKubeGetServer(t *testing.T) (srv *Server, hostID int64, commands func() []string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "kube-get.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hostID, err = db.CreateHost(&store.Host{Name: "k3s-1", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var cmds []string
	ins := inspector.New(db, func(sshrunner.Target) (sshrunner.Runner, error) {
		mock := sshrunner.NewMockRunner()
		mock.DefaultExec = func(cmd string) (string, string, int, error) {
			mu.Lock()
			cmds = append(cmds, cmd)
			mu.Unlock()
			return kubeGetFixture, "", 0, nil
		}
		return mock, nil
	}, nil)
	commands = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), cmds...)
	}
	return NewServer(db, ins, nil), hostID, commands
}

func TestToolKubeGetMultiKindIsOneKubectlCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      map[string]interface{}
		wantTotal float64
		wantCmd   string
	}{
		{
			name:      "comma-separated kinds across all namespaces",
			args:      map[string]interface{}{"kind": "deployments,jobs", "namespace": "*", "unhealthy_only": true},
			wantTotal: 2, wantCmd: "kc get deployments,jobs -A -o json",
		},
		{
			name:      "list of kinds with summary only",
			args:      map[string]interface{}{"kind": []interface{}{"deployments", "jobs"}, "namespace": "all", "summary_only": true},
			wantTotal: 3, wantCmd: "kc get deployments,jobs -A -o json",
		},
		{
			// The fake answers with the whole fixture whatever the scope, so only the command is checked here.
			name:      "one namespace",
			args:      map[string]interface{}{"kind": "deployments", "namespace": "shop"},
			// The script is wrapped in sh -c, so the quoted namespace is escaped; the prefix is what matters.
			wantTotal: 3, wantCmd: "kc get deployments -n ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, hostID, commands := newKubeGetServer(t)
			tt.args["target_id"] = float64(hostID)

			out, err := toolKubeGet(srv, nil, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			var res map[string]interface{}
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, out)
			}
			if res["total_count"] != tt.wantTotal {
				t.Errorf("total_count = %v, want %v", res["total_count"], tt.wantTotal)
			}
			cmds := commands()
			if len(cmds) != 1 || !strings.Contains(cmds[0], tt.wantCmd) {
				t.Errorf("kubectl commands = %q, want exactly one containing %q", cmds, tt.wantCmd)
			}
		})
	}
}

func TestToolKubeGetRejectsUnknownKinds(t *testing.T) {
	t.Parallel()
	srv, hostID, commands := newKubeGetServer(t)
	_, err := toolKubeGet(srv, nil, map[string]interface{}{"target_id": float64(hostID), "kind": "secrets"})
	if err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Fatalf("err = %v, want unsupported kind", err)
	}
	if _, err := toolKubeGet(srv, nil, map[string]interface{}{"target_id": float64(hostID)}); err == nil {
		t.Error("a call with neither kind nor what must fail")
	}
	if n := len(commands()); n != 0 {
		t.Errorf("kubectl ran %d times for rejected input", n)
	}
}
