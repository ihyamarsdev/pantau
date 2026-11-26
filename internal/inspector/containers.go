package inspector

// Container listing: the containers of a host and the listing kept for the
// fleet search.

import (
	"fmt"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// DockerContainer is a live snapshot of one container (running or not) on a host.
type DockerContainer struct {
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	State          string            `json:"state"`  // running, paused, restarting, exited, created, dead
	Status         string            `json:"status"` // raw `docker ps` status, e.g. "Up 3 hours (healthy)"
	Ports          string            `json:"ports"`
	ComposeProject string            `json:"compose_project,omitempty"`
	ComposeService string            `json:"compose_service,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// ListDockerContainers lists all containers (including stopped ones) via `docker ps -a`.
// The state is derived from the Status column so it works on legacy Docker versions without {{.State}}.
func (ins *Inspector) ListDockerContainers(hostID int64) ([]DockerContainer, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("host not found")
	}
	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	stdout, stderr, exitCode, err := runner.Exec(dockerShell(dockerPSCommand))
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = fmt.Sprintf("docker ps failed (exit %d)", exitCode)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	containers := parseDockerPS(stdout)
	ins.saveHostContainers(host, containers)
	return containers, nil
}

// dockerPSCommand lists every container (running or not) with its image, status, ports, and Compose labels.
const dockerPSCommand = `docker ps -a --no-trunc --format '{{.Names}}|{{.Image}}|{{.Status}}|{{.Ports}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}'`

// dockerNotInstalled is printed by the inspection probe when docker is absent.
const dockerNotInstalled = "__PANTAU_NO_DOCKER__"

// dockerProbeCommand lists the containers and reports a missing docker instead
// of failing, so the inspection can tell "no docker" from "docker is broken".
const dockerProbeCommand = `command -v docker >/dev/null 2>&1 || { echo ` + dockerNotInstalled + `; exit 0; }
` + dockerPSCommand

// saveHostContainers stores the container listing of a host for the fleet search.
func (ins *Inspector) saveHostContainers(host *store.Host, cs []DockerContainer) {
	rows := make([]store.HostContainer, 0, len(cs))
	now := time.Now()
	for _, c := range cs {
		rows = append(rows, store.HostContainer{HostID: host.ID, Name: c.Name, Image: c.Image, State: c.State, SeenAt: now})
	}
	if err := ins.db.ReplaceHostContainers(host.ID, rows); err != nil {
		logger.Warn("inspect", host.Name, "store containers failed: "+err.Error())
	}
}

// collectHostContainers refreshes the stored container listing of a host.
// It never fails the inspection: on a docker error the stored rows stay untouched.
func (ins *Inspector) collectHostContainers(host *store.Host, runner sshrunner.Runner, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	stdout, stderr, exitCode, err := runner.ExecTimeout(dockerShell(dockerProbeCommand), timeout)
	if err != nil {
		logger.Debug("inspect", host.Name, "container listing skipped: "+err.Error())
		return
	}
	if exitCode != 0 {
		logger.Debug("inspect", host.Name, "docker ps failed, keeping stored containers: "+strings.TrimSpace(stderr))
		return
	}
	if strings.Contains(stdout, dockerNotInstalled) {
		ins.saveHostContainers(host, nil)
		return
	}
	ins.saveHostContainers(host, parseDockerPS(stdout))
}

// dockerProbeTimeout caps the container listing of one inspection.
const dockerProbeTimeout = 20 * time.Second

func parseDockerPS(out string) []DockerContainer {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	containers := make([]DockerContainer, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "|", 6)
		if len(f) < 3 {
			continue
		}
		c := DockerContainer{Name: f[0], Image: f[1], Status: f[2], State: dockerStateFromStatus(f[2])}
		if len(f) >= 4 {
			c.Ports = f[3]
		}
		if len(f) >= 5 {
			c.ComposeProject = strings.TrimSpace(f[4])
		}
		if len(f) >= 6 {
			c.ComposeService = strings.TrimSpace(f[5])
		}
		if c.ComposeProject != "" || c.ComposeService != "" {
			c.Labels = make(map[string]string, 2)
			if c.ComposeProject != "" {
				c.Labels["com.docker.compose.project"] = c.ComposeProject
			}
			if c.ComposeService != "" {
				c.Labels["com.docker.compose.service"] = c.ComposeService
			}
		}
		containers = append(containers, c)
	}
	return containers
}

func dockerStateFromStatus(status string) string {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "(paused)"):
		return "paused"
	case strings.HasPrefix(s, "up"):
		return "running"
	case strings.HasPrefix(s, "restarting"):
		return "restarting"
	case strings.HasPrefix(s, "exited"):
		return "exited"
	case strings.HasPrefix(s, "created"):
		return "created"
	case strings.HasPrefix(s, "dead"):
		return "dead"
	}
	return "unknown"
}
