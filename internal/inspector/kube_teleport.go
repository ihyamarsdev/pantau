package inspector

import (
	"errors"
	"fmt"
	"os"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/teleport"
)

// SetTeleportDataDir sets the data directory. Teleport kubeconfigs live in its teleport/ subfolder.
func (ins *Inspector) SetTeleportDataDir(dir string) {
	ins.teleportDir = dir
}

// TeleportAccess returns the isolated kubeconfig file and context a Teleport cluster runs with, for callers
// outside the package such as the port-forward manager.
func (ins *Inspector) TeleportAccess(c *store.KubeCluster) (kubeconfig, kubeContext string, err error) {
	return ins.teleportAccess(c)
}

// isTeleportTarget reports whether hostID addresses a cluster synced from a Teleport profile.
func (ins *Inspector) isTeleportTarget(hostID int64) bool {
	if !IsKubeClusterTarget(hostID) {
		return false
	}
	c, err := ins.getClusterTarget(hostID)
	return err == nil && c.SourceType == store.KubeSourceTeleport
}

// teleportAccess returns the isolated kubeconfig file and the context of a Teleport cluster. A missing file
// means this server never logged the profile in, so the error is the expired-session one that asks for Re-Login.
func (ins *Inspector) teleportAccess(c *store.KubeCluster) (kubeconfig, kubeContext string, err error) {
	if ins.teleportDir == "" {
		return "", "", errors.New("teleport data directory is not configured")
	}
	if c.KubeContextName == "" {
		return "", "", teleport.ErrContextNotProvisioned
	}
	if err := teleport.ValidateContextName(c.KubeContextName); err != nil {
		return "", "", err
	}
	path, err := teleport.KubeconfigPath(ins.teleportDir, c.TeleportProfileID)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("no isolated kubeconfig for this profile: %w", teleport.ErrTeleportSessionExpired)
		}
		return "", "", fmt.Errorf("stat isolated kubeconfig: %w", err)
	}
	return path, c.KubeContextName, nil
}

// clusterRunner returns the runner and command wrapper for an external cluster. A kubeconfig cluster runs against
// a temp copy of its kubeconfig. A Teleport cluster runs against its isolated file, and every kubectl call names
// that file and its context with --kubeconfig and --context, so switching clusters needs no shared state.
func (ins *Inspector) clusterRunner(c *store.KubeCluster) (sshrunner.Runner, func(string) string, error) {
	if c.SourceType != store.KubeSourceTeleport {
		runner, err := newLocalKubeRunner(c.Kubeconfig)
		if err != nil {
			return nil, nil, err
		}
		return runner, func(body string) string { return wrapClusterCmd(body, c.Provider) }, nil
	}
	kubeconfig, kubeContext, err := ins.teleportAccess(c)
	if err != nil {
		return nil, nil, err
	}
	runner, err := newTeleportKubeRunner(kubeconfig)
	if err != nil {
		return nil, nil, err
	}
	prelude := teleportPrelude(c.Provider, kubeconfig, kubeContext)
	return runner, func(body string) string { return wrapWithPrelude(unwrapKubeShell(body), prelude) }, nil
}

// teleportExpiredError returns the expired-session error when a failed kubectl run says the Teleport certificate
// was rejected. It returns nil for every other failure, which keeps its own message.
func teleportExpiredError(stdout, stderr string) error {
	if !teleport.IsSessionExpiredOutput(stderr + "\n" + stdout) {
		return nil
	}
	return fmt.Errorf("kube command: %w", teleport.ErrTeleportSessionExpired)
}

// newTeleportKubeRunner runs commands against an existing isolated kubeconfig. Close removes nothing: the file
// belongs to the profile, not to one command.
func newTeleportKubeRunner(kubeconfigPath string) (*localKubeRunner, error) {
	if err := ensureKubectlInstalled(); err != nil {
		return nil, err
	}
	return &localKubeRunner{kubeconfigPath: kubeconfigPath}, nil
}

// teleportPrelude defines kc() so that every kubectl call names the isolated kubeconfig and the cluster context.
// Values are shell-quoted; the context name was already validated by teleport.ValidateContextName.
func teleportPrelude(provider, kubeconfig, kubeContext string) string {
	return fmt.Sprintf("KUBE_DISTRO=%s\nkc() { kubectl --kubeconfig %s --context %s \"$@\"; }\n",
		shellQuote(provider), shellQuote(kubeconfig), shellQuote(kubeContext))
}
