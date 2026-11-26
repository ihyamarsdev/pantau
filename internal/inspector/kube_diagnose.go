package inspector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// KubePodLastState captures the previous terminated state of a container, highlighting OOMKilled and exit codes.
type KubePodLastState struct {
	Reason     string `json:"reason,omitempty"`
	ExitCode   int    `json:"exit_code"`
	OOMKilled  bool   `json:"oom_killed"`
	Message    string `json:"message,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// KubePodContainerDiag summarizes one container (or initContainer) in a Pod diagnosis.
type KubePodContainerDiag struct {
	Name         string            `json:"name"`
	Init         bool              `json:"init,omitempty"`
	Image        string            `json:"image"`
	Ready        bool              `json:"ready"`
	RestartCount int               `json:"restart_count"`
	State        string            `json:"state"`
	StateReason  string            `json:"state_reason,omitempty"`
	StateMessage string            `json:"state_message,omitempty"`
	ExitCode     *int              `json:"exit_code,omitempty"`
	LastState    *KubePodLastState `json:"last_state,omitempty"`
	LogSource    string            `json:"log_source,omitempty"` // "previous" | "current"
	LogLines     []string          `json:"log_lines,omitempty"`
}

// KubePodConditionDiag summarizes a Pod condition.
type KubePodConditionDiag struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// KubePodDiagnosis is the compact, LLM-ready diagnostic summary of a single Pod.
type KubePodDiagnosis struct {
	Namespace     string                 `json:"namespace"`
	Name          string                 `json:"name"`
	Phase         string                 `json:"phase"`
	Reason        string                 `json:"reason,omitempty"`
	Message       string                 `json:"message,omitempty"`
	Node          string                 `json:"node,omitempty"`
	PodIP         string                 `json:"pod_ip,omitempty"`
	QoSClass      string                 `json:"qos_class,omitempty"`
	Terminating   bool                   `json:"terminating,omitempty"`
	Created       string                 `json:"created,omitempty"`
	Conditions    []KubePodConditionDiag `json:"conditions,omitempty"`
	Containers    []KubePodContainerDiag `json:"containers"`
	WarningEvents []KubeEvent            `json:"warning_events,omitempty"`
}

type rawSinglePod struct {
	Metadata struct {
		Name              string  `json:"name"`
		Namespace         string  `json:"namespace"`
		CreationTimestamp string  `json:"creationTimestamp"`
		DeletionTimestamp *string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
		InitContainers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"initContainers"`
	} `json:"spec"`
	Status struct {
		Phase      string `json:"phase"`
		Reason     string `json:"reason"`
		Message    string `json:"message"`
		PodIP      string `json:"podIP"`
		QoSClass   string `json:"qosClass"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
		ContainerStatuses     []rawDiagContainerStatus `json:"containerStatuses"`
		InitContainerStatuses []rawDiagContainerStatus `json:"initContainerStatuses"`
	} `json:"status"`
}

type rawDiagContainerStatus struct {
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	Ready        bool              `json:"ready"`
	RestartCount int               `json:"restartCount"`
	State        rawContainerState `json:"state"`
	LastState    rawContainerState `json:"lastState"`
}

type rawContainerState struct {
	Running *struct {
		StartedAt string `json:"startedAt"`
	} `json:"running"`
	Waiting *struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"waiting"`
	Terminated *struct {
		Reason     string `json:"reason"`
		Message    string `json:"message"`
		ExitCode   int    `json:"exitCode"`
		StartedAt  string `json:"startedAt"`
		FinishedAt string `json:"finishedAt"`
	} `json:"terminated"`
}

