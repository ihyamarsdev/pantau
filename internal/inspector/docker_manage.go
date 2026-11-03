package inspector

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
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
// denied".
const dockerSudoPrelude = `e=$(command docker ps -q 2>&1 >/dev/null)
case "$e" in
  *ermission\ denied*) if sudo -n true 2>/dev/null; then docker() { sudo -n docker "$@"; }; fi;;
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

// ---------------------------------------------------------------- inspect

// DockerPortBinding is one published (or exposed) container port.
type DockerPortBinding struct {
	ContainerPort string `json:"container_port"` // 80/tcp
	HostIP        string `json:"host_ip"`
	HostPort      string `json:"host_port"` // empty when only exposed
}

// DockerMount is one volume or bind mount of a container.
type DockerMount struct {
	Type        string `json:"type"` // volume, bind, tmpfs
	Name        string `json:"name"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadWrite   bool   `json:"rw"`
}

// DockerContainerNetwork is a network a container is attached to.
type DockerContainerNetwork struct {
	Name    string   `json:"name"`
	IP      string   `json:"ip"`
	Gateway string   `json:"gateway"`
	MAC     string   `json:"mac"`
	Aliases []string `json:"aliases"`
}

// DockerContainerDetail is the trimmed `docker inspect` view of one container.
type DockerContainerDetail struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Image         string                   `json:"image"`
	ImageID       string                   `json:"image_id"`
	Created       string                   `json:"created"`
	Status        string                   `json:"status"`
	Running       bool                     `json:"running"`
	Paused        bool                     `json:"paused"`
	Restarting    bool                     `json:"restarting"`
	OOMKilled     bool                     `json:"oom_killed"`
	ExitCode      int                      `json:"exit_code"`
	Error         string                   `json:"error"`
	StartedAt     string                   `json:"started_at"`
	FinishedAt    string                   `json:"finished_at"`
	RestartCount  int                      `json:"restart_count"`
	Health        string                   `json:"health"`
	HealthLog     string                   `json:"health_log"`
	Command       string                   `json:"command"`
	Entrypoint    string                   `json:"entrypoint"`
	WorkingDir    string                   `json:"working_dir"`
	User          string                   `json:"user"`
	Hostname      string                   `json:"hostname"`
	Env           []string                 `json:"env"`
	Labels        map[string]string        `json:"labels"`
	RestartPolicy string                   `json:"restart_policy"`
	MemoryLimit   int64                    `json:"memory_limit"`
	CPUs          float64                  `json:"cpus"`
	Privileged    bool                     `json:"privileged"`
	NetworkMode   string                   `json:"network_mode"`
	Ports         []DockerPortBinding      `json:"ports"`
	Mounts        []DockerMount            `json:"mounts"`
	Networks      []DockerContainerNetwork `json:"networks"`
}

type dockerPortMap map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

