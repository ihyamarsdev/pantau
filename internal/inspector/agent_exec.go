package inspector

import (
	"fmt"
	"time"
)

// DockerLogs returns the last tail lines (1..1000, default 100) of a container's output, stdout and stderr merged.
func (ins *Inspector) DockerLogs(hostID int64, name string, tail int) (string, error) {
	if !ValidDockerName(name) {
		return "", dockerInvalid("invalid container name: %s", name)
	}
	if tail < 1 || tail > 1000 {
		tail = 100
	}
	stdout, stderr, code, err := ins.dockerExec(hostID, fmt.Sprintf("docker logs --tail %d %s 2>&1", tail, shellQuote(name)), dockerOpTimeout)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", dockerCommandError("docker logs", stdout, stderr, code)
	}
	return stdout, nil
}

// ExecShell runs one shell command as the host's SSH user. It performs no policy check: the AI agent gate
// (operator approval) lives in the web layer and every caller must pass it first.
func (ins *Inspector) ExecShell(hostID int64, cmd string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return "", "", -1, err
	}
	defer runner.Close()
	return runner.ExecTimeout("sh -c "+shellQuote(DarwinPathPrelude+cmd), timeout)
}
