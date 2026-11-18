package inspector

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"pantau/internal/logger"
)

// Docker management (Portainer-style): inspect, container lifecycle, create/run, images, volumes, networks,
// Compose stacks and system maintenance. Everything runs as the SSH user through the docker CLI; when that user
// is refused by the Docker socket, passwordless `sudo -n docker` is used instead (never a password).

const (
	dockerOpTimeout     = 60 * time.Second
	dockerLongTimeout   = 10 * time.Minute // pull / run (may pull) / compose up / prune
	dockerStackFileMax  = 64 * 1024
	dockerStackDirName  = ".pantau/stacks"
	composeProjectLabel = "com.docker.compose.project"
)

// dockerSudoPrelude defines a `docker` shell function that transparently prefixes `sudo -n` when the plain
// CLI is refused by the Docker socket. Defining a function (instead of rewriting every command) keeps the
// existing docker scripts unchanged. It makes at most one sudo probe, and only when docker said "permission
// denied". It also sets DOCKER_SUDO, for programs that talk to the Docker socket themselves (the image scanner):
// a shell function is invisible to them.
const dockerSudoPrelude = `e=$(command docker ps -q 2>&1 >/dev/null)
case "$e" in
  *ermission\ denied*) if sudo -n true 2>/dev/null; then docker() { sudo -n docker "$@"; }; DOCKER_SUDO="sudo -n"; fi;;
esac
`

// dockerShell wraps a docker script so it runs under `sh` (not the login shell) with the sudo fallback.
func dockerShell(body string) string {
	return "sh -c " + shellQuote(DarwinPathPrelude+dockerSudoPrelude+body)
}

func (ins *Inspector) dockerExec(hostID int64, body string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return "", "", 0, err
	}
	defer runner.Close()
	return runner.ExecTimeout(dockerShell(body), timeout)
}

// ---------------------------------------------------------------- validation

var (
	dockerNameRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	dockerImageRe   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/@+-]{0,255}$`)
	dockerEnvKeyRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	dockerPortRe    = regexp.MustCompile(`^(([0-9]{1,3}\.){3}[0-9]{1,3}:)?[0-9]{1,5}(-[0-9]{1,5})?:[0-9]{1,5}(-[0-9]{1,5})?(/(tcp|udp|sctp))?$`)
	dockerMemRe     = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[bBkKmMgG]?$`)
	dockerCPURe     = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	dockerRestartRe = regexp.MustCompile(`^(no|always|unless-stopped|on-failure(:[0-9]{1,3})?)$`)
	dockerCIDRRe    = regexp.MustCompile(`^[0-9a-fA-F:.]+/[0-9]{1,3}$`)
	dockerIPRe      = regexp.MustCompile(`^[0-9a-fA-F:.]+$`)
	composeNameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	dockerAbsPathRe = regexp.MustCompile(`^/[^\x00-\x1f:'"\\]*$`)
)

// ErrDockerInvalid marks input rejected before anything is sent to the host.
var ErrDockerInvalid = errors.New("invalid docker request")

func dockerInvalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrDockerInvalid, fmt.Sprintf(format, a...))
}

// ValidDockerName reports whether s is a safe container / volume / network name (or ID).
func ValidDockerName(s string) bool { return dockerNameRe.MatchString(s) }

// ValidDockerImage reports whether s is a safe image reference (repo[:tag][@digest] or ID).
func ValidDockerImage(s string) bool { return dockerImageRe.MatchString(s) }

// ---------------------------------------------------------------- container lifecycle

// DockerContainerOpts carries the optional flags of container operations.
type DockerContainerOpts struct {
	Force   bool   `json:"force"`   // remove: -f (also kills a running container)
	Volumes bool   `json:"volumes"` // remove: -v (anonymous volumes)
	NewName string `json:"new_name"`
}

var dockerContainerVerbs = map[string]bool{
	"start": true, "stop": true, "restart": true, "pause": true, "unpause": true, "kill": true, "remove": true, "rename": true,
}

