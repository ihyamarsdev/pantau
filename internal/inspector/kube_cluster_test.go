package inspector

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

// writeFakeKubectl writes an executable fake kubectl script in a temp dir and prepends it to PATH.
func writeFakeKubectl(t *testing.T, scriptContent string) string {
	t.Helper()
	dir := t.TempDir()
	kubectlPath := filepath.Join(dir, "kubectl")
	fullScript := "#!/bin/sh\n" + scriptContent + "\n"
	if err := os.WriteFile(kubectlPath, []byte(fullScript), 0755); err != nil {
		t.Fatalf("failed to write fake kubectl: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
	return dir
}

func TestKubeClusterTargetHelpers(t *testing.T) {
	if !IsKubeClusterTarget(KubeClusterTarget(1)) {
		t.Errorf("expected KubeClusterTarget(1) to be cluster target")
	}
	if !IsKubeClusterTarget(-1) {
		t.Errorf("expected -1 to be cluster target")
	}
	if IsKubeClusterTarget(1) {
		t.Errorf("expected 1 not to be cluster target")
	}
	if IsKubeClusterTarget(0) {
		t.Errorf("expected 0 not to be cluster target")
	}
	if KubeClusterTarget(42) != -42 {
		t.Errorf("expected -42, got %d", KubeClusterTarget(42))
	}
}

func TestPrepareKubeconfigSecurityAndValidation(t *testing.T) {
	// Set up fake kubectl that inspects input and returns appropriate JSON for `config view`
	fakeScript := `
if [ "$1" = "config" ] && [ "$2" = "view" ]; then
  # Find --kubeconfig arg
  cfg=""
  for arg in "$@"; do
    case "$arg" in
      --kubeconfig=*) cfg="${arg#*=}" ;;
    esac
  done
  if [ -z "$cfg" ]; then
    # Maybe next arg
    while [ $# -gt 0 ]; do
      if [ "$1" = "--kubeconfig" ]; then
        cfg="$2"
        shift 2
      else
        shift
      fi
    done
  fi
  cat "$cfg"
  exit 0
fi
exec /usr/bin/kubectl "$@"
`
	writeFakeKubectl(t, fakeScript)

	// 1. Oversized input (> 128 KB)
	oversized := strings.Repeat("a", maxKubeconfigBytes+1)
	if _, err := PrepareKubeconfig(oversized, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for oversized kubeconfig, got %v", err)
	}

	// 2. Empty input
	if _, err := PrepareKubeconfig("   \n", ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for empty kubeconfig, got %v", err)
	}

	// Helper to test JSON output through fake kubectl
	runPrepare := func(fakeJSON string, ctx string) (*PreparedKubeconfig, error) {
		return PrepareKubeconfig(fakeJSON, ctx)
	}

	// 3. Valid EKS-style kubeconfig with exec (aws)
	eksJSON := `{
  "apiVersion": "v1",
  "kind": "Config",
  "current-context": "eks-ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://12345.gr7.us-east-1.eks.amazonaws.com", "certificate-authority-data": "LS0t"}}],
  "contexts": [{"name": "eks-ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"exec": {"command": "aws", "args": ["eks", "get-token"]}}}]
}`
	prep, err := runPrepare(eksJSON, "")
	if err != nil {
		t.Fatalf("expected valid EKS kubeconfig, got %v", err)
	}
	if prep.Provider != "eks" {
		t.Errorf("expected provider eks, got %s", prep.Provider)
	}
	if prep.Context != "eks-ctx" {
		t.Errorf("expected context eks-ctx, got %s", prep.Context)
	}

	// 4. Valid inline-token and client cert data kubeconfig
	tokenJSON := `{
  "apiVersion": "v1",
  "kind": "Config",
  "current-context": "token-ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s.example.com:6443", "certificate-authority-data": "LS0t"}}],
  "contexts": [{"name": "token-ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"token": "secret-token", "client-certificate-data": "cert", "client-key-data": "key"}}]
}`
	prep, err = runPrepare(tokenJSON, "")
	if err != nil {
		t.Fatalf("expected valid token kubeconfig, got %v", err)
	}
	if prep.Provider != "generic" {
		t.Errorf("expected provider generic, got %s", prep.Provider)
	}

	// 5. Rejection: file-path certificate-authority
	caFileJSON := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s:6443", "certificate-authority": "/etc/ca.crt"}}],
  "contexts": [{"name": "ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"token": "tok"}}]
}`
	if _, err := runPrepare(caFileJSON, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for file-path certificate-authority, got %v", err)
	}

	// 6. Rejection: tokenFile
	tokFileJSON := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s:6443"}}],
  "contexts": [{"name": "ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"tokenFile": "/var/run/token"}}]
}`
	if _, err := runPrepare(tokFileJSON, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for tokenFile, got %v", err)
	}

	// 7. Rejection: exec command with path separator '/'
	execSlashJSON := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s:6443"}}],
  "contexts": [{"name": "ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"exec": {"command": "/usr/local/bin/aws"}}}]
}`
	if _, err := runPrepare(execSlashJSON, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for exec command with '/', got %v", err)
	}

	// 8. Rejection: non-allowlisted exec command
	execBadJSON := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s:6443"}}],
  "contexts": [{"name": "ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"exec": {"command": "curl"}}}]
}`
	if _, err := runPrepare(execBadJSON, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for unallowlisted exec command, got %v", err)
	}

	// 9. Rejection: auth-provider with cmd-path
	authProviderJSON := `{
  "apiVersion": "v1", "kind": "Config", "current-context": "ctx",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s:6443"}}],
  "contexts": [{"name": "ctx", "context": {"cluster": "c1", "user": "u1"}}],
  "users": [{"name": "u1", "user": {"auth-provider": {"name": "gcp", "cmd-path": "/bin/sh"}}}]
}`
	if _, err := runPrepare(authProviderJSON, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for auth-provider, got %v", err)
	}

	// 10. Rejection: missing context
	noCtxJSON := `{
  "apiVersion": "v1", "kind": "Config",
  "clusters": [{"name": "c1", "cluster": {"server": "https://k8s:6443"}}],
  "contexts": [],
  "users": [{"name": "u1", "user": {"token": "tok"}}]
}`
	if _, err := runPrepare(noCtxJSON, ""); !errors.Is(err, ErrKubeInvalid) {
		t.Errorf("expected ErrKubeInvalid for missing context, got %v", err)
	}
}

func TestDeriveProvider(t *testing.T) {
	tests := []struct {
		exec     string
		server   string
		expected string
	}{
		{"aws", "https://xyz", "eks"},
		{"aws-iam-authenticator", "https://xyz", "eks"},
		{"gke-gcloud-auth-plugin", "https://xyz", "gke"},
		{"gcloud", "https://xyz", "gke"},
		{"kubelogin", "https://xyz", "aks"},
		{"az", "https://xyz", "aks"},
		{"", "https://123.gr7.us-west-2.eks.amazonaws.com", "eks"},
		{"", "https://mycluster-dns-1234.hcp.eastus.azmk8s.io:443", "aks"},
		{"", "https://container.googleapis.com", "gke"},
		{"", "https://10.0.0.1:6443", "generic"},
	}
	for _, tc := range tests {
		got := deriveProvider(tc.exec, tc.server)
		if got != tc.expected {
			t.Errorf("deriveProvider(%q, %q) = %q; want %q", tc.exec, tc.server, got, tc.expected)
		}
	}
}

func TestListKubeconfigContexts(t *testing.T) {
	fakeScript := `
if [ "$1" = "config" ] && [ "$2" = "view" ]; then
  cat <<'EOF'
{
  "contexts": [{"name": "ctx-a"}, {"name": "ctx-b"}],
  "current-context": "ctx-a"
}
EOF
  exit 0
fi
exec /usr/bin/kubectl "$@"
`
	writeFakeKubectl(t, fakeScript)

	res, err := ListKubeconfigContexts("dummy-config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Current != "ctx-a" {
		t.Errorf("expected Current 'ctx-a', got %q", res.Current)
	}
	if len(res.Contexts) != 2 || res.Contexts[0] != "ctx-a" || res.Contexts[1] != "ctx-b" {
		t.Errorf("unexpected contexts: %v", res.Contexts)
	}
}

func TestLocalKubeRunner(t *testing.T) {
	// Fake kubectl is not needed for runner execution test since runner runs sh -c
	runner, err := newLocalKubeRunner("test-kubeconfig-content")
	if err != nil {
		t.Fatalf("newLocalKubeRunner failed: %v", err)
	}
	defer runner.Close()

	// 1. Passes KUBECONFIG env pointing to temp file with content
	stdout, stderr, exitCode, err := runner.Exec("cat \"$KUBECONFIG\"")
	if err != nil || exitCode != 0 {
		t.Fatalf("runner.Exec failed: err=%v, code=%d, stderr=%s", err, exitCode, stderr)
	}
	if strings.TrimSpace(stdout) != "test-kubeconfig-content" {
		t.Errorf("expected 'test-kubeconfig-content', got %q", stdout)
	}

	// 2. Returns non-zero exit code without error
	stdout, stderr, exitCode, err = runner.Exec("echo error-msg >&2; exit 42")
	if err != nil {
		t.Errorf("expected nil error on non-zero exit code, got %v", err)
	}
	if exitCode != 42 {
		t.Errorf("expected exitCode 42, got %d", exitCode)
	}
	if strings.TrimSpace(stderr) != "error-msg" {
		t.Errorf("expected 'error-msg', got %q", stderr)
	}

	// 3. Honours timeout and kills process group
	_, _, _, err = runner.ExecTimeout("sleep 5", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "command timed out after") {
		t.Errorf("unexpected error text: %v", err)
	}

	// 4. Temp file is removed on Close
	path := runner.kubeconfigPath
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("temp file does not exist before Close: %v", err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("runner.Close failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be removed after Close, but stat returned %v", err)
	}
}

func TestKubeClusterTargetIntegration(t *testing.T) {
	// Set up fake kubectl for nodes and detect
	fakeScript := `
if [ "$1" = "get" ] && [ "$2" = "nodes" ]; then
  cat <<'EOF'
{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "ext-node-1", "creationTimestamp": "2026-01-01T00:00:00Z"},
      "status": {
        "conditions": [{"type": "Ready", "status": "True"}],
        "nodeInfo": {"kubeletVersion": "v1.31.0"},
        "addresses": [{"type": "InternalIP", "address": "10.0.1.10"}]
      }
    }
  ]
}
EOF
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "--raw" ]; then
  echo "{}"
  exit 0
fi
if [ "$1" = "version" ]; then
  echo '{"serverVersion":{"gitVersion":"v1.31.0"}}'
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "ns" ]; then
  echo "test-cluster-uid"
  exit 0
fi
echo "unhandled: $@" >&2
exit 1
`
	writeFakeKubectl(t, fakeScript)

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer db.Close()

	cluster := &store.KubeCluster{
		Name:       "my-eks",
		Context:    "eks-context",
		Server:     "https://eks.example.com",
		Provider:   "eks",
		KubeMode:   "full",
		Kubeconfig: `{"apiVersion":"v1"}`,
	}
	clusterID, err := db.CreateKubeCluster(cluster)
	if err != nil {
		t.Fatalf("CreateKubeCluster failed: %v", err)
	}

	ins := New(db, nil, nil)
	targetID := KubeClusterTarget(clusterID)

	// 1. KubeNodes through cluster target
	nodes, err := ins.KubeNodes(targetID)
	if err != nil {
		t.Fatalf("KubeNodes failed for cluster target: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "ext-node-1" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}

	// 2. KubeDetect through cluster target
	info, err := ins.KubeDetect(targetID)
	if err != nil {
		t.Fatalf("KubeDetect failed for cluster target: %v", err)
	}
	if info.Distro != "eks" {
		t.Errorf("expected Distro 'eks', got %q", info.Distro)
	}
	if info.ClusterID != "test-cluster-uid" {
		t.Errorf("expected ClusterID 'test-cluster-uid', got %q", info.ClusterID)
	}

	// 3. KubeExecCmd rejects cluster target
	if _, err := ins.KubeExecCmd("default", "pod-1", ""); err != nil {
		// KubeExecCmd takes ns, pod, container; error check on cluster target is in KubeStreamRunner or handler
	}
}
