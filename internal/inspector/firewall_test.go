package inspector

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const ufwNumberedFixture = `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80                         ALLOW IN    192.168.1.0/24
[ 3] Anywhere                   DENY IN     10.0.0.5
[ 4] OpenSSH                    ALLOW IN    Anywhere
[ 5] 22/tcp (v6)                ALLOW IN    Anywhere (v6)
[ 6] 53                         ALLOW OUT   Anywhere (out)
[ 7] 443/tcp                    LIMIT IN    Anywhere
`

func TestParseUfw(t *testing.T) {
	b := FirewallBackend{ID: "ufw", Rules: []FirewallRule{}}
	parseUfw(&b, "Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n", ufwNumberedFixture)
	if !b.Active || b.Defaults != "deny (incoming), allow (outgoing), disabled (routed)" {
		t.Fatalf("status/defaults: %+v", b)
	}
	if len(b.Rules) != 7 {
		t.Fatalf("want 7 rules, got %d: %+v", len(b.Rules), b.Rules)
	}
	r := b.Rules
	if r[0].Index != 1 || r[0].Port != "22" || r[0].Proto != "tcp" || r[0].Action != "allow" || r[0].Direction != "in" || r[0].Source != "" || !r[0].Editable {
		t.Errorf("rule 1: %+v", r[0])
	}
	if r[0].Raw != "22/tcp ALLOW IN Anywhere" {
		t.Errorf("rule 1 raw: %q", r[0].Raw)
	}
	if r[1].Port != "80" || r[1].Source != "192.168.1.0/24" || !r[1].Editable {
		t.Errorf("rule 2: %+v", r[1])
	}
	if r[2].Port != "" || r[2].Source != "10.0.0.5" || r[2].Action != "deny" || !r[2].Editable {
		t.Errorf("rule 3: %+v", r[2])
	}
	if r[3].Editable { // app profile: not expressible in the form
		t.Errorf("app profile must be read-only: %+v", r[3])
	}
	if r[4].Editable { // v6 twin
		t.Errorf("v6 row must be read-only: %+v", r[4])
	}
	if r[5].Direction != "out" || r[5].Source != "" {
		t.Errorf("out rule: %+v", r[5])
	}
	if r[6].Action != "limit" {
		t.Errorf("limit rule: %+v", r[6])
	}

	denied := FirewallBackend{Rules: []FirewallRule{}}
	parseUfw(&denied, "ERROR: You need to be root to run this script\n", "")
	if denied.Active || !strings.Contains(denied.Error, "need to be root") {
		t.Errorf("permission error not surfaced: %+v", denied)
	}
}

const firewalldZonesFixture = `public (default)
  target: default
  icmp-block-inversion: no
  interfaces:
  sources:
  services: dhcpv6-client ssh
  ports: 8080/tcp 9000-9100/udp
  protocols:
  forward: yes
  masquerade: no
  forward-ports:
  source-ports:
  icmp-blocks:
  rich rules:
	rule family="ipv4" source address="10.1.1.1" reject
	rule family="ipv6" source address="fe80::1" accept
trusted
  target: ACCEPT
  services:
  ports:
  rich rules:
`

func TestParseFirewalldZones(t *testing.T) {
	zones, rules := parseFirewalldZones(firewalldZonesFixture)
	if strings.Join(zones, ",") != "public,trusted" {
		t.Fatalf("zones: %v", zones)
	}
	want := []struct{ kind, value string }{
		{"service", "dhcpv6-client"}, {"service", "ssh"}, {"port", "8080/tcp"}, {"port", "9000-9100/udp"},
		{"rich", `rule family="ipv4" source address="10.1.1.1" reject`},
		{"rich", `rule family="ipv6" source address="fe80::1" accept`},
	}
	if len(rules) != len(want) {
		t.Fatalf("want %d rules, got %d: %+v", len(want), len(rules), rules)
	}
	for i, w := range want {
		if rules[i].Kind != w.kind || rules[i].Value != w.value || rules[i].Zone != "public" {
			t.Errorf("rule %d: %+v", i, rules[i])
		}
	}
}

