package inspector

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pantau/internal/sshrunner"
)

// DockerStat is one running container's resource usage from `docker stats --no-stream`.
type DockerStat struct {
	Name          string  `json:"name"`
	ID            string  `json:"id"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemPercent    float64 `json:"mem_percent"`
	MemUsedBytes  int64   `json:"mem_used_bytes"`
	MemLimitBytes int64   `json:"mem_limit_bytes"`
	NetIO         string  `json:"net_io"`
	BlockIO       string  `json:"block_io"`
	PIDs          int     `json:"pids"`
}

// DockerVolume is a named volume with its usage by containers. Size is empty when Docker cannot report it
// (older engines without `system df -v --format`, or the size scan timed out).
type DockerVolume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  string            `json:"created_at"`
	Labels     map[string]string `json:"labels"`
	Size       string            `json:"size"`
	UsedBy     []string          `json:"used_by"`
	Anonymous  bool              `json:"anonymous"` // created implicitly by a container (VOLUME in an image / `-v /path`)
}

// DockerNetworkContainer is a container attached to a Docker network.
type DockerNetworkContainer struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// DockerNetwork is a Docker network with its IPAM subnets and attached containers.
type DockerNetwork struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Driver     string                   `json:"driver"`
	Scope      string                   `json:"scope"`
	Internal   bool                     `json:"internal"`
	IPv6       bool                     `json:"ipv6"`
	BuiltIn    bool                     `json:"built_in"`
	Subnets    []string                 `json:"subnets"`
	Gateways   []string                 `json:"gateways"`
	Containers []DockerNetworkContainer `json:"containers"`
}

const (
	dockerScriptTimeout = 60 * time.Second

	dockerSectionError    = "===ERROR==="
	dockerSectionVolumes  = "===VOLUMES==="
	dockerSectionUsers    = "===USERS==="
	dockerSectionDF       = "===DF==="
	dockerSectionNetworks = "===NETWORKS==="
)

// dockerVolumesScript runs under `sh -c` (not the login shell: zsh on macOS does not word-split variables).
// Sizes come last and run under `timeout` when available because `system df -v` walks every volume.
var dockerVolumesScript = DarwinPathPrelude + `
out=$(docker volume ls -q 2>&1); rc=$?
if [ $rc -ne 0 ]; then echo "` + dockerSectionError + `"; echo "$out"; exit 0; fi
echo "` + dockerSectionVolumes + `"
if [ -n "$out" ]; then docker volume inspect $out 2>/dev/null; else echo "[]"; fi
echo "` + dockerSectionUsers + `"
ids=$(docker ps -aq 2>/dev/null)
if [ -n "$ids" ]; then docker inspect --format "{{.Name}}|{{range .Mounts}}{{.Type}}:{{.Name}},{{end}}" $ids 2>/dev/null; fi
echo "` + dockerSectionDF + `"
T=""; if command -v timeout >/dev/null 2>&1; then T="timeout 40"; fi
$T docker system df -v --format "{{json .Volumes}}" 2>/dev/null
`

var dockerNetworksScript = DarwinPathPrelude + `
out=$(docker network ls -q 2>&1); rc=$?
if [ $rc -ne 0 ]; then echo "` + dockerSectionError + `"; echo "$out"; exit 0; fi
echo "` + dockerSectionNetworks + `"
if [ -n "$out" ]; then docker network inspect $out 2>/dev/null; else echo "[]"; fi
`

// ListDockerStats returns a one-shot resource snapshot of all running containers.
func (ins *Inspector) ListDockerStats(hostID int64) ([]DockerStat, error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	stdout, stderr, exitCode, err := runner.ExecTimeout(DarwinPathPrelude+`docker stats --no-stream --no-trunc --format '{{json .}}' 2>&1`, dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker stats", stdout, stderr, exitCode)
	}
	return parseDockerStats(stdout), nil
}

// ListDockerVolumes returns named volumes with their size (when available) and the containers using them.
func (ins *Inspector) ListDockerVolumes(hostID int64) ([]DockerVolume, error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	stdout, stderr, exitCode, err := runner.ExecTimeout("sh -c "+shellQuote(dockerVolumesScript), dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker volume", stdout, stderr, exitCode)
	}
	return parseDockerVolumes(stdout)
}

// ListDockerNetworks returns Docker networks with subnets and attached containers.
func (ins *Inspector) ListDockerNetworks(hostID int64) ([]DockerNetwork, error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	stdout, stderr, exitCode, err := runner.ExecTimeout("sh -c "+shellQuote(dockerNetworksScript), dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker network", stdout, stderr, exitCode)
	}
	return parseDockerNetworks(stdout)
}

func (ins *Inspector) dockerRunner(hostID int64) (sshrunner.Runner, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("host not found")
	}
	return ins.GetRunnerForHost(host)
}

func dockerCommandError(what, stdout, stderr string, exitCode int) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = strings.TrimSpace(stdout)
	}
	if msg == "" {
		msg = fmt.Sprintf("%s failed (exit %d)", what, exitCode)
	}
	return fmt.Errorf("%s", msg)
}

// splitDockerSections splits script output on its ===NAME=== marker lines.
func splitDockerSections(out string) map[string]string {
	sections := map[string]string{}
	var name string
	var cur strings.Builder
	flush := func() {
		if name != "" {
			sections[name] = cur.String()
		}
		cur.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===") && len(trimmed) > 6 {
			flush()
			name = trimmed
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	flush()
	return sections
}

func parseDockerStats(out string) []DockerStat {
	stats := []DockerStat{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var raw struct {
			Name     string `json:"Name"`
			ID       string `json:"ID"`
			CPUPerc  string `json:"CPUPerc"`
			MemPerc  string `json:"MemPerc"`
			MemUsage string `json:"MemUsage"`
			NetIO    string `json:"NetIO"`
			BlockIO  string `json:"BlockIO"`
			PIDs     string `json:"PIDs"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil || raw.Name == "" {
			continue
		}
		used, limit := int64(0), int64(0)
		if parts := strings.SplitN(raw.MemUsage, "/", 2); len(parts) == 2 {
			used = parseDockerSizeBytes(parts[0])
			limit = parseDockerSizeBytes(parts[1])
		}
		pids, _ := strconv.Atoi(strings.TrimSpace(raw.PIDs))
		stats = append(stats, DockerStat{
			Name:          raw.Name,
			ID:            raw.ID,
			CPUPercent:    parseDockerPercent(raw.CPUPerc),
			MemPercent:    parseDockerPercent(raw.MemPerc),
			MemUsedBytes:  used,
			MemLimitBytes: limit,
			NetIO:         raw.NetIO,
			BlockIO:       raw.BlockIO,
			PIDs:          pids,
		})
	}
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].CPUPercent > stats[j].CPUPercent })
	return stats
}

