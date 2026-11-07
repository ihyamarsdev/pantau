package inspector

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

var (
	// ErrFirewallProtected is returned when a change would remove or block the SSH port Pantau connects on.
	ErrFirewallProtected = errors.New("this rule affects the SSH port Pantau uses to reach the host; change it from a terminal instead")
	// ErrFirewallInvalid wraps every request validation failure (maps to HTTP 400).
	ErrFirewallInvalid = errors.New("invalid firewall request")
)

func fwInvalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrFirewallInvalid, fmt.Sprintf(format, a...))
}

const (
	firewallReadTimeout   = 30 * time.Second
	firewallActionTimeout = 60 * time.Second
	firewallSetupTimeout  = 5 * time.Minute

	fwBackendUfw       = "ufw"
	fwBackendFirewalld = "firewalld"
	fwBackendIptables  = "iptables"
)

// FirewallRule is one rule of any backend. Fields are the subset the backend uses:
// ufw: Index, Action, Direction, Proto, Port, Source; firewalld: Zone, Kind, Value; iptables: Index, Chain,
// Action, Proto, Port, Source, Dest, InIface, OutIface. Raw is the rule as the tool prints it; it is echoed
// back on edit/delete so the host can refuse when the rule moved or changed since it was listed.
type FirewallRule struct {
	Index     int    `json:"index,omitempty"`
	Zone      string `json:"zone,omitempty"`
	Kind      string `json:"kind,omitempty"` // firewalld: service | port | rich
	Chain     string `json:"chain,omitempty"`
	Action    string `json:"action,omitempty"`
	Direction string `json:"direction,omitempty"`
	Proto     string `json:"proto,omitempty"`
	Port      string `json:"port,omitempty"`
	Source    string `json:"source,omitempty"`
	Dest      string `json:"dest,omitempty"`
	InIface   string `json:"in_iface,omitempty"`
	OutIface  string `json:"out_iface,omitempty"`
	Value     string `json:"value,omitempty"` // firewalld service name, "8080/tcp", or rich rule text
	Raw       string `json:"raw,omitempty"`
	Editable  bool   `json:"editable"` // false: shown and deletable, but not expressible in the edit form
}

// FirewallBackend is the live state of one firewall tool on the host.
type FirewallBackend struct {
	ID        string         `json:"id"`
	Installed bool           `json:"installed"`
	Active    bool           `json:"active"`
	Detail    string         `json:"detail,omitempty"`
	Defaults  string         `json:"defaults,omitempty"`
	Error     string         `json:"error,omitempty"` // tool output when the rules could not be read (usually missing root)
	Zones     []string       `json:"zones,omitempty"`
	Rules     []FirewallRule `json:"rules"`
}

// FirewallState is the read side: all three backends plus how the commands were run.
type FirewallState struct {
	Supported bool              `json:"supported"`
	Mode      string            `json:"mode"` // root | sudo | none
	SSHPort   int               `json:"ssh_port"`
	Backends  []FirewallBackend `json:"backends"`
}

// FirewallRequest is one mutation. Rule is the new rule (add/edit); Old is the listed rule (edit/delete).
type FirewallRequest struct {
	Backend string        `json:"backend"`
	Action  string        `json:"action"` // setup | enable | disable | add | edit | delete | persist
	Rule    *FirewallRule `json:"rule,omitempty"`
	Old     *FirewallRule `json:"old,omitempty"`
}

