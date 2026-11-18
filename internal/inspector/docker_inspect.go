package inspector

// Detailed docker inspect parsing and the container-detail data model.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

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
