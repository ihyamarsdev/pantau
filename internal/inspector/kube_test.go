package inspector

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestKubeValidation(t *testing.T) {
	validNS := []string{"default", "kube-system", "pantau-app", "ns-1", "a", "k8s-123"}
	for _, ns := range validNS {
		if !ValidKubeNamespace(ns) {
			t.Errorf("expected valid namespace for %q", ns)
		}
	}

	invalidNS := []string{
		"Kube-System",   // uppercase
		"default;reboot", // injection semicolon
		"a;rm -rf /",     // injection
		"$(x)",           // injection command substitution
		"`ls`",           // backticks
		"ns\nline",       // newline
		"",               // empty
		"-starts-hyphen", // leading hyphen
		"ends-hyphen-",   // trailing hyphen
		"has.dot",        // namespace cannot contain dots
		"has_underscore", // no underscores
		strings.Repeat("a", 64), // over 63 chars
	}
	for _, ns := range invalidNS {
		if ValidKubeNamespace(ns) {
			t.Errorf("expected invalid namespace for %q", ns)
		}
	}

	validNames := []string{"web", "my-app", "node-1.k8s.local", "a", "ingress-controller-123"}
	for _, name := range validNames {
		if !ValidKubeName(name) {
			t.Errorf("expected valid resource name for %q", name)
		}
	}

	invalidNames := []string{
		"Web",           // uppercase
		"a;rm -rf /",    // injection
		"$(x)",          // injection
		"name\nfoo",     // newline
		"",              // empty
		"-starts-dash",  // leading hyphen
		"ends-dash-",    // trailing hyphen
		strings.Repeat("a", 254), // over 253 chars
	}
	for _, name := range invalidNames {
		if ValidKubeName(name) {
			t.Errorf("expected invalid resource name for %q", name)
		}
	}
}

