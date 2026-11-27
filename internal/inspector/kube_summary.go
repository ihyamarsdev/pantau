package inspector

// Single-call Kubernetes aggregation, shared by the in-process AI agent and the local MCP server. Each summary runs
// one read-only kubectl command, parses the JSON in Go and returns counts plus a short list. Raw manifests never
// reach the model.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"pantau/internal/store"
)

const (
	kubeSummaryTimeout      = 15 * time.Second
	kubeSummaryTTL          = 25 * time.Second
	kubeSummaryMaxUnhealthy = 25
	kubeSummaryDefaultLimit = 50
	kubeSummaryMaxLimit     = 200
)

// errKubeSummaryAborted is what waiting callers see when the loader panics instead of returning.
var errKubeSummaryAborted = errors.New("kubernetes summary was aborted")

// kubeObject is the part of a Kubernetes object the aggregators read. Kinds use different fields, so one struct
// covers every list; absent fields keep their zero value.
type kubeObject struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int   `json:"replicas"`
		Suspend  bool   `json:"suspend"`
		Type     string `json:"type"`
	} `json:"spec"`
	Status struct {
		Phase                  string `json:"phase"`
		ReadyReplicas          int    `json:"readyReplicas"`
		DesiredNumberScheduled int    `json:"desiredNumberScheduled"`
		NumberReady            int    `json:"numberReady"`
		Conditions             []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		ContainerStatuses []struct {
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
			State        struct {
				Waiting *struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

type kubeList struct {
	Items []kubeObject `json:"items"`
}

// hasCondition reports whether the object has a condition of this type with this status.
func (o *kubeObject) hasCondition(typ, status string) bool {
	for _, c := range o.Status.Conditions {
		if c.Type == typ && c.Status == status {
			return true
		}
	}
	return false
}

// workloadCounts returns the ready and desired replica counts of a Deployment, StatefulSet or DaemonSet.
func (o *kubeObject) workloadCounts() (ready, desired int) {
	switch o.Kind {
	case "DaemonSet":
		return o.Status.NumberReady, o.Status.DesiredNumberScheduled
	case "Deployment", "StatefulSet":
		desired = 1 // kubectl's default when spec.replicas is unset
		if o.Spec.Replicas != nil {
			desired = *o.Spec.Replicas
		}
		return o.Status.ReadyReplicas, desired
	}
	return 0, 0
}

// kubeObjectReason returns why an object needs attention, or "" when it is healthy or has no health notion.
func kubeObjectReason(o *kubeObject) string {
	switch o.Kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		if ready, desired := o.workloadCounts(); ready < desired {
			return "NotReady"
		}
	case "Job":
		if o.hasCondition("Failed", "True") {
			return "JobFailed"
		}
	case "Pod":
		switch o.Status.Phase {
		case "Failed", "Unknown", "Pending":
			return o.Status.Phase
		}
		for _, cs := range o.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				return cs.State.Waiting.Reason
			}
		}
	case "Node":
		if !o.hasCondition("Ready", "True") {
			return "NotReady"
		}
	case "PersistentVolumeClaim":
		if o.Status.Phase == "Pending" || o.Status.Phase == "Lost" {
			return o.Status.Phase
		}
	}
	return ""
}

// kubeWorkloadRow is one workload in the compact snapshot that is cached and summarized.
type kubeWorkloadRow struct {
	kind, namespace, name string
	ready, desired        int
	reason                string
}

// kubeWorkloadSnapshot is the parsed result of one workloads query. It is what the cache keeps.
type kubeWorkloadSnapshot struct {
	rows          []kubeWorkloadRow
	hasPods       bool
	podsTotal     int
	podsUnhealthy int
}

// kubeShortKind maps the kubectl kind of a JSON object to the short name used in summaries and tool output.
var kubeShortKind = map[string]string{
	"Deployment": "deployment", "StatefulSet": "statefulset", "DaemonSet": "daemonset", "CronJob": "cronjob",
	"Job": "job", "Pod": "pod", "Node": "node", "Namespace": "namespace", "Service": "service",
	"Ingress": "ingress", "ConfigMap": "configmap", "PersistentVolumeClaim": "pvc", "PersistentVolume": "pv",
}

