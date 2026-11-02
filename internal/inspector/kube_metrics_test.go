package inspector

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseKubeCPUMilli(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"142m", 142, false},
		{"0m", 0, false},
		{"3142857n", 3, false},
		{"430206433n", 430, false},
		{"1", 1000, false},
		{"8", 8000, false},
		{"0.5", 500, false},
		{"0", 0, false},
		{"2000u", 2, false},
		{"", 0, true},
		{"   ", 0, true},
		{"abc", 0, true},
		{"-5m", 0, true},
		{"-1", 0, true},
		{"142x", 0, true},
	}
	for _, tc := range cases {
		got, err := parseKubeCPUMilli(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseKubeCPUMilli(%q) expected error, got %d", tc.input, got)
			}
		} else {
			if err != nil {
				t.Errorf("parseKubeCPUMilli(%q) unexpected error: %v", tc.input, err)
			} else if got != tc.expected {
				t.Errorf("parseKubeCPUMilli(%q) = %d, expected %d", tc.input, got, tc.expected)
			}
		}
	}
}

func TestParseKubeMemoryBytes(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"16107360Ki", 16107360 * 1024, false},
		{"3724780Ki", 3724780 * 1024, false},
		{"512Mi", 512 * 1024 * 1024, false},
		{"1Gi", 1024 * 1024 * 1024, false},
		{"2Ti", 2 * 1024 * 1024 * 1024 * 1024, false},
		{"1048576", 1048576, false},
		{"0", 0, false},
		{"1000k", 1000000, false},
		{"10M", 10000000, false},
		{"1.5Gi", int64(1.5 * 1024 * 1024 * 1024), false},
		{"", 0, true},
		{"invalid", 0, true},
		{"-10Ki", 0, true},
		{"-1", 0, true},
	}
	for _, tc := range cases {
		got, err := parseKubeMemoryBytes(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseKubeMemoryBytes(%q) expected error, got %d", tc.input, got)
			}
		} else {
			if err != nil {
				t.Errorf("parseKubeMemoryBytes(%q) unexpected error: %v", tc.input, err)
			} else if got != tc.expected {
				t.Errorf("parseKubeMemoryBytes(%q) = %d, expected %d", tc.input, got, tc.expected)
			}
		}
	}
}