func TestKubeActionCommands(t *testing.T) {
	r3 := 3
	r0 := 0
	rBad := 1001
	rNeg := -1

	tests := []struct {
		name       string
		req        KubeActionReq
		wantCmd    string
		wantTarget string
		wantErr    bool
	}{
		{
			name: "scale deployment",
			req: KubeActionReq{
				Op:        "scale",
				Kind:      "deployment",
				Namespace: "prod",
				Name:      "web",
				Replicas:  &r3,
			},
			wantCmd:    "kc scale deployment 'web' -n 'prod' --replicas=3",
			wantTarget: "deployment/web",
		},
		{
			name: "scale statefulset to 0",
			req: KubeActionReq{
				Op:        "scale",
				Kind:      "statefulset",
				Namespace: "db",
				Name:      "redis-cluster",
				Replicas:  &r0,
			},
			wantCmd:    "kc scale statefulset 'redis-cluster' -n 'db' --replicas=0",
			wantTarget: "statefulset/redis-cluster",
		},
		{
			name: "scale invalid replicas > 1000",
			req: KubeActionReq{
				Op:        "scale",
				Kind:      "deployment",
				Namespace: "prod",
				Name:      "web",
				Replicas:  &rBad,
			},
			wantErr: true,
		},
		{
			name: "scale negative replicas",
			req: KubeActionReq{
				Op:        "scale",
				Kind:      "deployment",
				Namespace: "prod",
				Name:      "web",
				Replicas:  &rNeg,
			},
			wantErr: true,
		},
		{
			name: "scale nil replicas",
			req: KubeActionReq{
				Op:        "scale",
				Kind:      "deployment",
				Namespace: "prod",
				Name:      "web",
			},
			wantErr: true,
		},
		{
			name: "scale invalid kind daemonset",
			req: KubeActionReq{
				Op:        "scale",
				Kind:      "daemonset",
				Namespace: "prod",
				Name:      "agent",
				Replicas:  &r3,
			},
			wantErr: true,
		},
		{
			name: "restart deployment",
			req: KubeActionReq{
				Op:        "restart",
				Kind:      "deployment",
				Namespace: "default",
				Name:      "api",
			},
			wantCmd:    "kc rollout restart 'deployment/api' -n 'default'",
			wantTarget: "deployment/api",
		},
		{
			name: "restart daemonset",
			req: KubeActionReq{
				Op:        "restart",
				Kind:      "daemonset",
				Namespace: "monitoring",
				Name:      "prom-node",
			},
			wantCmd:    "kc rollout restart 'daemonset/prom-node' -n 'monitoring'",
			wantTarget: "daemonset/prom-node",
		},
		{
			name: "restart invalid kind pod",
			req: KubeActionReq{
				Op:        "restart",
				Kind:      "pod",
				Namespace: "default",
				Name:      "api-pod",
			},
			wantErr: true,
		},
		{
			name: "delete pod",
			req: KubeActionReq{
				Op:        "delete",
				Kind:      "pod",
				Namespace: "default",
				Name:      "api-789f",
			},
			wantCmd:    "kc delete pod 'api-789f' -n 'default'",
			wantTarget: "pod/api-789f",
		},
		{
			name: "delete invalid kind deployment",
			req: KubeActionReq{
				Op:        "delete",
				Kind:      "deployment",
				Namespace: "default",
				Name:      "api",
			},
			wantErr: true,
		},
		{
			name: "cordon node",
			req: KubeActionReq{
				Op:   "cordon",
				Kind: "node",
				Name: "worker-1",
			},
			wantCmd:    "kc cordon 'worker-1'",
			wantTarget: "worker-1",
		},
		{
			name: "uncordon node",
			req: KubeActionReq{
				Op:   "uncordon",
				Kind: "node",
				Name: "worker-1",
			},
			wantCmd:    "kc uncordon 'worker-1'",
			wantTarget: "worker-1",
		},
		{
			name: "cordon invalid kind pod",
			req: KubeActionReq{
				Op:   "cordon",
				Kind: "pod",
				Name: "web-1",
			},
			wantErr: true,
		},
		{
			name: "injection in name",
			req: KubeActionReq{
				Op:        "delete",
				Kind:      "pod",
				Namespace: "default",
				Name:      "web;rm -rf /",
			},
			wantErr: true,
		},
		{
			name: "injection in namespace",
			req: KubeActionReq{
				Op:        "restart",
				Kind:      "deployment",
				Namespace: "def$(reboot)",
				Name:      "web",
			},
			wantErr: true,
		},
		{
			name: "unsupported op",
			req: KubeActionReq{
				Op:        "drain",
				Kind:      "node",
				Name:      "worker-1",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, target, err := kubeActionCommand(tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, ErrKubeInvalid) {
					t.Fatalf("expected ErrKubeInvalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd != tc.wantCmd {
				t.Errorf("cmd mismatch: got %q, want %q", cmd, tc.wantCmd)
			}
			if target != tc.wantTarget {
				t.Errorf("target mismatch: got %q, want %q", target, tc.wantTarget)
			}
		})
	}
}

func TestKubeApplyValidationAndScript(t *testing.T) {
	if err := validateKubeApply(""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("empty yaml must return ErrKubeInvalid, got %v", err)
	}
	if err := validateKubeApply("   \n\t  "); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("whitespace-only yaml must return ErrKubeInvalid, got %v", err)
	}
	tooBig := strings.Repeat("a", kubeYAMLMax+1)
	if err := validateKubeApply(tooBig); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("oversized yaml must return ErrKubeInvalid, got %v", err)
	}
	nulByte := "apiVersion: v1\x00kind: Pod"
	if err := validateKubeApply(nulByte); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("yaml with NUL must return ErrKubeInvalid, got %v", err)
	}

	validYAML := "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: test\n"
	if err := validateKubeApply(validYAML); err != nil {
		t.Fatalf("valid yaml returned error: %v", err)
	}

	script := kubeApplyScript("dGVzdA==")
	if !strings.Contains(script, "kc apply --dry-run=server -f \"$TMP\"") {
		t.Errorf("script must contain dry-run check: %s", script)
	}
	if !strings.Contains(script, "kc apply -f \"$TMP\"") {
		t.Errorf("script must contain actual apply: %s", script)
	}
	if !strings.Contains(script, "base64") {
		t.Errorf("script must decode base64: %s", script)
	}
}

