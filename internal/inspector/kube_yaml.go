package inspector

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// kubeYAMLKinds whitelists resources viewable/editable via kube/yaml.
// Secret is deliberately excluded: never view or edit secrets.
var kubeYAMLKinds = map[string]string{
	"deployment":  "Deployment",
	"statefulset": "StatefulSet",
	"daemonset":   "DaemonSet",
	"service":     "Service",
	"ingress":     "Ingress",
	"configmap":   "ConfigMap",
	"pvc":         "PersistentVolumeClaim",
	"job":         "Job",
	"cronjob":     "CronJob",
	"namespace":   "Namespace",
}

// KubeYAMLAllowed reports whether kind is whitelisted for YAML view/edit.
func KubeYAMLAllowed(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if _, ok := kubeYAMLKinds[kind]; !ok {
		return false
	}
	return true
}

// KubeYAML fetches the resource as JSON, strips server-only fields, and emits
// clean YAML suitable for kc diff / kc apply.
func (ins *Inspector) KubeYAML(hostID int64, kind, ns, name string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	name = strings.TrimSpace(name)
	ns = strings.TrimSpace(ns)

	kindName, ok := kubeYAMLKinds[kind]
	if !ok {
		return "", kubeInvalid("unsupported kind for yaml: %s", kind)
	}
	if !ValidKubeName(name) {
		return "", kubeInvalid("invalid name: %s", name)
	}
	if kind != "namespace" {
		if !ValidKubeNamespace(ns) {
			return "", kubeInvalid("invalid namespace: %s", ns)
		}
	}
	cmd := fmt.Sprintf("kc get %s %s -o json", kind, shellQuote(name))
	if kind != "namespace" {
		cmd += " -n " + shellQuote(ns)
	}
	out, err := ins.kubeQuery(hostID, cmd, "kube yaml "+kindName, kubeQueryTimeout)
	if err != nil {
		return "", err
	}
	return kubeJSONToYAML(out, kind)
}

// kubeJSONToYAML cleans server-only fields from a kubectl -o json payload and
// emits YAML. Returns the YAML string.
func kubeJSONToYAML(out, kind string) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return "", fmt.Errorf("kube yaml: cannot parse resource JSON: %w", err)
	}
	cleanKubeJSON(doc, kind)
	var b strings.Builder
	if err := emitYAML(&b, doc, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// kubeJSONToYAMLDoc emits YAML from an already-decoded JSON document (test hook).
func kubeJSONToYAMLDoc(doc map[string]any) (string, error) {
	var b strings.Builder
	if err := emitYAML(&b, doc, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// cleanKubeJSON removes fields kubectl apply must not round-trip: status,
// server-assigned metadata, the last-applied annotation, and for Service the
// cluster-assigned IP fields. Defense in depth: if a Secret somehow passed the
// whitelist, drop its data so secrets are never exposed.
func cleanKubeJSON(doc map[string]any, kind string) {
	delete(doc, "status")
	if string(kind) == "Secret" || strings.EqualFold(kind, "secret") {
		delete(doc, "data")
		delete(doc, "stringData")
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta != nil {
		for _, k := range []string{"uid", "creationTimestamp", "generation", "managedFields", "selfLink", "ownerReferences"} {
			delete(meta, k)
		}
		if ann, ok := meta["annotations"].(map[string]any); ok {
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			if len(ann) == 0 {
				delete(meta, "annotations")
			}
		}
	}
	if strings.EqualFold(kind, "service") {
		if spec, ok := doc["spec"].(map[string]any); ok {
			for _, k := range []string{"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy"} {
				delete(spec, k)
			}
		}
	}
}

// emitYAML writes v as YAML with keys in lexicographic order. Scalars that are
// not safe as bare YAML are emitted as JSON scalars (always valid YAML flow
// scalars). Multi-line strings use block style.
func emitYAML(b *strings.Builder, v any, indent int) error {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			b.WriteString(strings.Repeat(" ", indent) + "{}\n")
			return nil
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(strings.Repeat(" ", indent))
			b.WriteString(yamlSafeKey(k))
			b.WriteString(":")
			emitChild(b, t[k], indent)
		}
	case []any:
		if len(t) == 0 {
			b.WriteString(strings.Repeat(" ", indent) + "[]\n")
			return nil
		}
		for _, item := range t {
			b.WriteString(strings.Repeat(" ", indent))
			b.WriteString("-")
			emitChild(b, item, indent)
		}
	default:
		b.WriteString(strings.Repeat(" ", indent))
		b.WriteString(yamlScalar(v))
		b.WriteString("\n")
	}
	return nil
}

// emitChild writes ": value" or ": \n<nested>" for a map/list child.
func emitChild(b *strings.Builder, child any, indent int) {
	switch child.(type) {
	case map[string]any, []any:
		// Non-empty containers nest on the following lines; empty ones stay inline.
		if isEmptyContainer(child) {
			b.WriteString(" ")
			_ = emitYAML(b, child, 0)
			return
		}
		b.WriteString("\n")
		_ = emitYAML(b, child, indent+2)
	default:
		b.WriteString(" ")
		b.WriteString(yamlScalar(child))
		b.WriteString("\n")
	}
}

func isEmptyContainer(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// yamlSafeKey quotes keys that are not safe as plain YAML keys.
func yamlSafeKey(k string) string {
	if k == "" || !yamlPlainSafe(k) {
		return string(mustJSON(k))
	}
	return k
}

// yamlScalar formats a scalar value: strings as plain text when safe, as JSON
// (quoted) otherwise; multi-line strings as a block literal.
func yamlScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers arrive as float64; keep the original JSON rendering.
		return jsonNumber(t)
	case string:
		return yamlString(t)
	default:
		// Should not happen for encoding/json output; fall back to JSON.
		raw, err := json.Marshal(v)
		if err != nil {
			return `"?"`
		}
		return string(raw)
	}
}

// jsonNumber renders a float64 like encoding/json would (shortest round-trip).
func jsonNumber(f float64) string { return mustJSON(f) }

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return `"?"`
	}
	return string(raw)
}