func parseDockerInspect(out string) (*DockerContainerDetail, error) {
	out = strings.TrimSpace(out)
	if i := strings.Index(out, "["); i > 0 {
		out = out[i:] // skip warnings printed before the JSON
	}
	var raw []struct {
		ID           string `json:"Id"`
		Name         string `json:"Name"`
		Created      string `json:"Created"`
		Image        string `json:"Image"`
		RestartCount int    `json:"RestartCount"`
		State        struct {
			Status     string `json:"Status"`
			Running    bool   `json:"Running"`
			Paused     bool   `json:"Paused"`
			Restarting bool   `json:"Restarting"`
			OOMKilled  bool   `json:"OOMKilled"`
			ExitCode   int    `json:"ExitCode"`
			Error      string `json:"Error"`
			StartedAt  string `json:"StartedAt"`
			FinishedAt string `json:"FinishedAt"`
			Health     *struct {
				Status string `json:"Status"`
				Log    []struct {
					Output string `json:"Output"`
				} `json:"Log"`
			} `json:"Health"`
		} `json:"State"`
		Config struct {
			Hostname   string            `json:"Hostname"`
			User       string            `json:"User"`
			WorkingDir string            `json:"WorkingDir"`
			Image      string            `json:"Image"`
			Cmd        []string          `json:"Cmd"`
			Entrypoint []string          `json:"Entrypoint"`
			Env        []string          `json:"Env"`
			Labels     map[string]string `json:"Labels"`
		} `json:"Config"`
		HostConfig struct {
			RestartPolicy struct {
				Name              string `json:"Name"`
				MaximumRetryCount int    `json:"MaximumRetryCount"`
			} `json:"RestartPolicy"`
			Memory       int64         `json:"Memory"`
			NanoCPUs     int64         `json:"NanoCpus"`
			Privileged   bool          `json:"Privileged"`
			NetworkMode  string        `json:"NetworkMode"`
			PortBindings dockerPortMap `json:"PortBindings"`
		} `json:"HostConfig"`
		Mounts []struct {
			Type        string `json:"Type"`
			Name        string `json:"Name"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		} `json:"Mounts"`
		NetworkSettings struct {
			Ports    dockerPortMap `json:"Ports"`
			Networks map[string]struct {
				IPAddress  string   `json:"IPAddress"`
				Gateway    string   `json:"Gateway"`
				MacAddress string   `json:"MacAddress"`
				Aliases    []string `json:"Aliases"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("could not read docker inspect output")
	}
	r := raw[0]
	d := &DockerContainerDetail{
		ID: r.ID, Name: strings.TrimPrefix(r.Name, "/"), Image: r.Config.Image, ImageID: r.Image, Created: r.Created,
		Status: r.State.Status, Running: r.State.Running, Paused: r.State.Paused, Restarting: r.State.Restarting,
		OOMKilled: r.State.OOMKilled, ExitCode: r.State.ExitCode, Error: r.State.Error,
		StartedAt: r.State.StartedAt, FinishedAt: r.State.FinishedAt, RestartCount: r.RestartCount,
		Command: strings.Join(r.Config.Cmd, " "), Entrypoint: strings.Join(r.Config.Entrypoint, " "),
		WorkingDir: r.Config.WorkingDir, User: r.Config.User, Hostname: r.Config.Hostname,
		Env: r.Config.Env, Labels: r.Config.Labels, MemoryLimit: r.HostConfig.Memory,
		CPUs: float64(r.HostConfig.NanoCPUs) / 1e9, Privileged: r.HostConfig.Privileged, NetworkMode: r.HostConfig.NetworkMode,
		Ports: []DockerPortBinding{}, Mounts: []DockerMount{}, Networks: []DockerContainerNetwork{},
	}
	if d.Env == nil {
		d.Env = []string{}
	}
	if d.Labels == nil {
		d.Labels = map[string]string{}
	}
	d.RestartPolicy = r.HostConfig.RestartPolicy.Name
	if d.RestartPolicy == "on-failure" && r.HostConfig.RestartPolicy.MaximumRetryCount > 0 {
		d.RestartPolicy += ":" + strconv.Itoa(r.HostConfig.RestartPolicy.MaximumRetryCount)
	}
	if d.RestartPolicy == "" {
		d.RestartPolicy = "no"
	}
	if h := r.State.Health; h != nil {
		d.Health = h.Status
		if n := len(h.Log); n > 0 {
			d.HealthLog = strings.TrimSpace(h.Log[n-1].Output)
		}
	}

	ports := r.NetworkSettings.Ports
	if len(ports) == 0 {
		ports = r.HostConfig.PortBindings
	}
	keys := make([]string, 0, len(ports))
	for k := range ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		binds := ports[k]
		if len(binds) == 0 {
			d.Ports = append(d.Ports, DockerPortBinding{ContainerPort: k})
			continue
		}
		for _, b := range binds {
			d.Ports = append(d.Ports, DockerPortBinding{ContainerPort: k, HostIP: b.HostIP, HostPort: b.HostPort})
		}
	}
	for _, m := range r.Mounts {
		d.Mounts = append(d.Mounts, DockerMount{Type: m.Type, Name: m.Name, Source: m.Source, Destination: m.Destination, ReadWrite: m.RW})
	}
	names := make([]string, 0, len(r.NetworkSettings.Networks))
	for n := range r.NetworkSettings.Networks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := r.NetworkSettings.Networks[n]
		d.Networks = append(d.Networks, DockerContainerNetwork{Name: n, IP: v.IPAddress, Gateway: v.Gateway, MAC: v.MacAddress, Aliases: v.Aliases})
	}
	return d, nil
}