// parseWorkloadSnapshot turns one `kubectl get deployments,...,jobs[,pods] -o json` list into rows. Other kinds in
// the list are ignored.
func parseWorkloadSnapshot(out string, includePods bool) (*kubeWorkloadSnapshot, error) {
	var list kubeList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("parse workloads: %w", err)
	}
	snap := &kubeWorkloadSnapshot{hasPods: includePods}
	for i := range list.Items {
		o := &list.Items[i]
		switch o.Kind {
		case "Pod":
			snap.podsTotal++
			if kubeObjectReason(o) != "" {
				snap.podsUnhealthy++
			}
		case "Deployment", "StatefulSet", "DaemonSet", "CronJob", "Job":
			ready, desired := o.workloadCounts()
			snap.rows = append(snap.rows, kubeWorkloadRow{
				kind: kubeShortKind[o.Kind], namespace: o.Metadata.Namespace, name: o.Metadata.Name,
				ready: ready, desired: desired, reason: kubeObjectReason(o),
			})
		}
	}
	return snap, nil
}

// SummaryCluster names the target a summary was taken from.
type SummaryCluster struct {
	Name   string `json:"name"`
	Type   string `json:"type"` // teleport | kubeconfig | ssh
	Cached bool   `json:"cached"`
}

// KubeWorkloadOpts selects the scope and the optional parts of SummarizeWorkloads.
type KubeWorkloadOpts struct {
	Namespace    string // "" = all namespaces
	IncludePods  bool
	IncludeNames bool
	Limit        int // items returned when IncludeNames is set; default 50, max 200
	Refresh      bool
}

// KubeWorkloadTotals counts workloads. Pod fields are set only when pods were requested.
type KubeWorkloadTotals struct {
	Deployments        int  `json:"deployments"`
	StatefulSets       int  `json:"statefulsets"`
	DaemonSets         int  `json:"daemonsets"`
	CronJobs           int  `json:"cronjobs"`
	Jobs               int  `json:"jobs"`
	TotalWorkloads     int  `json:"total_workloads"`
	ReadyWorkloads     int  `json:"ready_workloads"`
	UnhealthyWorkloads int  `json:"unhealthy_workloads"`
	PodsTotal          *int `json:"pods_total,omitempty"`
	PodsUnhealthy      *int `json:"pods_unhealthy,omitempty"`
}

// KubeNamespaceWorkloads is one row of the per-namespace breakdown.
type KubeNamespaceWorkloads struct {
	Namespace    string `json:"namespace"`
	Deployments  int    `json:"deployments"`
	StatefulSets int    `json:"statefulsets"`
	DaemonSets   int    `json:"daemonsets"`
	CronJobs     int    `json:"cronjobs"`
	Jobs         int    `json:"jobs"`
	Total        int    `json:"total"`
	Unhealthy    int    `json:"unhealthy"`
}

// KubeObjectRef names one object and its health. Reason is empty for a healthy object.
type KubeObjectRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     int    `json:"ready"`
	Desired   int    `json:"desired"`
	Reason    string `json:"reason,omitempty"`
}

// KubeWorkloadSummary is the answer of SummarizeWorkloads.
type KubeWorkloadSummary struct {
	Cluster        SummaryCluster           `json:"cluster"`
	Totals         KubeWorkloadTotals       `json:"totals"`
	ByNamespace    []KubeNamespaceWorkloads `json:"by_namespace"`
	Unhealthy      []KubeObjectRef          `json:"unhealthy"`
	UnhealthyTotal int                      `json:"unhealthy_total"`
	Items          []KubeObjectRef          `json:"items,omitempty"`
	ItemsTruncated bool                     `json:"items_truncated,omitempty"`
}