func TestParseKubeNodesWithMetrics(t *testing.T) {
	fixture := `===NODES===
{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "metadata": {
        "name": "odin01.torcons.ai",
        "labels": {"node-role.kubernetes.io/control-plane": "true"}
      },
      "status": {
        "addresses": [{"type": "InternalIP", "address": "10.10.1.10"}],
        "capacity": {"cpu": "8", "memory": "16107368Ki"},
        "allocatable": {"cpu": "8", "memory": "16107368Ki"},
        "conditions": [{"type": "Ready", "status": "True"}],
        "nodeInfo": {"kubeletVersion": "v1.36.5+k3s1"}
      }
    },
    {
      "metadata": {
        "name": "loki01.torcons.ai",
        "labels": {"node-role.kubernetes.io/control-plane": "true"}
      },
      "status": {
        "addresses": [{"type": "InternalIP", "address": "10.10.1.14"}],
        "capacity": {"cpu": "8", "memory": "16107360Ki"},
        "allocatable": {"cpu": "8", "memory": "16107360Ki"},
        "conditions": [{"type": "Ready", "status": "True"}],
        "nodeInfo": {"kubeletVersion": "v1.36.5+k3s1"}
      }
    }
  ]
}
===METRICS===
{
  "kind": "NodeMetricsList",
  "apiVersion": "metrics.k8s.io/v1beta1",
  "metadata": {},
  "items": [
    {
      "metadata": {"name": "odin01.torcons.ai"},
      "timestamp": "2026-11-02T10:30:33Z",
      "window": "20.055s",
      "usage": {"cpu": "424260084n", "memory": "4094132Ki"}
    },
    {
      "metadata": {"name": "loki01.torcons.ai"},
      "timestamp": "2026-11-02T10:30:28Z",
      "window": "10.028s",
      "usage": {"cpu": "374509074n", "memory": "3711900Ki"}
    }
  ]
}`

	nodes, err := parseKubeNodes(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	odin := nodes[0]
	if odin.Name != "odin01.torcons.ai" {
		t.Errorf("expected odin node, got %s", odin.Name)
	}
	if odin.AllocatableCPU != "8" {
		t.Errorf("expected AllocatableCPU 8, got %q", odin.AllocatableCPU)
	}
	if odin.AllocatableMem != "16107368Ki" {
		t.Errorf("expected AllocatableMem 16107368Ki, got %q", odin.AllocatableMem)
	}
	if odin.UsageCPU != "424m" {
		t.Errorf("expected UsageCPU 424m, got %q", odin.UsageCPU)
	}
	if odin.UsageMemory != "4094132Ki" {
		t.Errorf("expected UsageMemory 4094132Ki, got %q", odin.UsageMemory)
	}
	if odin.CPUPercent == nil || *odin.CPUPercent != 5.3 {
		t.Errorf("expected CPUPercent 5.3, got %v", odin.CPUPercent)
	}
	if odin.MemoryPercent == nil || *odin.MemoryPercent != 25.4 {
		t.Errorf("expected MemoryPercent 25.4, got %v", odin.MemoryPercent)
	}

	loki := nodes[1]
	if loki.UsageCPU != "375m" {
		t.Errorf("expected loki UsageCPU 375m, got %q", loki.UsageCPU)
	}
	if loki.UsageMemory != "3711900Ki" {
		t.Errorf("expected loki UsageMemory 3711900Ki, got %q", loki.UsageMemory)
	}
	if loki.CPUPercent == nil || *loki.CPUPercent != 4.7 {
		t.Errorf("expected loki CPUPercent 4.7, got %v", loki.CPUPercent)
	}
	if loki.MemoryPercent == nil || *loki.MemoryPercent != 23.0 {
		t.Errorf("expected loki MemoryPercent 23.0, got %v", loki.MemoryPercent)
	}
}

func TestParseKubeNodesMissingAllocatable(t *testing.T) {
	fixture := `===NODES===
{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "metadata": {"name": "bare-node"},
      "status": {
        "capacity": {"cpu": "4", "memory": "8000000Ki"},
        "conditions": [{"type": "Ready", "status": "True"}]
      }
    }
  ]
}
===METRICS===
{
  "kind": "NodeMetricsList",
  "items": [
    {
      "metadata": {"name": "bare-node"},
      "usage": {"cpu": "200m", "memory": "2000000Ki"}
    }
  ]
}`

	nodes, err := parseKubeNodes(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.UsageCPU != "200m" {
		t.Errorf("expected UsageCPU 200m, got %q", n.UsageCPU)
	}
	if n.AllocatableCPU != "" {
		t.Errorf("expected empty AllocatableCPU, got %q", n.AllocatableCPU)
	}
	if n.CPUPercent != nil {
		t.Errorf("expected nil CPUPercent when allocatable missing, got %v", *n.CPUPercent)
	}
	if n.MemoryPercent != nil {
		t.Errorf("expected nil MemoryPercent when allocatable missing, got %v", *n.MemoryPercent)
	}
}

func TestParseKubePodsWithMetrics(t *testing.T) {
	fixture := `===PODS===
{
  "apiVersion": "v1",
  "kind": "PodList",
  "items": [
    {
      "metadata": {
        "name": "cnpg-controller-manager-5fbdd6bb78-xz6rw",
        "namespace": "cnpg-system",
        "creationTimestamp": "2026-11-02T10:30:33Z"
      },
      "spec": {
        "containers": [{"name": "manager"}]
      },
      "status": {
        "phase": "Running",
        "containerStatuses": [{"ready": true, "restartCount": 0}]
      }
    },
    {
      "metadata": {
        "name": "multi-container-pod",
        "namespace": "default",
        "creationTimestamp": "2026-11-02T10:30:33Z"
      },
      "spec": {
        "containers": [{"name": "c1"}, {"name": "c2"}]
      },
      "status": {
        "phase": "Running",
        "containerStatuses": [{"ready": true, "restartCount": 0}, {"ready": true, "restartCount": 0}]
      }
    }
  ]
}
===METRICS===
{
  "kind": "PodMetricsList",
  "apiVersion": "metrics.k8s.io/v1beta1",
  "metadata": {},
  "items": [
    {
      "metadata": {
        "name": "cnpg-controller-manager-5fbdd6bb78-xz6rw",
        "namespace": "cnpg-system"
      },
      "containers": [
        {
          "name": "manager",
          "usage": {"cpu": "1955086n", "memory": "38496Ki"}
        }
      ]
    },
    {
      "metadata": {
        "name": "multi-container-pod",
        "namespace": "default"
      },
      "containers": [
        {
          "name": "c1",
          "usage": {"cpu": "50m", "memory": "100Mi"}
        },
        {
          "name": "c2",
          "usage": {"cpu": "25m", "memory": "50Mi"}
        }
      ]
    }
  ]
}`

	pods, err := parseKubePods(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(pods))
	}

	p1 := pods[0]
	if p1.UsageCPU != "2m" {
		t.Errorf("expected UsageCPU 2m, got %q", p1.UsageCPU)
	}
	if p1.UsageMemory != "38496Ki" {
		t.Errorf("expected UsageMemory 38496Ki, got %q", p1.UsageMemory)
	}

	p2 := pods[1]
	if p2.UsageCPU != "75m" {
		t.Errorf("expected UsageCPU 75m, got %q", p2.UsageCPU)
	}
	// 150 Mi = 150 * 1024 Ki = 153600Ki
	if p2.UsageMemory != "153600Ki" {
		t.Errorf("expected UsageMemory 153600Ki, got %q", p2.UsageMemory)
	}
}

func TestParseKubeMetricsNonFatalErrors(t *testing.T) {
	nodeBase := `===NODES===
{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "metadata": {"name": "test-node"},
      "status": {
        "capacity": {"cpu": "4", "memory": "8000000Ki"},
        "allocatable": {"cpu": "4", "memory": "8000000Ki"},
        "conditions": [{"type": "Ready", "status": "True"}]
      }
    }
  ]
}`

	podBase := `===PODS===
{
  "apiVersion": "v1",
  "kind": "PodList",
  "items": [
    {
      "metadata": {"name": "test-pod", "namespace": "default"},
      "spec": {"containers": [{"name": "app"}]},
      "status": {"phase": "Running"}
    }
  ]
}`

	errorMetrics := []struct {
		desc    string
		metrics string
	}{
		{"404 Status", `{"kind":"Status","status":"Failure","message":"not found","code":404}`},
		{"503 Status", `{"kind":"Status","status":"Failure","message":"service unavailable","code":503}`},
		{"empty string", ``},
		{"whitespace only", `   `},
		{"plain error string", `Error from server (NotFound): the server could not find the requested resource`},
		{"empty node items", `{"kind":"NodeMetricsList","items":[]}`},
		{"empty pod items", `{"kind":"PodMetricsList","items":[]}`},
		{"oversized payload", `{"kind":"PodMetricsList","items":[` + strings.Repeat(`{"metadata":{"name":"x","namespace":"y"}},`, 150000) + `{}]}`},
	}

	for _, tc := range errorMetrics {
		t.Run("node_"+tc.desc, func(t *testing.T) {
			input := fmt.Sprintf("%s\n===METRICS===\n%s", nodeBase, tc.metrics)
			nodes, err := parseKubeNodes(input)
			if err != nil {
				t.Fatalf("parseKubeNodes failed on non-fatal %s: %v", tc.desc, err)
			}
			if len(nodes) != 1 {
				t.Fatalf("expected 1 node, got %d", len(nodes))
			}
			if nodes[0].UsageCPU != "" || nodes[0].UsageMemory != "" || nodes[0].CPUPercent != nil {
				t.Errorf("expected empty metrics on %s, got cpu=%q mem=%q pct=%v", tc.desc, nodes[0].UsageCPU, nodes[0].UsageMemory, nodes[0].CPUPercent)
			}
		})

		t.Run("pod_"+tc.desc, func(t *testing.T) {
			input := fmt.Sprintf("%s\n===METRICS===\n%s", podBase, tc.metrics)
			pods, err := parseKubePods(input)
			if err != nil {
				t.Fatalf("parseKubePods failed on non-fatal %s: %v", tc.desc, err)
			}
			if len(pods) != 1 {
				t.Fatalf("expected 1 pod, got %d", len(pods))
			}
			if pods[0].UsageCPU != "" || pods[0].UsageMemory != "" {
				t.Errorf("expected empty metrics on %s, got cpu=%q mem=%q", tc.desc, pods[0].UsageCPU, pods[0].UsageMemory)
			}
		})
	}
}