// DockerInspectContainer returns the details of one container.
func (ins *Inspector) DockerInspectContainer(hostID int64, name string) (*DockerContainerDetail, error) {
	if !ValidDockerName(name) {
		return nil, dockerInvalid("invalid container name: %s", name)
	}
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, "docker inspect "+shellQuote(name)+" 2>/dev/null", dockerOpTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 || strings.TrimSpace(stdout) == "" {
		return nil, dockerCommandError("docker inspect", "", stderr, exitCode)
	}
	return parseDockerInspect(stdout)
}

// ---------------------------------------------------------------- images

// DockerImage is a local image with whether any container uses it.
type DockerImage struct {
	ID         string `json:"id"` // short (12) without the sha256: prefix
	FullID     string `json:"full_id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       int64  `json:"size"`
	Created    string `json:"created"`
	Age        string `json:"age"`
	InUse      bool   `json:"in_use"`
	Dangling   bool   `json:"dangling"`
}

const (
	dockerSecImages  = "===IMAGES==="
	dockerSecUsed    = "===USED==="
	dockerSecInfo    = "===INFO==="
	dockerSecDF      = "===DF==="
	dockerSecCompose = "===COMPOSE==="
	dockerSecVersion = "===VERSION==="
)

const dockerImagesScript = `echo "` + dockerSecImages + `"
docker images --no-trunc --format '{{json .}}' 2>/dev/null
echo "` + dockerSecUsed + `"
ids=$(docker ps -aq 2>/dev/null)
if [ -n "$ids" ]; then docker inspect --format '{{.Image}}' $ids 2>/dev/null; fi
`

func shortImageID(full string) string {
	id := strings.TrimPrefix(full, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func parseDockerImages(out string) []DockerImage {
	sections := splitDockerSections(out)
	used := map[string]bool{}
	for _, line := range strings.Split(sections[dockerSecUsed], "\n") {
		if l := strings.TrimSpace(line); l != "" {
			used[l] = true
		}
	}
	images := []DockerImage{}
	for _, line := range strings.Split(sections[dockerSecImages], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var raw struct {
			ID           string `json:"ID"`
			Repository   string `json:"Repository"`
			Tag          string `json:"Tag"`
			Size         string `json:"Size"`
			CreatedAt    string `json:"CreatedAt"`
			CreatedSince string `json:"CreatedSince"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil || raw.ID == "" {
			continue
		}
		img := DockerImage{
			ID: shortImageID(raw.ID), FullID: raw.ID, Repository: raw.Repository, Tag: raw.Tag,
			Size: parseDockerSizeBytes(raw.Size), Created: raw.CreatedAt, Age: raw.CreatedSince,
			InUse: used[raw.ID], Dangling: raw.Repository == "<none>",
		}
		images = append(images, img)
	}
	sort.SliceStable(images, func(i, j int) bool {
		if images[i].Dangling != images[j].Dangling {
			return !images[i].Dangling
		}
		if images[i].Repository != images[j].Repository {
			return images[i].Repository < images[j].Repository
		}
		return images[i].Tag < images[j].Tag
	})
	return images
}

// DockerListImages returns local images (in-use flag included).
func (ins *Inspector) DockerListImages(hostID int64) ([]DockerImage, error) {
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerImagesScript, dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker images", stdout, stderr, exitCode)
	}
	if !strings.Contains(stdout, dockerSecImages) {
		return nil, dockerCommandError("docker images", stdout, stderr, exitCode)
	}
	return parseDockerImages(stdout), nil
}

