package inspector

import (
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestParseKubePodDiagnosis(t *testing.T) {
	podJSON := `{
  "metadata": {
    "name": "api-7f9b",
    "namespace": "prod",
    "creationTimestamp": "2026-11-26T08:00:00Z"
  },
  "spec": {
    "nodeName": "node-2",
    "containers": [
      {"name": "api", "image": "ghcr.io/acme/api:v1.2"}
    ]
  },
  "status": {
    "phase": "Running",
    "podIP": "10.42.1.9",
    "qosClass": "Burstable",
    "conditions": [
      {"type": "Ready", "status": "False", "reason": "ContainersNotReady", "message": "containers with unready status: [api]"}
    ],
    "containerStatuses": [
      {
        "name": "api",
        "image": "ghcr.io/acme/api:v1.2",
        "ready": false,
        "restartCount": 4,
        "state": {
          "waiting": {"reason": "CrashLoopBackOff", "message": "back-off 40s restarting failed container=api"}
        },
        "lastState": {
          "terminated": {
            "reason": "OOMKilled",
            "exitCode": 137,
            "startedAt": "2026-11-26T08:05:00Z",
            "finishedAt": "2026-11-26T08:05:30Z"
          }
        }
      }
    ]
  }
}`
	eventsJSON := `{
  "items": [
    {
      "metadata": {"namespace": "prod", "creationTimestamp": "2026-11-26T08:05:31Z"},
      "involvedObject": {"kind": "Pod", "name": "api-7f9b"},
      "type": "Warning",
      "reason": "BackOff",
      "message": "Back-off restarting failed container",
      "count": 3,
      "lastTimestamp": "2026-11-26T08:06:00Z"
    },
    {
      "metadata": {"namespace": "prod", "creationTimestamp": "2026-11-26T08:00:01Z"},
      "involvedObject": {"kind": "Pod", "name": "api-7f9b"},
      "type": "Normal",
      "reason": "Scheduled",
      "message": "Successfully assigned prod/api-7f9b to node-2",
      "count": 1,
      "lastTimestamp": "2026-11-26T08:00:01Z"
    },
    {
      "metadata": {"namespace": "prod", "creationTimestamp": "2026-11-26T08:02:00Z"},
      "involvedObject": {"kind": "Pod", "name": "other-pod"},
      "type": "Warning",
      "reason": "FailedScheduling",
      "message": "0/3 nodes available",
      "count": 1,
      "lastTimestamp": "2026-11-26T08:02:00Z"
    }
  ]
}`

	diag, err := parseKubePodDiagnosis(podJSON, eventsJSON)
	if err != nil {
		t.Fatalf("parseKubePodDiagnosis: %v", err)
	}
	if diag.Name != "api-7f9b" || diag.Namespace != "prod" || diag.Node != "node-2" {
		t.Fatalf("unexpected pod metadata: %+v", diag)
	}
	if len(diag.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(diag.Containers))
	}
	c := diag.Containers[0]
	if c.StateReason != "CrashLoopBackOff" || c.RestartCount != 4 {
		t.Fatalf("unexpected container state: %+v", c)
	}
	if c.LastState == nil || !c.LastState.OOMKilled || c.LastState.ExitCode != 137 || c.LastState.Reason != "OOMKilled" {
		t.Fatalf("expected OOMKilled lastState with exitCode 137, got %+v", c.LastState)
	}
	if len(diag.WarningEvents) != 1 || diag.WarningEvents[0].Reason != "BackOff" {
		t.Fatalf("expected 1 filtered Warning event for api-7f9b, got %+v", diag.WarningEvents)
	}
}

func TestKubeDiagnosePodFakeRunner(t *testing.T) {
	fakeScript := `
if [ "$1" = "get" ] && [ "$2" = "pod" ]; then
  cat <<'EOF'
{
  "metadata": {"name": "web-1", "namespace": "app", "creationTimestamp": "2026-11-26T08:00:00Z"},
  "spec": {"nodeName": "n1", "containers": [{"name": "web", "image": "nginx:1.27"}]},
  "status": {
    "phase": "Running",
    "containerStatuses": [{
      "name": "web",
      "ready": false,
      "restartCount": 2,
      "state": {"waiting": {"reason": "CrashLoopBackOff"}},
      "lastState": {"terminated": {"reason": "Error", "exitCode": 1}}
    }]
  }
}
EOF
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "events" ]; then
  echo '{"items":[{"metadata":{"namespace":"app"},"involvedObject":{"kind":"Pod","name":"web-1"},"type":"Warning","reason":"BackOff","message":"Back-off restarting","count":2,"lastTimestamp":"2026-11-26T08:01:00Z"}]}'
  exit 0
fi
if [ "$1" = "logs" ]; then
  for a in "$@"; do
    if [ "$a" = "--previous" ]; then
      echo "prev line 1"
      echo "fatal: config missing"
      exit 0
    fi
  done
  echo "current line"
  exit 0
fi
echo "unhandled: $@" >&2
exit 1
`
	writeFakeKubectl(t, fakeScript)

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	cid, err := db.CreateKubeCluster(&store.KubeCluster{
		Name:       "fake-eks",
		Context:    "ctx",
		Kubeconfig: "apiVersion: v1\nkind: Config\n",
		KubeMode:   "full",
		Provider:   "eks",
	})
	if err != nil {
		t.Fatalf("CreateKubeCluster: %v", err)
	}

	ins := New(db, nil, nil)
	diag, err := ins.KubeDiagnosePod(-cid, "app", "web-1")
	if err != nil {
		t.Fatalf("KubeDiagnosePod: %v", err)
	}
	if len(diag.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(diag.Containers))
	}
	c := diag.Containers[0]
	if c.LogSource != "previous" {
		t.Fatalf("expected --previous log source when restartCount > 0, got %q", c.LogSource)
	}
	if len(c.LogLines) != 2 || !strings.Contains(c.LogLines[1], "fatal: config missing") {
		t.Fatalf("unexpected log lines: %+v", c.LogLines)
	}
	if len(diag.WarningEvents) != 1 || diag.WarningEvents[0].Reason != "BackOff" {
		t.Fatalf("unexpected warning events: %+v", diag.WarningEvents)
	}
}