func convertContainerDiag(cs rawDiagContainerStatus, fallbackImage string, isInit bool) KubePodContainerDiag {
	img := cs.Image
	if img == "" {
		img = fallbackImage
	}
	d := KubePodContainerDiag{
		Name:         cs.Name,
		Init:         isInit,
		Image:        img,
		Ready:        cs.Ready,
		RestartCount: cs.RestartCount,
		State:        "unknown",
	}
	switch {
	case cs.State.Running != nil:
		d.State = "running"
	case cs.State.Waiting != nil:
		d.State = "waiting"
		d.StateReason = cs.State.Waiting.Reason
		d.StateMessage = cs.State.Waiting.Message
	case cs.State.Terminated != nil:
		d.State = "terminated"
		d.StateReason = cs.State.Terminated.Reason
		d.StateMessage = cs.State.Terminated.Message
		ec := cs.State.Terminated.ExitCode
		d.ExitCode = &ec
	}
	if cs.LastState.Terminated != nil {
		t := cs.LastState.Terminated
		d.LastState = &KubePodLastState{
			Reason:     t.Reason,
			ExitCode:   t.ExitCode,
			OOMKilled:  strings.EqualFold(t.Reason, "OOMKilled") || t.ExitCode == 137,
			Message:    t.Message,
			StartedAt:  t.StartedAt,
			FinishedAt: t.FinishedAt,
		}
	}
	return d
}

