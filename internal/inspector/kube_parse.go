package inspector

// JSON output parsers for Kubernetes inspection commands.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------- JSON parsers

func parseKubeDetect(out string) (*KubeClusterInfo, error) {
	sections := splitDockerSections(out)
	distro := strings.TrimSpace(sections["===DISTRO==="])
	if distro == "" {
		distro = strings.TrimSpace(sections["DISTRO"])
	}
	if distro == "" {
		return nil, errors.New("no k3s/rke2 control plane found")
	}
	verRaw := strings.TrimSpace(sections["===VERSION==="])
	if verRaw == "" {
		verRaw = strings.TrimSpace(sections["VERSION"])
	}
	version := extractKubeVersion(verRaw)
	if version == "" {
		version = "unknown"
	}
	clusterID := strings.TrimSpace(sections["===CLUSTER_ID==="])
	if clusterID == "" {
		clusterID = strings.TrimSpace(sections["CLUSTER_ID"])
	}
	return &KubeClusterInfo{
		Distro:    distro,
		Version:   version,
		Role:      "server",
		ClusterID: clusterID,
	}, nil
}

func extractKubeVersion(verRaw string) string {
	var v struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
		ClientVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"clientVersion"`
	}
	if err := json.Unmarshal([]byte(verRaw), &v); err == nil {
		if v.ServerVersion.GitVersion != "" {
			return v.ServerVersion.GitVersion
		}
		if v.ClientVersion.GitVersion != "" {
			return v.ClientVersion.GitVersion
		}
	}
	if m := kubeVerRe.FindString(verRaw); m != "" {
		return m
	}
	return ""
}

type rawNodeList struct {
	Items []struct {
		Metadata struct {
			Name              string            `json:"name"`
			CreationTimestamp string            `json:"creationTimestamp"`
			Labels            map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Unschedulable bool `json:"unschedulable"`
		} `json:"spec"`
		Status struct {
			Addresses []struct {
				Type    string `json:"type"`
				Address string `json:"address"`
			} `json:"addresses"`
			Capacity struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"capacity"`
			Allocatable struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"allocatable"`
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
			NodeInfo struct {
				KubeletVersion string `json:"kubeletVersion"`
			} `json:"nodeInfo"`
		} `json:"status"`
	} `json:"items"`
}

func parseKubeNodes(jsonStr string) ([]KubeNode, error) {
	payloadJSON, metricsJSON := splitPayloadAndMetrics(jsonStr, "===NODES===")
	var list rawNodeList
	if err := json.Unmarshal([]byte(payloadJSON), &list); err != nil {
		return nil, fmt.Errorf("parse nodes: %w", err)
	}
	nodes := make([]KubeNode, 0, len(list.Items))
	allocatables := make(map[string]nodeAllocatableData, len(list.Items))
	for _, item := range list.Items {
		var internalIP string
		for _, addr := range item.Status.Addresses {
			if addr.Type == "InternalIP" {
				internalIP = addr.Address
				break
			}
		}

		status := "Unknown"
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" {
				if cond.Status == "True" {
					status = "Ready"
				} else if cond.Status == "False" {
					status = "NotReady"
				}
				break
			}
		}

		var roles []string
		for k := range item.Metadata.Labels {
			if strings.HasPrefix(k, "node-role.kubernetes.io/") {
				r := strings.TrimPrefix(k, "node-role.kubernetes.io/")
				if r != "" {
					roles = append(roles, r)
				}
			}
		}
		sort.Strings(roles)
		rolesStr := strings.Join(roles, ",")
		if rolesStr == "" {
			if r, ok := item.Metadata.Labels["kubernetes.io/role"]; ok && r != "" {
				rolesStr = r
			} else {
				rolesStr = "<none>"
			}
		}

		nodes = append(nodes, KubeNode{
			Name:          item.Metadata.Name,
			Status:        status,
			Roles:         rolesStr,
			Version:       item.Status.NodeInfo.KubeletVersion,
			InternalIP:    internalIP,
			CPU:           item.Status.Capacity.CPU,
			Memory:        item.Status.Capacity.Memory,
			Created:       item.Metadata.CreationTimestamp,
			Unschedulable: item.Spec.Unschedulable,
		})
		allocatables[item.Metadata.Name] = nodeAllocatableData{
			cpu: item.Status.Allocatable.CPU,
			mem: item.Status.Allocatable.Memory,
		}
	}
	applyNodeMetrics(nodes, metricsJSON, allocatables)
	return nodes, nil
}

type rawNamespaceList struct {
	Items []struct {
		Metadata struct {
			Name              string `json:"name"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	} `json:"items"`
}