const iptablesFixture = `-P INPUT DROP
-P FORWARD DROP
-P OUTPUT ACCEPT
-N DOCKER
-A INPUT -i lo -j ACCEPT
-A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
-A INPUT -s 10.0.0.0/8 -p tcp -m tcp --dport 22 -j ACCEPT
-A INPUT -p tcp -m tcp --dport 80 -m comment --comment "web server" -j ACCEPT
-A FORWARD -j DOCKER
-A DOCKER -p tcp -m tcp --dport 8080 -j ACCEPT
-A OUTPUT -p udp -m udp --dport 53 -j ACCEPT
-A OUTPUT -p tcp -m tcp --dport 81 -j REJECT --reject-with icmp-port-unreachable
-A OUTPUT -p tcp -m tcp --dport 82 -j REJECT --reject-with tcp-reset
`

func TestParseIptables(t *testing.T) {
	b := FirewallBackend{ID: "iptables", Rules: []FirewallRule{}}
	parseIptables(&b, "iptables v1.8.10 (nf_tables)\n", iptablesFixture)
	if !b.Active || b.Defaults != "INPUT DROP, FORWARD DROP, OUTPUT ACCEPT" || b.Error != "" {
		t.Fatalf("backend: %+v", b)
	}
	if len(b.Rules) != 8 { // DOCKER chain rule is not listed
		t.Fatalf("want 8 rules, got %d: %+v", len(b.Rules), b.Rules)
	}
	r := b.Rules
	if r[0].Chain != "INPUT" || r[0].Index != 1 || r[0].InIface != "lo" || !r[0].Editable {
		t.Errorf("rule 0: %+v", r[0])
	}
	if r[1].Editable { // conntrack
		t.Errorf("conntrack rule must be read-only: %+v", r[1])
	}
	if r[2].Index != 3 || r[2].Source != "10.0.0.0/8" || r[2].Proto != "tcp" || r[2].Port != "22" || r[2].Action != "ACCEPT" || !r[2].Editable {
		t.Errorf("rule 2: %+v", r[2])
	}
	if r[3].Editable { // comment would be dropped by an edit
		t.Errorf("commented rule must be read-only: %+v", r[3])
	}
	if r[4].Chain != "FORWARD" || r[4].Action != "DOCKER" || r[4].Editable || r[4].Index != 1 {
		t.Errorf("forward jump: %+v", r[4])
	}
	if r[5].Chain != "OUTPUT" || r[5].Index != 1 || r[5].Port != "53" {
		t.Errorf("output rule: %+v", r[5])
	}
	if r[6].Index != 2 || r[6].Action != "REJECT" || !r[6].Editable { // our own REJECT rules stay editable
		t.Errorf("reject rule: %+v", r[6])
	}
	if r[7].Action != "REJECT" || r[7].Editable {
		t.Errorf("non-default reject type must be read-only: %+v", r[7])
	}

	denied := FirewallBackend{Rules: []FirewallRule{}}
	parseIptables(&denied, "iptables v1.8\n", "iptables v1.8.10: can't initialize iptables table `filter': Permission denied (you must be root)\n")
	if denied.Error == "" || denied.Active {
		t.Errorf("permission error not surfaced: %+v", denied)
	}
}

func TestParseFirewallState_Sections(t *testing.T) {
	out := "===MODE:sudo===\n===OS:Linux===\n===HAVE:ufw iptables ===\n===UFW===\nStatus: inactive\n===UFW_NUM===\nStatus: inactive\n" +
		"===IPT_VERSION===\niptables v1.8.10 (legacy)\n===IPT_RULES===\n-P INPUT ACCEPT\n-P FORWARD ACCEPT\n-P OUTPUT ACCEPT\n"
	st := parseFirewallState(out, 2222)
	if !st.Supported || st.Mode != "sudo" || st.SSHPort != 2222 || len(st.Backends) != 3 {
		t.Fatalf("state: %+v", st)
	}
	if !st.Backends[0].Installed || st.Backends[0].Active {
		t.Errorf("ufw: %+v", st.Backends[0])
	}
	if st.Backends[1].Installed {
		t.Errorf("firewalld must be reported as not installed: %+v", st.Backends[1])
	}
	if !st.Backends[2].Installed || st.Backends[2].Active {
		t.Errorf("iptables with ACCEPT policy and no rules is inactive: %+v", st.Backends[2])
	}
	if mac := parseFirewallState("===MODE:root===\n===OS:Darwin===\n", 22); mac.Supported {
		t.Error("non-Linux host must be unsupported")
	}
}

