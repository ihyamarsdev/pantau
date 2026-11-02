package inspector

import (
	"fmt"
	"strings"
)

type KubeDrainPod struct {
	Namespace string `json:"ns"`
	Name      string `json:"name"`
}

type KubeDrainPreview struct {
	Node             string         `json:"node"`
	Pods             []KubeDrainPod `json:"pods"`
	PodCount         int            `json:"pod_count"`
	DaemonsetSkipped []string       `json:"daemonset_skipped"`
	Blocked          []string       `json:"blocked"`
}

// KubeDrainPreview runs a server-side dry-run drain (no cluster change) and
// reports which pods would be evicted, which DaemonSet pods are skipped, and
// which pods are blocked (e.g. PDB).
func (ins *Inspector) KubeDrainPreview(hostID int64, name string) (*KubeDrainPreview, error) {
	name = strings.TrimSpace(name)
	if !ValidKubeName(name) {
		return nil, kubeInvalid("invalid node name: %s", name)
	}
	cmd := fmt.Sprintf("kc drain %s --ignore-daemonsets --delete-emptydir-data --dry-run=server --timeout=15s 2>&1", shellQuote(name))
	stdout, stderr, exitCode, err := ins.kubeExec(hostID, cmd, kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	// A PDB-blocked drain exits non-zero after --timeout; the output is still
	// the preview. Only fail when we got nothing parseable at all.
	out := stdout
	if strings.TrimSpace(out) == "" {
		out = stderr
	}
	if strings.TrimSpace(out) == "" && exitCode != 0 {
		return nil, kubeCommandError("kube drain preview", stdout, stderr, exitCode)
	}
	return parseKubeDrainPreview(out, name), nil
}

// parseKubeDrainPreview parses `kc drain --dry-run=server` output:
//
//	node/X cordoned (server dry run)
//	Warning: ignoring DaemonSet-managed Pods: ns/ds-pod ns/ds-pod2
//	evicting pod ns/name (server dry run)
//	error when evicting pods/"name" -n "ns" (will retry after 5s): ...
func parseKubeDrainPreview(out, node string) *KubeDrainPreview {
	prev := &KubeDrainPreview{Node: node, Pods: []KubeDrainPod{}, DaemonsetSkipped: []string{}, Blocked: []string{}}
	seen := map[string]bool{}
	blockedSeen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "evicting pod ") {
			rest := strings.TrimPrefix(line, "evicting pod ")
			rest = strings.TrimSuffix(rest, "(server dry run)")
			rest = strings.TrimSpace(rest)
			if i := strings.IndexByte(rest, '/'); i > 0 && !seen[rest] {
				seen[rest] = true
				prev.Pods = append(prev.Pods, KubeDrainPod{Namespace: rest[:i], Name: rest[i+1:]})
			}
			continue
		}
		if strings.HasPrefix(line, "Warning: ignoring DaemonSet-managed Pods:") {
			for _, f := range strings.Fields(strings.TrimPrefix(line, "Warning: ignoring DaemonSet-managed Pods:")) {
				if f != "(server" && f != "dry" && f != "run)" && !containsStr(prev.DaemonsetSkipped, f) {
					prev.DaemonsetSkipped = append(prev.DaemonsetSkipped, f)
				}
			}
			continue
		}
		if strings.Contains(line, "error when evicting pods/") {
			if i := strings.Index(line, `error when evicting pods/`); i >= 0 {
				rest := line[i+len(`error when evicting pods/`):]
				rest = strings.TrimPrefix(rest, `"`)
				if j := strings.IndexByte(rest, '"'); j > 0 {
					podName := rest[:j]
					rest2 := rest[j+1:]
					ns := ""
					if k := strings.Index(rest2, `-n "`); k >= 0 {
						rest2 = rest2[k+len(`-n "`):]
						if e := strings.IndexByte(rest2, '"'); e >= 0 {
							ns = rest2[:e]
						}
					}
					key := ns + "/" + podName
					if !blockedSeen[key] {
						blockedSeen[key] = true
						prev.Blocked = append(prev.Blocked, key)
					}
				}
			}
		}
	}
	prev.PodCount = len(prev.Pods)
	return prev
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
