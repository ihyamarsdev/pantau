package inspector

// Compose stack management on a Host: listing, file access and deploy operations.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"pantau/internal/logger"
)

// ---------------------------------------------------------------- compose stacks

// DockerStack is a Compose project reconstructed from container labels.
type DockerStack struct {
	Name        string   `json:"name"`
	WorkingDir  string   `json:"working_dir"`
	ConfigFiles []string `json:"config_files"`
	Services    []string `json:"services"`
	Total       int      `json:"total"`
	Running     int      `json:"running"`
	Status      string   `json:"status"` // running | partial | stopped | down (no containers; remembered from an earlier listing)
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

// DockerListStacks returns Compose projects found among the host's containers, plus remembered ones whose
// containers are gone (after `compose down`) with status "down".
func (ins *Inspector) DockerListStacks(hostID int64) ([]DockerStack, error) {
	stdout, stderr, exitCode, err := ins.dockerExec(hostID, dockerStacksScript, dockerScriptTimeout)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, dockerCommandError("docker ps", stdout, stderr, exitCode)
	}
	return ins.mergeKnownStacks(hostID, parseDockerStacks(stdout)), nil
}

// mergeKnownStacks records every live stack that has a compose file and appends the remembered stacks that no
// longer have containers. A store failure never hides the live stacks.
func (ins *Inspector) mergeKnownStacks(hostID int64, live []DockerStack) []DockerStack {
	known, err := ins.db.GetDockerStacks(hostID)
	if err != nil {
		logger.Warn("docker", "", fmt.Sprintf("read remembered stacks of host %d: %v", hostID, err))
		return live
	}
	seen := make(map[string]bool, len(live))
	for _, st := range live {
		seen[st.Name] = true
		if len(st.ConfigFiles) == 0 {
			continue
		}
		rec, _ := json.Marshal(DockerStack{Name: st.Name, WorkingDir: st.WorkingDir, ConfigFiles: st.ConfigFiles, Services: st.Services})
		if known[st.Name] != string(rec) {
			if err := ins.db.SaveDockerStack(hostID, st.Name, string(rec)); err != nil {
				logger.Warn("docker", "", fmt.Sprintf("remember stack %q of host %d: %v", st.Name, hostID, err))
			}
		}
	}
	for name, data := range known {
		var st DockerStack
		if seen[name] || json.Unmarshal([]byte(data), &st) != nil {
			continue
		}
		st.Status = "down"
		live = append(live, st)
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].Name < live[j].Name })
	return live
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

// Actions on existing containers must see profile-gated services too (compose ignores them otherwise, so `down`
// silently does nothing). up/pull/recreate/update keep the default profiles so they never start extra services.
var dockerStackAllProfiles = map[string]bool{"down": true, "start": true, "stop": true, "restart": true}

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
	if dockerStackAllProfiles[action] {
		base = `$C --profile '*' -p ` + shellQuote(st.Name) + flags
	}
	cmds := make([]string, len(subs))
	for i, s := range subs {
		cmds[i] = base + " " + s
	}
	return composePrelude + composeSudoProbe(st.ConfigFiles) + cd + "\n" + strings.Join(cmds, " && ") + "\n", nil
}

// composeSudoProbe makes `docker` run through passwordless sudo when the SSH user cannot read a compose file,
// traverse its directory or read the sibling .env. Compose parses these client-side, so being in the docker
// group (socket access) is not enough; dockerSudoPrelude only covers a refused socket. Without sudo the plain
// run goes ahead and reports the real permission error.
func composeSudoProbe(files []string) string {
	var b strings.Builder
	for _, f := range files {
		q := shellQuote(f)
		b.WriteString(`f=` + q + `; d=${f%/*}; d=${d:-/}
if [ ! -r "$f" ] || [ ! -x "$d" ] || { [ -e "$d/.env" ] && [ ! -r "$d/.env" ]; }; then
  if sudo -n true 2>/dev/null; then docker() { sudo -n docker "$@"; }; fi
fi
`)
	}
	return b.String()
}

// DockerStackOp runs a Compose action (up, down, start, stop, restart, pull, recreate, update) on a project. A
// non-nil live receives the output as it is produced (and the returned output is empty).
func (ins *Inspector) DockerStackOp(hostID int64, name, action string, live io.Writer) (string, error) {
	st, err := ins.dockerFindStack(hostID, name)
	if err != nil {
		return "", err
	}
	if action == "forget" {
		if st.Status != "down" {
			return "", dockerInvalid("stack %q still has containers; take it down first", name)
		}
		return "", ins.db.DeleteDockerStack(hostID, name)
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
