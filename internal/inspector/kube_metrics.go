package inspector

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	maxPodMetricsPayload  = 2 * 1024 * 1024 // 2 MB: drop pod metrics when payload exceeds this limit
	maxNodeMetricsPayload = 2 * 1024 * 1024
)

// parseKubeCPUMilli parses a Kubernetes CPU quantity string into millicores.
// Supported units: m (millicores), n (nanocores), u (microcores), or plain whole/decimal cores.
func parseKubeCPUMilli(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty cpu quantity")
	}
	if strings.HasSuffix(s, "m") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid cpu milli: %q", s)
		}
		return int64(math.Round(v)), nil
	}
	if strings.HasSuffix(s, "n") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "n"), 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid cpu nano: %q", s)
		}
		return int64(math.Round(v / 1_000_000.0)), nil
	}
	if strings.HasSuffix(s, "u") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "u"), 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid cpu micro: %q", s)
		}
		return int64(math.Round(v / 1_000.0)), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid cpu cores: %q", s)
	}
	return int64(math.Round(v * 1000.0)), nil
}

// parseKubeMemoryBytes parses a Kubernetes memory quantity string into bytes.
// Supported units: binary SI (Ki, Mi, Gi, Ti, Pi, Ei), decimal SI (k, M, G, T), or plain bytes.
func parseKubeMemoryBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty memory quantity")
	}
	// shortcut: list common prefixes in descending length order, upgrade if binary float notation needed
	suffixes := []struct {
		suffix string
		mult   float64
	}{
		{"eib", 1024 * 1024 * 1024 * 1024 * 1024 * 1024},
		{"ei", 1024 * 1024 * 1024 * 1024 * 1024 * 1024},
		{"pib", 1024 * 1024 * 1024 * 1024 * 1024},
		{"pi", 1024 * 1024 * 1024 * 1024 * 1024},
		{"tib", 1024 * 1024 * 1024 * 1024},
		{"ti", 1024 * 1024 * 1024 * 1024},
		{"gib", 1024 * 1024 * 1024},
		{"gi", 1024 * 1024 * 1024},
		{"mib", 1024 * 1024},
		{"mi", 1024 * 1024},
		{"kib", 1024},
		{"ki", 1024},
		{"t", 1000 * 1000 * 1000 * 1000},
		{"g", 1000 * 1000 * 1000},
		{"m", 1000 * 1000},
		{"k", 1000},
		{"b", 1},
	}
	lower := strings.ToLower(s)
	for _, entry := range suffixes {
		if strings.HasSuffix(lower, entry.suffix) {
			numStr := strings.TrimSpace(s[:len(s)-len(entry.suffix)])
			v, err := strconv.ParseFloat(numStr, 64)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("invalid memory quantity: %q", s)
			}
			return int64(math.Round(v * entry.mult)), nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid memory bytes: %q", s)
	}
	return int64(math.Round(v)), nil
}

// splitPayloadAndMetrics splits combined script output on ===METRICS=== if present.
func splitPayloadAndMetrics(out, primarySection string) (payloadJSON, metricsJSON string) {
	if strings.Contains(out, "===METRICS===") {
		sections := splitDockerSections(out)
		if p, ok := sections[primarySection]; ok {
			payloadJSON = p
		} else if p, ok := sections[strings.Trim(primarySection, "=")]; ok {
			payloadJSON = p
		}
		if payloadJSON == "" {
			parts := strings.SplitN(out, "===METRICS===", 2)
			payloadJSON = parts[0]
			if len(parts) > 1 {
				metricsJSON = parts[1]
			}
		} else {
			metricsJSON = sections["===METRICS==="]
			if metricsJSON == "" {
				metricsJSON = sections["METRICS"]
			}
		}
	} else {
		payloadJSON = out
	}
	return strings.TrimSpace(payloadJSON), strings.TrimSpace(metricsJSON)
}

type rawNodeMetricsList struct {
	Kind  string `json:"kind"`
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Usage struct {
			CPU    string `json:"cpu"`
			Memory string `json:"memory"`
		} `json:"usage"`
	} `json:"items"`
}