// FirewallActionResult is the outcome of one mutation.
type FirewallActionResult struct {
	Backend  string `json:"backend"`
	Action   string `json:"action"`
	Mode     string `json:"mode"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

func (r *FirewallActionResult) Succeeded() bool { return r.ExitCode == 0 }

const firewallPrelude = `PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin"; export PATH
if [ "$(id -u)" = "0" ]; then S=""; M=root
elif sudo -n true 2>/dev/null; then S="sudo -n"; M=sudo
else S=""; M=none; fi
echo "===MODE:$M==="
`

const firewallReadScript = firewallPrelude + `echo "===OS:$(uname -s)==="
echo "===HAVE:$(for c in ufw firewall-cmd iptables; do command -v $c >/dev/null 2>&1 && printf '%s ' $c; done)==="
if command -v ufw >/dev/null 2>&1; then
echo "===UFW==="; $S ufw status verbose 2>&1 | head -c 20000
echo "===UFW_NUM==="; $S ufw status numbered 2>&1 | head -c 100000
fi
if command -v firewall-cmd >/dev/null 2>&1; then
echo "===FWD_STATE==="; $S firewall-cmd --state 2>&1
echo "===FWD_DEFAULT==="; $S firewall-cmd --get-default-zone 2>&1
echo "===FWD_ACTIVE==="; $S firewall-cmd --get-active-zones 2>&1
echo "===FWD_ZONES==="; $S firewall-cmd --permanent --list-all-zones 2>&1 | head -c 100000
fi
if command -v iptables >/dev/null 2>&1; then
echo "===IPT_VERSION==="; iptables --version 2>&1
echo "===IPT_RULES==="; $S iptables -S 2>&1 | head -c 100000
fi
`

// splitFirewallSections cuts the read script output at ===NAME=== / ===NAME:value=== marker lines.
func splitFirewallSections(out string) map[string]string {
	sec := map[string]string{}
	cur := ""
	var buf []string
	flush := func() {
		if cur != "" {
			sec[cur] = strings.Join(buf, "\n")
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) > 6 && strings.HasPrefix(line, "===") && strings.HasSuffix(line, "===") {
			flush()
			name, val, _ := strings.Cut(line[3:len(line)-3], ":")
			cur, buf = name, nil
			if val != "" {
				buf = []string{val}
			}
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return sec
}

func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// firstLines is a short, single-paragraph excerpt of tool output for the UI error line.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseFirewallState(out string, sshPort int) *FirewallState {
	sec := splitFirewallSections(out)
	st := &FirewallState{SSHPort: sshPort, Mode: strings.TrimSpace(sec["MODE"]), Backends: []FirewallBackend{}}
	if st.Mode == "" {
		st.Mode = "none"
	}
	if strings.TrimSpace(sec["OS"]) != "Linux" {
		return st
	}
	st.Supported = true
	have := strings.Fields(sec["HAVE"])
	has := func(c string) bool {
		for _, h := range have {
			if h == c {
				return true
			}
		}
		return false
	}
	ufw := FirewallBackend{ID: fwBackendUfw, Installed: has("ufw"), Rules: []FirewallRule{}}
	if ufw.Installed {
		parseUfw(&ufw, sec["UFW"], sec["UFW_NUM"])
	}
	fwd := FirewallBackend{ID: fwBackendFirewalld, Installed: has("firewall-cmd"), Rules: []FirewallRule{}}
	if fwd.Installed {
		parseFirewalld(&fwd, sec)
	}
	ipt := FirewallBackend{ID: fwBackendIptables, Installed: has("iptables"), Rules: []FirewallRule{}}
	if ipt.Installed {
		parseIptables(&ipt, sec["IPT_VERSION"], sec["IPT_RULES"])
	}
	st.Backends = append(st.Backends, ufw, fwd, ipt)
	return st
}

// ---- ufw ----

var (
	ufwStatusRe = regexp.MustCompile(`(?m)^Status:\s*(\w+)`)
	// [ 1] 22/tcp                     ALLOW IN    Anywhere
	ufwRuleRe = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.+?)\s{2,}(ALLOW|DENY|REJECT|LIMIT)(?:\s+(IN|OUT|FWD))?\s{2,}(.+?)\s*$`)
	ufwToRe   = regexp.MustCompile(`^(\d+(?::\d+)?)(?:/(tcp|udp))?( \(v6\))?$`)
	ipAddrRe  = regexp.MustCompile(`^[0-9a-fA-F:.]+(?:/\d{1,3})?$`)
)

func parseUfw(b *FirewallBackend, verbose, numbered string) {
	m := ufwStatusRe.FindStringSubmatch(verbose)
	if m == nil {
		b.Error = firstLines(verbose, 3)
		return
	}
	b.Active = m[1] == "active"
	b.Detail = "Status: " + m[1]
	for _, l := range strings.Split(verbose, "\n") {
		if strings.HasPrefix(l, "Default:") {
			b.Defaults = strings.TrimSpace(strings.TrimPrefix(l, "Default:"))
		}
	}
	for _, line := range strings.Split(numbered, "\n") {
		m := ufwRuleRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		r := FirewallRule{
			Index: idx, Action: strings.ToLower(m[3]), Direction: strings.ToLower(m[4]),
			Raw: normSpace(m[2] + " " + m[3] + " " + m[4] + "  " + m[5]),
		}
		if r.Direction == "" {
			r.Direction = "in"
		}
		to, from := m[2], m[5]
		toM := ufwToRe.FindStringSubmatch(to)
		if toM != nil {
			r.Port, r.Proto = toM[1], toM[2]
		}
		v6 := strings.Contains(m[5], "(v6)") || (toM != nil && toM[3] != "")
		from = strings.TrimSpace(strings.NewReplacer("(v6)", "", "(out)", "").Replace(from))
		if strings.EqualFold(from, "Anywhere") {
			from = ""
		}
		r.Source = from
		// Editable only when the row is exactly what the edit form can express; v6 twins, app profiles,
		// interface-bound and forward rows stay deletable but read-only.
		toOK := toM != nil || strings.EqualFold(to, "Anywhere")
		srcOK := from == "" || ipAddrRe.MatchString(from)
		r.Editable = toOK && srcOK && !v6 && r.Direction != "fwd" && (r.Port != "" || r.Source != "")
		b.Rules = append(b.Rules, r)
	}
}

