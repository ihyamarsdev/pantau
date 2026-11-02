package inspector

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
)

const (
	kubeOpTimeout    = 60 * time.Second
	kubeQueryTimeout = 30 * time.Second
	kubeYAMLMax      = 64 * 1024
)

// kubePrelude defines kc() preferring k3s, then rke2, else exiting with the required error message.
// Privilege fallback: runs as SSH user; only if kubeconfig is refused with permission denied AND
// sudo -n true works, runs via sudo -n env KUBECONFIG=... (never a password).
const kubePrelude = `PATH="/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin:$PATH"
if command -v k3s >/dev/null 2>&1 && { [ -f /etc/rancher/k3s/k3s.yaml ] || (sudo -n test -f /etc/rancher/k3s/k3s.yaml 2>/dev/null); }; then
  KUBE_DISTRO="k3s"
  KUBE_BIN="$(command -v k3s) kubectl" # absolute: sudo's secure_path omits /usr/local/bin on RHEL-family hosts
  KUBE_CFG="/etc/rancher/k3s/k3s.yaml"
elif { [ -x /var/lib/rancher/rke2/bin/kubectl ] || (sudo -n test -x /var/lib/rancher/rke2/bin/kubectl 2>/dev/null); } && { [ -f /etc/rancher/rke2/rke2.yaml ] || (sudo -n test -f /etc/rancher/rke2/rke2.yaml 2>/dev/null); }; then
  KUBE_DISTRO="rke2"
  KUBE_BIN="/var/lib/rancher/rke2/bin/kubectl"
  KUBE_CFG="/etc/rancher/rke2/rke2.yaml"
else
  echo "no k3s/rke2 control plane found" >&2
  exit 1
fi
kc() {
  KUBECONFIG="$KUBE_CFG" $KUBE_BIN "$@"
}
e=$(cat "$KUBE_CFG" 2>&1 >/dev/null || true)
case "$e" in
  *ermission\ denied*)
    if sudo -n true 2>/dev/null; then
      kc() {
        sudo -n env KUBECONFIG="$KUBE_CFG" $KUBE_BIN "$@"
      }
    fi
    ;;
esac
`

// KubeShell wraps body with the distro detection prelude and sh -c invocation.
func KubeShell(body string) string {
	return "sh -c " + shellQuote(kubePrelude+body)
}

func kubeShell(body string) string {
	return KubeShell(body)
}

func (ins *Inspector) kubeRunner(hostID int64) (sshrunner.Runner, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("host not found")
	}
	return ins.GetRunnerForHost(host)
}

func (ins *Inspector) kubeExec(hostID int64, body string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	runner, err := ins.kubeRunner(hostID)
	if err != nil {
		return "", "", 0, err
	}
	defer runner.Close()
	return runner.ExecTimeout(kubeShell(body), timeout)
}

func (ins *Inspector) kubeQuery(hostID int64, body, what string, timeout time.Duration) (string, error) {
	stdout, stderr, exitCode, err := ins.kubeExec(hostID, body, timeout)
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", kubeCommandError(what, stdout, stderr, exitCode)
	}
	return stdout, nil
}

func (ins *Inspector) kubeRun(hostID int64, body, what, target string, timeout time.Duration) (string, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return "", fmt.Errorf("host not found")
	}
	stdout, stderr, exitCode, err := ins.kubeExec(hostID, body, timeout)
	if err != nil {
		logger.Warn("kube", host.Name, fmt.Sprintf("%s %s failed: %v", what, target, err))
		return "", err
	}
	if exitCode != 0 {
		e := kubeCommandError(what, stdout, stderr, exitCode)
		logger.Warn("kube", host.Name, fmt.Sprintf("%s %s failed: %s", what, target, shortEvidence(e.Error())))
		return "", e
	}
	logger.Info("kube", host.Name, fmt.Sprintf("%s %s", what, target))
	return strings.TrimSpace(stdout), nil
}