func parseDockerPercent(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	if err != nil {
		return 0 // "--" for containers whose cgroup reports nothing
	}
	return v
}

var dockerSizeRe = regexp.MustCompile(`^\s*([0-9]*\.?[0-9]+)\s*([A-Za-z]*)\s*$`)

// parseDockerSizeBytes parses Docker's human sizes: decimal (kB, MB, GB) and binary (KiB, MiB, GiB).
func parseDockerSizeBytes(s string) int64 {
	m := dockerSizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{
		"": 1, "b": 1,
		"kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12, "pb": 1e15,
		"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40, "pib": 1 << 50,
	}[strings.ToLower(m[2])]
	return int64(v * mult)
}

func parseDockerVolumes(out string) ([]DockerVolume, error) {
	sections := splitDockerSections(out)
	if e, ok := sections[dockerSectionError]; ok {
		return nil, fmt.Errorf("%s", strings.TrimSpace(e))
	}

	var inspected []struct {
		Name       string            `json:"Name"`
		Driver     string            `json:"Driver"`
		Scope      string            `json:"Scope"`
		Mountpoint string            `json:"Mountpoint"`
		CreatedAt  string            `json:"CreatedAt"`
		Labels     map[string]string `json:"Labels"`
	}
	if body := strings.TrimSpace(sections[dockerSectionVolumes]); body != "" {
		if err := json.Unmarshal([]byte(body), &inspected); err != nil {
			return nil, fmt.Errorf("parse docker volume inspect: %w", err)
		}
	}

	sizes := map[string]string{}
	var df []struct {
		Name string `json:"Name"`
		Size string `json:"Size"`
	}
	if body := strings.TrimSpace(sections[dockerSectionDF]); strings.HasPrefix(body, "[") {
		if json.Unmarshal([]byte(body), &df) == nil {
			for _, v := range df {
				if v.Size != "" && v.Size != "N/A" {
					sizes[v.Name] = v.Size
				}
			}
		}
	}

	users := map[string][]string{}
	for _, line := range strings.Split(sections[dockerSectionUsers], "\n") {
		line = strings.TrimSpace(line)
		name, mounts, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		name = strings.TrimPrefix(name, "/")
		for _, m := range strings.Split(mounts, ",") {
			if vol, found := strings.CutPrefix(m, "volume:"); found && vol != "" {
				users[vol] = append(users[vol], name)
			}
		}
	}

	volumes := make([]DockerVolume, 0, len(inspected))
	for _, v := range inspected {
		usedBy := users[v.Name]
		sort.Strings(usedBy)
		if usedBy == nil {
			usedBy = []string{}
		}
		if v.Labels == nil {
			v.Labels = map[string]string{}
		}
		volumes = append(volumes, DockerVolume{
			Name: v.Name, Driver: v.Driver, Scope: v.Scope, Mountpoint: v.Mountpoint, CreatedAt: v.CreatedAt,
			Labels: v.Labels, Size: sizes[v.Name], UsedBy: usedBy, Anonymous: isAnonymousDockerVolume(v.Name, v.Labels),
		})
	}
	sort.SliceStable(volumes, func(i, j int) bool {
		if volumes[i].Anonymous != volumes[j].Anonymous {
			return !volumes[i].Anonymous // named volumes first
		}
		return volumes[i].Name < volumes[j].Name
	})
	return volumes, nil
}

var anonymousVolumeNameRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func isAnonymousDockerVolume(name string, labels map[string]string) bool {
	if _, ok := labels["com.docker.volume.anonymous"]; ok {
		return true
	}
	return anonymousVolumeNameRe.MatchString(name)
}

func parseDockerNetworks(out string) ([]DockerNetwork, error) {
	sections := splitDockerSections(out)
	if e, ok := sections[dockerSectionError]; ok {
		return nil, fmt.Errorf("%s", strings.TrimSpace(e))
	}
	var inspected []struct {
		ID         string `json:"Id"`
		Name       string `json:"Name"`
		Driver     string `json:"Driver"`
		Scope      string `json:"Scope"`
		Internal   bool   `json:"Internal"`
		EnableIPv6 bool   `json:"EnableIPv6"`
		IPAM       struct {
			Config []struct {
				Subnet  string `json:"Subnet"`
				Gateway string `json:"Gateway"`
			} `json:"Config"`
		} `json:"IPAM"`
		Containers map[string]struct {
			Name        string `json:"Name"`
			IPv4Address string `json:"IPv4Address"`
			IPv6Address string `json:"IPv6Address"`
		} `json:"Containers"`
	}
	if body := strings.TrimSpace(sections[dockerSectionNetworks]); body != "" {
		if err := json.Unmarshal([]byte(body), &inspected); err != nil {
			return nil, fmt.Errorf("parse docker network inspect: %w", err)
		}
	}

	networks := make([]DockerNetwork, 0, len(inspected))
	for _, n := range inspected {
		id := n.ID
		if len(id) > 12 {
			id = id[:12]
		}
		net := DockerNetwork{
			ID: id, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Internal: n.Internal, IPv6: n.EnableIPv6,
			BuiltIn:    n.Name == "bridge" || n.Name == "host" || n.Name == "none",
			Subnets:    []string{},
			Gateways:   []string{},
			Containers: []DockerNetworkContainer{},
		}
		for _, c := range n.IPAM.Config {
			if c.Subnet != "" {
				net.Subnets = append(net.Subnets, c.Subnet)
			}
			if c.Gateway != "" {
				net.Gateways = append(net.Gateways, c.Gateway)
			}
		}
		for _, c := range n.Containers {
			net.Containers = append(net.Containers, DockerNetworkContainer{Name: c.Name, IPv4: c.IPv4Address, IPv6: c.IPv6Address})
		}
		sort.Slice(net.Containers, func(i, j int) bool { return net.Containers[i].Name < net.Containers[j].Name })
		networks = append(networks, net)
	}
	sort.SliceStable(networks, func(i, j int) bool {
		if networks[i].BuiltIn != networks[j].BuiltIn {
			return !networks[i].BuiltIn // user-defined networks first
		}
		return networks[i].Name < networks[j].Name
	})
	return networks, nil
}
