package inspector

// Hardening parsers for the agentless security scan: sshd, automatic updates,
// extended Linux hardening (MAC/sysctl/brute-force/auditd) and host firewall.

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"pantau/internal/store"
)

// sshdWeakMarkers flags weak algorithms per sshd_config directive.
var sshdWeakMarkers = map[string][]string{
	"ciphers":       {"cbc", "3des", "arcfour", "blowfish", "cast128", "rijndael"},
	"macs":          {"md5", "sha1", "-96", "umac-64"},
	"kexalgorithms": {"sha1"},
}

// sshdCryptoKeys are the directives checked for weak algorithms.
var sshdCryptoKeys = []string{"ciphers", "macs", "kexalgorithms"}

// parseHardeningChecks parses sshd, automatic updates, firewall and (Linux only) extended hardening checks.
func parseHardeningChecks(osFamily, sshdSec, autoSec, firewallSec, extraSec string) (checks []store.HardeningCheck, notes []string) {
	permitRoot, passAuth, sshdNotes := parseSshdConfig(sshdSec, osFamily)
	checks = append(checks, permitRoot, passAuth)
	checks = append(checks, parseSshdExtraChecks(sshdSec)...)
	notes = append(notes, sshdNotes...)

	autoCheck, autoNotes := parseAutoUpdates(autoSec)
	checks = append(checks, autoCheck)
	notes = append(notes, autoNotes...)

	firewallCheck, fwNotes := parseFirewall(firewallSec)
	checks = append(checks, firewallCheck)
	notes = append(notes, fwNotes...)

	// MAC/sysctl/systemd probes are Linux concepts; macOS has no equivalent section.
	if osFamily != "macos" {
		checks = append(checks, parseExtraHardening(extraSec)...)
	}

	return checks, notes
}

// sshdSettings returns sshd's global directives (lowercased, first value wins, Match blocks skipped).
// A SSHD_EFFECTIVE dump is `sshd -G`/`sshd -T` output, which already resolves Include, Match and the distro's
// compiled-in defaults. Otherwise the FILE: dumps are read the way sshd does: an Include pulls the matching
// drop-ins in at its own line, so drop-ins included at the top of sshd_config override it.
func sshdSettings(content string) map[string]string {
	settings := map[string]string{}
	fields := func(line string) []string {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil
		}
		return strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })
	}

	if rest, ok := strings.CutPrefix(content, "SSHD_EFFECTIVE"); ok {
		for _, line := range strings.Split(rest, "\n") {
			if f := fields(line); len(f) >= 2 {
				settings[strings.ToLower(f[0])] = strings.ToLower(f[1])
			}
		}
		// Before OpenSSH 8.7 ChallengeResponseAuthentication was a separate switch that also enabled keyboard-interactive.
		if settings["challengeresponseauthentication"] == "yes" {
			settings["kbdinteractiveauthentication"] = "yes"
		}
		return settings
	}

	files := map[string][]string{}
	var order []string // shell glob order, which is sorted like sshd's own glob(3)
	cur := ""
	for _, line := range strings.Split(content, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "FILE:"); ok {
			cur = p
			order = append(order, p)
			continue
		}
		files[cur] = append(files[cur], line)
	}

	var walk func(file string, depth int, inMatch bool)
	walk = func(file string, depth int, inMatch bool) {
		for _, line := range files[file] {
			f := fields(line)
			if len(f) == 0 {
				continue
			}
			key := strings.ToLower(f[0])
			switch {
			case key == "match":
				inMatch = len(f) < 2 || !strings.EqualFold(f[1], "all")
			case key == "include":
				if depth >= 16 {
					continue
				}
				for _, pat := range f[1:] {
					if !strings.HasPrefix(pat, "/") {
						pat = "/etc/ssh/" + pat
					}
					for _, p := range order {
						if ok, _ := path.Match(pat, p); ok {
							walk(p, depth+1, inMatch)
						}
					}
				}
			case !inMatch && len(f) >= 2:
				if key == "challengeresponseauthentication" { // alias of KbdInteractiveAuthentication since OpenSSH 8.7
					key = "kbdinteractiveauthentication"
				}
				if _, seen := settings[key]; !seen {
					settings[key] = strings.ToLower(f[1])
				}
			}
		}
	}
	if _, ok := files["/etc/ssh/sshd_config"]; ok {
		walk("/etc/ssh/sshd_config", 0, false)
	} else {
		// Main file unreadable but drop-ins readable: read them in order.
		for _, p := range order {
			walk(p, 0, false)
		}
	}
	return settings
}

