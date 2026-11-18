package inspector

// Docker image, volume, network and system maintenance operations.

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

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

// DockerImageRefs is one local image with every repo:tag and the registry digests the engine recorded for it.
type DockerImageRefs struct {
	ID      string
	Tags    []string
	Digests []string
}

// A refused `docker images` must fail the script, not look like a host without images.
const dockerImageRefsScript = `ids=$(docker images -q --no-trunc) || exit $?
ids=$(printf '%s\n' "$ids" | sort -u)
[ -z "$ids" ] || docker image inspect --format '{{.Id}}|{{join .RepoTags ","}}|{{join .RepoDigests ","}}' $ids
`

func parseDockerImageRefs(out string) []DockerImageRefs {
	list := func(s, none string) []string {
		var v []string
		for _, x := range strings.Split(s, ",") {
			if x = strings.TrimSpace(x); x != "" && x != none {
				v = append(v, x)
			}
		}
		return v
	}
	refs := []DockerImageRefs{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 3 || !strings.HasPrefix(f[0], "sha256:") {
			continue
		}
		refs = append(refs, DockerImageRefs{ID: f[0], Tags: list(f[1], "<none>:<none>"), Digests: list(f[2], "<none>@<none>")})
	}
	return refs
}

// DockerImageRefs returns the repo:tag names and registry digests of every local image (for the update check).
func (ins *Inspector) DockerImageRefs(hostID int64) ([]DockerImageRefs, error) {
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerImageRefsScript, dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker image inspect", stdout, stderr, exitCode)
	}
	return parseDockerImageRefs(stdout), nil
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