// SummarizeWorkloads counts the Deployments, StatefulSets, DaemonSets, CronJobs and Jobs of a target with one
// kubectl call. Results are cached for kubeSummaryTTL per (target, namespace, pods); Refresh bypasses the cache.
func (ins *Inspector) SummarizeWorkloads(ctx context.Context, targetID int64, opts KubeWorkloadOpts) (*KubeWorkloadSummary, error) {
	if opts.Namespace != "" && !ValidKubeNamespace(opts.Namespace) {
		return nil, kubeInvalid("invalid namespace: %s", opts.Namespace)
	}
	meta, err := ins.summaryClusterMeta(targetID)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("workloads|%d|%s|%t", targetID, opts.Namespace, opts.IncludePods)
	v, cached, err := ins.summaries.get(ctx, key, opts.Refresh, func() (any, error) {
		return ins.fetchWorkloadSnapshot(targetID, opts.Namespace, opts.IncludePods)
	})
	if err != nil {
		return nil, err
	}
	meta.Cached = cached
	return buildWorkloadSummary(v.(*kubeWorkloadSnapshot), meta, opts), nil
}

func (ins *Inspector) fetchWorkloadSnapshot(targetID int64, ns string, includePods bool) (*kubeWorkloadSnapshot, error) {
	kinds := "deployments,statefulsets,daemonsets,cronjobs,jobs"
	if includePods {
		kinds += ",pods"
	}
	scope := "-A"
	if ns != "" {
		scope = "-n " + shellQuote(ns)
	}
	out, err := ins.kubeQuery(targetID, fmt.Sprintf("kc get %s %s -o json", kinds, scope), "kube workload summary", kubeSummaryTimeout)
	if err != nil {
		return nil, err
	}
	if len(out) > maxKubePodsPayload {
		return nil, fmt.Errorf("kube workload summary: output exceeds %d bytes", maxKubePodsPayload)
	}
	return parseWorkloadSnapshot(out, includePods)
}

func buildWorkloadSummary(snap *kubeWorkloadSnapshot, meta SummaryCluster, opts KubeWorkloadOpts) *KubeWorkloadSummary {
	sum := &KubeWorkloadSummary{Cluster: meta, Unhealthy: []KubeObjectRef{}, ByNamespace: []KubeNamespaceWorkloads{}}
	perNS := map[string]*KubeNamespaceWorkloads{}
	var unhealthy []KubeObjectRef
	for _, r := range snap.rows {
		t := &sum.Totals
		switch r.kind {
		case "deployment":
			t.Deployments++
		case "statefulset":
			t.StatefulSets++
		case "daemonset":
			t.DaemonSets++
		case "cronjob":
			t.CronJobs++
		case "job":
			t.Jobs++
		}
		t.TotalWorkloads++
		ns := perNS[r.namespace]
		if ns == nil {
			ns = &KubeNamespaceWorkloads{Namespace: r.namespace}
			perNS[r.namespace] = ns
		}
		ns.Total++
		switch r.kind {
		case "deployment":
			ns.Deployments++
		case "statefulset":
			ns.StatefulSets++
		case "daemonset":
			ns.DaemonSets++
		case "cronjob":
			ns.CronJobs++
		case "job":
			ns.Jobs++
		}
		ref := KubeObjectRef{Kind: r.kind, Namespace: r.namespace, Name: r.name, Ready: r.ready, Desired: r.desired, Reason: r.reason}
		if r.reason == "" {
			t.ReadyWorkloads++
		} else {
			t.UnhealthyWorkloads++
			ns.Unhealthy++
			unhealthy = append(unhealthy, ref)
		}
		if opts.IncludeNames {
			sum.Items = append(sum.Items, ref)
		}
	}
	if snap.hasPods {
		total, bad := snap.podsTotal, snap.podsUnhealthy
		sum.Totals.PodsTotal, sum.Totals.PodsUnhealthy = &total, &bad
	}
	for _, ns := range perNS {
		sum.ByNamespace = append(sum.ByNamespace, *ns)
	}
	sort.Slice(sum.ByNamespace, func(i, j int) bool { return sum.ByNamespace[i].Namespace < sum.ByNamespace[j].Namespace })
	sortRefs(unhealthy)
	sum.UnhealthyTotal = len(unhealthy)
	if len(unhealthy) > kubeSummaryMaxUnhealthy {
		unhealthy = unhealthy[:kubeSummaryMaxUnhealthy]
	}
	sum.Unhealthy = append(sum.Unhealthy, unhealthy...)
	if opts.IncludeNames {
		sortRefs(sum.Items)
		limit := clampLimit(opts.Limit)
		if len(sum.Items) > limit {
			sum.Items = sum.Items[:limit]
			sum.ItemsTruncated = true
		}
	}
	return sum
}