// parseSshdConfig computes effective PermitRootLogin and PasswordAuthentication.
func parseSshdConfig(content, osFamily string) (permitRoot, passAuth store.HardeningCheck, notes []string) {
	permitRoot = store.HardeningCheck{ID: "sshd_permit_root_login", Title: "SSH Root Login"}
	passAuth = store.HardeningCheck{ID: "sshd_password_auth", Title: "SSH Password Authentication"}

	if strings.Contains(content, "SSHD_UNREADABLE") || strings.TrimSpace(content) == "" {
		permitRoot.Status = "unknown"
		permitRoot.Detail = "unable to read sshd_config (permission denied)"
		passAuth.Status = "unknown"
		passAuth.Detail = "unable to read sshd_config (permission denied)"
		notes = append(notes, "sshd_config is not readable without elevated permissions")
		return permitRoot, passAuth, notes
	}

	cfg := sshdSettings(content)

	root, rootSet := cfg["permitrootlogin"]
	switch {
	case !rootSet && osFamily != "debian" && osFamily != "ubuntu" && osFamily != "alpine" && osFamily != "arch" && osFamily != "macos":
		// Upstream defaults to prohibit-password, but RHEL/CentOS builds up to 8 default to yes.
		permitRoot.Status = "unknown"
		permitRoot.Detail = "PermitRootLogin is unset and this distribution's compiled-in default differs between releases; root or passwordless sudo lets Pantau read it with sshd -T"
	case !rootSet:
		permitRoot.Status = "pass"
		permitRoot.Detail = "PermitRootLogin is unset (OpenSSH default: prohibit-password)"
	case root == "no":
		permitRoot.Status = "pass"
		permitRoot.Detail = "PermitRootLogin is disabled ('no')"
	case root == "prohibit-password" || root == "without-password":
		permitRoot.Status = "pass"
		permitRoot.Detail = fmt.Sprintf("PermitRootLogin is restricted ('%s')", root)
	case root == "forced-commands-only":
		permitRoot.Status = "pass"
		permitRoot.Detail = "PermitRootLogin is restricted to forced commands"
	case root == "yes":
		permitRoot.Status = "fail"
		permitRoot.Detail = "PermitRootLogin is enabled ('yes')"
	default:
		permitRoot.Status = "warn"
		permitRoot.Detail = fmt.Sprintf("PermitRootLogin set to '%s'", root)
	}

	pw, pwSet := cfg["passwordauthentication"]
	kbd, kbdSet := cfg["kbdinteractiveauthentication"]
	switch {
	case !pwSet:
		passAuth.Status = "warn"
		passAuth.Detail = "PasswordAuthentication is unset (OpenSSH default: yes)"
	case pw != "no":
		passAuth.Status = "warn"
		passAuth.Detail = fmt.Sprintf("PasswordAuthentication is enabled ('%s')", pw)
	case cfg["usepam"] == "yes" && (!kbdSet || kbd != "no"):
		// PAM's keyboard-interactive prompt asks for the same password (OpenSSH default: KbdInteractiveAuthentication yes).
		passAuth.Status = "warn"
		passAuth.Detail = "PasswordAuthentication is 'no', but KbdInteractiveAuthentication with UsePAM still accepts passwords"
	default:
		passAuth.Status = "pass"
		passAuth.Detail = "PasswordAuthentication is disabled ('no')"
	}

	return permitRoot, passAuth, notes
}

