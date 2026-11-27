package inspector

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/store"
)

const healthNodesFixture = `{"items":[
 {"metadata":{"name":"n1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
 {"metadata":{"name":"n2"},"status":{"conditions":[{"type":"Ready","status":"False","reason":"KubeletNotReady"}]}}
]}`

const healthPodsFixture = `{"items":[
 {"metadata":{"namespace":"app","name":"ok-1"},"spec":{"nodeName":"n1"},"status":{"phase":"Running","containerStatuses":[{"restartCount":0,"state":{"running":{}}}]}},
 {"metadata":{"namespace":"app","name":"crash-1"},"spec":{"nodeName":"n1"},"status":{"phase":"Running","containerStatuses":[{"restartCount":5,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}},
 {"metadata":{"namespace":"app","name":"oom-1"},"spec":{"nodeName":"n2"},"status":{"phase":"Running","containerStatuses":[{"restartCount":1,"state":{"running":{}},"lastState":{"terminated":{"reason":"OOMKilled"}}}]}},
 {"metadata":{"namespace":"db","name":"pend-1"},"spec":{},"status":{"phase":"Pending"}},
 {"metadata":{"namespace":"db","name":"failed-1"},"spec":{"nodeName":"n2"},"status":{"phase":"Failed"}}
]}`

func TestParseKubeClusterHealth(t *testing.T) {
	tests := []struct {
		name          string
		nodes, pods   string
		wantNodesDown int
		wantTotal     int
		wantReasons   map[string]string // pod name -> reason
		wantErr       bool
	}{
		{
			name:          "mixed healthy and unhealthy",
			nodes:         healthNodesFixture,
			pods:          healthPodsFixture,
			wantNodesDown: 1,
			wantTotal:     5,
			wantReasons:   map[string]string{"crash-1": "CrashLoopBackOff", "oom-1": "OOMKilled", "pend-1": "Pending", "failed-1": "Failed"},
		},
		{
			name:      "empty lists",
			nodes:     `{"items":[]}`,
			pods:      `{"items":[]}`,
			wantTotal: 0,
		},
		{
			name:    "bad pods json is an error, not a silent empty answer",
			nodes:   healthNodesFixture,
			pods:    `{"items":[`,
			wantErr: true,
		},
		{
			name:    "bad nodes json is an error",
			nodes:   `not json`,
			pods:    `{"items":[]}`,
			wantErr: true,
		},
		{
			name:      "blank sections are treated as empty",
			wantTotal: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseKubeClusterHealth(tt.nodes, tt.pods)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseKubeClusterHealth: %v", err)
			}
			if got.PodTotal != tt.wantTotal {
				t.Errorf("PodTotal = %d, want %d", got.PodTotal, tt.wantTotal)
			}
			down := 0
			for _, n := range got.Nodes {
				if !n.Ready {
					down++
				}
			}
			if down != tt.wantNodesDown {
				t.Errorf("nodes not ready = %d, want %d", down, tt.wantNodesDown)
			}
			reasons := map[string]string{}
			for _, p := range got.UnhealthyPods {
				reasons[p.Name] = p.Reason
			}
			for name, want := range tt.wantReasons {
				if reasons[name] != want {
					t.Errorf("pod %s reason = %q, want %q", name, reasons[name], want)
				}
			}
			if len(reasons) != len(tt.wantReasons) {
				t.Errorf("unhealthy pods = %v, want %d entries", reasons, len(tt.wantReasons))
			}
		})
	}
}

func TestParseKubeClusterHealthCapsUnhealthyPods(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	const n = kubeHealthMaxPods + 10
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"metadata":{"namespace":"a","name":"p"},"status":{"phase":"Pending"}}`)
	}
	b.WriteString(`]}`)

	got, err := ParseKubeClusterHealth("", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UnhealthyPods) != kubeHealthMaxPods {
		t.Errorf("listed %d pods, want cap %d", len(got.UnhealthyPods), kubeHealthMaxPods)
	}
	if got.UnhealthyTotal != n {
		t.Errorf("UnhealthyTotal = %d, want %d", got.UnhealthyTotal, n)
	}
}

func TestKubeClusterHealthLiveFakeRunner(t *testing.T) {
	writeFakeKubectl(t, `
if [ "$1" = "get" ] && [ "$2" = "nodes" ]; then
  echo '`+healthNodesFixture+`'
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "pods" ]; then
  echo '`+healthPodsFixture+`'
  exit 0
fi
echo "unhandled: $@" >&2
exit 1
`)
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	cid, err := db.CreateKubeCluster(&store.KubeCluster{
		Name: "fake", Context: "ctx", Kubeconfig: "apiVersion: v1\nkind: Config\n", KubeMode: "full", Provider: "eks",
	})
	if err != nil {
		t.Fatalf("CreateKubeCluster: %v", err)
	}
	got, err := New(db, nil, nil).KubeClusterHealthLive(KubeClusterTarget(cid))
	if err != nil {
		t.Fatalf("KubeClusterHealthLive: %v", err)
	}
	if got.UnhealthyTotal != 4 || got.PodTotal != 5 {
		t.Errorf("got total=%d unhealthy=%d, want 5 and 4", got.PodTotal, got.UnhealthyTotal)
	}
}

// FuzzKubeClusterHealthParser: hostile listings must never panic and a summary must always serialize.
//
// Smoke run: go test -run '^$' -fuzz FuzzKubeClusterHealthParser -fuzztime=5s ./internal/inspector
func FuzzKubeClusterHealthParser(f *testing.F) {
	f.Add(healthNodesFixture, healthPodsFixture)
	f.Add("", "")
	f.Add(`{"items":[{"status":{"conditions":[]}}]}`, `{"items":[null]}`)
	f.Fuzz(func(t *testing.T, nodes, pods string) {
		got, err := ParseKubeClusterHealth(nodes, pods)
		if err != nil {
			return
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatalf("summary does not serialize: %v", err)
		}
	})
}
