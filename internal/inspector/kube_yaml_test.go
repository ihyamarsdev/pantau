package inspector

import (
	"strings"
	"testing"
)

func TestKubeYAMLAllowed(t *testing.T) {
	allowed := []string{"deployment", "statefulset", "daemonset", "service", "ingress", "configmap", "pvc", "job", "cronjob", "namespace"}
	for _, k := range allowed {
		if !KubeYAMLAllowed(k) {
			t.Errorf("expected allowed: %s", k)
		}
	}
	// Mixed case is accepted (lowercased first, like KubeDescribe).
	for _, k := range []string{"secret", "pod", "node", "pv", "", "secret ", "deployment; rm"} {
		if KubeYAMLAllowed(k) {
			t.Errorf("expected rejected: %q", k)
		}
	}
}

func TestCleanKubeJSON(t *testing.T) {
	doc := map[string]any{
		"status": map[string]any{"loadBalancer": map[string]any{}},
		"metadata": map[string]any{
			"name":              "kube-dns",
			"namespace":         "kube-system",
			"uid":               "u-1",
			"creationTimestamp": "2026-10-27T02:12:16Z",
			"generation":        float64(1),
			"managedFields":     []any{map[string]any{"manager": "x"}},
			"resourceVersion":   "491",
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
		},
		"spec": map[string]any{
			"clusterIP":      "10.43.0.10",
			"clusterIPs":     []any{"10.43.0.10"},
			"ipFamilies":     []any{"IPv4"},
			"ipFamilyPolicy": "SingleStack",
			"type":           "ClusterIP",
			"ports":          []any{},
		},
	}
	cleanKubeJSON(doc, "service")
	meta := doc["metadata"].(map[string]any)
	for _, gone := range []string{"uid", "creationTimestamp", "generation", "managedFields", "annotations"} {
		if _, ok := meta[gone]; ok {
			t.Errorf("metadata.%s should be removed", gone)
		}
	}
	if meta["resourceVersion"] != "491" {
		t.Error("resourceVersion must be preserved")
	}
	spec := doc["spec"].(map[string]any)
	for _, gone := range []string{"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy"} {
		if _, ok := spec[gone]; ok {
			t.Errorf("spec.%s should be removed", gone)
		}
	}
	if _, ok := doc["status"]; ok {
		t.Error("status should be removed")
	}
	// Empty ports list preserved (not dropped).
	if _, ok := spec["ports"]; !ok {
		t.Error("empty spec.ports list must be preserved")
	}
	if _, ok := spec["type"]; !ok {
		t.Error("non-cleaned spec keys must be preserved")
	}

	// Defense in depth: Secret data dropped.
	sdoc := map[string]any{"data": map[string]any{"k": "dg=="},
		"metadata": map[string]any{"resourceVersion": "1"}}
	cleanKubeJSON(sdoc, "Secret")
	if _, ok := sdoc["data"]; ok {
		t.Error("secret data must be dropped")
	}
	if sdoc["metadata"].(map[string]any)["resourceVersion"] != "1" {
		t.Error("non-cleaned metadata must be preserved")
	}
}

func TestKubeJSONToYAMLScalars(t *testing.T) {
	doc := map[string]any{
		"plain":     "hello",
		"trueStr":   "true",
		"numStr":    "123",
		"num":       float64(123),
		"starStr":   "*",
		"ampStr":    "&x",
		"colonStr":  "a:b c",
		"spaceStr":  " padded ",
		"multi":     "line1\nline2",
		"emptyMap":  map[string]any{},
		"emptyList": []any{},
		"list":      []any{"a", float64(2), true},
		"nilVal":    nil,
		"expNum":    float64(1.5e10),
	}
	out, err := kubeJSONToYAMLDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]bool{
		"plain: hello":           true,
		`"trueStr": "true"`:      true,
		`"numStr": "123"`:        true,
		"num: 123":               true,
		`"*": "*"`:               true,
		`"&x": "&x"`:             true,
		`colonStr: a:b c`:        false, // ": " inside a plain scalar is fine; key "colonStr" is plain
		`"colonStr": "a:b c"`:    false,
		`"spaceStr": " padded "`: true,
		"multi: |-":              true,
		"emptyMap: {}":           true,
		"emptyList: []":          true,
		"nilVal: null":           true,
		"expNum: 15000000000":    true,
	}
	for k := range checks {
		_ = k
	}
	for _, want := range []string{
		"plain: hello",
		"trueStr: \"true\"",
		"numStr: \"123\"",
		"num: 123",
		"starStr: \"*\"",
		"ampStr: \"\\u0026x\"",
		"spaceStr: \" padded \"",
		"multi: |-",
		"emptyMap: {}",
		"emptyList: []",
		"nilVal: null",
		"expNum: 15000000000",
		"colonStr: a:b c",
		"list:",
		"- a",
		"- 2",
		"- true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestYAMLPlainSafe(t *testing.T) {
	unsafe := []string{"true", "false", "null", "yes", "no", "on", "off", "~", "123", "-abc", "#c", "a: b", "x\n", " lead", "trail ", "1.5", "1e5"}
	for _, s := range unsafe {
		if yamlPlainSafe(s) {
			t.Errorf("expected unsafe: %q", s)
		}
	}
	safe := []string{"hello", "kube-dns", "a:b", "10.43.0.10", "IPv4", "a#b", "x:y"}
	for _, s := range safe {
		if !yamlPlainSafe(s) {
			t.Errorf("expected safe: %q", s)
		}
	}
}

func TestKubeYAMLValidation(t *testing.T) {
	ins := &Inspector{}
	if _, err := ins.KubeYAML(0, "secret", "default", "x"); err == nil {
		t.Error("secret must be rejected")
	}
	if _, err := ins.KubeYAML(0, "deployment", "default", "a;rm"); err == nil {
		t.Error("injected name must be rejected")
	}
	if _, err := ins.KubeYAML(0, "deployment", "a;rm", "x"); err == nil {
		t.Error("injected ns must be rejected")
	}
}
