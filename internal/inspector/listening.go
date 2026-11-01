package inspector

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

const (
	// sudoRetryAfter bounds how often passwordless sudo is probed on a host where it failed, so a host that
	// requires a sudo password records at most one failed attempt per interval in its auth log.
	sudoRetryAfter       = 24 * time.Hour
	listenEnrichTimeout  = 15 * time.Second
	listenMarkerRoot     = "===ROOT==="
	listenMarkerSudo     = "===SUDO==="
	listenMarkerNoPriv   = "===NOPRIV==="
	listenMarkerOK       = "===OK==="
	listenMarkerSudoFail = "===SUDOFAIL==="
)

// wellKnownPortServices names the usual service of a port. It is only a last-resort hint for sockets whose
// owner process is not visible to the SSH user; the UI labels it as a guess, never as the running process.
var wellKnownPortServices = map[string]string{
	"21": "ftp", "22": "ssh", "23": "telnet", "25": "smtp", "53": "dns", "80": "http", "110": "pop3",
	"123": "ntp", "143": "imap", "389": "ldap", "443": "https", "465": "smtps", "514": "syslog",
	"587": "smtp-submission", "631": "cups", "636": "ldaps", "873": "rsync", "993": "imaps", "995": "pop3s",
	"1433": "mssql", "1521": "oracle", "2049": "nfs", "2375": "docker-api", "2376": "docker-api-tls",
	"3306": "mysql", "3389": "rdp", "5432": "postgresql", "5672": "rabbitmq", "5900": "vnc",
	"6379": "redis", "8080": "http-alt", "8443": "https-alt", "9090": "prometheus", "9100": "node-exporter",
	"9200": "elasticsearch", "11211": "memcached", "15672": "rabbitmq-mgmt", "27017": "mongodb",
}

// shQuote single-quotes s for a POSIX shell so the script runs under `sh` regardless of the login shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// linuxListenOwnerScript lists listening sockets with their owner process. Plain `ss -p` only reveals the SSH
// user's own processes; with passwordless sudo (tried only when allowed) it reveals every owner. Root needs no
// help: the main metrics batch already ran as root.
func linuxListenOwnerScript(trySudo bool) string {
	try := "0"
	if trySudo {
		try = "1"
	}
	return `TRY=` + try + `
if [ "$(id -u)" = "0" ]; then echo "` + listenMarkerRoot + `"; exit 0
elif [ "$TRY" = "1" ] && sudo -n true 2>/dev/null; then S="sudo -n"; echo "` + listenMarkerSudo + `"
else echo "` + listenMarkerNoPriv + `"; exit 0; fi
($S ss -tlpn 2>/dev/null || $S netstat -tlpn 2>/dev/null) | awk '!/State|Proto|Active/ {print $1, $4, $6, $7}' | head -n 200
`
}

// darwinListenOwnerScript uses lsof, which (unlike the netstat used by the macOS batch) knows the owner. It
// works for the SSH user's own processes, and for all of them as root or with passwordless sudo.
func darwinListenOwnerScript(trySudo bool) string {
	try := "0"
	if trySudo {
		try = "1"
	}
	return DarwinPathPrelude + `TRY=` + try + `
S=""
if [ "$(id -u)" != "0" ] && [ "$TRY" = "1" ]; then
  if sudo -n true 2>/dev/null; then S="sudo -n"; else echo "` + listenMarkerSudoFail + `"; fi
fi
echo "` + listenMarkerOK + `"
$S lsof +c 0 -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk 'NR>1 && NF>=2 {print "tcp", $(NF-1), $1}' | head -n 200
`
}

type listenOwnerResult struct {
	owners     map[string]string // port -> owner process name(s)
	ok         bool              // the script ran far enough to report owners
	noPriv     bool              // non-root and passwordless sudo skipped or refused (Linux)
	sudoFailed bool              // passwordless sudo was attempted and refused (macOS)
	sudoUsed   bool
}

func portFromAddr(addr string) string {
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return ""
	}
	port := addr[i+1:]
	if _, err := strconv.Atoi(port); err != nil {
		return ""
	}
	return port
}