func TestFirewallRuleValidation(t *testing.T) {
	bad := []FirewallRequest{
		{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "allow"}},                                    // no port/source
		{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "allow", Port: "80; reboot"}},                // injection
		{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "allow", Port: "8000:8100"}},                 // range w/o proto
		{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "allow", Port: "80", Source: "1.2.3.4; id"}}, // bad source
		{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "permit", Port: "80"}},                       // bad action
		{Backend: "ufw", Action: "delete"}, // no old rule
		{Backend: "ufw", Action: "edit", Rule: &FirewallRule{Action: "allow", Port: "80"}, Old: &FirewallRule{Index: 2, Action: "allow", Port: "80", Direction: "in", Raw: "80 ALLOW IN Anywhere"}}, // unchanged
		{Backend: "firewalld", Action: "add", Rule: &FirewallRule{Zone: "public", Kind: "port", Value: "80"}},                                                                                       // no proto
		{Backend: "firewalld", Action: "add", Rule: &FirewallRule{Zone: "pub lic", Kind: "service", Value: "http"}},                                                                                 // bad zone
		{Backend: "firewalld", Action: "add", Rule: &FirewallRule{Zone: "public", Kind: "rich", Value: "reject all"}},                                                                               // not a rule
		{Backend: "firewalld", Action: "edit", Rule: &FirewallRule{Zone: "public", Kind: "service", Value: "http"}, Old: &FirewallRule{Zone: "public", Kind: "service", Value: "http", Raw: "http"}},
		{Backend: "iptables", Action: "add", Rule: &FirewallRule{Chain: "DOCKER", Action: "ACCEPT", Proto: "tcp", Port: "80"}},
		{Backend: "iptables", Action: "add", Rule: &FirewallRule{Chain: "INPUT", Action: "ACCEPT"}},              // matches everything
		{Backend: "iptables", Action: "add", Rule: &FirewallRule{Chain: "INPUT", Action: "ACCEPT", Port: "80"}},  // port w/o proto
		{Backend: "iptables", Action: "add", Rule: &FirewallRule{Chain: "INPUT", Action: "ACCEPT", Source: "x"}}, // bad source
		{Backend: "iptables", Action: "disable"},
		{Backend: "nft", Action: "add"},
	}
	for i, req := range bad {
		if _, _, err := buildFirewallSteps(req, 22); !errors.Is(err, ErrFirewallInvalid) {
			t.Errorf("case %d (%s %s): want ErrFirewallInvalid, got %v", i, req.Backend, req.Action, err)
		}
	}
}

func TestFirewallSteps_Content(t *testing.T) {
	steps, _, err := buildFirewallSteps(FirewallRequest{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "ALLOW", Proto: "tcp", Port: "8080", Source: "10.0.0.0/8"}}, 22)
	if err != nil || len(steps) != 1 || steps[0] != "$S 'ufw' 'allow' 'in' 'proto' 'tcp' 'from' '10.0.0.0/8' 'to' 'any' 'port' '8080'" {
		t.Errorf("ufw add: %v %q", err, steps)
	}

	// iptables edit replaces in place (-R) after verifying the listed rule is still at that position.
	old := FirewallRule{Chain: "INPUT", Index: 3, Raw: "-A INPUT -p tcp -m tcp --dport 80 -j ACCEPT"}
	steps, _, err = buildFirewallSteps(FirewallRequest{Backend: "iptables", Action: "edit", Old: &old, Rule: &FirewallRule{Chain: "INPUT", Action: "DROP", Proto: "tcp", Port: "80"}}, 22)
	if err != nil || len(steps) != 2 || !strings.Contains(steps[0], "sed -n '3p'") || steps[1] != "$S 'iptables' '-R' 'INPUT' '3' '-p' 'tcp' '--dport' '80' '-j' 'DROP'" {
		t.Errorf("iptables edit: %v %q", err, steps)
	}
	if _, _, err = buildFirewallSteps(FirewallRequest{Backend: "iptables", Action: "edit", Old: &old, Rule: &FirewallRule{Chain: "OUTPUT", Action: "DROP", Proto: "tcp", Port: "80"}}, 22); !errors.Is(err, ErrFirewallInvalid) {
		t.Errorf("moving a rule between chains must be refused, got %v", err)
	}

	// firewalld edit: verify old exists, add new, remove old, reload.
	steps, _, err = buildFirewallSteps(FirewallRequest{Backend: "firewalld", Action: "edit",
		Old:  &FirewallRule{Zone: "public", Kind: "port", Value: "8080/tcp"},
		Rule: &FirewallRule{Zone: "public", Kind: "port", Value: "9090/tcp"}}, 22)
	if err != nil || len(steps) != 4 ||
		!strings.Contains(steps[0], "--query-port=8080/tcp") || !strings.Contains(steps[1], "--add-port=9090/tcp") ||
		!strings.Contains(steps[2], "--remove-port=8080/tcp") || !strings.Contains(steps[3], "--reload") {
		t.Errorf("firewalld edit: %v %q", err, steps)
	}

	// A rich rule is passed as one quoted word, never interpreted by the shell.
	steps, _, err = buildFirewallSteps(FirewallRequest{Backend: "firewalld", Action: "add",
		Rule: &FirewallRule{Zone: "public", Kind: "rich", Value: `rule family="ipv4" source address="1.2.3.4" reject'; reboot; '`}}, 22)
	if err != nil || !strings.Contains(steps[0], `'--add-rich-rule=rule family="ipv4" source address="1.2.3.4" reject'\''; reboot; '\'''`) {
		t.Errorf("rich rule quoting: %v %q", err, steps)
	}
}