// DockerImageOp pulls or removes an image, or prunes dangling (`prune`) / every unused (`prune_all`) images. A
// non-nil live receives the output as it is produced (and the returned output is empty).
func (ins *Inspector) DockerImageOp(hostID int64, action, image string, force bool, live io.Writer) (string, error) {
	switch action {
	case "pull", "remove":
		if !ValidDockerImage(image) {
			return "", dockerInvalid("invalid image: %q", image)
		}
	case "prune", "prune_all":
	default:
		return "", dockerInvalid("unsupported action: %s", action)
	}
	var cmd string
	timeout := dockerOpTimeout
	switch action {
	case "pull":
		cmd, timeout = "docker pull "+shellQuote(image), dockerLongTimeout
	case "remove":
		f := ""
		if force {
			f = " -f"
		}
		cmd = "docker rmi" + f + " " + shellQuote(image)
	case "prune":
		cmd, timeout = "docker image prune -f", dockerLongTimeout
	case "prune_all":
		cmd, timeout = "docker image prune -a -f", dockerLongTimeout
	}
	return ins.dockerRunOrStream(hostID, cmd+" 2>&1", "docker image "+action, image, timeout, live)
}

// ---------------------------------------------------------------- volumes and networks

// DockerVolumeOp creates or removes a named volume, or prunes unused ones (`prune`: anonymous only on new Docker
// engines; `prune_all`: every volume no container uses).
func (ins *Inspector) DockerVolumeOp(hostID int64, action, name string) (string, error) {
	var cmd string
	switch action {
	case "create", "remove":
		if !ValidDockerName(name) {
			return "", dockerInvalid("invalid volume name: %q", name)
		}
		verb := map[string]string{"create": "create", "remove": "rm"}[action]
		cmd = "docker volume " + verb + " " + shellQuote(name)
	case "prune":
		cmd = "docker volume prune -f"
	case "prune_all":
		cmd = "docker volume prune -a -f 2>/dev/null || docker volume prune -f"
	default:
		return "", dockerInvalid("unsupported action: %s", action)
	}
	return ins.dockerRun(hostID, cmd+" 2>&1", "docker volume "+action, name, dockerLongTimeout)
}

// DockerNetworkSpec describes a network to create.
type DockerNetworkSpec struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"` // bridge | macvlan | ipvlan
	Subnet   string `json:"subnet"`
	Gateway  string `json:"gateway"`
	Internal bool   `json:"internal"`
}

func buildDockerNetworkCreate(s DockerNetworkSpec) (string, error) {
	if !ValidDockerName(s.Name) {
		return "", dockerInvalid("invalid network name: %q", s.Name)
	}
	cmd := "docker network create"
	if s.Driver != "" {
		if s.Driver != "bridge" && s.Driver != "macvlan" && s.Driver != "ipvlan" {
			return "", dockerInvalid("unsupported network driver: %q", s.Driver)
		}
		cmd += " --driver " + shellQuote(s.Driver)
	}
	if s.Subnet != "" {
		if !dockerCIDRRe.MatchString(s.Subnet) {
			return "", dockerInvalid("invalid subnet (use CIDR such as 172.30.0.0/16): %q", s.Subnet)
		}
		cmd += " --subnet " + shellQuote(s.Subnet)
	}
	if s.Gateway != "" {
		if !dockerIPRe.MatchString(s.Gateway) {
			return "", dockerInvalid("invalid gateway: %q", s.Gateway)
		}
		if s.Subnet == "" {
			return "", dockerInvalid("a gateway needs a subnet")
		}
		cmd += " --gateway " + shellQuote(s.Gateway)
	}
	if s.Internal {
		cmd += " --internal"
	}
	return cmd + " " + shellQuote(s.Name), nil
}

// DockerNetworkOp creates, removes, prunes, connects or disconnects networks.
func (ins *Inspector) DockerNetworkOp(hostID int64, action string, spec DockerNetworkSpec, container string) (string, error) {
	var cmd string
	switch action {
	case "create":
		c, err := buildDockerNetworkCreate(spec)
		if err != nil {
			return "", err
		}
		cmd = c
	case "remove":
		if !ValidDockerName(spec.Name) {
			return "", dockerInvalid("invalid network name: %q", spec.Name)
		}
		cmd = "docker network rm " + shellQuote(spec.Name)
	case "connect", "disconnect":
		if !ValidDockerName(spec.Name) || !ValidDockerName(container) {
			return "", dockerInvalid("invalid network or container name")
		}
		cmd = "docker network " + action + " " + shellQuote(spec.Name) + " " + shellQuote(container)
	case "prune":
		cmd = "docker network prune -f"
	default:
		return "", dockerInvalid("unsupported action: %s", action)
	}
	return ins.dockerRun(hostID, cmd+" 2>&1", "docker network "+action, spec.Name, dockerLongTimeout)
}