// parseSshdExtraChecks evaluates CIS-style sshd settings beyond root login and password auth.
func parseSshdExtraChecks(content string) []store.HardeningCheck {
	empty := store.HardeningCheck{ID: "sshd_empty_passwords", Title: "SSH Empty Passwords"}
	tries := store.HardeningCheck{ID: "sshd_max_auth_tries", Title: "SSH MaxAuthTries"}
	x11 := store.HardeningCheck{ID: "sshd_x11_forwarding", Title: "SSH X11 Forwarding"}
	crypto := store.HardeningCheck{ID: "sshd_weak_crypto", Title: "SSH Weak Ciphers/MACs/Kex"}
	if strings.Contains(content, "SSHD_UNREADABLE") || strings.TrimSpace(content) == "" {
		for _, c := range []*store.HardeningCheck{&empty, &tries, &x11, &crypto} {
			c.Status, c.Detail = "unknown", "unable to read sshd_config (permission denied)"
		}
		return []store.HardeningCheck{empty, tries, x11, crypto}
	}
	cfg := sshdSettings(content)
	effective := strings.HasPrefix(content, "SSHD_EFFECTIVE")

	if cfg["permitemptypasswords"] == "yes" {
		empty.Status, empty.Detail = "fail", "PermitEmptyPasswords is enabled ('yes')"
	} else {
		empty.Status, empty.Detail = "pass", "PermitEmptyPasswords is disabled (OpenSSH default: no)"
	}

	// CIS: MaxAuthTries <= 4; OpenSSH default is 6.
	if v, ok := cfg["maxauthtries"]; !ok {
		tries.Status, tries.Detail = "warn", "MaxAuthTries is unset (OpenSSH default: 6; recommended <= 4)"
	} else if n, err := strconv.Atoi(v); err != nil {
		tries.Status, tries.Detail = "warn", fmt.Sprintf("MaxAuthTries has an unparsable value '%s'", v)
	} else if n > 4 {
		tries.Status, tries.Detail = "warn", fmt.Sprintf("MaxAuthTries is %d (recommended <= 4)", n)
	} else {
		tries.Status, tries.Detail = "pass", fmt.Sprintf("MaxAuthTries is %d", n)
	}

	if cfg["x11forwarding"] == "yes" {
		x11.Status, x11.Detail = "warn", "X11Forwarding is enabled ('yes')"
	} else {
		x11.Status, x11.Detail = "pass", "X11Forwarding is disabled (OpenSSH default: no)"
	}

	weakMarkers := sshdWeakMarkers
	var weak, unset []string
	for _, key := range sshdCryptoKeys {
		v, ok := cfg[key]
		// A '-' list only removes algorithms from the compiled-in defaults, which stay unknown from files.
		if !ok || strings.HasPrefix(v, "-") {
			unset = append(unset, key)
			continue
		}
		for _, alg := range strings.Split(strings.TrimLeft(v, "+^"), ",") {
			for _, m := range weakMarkers[key] {
				if strings.Contains(alg, m) {
					weak = append(weak, alg)
					break
				}
			}
		}
	}
	switch {
	case len(weak) > 0:
		crypto.Status, crypto.Detail = "warn", "weak algorithms enabled: "+strings.Join(weak, ", ")
	case len(unset) > 0 && !effective:
		// Older OpenSSH builds still ship hmac-sha1/umac-64 in their defaults: never claim pass from files alone.
		crypto.Status, crypto.Detail = "unknown", "unset in sshd_config ("+strings.Join(unset, ", ")+"), so compiled-in defaults apply; root or passwordless sudo lets Pantau read them with sshd -T"
	default:
		crypto.Status, crypto.Detail = "pass", "no weak ciphers, MACs or key exchange algorithms enabled"
	}
	return []store.HardeningCheck{empty, tries, x11, crypto}
}