func TestFirewallProtected(t *testing.T) {
	ufwSSH := &FirewallRule{Index: 1, Action: "allow", Direction: "in", Proto: "tcp", Port: "22", Raw: "22/tcp ALLOW IN Anywhere"}
	cases := []struct {
		name string
		req  FirewallRequest
		want bool
	}{
		{"delete ufw ssh allow", FirewallRequest{Backend: "ufw", Action: "delete", Old: ufwSSH}, true},
		{"delete ufw OpenSSH profile", FirewallRequest{Backend: "ufw", Action: "delete", Old: &FirewallRule{Index: 4, Action: "allow", Raw: "OpenSSH ALLOW IN Anywhere"}}, true},
		{"delete ufw http", FirewallRequest{Backend: "ufw", Action: "delete", Old: &FirewallRule{Index: 2, Action: "allow", Port: "80", Raw: "80 ALLOW IN Anywhere"}}, false},
		{"delete ufw ssh allow from one subnet", FirewallRequest{Backend: "ufw", Action: "delete", Old: &FirewallRule{Index: 2, Action: "allow", Port: "22", Source: "10.0.0.0/8", Raw: "22 ALLOW IN 10.0.0.0/8"}}, false},
		{"add ufw deny ssh", FirewallRequest{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "deny", Port: "20:30", Proto: "tcp"}}, true},
		{"add ufw deny ssh from one host", FirewallRequest{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "deny", Port: "22", Source: "1.2.3.4"}}, false},
		{"add ufw allow ssh", FirewallRequest{Backend: "ufw", Action: "add", Rule: &FirewallRule{Action: "allow", Port: "22"}}, false},
		{"edit ssh allow to other port", FirewallRequest{Backend: "ufw", Action: "edit", Old: ufwSSH, Rule: &FirewallRule{Action: "allow", Port: "2200"}}, true},
		{"edit ssh allow keeping ssh", FirewallRequest{Backend: "ufw", Action: "edit", Old: ufwSSH, Rule: &FirewallRule{Action: "limit", Port: "22", Proto: "tcp"}}, false},
		{"delete iptables ssh accept", FirewallRequest{Backend: "iptables", Action: "delete", Old: &FirewallRule{Chain: "INPUT", Index: 2, Proto: "tcp", Port: "22", Action: "ACCEPT", Raw: "x"}}, true},
		{"delete iptables output rule", FirewallRequest{Backend: "iptables", Action: "delete", Old: &FirewallRule{Chain: "OUTPUT", Index: 1, Proto: "tcp", Port: "22", Action: "ACCEPT", Raw: "x"}}, false},
		{"add iptables drop ssh", FirewallRequest{Backend: "iptables", Action: "add", Rule: &FirewallRule{Chain: "INPUT", Proto: "tcp", Port: "22", Action: "DROP"}}, true},
		{"delete firewalld ssh service", FirewallRequest{Backend: "firewalld", Action: "delete", Old: &FirewallRule{Zone: "public", Kind: "service", Value: "ssh", Raw: "ssh"}}, true},
		{"delete firewalld ssh port range", FirewallRequest{Backend: "firewalld", Action: "delete", Old: &FirewallRule{Zone: "public", Kind: "port", Value: "20-30/tcp", Raw: "x"}}, true},
		{"delete firewalld http", FirewallRequest{Backend: "firewalld", Action: "delete", Old: &FirewallRule{Zone: "public", Kind: "service", Value: "http", Raw: "http"}}, false},
		{"add firewalld rich reject ssh", FirewallRequest{Backend: "firewalld", Action: "add", Rule: &FirewallRule{Zone: "public", Kind: "rich", Value: `rule service name="ssh" reject`}}, true},
		{"add firewalld rich reject ssh from host", FirewallRequest{Backend: "firewalld", Action: "add", Rule: &FirewallRule{Zone: "public", Kind: "rich", Value: `rule family="ipv4" source address="1.2.3.4" service name="ssh" reject`}}, false},
		{"disable ufw", FirewallRequest{Backend: "ufw", Action: "disable"}, false},
	}
	for _, c := range cases {
		if got := firewallProtected(c.req, 22); got != c.want {
			t.Errorf("%s: protected=%v, want %v", c.name, got, c.want)
		}
	}
	// A non-default SSH port is what is protected, not 22.
	if firewallProtected(FirewallRequest{Backend: "ufw", Action: "delete", Old: ufwSSH}, 2222) {
		t.Error("port 22 is not protected when the host is reached on 2222")
	}
}

