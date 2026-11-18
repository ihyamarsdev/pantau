package inspector

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
)

const (
	kubeOpTimeout    = 60 * time.Second
	kubeQueryTimeout = 30 * time.Second
	kubeYAMLMax      = 64 * 1024
	kubeDrainTimeout = 310 * time.Second // max drain 300s + SSH/awk margin
	// maxKubePodsPayload bounds cluster-wide `get pods -o json` memory like the 2 MB metrics guards.
	maxKubePodsPayload = 32 * 1024 * 1024
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
func (ins *Inspector) kubeRunner(targetID int64) (sshrunner.Runner, error) {
	if IsKubeClusterTarget(targetID) {
		cluster, err := ins.getClusterTarget(targetID)
		if err != nil {
			return nil, err
		}
		return newLocalKubeRunner(cluster.Kubeconfig)
	}
	host, err := ins.db.GetHost(targetID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("host not found")
	}
	return ins.GetRunnerForHost(host)
}

// CheckKubeConnection verifies SSH connectivity to hostID without running commands.
func (ins *Inspector) CheckKubeConnection(hostID int64) error {
	runner, err := ins.kubeRunner(hostID)
	if err != nil {
		return err
	}
	return runner.Close()
}

func (ins *Inspector) kubeExec(hostID int64, body string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	runner, wrap, err := ins.KubeStreamRunner(hostID)
	if err != nil {
		return "", "", 0, err
	}
	defer runner.Close()
	return runner.ExecTimeout(wrap(body), timeout)
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
	targetName := "cluster"
	if IsKubeClusterTarget(hostID) {
		cluster, err := ins.getClusterTarget(hostID)
		if err != nil {
			return "", err
		}
		targetName = cluster.Name
	} else {
		host, err := ins.db.GetHost(hostID)
		if err != nil || host == nil {
			return "", fmt.Errorf("host not found")
		}
		targetName = host.Name
	}
	stdout, stderr, exitCode, err := ins.kubeExec(hostID, body, timeout)
	if err != nil {
		logger.Warn("kube", targetName, fmt.Sprintf("%s %s failed: %v", what, target, err))
		return "", err
	}
	if exitCode != 0 {
		e := kubeCommandError(what, stdout, stderr, exitCode)
		logger.Warn("kube", targetName, fmt.Sprintf("%s %s failed: %s", what, target, shortEvidence(e.Error())))
		return "", e
	}
	logger.Info("kube", targetName, fmt.Sprintf("%s %s", what, target))
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
	Distro    string `json:"distro"` // "k3s" | "rke2"
	Version   string `json:"version"`
	Role      string `json:"role"` // "server"
	ClusterID string `json:"cluster_id"`
}

type KubeNode struct {
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	Roles          string   `json:"roles"`
	Version        string   `json:"version"`
	InternalIP     string   `json:"internalIP"`
	CPU            string   `json:"cpu"`
	Memory         string   `json:"memory"`
	Created        string   `json:"created"`
	Unschedulable  bool     `json:"unschedulable"`
	UsageCPU       string   `json:"usageCPU,omitempty"`
	UsageMemory    string   `json:"usageMemory,omitempty"`
	CPUPercent     *float64 `json:"cpuPercent,omitempty"`
	MemoryPercent  *float64 `json:"memoryPercent,omitempty"`
	AllocatableCPU string   `json:"allocatableCPU,omitempty"`
	AllocatableMem string   `json:"allocatableMem,omitempty"`
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
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	Ready       string   `json:"ready"`
	Status      string   `json:"status"`
	Restarts    int      `json:"restarts"`
	Node        string   `json:"node"`
	IP          string   `json:"ip"`
	Created     string   `json:"created"`
	Containers  []string `json:"containers"`
	UsageCPU    string   `json:"usageCPU,omitempty"`
	UsageMemory string   `json:"usageMemory,omitempty"`
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
	// Drain flags (op == "drain").
	IgnoreDaemonsets   *bool `json:"ignoreDaemonsets,omitempty"`
	DeleteEmptyDirData *bool `json:"deleteEmptyDirData,omitempty"`
	Force              *bool `json:"force,omitempty"`
	TimeoutSeconds     *int  `json:"timeoutSeconds,omitempty"`
}

func (req KubeActionReq) drainIgnoreDaemonsets() bool {
	return req.IgnoreDaemonsets == nil || *req.IgnoreDaemonsets
}
func (req KubeActionReq) drainDeleteEmptyDirData() bool {
	return req.DeleteEmptyDirData != nil && *req.DeleteEmptyDirData
}
func (req KubeActionReq) drainForce() bool { return req.Force != nil && *req.Force }
func (req KubeActionReq) drainTimeoutSeconds() int {
	if req.TimeoutSeconds == nil || *req.TimeoutSeconds < 1 {
		return 120
	}
	if *req.TimeoutSeconds > 300 {
		return 300
	}
	return *req.TimeoutSeconds
}

// ---------------------------------------------------------------- inspector methods

func (ins *Inspector) KubeDetect(hostID int64) (*KubeClusterInfo, error) {
	script := `echo "===DISTRO==="
echo "$KUBE_DISTRO"
echo "===VERSION==="
kc version -o json 2>/dev/null || kc version 2>/dev/null || true
echo "===CLUSTER_ID==="
kc get ns kube-system -o jsonpath='{.metadata.uid}' 2>/dev/null || true
`
	out, err := ins.kubeQuery(hostID, script, "kube detect", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	return parseKubeDetect(out)
}

func (ins *Inspector) KubeNodes(hostID int64) ([]KubeNode, error) {
	script := `echo "===NODES==="
kc get nodes -o json
echo "===METRICS==="
kc get --raw /apis/metrics.k8s.io/v1beta1/nodes 2>/dev/null || true
`
	out, err := ins.kubeQuery(hostID, script, "kube nodes", kubeQueryTimeout)
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
	var getCmd string
	var metricsEndpoint string
	if ns != "" {
		if !ValidKubeNamespace(ns) {
			return nil, kubeInvalid("invalid namespace: %s", ns)
		}
		getCmd = fmt.Sprintf("kc get pods -o json -n %s", shellQuote(ns))
		metricsEndpoint = fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods", ns)
	} else {
		getCmd = "kc get pods -o json -A"
		metricsEndpoint = "/apis/metrics.k8s.io/v1beta1/pods"
	}
	script := fmt.Sprintf(`echo "===PODS==="
%s
echo "===METRICS==="
kc get --raw %s 2>/dev/null || true
`, getCmd, shellQuote(metricsEndpoint))
	out, err := ins.kubeQuery(hostID, script, "kube pods", kubeQueryTimeout)
	if err != nil {
		return nil, err
	}
	if len(out) > maxKubePodsPayload {
		return nil, fmt.Errorf("pod list output exceeds %d MB; narrow the namespace", maxKubePodsPayload/(1024*1024))
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

func (ins *Inspector) KubeLogStreamCmd(ns, pod, container string, tail int, previous, timestamps bool) string {
	ns = strings.TrimSpace(ns)
	pod = strings.TrimSpace(pod)
	container = strings.TrimSpace(container)
	if tail < 1 || tail > 2000 {
		tail = 200
	}
	flags := make([]string, 0, 3)
	if previous {
		flags = append(flags, "--previous")
	} else {
		flags = append(flags, "-f")
	}
	flags = append(flags, fmt.Sprintf("--tail=%d", tail))
	if timestamps {
		flags = append(flags, "--timestamps")
	}
	flag := strings.Join(flags, " ")
	var body string
	if container != "" {
		body = fmt.Sprintf("kc logs -n %s %s -c %s %s", shellQuote(ns), shellQuote(pod), shellQuote(container), flag)
	} else {
		body = fmt.Sprintf("kc logs -n %s %s %s", shellQuote(ns), shellQuote(pod), flag)
	}
	return kubeShell(body)
}

// KubeWorkloadLogStreamCmd builds the follow command for a workload's pods.
// Uses --all-pods (kubectl >= 1.36) so every pod of the workload streams with
// a [pod/name/container] prefix. previous is rejected for workloads: kubectl
// cannot follow previous logs of multiple pods.
func (ins *Inspector) KubeWorkloadLogStreamCmd(kind, name, ns string, tail int, timestamps bool) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "deployment", "statefulset", "daemonset":
	default:
		return "", kubeInvalid("unsupported workload kind: %s", kind)
	}
	if !ValidKubeName(name) {
		return "", kubeInvalid("invalid workload name: %s", name)
	}
	if !ValidKubeNamespace(ns) {
		return "", kubeInvalid("invalid namespace: %s", ns)
	}
	if tail < 1 || tail > 2000 {
		tail = 200
	}
	body := fmt.Sprintf("kc logs %s/%s -n %s --all-pods --prefix --all-containers -f --tail=%d --max-log-requests=20",
		kind, shellQuote(name), shellQuote(ns), tail)
	if timestamps {
		body += " --timestamps"
	}
	return kubeShell(body), nil
}

func (ins *Inspector) KubeAction(hostID int64, req KubeActionReq) (string, error) {
	cmd, target, err := kubeActionCommand(req)
	if err != nil {
		return "", err
	}
	timeout := kubeOpTimeout
	if strings.EqualFold(strings.TrimSpace(req.Op), "drain") {
		if _, loaded := ins.drainInFlight.LoadOrStore(hostID, struct{}{}); loaded {
			return "", kubeInvalid("a drain is already running on this host")
		}
		defer ins.drainInFlight.Delete(hostID)
		timeout = kubeDrainTimeout
	}
	return ins.kubeRun(hostID, cmd, "kube "+strings.ToLower(strings.TrimSpace(req.Op)), target, timeout)
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

	case "drain":
		if kind != "node" {
			return "", "", kubeInvalid("unsupported kind for drain: %s", kind)
		}
		if !ValidKubeName(name) {
			return "", "", kubeInvalid("invalid node name: %s", name)
		}
		cmd = fmt.Sprintf("kc drain %s", shellQuote(name))
		if req.drainIgnoreDaemonsets() {
			cmd += " --ignore-daemonsets"
		}
		if req.drainDeleteEmptyDirData() {
			cmd += " --delete-emptydir-data"
		}
		if req.drainForce() {
			cmd += " --force"
		}
		cmd += fmt.Sprintf(" --timeout=%ds 2>&1", req.drainTimeoutSeconds())
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

// ValidateKubeAction checks action parameters before attempting any connection.
func ValidateKubeAction(req KubeActionReq) error {
	_, _, err := kubeActionCommand(req)
	return err
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

// ValidateKubeApply checks manifest constraints before attempting any connection.
func ValidateKubeApply(yaml string) error {
	return validateKubeApply(yaml)
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