func validPortSpec(p string, sep string) (lo, hi int, ok bool) {
	a, z, isRange := strings.Cut(p, sep)
	lo, err := strconv.Atoi(a)
	if err != nil || lo < 1 || lo > 65535 {
		return 0, 0, false
	}
	hi = lo
	if isRange {
		hi, err = strconv.Atoi(z)
		if err != nil || hi < lo || hi > 65535 {
			return 0, 0, false
		}
	}
	return lo, hi, true
}

func portCovers(p, sep string, port int) bool {
	for _, one := range strings.Split(p, ",") {
		if lo, hi, ok := validPortSpec(strings.TrimSpace(one), sep); ok && port >= lo && port <= hi {
			return true
		}
	}
	return false
}

func validAddr(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// ufwRuleArgs builds `ufw <args>` for add/edit from a structured rule.
func ufwRuleArgs(r FirewallRule) ([]string, error) {
	action := strings.ToLower(r.Action)
	switch action {
	case "allow", "deny", "reject", "limit":
	default:
		return nil, fwInvalid("action must be allow, deny, reject or limit")
	}
	dir := strings.ToLower(r.Direction)
	if dir == "" {
		dir = "in"
	}
	if dir != "in" && dir != "out" {
		return nil, fwInvalid("direction must be in or out")
	}
	proto := strings.ToLower(r.Proto)
	if proto != "" && proto != "tcp" && proto != "udp" {
		return nil, fwInvalid("protocol must be tcp or udp")
	}
	if r.Port != "" {
		if _, _, ok := validPortSpec(r.Port, ":"); !ok {
			return nil, fwInvalid("port must be 1-65535 or a range like 8000:8100")
		}
		if strings.Contains(r.Port, ":") && proto == "" {
			return nil, fwInvalid("a port range needs a protocol (tcp or udp)")
		}
	}
	src := "any"
	if r.Source != "" {
		if !validAddr(r.Source) {
			return nil, fwInvalid("source must be an IP address or CIDR")
		}
		src = r.Source
	}
	if r.Port == "" && src == "any" {
		return nil, fwInvalid("a port or a source address is required")
	}
	args := []string{action, dir}
	if proto != "" {
		args = append(args, "proto", proto)
	}
	args = append(args, "from", src, "to", "any")
	if r.Port != "" {
		args = append(args, "port", r.Port)
	}
	return args, nil
}

// ---- firewalld ----

var (
	fwdNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	fwdZoneRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,17}$`)
	fwdPortRe = regexp.MustCompile(`^(\d{1,5})(?:-(\d{1,5}))?/(tcp|udp|sctp|dccp)$`)
)

func parseFirewalld(b *FirewallBackend, sec map[string]string) {
	state := strings.TrimSpace(sec["FWD_STATE"])
	b.Active = state == "running"
	b.Detail = firstLines(state, 1)
	if !b.Active {
		return
	}
	zonesTxt := sec["FWD_ZONES"]
	if !strings.Contains(zonesTxt, "target:") {
		b.Error = firstLines(zonesTxt, 3)
		return
	}
	def := strings.TrimSpace(sec["FWD_DEFAULT"])
	var active []string
	for _, l := range strings.Split(sec["FWD_ACTIVE"], "\n") {
		if l != "" && l[0] != ' ' && l[0] != '\t' {
			active = append(active, strings.TrimSpace(l))
		}
	}
	b.Defaults = "default zone: " + def
	if len(active) > 0 {
		b.Defaults += "; active: " + strings.Join(active, ", ")
	}
	b.Zones, b.Rules = parseFirewalldZones(zonesTxt)
}

// parseFirewalldZones reads `firewall-cmd --permanent --list-all-zones` into zone names and rules
// (services, ports, rich rules). Other zone properties (sources, interfaces, forwarding) are not rules here.
func parseFirewalldZones(text string) (zones []string, rules []FirewallRule) {
	zones, rules = []string{}, []FirewallRule{}
	zone, inRich := "", false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			zone = strings.Fields(line)[0]
			zones = append(zones, zone)
			inRich = false
			continue
		}
		t := strings.TrimSpace(line)
		if inRich && strings.HasPrefix(t, "rule ") {
			rules = append(rules, FirewallRule{Zone: zone, Kind: "rich", Value: t, Raw: t, Editable: true})
			continue
		}
		key, val, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		inRich = key == "rich rules"
		kind := map[string]string{"services": "service", "ports": "port"}[key]
		if kind == "" {
			continue
		}
		for _, v := range strings.Fields(val) {
			rules = append(rules, FirewallRule{Zone: zone, Kind: kind, Value: v, Raw: v, Editable: true})
		}
	}
	return zones, rules
}

// firewalldFlag returns the firewall-cmd flag ("--add-port=8080/tcp") that adds/removes/queries a rule.
func firewalldFlag(op string, r FirewallRule) (string, error) {
	if !fwdNameRe.MatchString(r.Zone) {
		return "", fwInvalid("zone name is invalid")
	}
	switch r.Kind {
	case "service":
		if !fwdNameRe.MatchString(r.Value) {
			return "", fwInvalid("service name is invalid")
		}
		return "--" + op + "-service=" + r.Value, nil
	case "port":
		m := fwdPortRe.FindStringSubmatch(r.Value)
		if m == nil {
			return "", fwInvalid("port must look like 8080/tcp or 8000-8100/udp")
		}
		for _, p := range m[1:3] {
			if n, _ := strconv.Atoi(p); p != "" && (n < 1 || n > 65535) {
				return "", fwInvalid("port must be 1-65535")
			}
		}
		return "--" + op + "-port=" + r.Value, nil
	case "rich":
		if !strings.HasPrefix(r.Value, "rule ") || len(r.Value) > 500 || strings.ContainsAny(r.Value, "\r\n\x00") {
			return "", fwInvalid(`rich rule must start with "rule " and be a single line`)
		}
		return "--" + op + "-rich-rule=" + r.Value, nil
	}
	return "", fwInvalid("kind must be service, port or rich")
}

// ---- iptables ----

var (
	iptIfaceRe  = regexp.MustCompile(`^[A-Za-z0-9_.@:+-]{1,15}$`)
	iptChainSet = map[string]bool{"INPUT": true, "OUTPUT": true, "FORWARD": true}
)

// splitShellWords splits on spaces, keeping "double quoted" words (iptables -S quotes comments) whole.
func splitShellWords(s string) []string {
	var out []string
	var cur strings.Builder
	inQ, has := false, false
	for _, c := range s {
		switch {
		case c == '"':
			inQ, has = !inQ, true
		case c == ' ' && !inQ:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(c)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

func parseIptables(b *FirewallBackend, version, rules string) {
	b.Detail = strings.TrimSpace(version)
	var policies []string
	perChain := map[string]int{}
	sawPolicy := false
	for _, line := range strings.Split(rules, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "-P "):
			sawPolicy = true
			f := strings.Fields(line)
			if len(f) == 3 && iptChainSet[f[1]] {
				policies = append(policies, f[1]+" "+f[2])
				if f[2] != "ACCEPT" {
					b.Active = true
				}
			}
		case strings.HasPrefix(line, "-A "):
			r, ok := parseIptablesRule(line)
			if !ok || !iptChainSet[r.Chain] {
				continue
			}
			perChain[r.Chain]++
			r.Index = perChain[r.Chain]
			b.Rules = append(b.Rules, r)
			b.Active = true
		}
	}
	b.Defaults = strings.Join(policies, ", ")
	if !sawPolicy {
		b.Error = firstLines(rules, 3)
	}
}

// parseIptablesRule reads one `iptables -S` "-A" line. Editable is true only when every token is one the
// edit form can regenerate (-p/-m tcp|udp/-s/-d/-i/-o/--dport/-j ACCEPT|DROP|REJECT); anything else (conntrack,
// multiport, comments, negation, custom chains) is kept deletable but read-only so an edit never drops it silently.
func parseIptablesRule(line string) (FirewallRule, bool) {
	w := splitShellWords(line)
	if len(w) < 3 || w[0] != "-A" {
		return FirewallRule{}, false
	}
	r := FirewallRule{Chain: w[1], Raw: line, Editable: true}
	for i := 2; i < len(w); i++ {
		next := func() string {
			if i+1 < len(w) {
				i++
				return w[i]
			}
			r.Editable = false
			return ""
		}
		switch w[i] {
		case "-p":
			r.Proto = next()
		case "-m":
			if m := next(); m != r.Proto || (m != "tcp" && m != "udp") {
				r.Editable = false
			}
		case "-s":
			r.Source = next()
		case "-d":
			r.Dest = next()
		case "-i":
			r.InIface = next()
		case "-o":
			r.OutIface = next()
		case "--dport":
			r.Port = next()
		case "-j":
			r.Action = next()
		case "--reject-with":
			// icmp-port-unreachable is what a plain `-j REJECT` prints; any other type is not in the form.
			if next() != "icmp-port-unreachable" {
				r.Editable = false
			}
		default:
			r.Editable = false
		}
	}
	switch r.Action {
	case "ACCEPT", "DROP", "REJECT":
	default:
		r.Editable = false
	}
	return r, true
}

// iptablesRuleArgs builds the match/target part of an iptables rule from a structured rule.
func iptablesRuleArgs(r FirewallRule) ([]string, error) {
	var args []string
	proto := strings.ToLower(r.Proto)
	switch proto {
	case "", "all":
		proto = ""
	case "tcp", "udp", "icmp":
		args = append(args, "-p", proto)
	default:
		return nil, fwInvalid("protocol must be tcp, udp, icmp or all")
	}
	for _, a := range []struct{ flag, val string }{{"-s", r.Source}, {"-d", r.Dest}} {
		if a.val == "" {
			continue
		}
		if !validAddr(a.val) {
			return nil, fwInvalid("%s must be an IP address or CIDR", map[string]string{"-s": "source", "-d": "destination"}[a.flag])
		}
		args = append(args, a.flag, a.val)
	}
	for _, a := range []struct{ flag, val string }{{"-i", r.InIface}, {"-o", r.OutIface}} {
		if a.val == "" {
			continue
		}
		if !iptIfaceRe.MatchString(a.val) {
			return nil, fwInvalid("interface name is invalid")
		}
		args = append(args, a.flag, a.val)
	}
	if r.Port != "" {
		if proto != "tcp" && proto != "udp" {
			return nil, fwInvalid("a port needs protocol tcp or udp")
		}
		if _, _, ok := validPortSpec(r.Port, ":"); !ok {
			return nil, fwInvalid("port must be 1-65535 or a range like 8000:8100")
		}
		args = append(args, "--dport", r.Port)
	}
	if len(args) == 0 {
		return nil, fwInvalid("at least one match (protocol, address, interface or port) is required")
	}
	target := strings.ToUpper(r.Action)
	if target != "ACCEPT" && target != "DROP" && target != "REJECT" {
		return nil, fwInvalid("action must be ACCEPT, DROP or REJECT")
	}
	return append(args, "-j", target), nil
}

// ---- SSH lock-out guard ----

// sshEffect reports whether r allows ("allow") or blocks ("block") inbound SSH from anywhere, or neither.
// Narrower rules (specific source, other port) never count: only an unconditional rule on the SSH port can
// cut Pantau off by itself.
func sshEffect(backend string, r FirewallRule, ssh int) string {
	switch backend {
	case fwBackendUfw:
		if r.Direction == "out" || r.Direction == "fwd" || r.Source != "" {
			return ""
		}
		proto := strings.ToLower(r.Proto)
		covers := (r.Port != "" && (proto == "" || proto == "tcp") && portCovers(r.Port, ":", ssh))
		if f := strings.Fields(r.Raw); len(f) > 0 && (strings.EqualFold(f[0], "openssh") || strings.EqualFold(f[0], "ssh")) {
			covers = true
		}
		if !covers {
			return ""
		}
		if act := strings.ToLower(r.Action); act == "allow" || act == "limit" {
			return "allow"
		}
		return "block"
	case fwBackendIptables:
		if r.Chain != "INPUT" || r.Source != "" || r.InIface == "lo" || r.Port == "" || (r.Proto != "" && !strings.EqualFold(r.Proto, "tcp")) || !portCovers(r.Port, ":", ssh) {
			return ""
		}
		if strings.EqualFold(r.Action, "ACCEPT") {
			return "allow"
		}
		return "block"
	case fwBackendFirewalld:
		switch r.Kind {
		case "service":
			if r.Value == "ssh" && ssh == 22 {
				return "allow"
			}
		case "port":
			if m := fwdPortRe.FindStringSubmatch(r.Value); m != nil && m[3] == "tcp" && portCovers(strings.TrimSuffix(r.Value, "/tcp"), "-", ssh) {
				return "allow"
			}
		case "rich":
			v := strings.ToLower(r.Value)
			if strings.Contains(v, " source ") || !(strings.Contains(v, `service name="ssh"`) || strings.Contains(v, fmt.Sprintf(`port="%d"`, ssh))) {
				return ""
			}
			if strings.HasSuffix(v, " accept") {
				return "allow"
			}
			if strings.HasSuffix(v, " reject") || strings.HasSuffix(v, " drop") {
				return "block"
			}
		}
	}
	return ""
}

func firewallProtected(req FirewallRequest, ssh int) bool {
	eff := func(r *FirewallRule) string {
		if r == nil {
			return ""
		}
		return sshEffect(req.Backend, *r, ssh)
	}
	switch req.Action {
	case "delete":
		return eff(req.Old) == "allow"
	case "add":
		return eff(req.Rule) == "block"
	case "edit":
		return eff(req.Rule) == "block" || (eff(req.Old) == "allow" && eff(req.Rule) != "allow")
	}
	return false
}

// ---- action scripts ----

func sc(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shellQuote(a)
	}
	return "$S " + strings.Join(q, " ")
}

func fwInstallStep(check, apt, rpm, other string) string {
	return `if ! command -v ` + check + ` >/dev/null 2>&1; then
if command -v apt-get >/dev/null 2>&1; then $S env DEBIAN_FRONTEND=noninteractive apt-get install -y ` + apt + `
elif command -v dnf >/dev/null 2>&1; then $S dnf install -y ` + rpm + `
elif command -v yum >/dev/null 2>&1; then $S yum install -y ` + rpm + `
elif command -v zypper >/dev/null 2>&1; then $S zypper --non-interactive install ` + other + `
elif command -v pacman >/dev/null 2>&1; then $S pacman -S --noconfirm ` + other + `
elif command -v apk >/dev/null 2>&1; then $S apk add ` + other + `
else echo "no supported package manager found"; exit 5; fi
fi`
}

// iptablesPersistFn saves the running rules where the distribution reloads them at boot.
const iptablesPersistFn = `persist() {
if command -v netfilter-persistent >/dev/null 2>&1; then $S netfilter-persistent save
elif [ -f /etc/sysconfig/iptables ] || [ -f /etc/redhat-release ]; then F=/etc/sysconfig/iptables
elif [ -f /etc/arch-release ]; then F=/etc/iptables/iptables.rules
elif [ -d /etc/iptables ]; then F=/etc/iptables/rules.v4
else echo "no persistence location found (install iptables-persistent or iptables-services)"; return 4; fi
if [ -n "$F" ]; then R=$($S iptables-save) && printf '%s\n' "$R" | $S tee "$F" >/dev/null && echo "saved to $F"; fi
}
`

func ufwSSHAllow(ssh int) string { return sc("ufw", "allow", strconv.Itoa(ssh)+"/tcp") }

func ufwGuard(old FirewallRule) string {
	return fmt.Sprintf(`cur=$($S ufw status numbered 2>&1 | sed -n 's/^\[ *%d\] *//p' | sed 's/[[:space:]][[:space:]]*/ /g; s/ $//')
[ "$cur" = %s ] || { echo "rule %d changed on the host; refresh the list and retry"; exit 3; }`, old.Index, shellQuote(old.Raw), old.Index)
}

func iptablesGuard(old FirewallRule) string {
	return fmt.Sprintf(`cur=$($S iptables -S %s 2>&1 | grep '^-A ' | sed -n '%dp')
[ "$cur" = %s ] || { echo "rule %d in %s changed on the host; refresh the list and retry"; exit 3; }`, old.Chain, old.Index, shellQuote(old.Raw), old.Index, old.Chain)
}

// buildFirewallSteps validates req and returns the shell steps (run under `set -e`) and their timeout.
func buildFirewallSteps(req FirewallRequest, ssh int) ([]string, time.Duration, error) {
	timeout := firewallActionTimeout
	var steps []string
	needOld := func() error {
		if req.Old == nil || req.Old.Raw == "" {
			return fwInvalid("the listed rule is required")
		}
		return nil
	}
	needRule := func() error {
		if req.Rule == nil {
			return fwInvalid("rule is required")
		}
		return nil
	}
	switch req.Backend {
	case fwBackendUfw:
		switch req.Action {
		case "setup":
			timeout = firewallSetupTimeout
			steps = []string{
				fwInstallStep("ufw", "ufw", "ufw", "ufw"),
				sc("ufw", "default", "deny", "incoming"), sc("ufw", "default", "allow", "outgoing"),
				ufwSSHAllow(ssh), sc("ufw", "--force", "enable"),
			}
		case "enable":
			steps = []string{ufwSSHAllow(ssh), sc("ufw", "--force", "enable")}
		case "disable":
			steps = []string{sc("ufw", "disable")}
		case "add", "edit":
			if err := needRule(); err != nil {
				return nil, 0, err
			}
			args, err := ufwRuleArgs(*req.Rule)
			if err != nil {
				return nil, 0, err
			}
			if req.Action == "edit" {
				if err := needOld(); err != nil {
					return nil, 0, err
				}
				// ufw skips an identical rule, and deleting the old one would then remove the only copy.
				if oldArgs, err := ufwRuleArgs(*req.Old); err == nil && strings.Join(oldArgs, " ") == strings.Join(args, " ") {
					return nil, 0, fwInvalid("nothing changed")
				}
				// New rules are appended, so the old rule keeps its number until it is deleted last.
				steps = []string{ufwGuard(*req.Old), sc(append([]string{"ufw"}, args...)...), sc("ufw", "--force", "delete", strconv.Itoa(req.Old.Index))}
			} else {
				steps = []string{sc(append([]string{"ufw"}, args...)...)}
			}
		case "delete":
			if err := needOld(); err != nil {
				return nil, 0, err
			}
			steps = []string{ufwGuard(*req.Old), sc("ufw", "--force", "delete", strconv.Itoa(req.Old.Index))}
		default:
			return nil, 0, fwInvalid("unsupported action %q for ufw", req.Action)
		}
	case fwBackendFirewalld:
		fc := func(args ...string) string { return sc(append([]string{"firewall-cmd"}, args...)...) }
		enable := []string{
			fc("--state") + " >/dev/null 2>&1 || " + sc("systemctl", "enable", "--now", "firewalld"),
			`for i in 1 2 3 4 5; do ` + fc("--state") + ` >/dev/null 2>&1 && break; sleep 1; done`,
			fc("--permanent", "--add-port="+strconv.Itoa(ssh)+"/tcp"), fc("--reload"),
		}
		switch req.Action {
		case "setup":
			timeout = firewallSetupTimeout
			steps = append([]string{fwInstallStep("firewall-cmd", "firewalld", "firewalld", "firewalld")}, enable...)
		case "enable":
			steps = enable
		case "disable":
			steps = []string{sc("systemctl", "disable", "--now", "firewalld")}
		case "add-zone":
			if err := needRule(); err != nil {
				return nil, 0, err
			}
			zone := req.Rule.Zone
			if !fwdZoneRe.MatchString(zone) {
				return nil, 0, fwInvalid("zone name is invalid (must be 1-17 alphanumeric, underscore, dot, or dash chars)")
			}
			steps = []string{
				fc("--permanent", "--get-zones") + ` | tr ' ' '\n' | grep -qxF -- ` + shellQuote(zone) + ` && { echo "zone already exists"; exit 3; } || true`,
				fc("--permanent", "--new-zone="+zone),
				fc("--reload"),
			}
		case "add", "edit", "delete":
			if req.Action != "delete" {
				if err := needRule(); err != nil {
					return nil, 0, err
				}
			}
			if req.Action != "add" {
				if req.Old == nil {
					return nil, 0, fwInvalid("the listed rule is required")
				}
			}
			if req.Action == "edit" && req.Rule.Zone == req.Old.Zone && req.Rule.Kind == req.Old.Kind && req.Rule.Value == req.Old.Value {
				return nil, 0, fwInvalid("nothing changed")
			}
			if req.Action != "delete" {
				f, err := firewalldFlag("add", *req.Rule)
				if err != nil {
					return nil, 0, err
				}
				steps = append(steps, fc("--permanent", "--zone="+req.Rule.Zone, f))
			}
			if req.Action != "add" {
				q, err := firewalldFlag("query", *req.Old)
				if err != nil {
					return nil, 0, err
				}
				rm, _ := firewalldFlag("remove", *req.Old)
				// Query first so a rule edited elsewhere is not silently "removed" (and, on edit, replaced).
				steps = append([]string{fc("--permanent", "--zone="+req.Old.Zone, q) + ` >/dev/null || { echo "rule is no longer present on the host; refresh the list and retry"; exit 3; }`}, steps...)
				steps = append(steps, fc("--permanent", "--zone="+req.Old.Zone, rm))
			}
			steps = append(steps, fc("--reload"))
		default:
			return nil, 0, fwInvalid("unsupported action %q for firewalld", req.Action)
		}
	case fwBackendIptables:
		portS := strconv.Itoa(ssh)
		switch req.Action {
		case "setup":
			timeout = firewallSetupTimeout
			// Inserted bottom-up so the final order is: loopback, established, SSH.
			steps = []string{
				fwInstallStep("iptables", "iptables iptables-persistent", "iptables iptables-services", "iptables"),
				sc("iptables", "-C", "INPUT", "-p", "tcp", "--dport", portS, "-j", "ACCEPT") + " 2>/dev/null || " + sc("iptables", "-I", "INPUT", "-p", "tcp", "--dport", portS, "-j", "ACCEPT"),
				sc("iptables", "-C", "INPUT", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT") + " 2>/dev/null || " + sc("iptables", "-I", "INPUT", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"),
				sc("iptables", "-C", "INPUT", "-i", "lo", "-j", "ACCEPT") + " 2>/dev/null || " + sc("iptables", "-I", "INPUT", "-i", "lo", "-j", "ACCEPT"),
				sc("iptables", "-P", "INPUT", "DROP"),
				iptablesPersistFn + `persist || echo "NOTE: rules are active but could not be saved to disk"`,
			}
		case "persist":
			steps = []string{iptablesPersistFn + "persist"}
		case "add", "edit":
			if err := needRule(); err != nil {
				return nil, 0, err
			}
			chain := strings.ToUpper(req.Rule.Chain)
			if !iptChainSet[chain] {
				return nil, 0, fwInvalid("chain must be INPUT, OUTPUT or FORWARD")
			}
			args, err := iptablesRuleArgs(*req.Rule)
			if err != nil {
				return nil, 0, err
			}
			if req.Action == "add" {
				if req.Rule.Index > 0 {
					steps = []string{sc(append([]string{"iptables", "-I", chain, strconv.Itoa(req.Rule.Index)}, args...)...)}
				} else {
					steps = []string{sc(append([]string{"iptables", "-A", chain}, args...)...)}
				}
				break
			}
			if err := needOld(); err != nil {
				return nil, 0, err
			}
			if req.Old.Chain != chain || req.Old.Index < 1 {
				return nil, 0, fwInvalid("an existing rule cannot move to another chain")
			}
			steps = []string{iptablesGuard(*req.Old), sc(append([]string{"iptables", "-R", chain, strconv.Itoa(req.Old.Index)}, args...)...)}
		case "delete":
			if err := needOld(); err != nil {
				return nil, 0, err
			}
			if !iptChainSet[req.Old.Chain] || req.Old.Index < 1 {
				return nil, 0, fwInvalid("chain or position is invalid")
			}
			steps = []string{iptablesGuard(*req.Old), sc("iptables", "-D", req.Old.Chain, strconv.Itoa(req.Old.Index))}
		default:
			return nil, 0, fwInvalid("unsupported action %q for iptables", req.Action)
		}
	default:
		return nil, 0, fwInvalid("backend must be ufw, firewalld or iptables")
	}
	return steps, timeout, nil
}

func firewallActionScript(steps []string) string {
	return firewallPrelude + "out=$( (set -e\n" + strings.Join(steps, "\n") + "\n) 2>&1 ); rc=$?\n" +
		`printf '%s\n' "$out" | head -c 8000` + "\n" + `echo "===EXIT:$rc==="` + "\n"
}

func parseFirewallActionOutput(out string, res *FirewallActionResult) {
	res.Mode, res.ExitCode = "none", -1
	var body []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "===MODE:") && strings.HasSuffix(line, "==="):
			res.Mode = strings.TrimSuffix(strings.TrimPrefix(line, "===MODE:"), "===")
		case strings.HasPrefix(line, "===EXIT:") && strings.HasSuffix(line, "==="):
			if rc, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "===EXIT:"), "===")); err == nil {
				res.ExitCode = rc
			}
		default:
			body = append(body, line)
		}
	}
	res.Output = strings.TrimSpace(strings.Join(body, "\n"))
}

func (ins *Inspector) firewallHost(hostID int64) (*store.Host, func(string, time.Duration) (string, string, int, error), func(), error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return nil, nil, nil, fmt.Errorf("host not found")
	}
	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, nil, nil, err
	}
	return host, runner.ExecTimeout, func() { runner.Close() }, nil
}

func (ins *Inspector) noteFirewallMode(hostID int64, mode string) {
	switch mode {
	case "sudo":
		ins.noteSudo(hostID, false)
	case "none":
		ins.noteSudo(hostID, true)
	}
}

// FirewallState reads ufw, firewalld and iptables on the host. Like systemd actions it is an explicit
// administrator request, so sudo is always tried (root, else passwordless sudo, else the plain user).
func (ins *Inspector) FirewallState(hostID int64) (*FirewallState, error) {
	host, exec, closeFn, err := ins.firewallHost(hostID)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	stdout, stderr, exit, err := exec("sh -c "+shellQuote(firewallReadScript), firewallReadTimeout)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stdout) == "" {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = fmt.Sprintf("firewall read failed (exit %d)", exit)
		}
		return nil, errors.New(msg)
	}
	st := parseFirewallState(stdout, host.Port)
	ins.noteFirewallMode(hostID, st.Mode)
	return st, nil
}

// FirewallApply validates and runs one firewall change, and writes it to the activity log.
func (ins *Inspector) FirewallApply(hostID int64, req FirewallRequest) (*FirewallActionResult, error) {
	host, exec, closeFn, err := ins.firewallHost(hostID)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	steps, timeout, err := buildFirewallSteps(req, host.Port)
	if err != nil {
		return nil, err
	}
	if firewallProtected(req, host.Port) {
		return nil, ErrFirewallProtected
	}
	res := &FirewallActionResult{Backend: req.Backend, Action: req.Action}
	stdout, stderr, _, err := exec("sh -c "+shellQuote(firewallActionScript(steps)), timeout)
	if err != nil {
		logger.Warn("firewall", host.Name, fmt.Sprintf("%s %s failed: %v", req.Backend, req.Action, err))
		return nil, err
	}
	parseFirewallActionOutput(stdout, res)
	if res.ExitCode == -1 && strings.TrimSpace(stderr) != "" {
		res.Output = strings.TrimSpace(stderr)
	}
	ins.noteFirewallMode(hostID, res.Mode)
	if res.Succeeded() {
		logger.Info("firewall", host.Name, fmt.Sprintf("%s %s (via %s) ok", req.Backend, req.Action, res.Mode))
	} else {
		logger.Warn("firewall", host.Name, fmt.Sprintf("%s %s (via %s) failed with exit %d: %s", req.Backend, req.Action, res.Mode, res.ExitCode, shortEvidence(res.Output)))
	}
	return res, nil
}