func parseKubeNamespaces(jsonStr string) ([]KubeNamespace, error) {
	var list rawNamespaceList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return nil, fmt.Errorf("parse namespaces: %w", err)
	}
	nss := make([]KubeNamespace, 0, len(list.Items))
	for _, item := range list.Items {
		nss = append(nss, KubeNamespace{
			Name:    item.Metadata.Name,
			Status:  item.Status.Phase,
			Created: item.Metadata.CreationTimestamp,
		})
	}
	return nss, nil
}

type rawWorkloadList struct {
	Items []struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name              string `json:"name"`
			Namespace         string `json:"namespace"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			Replicas               int `json:"replicas"`
			ReadyReplicas          int `json:"readyReplicas"`
			AvailableReplicas      int `json:"availableReplicas"`
			DesiredNumberScheduled int `json:"desiredNumberScheduled"`
			NumberReady            int `json:"numberReady"`
			NumberAvailable        int `json:"numberAvailable"`
		} `json:"status"`
	} `json:"items"`
}

func parseKubeWorkloads(jsonStr string) ([]KubeWorkload, error) {
	var list rawWorkloadList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return nil, fmt.Errorf("parse workloads: %w", err)
	}
	workloads := make([]KubeWorkload, 0, len(list.Items))
	for _, item := range list.Items {
		var images []string
		for _, c := range item.Spec.Template.Spec.Containers {
			if c.Image != "" {
				images = append(images, c.Image)
			}
		}

		var ready, desired, available int
		switch item.Kind {
		case "Deployment":
			desired = 1
			if item.Spec.Replicas != nil {
				desired = *item.Spec.Replicas
			}
			ready = item.Status.ReadyReplicas
			available = item.Status.AvailableReplicas
		case "StatefulSet":
			desired = 1
			if item.Spec.Replicas != nil {
				desired = *item.Spec.Replicas
			}
			ready = item.Status.ReadyReplicas
			available = item.Status.AvailableReplicas
			if available == 0 && ready > 0 {
				available = ready
			}
		case "DaemonSet":
			desired = item.Status.DesiredNumberScheduled
			ready = item.Status.NumberReady
			available = item.Status.NumberAvailable
		}

		workloads = append(workloads, KubeWorkload{
			Kind:      item.Kind,
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			Ready:     ready,
			Desired:   desired,
			Available: available,
			Images:    images,
			Created:   item.Metadata.CreationTimestamp,
		})
	}
	return workloads, nil
}

type rawPodContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int    `json:"restartCount"`
	State        struct {
		Waiting *struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
		Terminated *struct {
			Reason   string `json:"reason"`
			ExitCode int    `json:"exitCode"`
		} `json:"terminated"`
		Running *struct{} `json:"running"`
	} `json:"state"`
}

type rawPodList struct {
	Items []struct {
		Metadata struct {
			Name              string  `json:"name"`
			Namespace         string  `json:"namespace"`
			CreationTimestamp string  `json:"creationTimestamp"`
			DeletionTimestamp *string `json:"deletionTimestamp"`
		} `json:"metadata"`
		Spec struct {
			NodeName   string `json:"nodeName"`
			Containers []struct {
				Name string `json:"name"`
			} `json:"containers"`
		} `json:"spec"`
		Status struct {
			Phase                 string                  `json:"phase"`
			PodIP                 string                  `json:"podIP"`
			Reason                string                  `json:"reason"`
			ContainerStatuses     []rawPodContainerStatus `json:"containerStatuses"`
			InitContainerStatuses []rawPodContainerStatus `json:"initContainerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func parseKubePods(jsonStr string) ([]KubePod, error) {
	payloadJSON, metricsJSON := splitPayloadAndMetrics(jsonStr, "===PODS===")
	var list rawPodList
	if err := json.Unmarshal([]byte(payloadJSON), &list); err != nil {
		return nil, fmt.Errorf("parse pods: %w", err)
	}
	pods := make([]KubePod, 0, len(list.Items))
	for _, item := range list.Items {
		containers := make([]string, 0, len(item.Spec.Containers))
		for _, c := range item.Spec.Containers {
			containers = append(containers, c.Name)
		}

		readyCount := 0
		totalCount := len(item.Spec.Containers)
		restarts := 0
		for _, cs := range item.Status.ContainerStatuses {
			if cs.Ready {
				readyCount++
			}
			restarts += cs.RestartCount
		}
		for _, cs := range item.Status.InitContainerStatuses {
			restarts += cs.RestartCount
		}

		status := item.Status.Phase
		if status == "" {
			status = "Unknown"
		}
		if item.Metadata.DeletionTimestamp != nil {
			status = "Terminating"
		} else if item.Status.Reason != "" {
			status = item.Status.Reason
		} else {
			for _, ics := range item.Status.InitContainerStatuses {
				if ics.State.Waiting != nil && ics.State.Waiting.Reason != "" {
					status = "Init:" + ics.State.Waiting.Reason
					break
				}
				if ics.State.Terminated != nil && ics.State.Terminated.ExitCode != 0 {
					if ics.State.Terminated.Reason != "" {
						status = "Init:" + ics.State.Terminated.Reason
					} else {
						status = fmt.Sprintf("Init:ExitCode:%d", ics.State.Terminated.ExitCode)
					}
					break
				}
			}
			if status == item.Status.Phase || status == "Unknown" {
				for _, cs := range item.Status.ContainerStatuses {
					if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
						status = cs.State.Waiting.Reason
						break
					}
					if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
						status = cs.State.Terminated.Reason
						break
					}
				}
			}
		}

		pods = append(pods, KubePod{
			Namespace:  item.Metadata.Namespace,
			Name:       item.Metadata.Name,
			Ready:      fmt.Sprintf("%d/%d", readyCount, totalCount),
			Status:     status,
			Restarts:   restarts,
			Node:       item.Spec.NodeName,
			IP:         item.Status.PodIP,
			Created:    item.Metadata.CreationTimestamp,
			Containers: containers,
		})
	}
	applyPodMetrics(pods, metricsJSON)
	return pods, nil
}

