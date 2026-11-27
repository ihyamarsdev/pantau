package inspector

// Multi-kind listing behind kube_get. One kubectl call covers every requested kind and namespace, and the rows come
// back compact (name, namespace, status, ready, age, restarts), never as raw JSON.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// kubeListKinds maps every accepted kind name (plural or singular) to its kubectl resource. Secrets are not listed.
var kubeListKinds = map[string]string{
	"deployments": "deployments", "deployment": "deployments",
	"statefulsets": "statefulsets", "statefulset": "statefulsets",
	"daemonsets": "daemonsets", "daemonset": "daemonsets",
	"cronjobs": "cronjobs", "cronjob": "cronjobs",
	"jobs": "jobs", "job": "jobs",
	"pods": "pods", "pod": "pods",
	"services": "services", "service": "services", "svc": "services",
	"ingresses": "ingresses", "ingress": "ingresses",
	"configmaps": "configmaps", "configmap": "configmaps",
	"pvcs": "pvcs", "pvc": "pvcs",
	"pvs": "pvs", "pv": "pvs",
	"nodes": "nodes", "node": "nodes",
	"namespaces": "namespaces", "namespace": "namespaces",
}

// kubeSelectorRe admits label and field selector syntax (k=v, !k, k in (a,b), a.b/c) and nothing that could be a flag.
var kubeSelectorRe = regexp.MustCompile(`^[A-Za-z0-9_./=!,:()\- ]{1,256}$`)

// KubeListOpts scopes a KubeList call.
type KubeListOpts struct {
	Namespace     string // "", "*" or "all" = every namespace
	LabelSelector string
	FieldSelector string
	SummaryOnly   bool // counts and names only; names are kept only when the total fits in Limit
	UnhealthyOnly bool
	Limit         int // rows returned; default 50, max 200
}

// KubeListRow is one object in compact form.
type KubeListRow struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
	Ready     string `json:"ready,omitempty"`
	Age       string `json:"age,omitempty"`
	Restarts  int    `json:"restarts,omitempty"`
}

// KubeListResult is the answer of KubeList. TotalCount is the full number of matches, whatever the limit.
type KubeListResult struct {
	TotalCount  int            `json:"total_count"`
	Truncated   bool           `json:"truncated"`
	ByKind      map[string]int `json:"by_kind"`
	ByNamespace map[string]int `json:"by_namespace,omitempty"`
	Items       []KubeListRow  `json:"items"`
}

