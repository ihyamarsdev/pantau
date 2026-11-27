package inspector

import (
	"encoding/json"
	"fmt"
	"time"
)

// KubeClusterHealth is a compact live view of one cluster: node readiness and the pods that are not healthy.
// It is read-only (kubectl get) and bounded: at most kubeHealthMaxPods unhealthy pods are returned.
type KubeClusterHealth struct {
	Nodes          []KubeNodeHealth   `json:"nodes"`
	PodTotal       int                `json:"pod_total"`
	UnhealthyTotal int                `json:"unhealthy_total"`
	UnhealthyPods  []KubeUnhealthyPod `json:"unhealthy_pods"`
}

// KubeNodeHealth is one node and whether its Ready condition is True.
type KubeNodeHealth struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// KubeUnhealthyPod is a pod whose phase or containers show a failure: CrashLoopBackOff, OOMKilled, Pending, Failed,
// or an image/config pull error.
type KubeUnhealthyPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Node      string `json:"node,omitempty"`
	Phase     string `json:"phase"`
	Reason    string `json:"reason"`
	Restarts  int    `json:"restarts"`
}

const (
	kubeHealthMaxPods = 50
	// kubeHealthTimeout bounds one live cluster query. The MCP caller also applies its own deadline.
	kubeHealthTimeout = 10 * time.Second
)

// kubeWaitingFailures are container waiting reasons that mean the pod is not making progress.
var kubeWaitingFailures = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"RunContainerError":          true,
}

type kubeHealthList struct {
	Items []json.RawMessage `json:"items"`
}

// KubeClusterHealthLive runs `kubectl get nodes` and `kubectl get pods -A` against a cluster target and parses both.
// The 10-second bound is enforced by the kubectl runner, not only by the caller.
func (ins *Inspector) KubeClusterHealthLive(targetID int64) (*KubeClusterHealth, error) {
	script := `echo "===NODES==="
kc get nodes -o json
echo "===PODS==="
kc get pods -A -o json
`
	out, err := ins.kubeQuery(targetID, script, "kube cluster health", kubeHealthTimeout)
	if err != nil {
		return nil, err
	}
	sections := splitDockerSections(out)
	return ParseKubeClusterHealth(sections["===NODES==="], sections["===PODS==="])
}

// ParseKubeClusterHealth turns `kubectl get nodes -o json` and `kubectl get pods -A -o json` output into a summary.
// Pure function: no I/O, so it is covered by fixture and fuzz tests.
func ParseKubeClusterHealth(nodesJSON, podsJSON string) (*KubeClusterHealth, error) {
	if len(podsJSON) > maxKubePodsPayload {
		return nil, fmt.Errorf("pods listing exceeds %d bytes", maxKubePodsPayload)
	}
	out := &KubeClusterHealth{Nodes: []KubeNodeHealth{}, UnhealthyPods: []KubeUnhealthyPod{}}

	var nodes kubeHealthList
	if nodesJSON != "" {
		if err := json.Unmarshal([]byte(nodesJSON), &nodes); err != nil {
			return nil, fmt.Errorf("parse nodes json: %w", err)
		}
	}
	for _, raw := range nodes.Items {
		var n struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
					Reason string `json:"reason"`
				} `json:"conditions"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			continue
		}
		h := KubeNodeHealth{Name: n.Metadata.Name}
		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" {
				h.Ready = c.Status == "True"
				h.Reason = c.Reason
			}
		}
		out.Nodes = append(out.Nodes, h)
	}

	var pods kubeHealthList
	if podsJSON != "" {
		if err := json.Unmarshal([]byte(podsJSON), &pods); err != nil {
			return nil, fmt.Errorf("parse pods json: %w", err)
		}
	}
	out.PodTotal = len(pods.Items)
	for _, raw := range pods.Items {
		p, bad := kubeUnhealthyOf(raw)
		if !bad {
			continue
		}
		out.UnhealthyTotal++
		if len(out.UnhealthyPods) < kubeHealthMaxPods {
			out.UnhealthyPods = append(out.UnhealthyPods, p)
		}
	}
	return out, nil
}

// kubeUnhealthyOf classifies one pod. The first matching reason wins, in priority order: container waiting failure,
// OOMKilled, Pending, then Failed.
func kubeUnhealthyOf(raw json.RawMessage) (KubeUnhealthyPod, bool) {
	var p struct {
		Metadata struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				RestartCount int `json:"restartCount"`
				State        struct {
					Waiting struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
					Terminated struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
				} `json:"state"`
				LastState struct {
					Terminated struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
				} `json:"lastState"`
			} `json:"containerStatuses"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return KubeUnhealthyPod{}, false
	}
	out := KubeUnhealthyPod{
		Namespace: p.Metadata.Namespace,
		Name:      p.Metadata.Name,
		Node:      p.Spec.NodeName,
		Phase:     p.Status.Phase,
	}
	var waiting, oom string
	for _, c := range p.Status.ContainerStatuses {
		out.Restarts += c.RestartCount
		if waiting == "" && kubeWaitingFailures[c.State.Waiting.Reason] {
			waiting = c.State.Waiting.Reason
		}
		if oom == "" && (c.State.Terminated.Reason == "OOMKilled" || c.LastState.Terminated.Reason == "OOMKilled") {
			oom = "OOMKilled"
		}
	}
	switch {
	case waiting != "":
		out.Reason = waiting
	case oom != "":
		out.Reason = oom
	case p.Status.Phase == "Pending":
		out.Reason = "Pending"
	case p.Status.Phase == "Failed":
		out.Reason = "Failed"
	default:
		return KubeUnhealthyPod{}, false
	}
	return out, true
}