func dockerContainerCommand(name, action string, o DockerContainerOpts) (string, error) {
	if !dockerContainerVerbs[action] {
		return "", dockerInvalid("unsupported action: %s", action)
	}
	if !ValidDockerName(name) {
		return "", dockerInvalid("invalid container name: %s", name)
	}
	q := shellQuote(name)
	switch action {
	case "remove":
		flags := ""
		if o.Force {
			flags += " -f"
		}
		if o.Volumes {
			flags += " -v"
		}
		return "docker rm" + flags + " " + q, nil
	case "rename":
		if !ValidDockerName(o.NewName) {
			return "", dockerInvalid("invalid new name: %s", o.NewName)
		}
		return "docker rename " + q + " " + shellQuote(o.NewName), nil
	}
	return "docker " + action + " " + q, nil
}

// DockerContainerOp runs one lifecycle operation on a container and returns the CLI output.
func (ins *Inspector) DockerContainerOp(hostID int64, name, action string, o DockerContainerOpts) (string, error) {
	cmd, err := dockerContainerCommand(name, action, o)
	if err != nil {
		return "", err
	}
	return ins.dockerRun(hostID, cmd+" 2>&1", "docker "+action, name, dockerOpTimeout)
}

// dockerRun executes a mutating docker command, logs it to the activity console and turns a non-zero exit
// into an error carrying the CLI message.
func (ins *Inspector) dockerRun(hostID int64, body, what, target string, timeout time.Duration) (string, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return "", fmt.Errorf("host not found")
	}
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, body, timeout)
	if err != nil {
		logger.Warn("docker", host.Name, fmt.Sprintf("%s %s failed: %v", what, target, err))
		return "", err
	}
	if exitCode != 0 {
		e := dockerCommandError(what, stdout, stderr, exitCode)
		logger.Warn("docker", host.Name, fmt.Sprintf("%s %s failed: %s", what, target, shortEvidence(e.Error())))
		return "", e
	}
	logger.Info("docker", host.Name, fmt.Sprintf("%s %s", what, target))
	return strings.TrimSpace(stdout), nil
}

// dockerRunOrStream is dockerRun, or dockerStream when live is set.
func (ins *Inspector) dockerRunOrStream(hostID int64, body, what, target string, timeout time.Duration, live io.Writer) (string, error) {
	if live == nil {
		return ins.dockerRun(hostID, body, what, target, timeout)
	}
	return "", ins.dockerStream(hostID, body, what, target, timeout, live)
}

// dockerStream runs a docker script and copies its combined output to live while it runs. A non-zero exit or a
// timeout is returned as an error after the output has been written.
func (ins *Inspector) dockerStream(hostID int64, body, what, target string, timeout time.Duration, live io.Writer) error {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return fmt.Errorf("host not found")
	}
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return err
	}
	defer runner.Close()
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() { timedOut.Store(true); _ = runner.Close() })
	defer timer.Stop()

	err = runner.Stream(dockerShell(body), live)
	var exit interface{ ExitStatus() int }
	switch {
	case timedOut.Load():
		err = fmt.Errorf("command timed out after %v", timeout)
	case errors.As(err, &exit):
		err = fmt.Errorf("%s failed (exit %d)", what, exit.ExitStatus())
	}
	if err != nil {
		logger.Warn("docker", host.Name, fmt.Sprintf("%s %s failed: %v", what, target, err))
		return err
	}
	logger.Info("docker", host.Name, fmt.Sprintf("%s %s", what, target))
	return nil
}

// ---------------------------------------------------------------- create / run container

// DockerRunSpec describes a new container (Portainer's "Add container").
type DockerRunSpec struct {
	Image   string   `json:"image"`
	Name    string   `json:"name"`
	Ports   []string `json:"ports"`   // [ip:]host:container[/proto]
	Env     []string `json:"env"`     // KEY=VALUE
	Volumes []string `json:"volumes"` // /host/path:/container/path[:ro] or volume:/container/path[:ro]
	Restart string   `json:"restart"` // no | always | unless-stopped | on-failure[:N]
	Network string   `json:"network"`
	Command string   `json:"command"` // optional override, split like a shell would (quotes honoured)
	Memory  string   `json:"memory"`  // e.g. 512m
	CPUs    string   `json:"cpus"`    // e.g. 1.5
}

