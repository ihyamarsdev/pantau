package ansible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// FactsTimeout bounds one setup run.
	FactsTimeout = 90 * time.Second
	// MaxFactsBytes bounds the serialized facts (the default callback emits ~70 KiB per host).
	MaxFactsBytes = 2 * 1024 * 1024
)

// GatherFacts runs `ansible <alias> -m setup` for one target on the Generated Inventory and returns its
// ansible_facts map. Output is the ad-hoc `minimal` stdout callback (`<alias> | SUCCESS => {json}`): it ships with
// ansible-core itself, unlike the `json` callback which lives in the ansible.posix collection and is missing from
// slim installs such as the Alpine ansible-core package. No JSON object in the output is an error, not empty facts.
func GatherFacts(ctx context.Context, t Target, cfg, libDir string, log io.Writer) (map[string]any, error) {
	if err := ValidatePattern(""); err != nil { // no-op, keeps the pattern contract documented in one place
		return nil, err
	}
	binPath, err := exec.LookPath("ansible")
	if err != nil {
		return nil, fmt.Errorf("ansible not found in PATH: %w", err)
	}
	factsCtx, cancel := context.WithTimeout(ctx, FactsTimeout)
	defer cancel()

	workDir, err := os.MkdirTemp("", "pantau-facts-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	_ = os.Chmod(workDir, 0700)
	defer os.RemoveAll(workDir)
	keysDir := filepath.Join(workDir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create keys dir: %w", err)
	}

	invBytes, aliasMap, err := InventoryJSON([]Target{t}, func(alias string) string {
		return filepath.Join(keysDir, alias+".key")
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate inventory: %w", err)
	}
	alias := ""
	for a := range aliasMap {
		alias = a
	}
	if strings.TrimSpace(t.PrivateKey) != "" {
		kp := filepath.Join(keysDir, alias+".key")
		if err := os.WriteFile(kp, []byte(strings.TrimSpace(t.PrivateKey)+"\n"), 0600); err != nil {
			return nil, fmt.Errorf("failed to write key file: %w", err)
		}
	}
	invPath := filepath.Join(workDir, "inventory.json")
	if err := os.WriteFile(invPath, invBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to write inventory.json: %w", err)
	}

	cfgText, err := BuildConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid ansible config: %w", err)
	}
	cfgPath := filepath.Join(workDir, "ansible.cfg")
	if err := os.WriteFile(cfgPath, []byte(cfgText), 0600); err != nil {
		return nil, fmt.Errorf("failed to write ansible.cfg: %w", err)
	}

	ansibleHome := filepath.Join(workDir, ".ansible")
	_ = os.MkdirAll(ansibleHome, 0700)
	cleanEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"HOME=" + workDir,
		"ANSIBLE_CONFIG=" + cfgPath,
		"ANSIBLE_FORCE_COLOR=0",
		// minimal is pinned by env so a user-validated ansible.cfg cannot switch the output format under the parser.
		"ANSIBLE_STDOUT_CALLBACK=minimal",
		"ANSIBLE_HOME=" + ansibleHome,
	}
	if libDir != "" {
		cleanEnv = append(cleanEnv,
			"ANSIBLE_ROLES_PATH="+strings.Join([]string{LibraryRolesDir(libDir), filepath.Join(ansibleHome, "roles")}, string(os.PathListSeparator)),
			"ANSIBLE_COLLECTIONS_PATH="+strings.Join([]string{LibraryCollectionsDir(libDir), filepath.Join(ansibleHome, "collections")}, string(os.PathListSeparator)),
		)
	}
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if val, ok := os.LookupEnv(k); ok {
			cleanEnv = append(cleanEnv, k+"="+val)
		}
	}

	// stdout is captured for JSON; stderr (warnings, connection errors) goes to the log and the error.
	var out bytes.Buffer
	cmd := exec.Command(binPath, alias, "-i", invPath, "-m", "setup")
	cmd.Dir = workDir
	cmd.Env = cleanEnv
	cmd.Stdout = &out
	stderr := &tailWriter{}
	if log != nil {
		cmd.Stderr = io.MultiWriter(log, stderr)
	} else {
		cmd.Stderr = stderr
	}
	exitCode, err := runCmd(factsCtx, cmd)
	if err != nil {
		return nil, err
	}
	if out.Len() > MaxFactsBytes {
		return nil, fmt.Errorf("facts exceed %d MiB", MaxFactsBytes/1024/1024)
	}
	facts, parseErr := parseSetupFacts(out.Bytes())
	if parseErr != nil {
		if exitCode != 0 && !strings.HasPrefix(parseErr.Error(), "setup failed") {
			return nil, fmt.Errorf("ansible exited with %d: %s", exitCode, stderr.summary())
		}
		return nil, parseErr
	}
	if exitCode != 0 {
		// A failed task can still ship a partial ansible_facts object; do not cache it.
		return nil, fmt.Errorf("ansible exited with %d: %s", exitCode, stderr.summary())
	}
	return facts, nil
}