// sortRefs orders objects by namespace, kind and name, so repeated calls return the same list.
func sortRefs(refs []KubeObjectRef) {
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
}

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return kubeSummaryDefaultLimit
	case n > kubeSummaryMaxLimit:
		return kubeSummaryMaxLimit
	}
	return n
}

// KubeClusterInventory is the answer of SummarizeClusterInventory.
type KubeClusterInventory struct {
	Cluster        SummaryCluster     `json:"cluster"`
	Nodes          KubeNodeCounts     `json:"nodes"`
	Namespaces     int                `json:"namespaces"`
	Workloads      KubeWorkloadTotals `json:"workloads"`
	Services       int                `json:"services"`
	Ingresses      int                `json:"ingresses"`
	PVCs           int                `json:"pvcs"`
	Anomalies      []KubeObjectRef    `json:"anomalies"`
	AnomaliesTotal int                `json:"anomalies_total"`
}

// KubeNodeCounts splits the nodes by their Ready condition.
type KubeNodeCounts struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	NotReady int `json:"not_ready"`
}

type kubeInventorySnapshot struct {
	nodes      KubeNodeCounts
	namespaces int
	workloads  kubeWorkloadSnapshot
	services   int
	ingresses  int
	pvcs       int
	anomalies  []KubeObjectRef
}

// SummarizeClusterInventory reports nodes, namespaces, workloads, services, ingresses, PVCs and the main problems of
// a target. It makes one kubectl call, capped at kubeSummaryTimeout, and shares the cache of SummarizeWorkloads.
func (ins *Inspector) SummarizeClusterInventory(ctx context.Context, targetID int64, refresh bool) (*KubeClusterInventory, error) {
	meta, err := ins.summaryClusterMeta(targetID)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("inventory|%d", targetID)
	v, cached, err := ins.summaries.get(ctx, key, refresh, func() (any, error) {
		return ins.fetchInventorySnapshot(targetID)
	})
	if err != nil {
		return nil, err
	}
	meta.Cached = cached
	snap := v.(*kubeInventorySnapshot)
	inv := &KubeClusterInventory{
		Cluster: meta, Nodes: snap.nodes, Namespaces: snap.namespaces,
		Services: snap.services, Ingresses: snap.ingresses, PVCs: snap.pvcs,
		Anomalies: []KubeObjectRef{},
	}
	inv.Workloads = buildWorkloadSummary(&snap.workloads, SummaryCluster{}, KubeWorkloadOpts{}).Totals
	inv.AnomaliesTotal = len(snap.anomalies)
	inv.Anomalies = append(inv.Anomalies, snap.anomalies[:min(len(snap.anomalies), kubeSummaryMaxUnhealthy)]...)
	return inv, nil
}