// parseKubePodDiagnosis parses `kubectl get pod -o json` and `kubectl get events -o json` into a compact summary.
func parseKubePodDiagnosis(podJSON, eventsJSON string) (*KubePodDiagnosis, error) {
	var pod rawSinglePod
	if err := json.Unmarshal([]byte(podJSON), &pod); err != nil {
		return nil, fmt.Errorf("parse pod json: %w", err)
	}
	if pod.Metadata.Name == "" {
		return nil, fmt.Errorf("pod not found in kubectl output")
	}

	diag := &KubePodDiagnosis{
		Namespace:   pod.Metadata.Namespace,
		Name:        pod.Metadata.Name,
		Phase:       pod.Status.Phase,
		Reason:      pod.Status.Reason,
		Message:     pod.Status.Message,
		Node:        pod.Spec.NodeName,
		PodIP:       pod.Status.PodIP,
		QoSClass:    pod.Status.QoSClass,
		Terminating: pod.Metadata.DeletionTimestamp != nil,
		Created:     pod.Metadata.CreationTimestamp,
		Containers:  []KubePodContainerDiag{},
	}
	if diag.Phase == "" {
		diag.Phase = "Unknown"
	}

	for _, c := range pod.Status.Conditions {
		if c.Status != "True" || c.Reason != "" || c.Message != "" {
			diag.Conditions = append(diag.Conditions, KubePodConditionDiag{
				Type:    c.Type,
				Status:  c.Status,
				Reason:  c.Reason,
				Message: c.Message,
			})
		}
	}

	specImages := make(map[string]string, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	for _, c := range pod.Spec.Containers {
		specImages[c.Name] = c.Image
	}
	for _, c := range pod.Spec.InitContainers {
		specImages[c.Name] = c.Image
	}

	for _, cs := range pod.Status.InitContainerStatuses {
		diag.Containers = append(diag.Containers, convertContainerDiag(cs, specImages[cs.Name], true))
	}
	for _, cs := range pod.Status.ContainerStatuses {
		diag.Containers = append(diag.Containers, convertContainerDiag(cs, specImages[cs.Name], false))
	}
	if len(diag.Containers) == 0 {
		for _, c := range pod.Spec.Containers {
			diag.Containers = append(diag.Containers, KubePodContainerDiag{
				Name:  c.Name,
				Image: c.Image,
				State: "pending",
			})
		}
	}

	if strings.TrimSpace(eventsJSON) != "" {
		if allEvents, err := parseKubeEvents(eventsJSON); err == nil {
			wantObj := "Pod/" + pod.Metadata.Name
			for _, ev := range allEvents {
				if !strings.EqualFold(ev.Type, "Warning") {
					continue
				}
				if ev.Object != "" && ev.Object != wantObj && ev.Object != pod.Metadata.Name {
					continue
				}
				diag.WarningEvents = append(diag.WarningEvents, ev)
				if len(diag.WarningEvents) >= 20 {
					break
				}
			}
			sort.SliceStable(diag.WarningEvents, func(i, j int) bool {
				ti, _ := time.Parse(time.RFC3339, diag.WarningEvents[i].Last)
				tj, _ := time.Parse(time.RFC3339, diag.WarningEvents[j].Last)
				return ti.After(tj)
			})
		}
	}

	return diag, nil
}

func buildKubePodLogCmd(ns, pod, container string, previous bool, tail int) string {
	if tail <= 0 {
		tail = 40
	}
	prevFlag := ""
	if previous {
		prevFlag = " --previous"
	}
	if container != "" {
		return fmt.Sprintf("kc logs -n %s %s -c %s%s --tail=%d", shellQuote(ns), shellQuote(pod), shellQuote(container), prevFlag, tail)
	}
	return fmt.Sprintf("kc logs -n %s %s%s --tail=%d", shellQuote(ns), shellQuote(pod), prevFlag, tail)
}

func splitLogLines(raw string, limit int) []string {
	raw = strings.TrimRight(raw, "\r\n")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

// KubeDiagnosePod collects pod status, container lastState/exitCode/OOMKilled, filtered warning events,
// and 40 relevant log lines (--previous when restart_count > 0) in a compact JSON-ready struct.
func (ins *Inspector) KubeDiagnosePod(hostID int64, ns, pod string) (*KubePodDiagnosis, error) {
	ns = strings.TrimSpace(ns)
	pod = strings.TrimSpace(pod)
	if !ValidKubeNamespace(ns) {
		return nil, kubeInvalid("invalid namespace: %s", ns)
	}
	if !ValidKubeName(pod) {
		return nil, kubeInvalid("invalid pod name: %s", pod)
	}

	script := fmt.Sprintf(`echo "===POD==="
kc get pod %s -n %s -o json
echo "===EVENTS==="
kc get events -n %s --field-selector %s -o json 2>/dev/null || true
`, shellQuote(pod), shellQuote(ns), shellQuote(ns), shellQuote("involvedObject.name="+pod))

	out, err := ins.kubeQuery(hostID, script, "kube diagnose pod", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	sections := splitDockerSections(out)
	podJSON := strings.TrimSpace(sections["===POD==="])
	if podJSON == "" {
		podJSON = strings.TrimSpace(out)
	}
	eventsJSON := strings.TrimSpace(sections["===EVENTS==="])

	diag, err := parseKubePodDiagnosis(podJSON, eventsJSON)
	if err != nil {
		return nil, err
	}

	const diagLogTail = 40
	// Fetch logs for up to 3 containers, prioritizing restarting or unready containers.
	idxs := make([]int, 0, len(diag.Containers))
	for i, c := range diag.Containers {
		if !c.Ready || c.RestartCount > 0 {
			idxs = append(idxs, i)
		}
	}
	for i, c := range diag.Containers {
		if c.Ready && c.RestartCount == 0 {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) > 3 {
		idxs = idxs[:3]
	}

	for _, i := range idxs {
		c := &diag.Containers[i]
		usePrev := c.RestartCount > 0 || (c.LastState != nil)
		if usePrev {
			cmd := buildKubePodLogCmd(ns, pod, c.Name, true, diagLogTail)
			if logOut, lerr := ins.kubeQuery(hostID, cmd, "kube logs previous", kubeQueryTimeout); lerr == nil && strings.TrimSpace(logOut) != "" {
				c.LogSource = "previous"
				c.LogLines = splitLogLines(logOut, diagLogTail)
				continue
			}
		}
		cmd := buildKubePodLogCmd(ns, pod, c.Name, false, diagLogTail)
		if logOut, lerr := ins.kubeQuery(hostID, cmd, "kube logs", kubeQueryTimeout); lerr == nil && strings.TrimSpace(logOut) != "" {
			c.LogSource = "current"
			c.LogLines = splitLogLines(logOut, diagLogTail)
		}
	}

	return diag, nil
}