// parseExtraHardening evaluates the Linux ===EXTRA_HARDENING=== section: MAC, kernel sysctls,
// brute-force protection and auditd. An absent section yields unknown, never pass.
func parseExtraHardening(content string) []store.HardeningCheck {
	mac := store.HardeningCheck{ID: "mac_enforced", Title: "Mandatory Access Control (SELinux/AppArmor)"}
	sysctl := store.HardeningCheck{ID: "kernel_sysctl", Title: "Kernel Network & Memory Hardening"}
	brute := store.HardeningCheck{ID: "bruteforce_protection", Title: "SSH Brute-Force Protection"}
	audit := store.HardeningCheck{ID: "auditd_active", Title: "Audit Daemon (auditd)"}
	if strings.TrimSpace(content) == "" {
		for _, c := range []*store.HardeningCheck{&mac, &sysctl, &brute, &audit} {
			c.Status, c.Detail = "unknown", "extended hardening data was not collected"
		}
		return []store.HardeningCheck{mac, sysctl, brute, audit}
	}
	kv := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = strings.TrimSpace(v)
		} else if k, v, ok := strings.Cut(line, ":"); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}

	selinux, hasSELinux := kv["SELINUX_ENFORCE"]
	switch {
	case selinux == "1":
		mac.Status, mac.Detail = "pass", "SELinux is enforcing"
	case kv["APPARMOR_ENABLED"] == "Y":
		mac.Status, mac.Detail = "pass", "AppArmor is enabled"
	case hasSELinux:
		mac.Status, mac.Detail = "warn", "SELinux is permissive"
	default:
		mac.Status, mac.Detail = "warn", "neither SELinux nor AppArmor is active"
	}

	// want: /proc/sys key, required value ("" = any non-zero), label.
	want := []struct{ key, val, label string }{
		{"kernel/randomize_va_space", "2", "kernel.randomize_va_space=2"},
		{"kernel/kptr_restrict", "", "kernel.kptr_restrict>=1"},
		{"net/ipv4/tcp_syncookies", "1", "net.ipv4.tcp_syncookies=1"},
		{"net/ipv4/conf/all/accept_redirects", "0", "net.ipv4.conf.all.accept_redirects=0"},
		{"net/ipv4/conf/all/accept_source_route", "0", "net.ipv4.conf.all.accept_source_route=0"},
	}
	var bad []string
	read := 0
	for _, w := range want {
		v, ok := kv[w.key]
		if !ok {
			continue
		}
		read++
		if (w.val == "" && (v == "" || v == "0")) || (w.val != "" && v != w.val) {
			bad = append(bad, w.label+" (is "+v+")")
		}
	}
	switch {
	case len(bad) > 0:
		sysctl.Status, sysctl.Detail = "warn", "not hardened: "+strings.Join(bad, "; ")
	case read < len(want):
		sysctl.Status, sysctl.Detail = "unknown", fmt.Sprintf("only %d of %d kernel parameters readable under /proc/sys", read, len(want))
	default:
		sysctl.Status, sysctl.Detail = "pass", "ASLR, kptr_restrict, SYN cookies and ICMP redirect/source-route hardening are set"
	}

	var active []string
	probed := false
	for _, s := range []string{"fail2ban", "sshguard", "crowdsec"} {
		if v, ok := kv[s]; ok && v != "noprobe" {
			probed = true
			if v == "active" {
				active = append(active, s)
			}
		}
	}
	switch {
	case len(active) > 0:
		brute.Status, brute.Detail = "pass", strings.Join(active, ", ")+" active"
	case !probed:
		brute.Status, brute.Detail = "unknown", "no service manager or process listing available"
	default:
		brute.Status, brute.Detail = "warn", "no fail2ban, sshguard or crowdsec service is active"
	}

	switch v := kv["auditd"]; v {
	case "active":
		audit.Status, audit.Detail = "pass", "auditd is active"
	case "", "noprobe":
		audit.Status, audit.Detail = "unknown", "no service manager or process listing available"
	default:
		audit.Status, audit.Detail = "warn", "auditd is not active ("+v+")"
	}
	return []store.HardeningCheck{mac, sysctl, brute, audit}
}

