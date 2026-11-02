package inspector

import (
	"strings"
	"testing"
)

// Fixture from real `kc drain NODE --ignore-daemonsets --delete-emptydir-data
// --dry-run=server --timeout=15s` output (ticket 08) with a PDB-blocked pod.
const drainFixture = `node/loki01.torcons.ai cordoned (server dry run)
Warning: ignoring DaemonSet-managed Pods: kube-system/svclb-traefik-01e42ac8-xrz2n
evicting pod default/kubernetes-bootcamp-85b8b57875-m8fsd (server dry run)
evicting pod kube-system/helm-install-traefik-9t4hc (server dry run)
evicting pod kube-system/coredns-8597cb5c8c-mrrxx (server dry run)
evicting pod torcons-chat/open-webui-1 (server dry run)
evicting pod kube-system/traefik-6c78f76497-6s2rr (server dry run)
evicting pod torcons-chat/redis-0 (server dry run)
evicting pod torcons-chat/torcons-db-1 (server dry run)
error when evicting pods/"torcons-db-1" -n "torcons-chat" (will retry after 5s): Cannot evict pod as it would violate the pod's disruption budget.
evicting pod torcons-chat/torcons-db-1 (server dry run)
error when evicting pods/"torcons-db-1" -n "torcons-chat" (will retry after 5s): Cannot evict pod as it would violate the pod's disruption budget.
There are pending pods in node "loki01.torcons.ai" when an error occurred: error when evicting pods/"torcons-db-1" -n "torcons-chat": global timeout reached: 15s
error: unable to drain node "loki01.torcons.ai" due to error: error when evicting pods/"torcons-db-1" -n "torcons-chat": global timeout reached: 15s, continuing command...
`

func TestParseKubeDrainPreview(t *testing.T) {
	prev := parseKubeDrainPreview(drainFixture, "loki01.torcons.ai")
	if prev.Node != "loki01.torcons.ai" {
		t.Errorf("node = %q", prev.Node)
	}
	if prev.PodCount != 7 {
		t.Errorf("pod_count = %d, want 7 (deduped)", prev.PodCount)
	}
	if len(prev.Pods) != 7 {
		t.Fatalf("pods = %d", len(prev.Pods))
	}
	first := prev.Pods[0]
	if first.Namespace != "default" || first.Name != "kubernetes-bootcamp-85b8b57875-m8fsd" {
		t.Errorf("first pod = %+v", first)
	}
	if len(prev.DaemonsetSkipped) != 1 || prev.DaemonsetSkipped[0] != "kube-system/svclb-traefik-01e42ac8-xrz2n" {
		t.Errorf("daemonset_skipped = %v", prev.DaemonsetSkipped)
	}
	if len(prev.Blocked) != 1 || prev.Blocked[0] != "torcons-chat/torcons-db-1" {
		t.Errorf("blocked = %v", prev.Blocked)
	}

	empty := parseKubeDrainPreview("node/x cordoned (server dry run)\n", "x")
	if empty.PodCount != 0 || empty.Pods == nil || empty.Blocked == nil || empty.DaemonsetSkipped == nil {
		t.Errorf("empty preview wrong: %+v", empty)
	}
}

func TestKubeDrainCommand(t *testing.T) {
	mk := func(req KubeActionReq) string {
		cmd, _, err := kubeActionCommand(req)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		return cmd
	}

	// Defaults: ignoreDaemonsets on, emptydir off, force off, timeout 120.
	cmd := mk(KubeActionReq{Op: "drain", Kind: "node", Name: "node-1"})
	if !strings.Contains(cmd, "kc drain 'node-1' --ignore-daemonsets --timeout=120s") {
		t.Errorf("default cmd = %q", cmd)
	}
	if strings.Contains(cmd, "delete-emptydir") || strings.Contains(cmd, "--force") {
		t.Errorf("default cmd has unexpected flags: %q", cmd)
	}

	cmd = mk(KubeActionReq{Op: "drain", Kind: "node", Name: "node-1",
		IgnoreDaemonsets: new(false), DeleteEmptyDirData: new(true), Force: new(true), TimeoutSeconds: new(999)})
	if strings.Contains(cmd, "ignore-daemonsets") {
		t.Errorf("flags off: %q", cmd)
	}
	if !strings.Contains(cmd, "--delete-emptydir-data --force --timeout=300s") {
		t.Errorf("flags on: %q", cmd)
	}

	// Timeout clamp: 0/invalid -> 120, >300 -> 300.
	if !strings.Contains(mk(KubeActionReq{Op: "drain", Kind: "node", Name: "n", TimeoutSeconds: new(0)}), "--timeout=120s") {
		t.Error("invalid timeout should default to 120")
	}

	// Injection rejection.
	for _, bad := range []string{"a;rm", "$(x)", "node\n1", ""} {
		if _, _, err := kubeActionCommand(KubeActionReq{Op: "drain", Kind: "node", Name: bad}); err == nil {
			t.Errorf("expected rejection for %q", bad)
		}
	}
	if _, _, err := kubeActionCommand(KubeActionReq{Op: "drain", Kind: "pod", Name: "x"}); err == nil {
		t.Error("non-node kind must be rejected")
	}
}

func TestDrainPreviewValidation(t *testing.T) {
	ins := &Inspector{}
	if _, err := ins.KubeDrainPreview(0, "a;rm"); err == nil {
		t.Error("injected node name must be rejected")
	}
	if _, err := ins.KubeDrainPreview(0, ""); err == nil {
		t.Error("empty node name must be rejected")
	}
}

func TestDrainActionErrorText(t *testing.T) {
	// Sanity: uncordon recovery hint is frontend copy; here we assert the
	// command form keeps 2>&1 so stderr reaches the timeout message.
	cmd, _, err := kubeActionCommand(KubeActionReq{Op: "drain", Kind: "node", Name: "n1"})
	if err != nil || !strings.HasSuffix(cmd, " 2>&1") {
		t.Errorf("cmd = %q err = %v", cmd, err)
	}
}