// parseListenOwnerOutput reads the enrichment script output: marker lines followed by rows in the same
// "<proto> <addr> <owner columns...>" layout as the main batch.
func parseListenOwnerOutput(out string) listenOwnerResult {
	res := listenOwnerResult{owners: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "":
			continue
		case listenMarkerRoot:
			return res
		case listenMarkerNoPriv:
			res.noPriv = true
			return res
		case listenMarkerSudo:
			res.ok, res.sudoUsed = true, true
			continue
		case listenMarkerOK:
			res.ok = true
			continue
		case listenMarkerSudoFail:
			res.sudoFailed = true
			continue
		}
		if !res.ok {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		port := portFromAddr(parts[1])
		name := extractListenProcess(parts[2:])
		if name == "" && !strings.HasPrefix(parts[2], "users:") && !strings.Contains(parts[2], "/") && parts[2] != "LISTEN" && parts[2] != "-" {
			name = parts[2] // lsof row: "tcp <addr> <command>"
		}
		if port == "" || name == "" {
			continue
		}
		if prev := res.owners[port]; prev == "" {
			res.owners[port] = name
		} else if !containsName(prev, name) {
			res.owners[port] = prev + ", " + name
		}
	}
	return res
}

func containsName(list, name string) bool {
	for _, n := range strings.Split(list, ", ") {
		if n == name {
			return true
		}
	}
	return false
}

func (ins *Inspector) sudoProbeAllowed(hostID int64) bool {
	ins.sudoMu.Lock()
	defer ins.sudoMu.Unlock()
	return !time.Now().Before(ins.sudoRetryAt[hostID])
}

func (ins *Inspector) noteSudo(hostID int64, failed bool) {
	ins.sudoMu.Lock()
	defer ins.sudoMu.Unlock()
	if ins.sudoRetryAt == nil {
		ins.sudoRetryAt = map[int64]time.Time{}
	}
	if failed {
		ins.sudoRetryAt[hostID] = time.Now().Add(sudoRetryAfter)
	} else {
		delete(ins.sudoRetryAt, hostID)
	}
}

// enrichListeningProcesses names the process behind every listening port the main batch could not attribute.
// Order of attempts: owner visible to the SSH user, then passwordless sudo (root sees everything anyway),
// then a clearly-labelled well-known-port hint.
func (ins *Inspector) enrichListeningProcesses(h *store.Host, runner sshrunner.Runner) {
	var ports []store.ListeningPort
	if err := json.Unmarshal([]byte(h.ListeningPorts), &ports); err != nil || len(ports) == 0 {
		return
	}
	unnamed := false
	for _, p := range ports {
		if p.Process == "" {
			unnamed = true
			break
		}
	}
	if !unnamed {
		return
	}

	darwin := strings.Contains(strings.ToLower(h.OSInfo), "macos")
	try := ins.sudoProbeAllowed(h.ID)
	script := linuxListenOwnerScript(try)
	if darwin {
		script = darwinListenOwnerScript(try)
	}
	owners := map[string]string{}
	if out, _, _, err := runner.ExecTimeout("sh -c "+shQuote(script), listenEnrichTimeout); err == nil {
		res := parseListenOwnerOutput(out)
		owners = res.owners
		switch {
		case res.sudoUsed:
			ins.noteSudo(h.ID, false)
		case res.sudoFailed || (res.noPriv && try):
			ins.noteSudo(h.ID, true)
			logger.Info("inspect", h.Name, "passwordless sudo unavailable; listening ports show only the SSH user's own processes")
		}
	}

	for i := range ports {
		if ports[i].Process != "" {
			continue
		}
		if name := owners[ports[i].Port]; name != "" {
			ports[i].Process, ports[i].ProcessSource = name, "process"
		} else if hint := wellKnownPortServices[ports[i].Port]; hint != "" {
			ports[i].Process, ports[i].ProcessSource = hint, "port_hint"
		}
	}
	if b, err := json.Marshal(ports); err == nil {
		h.ListeningPorts = string(b)
	}
}