// ---------------------------------------------------------------- system

// DockerSystemInfo is `docker info` + `docker system df`.
type DockerSystemInfo struct {
	ServerVersion     string            `json:"server_version"`
	ClientVersion     string            `json:"client_version"`
	ComposeVersion    string            `json:"compose_version"`
	OS                string            `json:"os"`
	Architecture      string            `json:"architecture"`
	Kernel            string            `json:"kernel"`
	CPUs              int               `json:"cpus"`
	MemoryBytes       int64             `json:"memory_bytes"`
	Containers        int               `json:"containers"`
	ContainersRunning int               `json:"containers_running"`
	ContainersPaused  int               `json:"containers_paused"`
	ContainersStopped int               `json:"containers_stopped"`
	Images            int               `json:"images"`
	StorageDriver     string            `json:"storage_driver"`
	LoggingDriver     string            `json:"logging_driver"`
	CgroupDriver      string            `json:"cgroup_driver"`
	RootDir           string            `json:"root_dir"`
	Swarm             string            `json:"swarm"`
	Rootless          bool              `json:"rootless"`
	Usage             []DockerDiskUsage `json:"usage"`
}

// DockerDiskUsage is one row of `docker system df`.
type DockerDiskUsage struct {
	Type        string `json:"type"`
	Total       string `json:"total"`
	Active      string `json:"active"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

const dockerSystemScript = `if ! docker info >/dev/null 2>&1; then echo "===ERROR==="; docker info 2>&1 | head -n 5; exit 0; fi
echo "` + dockerSecInfo + `"
docker info --format '{{json .}}' 2>/dev/null
echo "` + dockerSecDF + `"
docker system df --format '{{json .}}' 2>/dev/null
echo "` + dockerSecVersion + `"
docker version --format '{{.Client.Version}}|{{.Server.Version}}' 2>/dev/null
echo "` + dockerSecCompose + `"
(docker compose version --short 2>/dev/null || docker-compose version --short 2>/dev/null)
`

func parseDockerSystem(out string) (*DockerSystemInfo, error) {
	sections := splitDockerSections(out)
	if e, ok := sections["===ERROR==="]; ok {
		return nil, fmt.Errorf("%s", strings.TrimSpace(e))
	}
	var raw struct {
		ServerVersion     string `json:"ServerVersion"`
		OperatingSystem   string `json:"OperatingSystem"`
		Architecture      string `json:"Architecture"`
		KernelVersion     string `json:"KernelVersion"`
		NCPU              int    `json:"NCPU"`
		MemTotal          int64  `json:"MemTotal"`
		Containers        int    `json:"Containers"`
		ContainersRunning int    `json:"ContainersRunning"`
		ContainersPaused  int    `json:"ContainersPaused"`
		ContainersStopped int    `json:"ContainersStopped"`
		Images            int    `json:"Images"`
		Driver            string `json:"Driver"`
		LoggingDriver     string `json:"LoggingDriver"`
		CgroupDriver      string `json:"CgroupDriver"`
		DockerRootDir     string `json:"DockerRootDir"`
		SecurityOptions   []string
		Swarm             struct {
			LocalNodeState string `json:"LocalNodeState"`
		} `json:"Swarm"`
	}
	info := strings.TrimSpace(sections[dockerSecInfo])
	if info == "" || json.Unmarshal([]byte(info), &raw) != nil {
		return nil, fmt.Errorf("could not read docker info output")
	}
	s := &DockerSystemInfo{
		ServerVersion: raw.ServerVersion, OS: raw.OperatingSystem, Architecture: raw.Architecture, Kernel: raw.KernelVersion,
		CPUs: raw.NCPU, MemoryBytes: raw.MemTotal, Containers: raw.Containers, ContainersRunning: raw.ContainersRunning,
		ContainersPaused: raw.ContainersPaused, ContainersStopped: raw.ContainersStopped, Images: raw.Images,
		StorageDriver: raw.Driver, LoggingDriver: raw.LoggingDriver, CgroupDriver: raw.CgroupDriver, RootDir: raw.DockerRootDir,
		Swarm: raw.Swarm.LocalNodeState, Usage: []DockerDiskUsage{},
	}
	for _, o := range raw.SecurityOptions {
		if strings.Contains(o, "rootless") {
			s.Rootless = true
		}
	}
	for _, line := range strings.Split(sections[dockerSecDF], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var u struct {
			Type        string `json:"Type"`
			TotalCount  string `json:"TotalCount"`
			Active      string `json:"Active"`
			Size        string `json:"Size"`
			Reclaimable string `json:"Reclaimable"`
		}
		if json.Unmarshal([]byte(line), &u) == nil && u.Type != "" {
			s.Usage = append(s.Usage, DockerDiskUsage{Type: u.Type, Total: u.TotalCount, Active: u.Active, Size: u.Size, Reclaimable: u.Reclaimable})
		}
	}
	if v := strings.TrimSpace(sections[dockerSecVersion]); v != "" {
		if c, _, ok := strings.Cut(v, "|"); ok {
			s.ClientVersion = strings.TrimSpace(c)
		}
	}
	s.ComposeVersion = strings.TrimSpace(sections[dockerSecCompose])
	return s, nil
}

// DockerSystem returns engine information and disk usage.
func (ins *Inspector) DockerSystem(hostID int64) (*DockerSystemInfo, error) {
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerSystemScript, dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 && strings.TrimSpace(stdout) == "" {
		return nil, dockerCommandError("docker info", stdout, stderr, exitCode)
	}
	return parseDockerSystem(stdout)
}

var dockerPruneCommands = map[string]string{
	"containers": "docker container prune -f",
	"images":     "docker image prune -f",
	"images_all": "docker image prune -a -f",
	"networks":   "docker network prune -f",
	"volumes":    "docker volume prune -a -f 2>/dev/null || docker volume prune -f",
	"builder":    "docker builder prune -f",
}

// DockerPrune removes unused objects of one kind. There is deliberately no "everything" target.
func (ins *Inspector) DockerPrune(hostID int64, target string) (string, error) {
	cmd, ok := dockerPruneCommands[target]
	if !ok {
		return "", dockerInvalid("unsupported prune target: %s", target)
	}
	return ins.dockerRun(hostID, cmd+" 2>&1", "docker prune", target, dockerLongTimeout)
}

// ---------------------------------------------------------------- compose stacks

// DockerStack is a Compose project reconstructed from container labels.
type DockerStack struct {
	Name        string   `json:"name"`
	WorkingDir  string   `json:"working_dir"`
	ConfigFiles []string `json:"config_files"`
	Services    []string `json:"services"`
	Total       int      `json:"total"`
	Running     int      `json:"running"`
	Status      string   `json:"status"` // running | partial | stopped
}

const dockerStacksScript = `docker ps -a --format '{{.State}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}|{{.Label "com.docker.compose.project.working_dir"}}|{{.Label "com.docker.compose.project.config_files"}}' 2>&1`

func parseDockerStacks(out string) []DockerStack {
	byName := map[string]*DockerStack{}
	var order []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(line), "|", 5)
		if len(f) < 5 || f[1] == "" {
			continue
		}
		st := byName[f[1]]
		if st == nil {
			st = &DockerStack{Name: f[1], WorkingDir: f[3], Services: []string{}, ConfigFiles: []string{}}
			for _, p := range strings.Split(f[4], ",") {
				if p = strings.TrimSpace(p); p != "" {
					st.ConfigFiles = append(st.ConfigFiles, p)
				}
			}
			byName[f[1]] = st
			order = append(order, f[1])
		}
		st.Total++
		if f[0] == "running" {
			st.Running++
		}
		if f[2] != "" {
			seen := false
			for _, s := range st.Services {
				if s == f[2] {
					seen = true
					break
				}
			}
			if !seen {
				st.Services = append(st.Services, f[2])
			}
		}
	}
	stacks := make([]DockerStack, 0, len(order))
	for _, n := range order {
		st := byName[n]
		sort.Strings(st.Services)
		switch {
		case st.Running == 0:
			st.Status = "stopped"
		case st.Running < st.Total:
			st.Status = "partial"
		default:
			st.Status = "running"
		}
		stacks = append(stacks, *st)
	}
	sort.SliceStable(stacks, func(i, j int) bool { return stacks[i].Name < stacks[j].Name })
	return stacks
}

// DockerListStacks returns Compose projects found among the host's containers.
func (ins *Inspector) DockerListStacks(hostID int64) ([]DockerStack, error) {
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerStacksScript, dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker ps", stdout, stderr, exitCode)
	}
	return parseDockerStacks(stdout), nil
}

func (ins *Inspector) dockerFindStack(hostID int64, name string) (*DockerStack, error) {
	if !composeNameRe.MatchString(name) {
		return nil, dockerInvalid("invalid stack name: %q", name)
	}
	stacks, err := ins.DockerListStacks(hostID)
	if err != nil {
		return nil, err
	}
	for i := range stacks {
		if stacks[i].Name == name {
			return &stacks[i], nil
		}
	}
	return nil, fmt.Errorf("stack %q not found on this host", name)
}

// validStackFile accepts absolute compose file paths only (they come from container labels, which any
// container creator controls, so they are validated like user input).
func validStackFile(p string) bool {
	if !dockerAbsPathRe.MatchString(p) || strings.Contains(p, "..") {
		return false
	}
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".yml") || strings.HasSuffix(l, ".yaml")
}

// Each action is one or more `compose` subcommands run in order (stopping at the first failure).
var dockerStackActions = map[string][]string{
	"up":       {"up -d"},
	"down":     {"down"},
	"start":    {"start"},
	"stop":     {"stop"},
	"restart":  {"restart"},
	"pull":     {"pull"},
	"recreate": {"up -d --force-recreate"},
	"update":   {"pull", "up -d"},
}

// composePrelude selects `docker compose` (v2) or `docker-compose` (v1) as $C.
const composePrelude = `exec 2>&1
if docker compose version >/dev/null 2>&1; then C="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then C="docker-compose"
else echo "docker compose is not installed on this host"; exit 127; fi
`

func composeFileFlags(files []string) (string, error) {
	var b strings.Builder
	for _, f := range files {
		if !validStackFile(f) {
			return "", dockerInvalid("unsafe compose file path: %q", f)
		}
		b.WriteString(" -f " + shellQuote(f))
	}
	return b.String(), nil
}

func composeStackScript(st *DockerStack, action string) (string, error) {
	subs, ok := dockerStackActions[action]
	if !ok {
		return "", dockerInvalid("unsupported stack action: %s", action)
	}
	if len(st.ConfigFiles) == 0 {
		return "", fmt.Errorf("stack %q has no compose file recorded (it was not started with Docker Compose)", st.Name)
	}
	flags, err := composeFileFlags(st.ConfigFiles)
	if err != nil {
		return "", err
	}
	wd := st.WorkingDir
	if wd != "" && !dockerAbsPathRe.MatchString(wd) {
		return "", dockerInvalid("unsafe working directory: %q", wd)
	}
	cd := `cd "$HOME"`
	if wd != "" {
		cd = "cd " + shellQuote(wd) + ` 2>/dev/null || cd "$HOME"`
	}
	// The -f flags are written inline (each path single-quoted): a variable holding them would not be re-parsed.
	base := `$C -p ` + shellQuote(st.Name) + flags
	cmds := make([]string, len(subs))
	for i, s := range subs {
		cmds[i] = base + " " + s
	}
	return composePrelude + cd + "\n" + strings.Join(cmds, " && ") + "\n", nil
}

// DockerStackOp runs a Compose action (up, down, start, stop, restart, pull, recreate, update) on a project. A
// non-nil live receives the output as it is produced (and the returned output is empty).
func (ins *Inspector) DockerStackOp(hostID int64, name, action string, live io.Writer) (string, error) {
	st, err := ins.dockerFindStack(hostID, name)
	if err != nil {
		return "", err
	}
	script, err := composeStackScript(st, action)
	if err != nil {
		return "", err
	}
	return ins.dockerRunOrStream(hostID, script, "docker compose "+action, name, dockerLongTimeout, live)
}

// DockerStackFile returns the compose file of a stack (the only file when a project has exactly one).
func (ins *Inspector) DockerStackFile(hostID int64, name string) (path, content string, err error) {
	st, err := ins.dockerFindStack(hostID, name)
	if err != nil {
		return "", "", err
	}
	if len(st.ConfigFiles) != 1 {
		return "", "", fmt.Errorf("stack %q uses %d compose files; only single-file stacks can be viewed or edited here", name, len(st.ConfigFiles))
	}
	path = st.ConfigFiles[0]
	if !validStackFile(path) {
		return "", "", dockerInvalid("unsafe compose file path: %q", path)
	}
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, "head -c "+strconv.Itoa(dockerStackFileMax+1)+" -- "+shellQuote(path)+" 2>&1", dockerOpTimeout)
	if err != nil {
		return "", "", err
	}
	if exitCode != 0 {
		return "", "", dockerCommandError("read compose file", stdout, stderr, exitCode)
	}
	if len(stdout) > dockerStackFileMax {
		return "", "", fmt.Errorf("compose file is larger than %d KB", dockerStackFileMax/1024)
	}
	return path, stdout, nil
}

func composeDeployScript(project, dir, file, b64 string, existing bool) string {
	backup := ""
	if existing {
		backup = `[ -f "$FILE" ] && cp -p "$FILE" "$FILE.pantau-bak"` + "\n"
	}
	return composePrelude + `P=` + shellQuote(project) + `
DIR=` + dir + `
FILE=` + file + `
mkdir -p "$DIR" || exit 1
` + backup + `printf '%s' ` + shellQuote(b64) + ` | (base64 -d 2>/dev/null || base64 -D) > "$FILE" || exit 1
cd "$DIR" || exit 1
if ! out=$($C -p "$P" -f "$FILE" config -q 2>&1); then echo "Compose file is not valid:"; echo "$out"; exit 1; fi
$C -p "$P" -f "$FILE" up -d 2>&1
`
}

// DockerStackDeploy creates a Pantau-managed stack (stored under ~/.pantau/stacks/<name>) or rewrites the single
// compose file of an existing one, validates it with `compose config`, and runs `up -d`. A non-nil live receives
// the output as it is produced (and the returned output is empty).
func (ins *Inspector) DockerStackDeploy(hostID int64, name, yaml string, live io.Writer) (string, error) {
	if !composeNameRe.MatchString(name) {
		return "", dockerInvalid("invalid stack name (lowercase letters, digits, - and _): %q", name)
	}
	if strings.TrimSpace(yaml) == "" {
		return "", dockerInvalid("compose file is empty")
	}
	if len(yaml) > dockerStackFileMax {
		return "", dockerInvalid("compose file is larger than %d KB", dockerStackFileMax/1024)
	}
	if strings.ContainsRune(yaml, 0) {
		return "", dockerInvalid("compose file contains a NUL byte")
	}

	dir := `"$HOME/` + dockerStackDirName + `/` + name + `"`
	file := `"$DIR/docker-compose.yml"`
	existing := false
	if st, err := ins.dockerFindStack(hostID, name); err == nil {
		if len(st.ConfigFiles) != 1 || !validStackFile(st.ConfigFiles[0]) {
			return "", fmt.Errorf("stack %q uses multiple or unsafe compose files and cannot be redeployed from here", name)
		}
		existing = true
		dir = shellQuote(pathDir(st.ConfigFiles[0]))
		file = shellQuote(st.ConfigFiles[0])
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(yaml))
	script := composeDeployScript(name, dir, file, b64, existing)
	return ins.dockerRunOrStream(hostID, script, "docker stack deploy", name, dockerLongTimeout, live)
}

func pathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "/"
}