type rawServiceList struct {
	Items []struct {
		Metadata struct {
			Name              string `json:"name"`
			Namespace         string `json:"namespace"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		Spec struct {
			Type         string   `json:"type"`
			ClusterIP    string   `json:"clusterIP"`
			ExternalIPs  []string `json:"externalIPs"`
			ExternalName string   `json:"externalName"`
			Ports        []struct {
				Port     int    `json:"port"`
				NodePort int    `json:"nodePort"`
				Protocol string `json:"protocol"`
			} `json:"ports"`
		} `json:"spec"`
		Status struct {
			LoadBalancer struct {
				Ingress []struct {
					IP       string `json:"ip"`
					Hostname string `json:"hostname"`
				} `json:"ingress"`
			} `json:"loadBalancer"`
		} `json:"status"`
	} `json:"items"`
}

func parseKubeServices(jsonStr string) ([]KubeService, error) {
	var list rawServiceList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return nil, fmt.Errorf("parse services: %w", err)
	}
	services := make([]KubeService, 0, len(list.Items))
	for _, item := range list.Items {
		externalIP := "<none>"
		if len(item.Status.LoadBalancer.Ingress) > 0 {
			ing := item.Status.LoadBalancer.Ingress[0]
			if ing.IP != "" {
				externalIP = ing.IP
			} else if ing.Hostname != "" {
				externalIP = ing.Hostname
			}
		} else if len(item.Spec.ExternalIPs) > 0 {
			externalIP = strings.Join(item.Spec.ExternalIPs, ",")
		} else if item.Spec.ExternalName != "" {
			externalIP = item.Spec.ExternalName
		}

		var portStrs []string
		for _, p := range item.Spec.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "TCP"
			}
			if p.NodePort > 0 {
				portStrs = append(portStrs, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, proto))
			} else {
				portStrs = append(portStrs, fmt.Sprintf("%d/%s", p.Port, proto))
			}
		}

		services = append(services, KubeService{
			Namespace:  item.Metadata.Namespace,
			Name:       item.Metadata.Name,
			Type:       item.Spec.Type,
			ClusterIP:  item.Spec.ClusterIP,
			ExternalIP: externalIP,
			Ports:      strings.Join(portStrs, ","),
			Created:    item.Metadata.CreationTimestamp,
		})
	}
	return services, nil
}

type rawEventList struct {
	Items []struct {
		Metadata struct {
			Namespace         string `json:"namespace"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		InvolvedObject struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"involvedObject"`
		Reason        string `json:"reason"`
		Message       string `json:"message"`
		Type          string `json:"type"`
		Count         int    `json:"count"`
		LastTimestamp string `json:"lastTimestamp"`
		EventTime     string `json:"eventTime"`
	} `json:"items"`
}

func parseKubeEvents(jsonStr string) ([]KubeEvent, error) {
	var list rawEventList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return nil, fmt.Errorf("parse events: %w", err)
	}
	events := make([]KubeEvent, 0, len(list.Items))
	for _, item := range list.Items {
		last := item.LastTimestamp
		if last == "" {
			last = item.EventTime
		}
		if last == "" {
			last = item.Metadata.CreationTimestamp
		}

		count := item.Count
		if count <= 0 {
			count = 1
		}

		obj := item.InvolvedObject.Name
		if item.InvolvedObject.Kind != "" && obj != "" {
			obj = item.InvolvedObject.Kind + "/" + obj
		}

		events = append(events, KubeEvent{
			Namespace: item.Metadata.Namespace,
			Type:      item.Type,
			Reason:    item.Reason,
			Object:    obj,
			Message:   item.Message,
			Count:     count,
			Last:      last,
		})
	}

	sort.Slice(events, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, events[i].Last)
		tj, _ := time.Parse(time.RFC3339, events[j].Last)
		return ti.After(tj)
	})
	if len(events) > 100 {
		events = events[:100]
	}
	return events, nil
}