var yamlBlockIndicators = "-?:[]{}#&*!|>'\"%@`,"

// isNumericLike reports whether s would parse as a YAML number (int/float/exp).
func isNumericLike(s string) bool {
	if s == "" {
		return false
	}
	body := s
	if body[0] == '+' || body[0] == '-' {
		body = body[1:]
	}
	if body == "" {
		return false
	}
	digits, dots, exp := 0, 0, false
	for i := range len(body) {
		c := body[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
			dots++
			if dots > 1 {
				return false
			}
		case (c == 'e' || c == 'E') && !exp && digits > 0 && i < len(body)-1:
			exp = true
			if body[i+1] == '+' || body[i+1] == '-' {
				i++
			}
		default:
			return false
		}
	}
	return digits > 0
}

func yamlPlainSafe(s string) bool {
	if s == "" {
		return false
	}
	// Leading character restrictions.
	if strings.ContainsRune(yamlBlockIndicators, rune(s[0])) {
		return false
	}
	if s == "true" || s == "false" || s == "null" || s == "yes" || s == "no" ||
		s == "on" || s == "off" || s == "~" || s == "True" || s == "False" || s == "Null" ||
		s == "TRUE" || s == "FALSE" || s == "NULL" {
		return false
	}
	if isNumericLike(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	// ": " or " #" anywhere needs quoting.
	if strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":") {
		return false
	}
	// Leading/trailing spaces are not preserved in plain style.
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return false
	}
	return true
}

// yamlString renders a string: block literal for multi-line, plain if safe,
// JSON double-quoted otherwise.
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	if strings.Contains(s, "\n") && !strings.Contains(s, "\r") &&
		!strings.HasPrefix(s, " ") && !strings.HasSuffix(s, " ") &&
		!strings.HasSuffix(s, "\n") {
		return emitYAMLBlockLiteral(s)
	}
	if yamlPlainSafe(s) {
		return s
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(raw)
}

// emitYAMLBlockLiteral renders s as a |-style block literal.
func emitYAMLBlockLiteral(s string) string {
	var b strings.Builder
	b.WriteString("|-")
	for _, line := range strings.Split(s, "\n") {
		b.WriteString("\n")
		b.WriteString(line)
	}
	return b.String()
}