func kubeCommandError(what, stdout, stderr string, exitCode int) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = strings.TrimSpace(stdout)
	}
	if msg == "" {
		msg = fmt.Sprintf("%s failed (exit %d)", what, exitCode)
	}
	return fmt.Errorf("%s", msg)
}

// ---------------------------------------------------------------- validation

var (
	// DNS-1123 label: lowercase alphanumeric or '-', max 63 characters, start and end with alphanumeric.
	kubeNamespaceRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	// DNS-1123 subdomain: lowercase alphanumeric, '-', or '.', max 253 characters.
	kubeNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	kubeVerRe  = regexp.MustCompile(`v[0-9]+\.[0-9]+(\.[0-9]+)?(\+[a-zA-Z0-9_.-]+)?`)
)

// ErrKubeInvalid marks input rejected before anything is sent to the host.
var ErrKubeInvalid = errors.New("invalid kube request")

func kubeInvalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrKubeInvalid, fmt.Sprintf(format, a...))
}

// ValidKubeNamespace reports whether s is a valid Kubernetes namespace name.
func ValidKubeNamespace(s string) bool { return kubeNamespaceRe.MatchString(s) }

// ValidKubeName reports whether s is a safe Kubernetes resource name.
func ValidKubeName(s string) bool { return kubeNameRe.MatchString(s) }

var kubeDescribeKinds = map[string]bool{
	"pod":         true,
	"deployment":  true,
	"statefulset": true,
	"daemonset":   true,
	"service":     true,
	"node":        true,
	"ingress":     true,
	"configmap":   true,
	"cm":          true,
	"pvc":         true,
	"pv":          true,
	"job":         true,
	"cronjob":     true,
}

// ---------------------------------------------------------------- structs

type KubeClusterInfo struct {
	Distro  string `json:"distro"` // "k3s" | "rke2"
	Version string `json:"version"`
	Role    string `json:"role"` // "server"
}

type KubeNode struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	Roles         string `json:"roles"`
	Version       string `json:"version"`
	InternalIP    string `json:"internalIP"`
	CPU           string `json:"cpu"`
	Memory        string `json:"memory"`
	Created       string `json:"created"`
	Unschedulable bool   `json:"unschedulable"`
}

type KubeNamespace struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Created string `json:"created"`
}

type KubeWorkload struct {
	Kind      string   `json:"kind"` // "Deployment" | "StatefulSet" | "DaemonSet"
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Ready     int      `json:"ready"`
	Desired   int      `json:"desired"`
	Available int      `json:"available"`
	Images    []string `json:"images"`
	Created   string   `json:"created"`
}

type KubePod struct {
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	Ready      string   `json:"ready"`
	Status     string   `json:"status"`
	Restarts   int      `json:"restarts"`
	Node       string   `json:"node"`
	IP         string   `json:"ip"`
	Created    string   `json:"created"`
	Containers []string `json:"containers"`
}

type KubeService struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ClusterIP  string `json:"clusterIP"`
	ExternalIP string `json:"externalIP"`
	Ports      string `json:"ports"`
	Created    string `json:"created"`
}

type KubeEvent struct {
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Object    string `json:"object"`
	Message   string `json:"message"`
	Count     int    `json:"count"`
	Last      string `json:"last"`
}

type KubeActionReq struct {
	Op        string `json:"op"`
	Kind      string `json:"kind"`
	Namespace string `json:"ns"`
	Name      string `json:"name"`
	Replicas  *int   `json:"replicas,omitempty"`
}

// ---------------------------------------------------------------- inspector methods

