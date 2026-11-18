package inspector

// Resource spec table and tabular parsing for the extended Kubernetes resource kinds.

import (
	"fmt"
	"strings"
)

type kubeResourceSpec struct {
	resource      string
	clusterScoped bool
	columns       []string
	template      string
}

var kubeResourceSpecs = map[string]kubeResourceSpec{
	"ingresses": {
		resource: "ingress",
		columns:  []string{"namespace", "name", "class", "hosts", "address", "created"},
		template: `{{range .items}}{{.metadata.namespace}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{or .spec.ingressClassName ""}}{{"\t"}}{{range $i, $r := .spec.rules}}{{if $i}},{{end}}{{or $r.host "*"}}{{end}}{{"\t"}}{{range $i, $e := .status.loadBalancer.ingress}}{{if $i}},{{end}}{{or $e.ip $e.hostname}}{{end}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
	"configmaps": {
		resource: "cm",
		columns:  []string{"namespace", "name", "keys", "created"},
		template: `{{range .items}}{{.metadata.namespace}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{if .data}}{{len .data}}{{else}}0{{end}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
	"secrets": {
		resource: "secret",
		columns:  []string{"namespace", "name", "type", "keys", "created"},
		template: `{{range .items}}{{.metadata.namespace}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{.type}}{{"\t"}}{{if .data}}{{len .data}}{{else}}0{{end}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
	"pvcs": {
		resource: "pvc",
		columns:  []string{"namespace", "name", "status", "volume", "capacity", "storageclass", "created"},
		template: `{{range .items}}{{.metadata.namespace}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{.status.phase}}{{"\t"}}{{or .spec.volumeName ""}}{{"\t"}}{{or .status.capacity.storage ""}}{{"\t"}}{{or .spec.storageClassName ""}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
	"pvs": {
		resource:      "pv",
		clusterScoped: true,
		columns:       []string{"name", "status", "capacity", "claim", "storageclass", "created"},
		template:      `{{range .items}}{{.metadata.name}}{{"\t"}}{{.status.phase}}{{"\t"}}{{or .spec.capacity.storage ""}}{{"\t"}}{{if .spec.claimRef}}{{.spec.claimRef.namespace}}/{{.spec.claimRef.name}}{{end}}{{"\t"}}{{or .spec.storageClassName ""}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
	"jobs": {
		resource: "job",
		columns:  []string{"namespace", "name", "completions", "status", "created"},
		template: `{{range .items}}{{.metadata.namespace}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{or .status.succeeded 0}}/{{or .spec.completions 1}}{{"\t"}}{{if .status.completionTime}}Complete{{else if .status.failed}}Failed{{else if .spec.suspend}}Suspended{{else}}Running{{end}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
	"cronjobs": {
		resource: "cronjob",
		columns:  []string{"namespace", "name", "schedule", "suspend", "lastSchedule", "created"},
		template: `{{range .items}}{{.metadata.namespace}}{{"\t"}}{{.metadata.name}}{{"\t"}}{{.spec.schedule}}{{"\t"}}{{if .spec.suspend}}true{{else}}false{{end}}{{"\t"}}{{or .status.lastScheduleTime "-"}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`,
	},
}

func init() {
	aliases := map[string]string{
		"ingress":   "ingresses",
		"configmap": "configmaps",
		"cm":        "configmaps",
		"secret":    "secrets",
		"pvc":       "pvcs",
		"pv":        "pvs",
		"job":       "jobs",
		"cronjob":   "cronjobs",
	}
	for alias, target := range aliases {
		kubeResourceSpecs[alias] = kubeResourceSpecs[target]
	}
}

func parseTabularResources(out string, numCols int) [][]string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < numCols {
			padded := make([]string, numCols)
			copy(padded, parts)
			parts = padded
		} else if len(parts) > numCols {
			parts = parts[:numCols]
		}
		rows = append(rows, parts)
	}
	return rows
}

func (ins *Inspector) KubeResources(hostID int64, kind, ns string) ([]string, [][]string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	ns = strings.TrimSpace(ns)

	spec, ok := kubeResourceSpecs[kind]
	if !ok {
		return nil, nil, kubeInvalid("unsupported kind: %s", kind)
	}

	var cmd string
	if spec.clusterScoped {
		cmd = fmt.Sprintf("kc get %s -o go-template=%s", spec.resource, shellQuote(spec.template))
	} else if ns != "" {
		if !ValidKubeNamespace(ns) {
			return nil, nil, kubeInvalid("invalid namespace: %s", ns)
		}
		cmd = fmt.Sprintf("kc get %s -n %s -o go-template=%s", spec.resource, shellQuote(ns), shellQuote(spec.template))
	} else {
		cmd = fmt.Sprintf("kc get %s -A -o go-template=%s", spec.resource, shellQuote(spec.template))
	}

	out, err := ins.kubeQuery(hostID, cmd, "kube resources "+kind, kubeQueryTimeout)
	if err != nil {
		return nil, nil, err
	}
	rows := parseTabularResources(out, len(spec.columns))
	return spec.columns, rows, nil
}