// KubeList lists objects of the given kinds on one target with a single kubectl call.
func (ins *Inspector) KubeList(targetID int64, kinds []string, opts KubeListOpts) (*KubeListResult, error) {
	resources, err := kubeListResources(kinds)
	if err != nil {
		return nil, err
	}
	scope := "-A"
	switch ns := strings.TrimSpace(opts.Namespace); ns {
	case "", "*", "all":
	default:
		if !ValidKubeNamespace(ns) {
			return nil, kubeInvalid("invalid namespace: %s", ns)
		}
		scope = "-n " + shellQuote(ns)
	}
	cmd := fmt.Sprintf("kc get %s %s", strings.Join(resources, ","), scope)
	if opts.LabelSelector != "" {
		if !kubeSelectorRe.MatchString(opts.LabelSelector) || strings.HasPrefix(opts.LabelSelector, "-") {
			return nil, kubeInvalid("invalid label_selector")
		}
		cmd += " --selector=" + shellQuote(opts.LabelSelector)
	}
	if opts.FieldSelector != "" {
		if !kubeSelectorRe.MatchString(opts.FieldSelector) || strings.HasPrefix(opts.FieldSelector, "-") {
			return nil, kubeInvalid("invalid field_selector")
		}
		cmd += " --field-selector=" + shellQuote(opts.FieldSelector)
	}
	out, err := ins.kubeQuery(targetID, cmd+" -o json", "kube list", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	if len(out) > maxKubePodsPayload {
		return nil, fmt.Errorf("kube list: output exceeds %d bytes", maxKubePodsPayload)
	}
	return buildKubeList(out, opts, time.Now())
}

// kubeListResources resolves the requested kind names to kubectl resources. Unknown names, including anything that
// starts with "-", are rejected before a command is built.
func kubeListResources(kinds []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, k := range kinds {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		res, ok := kubeListKinds[k]
		if !ok {
			return nil, kubeInvalid("unsupported kind: %s", k)
		}
		if !seen[res] {
			seen[res] = true
			out = append(out, res)
		}
	}
	if len(out) == 0 {
		return nil, kubeInvalid("at least one kind is required")
	}
	sort.Strings(out)
	return out, nil
}

// buildKubeList turns the raw list into counts and rows. It is separate from KubeList so tests can pass a clock.
func buildKubeList(out string, opts KubeListOpts, now time.Time) (*KubeListResult, error) {
	var list kubeList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("parse list: %w", err)
	}
	type matched struct {
		row    KubeListRow
		reason string
	}
	var all []matched
	for i := range list.Items {
		o := &list.Items[i]
		reason := kubeObjectReason(o)
		if opts.UnhealthyOnly && reason == "" {
			continue
		}
		all = append(all, matched{row: kubeListRowOf(o, reason, now), reason: reason})
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i].row, all[j].row
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})

	res := &KubeListResult{TotalCount: len(all), ByKind: map[string]int{}, ByNamespace: map[string]int{}, Items: []KubeListRow{}}
	for _, m := range all {
		res.ByKind[m.row.Kind]++
		if m.row.Namespace != "" {
			res.ByNamespace[m.row.Namespace]++
		}
	}
	if len(res.ByNamespace) == 0 {
		res.ByNamespace = nil
	}
	limit := clampLimit(opts.Limit)
	switch {
	case opts.SummaryOnly:
		if len(all) <= limit {
			for _, m := range all {
				res.Items = append(res.Items, KubeListRow{Kind: m.row.Kind, Namespace: m.row.Namespace, Name: m.row.Name})
			}
		}
		res.Truncated = len(all) > limit
	default:
		for i, m := range all {
			if i >= limit {
				res.Truncated = true
				break
			}
			res.Items = append(res.Items, m.row)
		}
	}
	return res, nil
}

// kubeListRowOf builds the compact row of one object.
func kubeListRowOf(o *kubeObject, reason string, now time.Time) KubeListRow {
	row := KubeListRow{
		Kind:      kubeShortKind[o.Kind],
		Namespace: o.Metadata.Namespace,
		Name:      o.Metadata.Name,
		Status:    kubeRowStatus(o, reason),
		Age:       kubeAge(o.Metadata.CreationTimestamp, now),
	}
	switch o.Kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		ready, desired := o.workloadCounts()
		row.Ready = fmt.Sprintf("%d/%d", ready, desired)
	case "Pod":
		ready := 0
		for _, cs := range o.Status.ContainerStatuses {
			row.Restarts += cs.RestartCount
			if cs.Ready {
				ready++
			}
		}
		row.Ready = fmt.Sprintf("%d/%d", ready, len(o.Status.ContainerStatuses))
	}
	return row
}

func kubeRowStatus(o *kubeObject, reason string) string {
	if reason != "" {
		return reason
	}
	switch o.Kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Node":
		return "Ready"
	case "CronJob":
		if o.Spec.Suspend {
			return "Suspended"
		}
		return "Active"
	case "Job":
		if o.hasCondition("Complete", "True") {
			return "Complete"
		}
		return "Running"
	}
	if o.Status.Phase != "" {
		return o.Status.Phase
	}
	return o.Spec.Type
}

// kubeAge renders the time since an RFC 3339 timestamp in the coarsest useful unit, such as 5d, 3h, 12m or 40s.
func kubeAge(created string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d >= 0:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	return ""
}