// parseSetupFacts reads the `minimal` callback output: `<alias> | SUCCESS => {"ansible_facts": {...}}`. Failed and
// unreachable hosts print the same shape with `FAILED!`/`UNREACHABLE!` and a `msg`, which becomes the error.
func parseSetupFacts(raw []byte) (map[string]any, error) {
	m := factsResultRegex.FindSubmatchIndex(raw)
	if m == nil {
		return nil, errors.New("ansible produced no per-host result in its output")
	}
	status := string(raw[m[2]:m[3]])
	var res struct {
		AnsibleFacts map[string]any `json:"ansible_facts"`
		Msg          string         `json:"msg"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw[m[1]:])).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to parse ansible output: %w", err)
	}
	if strings.HasPrefix(status, "FAILED") || strings.HasPrefix(status, "UNREACHABLE") {
		return nil, fmt.Errorf("setup failed: %s", res.Msg)
	}
	if len(res.AnsibleFacts) == 0 {
		return nil, errors.New("ansible returned no facts")
	}
	return res.AnsibleFacts, nil
}

// factsResultRegex matches the header of the result line and leaves the JSON object right after it.
var factsResultRegex = regexp.MustCompile(`(?m)^\S+ \| (SUCCESS|CHANGED|FAILED!|UNREACHABLE!) => `)

// tailWriter keeps the last few KiB of stderr for a concise error message.
type tailWriter struct{ buf []byte }

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if len(w.buf) > 4096 {
		w.buf = w.buf[len(w.buf)-4096:]
	}
	return len(p), nil
}

var errorLineRegex = regexp.MustCompile(`(?m)^(?:\[ERROR\]: |fatal: )?(.*\S)$`)

// summary is the first non-warning line of stderr, trimmed.
func (w *tailWriter) summary() string {
	for _, line := range strings.Split(string(w.buf), "\n") {
		t := strings.TrimSpace(adhocANSIRegex.ReplaceAllString(line, ""))
		if t == "" || strings.HasPrefix(t, "[WARNING]") || strings.HasPrefix(t, "[DEPRECATION WARNING]") {
			continue
		}
		t = strings.TrimPrefix(t, "[ERROR]: ")
		return truncateLine(t)
	}
	return "no output"
}

// FactsMount is one filesystem.
type FactsMount struct {
	Mount     string `json:"mount"`
	Device    string `json:"device"`
	FSType    string `json:"fstype"`
	SizeTotal int64  `json:"size_total"`
	SizeAvail int64  `json:"size_available"`
}

// FactsInterface is one network interface.
type FactsInterface struct {
	Name   string   `json:"name"`
	Active bool     `json:"active"`
	IPv4   string   `json:"ipv4,omitempty"`
	IPv6   []string `json:"ipv6,omitempty"`
	MAC    string   `json:"mac,omitempty"`
	MTU    int      `json:"mtu,omitempty"`
}

// FactsSummary is the human-facing subset of ansible_facts.
type FactsSummary struct {
	OS             string           `json:"os"`
	OSFamily       string           `json:"os_family"`
	Distribution   string           `json:"distribution"`
	Version        string           `json:"version"`
	Kernel         string           `json:"kernel"`
	Architecture   string           `json:"architecture"`
	Hostname       string           `json:"hostname"`
	FQDN           string           `json:"fqdn"`
	CPUCores       int              `json:"cpu_cores"`
	CPUModel       string           `json:"cpu_model"`
	MemTotalMB     int64            `json:"memtotal_mb"`
	SwapTotalMB    int64            `json:"swaptotal_mb"`
	Mounts         []FactsMount     `json:"mounts"`
	Interfaces     []FactsInterface `json:"interfaces"`
	DefaultIPv4    string           `json:"default_ipv4"`
	UptimeSeconds  int64            `json:"uptime_seconds"`
	Python         string           `json:"python"`
	Virtualization string           `json:"virtualization"`
}

// SummarizeFacts maps the keys ansible-core 2.21 `setup` returns (all prefixed `ansible_`) to a stable summary.
// Missing keys stay zero; malformed entries are skipped rather than failing the whole summary.
func SummarizeFacts(f map[string]any) FactsSummary {
	s := FactsSummary{
		OSFamily:       factString(f, "ansible_os_family"),
		Distribution:   factString(f, "ansible_distribution"),
		Version:        factString(f, "ansible_distribution_version"),
		Kernel:         factString(f, "ansible_kernel"),
		Architecture:   factString(f, "ansible_architecture"),
		Hostname:       factString(f, "ansible_hostname"),
		FQDN:           factString(f, "ansible_fqdn"),
		CPUModel:       factCPUModel(f),
		MemTotalMB:     factInt(f, "ansible_memtotal_mb"),
		SwapTotalMB:    factInt(f, "ansible_swaptotal_mb"),
		DefaultIPv4:    factString(f, "ansible_default_ipv4", "address"),
		UptimeSeconds:  factInt(f, "ansible_uptime_seconds"),
		Python:         factString(f, "ansible_python_version"),
		Virtualization: factString(f, "ansible_virtualization_type"),
		Mounts:         []FactsMount{},
		Interfaces:     []FactsInterface{},
	}
	s.CPUCores = int(factInt(f, "ansible_processor_vcpus"))
	if s.CPUCores == 0 {
		s.CPUCores = int(factInt(f, "ansible_processor_cores"))
	}
	if s.OSFamily != "" || s.Distribution != "" {
		s.OS = strings.TrimSpace(s.Distribution + " " + s.Version)
	}

	if mounts, ok := f["ansible_mounts"].([]any); ok {
		for _, m := range mounts {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			s.Mounts = append(s.Mounts, FactsMount{
				Mount:     factStr(mm["mount"]),
				Device:    factStr(mm["device"]),
				FSType:    factStr(mm["fstype"]),
				SizeTotal: asInt64(mm["size_total"]),
				SizeAvail: asInt64(mm["size_available"]),
			})
		}
	}

	if names, ok := f["ansible_interfaces"].([]any); ok {
		for _, n := range names {
			name := factStr(n)
			if name == "" {
				continue
			}
			raw, ok := f["ansible_"+name].(map[string]any)
			if !ok {
				continue
			}
			iface := FactsInterface{Name: name, Active: raw["active"] == true, MTU: int(asInt64(raw["mtu"]))}
			if v4, ok := raw["ipv4"].(map[string]any); ok {
				iface.IPv4 = factStr(v4["address"])
				iface.MAC = factStr(v4["macaddress"])
			}
			if iface.MAC == "" {
				iface.MAC = factStr(raw["macaddress"])
			}
			if v6, ok := raw["ipv6"].([]any); ok {
				for _, a := range v6 {
					if am, ok := a.(map[string]any); ok {
						if addr := factStr(am["address"]); addr != "" {
							iface.IPv6 = append(iface.IPv6, addr)
						}
					}
				}
			}
			s.Interfaces = append(s.Interfaces, iface)
		}
	}
	return s
}

func factString(f map[string]any, keys ...string) string {
	var cur any = f
	for i, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
		if i == len(keys)-1 {
			return factStr(cur)
		}
	}
	return ""
}

func factInt(f map[string]any, key string) int64 {
	return asInt64(f[key])
}

// factCPUModel reads the model name from `ansible_processor` (["0", "GenuineIntel", "<model>", "1", ...]).
func factCPUModel(f map[string]any) string {
	list, ok := f["ansible_processor"].([]any)
	if !ok {
		return ""
	}
	for i := 2; i < len(list); i += 3 {
		if m := factStr(list[i]); m != "" {
			return m
		}
	}
	return ""
}

func factStr(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}