func (ins *Inspector) fetchInventorySnapshot(targetID int64) (*kubeInventorySnapshot, error) {
	kinds := "nodes,namespaces,services,ingresses,pvcs,deployments,statefulsets,daemonsets,cronjobs,jobs"
	out, err := ins.kubeQuery(targetID, "kc get "+kinds+" -A -o json", "kube cluster summary", kubeSummaryTimeout)
	if err != nil {
		return nil, err
	}
	if len(out) > maxKubePodsPayload {
		return nil, fmt.Errorf("kube cluster summary: output exceeds %d bytes", maxKubePodsPayload)
	}
	var list kubeList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("parse cluster inventory: %w", err)
	}
	snap := &kubeInventorySnapshot{}
	for i := range list.Items {
		o := &list.Items[i]
		switch o.Kind {
		case "Node":
			snap.nodes.Total++
			if kubeObjectReason(o) == "" {
				snap.nodes.Ready++
			} else {
				snap.nodes.NotReady++
			}
		case "Namespace":
			snap.namespaces++
		case "Service":
			snap.services++
		case "Ingress":
			snap.ingresses++
		case "PersistentVolumeClaim":
			snap.pvcs++
		}
		// Workload anomalies come from the parsed rows below; only nodes and claims are judged here.
		if r := kubeObjectReason(o); r != "" && (o.Kind == "Node" || o.Kind == "PersistentVolumeClaim") {
			snap.anomalies = append(snap.anomalies, KubeObjectRef{Kind: kubeShortKind[o.Kind], Namespace: o.Metadata.Namespace, Name: o.Metadata.Name, Reason: r})
		}
	}
	workloads, err := parseWorkloadSnapshot(out, false)
	if err != nil {
		return nil, err
	}
	snap.workloads = *workloads
	for _, r := range workloads.rows {
		if r.reason != "" {
			snap.anomalies = append(snap.anomalies, KubeObjectRef{Kind: r.kind, Namespace: r.namespace, Name: r.name, Ready: r.ready, Desired: r.desired, Reason: r.reason})
		}
	}
	sortRefs(snap.anomalies)
	return snap, nil
}

// summaryClusterMeta resolves the display name and the access type of a target. Both come from the store.
func (ins *Inspector) summaryClusterMeta(targetID int64) (SummaryCluster, error) {
	if IsKubeClusterTarget(targetID) {
		c, err := ins.getClusterTarget(targetID)
		if err != nil {
			return SummaryCluster{}, err
		}
		typ := "kubeconfig"
		if c.SourceType == store.KubeSourceTeleport {
			typ = "teleport"
		}
		return SummaryCluster{Name: c.Name, Type: typ}, nil
	}
	host, err := ins.db.GetHost(targetID)
	if err != nil || host == nil {
		return SummaryCluster{}, fmt.Errorf("host not found")
	}
	return SummaryCluster{Name: host.Name, Type: "ssh"}, nil
}

// kubeSummaryCache keeps recent snapshots for kubeSummaryTTL and joins concurrent loads of the same key
// (singleflight). Its zero value is ready to use. Errors are never cached.
// shortcut: entries are never evicted; the key count is bounded by targets x namespaces, so add an LRU if that grows.
type kubeSummaryCache struct {
	mu    sync.Mutex
	now   func() time.Time // nil = time.Now; tests set it
	ready map[string]kubeCacheEntry
	calls map[string]*kubeCall
}

type kubeCacheEntry struct {
	at  time.Time
	val any
}

type kubeCall struct {
	done chan struct{}
	val  any
	err  error
}

func (c *kubeSummaryCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// get returns the snapshot for key. It runs load at most once for concurrent callers, and reuses a result younger
// than kubeSummaryTTL unless refresh is set. cached is true when this call ran no load of its own.
func (c *kubeSummaryCache) get(ctx context.Context, key string, refresh bool, load func() (any, error)) (val any, cached bool, err error) {
	c.mu.Lock()
	if c.ready == nil {
		c.ready = map[string]kubeCacheEntry{}
		c.calls = map[string]*kubeCall{}
	}
	if e, ok := c.ready[key]; ok && !refresh && c.clock().Sub(e.at) < kubeSummaryTTL {
		c.mu.Unlock()
		return e.val, true, nil
	}
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.val, true, call.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	call := &kubeCall{done: make(chan struct{}), err: errKubeSummaryAborted}
	c.calls[key] = call
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.calls, key)
		if call.err == nil {
			c.ready[key] = kubeCacheEntry{at: c.clock(), val: call.val}
		}
		c.mu.Unlock()
		close(call.done)
	}()
	call.val, call.err = load()
	return call.val, false, call.err
}