// parseAutoUpdates checks if automatic updates are enabled / active.
func parseAutoUpdates(content string) (store.HardeningCheck, []string) {
	cLower := strings.ToLower(content)

	// macOS SoftwareUpdate defaults check
	if strings.Contains(cLower, "defaults_unreadable") || strings.Contains(cLower, "com.apple.softwareupdate does not exist") {
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "unknown",
			Detail: "unable to read macOS SoftwareUpdate preferences",
		}, []string{"automatic update preferences could not be read"}
	}
	if strings.Contains(content, "CriticalUpdateInstall") || strings.Contains(content, "ConfigDataInstall") ||
		strings.Contains(content, "AutomaticDownload") || strings.Contains(content, "AutomaticallyInstallMacOSUpdates") {
		hasCritical := macAutoCriticalRe.MatchString(content)
		hasConfig := macAutoConfigRe.MatchString(content)
		if hasCritical && hasConfig {
			return store.HardeningCheck{
				ID:     "auto_security_updates",
				Title:  "Automatic Security Updates",
				Status: "pass",
				Detail: "macOS automatic security and configuration updates are enabled",
			}, nil
		}
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "warn",
			Detail: "macOS automatic security updates are disabled or incomplete",
		}, nil
	}

	check := func(status, detail string) store.HardeningCheck {
		return store.HardeningCheck{ID: "auto_security_updates", Title: "Automatic Security Updates", Status: status, Detail: detail}
	}
	// value returns the rest of the first line starting with prefix, without APT's quotes and semicolon.
	value := func(prefix string) string {
		for _, line := range strings.Split(content, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
				return strings.Trim(strings.TrimSpace(v), `";`)
			}
		}
		return ""
	}
	unitOn := func(unit string) bool {
		return value(unit+"_ENABLED:") == "enabled" || value(unit+"_ACTIVE:") == "active"
	}
	// applyOn reads `apply_updates = yes` from a `grep -H` line; dnf-automatic and yum-cron default to download-only.
	applyOn := func(file string) bool {
		_, v, _ := strings.Cut(value(file+":"), "=")
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "yes", "true", "1", "on":
			return true
		}
		return false
	}

	switch {
	case unitOn("DNF_INSTALL_TIMER"):
		return check("pass", "dnf-automatic-install.timer installs updates automatically"), nil
	case unitOn("DNF_TIMER") && applyOn("/etc/dnf/automatic.conf"):
		return check("pass", "dnf-automatic.timer is active and apply_updates is on"), nil
	case unitOn("DNF_TIMER"):
		return check("warn", "dnf-automatic.timer is active, but apply_updates is off in /etc/dnf/automatic.conf, so updates are only downloaded"), nil
	case unitOn("YUM_CRON") && applyOn("/etc/yum/yum-cron.conf"):
		return check("pass", "yum-cron is active and apply_updates is on"), nil
	case unitOn("YUM_CRON"):
		return check("warn", "yum-cron is active, but apply_updates is off in /etc/yum/yum-cron.conf, so updates are only downloaded"), nil
	case strings.Contains(content, "---APT_CONFIG---"):
		// Effective `apt-config dump` values, so a later file that turns a setting off is honored.
		uu := value("APT::Periodic::Unattended-Upgrade ")
		lists := value("APT::Periodic::Update-Package-Lists ")
		timer := value("APT_TIMER_ENABLED:")
		switch {
		case (uu == "" || uu == "0") && lists != "" && lists != "0":
			return check("warn", "APT refreshes package lists, but APT::Periodic::Unattended-Upgrade is off, so nothing is installed"), nil
		case uu == "" || uu == "0":
			return check("warn", "APT::Periodic::Unattended-Upgrade is not enabled"), nil
		case !strings.Contains(content, "UU_INSTALLED"):
			return check("warn", "APT::Periodic::Unattended-Upgrade is on, but the unattended-upgrades package is not installed"), nil
		case timer == "disabled" || timer == "masked":
			return check("warn", fmt.Sprintf("apt-daily-upgrade.timer is %s, so unattended upgrades never run", timer)), nil
		default:
			return check("pass", fmt.Sprintf("unattended-upgrades runs daily (APT::Periodic::Unattended-Upgrade \"%s\")", uu)), nil
		}
	case strings.Contains(cLower, "permission denied") || strings.Contains(cLower, "failed to connect to bus"):
		return check("unknown", "unable to determine automatic update status"), []string{"automatic update service status could not be read"}
	default:
		return check("warn", "automatic security updates are not active or enabled"), nil
	}
}