type rawPodMetricsList struct {
	Kind  string `json:"kind"`
	Items []struct {
		Metadata struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"metadata"`
		Containers []struct {
			Name  string `json:"name"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

type nodeMetricData struct {
	cpu    string
	memory string
}

type podMetricData struct {
	usageCPU    string
	usageMemory string
}

func parseNodeMetrics(jsonStr string) map[string]nodeMetricData {
	res := make(map[string]nodeMetricData)
	if len(jsonStr) == 0 || len(jsonStr) > maxNodeMetricsPayload {
		return res
	}
	var list rawNodeMetricsList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return res
	}
	if list.Kind != "" && list.Kind != "NodeMetricsList" {
		return res
	}
	for _, it := range list.Items {
		if it.Metadata.Name != "" {
			res[it.Metadata.Name] = nodeMetricData{
				cpu:    it.Usage.CPU,
				memory: it.Usage.Memory,
			}
		}
	}
	return res
}

func parsePodMetrics(jsonStr string) map[string]podMetricData {
	res := make(map[string]podMetricData)
	if len(jsonStr) == 0 || len(jsonStr) > maxPodMetricsPayload {
		return res
	}
	var list rawPodMetricsList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return res
	}
	if list.Kind != "" && list.Kind != "PodMetricsList" {
		return res
	}
	for _, it := range list.Items {
		var totalMilli int64
		var totalMemBytes int64
		hasCPU := false
		hasMem := false

		for _, c := range it.Containers {
			if c.Usage.CPU != "" {
				if m, err := parseKubeCPUMilli(c.Usage.CPU); err == nil {
					totalMilli += m
					hasCPU = true
				}
			}
			if c.Usage.Memory != "" {
				if b, err := parseKubeMemoryBytes(c.Usage.Memory); err == nil {
					totalMemBytes += b
					hasMem = true
				}
			}
		}

		var usageCPU, usageMem string
		if hasCPU {
			usageCPU = fmt.Sprintf("%dm", totalMilli)
		}
		if hasMem {
			usageMem = fmt.Sprintf("%dKi", totalMemBytes/1024)
		}

		key := it.Metadata.Namespace + "/" + it.Metadata.Name
		res[key] = podMetricData{
			usageCPU:    usageCPU,
			usageMemory: usageMem,
		}
	}
	return res
}

type nodeAllocatableData struct {
	cpu string
	mem string
}

func applyNodeMetrics(nodes []KubeNode, metricsJSON string, allocatables map[string]nodeAllocatableData) {
	metrics := parseNodeMetrics(metricsJSON)
	for i := range nodes {
		n := &nodes[i]
		alloc := allocatables[n.Name]
		if alloc.cpu != "" {
			n.AllocatableCPU = alloc.cpu
		}
		if alloc.mem != "" {
			n.AllocatableMem = alloc.mem
		}

		m, ok := metrics[n.Name]
		if !ok {
			continue
		}

		if m.cpu != "" {
			if uMilli, err := parseKubeCPUMilli(m.cpu); err == nil {
				n.UsageCPU = fmt.Sprintf("%dm", uMilli)
				if alloc.cpu != "" {
					if aMilli, err := parseKubeCPUMilli(alloc.cpu); err == nil && aMilli > 0 {
						pct := math.Round((float64(uMilli)/float64(aMilli)*100.0)*10.0) / 10.0
						n.CPUPercent = &pct
					}
				}
			}
		}

		if m.memory != "" {
			if uBytes, err := parseKubeMemoryBytes(m.memory); err == nil {
				n.UsageMemory = m.memory
				if alloc.mem != "" {
					if aBytes, err := parseKubeMemoryBytes(alloc.mem); err == nil && aBytes > 0 {
						pct := math.Round((float64(uBytes)/float64(aBytes)*100.0)*10.0) / 10.0
						n.MemoryPercent = &pct
					}
				}
			}
		}
	}
}

func applyPodMetrics(pods []KubePod, metricsJSON string) {
	metrics := parsePodMetrics(metricsJSON)
	for i := range pods {
		p := &pods[i]
		key := p.Namespace + "/" + p.Name
		if m, ok := metrics[key]; ok {
			p.UsageCPU = m.usageCPU
			p.UsageMemory = m.usageMemory
		}
	}
}