// runFakeUfw executes a real generated script with a fake ufw (and a failing sudo, so the mode is root or
// none and commands run unprivileged) and returns the arguments ufw received plus the parsed result.
func runFakeUfw(t *testing.T, req FirewallRequest) (calls string, res *FirewallActionResult) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	fake := "#!/bin/sh\n" +
		"if [ \"$1 $2\" = \"status numbered\" ]; then cat <<'EOF'\n" + ufwNumberedFixture + "EOF\nexit 0; fi\n" +
		"echo \"$@\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ufw"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	steps, _, err := buildFirewallSteps(req, 22)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", firewallActionScript(steps))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	out, _ := cmd.Output()
	res = &FirewallActionResult{}
	parseFirewallActionOutput(string(out), res)
	b, _ := os.ReadFile(log)
	return string(b), res
}

func TestFirewallScript_UfwGuardRunsAgainstRealShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	listed := FirewallRule{Index: 2, Raw: "80 ALLOW IN 192.168.1.0/24"}
	calls, res := runFakeUfw(t, FirewallRequest{Backend: "ufw", Action: "delete", Old: &listed})
	if !res.Succeeded() || strings.TrimSpace(calls) != "--force delete 2" {
		t.Fatalf("matching rule must be deleted: res=%+v calls=%q", res, calls)
	}

	stale := FirewallRule{Index: 2, Raw: "8080 ALLOW IN Anywhere"} // rule 2 changed since it was listed
	calls, res = runFakeUfw(t, FirewallRequest{Backend: "ufw", Action: "delete", Old: &stale})
	if res.ExitCode != 3 || calls != "" || !strings.Contains(res.Output, "changed on the host") {
		t.Fatalf("stale rule must be refused without touching ufw: res=%+v calls=%q", res, calls)
	}

	// Edit: guard, then add (appended), then delete the old number; nothing runs when the guard fails.
	calls, res = runFakeUfw(t, FirewallRequest{Backend: "ufw", Action: "edit", Old: &listed, Rule: &FirewallRule{Action: "allow", Port: "8080", Proto: "tcp"}})
	if !res.Succeeded() || calls != "allow in proto tcp from any to any port 8080\n--force delete 2\n" {
		t.Fatalf("edit order: res=%+v calls=%q", res, calls)
	}
	calls, res = runFakeUfw(t, FirewallRequest{Backend: "ufw", Action: "edit", Old: &stale, Rule: &FirewallRule{Action: "allow", Port: "8080", Proto: "tcp"}})
	if res.Succeeded() || calls != "" {
		t.Fatalf("edit of a stale rule must not add anything: res=%+v calls=%q", res, calls)
	}
}