// nftFiltersInput reports whether an nft base chain on the input hook drops by policy or ends in an
// unconditional drop/reject. Tables that only Docker, kube-proxy or fail2ban create (policy accept) do not count.
func nftFiltersInput(ruleset string) bool {
	inInput := false
	for _, line := range strings.Split(ruleset, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "chain ") || l == "}":
			inInput = false
		case strings.HasPrefix(l, "type filter hook input "):
			if strings.Contains(l, "policy drop") {
				return true
			}
			inInput = true
		case inInput:
			f := strings.Fields(l)
			for len(f) > 0 && f[0] == "counter" {
				f = f[1:]
				if len(f) >= 4 && f[0] == "packets" && f[2] == "bytes" {
					f = f[4:]
				}
			}
			if len(f) > 0 && (f[0] == "drop" || f[0] == "reject") {
				return true
			}
		}
	}
	return false
}

// iptablesFiltersInput reports whether the INPUT chain drops by policy or has an unconditional DROP/REJECT rule.
// Rules elsewhere (Docker's FORWARD/DOCKER chains, fail2ban jumps) leave inbound connections open.
func iptablesFiltersInput(rules string) bool {
	for _, line := range strings.Split(rules, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "-P" && f[1] == "INPUT" && f[2] == "DROP":
			return true
		case len(f) >= 4 && f[0] == "-A" && f[1] == "INPUT" && f[2] == "-j" && (f[3] == "DROP" || f[3] == "REJECT"):
			return true
		case strings.HasPrefix(strings.TrimSpace(line), "Chain INPUT (policy DROP"): // `iptables -L -n` fallback
			return true
		}
	}
	return false
}

// parseFirewall inspects ufw, firewalld, nftables, and iptables.
func parseFirewall(content string) (store.HardeningCheck, []string) {
	cLower := strings.ToLower(content)

	if strings.Contains(cLower, "socketfilterfw_unreadable") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "unknown",
			Detail: "Application Firewall status could not be read",
		}, []string{"firewall status could not be read"}
	}
	if strings.Contains(cLower, "state = 1") || strings.Contains(cLower, "state = 2") ||
		(strings.Contains(cLower, "firewall is enabled") && !strings.Contains(cLower, "disabled")) {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "pass",
			Detail: "Application Firewall is enabled",
		}, nil
	}
	if strings.Contains(cLower, "state = 0") || strings.Contains(cLower, "firewall is disabled") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "warn",
			Detail: "Application Firewall is disabled",
		}, nil
	}

	check := func(status, detail string) store.HardeningCheck {
		return store.HardeningCheck{ID: "firewall_active", Title: "Host Firewall", Status: status, Detail: detail}
	}

	if strings.Contains(cLower, "status: active") {
		return check("pass", "ufw firewall is active"), nil
	}

	fwState := firstLines(extractSection(content, "---FIREWALLD---", "---NFT---"), 1)
	if fwState == "active" || fwState == "running" {
		return check("pass", "firewalld is active"), nil
	}

	nftSec := extractSection(content, "---NFT---", "---IPTABLES---")
	if nftFiltersInput(nftSec) {
		return check("pass", "nftables input chain drops unmatched traffic"), nil
	}
	iptSec := extractSection(content, "---IPTABLES---", "")
	if iptablesFiltersInput(iptSec) {
		return check("pass", "iptables INPUT chain drops unmatched traffic"), nil
	}
	if strings.Contains(nftSec, "hook input") || strings.Contains(iptSec, "-P INPUT") || strings.Contains(iptSec, "Chain INPUT") {
		return check("warn", "rules are loaded, but no input chain drops unmatched traffic (policy accept); Docker or fail2ban rules alone do not filter inbound connections"), nil
	}

	if strings.Contains(cLower, "permission denied") ||
		strings.Contains(cLower, "operation not permitted") ||
		strings.Contains(cLower, "must be root") ||
		strings.Contains(cLower, "need to be root") {
		return check("unknown", "unable to read firewall status without root privileges"), []string{"firewall status could not be read without root privileges"}
	}

	if strings.Contains(cLower, "status: inactive") || fwState == "inactive" {
		return check("warn", "firewall is inactive"), nil
	}

	return check("warn", "no active firewall detected (ufw, firewalld, nftables, iptables)"), nil
}