// splitCommandLine splits s into arguments honouring single quotes, double quotes and backslash escapes. No
// shell expansion happens: every returned argument is later single-quoted.
func splitCommandLine(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, dockerInvalid("unterminated quote in command")
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

func validDockerVolumeSpec(v string) bool {
	parts := strings.Split(v, ":")
	mode := ""
	switch len(parts) {
	case 2:
	case 3:
		mode = parts[2]
	default:
		return false
	}
	if mode != "" && mode != "ro" && mode != "rw" {
		return false
	}
	src, dst := parts[0], parts[1]
	if !dockerAbsPathRe.MatchString(dst) {
		return false
	}
	return dockerAbsPathRe.MatchString(src) || ValidDockerName(src)
}

// buildDockerRun validates spec and returns the `docker run -d` command line (every value single-quoted).
func buildDockerRun(spec DockerRunSpec) (string, error) {
	if !ValidDockerImage(spec.Image) {
		return "", dockerInvalid("invalid image: %q", spec.Image)
	}
	var b strings.Builder
	b.WriteString("docker run -d")
	if spec.Name != "" {
		if !ValidDockerName(spec.Name) {
			return "", dockerInvalid("invalid container name: %q", spec.Name)
		}
		b.WriteString(" --name " + shellQuote(spec.Name))
	}
	if spec.Restart != "" {
		if !dockerRestartRe.MatchString(spec.Restart) {
			return "", dockerInvalid("invalid restart policy: %q", spec.Restart)
		}
		b.WriteString(" --restart " + shellQuote(spec.Restart))
	}
	if spec.Network != "" {
		if !ValidDockerName(spec.Network) {
			return "", dockerInvalid("invalid network: %q", spec.Network)
		}
		b.WriteString(" --network " + shellQuote(spec.Network))
	}
	if spec.Memory != "" {
		if !dockerMemRe.MatchString(spec.Memory) {
			return "", dockerInvalid("invalid memory limit: %q", spec.Memory)
		}
		b.WriteString(" -m " + shellQuote(spec.Memory))
	}
	if spec.CPUs != "" {
		if !dockerCPURe.MatchString(spec.CPUs) {
			return "", dockerInvalid("invalid cpu limit: %q", spec.CPUs)
		}
		b.WriteString(" --cpus " + shellQuote(spec.CPUs))
	}
	for _, p := range spec.Ports {
		if !dockerPortRe.MatchString(p) {
			return "", dockerInvalid("invalid port mapping: %q", p)
		}
		b.WriteString(" -p " + shellQuote(p))
	}
	for _, e := range spec.Env {
		k, _, ok := strings.Cut(e, "=")
		if !ok || !dockerEnvKeyRe.MatchString(k) || strings.ContainsAny(e, "\x00\n\r") {
			return "", dockerInvalid("invalid environment variable: %q (use KEY=value)", e)
		}
		b.WriteString(" -e " + shellQuote(e))
	}
	for _, v := range spec.Volumes {
		if !validDockerVolumeSpec(v) {
			return "", dockerInvalid("invalid volume mapping: %q", v)
		}
		b.WriteString(" -v " + shellQuote(v))
	}
	b.WriteString(" " + shellQuote(spec.Image))
	if strings.TrimSpace(spec.Command) != "" {
		args, err := splitCommandLine(spec.Command)
		if err != nil {
			return "", err
		}
		for _, a := range args {
			if strings.ContainsAny(a, "\x00") {
				return "", dockerInvalid("invalid command")
			}
			b.WriteString(" " + shellQuote(a))
		}
	}
	return b.String(), nil
}

// DockerRunContainer creates and starts a container (pulling the image when it is missing) and returns its ID.
func (ins *Inspector) DockerRunContainer(hostID int64, spec DockerRunSpec) (string, error) {
	cmd, err := buildDockerRun(spec)
	if err != nil {
		return "", err
	}
	out, err := ins.dockerRun(hostID, cmd+" 2>&1", "docker run", spec.Image, dockerLongTimeout)
	if err != nil {
		return "", err
	}
	// `docker run` prints pull progress before the ID: the ID is the last line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}
