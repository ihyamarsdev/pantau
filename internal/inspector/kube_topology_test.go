package inspector

import "testing"

func TestBuildKubeTopology(t *testing.T) {
	raw := `{
  "items": [
    {
      "kind": "Ingress",
      "metadata": { "name": "web-ing", "namespace": "prod" },
      "spec": {
        "rules": [{
          "host": "app.example.com",
          "http": { "paths": [{ "backend": { "service": { "name": "web-svc" } } }] }
        }]
      }
    },
    {
      "kind": "Service",
      "metadata": { "name": "web-svc", "namespace": "prod" },
      "spec": { "type": "ClusterIP", "clusterIP": "10.43.0.10", "selector": { "app": "web" } }
    },
    {
      "kind": "Deployment",
      "metadata": { "name": "web", "namespace": "prod" },
      "spec": { "replicas": 2 },
      "status": { "readyReplicas": 1, "replicas": 2 }
    },
    {
      "kind": "ReplicaSet",
      "metadata": {
        "name": "web-rs",
        "namespace": "prod",
        "ownerReferences": [{ "kind": "Deployment", "name": "web" }]
      }
    },
    {
      "kind": "Pod",
      "metadata": {
        "name": "web-pod-1",
        "namespace": "prod",
        "labels": { "app": "web" },
        "ownerReferences": [{ "kind": "ReplicaSet", "name": "web-rs" }]
      },
      "spec": {
        "volumes": [{ "persistentVolumeClaim": { "claimName": "web-pvc" } }]
      },
      "status": { "phase": "Running", "podIP": "10.42.0.5" }
    },
    {
      "kind": "PersistentVolumeClaim",
      "metadata": { "name": "web-pvc", "namespace": "prod" },
      "spec": { "resources": { "requests": { "storage": "5Gi" } } },
      "status": { "phase": "Bound" }
    }
  ]
}`

	topo, err := buildKubeTopology("deployment", "prod", "web", raw)
	if err != nil {
		t.Fatalf("buildKubeTopology error: %v", err)
	}
	if topo.Root.ID != "Deployment:prod/web" {
		t.Fatalf("unexpected root ID: %s", topo.Root.ID)
	}
	for i, want := range []int{1, 1, 1, 1, 1} {
		if len(topo.Columns[i].Items) != want {
			t.Fatalf("column %s got %d items, want %d", topo.Columns[i].Stage, len(topo.Columns[i].Items), want)
		}
	}
	if len(topo.Edges) != 4 {
		t.Fatalf("got %d edges, want 4: %+v", len(topo.Edges), topo.Edges)
	}
	if topo.Columns[2].Items[0].Status != "Degraded" {
		t.Fatalf("expected Degraded workload status for 1/2 ready, got %s", topo.Columns[2].Items[0].Status)
	}
}
