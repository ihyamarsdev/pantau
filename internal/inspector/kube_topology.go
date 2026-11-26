package inspector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// KubeTopologyNode represents one Kubernetes resource card in the topology drawer.
type KubeTopologyNode struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Meta      string `json:"meta,omitempty"`
}

// KubeTopologyColumn groups nodes by stage: ingress, service, workload, pod, pvc.
type KubeTopologyColumn struct {
	Stage string             `json:"stage"`
	Items []KubeTopologyNode `json:"items"`
}

// KubeTopologyEdge connects two topology node IDs.
type KubeTopologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// KubeTopology is the lightweight Ingress -> Service -> Workload -> Pod -> PVC graph
// for a selected Kubernetes resource.
type KubeTopology struct {
	Root    KubeTopologyNode     `json:"root"`
	Columns []KubeTopologyColumn `json:"columns"`
	Edges   []KubeTopologyEdge   `json:"edges"`
}

type rawKubeList struct {
	Items []rawKubeItem `json:"items"`
}

type rawKubeItem struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		Labels          map[string]string `json:"labels"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		// Service
		Type      string            `json:"type"`
		ClusterIP string            `json:"clusterIP"`
		Selector  map[string]string `json:"selector"`
		Ports     []struct {
			Port int `json:"port"`
		} `json:"ports"`
		// Deployment / StatefulSet / ReplicaSet
		Replicas *int `json:"replicas"`
		// Ingress
		Rules []struct {
			Host string `json:"host"`
			HTTP *struct {
				Paths []struct {
					Backend struct {
						Service *struct {
							Name string `json:"name"`
						} `json:"service"`
						ServiceName string `json:"serviceName"`
					} `json:"backend"`
				} `json:"paths"`
			} `json:"http"`
		} `json:"rules"`
		// Pod
		Volumes []struct {
			PersistentVolumeClaim *struct {
				ClaimName string `json:"claimName"`
			} `json:"persistentVolumeClaim"`
		} `json:"volumes"`
		// PVC
		StorageClassName *string `json:"storageClassName"`
		Resources        *struct {
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		ReadyReplicas     int    `json:"readyReplicas"`
		Replicas          int    `json:"replicas"`
		PodIP             string `json:"podIP"`
		ContainerStatuses []struct {
			State struct {
				Waiting *struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated *struct {
					Reason string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

func topoID(kind, ns, name string) string {
	return kind + ":" + ns + "/" + name
}

func labelsMatch(selector, labels map[string]string) bool {
	if len(selector) == 0 || len(labels) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func podTopoStatus(item rawKubeItem) string {
	for _, cs := range item.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" && cs.State.Terminated.Reason != "Completed" {
			return cs.State.Terminated.Reason
		}
	}
	if item.Status.Phase != "" {
		return item.Status.Phase
	}
	return "Unknown"
}

// KubeTopology fetches resources in ns and builds the relation graph around kind/name.
func (ins *Inspector) KubeTopology(hostID int64, kind, ns, name string) (*KubeTopology, error) {
	kind = strings.TrimSpace(kind)
	ns = strings.TrimSpace(ns)
	name = strings.TrimSpace(name)
	if !ValidKubeNamespace(ns) {
		return nil, kubeInvalid("invalid namespace: %s", ns)
	}
	if !ValidKubeName(name) {
		return nil, kubeInvalid("invalid name: %s", name)
	}
	cmd := fmt.Sprintf("kc get ingress,service,deployment,statefulset,replicaset,pod,pvc -n %s -o json", shellQuote(ns))
	out, err := ins.kubeQuery(hostID, cmd, "kube topology", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return buildKubeTopology(kind, ns, name, out)
}

func normalizeTopoKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "ingress", "ingresses":
		return "Ingress"
	case "service", "services", "svc":
		return "Service"
	case "deployment", "deployments", "deploy":
		return "Deployment"
	case "statefulset", "statefulsets", "sts":
		return "StatefulSet"
	case "daemonset", "daemonsets", "ds":
		return "DaemonSet"
	case "pod", "pods":
		return "Pod"
	case "pvc", "persistentvolumeclaim", "persistentvolumeclaims":
		return "PVC"
	default:
		return k
	}
}

func buildKubeTopology(kind, ns, name, rawJSON string) (*KubeTopology, error) {
	var list rawKubeList
	if err := json.Unmarshal([]byte(rawJSON), &list); err != nil {
		return nil, fmt.Errorf("parse topology json: %w", err)
	}

	normKind := normalizeTopoKind(kind)
	var ingresses, services, deployments, statefulsets, replicasets, pods, pvcs []rawKubeItem
	for _, it := range list.Items {
		switch it.Kind {
		case "Ingress":
			ingresses = append(ingresses, it)
		case "Service":
			services = append(services, it)
		case "Deployment":
			deployments = append(deployments, it)
		case "StatefulSet":
			statefulsets = append(statefulsets, it)
		case "ReplicaSet":
			replicasets = append(replicasets, it)
		case "Pod":
			pods = append(pods, it)
		case "PersistentVolumeClaim":
			pvcs = append(pvcs, it)
		}
	}

	rsOwner := map[string]struct{ Kind, Name string }{}
	for _, rs := range replicasets {
		for _, o := range rs.Metadata.OwnerReferences {
			if o.Kind == "Deployment" {
				rsOwner[rs.Metadata.Name] = struct{ Kind, Name string }{Kind: "Deployment", Name: o.Name}
			}
		}
	}

	podOwner := map[string]struct{ Kind, Name string }{}
	for _, p := range pods {
		for _, o := range p.Metadata.OwnerReferences {
			if o.Kind == "ReplicaSet" {
				if dep, ok := rsOwner[o.Name]; ok {
					podOwner[p.Metadata.Name] = dep
				}
			} else if o.Kind == "StatefulSet" || o.Kind == "Deployment" || o.Kind == "DaemonSet" {
				podOwner[p.Metadata.Name] = struct{ Kind, Name string }{Kind: o.Kind, Name: o.Name}
			}
		}
	}

	// Build full namespace adjacency:
	// Ingress -> Service -> Workload -> Pod -> PVC
	ingToSvc := map[string][]string{}
	svcToWl := map[string][]string{}
	wlToPod := map[string][]string{}
	podToPvc := map[string][]string{}

	svcByName := map[string]rawKubeItem{}
	for _, s := range services {
		svcByName[s.Metadata.Name] = s
	}
	pvcByName := map[string]rawKubeItem{}
	for _, v := range pvcs {
		pvcByName[v.Metadata.Name] = v
	}

	for _, ing := range ingresses {
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, p := range rule.HTTP.Paths {
				svcName := p.Backend.ServiceName
				if p.Backend.Service != nil && p.Backend.Service.Name != "" {
					svcName = p.Backend.Service.Name
				}
				if svcName != "" {
					ingToSvc[ing.Metadata.Name] = appendUnique(ingToSvc[ing.Metadata.Name], svcName)
				}
			}
		}
	}

	for _, p := range pods {
		pName := p.Metadata.Name
		if owner, ok := podOwner[pName]; ok {
			wlKey := owner.Kind + ":" + owner.Name
			wlToPod[wlKey] = appendUnique(wlToPod[wlKey], pName)
		}
		for _, vol := range p.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName != "" {
				podToPvc[pName] = appendUnique(podToPvc[pName], vol.PersistentVolumeClaim.ClaimName)
			}
		}
		for _, svc := range services {
			if labelsMatch(svc.Spec.Selector, p.Metadata.Labels) {
				if owner, ok := podOwner[pName]; ok {
					wlKey := owner.Kind + ":" + owner.Name
					svcToWl[svc.Metadata.Name] = appendUnique(svcToWl[svc.Metadata.Name], wlKey)
				}
			}
		}
	}

	// Find connected subgraph starting from (normKind, name).
	selIng := map[string]bool{}
	selSvc := map[string]bool{}
	selWl := map[string]bool{}
	selPod := map[string]bool{}
	selPvc := map[string]bool{}

	switch normKind {
	case "Ingress":
		selIng[name] = true
		for _, s := range ingToSvc[name] {
			selSvc[s] = true
		}
		expandDownFromSvcs(selSvc, selWl, selPod, selPvc, svcToWl, wlToPod, podToPvc)
	case "Service":
		selSvc[name] = true
		for ingName, svcs := range ingToSvc {
			if containsStr(svcs, name) {
				selIng[ingName] = true
			}
		}
		expandDownFromSvcs(selSvc, selWl, selPod, selPvc, svcToWl, wlToPod, podToPvc)
	case "Deployment", "StatefulSet", "DaemonSet":
		wlKey := normKind + ":" + name
		selWl[wlKey] = true
		for svcName, wls := range svcToWl {
			if containsStr(wls, wlKey) {
				selSvc[svcName] = true
			}
		}
		for ingName, svcs := range ingToSvc {
			for s := range selSvc {
				if containsStr(svcs, s) {
					selIng[ingName] = true
				}
			}
		}
		for _, pName := range wlToPod[wlKey] {
			selPod[pName] = true
			for _, vName := range podToPvc[pName] {
				selPvc[vName] = true
			}
		}
	case "Pod":
		selPod[name] = true
		for _, vName := range podToPvc[name] {
			selPvc[vName] = true
		}
		if owner, ok := podOwner[name]; ok {
			wlKey := owner.Kind + ":" + owner.Name
			selWl[wlKey] = true
			for svcName, wls := range svcToWl {
				if containsStr(wls, wlKey) {
					selSvc[svcName] = true
				}
			}
			for ingName, svcs := range ingToSvc {
				for s := range selSvc {
					if containsStr(svcs, s) {
						selIng[ingName] = true
					}
				}
			}
		}
	case "PVC":
		selPvc[name] = true
		for pName, pvcs := range podToPvc {
			if containsStr(pvcs, name) {
				selPod[pName] = true
				if owner, ok := podOwner[pName]; ok {
					wlKey := owner.Kind + ":" + owner.Name
					selWl[wlKey] = true
					for svcName, wls := range svcToWl {
						if containsStr(wls, wlKey) {
							selSvc[svcName] = true
						}
					}
				}
			}
		}
		for ingName, svcs := range ingToSvc {
			for s := range selSvc {
				if containsStr(svcs, s) {
					selIng[ingName] = true
				}
			}
		}
	}

	topo := &KubeTopology{
		Root: KubeTopologyNode{ID: topoID(normKind, ns, name), Kind: normKind, Namespace: ns, Name: name},
		Columns: []KubeTopologyColumn{
			{Stage: "ingress", Items: []KubeTopologyNode{}},
			{Stage: "service", Items: []KubeTopologyNode{}},
			{Stage: "workload", Items: []KubeTopologyNode{}},
			{Stage: "pod", Items: []KubeTopologyNode{}},
			{Stage: "pvc", Items: []KubeTopologyNode{}},
		},
		Edges: []KubeTopologyEdge{},
	}

	for _, ing := range ingresses {
		if !selIng[ing.Metadata.Name] {
			continue
		}
		hosts := make([]string, 0, len(ing.Spec.Rules))
		for _, r := range ing.Spec.Rules {
			if r.Host != "" {
				hosts = append(hosts, r.Host)
			}
		}
		topo.Columns[0].Items = append(topo.Columns[0].Items, KubeTopologyNode{
			ID: topoID("Ingress", ns, ing.Metadata.Name), Kind: "Ingress", Namespace: ns, Name: ing.Metadata.Name,
			Status: "Active", Meta: strings.Join(hosts, ", "),
		})
	}
	for _, svc := range services {
		if !selSvc[svc.Metadata.Name] {
			continue
		}
		meta := svc.Spec.Type
		if svc.Spec.ClusterIP != "" {
			meta += " " + svc.Spec.ClusterIP
		}
		topo.Columns[1].Items = append(topo.Columns[1].Items, KubeTopologyNode{
			ID: topoID("Service", ns, svc.Metadata.Name), Kind: "Service", Namespace: ns, Name: svc.Metadata.Name,
			Status: "Active", Meta: strings.TrimSpace(meta),
		})
	}
	for _, wl := range append(append([]rawKubeItem{}, deployments...), statefulsets...) {
		wlKey := wl.Kind + ":" + wl.Metadata.Name
		if !selWl[wlKey] {
			continue
		}
		desired := 1
		if wl.Spec.Replicas != nil {
			desired = *wl.Spec.Replicas
		}
		st := "Healthy"
		if wl.Status.ReadyReplicas < desired {
			st = "Degraded"
		}
		topo.Columns[2].Items = append(topo.Columns[2].Items, KubeTopologyNode{
			ID: topoID(wl.Kind, ns, wl.Metadata.Name), Kind: wl.Kind, Namespace: ns, Name: wl.Metadata.Name,
			Status: st, Meta: fmt.Sprintf("%d/%d ready", wl.Status.ReadyReplicas, desired),
		})
	}
	for _, p := range pods {
		if !selPod[p.Metadata.Name] {
			continue
		}
		topo.Columns[3].Items = append(topo.Columns[3].Items, KubeTopologyNode{
			ID: topoID("Pod", ns, p.Metadata.Name), Kind: "Pod", Namespace: ns, Name: p.Metadata.Name,
			Status: podTopoStatus(p), Meta: p.Status.PodIP,
		})
	}
	for _, v := range pvcs {
		if !selPvc[v.Metadata.Name] {
			continue
		}
		req := ""
		if v.Spec.Resources != nil {
			req = v.Spec.Resources.Requests["storage"]
		}
		topo.Columns[4].Items = append(topo.Columns[4].Items, KubeTopologyNode{
			ID: topoID("PVC", ns, v.Metadata.Name), Kind: "PVC", Namespace: ns, Name: v.Metadata.Name,
			Status: v.Status.Phase, Meta: req,
		})
	}

	for ingName := range selIng {
		for _, sName := range ingToSvc[ingName] {
			if selSvc[sName] {
				topo.Edges = append(topo.Edges, KubeTopologyEdge{From: topoID("Ingress", ns, ingName), To: topoID("Service", ns, sName)})
			}
		}
	}
	for sName := range selSvc {
		for _, wlKey := range svcToWl[sName] {
			if selWl[wlKey] {
				parts := strings.SplitN(wlKey, ":", 2)
				topo.Edges = append(topo.Edges, KubeTopologyEdge{From: topoID("Service", ns, sName), To: topoID(parts[0], ns, parts[1])})
			}
		}
	}
	for wlKey := range selWl {
		parts := strings.SplitN(wlKey, ":", 2)
		for _, pName := range wlToPod[wlKey] {
			if selPod[pName] {
				topo.Edges = append(topo.Edges, KubeTopologyEdge{From: topoID(parts[0], ns, parts[1]), To: topoID("Pod", ns, pName)})
			}
		}
	}
	for pName := range selPod {
		for _, vName := range podToPvc[pName] {
			if selPvc[vName] {
				topo.Edges = append(topo.Edges, KubeTopologyEdge{From: topoID("Pod", ns, pName), To: topoID("PVC", ns, vName)})
			}
		}
	}

	sort.Slice(topo.Edges, func(i, j int) bool {
		if topo.Edges[i].From != topo.Edges[j].From {
			return topo.Edges[i].From < topo.Edges[j].From
		}
		return topo.Edges[i].To < topo.Edges[j].To
	})

	return topo, nil
}

func expandDownFromSvcs(selSvc, selWl, selPod, selPvc map[string]bool, svcToWl, wlToPod, podToPvc map[string][]string) {
	for sName := range selSvc {
		for _, wlKey := range svcToWl[sName] {
			selWl[wlKey] = true
			for _, pName := range wlToPod[wlKey] {
				selPod[pName] = true
				for _, vName := range podToPvc[pName] {
					selPvc[vName] = true
				}
			}
		}
	}
}

func appendUnique(slice []string, val string) []string {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}