func (ins *Inspector) KubeDetect(hostID int64) (*KubeClusterInfo, error) {
	script := `echo "===DISTRO==="
echo "$KUBE_DISTRO"
echo "===VERSION==="
kc version -o json 2>/dev/null || kc version 2>/dev/null || true
`
	out, err := ins.kubeQuery(hostID, script, "kube detect", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeDetect(out)
}

func (ins *Inspector) KubeNodes(hostID int64) ([]KubeNode, error) {
	out, err := ins.kubeQuery(hostID, "kc get nodes -o json", "kube nodes", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeNodes(out)
}

func (ins *Inspector) KubeNamespaces(hostID int64) ([]KubeNamespace, error) {
	out, err := ins.kubeQuery(hostID, "kc get namespaces -o json", "kube namespaces", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeNamespaces(out)
}

func (ins *Inspector) KubeWorkloads(hostID int64, ns string) ([]KubeWorkload, error) {
	var cmd string
	if ns != "" {
		if !ValidKubeNamespace(ns) {
			return nil, kubeInvalid("invalid namespace: %s", ns)
		}
		cmd = fmt.Sprintf("kc get deployments,statefulsets,daemonsets -o json -n %s", shellQuote(ns))
	} else {
		cmd = "kc get deployments,statefulsets,daemonsets -o json -A"
	}
	out, err := ins.kubeQuery(hostID, cmd, "kube workloads", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeWorkloads(out)
}

func (ins *Inspector) KubePods(hostID int64, ns string) ([]KubePod, error) {
	var cmd string
	if ns != "" {
		if !ValidKubeNamespace(ns) {
			return nil, kubeInvalid("invalid namespace: %s", ns)
		}
		cmd = fmt.Sprintf("kc get pods -o json -n %s", shellQuote(ns))
	} else {
		cmd = "kc get pods -o json -A"
	}
	out, err := ins.kubeQuery(hostID, cmd, "kube pods", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubePods(out)
}

func (ins *Inspector) KubeServices(hostID int64, ns string) ([]KubeService, error) {
	var cmd string
	if ns != "" {
		if !ValidKubeNamespace(ns) {
			return nil, kubeInvalid("invalid namespace: %s", ns)
		}
		cmd = fmt.Sprintf("kc get services -o json -n %s", shellQuote(ns))
	} else {
		cmd = "kc get services -o json -A"
	}
	out, err := ins.kubeQuery(hostID, cmd, "kube services", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeServices(out)
}

func (ins *Inspector) KubeEvents(hostID int64, ns string) ([]KubeEvent, error) {
	var cmd string
	if ns != "" {
		if !ValidKubeNamespace(ns) {
			return nil, kubeInvalid("invalid namespace: %s", ns)
		}
		cmd = fmt.Sprintf("kc get events -o json -n %s", shellQuote(ns))
	} else {
		cmd = "kc get events -o json -A"
	}
	out, err := ins.kubeQuery(hostID, cmd, "kube events", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeEvents(out)
}

func (ins *Inspector) KubeDescribe(hostID int64, kind, ns, name string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	name = strings.TrimSpace(name)
	ns = strings.TrimSpace(ns)

	if !kubeDescribeKinds[kind] {
		return "", kubeInvalid("unsupported kind: %s", kind)
	}
	if !ValidKubeName(name) {
		return "", kubeInvalid("invalid name: %s", name)
	}

	var cmd string
	if kind == "node" || kind == "pv" {
		cmd = fmt.Sprintf("kc describe %s %s", kind, shellQuote(name))
	} else {
		if !ValidKubeNamespace(ns) {
			return "", kubeInvalid("invalid namespace: %s", ns)
		}
		cmd = fmt.Sprintf("kc describe %s %s -n %s", kind, shellQuote(name), shellQuote(ns))
	}
	return ins.kubeQuery(hostID, cmd, "kube describe", kubeQueryTimeout)
}

func (ins *Inspector) KubeLogs(hostID int64, ns, pod, container string, tail int) (string, error) {
	ns = strings.TrimSpace(ns)
	pod = strings.TrimSpace(pod)
	container = strings.TrimSpace(container)

	if !ValidKubeNamespace(ns) {
		return "", kubeInvalid("invalid namespace: %s", ns)
	}
	if !ValidKubeName(pod) {
		return "", kubeInvalid("invalid pod name: %s", pod)
	}
	if container != "" && !ValidKubeName(container) {
		return "", kubeInvalid("invalid container name: %s", container)
	}
	if tail == 0 {
		tail = 200
	}
	if tail < 1 || tail > 2000 {
		return "", kubeInvalid("tail must be between 1 and 2000: got %d", tail)
	}

	var cmd string
	if container != "" {
		cmd = fmt.Sprintf("kc logs -n %s %s -c %s --tail=%d", shellQuote(ns), shellQuote(pod), shellQuote(container), tail)
	} else {
		cmd = fmt.Sprintf("kc logs -n %s %s --tail=%d", shellQuote(ns), shellQuote(pod), tail)
	}
	return ins.kubeQuery(hostID, cmd, "kube logs", kubeQueryTimeout)
}

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
	rows := make([][]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
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

func (ins *Inspector) KubeExecCmd(ns, pod, container string) (string, error) {
	ns = strings.TrimSpace(ns)
	pod = strings.TrimSpace(pod)
	container = strings.TrimSpace(container)

	if !ValidKubeNamespace(ns) {
		return "", kubeInvalid("invalid namespace: %s", ns)
	}
	if !ValidKubeName(pod) {
		return "", kubeInvalid("invalid pod name: %s", pod)
	}
	if container != "" && !ValidKubeName(container) {
		return "", kubeInvalid("invalid container name: %s", container)
	}

	shell := "command -v bash >/dev/null 2>&1 && exec bash || exec sh"
	var body string
	if container != "" {
		body = fmt.Sprintf("kc exec -it -n %s %s -c %s -- sh -c %s",
			shellQuote(ns), shellQuote(pod), shellQuote(container), shellQuote(shell))
	} else {
		body = fmt.Sprintf("kc exec -it -n %s %s -- sh -c %s",
			shellQuote(ns), shellQuote(pod), shellQuote(shell))
	}
	return kubeShell(body), nil
}

func (ins *Inspector) KubeLogStreamCmd(ns, pod, container string, tail int, previous bool) string {
	ns = strings.TrimSpace(ns)
	pod = strings.TrimSpace(pod)
	container = strings.TrimSpace(container)
	if tail < 1 || tail > 2000 {
		tail = 200
	}
	var flag string
	if previous {
		flag = fmt.Sprintf("--tail=%d --previous", tail)
	} else {
		flag = fmt.Sprintf("-f --tail=%d", tail)
	}
	var body string
	if container != "" {
		body = fmt.Sprintf("kc logs -n %s %s -c %s %s", shellQuote(ns), shellQuote(pod), shellQuote(container), flag)
	} else {
		body = fmt.Sprintf("kc logs -n %s %s %s", shellQuote(ns), shellQuote(pod), flag)
	}
	return kubeShell(body)
}

func (ins *Inspector) KubeAction(hostID int64, req KubeActionReq) (string, error) {
	cmd, target, err := kubeActionCommand(req)
	if err != nil {
		return "", err
	}
	return ins.kubeRun(hostID, cmd, "kube "+strings.ToLower(strings.TrimSpace(req.Op)), target, kubeOpTimeout)
}

func (ins *Inspector) KubeApply(hostID int64, yaml string) (string, error) {
	if err := validateKubeApply(yaml); err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(yaml))
	script := kubeApplyScript(b64)
	return ins.kubeRun(hostID, script, "kube apply", "manifest", kubeOpTimeout)
}

// ---------------------------------------------------------------- command builders

func kubeActionCommand(req KubeActionReq) (cmd, target string, err error) {
	op := strings.ToLower(strings.TrimSpace(req.Op))
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	ns := strings.TrimSpace(req.Namespace)
	name := strings.TrimSpace(req.Name)

	switch op {
	case "scale":
		if kind != "deployment" && kind != "statefulset" {
			return "", "", kubeInvalid("unsupported kind for scale: %s", kind)
		}
		if !ValidKubeNamespace(ns) {
			return "", "", kubeInvalid("invalid namespace: %s", ns)
		}
		if !ValidKubeName(name) {
			return "", "", kubeInvalid("invalid name: %s", name)
		}
		if req.Replicas == nil || *req.Replicas < 0 || *req.Replicas > 1000 {
			return "", "", kubeInvalid("replicas must be between 0 and 1000")
		}
		cmd = fmt.Sprintf("kc scale %s %s -n %s --replicas=%d", kind, shellQuote(name), shellQuote(ns), *req.Replicas)
		target = fmt.Sprintf("%s/%s", kind, name)
		return cmd, target, nil

	case "restart":
		if kind != "deployment" && kind != "statefulset" && kind != "daemonset" {
			return "", "", kubeInvalid("unsupported kind for restart: %s", kind)
		}
		if !ValidKubeNamespace(ns) {
			return "", "", kubeInvalid("invalid namespace: %s", ns)
		}
		if !ValidKubeName(name) {
			return "", "", kubeInvalid("invalid name: %s", name)
		}
		target = fmt.Sprintf("%s/%s", kind, name)
		cmd = fmt.Sprintf("kc rollout restart %s -n %s", shellQuote(target), shellQuote(ns))
		return cmd, target, nil

	case "delete":
		if kind != "pod" {
			return "", "", kubeInvalid("unsupported kind for delete: %s", kind)
		}
		if !ValidKubeNamespace(ns) {
			return "", "", kubeInvalid("invalid namespace: %s", ns)
		}
		if !ValidKubeName(name) {
			return "", "", kubeInvalid("invalid name: %s", name)
		}
		cmd = fmt.Sprintf("kc delete pod %s -n %s", shellQuote(name), shellQuote(ns))
		target = fmt.Sprintf("pod/%s", name)
		return cmd, target, nil

	case "cordon":
		if kind != "node" {
			return "", "", kubeInvalid("unsupported kind for cordon: %s", kind)
		}
		if !ValidKubeName(name) {
			return "", "", kubeInvalid("invalid node name: %s", name)
		}
		cmd = fmt.Sprintf("kc cordon %s", shellQuote(name))
		return cmd, name, nil

	case "uncordon":
		if kind != "node" {
			return "", "", kubeInvalid("unsupported kind for uncordon: %s", kind)
		}
		if !ValidKubeName(name) {
			return "", "", kubeInvalid("invalid node name: %s", name)
		}
		cmd = fmt.Sprintf("kc uncordon %s", shellQuote(name))
		return cmd, name, nil

	default:
		return "", "", kubeInvalid("unsupported action: %s", op)
	}
}

func validateKubeApply(yaml string) error {
	if strings.TrimSpace(yaml) == "" {
		return kubeInvalid("yaml manifest is empty")
	}
	if len(yaml) > kubeYAMLMax {
		return kubeInvalid("yaml manifest is larger than %d KB", kubeYAMLMax/1024)
	}
	if strings.ContainsRune(yaml, 0) {
		return kubeInvalid("yaml manifest contains a NUL byte")
	}
	return nil
}

func kubeApplyScript(b64 string) string {
	return `TMP=$(mktemp) || exit 1
printf '%s' ` + shellQuote(b64) + ` | (base64 -d 2>/dev/null || base64 -D) > "$TMP" || { rm -f "$TMP"; exit 1; }
if ! out=$(kc apply --dry-run=server -f "$TMP" 2>&1); then
  echo "Dry-run failed:"
  echo "$out"
  rm -f "$TMP"
  exit 1
fi
kc apply -f "$TMP" 2>&1
rc=$?
rm -f "$TMP"
exit $rc
`
}

// ---------------------------------------------------------------- JSON parsers

func parseKubeDetect(out string) (*KubeClusterInfo, error) {
	sections := splitDockerSections(out)
	distro := strings.TrimSpace(sections["===DISTRO==="])
	if distro == "" {
		distro = strings.TrimSpace(sections["DISTRO"])
	}
	if distro != "k3s" && distro != "rke2" {
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
	return &KubeClusterInfo{
		Distro:  distro,
		Version: version,
		Role:    "server",
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
	var list rawNodeList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
		return nil, fmt.Errorf("parse nodes: %w", err)
	}
	nodes := make([]KubeNode, 0, len(list.Items))
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
	}
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
	var list rawPodList
	if err := json.Unmarshal([]byte(jsonStr), &list); err != nil {
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