func TestKubeShellPrelude(t *testing.T) {
	if strings.Contains(kubePrelude, "DarwinPathPrelude") {
		t.Errorf("prelude must not reference DarwinPathPrelude")
	}
	if !strings.Contains(kubePrelude, "command -v k3s) kubectl") {
		t.Errorf("prelude must handle k3s kubectl")
	}
	if !strings.Contains(kubePrelude, "/etc/rancher/k3s/k3s.yaml") {
		t.Errorf("prelude must check k3s kubeconfig")
	}
	if !strings.Contains(kubePrelude, "/var/lib/rancher/rke2/bin/kubectl") {
		t.Errorf("prelude must handle rke2 kubectl")
	}
	if !strings.Contains(kubePrelude, "/etc/rancher/rke2/rke2.yaml") {
		t.Errorf("prelude must check rke2 kubeconfig")
	}
	if !strings.Contains(kubePrelude, "no k3s/rke2 control plane found") {
		t.Errorf("prelude must exit with 'no k3s/rke2 control plane found' on failure")
	}
	if !strings.Contains(kubePrelude, "sudo -n env KUBECONFIG=") {
		t.Errorf("prelude must preserve KUBECONFIG under sudo fallback")
	}
}

func TestParseKubeDetect(t *testing.T) {
	out := `===DISTRO===
k3s
===VERSION===
{
  "clientVersion": {"gitVersion": "v1.31.2+k3s1"},
  "serverVersion": {"gitVersion": "v1.31.2+k3s1"}
}
`
	info, err := parseKubeDetect(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Distro != "k3s" || info.Version != "v1.31.2+k3s1" || info.Role != "server" {
		t.Errorf("unexpected info: %+v", info)
	}

	// Plain text version fallback
	outText := `===DISTRO===
rke2
===VERSION===
rke2 version v1.30.4+rke2r1 (commit abc)
`
	info2, err := parseKubeDetect(outText)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info2.Distro != "rke2" || info2.Version != "v1.30.4+rke2r1" || info2.Role != "server" {
		t.Errorf("unexpected info: %+v", info2)
	}

	// Neither control plane
	outNone := `===DISTRO===

===VERSION===
`
	if _, err := parseKubeDetect(outNone); err == nil {
		t.Errorf("expected error when no distro found")
	}
}

func TestParseKubeNodes(t *testing.T) {
	fixture := `{
  "apiVersion": "v1",
  "kind": "NodeList",
  "items": [
    {
      "metadata": {
        "name": "master-0",
        "creationTimestamp": "2026-01-15T08:00:00Z",
        "labels": {
          "node-role.kubernetes.io/control-plane": "",
          "node-role.kubernetes.io/master": ""
        }
      },
      "spec": {
        "unschedulable": false
      },
      "status": {
        "addresses": [
          {"type": "InternalIP", "address": "192.168.1.50"},
          {"type": "Hostname", "address": "master-0"}
        ],
        "capacity": {
          "cpu": "8",
          "memory": "32890120Ki"
        },
        "conditions": [
          {"type": "Ready", "status": "True"}
        ],
        "nodeInfo": {
          "kubeletVersion": "v1.31.2+k3s1"
        }
      }
    },
    {
      "metadata": {
        "name": "worker-1",
        "creationTimestamp": "2026-01-16T09:30:00Z",
        "labels": {
          "node-role.kubernetes.io/worker": ""
        }
      },
      "spec": {
        "unschedulable": true
      },
      "status": {
        "addresses": [
          {"type": "InternalIP", "address": "192.168.1.51"}
        ],
        "capacity": {
          "cpu": "16",
          "memory": "65780240Ki"
        },
        "conditions": [
          {"type": "Ready", "status": "False"}
        ],
        "nodeInfo": {
          "kubeletVersion": "v1.31.2+k3s1"
        }
      }
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

	n0 := nodes[0]
	if n0.Name != "master-0" || n0.Status != "Ready" || n0.Roles != "control-plane,master" ||
		n0.InternalIP != "192.168.1.50" || n0.CPU != "8" || n0.Memory != "32890120Ki" ||
		n0.Version != "v1.31.2+k3s1" || n0.Unschedulable != false {
		t.Errorf("node 0 mismatch: %+v", n0)
	}

	n1 := nodes[1]
	if n1.Name != "worker-1" || n1.Status != "NotReady" || n1.Roles != "worker" ||
		n1.InternalIP != "192.168.1.51" || n1.Unschedulable != true {
		t.Errorf("node 1 mismatch: %+v", n1)
	}
}

func TestParseKubeNamespaces(t *testing.T) {
	fixture := `{
  "apiVersion": "v1",
  "kind": "NamespaceList",
  "items": [
    {
      "metadata": {"name": "default", "creationTimestamp": "2026-01-01T00:00:00Z"},
      "status": {"phase": "Active"}
    },
    {
      "metadata": {"name": "terminating-ns", "creationTimestamp": "2026-01-02T00:00:00Z"},
      "status": {"phase": "Terminating"}
    }
  ]
}`
	nss, err := parseKubeNamespaces(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nss) != 2 {
		t.Fatalf("expected 2 namespaces, got %d", len(nss))
	}
	if nss[0].Name != "default" || nss[0].Status != "Active" {
		t.Errorf("ns 0 mismatch: %+v", nss[0])
	}
	if nss[1].Name != "terminating-ns" || nss[1].Status != "Terminating" {
		t.Errorf("ns 1 mismatch: %+v", nss[1])
	}
}

func TestParseKubeWorkloads(t *testing.T) {
	fixture := `{
  "apiVersion": "v1",
  "kind": "List",
  "items": [
    {
      "kind": "Deployment",
      "metadata": {"name": "web", "namespace": "prod", "creationTimestamp": "2026-02-01T10:00:00Z"},
      "spec": {
        "replicas": 3,
        "template": {"spec": {"containers": [{"image": "nginx:alpine"}, {"image": "prom/sidecar:v1"}]}}
      },
      "status": {"readyReplicas": 3, "availableReplicas": 3}
    },
    {
      "kind": "StatefulSet",
      "metadata": {"name": "redis", "namespace": "cache", "creationTimestamp": "2026-02-02T11:00:00Z"},
      "spec": {
        "replicas": 1,
        "template": {"spec": {"containers": [{"image": "redis:7-alpine"}]}}
      },
      "status": {"readyReplicas": 1, "availableReplicas": 1}
    },
    {
      "kind": "DaemonSet",
      "metadata": {"name": "prom-node", "namespace": "monitoring", "creationTimestamp": "2026-02-03T12:00:00Z"},
      "spec": {
        "template": {"spec": {"containers": [{"image": "prom/node-exporter:v1.7.0"}]}}
      },
      "status": {"desiredNumberScheduled": 5, "numberReady": 4, "numberAvailable": 4}
    }
  ]
}`
	wls, err := parseKubeWorkloads(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wls) != 3 {
		t.Fatalf("expected 3 workloads, got %d", len(wls))
	}

	dep := wls[0]
	if dep.Kind != "Deployment" || dep.Namespace != "prod" || dep.Name != "web" ||
		dep.Desired != 3 || dep.Ready != 3 || dep.Available != 3 || len(dep.Images) != 2 ||
		dep.Images[0] != "nginx:alpine" || dep.Images[1] != "prom/sidecar:v1" {
		t.Errorf("deployment mismatch: %+v", dep)
	}

	sts := wls[1]
	if sts.Kind != "StatefulSet" || sts.Namespace != "cache" || sts.Name != "redis" ||
		sts.Desired != 1 || sts.Ready != 1 || sts.Available != 1 || len(sts.Images) != 1 {
		t.Errorf("statefulset mismatch: %+v", sts)
	}

	ds := wls[2]
	if ds.Kind != "DaemonSet" || ds.Namespace != "monitoring" || ds.Name != "prom-node" ||
		ds.Desired != 5 || ds.Ready != 4 || ds.Available != 4 || len(ds.Images) != 1 {
		t.Errorf("daemonset mismatch: %+v", ds)
	}
}

func TestParseKubePods(t *testing.T) {
	delTs := "2026-03-01T12:00:00Z"
	fixture := `{
  "apiVersion": "v1",
  "kind": "PodList",
  "items": [
    {
      "metadata": {
        "name": "web-7f89d-abc",
        "namespace": "prod",
        "creationTimestamp": "2026-03-01T10:00:00Z"
      },
      "spec": {
        "nodeName": "worker-1",
        "containers": [{"name": "nginx"}, {"name": "proxy"}]
      },
      "status": {
        "phase": "Running",
        "podIP": "10.42.1.20",
        "containerStatuses": [
          {"name": "nginx", "ready": true, "restartCount": 1},
          {"name": "proxy", "ready": true, "restartCount": 0}
        ]
      }
    },
    {
      "metadata": {
        "name": "crashed-pod",
        "namespace": "prod",
        "creationTimestamp": "2026-03-01T11:00:00Z"
      },
      "spec": {
        "nodeName": "worker-2",
        "containers": [{"name": "app"}]
      },
      "status": {
        "phase": "Running",
        "podIP": "10.42.2.15",
        "containerStatuses": [
          {
            "name": "app",
            "ready": false,
            "restartCount": 5,
            "state": {"waiting": {"reason": "CrashLoopBackOff"}}
          }
        ]
      }
    },
    {
      "metadata": {
        "name": "dying-pod",
        "namespace": "prod",
        "creationTimestamp": "2026-03-01T09:00:00Z",
        "deletionTimestamp": "` + delTs + `"
      },
      "spec": {
        "nodeName": "worker-1",
        "containers": [{"name": "app"}]
      },
      "status": {
        "phase": "Running",
        "podIP": "10.42.1.18",
        "containerStatuses": [{"name": "app", "ready": true, "restartCount": 0}]
      }
    }
  ]
}`
	pods, err := parseKubePods(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(pods))
	}

	p0 := pods[0]
	if p0.Name != "web-7f89d-abc" || p0.Namespace != "prod" || p0.Ready != "2/2" ||
		p0.Status != "Running" || p0.Restarts != 1 || p0.Node != "worker-1" ||
		p0.IP != "10.42.1.20" || len(p0.Containers) != 2 {
		t.Errorf("pod 0 mismatch: %+v", p0)
	}

	p1 := pods[1]
	if p1.Name != "crashed-pod" || p1.Ready != "0/1" || p1.Status != "CrashLoopBackOff" || p1.Restarts != 5 {
		t.Errorf("pod 1 mismatch: %+v", p1)
	}

	p2 := pods[2]
	if p2.Name != "dying-pod" || p2.Status != "Terminating" {
		t.Errorf("pod 2 mismatch: %+v", p2)
	}
}

func TestParseKubeServices(t *testing.T) {
	fixture := `{
  "apiVersion": "v1",
  "kind": "ServiceList",
  "items": [
    {
      "metadata": {"name": "kubernetes", "namespace": "default", "creationTimestamp": "2026-01-01T00:00:00Z"},
      "spec": {
        "type": "ClusterIP",
        "clusterIP": "10.43.0.1",
        "ports": [{"port": 443, "protocol": "TCP"}]
      }
    },
    {
      "metadata": {"name": "web-svc", "namespace": "prod", "creationTimestamp": "2026-02-01T00:00:00Z"},
      "spec": {
        "type": "LoadBalancer",
        "clusterIP": "10.43.50.20",
        "ports": [{"port": 80, "nodePort": 30080, "protocol": "TCP"}, {"port": 443, "nodePort": 30443, "protocol": "TCP"}]
      },
      "status": {
        "loadBalancer": {"ingress": [{"ip": "198.51.100.10"}]}
      }
    }
  ]
}`
	svcs, err := parseKubeServices(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 2 {
		t.Fatalf("expected 2 services, got %d", len(svcs))
	}

	s0 := svcs[0]
	if s0.Name != "kubernetes" || s0.Type != "ClusterIP" || s0.ClusterIP != "10.43.0.1" ||
		s0.ExternalIP != "<none>" || s0.Ports != "443/TCP" {
		t.Errorf("service 0 mismatch: %+v", s0)
	}

	s1 := svcs[1]
	if s1.Name != "web-svc" || s1.Type != "LoadBalancer" || s1.ExternalIP != "198.51.100.10" ||
		s1.Ports != "80:30080/TCP,443:30443/TCP" {
		t.Errorf("service 1 mismatch: %+v", s1)
	}
}

func TestParseKubeEvents(t *testing.T) {
	fixture := `{
  "apiVersion": "v1",
  "kind": "EventList",
  "items": [
    {
      "metadata": {"namespace": "default", "creationTimestamp": "2026-03-01T10:00:00Z"},
      "involvedObject": {"kind": "Pod", "name": "web-pod"},
      "reason": "Scheduled",
      "message": "Successfully assigned default/web-pod to worker-1",
      "type": "Normal",
      "count": 1,
      "lastTimestamp": "2026-03-01T10:00:00Z"
    },
    {
      "metadata": {"namespace": "default", "creationTimestamp": "2026-03-01T10:05:00Z"},
      "involvedObject": {"kind": "Pod", "name": "web-pod"},
      "reason": "BackOff",
      "message": "Back-off restarting failed container",
      "type": "Warning",
      "count": 3,
      "lastTimestamp": "2026-03-01T10:05:00Z"
    }
  ]
}`
	events, err := parseKubeEvents(fixture)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Sorted newest first
	if events[0].Reason != "BackOff" || events[0].Count != 3 || events[0].Object != "Pod/web-pod" {
		t.Errorf("event 0 should be newest: %+v", events[0])
	}
	if events[1].Reason != "Scheduled" || events[1].Count != 1 {
		t.Errorf("event 1 mismatch: %+v", events[1])
	}
}

func TestKubeDescribeAndLogsValidation(t *testing.T) {
	ins := &Inspector{}
	// Test KubeDescribe input validation
	if _, err := ins.KubeDescribe(1, "invalid_kind", "default", "pod-1"); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid kind, got %v", err)
	}
	if _, err := ins.KubeDescribe(1, "pod", "UPPER_NS", "pod-1"); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid ns, got %v", err)
	}
	if _, err := ins.KubeDescribe(1, "pod", "default", "bad;name"); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid name, got %v", err)
	}

	// Test KubeLogs input validation
	if _, err := ins.KubeLogs(1, "UPPER_NS", "pod-1", "", 100); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid ns, got %v", err)
	}
	if _, err := ins.KubeLogs(1, "default", "bad;name", "", 100); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid pod, got %v", err)
	}
	if _, err := ins.KubeLogs(1, "default", "pod-1", "bad;container", 100); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid container, got %v", err)
	}
	if _, err := ins.KubeLogs(1, "default", "pod-1", "", -5); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for negative tail, got %v", err)
	}
	if _, err := ins.KubeLogs(1, "default", "pod-1", "", 2500); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for tail > 2000, got %v", err)
	}
}

func setupKubeInspector(t *testing.T) (*Inspector, int64) {
	dbPath := filepath.Join(t.TempDir(), "kube_test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hostID, err := db.CreateHost(&store.Host{Name: "test-host", Host: "localhost"})
	if err != nil {
		t.Fatalf("failed to add test host: %v", err)
	}

	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return sshrunner.NewMockRunner(), nil
	}, nil)
	return ins, hostID
}

func TestKubeDescribeExtendedKinds(t *testing.T) {
	ins, hostID := setupKubeInspector(t)
	// Extended describe kinds must be accepted (not ErrKubeInvalid for kind)
	for _, kind := range []string{"ingress", "configmap", "cm", "pvc", "job", "cronjob"} {
		_, err := ins.KubeDescribe(hostID, kind, "default", "res-1")
		if errors.Is(err, ErrKubeInvalid) {
			t.Errorf("kind %s should be supported, got ErrKubeInvalid: %v", kind, err)
		}
	}
	// pv is cluster-scoped: valid name with empty namespace should not fail namespace validation
	_, err := ins.KubeDescribe(hostID, "pv", "", "pv-1")
	if errors.Is(err, ErrKubeInvalid) {
		t.Errorf("pv describe should be supported without namespace, got %v", err)
	}
	// secret must NOT be supported in describe
	if _, err := ins.KubeDescribe(hostID, "secret", "default", "my-secret"); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("secret describe must be rejected with ErrKubeInvalid, got %v", err)
	}
}

func TestKubeExecCmd(t *testing.T) {
	ins := &Inspector{}

	// 1. Valid without container
	cmd, err := ins.KubeExecCmd("default", "my-pod", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cmd, "sh -c '") {
		t.Errorf("expected sh -c wrapper, got: %s", cmd)
	}
	if !strings.Contains(cmd, "exec -it") {
		t.Errorf("expected exec -it in cmd, got: %s", cmd)
	}
	if !strings.Contains(cmd, "kc exec -it -n '\\''default'\\'' '\\''my-pod'\\'' --") {
		t.Errorf("expected quoted ns and pod in cmd, got: %s", cmd)
	}
	if strings.Contains(cmd, "'\\''my-pod'\\'' -c") {
		t.Errorf("did not expect container -c flag when container is empty, got: %s", cmd)
	}

	// 2. Valid with container
	cmdWithContainer, err := ins.KubeExecCmd("kube-system", "coredns-123", "coredns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmdWithContainer, "'\\''coredns-123'\\'' -c '\\''coredns'\\'' --") {
		t.Errorf("expected -c 'coredns' flag, got: %s", cmdWithContainer)
	}

	// 3. Validation rejections (injection attempts)
	badInputs := []struct {
		ns, pod, container string
	}{
		{"default; rm -rf /", "pod-1", ""},
		{"default", "pod-1; echo pwned", ""},
		{"default", "pod-1", "cont; dangerous"},
		{"UPPER_NS", "pod-1", ""},
		{"default", "UPPER_POD", ""},
		{"", "pod-1", ""},
		{"default", "", ""},
	}
	for _, tc := range badInputs {
		if _, err := ins.KubeExecCmd(tc.ns, tc.pod, tc.container); !errors.Is(err, ErrKubeInvalid) {
			t.Errorf("expected ErrKubeInvalid for input (%q, %q, %q), got %v", tc.ns, tc.pod, tc.container, err)
		}
	}
}

func TestKubeSecretsTemplateSafety(t *testing.T) {
	spec, ok := kubeResourceSpecs["secrets"]
	if !ok {
		t.Fatal("secrets spec not found")
	}
	tmpl := spec.template
	// MUST NOT output JSON or raw data
	if strings.Contains(tmpl, "-o json") || strings.Contains(tmpl, ".Data") {
		t.Errorf("secrets template must not contain json or .Data: %s", tmpl)
	}
	// Template must only print metadata + len .data, never raw secret content
	stripped := strings.ReplaceAll(tmpl, "len .data", "")
	stripped = strings.ReplaceAll(stripped, "if .data", "")
	if strings.Contains(stripped, ".data") {
		t.Errorf("secrets template accesses .data beyond existence check and length: %s", tmpl)
	}
}

func TestParseTabularResources(t *testing.T) {
	raw := "ns1\tname1\tval1\t2026-11-01T00:00:00Z\r\nns2\tname2\tval2\t2026-11-02T00:00:00Z\n"
	rows := parseTabularResources(raw, 4)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "ns1" || rows[0][1] != "name1" || rows[0][2] != "val1" || rows[0][3] != "2026-11-01T00:00:00Z" {
		t.Errorf("row 0 mismatch: %v", rows[0])
	}
	if rows[1][0] != "ns2" || rows[1][1] != "name2" || rows[1][2] != "val2" || rows[1][3] != "2026-11-02T00:00:00Z" {
		t.Errorf("row 1 mismatch: %v", rows[1])
	}

	// Empty string produces empty slice, not nil
	emptyRows := parseTabularResources("", 4)
	if emptyRows == nil || len(emptyRows) != 0 {
		t.Errorf("expected empty non-nil slice, got: %v", emptyRows)
	}

	// Missing columns are padded
	shortRaw := "ns1\tname1\n"
	padded := parseTabularResources(shortRaw, 4)
	if len(padded) != 1 || len(padded[0]) != 4 {
		t.Fatalf("expected 1 row of 4 columns, got %v", padded)
	}
	if padded[0][2] != "" || padded[0][3] != "" {
		t.Errorf("expected empty string padding, got %v", padded[0])
	}
}

func TestKubeResourcesValidation(t *testing.T) {
	ins, hostID := setupKubeInspector(t)
	// Unknown kind
	if _, _, err := ins.KubeResources(hostID, "bogus_kind", "default"); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for bogus kind, got %v", err)
	}
	// Invalid namespace
	if _, _, err := ins.KubeResources(hostID, "ingresses", "UPPER_NS"); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for invalid ns, got %v", err)
	}
	// Valid kinds pass validation
	for _, kind := range []string{"ingresses", "configmaps", "secrets", "pvcs", "pvs", "jobs", "cronjobs"} {
		cols, _, err := ins.KubeResources(hostID, kind, "default")
		if errors.Is(err, ErrKubeInvalid) {
			t.Errorf("kind %s should be supported, got ErrKubeInvalid: %v", kind, err)
		}
		if len(cols) == 0 {
			t.Errorf("kind %s returned empty columns", kind)
		}
	}
}
