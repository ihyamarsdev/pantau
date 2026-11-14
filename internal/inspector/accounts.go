package inspector

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"pantau/internal/store"
)

var (
	// ErrAccountAuditInProgress is returned when an account audit is already running for a host.
	ErrAccountAuditInProgress = errors.New("account audit already in progress for this host")
)

// accountAuditInFlight guards one account audit per host (like the security scan singleflight).
var accountAuditInFlight sync.Map

// AccountAuditCmd is a polyglot, agentless, read-only script executed on the remote host.
// It collects local accounts, their authorized_keys files, the shadow password state, sudoers
// grants, group memberships and the sshd AuthorizedKeysFile override. Nothing is installed and
// nothing is written on the host.
const AccountAuditCmd = `PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin"; export PATH
S=""
MODE=none
if [ "$(id -u 2>/dev/null)" = "0" ]; then
  MODE=root
elif [ "$PANTAU_TRY_SUDO" = "1" ] && sudo -n true 2>/dev/null; then
  S="sudo -n"; MODE=sudo; echo "---SUDO:ok---"
elif [ "$PANTAU_TRY_SUDO" = "1" ]; then
  echo "---SUDO:denied---"
fi
echo "===MODE:$MODE==="
echo "===OS==="
uname -s 2>/dev/null
if [ "$(uname -s 2>/dev/null)" = "Darwin" ]; then
  echo "===DARWIN==="
  exit 0
fi

echo "===PASSWD==="
cat /etc/passwd 2>/dev/null | while IFS=: read -r n p u g gecos h sh; do
  keep=1
  case "$sh" in *nologin|*false) [ "$u" = "0" ] || keep=0 ;; esac
  [ "$keep" = "1" ] && echo "U|$n|$u|$g|$h|$sh"
done

echo "===GROUP==="
cat /etc/group 2>/dev/null | while IFS=: read -r n p g m; do echo "G|$n|$g|$m"; done

echo "===AKFILE==="
cat /etc/passwd 2>/dev/null | while IFS=: read -r n p u g gecos h sh; do
  keep=1
  case "$sh" in *nologin|*false) [ "$u" = "0" ] || keep=0 ;; esac
  [ "$keep" = "1" ] || continue
  [ -n "$h" ] || continue
  for f in "$h/.ssh/authorized_keys" "$h/.ssh/authorized_keys2"; do
    [ -e "$f" ] || continue
    if [ -r "$f" ]; then
      echo "AK|$n|$f|readable"
      echo "---CONTENT---"
      sed 's/^/K|/' "$f"
      echo "---ENDCONTENT---"
    else
      body=""; rc=1
      if [ -n "$S" ]; then body=$($S cat "$f" 2>/dev/null); rc=$?; fi
      if [ "$rc" = "0" ]; then
        echo "AK|$n|$f|readable"
        echo "---CONTENT---"
        printf '%s\n' "$body" | sed 's/^/K|/'
        echo "---ENDCONTENT---"
      else
        echo "AK|$n|$f|unreadable"
      fi
    fi
  done
done

echo "===SHADOW==="
SHADOW_DATA=""; SHADOW_RC=1
if [ -r /etc/shadow ]; then
  SHADOW_DATA=$(cat /etc/shadow 2>/dev/null); SHADOW_RC=$?
elif [ -n "$S" ]; then
  SHADOW_DATA=$($S cat /etc/shadow 2>/dev/null); SHADOW_RC=$?
fi
if [ "$SHADOW_RC" = "0" ]; then
  printf '%s\n' "$SHADOW_DATA" | while IFS=: read -r n p rest; do echo "SH|$n|$p"; done
else
  echo "SHADOW_UNKNOWN"
fi

echo "===SUDO==="
SUDO_OK=0
if [ -r /etc/sudoers ]; then
  grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' /etc/sudoers 2>/dev/null | while IFS= read -r line; do echo "SD|/etc/sudoers|$line"; done
  SUDO_OK=1
elif [ -n "$S" ] && $S cat /etc/sudoers >/dev/null 2>&1; then
  $S cat /etc/sudoers 2>/dev/null | grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' | while IFS= read -r line; do echo "SD|/etc/sudoers|$line"; done
  SUDO_OK=1
fi
for f in /etc/sudoers.d/*; do
  [ -f "$f" ] || continue
  if [ -r "$f" ]; then
    grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' "$f" 2>/dev/null | while IFS= read -r line; do echo "SD|$f|$line"; done
    SUDO_OK=1
  elif [ -n "$S" ] && $S cat "$f" >/dev/null 2>&1; then
    $S cat "$f" 2>/dev/null | grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' | while IFS= read -r line; do echo "SD|$f|$line"; done
    SUDO_OK=1
  fi
done
[ "$SUDO_OK" = "1" ] || echo "SUDO_UNKNOWN"

echo "===LASTLOG==="
if command -v lastlog >/dev/null 2>&1; then
  cat /etc/passwd 2>/dev/null | while IFS=: read -r n p u g gecos h sh; do
    keep=1
    case "$sh" in *nologin|*false) [ "$u" = "0" ] || keep=0 ;; esac
    [ "$keep" = "1" ] || continue
    line=$(lastlog -u "$n" 2>/dev/null | tail -n 1)
    v=${line#* }
    case "$v" in *never*) v=never ;; *\**) v=hidden ;; esac
    echo "LL|$n|$v"
  done
else
  echo "LASTLOG_UNKNOWN"
fi

echo "===SSHD==="
if command -v sshd >/dev/null 2>&1; then
  akf=$($S sshd -T 2>/dev/null | grep -i '^authorizedkeysfile' | head -n 1)
  [ -n "$akf" ] && echo "AKF|${akf#* }"
fi
for f in /etc/ssh/sshd_config /etc/ssh/sshd_config.d/*.conf; do
  [ -f "$f" ] || continue
  if [ -r "$f" ]; then
    grep -i -h '^[[:space:]]*AuthorizedKeysFile' "$f" 2>/dev/null | while IFS= read -r line; do echo "AKFC|$line"; done
  elif [ -n "$S" ] && $S cat "$f" >/dev/null 2>&1; then
    $S cat "$f" 2>/dev/null | grep -i '^[[:space:]]*AuthorizedKeysFile' | while IFS= read -r line; do echo "AKFC|$line"; done
  fi
done
`

// accountAuditTimeout is the wall-clock budget of one account audit.
const accountAuditTimeout = 90 * time.Second

// ScanAccountAudit runs the read-only account audit over SSH and stores the result.
func (ins *Inspector) ScanAccountAudit(hostID int64) error {
	return ins.ScanAccountAuditWithTimeout(hostID, accountAuditTimeout)
}

// ScanAccountAuditWithTimeout runs the account audit with a given timeout budget.
func (ins *Inspector) ScanAccountAuditWithTimeout(hostID int64, timeout time.Duration) error {
	if _, loaded := accountAuditInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		return ErrAccountAuditInProgress
	}
	defer accountAuditInFlight.Delete(hostID)

	if timeout <= 0 {
		timeout = accountAuditTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return fmt.Errorf("connect host %d: %w", hostID, err)
	}
	defer runner.Close()

	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-doneChan:
		case <-ctx.Done():
			_ = runner.Close()
		}
	}()

	trySudo := "0"
	if ins.sudoProbeAllowed(hostID) {
		trySudo = "1"
	}
	stdout, _, _, err := runner.Exec("PANTAU_TRY_SUDO=" + trySudo + "\n" + AccountAuditCmd)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("exec account audit timed out: %w", ctx.Err())
		}
		return fmt.Errorf("exec account audit: %w", err)
	}
	switch {
	case strings.Contains(stdout, "---SUDO:ok---"):
		ins.noteSudo(hostID, false)
	case strings.Contains(stdout, "---SUDO:denied---"):
		ins.noteSudo(hostID, true)
	}

	library, err := ins.sshKeyLibrary()
	if err != nil {
		return err
	}
	audit := parseAccountAuditOutput(stdout, hostID, library)
	audit.ScannedAt = time.Now()
	return ins.db.SaveAccountAudit(audit)
}

// sshKeyLibrary returns fingerprint -> library key name of the SSH key library.
func (ins *Inspector) sshKeyLibrary() (map[string]string, error) {
	keys, err := ins.db.ListSSHKeys()
	if err != nil {
		return nil, err
	}
	lib := make(map[string]string, len(keys))
	for _, k := range keys {
		if k.Fingerprint != "" {
			lib[k.Fingerprint] = k.Name
		}
	}
	return lib, nil
}

/* ---------- raw script output ---------- */

type rawUser struct {
	name  string
	uid   int
	gid   int
	home  string
	shell string
}

type rawGroup struct {
	name    string
	gid     int
	members []string
}

type rawKeyFile struct {
	user  string
	path  string
	read  bool
	lines []string
}

type rawAccountAudit struct {
	mode          string
	os            string
	darwin        bool
	users         []rawUser
	groups        []rawGroup
	keyFiles      []rawKeyFile
	shadow        map[string]string
	shadowUnknown bool
	sudoRules     []string
	sudoUnknown   bool
	lastLog       map[string]string
	lastLogUnkn   bool
	authKeysFile  string
	authKeysConf  []string
}

// parseRawAccountAudit splits the script output into its sections.
func parseRawAccountAudit(stdout string) *rawAccountAudit {
	raw := &rawAccountAudit{
		shadow:  map[string]string{},
		lastLog: map[string]string{},
	}
	section := ""
	inContent := false
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		if inContent {
			switch {
			case strings.HasPrefix(line, "---ENDCONTENT---"):
				inContent = false
			case strings.HasPrefix(line, "K|") && len(raw.keyFiles) > 0:
				last := &raw.keyFiles[len(raw.keyFiles)-1]
				last.lines = append(last.lines, strings.TrimPrefix(line, "K|"))
			}
			continue
		}
		if strings.HasPrefix(line, "---CONTENT---") {
			inContent = true
			continue
		}
		if strings.HasPrefix(line, "===") {
			switch {
			case strings.HasPrefix(line, "===MODE:"):
				raw.mode = strings.TrimSuffix(strings.TrimPrefix(line, "===MODE:"), "===")
				section = ""
			case line == "===OS===":
				section = "os"
			case line == "===DARWIN===":
				raw.darwin = true
				section = ""
			case line == "===PASSWD===":
				section = "passwd"
			case line == "===GROUP===":
				section = "group"
			case line == "===AKFILE===":
				section = "akfile"
			case line == "===SHADOW===":
				section = "shadow"
			case line == "===SUDO===":
				section = "sudo"
			case line == "===LASTLOG===":
				section = "lastlog"
			case line == "===SSHD===":
				section = "sshd"
			default:
				section = ""
			}
			continue
		}
		switch section {
		case "os":
			raw.os = strings.TrimSpace(line)
			section = ""
		case "passwd":
			if f := strings.SplitN(line, "|", 6); len(f) == 6 {
				raw.users = append(raw.users, rawUser{
					name:  f[1],
					uid:   atoiOr(f[2], -1),
					gid:   atoiOr(f[3], -1),
					home:  f[4],
					shell: f[5],
				})
			}
		case "group":
			if f := strings.SplitN(line, "|", 4); len(f) == 4 {
				g := rawGroup{name: f[1], gid: atoiOr(f[2], -1)}
				if m := strings.TrimSpace(f[3]); m != "" {
					g.members = strings.Split(m, ",")
				}
				raw.groups = append(raw.groups, g)
			}
		case "akfile":
			if f := strings.SplitN(line, "|", 4); len(f) == 4 {
				raw.keyFiles = append(raw.keyFiles, rawKeyFile{
					user: f[1],
					path: f[2],
					read: f[3] == "readable",
				})
			}
		case "shadow":
			if line == "SHADOW_UNKNOWN" {
				raw.shadowUnknown = true
				continue
			}
			if f := strings.SplitN(line, "|", 3); len(f) == 3 {
				raw.shadow[f[1]] = f[2]
			}
		case "sudo":
			if line == "SUDO_UNKNOWN" {
				raw.sudoUnknown = true
				continue
			}
			if f := strings.SplitN(line, "|", 3); len(f) == 3 {
				raw.sudoRules = append(raw.sudoRules, strings.TrimSpace(f[2]))
			}
		case "lastlog":
			if line == "LASTLOG_UNKNOWN" {
				raw.lastLogUnkn = true
				continue
			}
			if f := strings.SplitN(line, "|", 3); len(f) == 3 {
				raw.lastLog[f[1]] = f[2]
			}
		case "sshd":
			if f := strings.SplitN(line, "|", 2); len(f) == 2 {
				if strings.HasPrefix(line, "AKFC|") {
					raw.authKeysConf = append(raw.authKeysConf, strings.TrimSpace(f[1]))
				} else {
					raw.authKeysFile = strings.TrimSpace(f[1])
				}
			}
		}
	}
	return raw
}

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// groupMembership maps user name -> group names (supplementary groups from /etc/group).
func (raw *rawAccountAudit) groupMembership() map[string][]string {
	out := map[string][]string{}
	for _, g := range raw.groups {
		for _, m := range g.members {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			out[m] = append(out[m], g.name)
		}
	}
	return out
}

/* ---------- users & keys ---------- */

// loginShell reports whether the shell allows an interactive login.
func loginShell(shell string) bool {
	s := strings.ToLower(shell)
	return s != "" && !strings.Contains(s, "nologin") && !strings.Contains(s, "false")
}

// buildAccountUsers turns the raw passwd/group/shadow/sudo data into the stored user list.
func buildAccountUsers(raw *rawAccountAudit, groupMembers map[string][]string) []store.AccountUser {
	sudoGrants := parseSudoGrants(raw.sudoRules)
	users := make([]store.AccountUser, 0, len(raw.users))
	for _, u := range raw.users {
		// The script already lists only login-capable accounts; keep the filter here too, but
		// never drop a UID 0 account: an extra root-equivalent must always stay visible.
		if !loginShell(u.shell) && u.uid != 0 {
			continue
		}
		groups := append([]string{}, groupMembers[u.name]...)
		sortStrings(groups)
		au := store.AccountUser{
			Name:           u.name,
			UID:            u.uid,
			GID:            u.gid,
			Home:           u.home,
			Shell:          u.shell,
			LoginCapable:   loginShell(u.shell),
			Groups:         groups,
			PasswordState:  "unknown",
			KeyFiles:       []string{},
			KeyFilesUnread: []string{},
		}
		if g, ok := sudoGrants[u.name]; ok {
			au.SudoGrant = g
		} else if raw.sudoUnknown {
			au.SudoGrant = "unknown"
		} else {
			au.SudoGrant = "none"
		}
		if raw.shadowUnknown {
			au.PasswordState = "unknown"
		} else if p, ok := raw.shadow[u.name]; ok {
			au.PasswordState = passwordState(p)
		}
		if v, ok := raw.lastLog[u.name]; ok && v != "" {
			au.LastLogin = v
		}
		users = append(users, au)
	}
	return users
}

// passwordState maps a /etc/shadow password field to set|empty|locked.
func passwordState(field string) string {
	switch {
	case field == "":
		return "empty"
	case strings.HasPrefix(field, "!") || strings.HasPrefix(field, "*"):
		return "locked"
	default:
		return "set"
	}
}

// parseSudoGrants extracts the sudo grant of every user/group mentioned in the sudoers rules.
// The result maps a user or group name to a short grant description ("ALL", "NOPASSWD: ALL", ...).
func parseSudoGrants(rules []string) map[string]string {
	grants := map[string]string{}
	for _, rule := range rules {
		if strings.Contains(rule, "Defaults") {
			continue
		}
		m := sudoRuleRe.FindStringSubmatch(rule)
		if m == nil {
			continue
		}
		who, runas, rest := m[1], m[2], m[4]
		command := strings.TrimSpace(rest)
		// The user list can be a comma separated set of users and %groups.
		for _, whoItem := range strings.Split(who, ",") {
			whoItem = strings.TrimSpace(whoItem)
			if whoItem == "" {
				continue
			}
			grant := sudoGrantDescription(runas, command)
			if grant == "" {
				continue
			}
			grants[whoItem] = grant
		}
	}
	return grants
}

// sudoGrantDescription describes a sudoers rule for the audit.
func sudoGrantDescription(runas, command string) string {
	upper := strings.ToUpper(runas)
	all := strings.Contains(upper, "ALL")
	nopasswd := strings.Contains(strings.ToUpper(command), "NOPASSWD")
	switch {
	case all && nopasswd:
		return "NOPASSWD: ALL"
	case all:
		return "ALL"
	case nopasswd:
		return "NOPASSWD"
	}
	return ""
}

// sudoRuleRe matches a sudoers rule line: "<users> <host>=(<runas>) <options> <command>".
var sudoRuleRe = regexp.MustCompile(`^(\S+)\s+(\S+?)\s*=\s*(\([^)]*\)|ALL|\S+)\s*(.*)$`)

// buildAccountKeys parses every authorized_keys line of the audit and cross-references the
// SSH key library by fingerprint. It returns the keys and the number of unparseable lines.
func buildAccountKeys(raw *rawAccountAudit, library map[string]string) ([]store.AccountKey, int) {
	keys := []store.AccountKey{}
	invalid := 0
	for _, f := range raw.keyFiles {
		if !f.read {
			keys = append(keys, store.AccountKey{
				User:  f.user,
				File:  f.path,
				Valid: false,
				Error: "unreadable",
			})
			continue
		}
		for _, line := range f.lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			k, err := parseAuthorizedKeyLine(trimmed)
			if err != nil {
				invalid++
				keys = append(keys, store.AccountKey{
					User:  f.user,
					File:  f.path,
					Valid: false,
					Error: err.Error(),
				})
				continue
			}
			k.User = f.user
			k.File = f.path
			if name, ok := library[k.Fingerprint]; ok {
				k.LibraryKey = name
			} else {
				k.Unknown = true
			}
			keys = append(keys, k)
		}
	}
	return keys, invalid
}

// parseAuthorizedKeyLine parses one authorized_keys line into its metadata.
func parseAuthorizedKeyLine(line string) (store.AccountKey, error) {
	pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return store.AccountKey{}, err
	}
	k := store.AccountKey{
		Type:    pub.Type(),
		Comment: comment,
		Options: options,
		Valid:   true,
	}
	if k.Options == nil {
		k.Options = []string{}
	}
	k.Fingerprint = ssh.FingerprintSHA256(pub)
	k.Bits = publicKeyBits(pub)
	k.Weak = pub.Type() == "ssh-dss" || (k.Bits > 0 && k.Bits < 2048)
	for _, opt := range options {
		o := strings.ToLower(opt)
		switch {
		case strings.HasPrefix(o, "command="):
			k.Restricted = true
		case strings.HasPrefix(o, "from="):
			k.SourceRestricted = true
		}
	}
	return k, nil
}

// publicKeyBits returns the modulus size of an RSA key, or 0 for keys without a bit size.
func publicKeyBits(pub ssh.PublicKey) int {
	if ck, ok := pub.(ssh.CryptoPublicKey); ok {
		if rk, ok := ck.CryptoPublicKey().(*rsa.PublicKey); ok {
			return rk.N.BitLen()
		}
	}
	return 0
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

/* ---------- findings ---------- */

// evaluateAccountFindings derives the findings and the coverage notes of one audit.
func evaluateAccountFindings(raw *rawAccountAudit, users []store.AccountUser, keys []store.AccountKey) ([]store.AccountFinding, []string) {
	findings := []store.AccountFinding{}
	notes := []string{}

	// Extra UID 0 accounts.
	for _, u := range users {
		if u.UID == 0 && u.Name != "root" {
			findings = append(findings, store.AccountFinding{
				ID:       "uid0_account",
				Severity: "critical",
				Title:    "Extra UID 0 account",
				Detail:   fmt.Sprintf("Account %q has UID 0 and can log in as root.", u.Name),
				User:     u.Name,
			})
		}
	}

	// Empty password on a login-capable account.
	for _, u := range users {
		if u.PasswordState == "empty" && u.LoginCapable {
			findings = append(findings, store.AccountFinding{
				ID:       "empty_password",
				Severity: "critical",
				Title:    "Empty password on a login account",
				Detail:   fmt.Sprintf("Account %q has an empty password field and a login shell (%s).", u.Name, u.Shell),
				User:     u.Name,
			})
		}
	}

	// Sudo grants.
	for _, u := range users {
		switch {
		case strings.HasPrefix(u.SudoGrant, "NOPASSWD"):
			findings = append(findings, store.AccountFinding{
				ID:       "sudo_nopasswd",
				Severity: "high",
				Title:    "Passwordless sudo grant",
				Detail:   fmt.Sprintf("Account %q can run %s without a password.", u.Name, u.SudoGrant),
				User:     u.Name,
			})
		case u.SudoGrant == "ALL":
			findings = append(findings, store.AccountFinding{
				ID:       "sudo_all",
				Severity: "medium",
				Title:    "Full sudo grant",
				Detail:   fmt.Sprintf("Account %q can run ALL commands with sudo (a password is required).", u.Name),
				User:     u.Name,
			})
		}
	}

	// Key findings.
	for _, k := range keys {
		if !k.Valid {
			continue
		}
		switch {
		case k.Type == "ssh-dss":
			findings = append(findings, store.AccountFinding{
				ID:          "weak_key_dss",
				Severity:    "critical",
				Title:       "Weak SSH key (DSA)",
				Detail:      fmt.Sprintf("The key authorized for %q is a DSA key (ssh-dss), which is deprecated.", k.User),
				User:        k.User,
				Fingerprint: k.Fingerprint,
			})
		case k.Bits > 0 && k.Bits < 2048:
			findings = append(findings, store.AccountFinding{
				ID:          "weak_key_rsa",
				Severity:    "high",
				Title:       "Weak SSH key (small RSA modulus)",
				Detail:      fmt.Sprintf("The RSA key authorized for %q is %d bits; 2048 bits is the minimum.", k.User, k.Bits),
				User:        k.User,
				Fingerprint: k.Fingerprint,
			})
		}
		if k.Unknown {
			findings = append(findings, store.AccountFinding{
				ID:          "unknown_key",
				Severity:    "medium",
				Title:       "SSH key not in the key library",
				Detail:      fmt.Sprintf("The key authorized for %q is not registered in the Pantau SSH key library.", k.User),
				User:        k.User,
				Fingerprint: k.Fingerprint,
			})
		}
		if !k.SourceRestricted {
			findings = append(findings, store.AccountFinding{
				ID:          "key_without_from",
				Severity:    "info",
				Title:       "SSH key without a source restriction",
				Detail:      fmt.Sprintf("The key authorized for %q has no from= restriction: it can be used from any address.", k.User),
				User:        k.User,
				Fingerprint: k.Fingerprint,
			})
		}
	}

	// A key authorized for many accounts on the same host is a wide blast radius (info).
	usersPerKey := map[string][]string{}
	for _, k := range keys {
		if !k.Valid {
			continue
		}
		usersPerKey[k.Fingerprint] = append(usersPerKey[k.Fingerprint], k.User)
	}
	for fp, users := range usersPerKey {
		if len(users) < 2 {
			continue
		}
		findings = append(findings, store.AccountFinding{
			ID:          "shared_key",
			Severity:    "info",
			Title:       "SSH key shared by several accounts",
			Detail:      fmt.Sprintf("The key %s is authorized for %d accounts on this host: %s.", fp, len(users), strings.Join(users, ", ")),
			Fingerprint: fp,
		})
	}

	// Unlocked service account with a login shell.
	for _, u := range users {
		if u.UID < 1000 || !u.LoginCapable || u.PasswordState != "locked" {
			continue
		}
		if u.Name == "root" || u.Name == "nobody" {
			continue
		}
		findings = append(findings, store.AccountFinding{
			ID:       "service_account_login_shell",
			Severity: "low",
			Title:    "Service account with a login shell",
			Detail:   fmt.Sprintf("Service account %q (UID %d) has a login shell (%s) although its password is locked.", u.Name, u.UID, u.Shell),
			User:     u.Name,
		})
	}

	// Coverage notes (honest coverage: unreadable data is unknown, never "clean").
	if raw.shadowUnknown {
		notes = append(notes, "/etc/shadow is not readable: the password state of every account is unknown")
	}
	if raw.sudoUnknown {
		notes = append(notes, "/etc/sudoers is not readable: sudo grants are unknown")
	}
	if raw.lastLogUnkn {
		notes = append(notes, "lastlog is not available: the last login time is unknown")
	}
	for _, f := range raw.keyFiles {
		if !f.read {
			notes = append(notes, fmt.Sprintf("%s could not be read: the keys of %q are unknown", f.path, f.user))
		}
	}
	if raw.authKeysFile != "" && !strings.Contains(raw.authKeysFile, ".ssh/authorized_keys") {
		notes = append(notes, fmt.Sprintf("sshd AuthorizedKeysFile is %q: the keys read by this audit may not be the ones sshd uses", raw.authKeysFile))
	}
	return findings, notes
}

// parseAccountAuditOutput turns the raw script output into a stored account audit.
func parseAccountAuditOutput(stdout string, hostID int64, library map[string]string) *store.AccountAudit {
	audit := &store.AccountAudit{
		HostID:   hostID,
		Status:   "error",
		Users:    []store.AccountUser{},
		Keys:     []store.AccountKey{},
		Findings: []store.AccountFinding{},
		Notes:    []string{},
	}
	if strings.TrimSpace(stdout) == "" {
		audit.Error = "empty output from remote host"
		audit.Notes = append(audit.Notes, "no output returned from remote host")
		return audit
	}
	raw := parseRawAccountAudit(stdout)
	if raw.darwin {
		audit.Status = "unsupported"
		audit.Notes = append(audit.Notes, "macOS hosts are not supported for the account audit: /etc/passwd, /etc/shadow and sudoers differ from Linux")
		return audit
	}

	groupMembers := raw.groupMembership()
	users := buildAccountUsers(raw, groupMembers)
	keys, invalid := buildAccountKeys(raw, library)
	findings, notes := evaluateAccountFindings(raw, users, keys)

	// Attach the key files to their user.
	filesPerUser := map[string][]string{}
	unreadPerUser := map[string][]string{}
	for _, k := range keys {
		if k.Valid {
			filesPerUser[k.User] = append(filesPerUser[k.User], k.File)
		} else if k.Error == "unreadable" {
			unreadPerUser[k.User] = append(unreadPerUser[k.User], k.File)
		}
	}
	for i := range users {
		users[i].KeyCount = len(filesPerUser[users[i].Name])
		users[i].KeyFiles = filesPerUser[users[i].Name]
		users[i].KeyFilesUnread = unreadPerUser[users[i].Name]
		if users[i].KeyFiles == nil {
			users[i].KeyFiles = []string{}
		}
		if users[i].KeyFilesUnread == nil {
			users[i].KeyFilesUnread = []string{}
		}
	}

	audit.Users = users
	audit.Keys = keys
	audit.Findings = findings
	audit.Notes = append(audit.Notes, notes...)

	summary := store.AccountAuditSummary{Users: len(users), InvalidLines: invalid}
	unique := map[string]bool{}
	for _, k := range keys {
		if !k.Valid {
			continue
		}
		summary.Keys++
		unique[k.Fingerprint] = true
		if k.Unknown {
			summary.UnknownKeys++
		}
		if k.Weak {
			summary.WeakKeys++
		}
	}
	summary.UniqueKeys = len(unique)
	for _, u := range users {
		if u.LoginCapable {
			summary.LoginUsers++
		}
		if u.UID == 0 && u.Name != "root" {
			summary.UID0Accounts++
		}
	}
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			summary.Critical++
		case "high":
			summary.High++
		case "medium":
			summary.Medium++
		case "low":
			summary.Low++
		case "info":
			summary.Info++
		}
	}
	audit.Summary = summary

	status := "ok"
	if len(raw.users) == 0 {
		status = "partial"
		audit.Notes = append(audit.Notes, "/etc/passwd could not be read: no account could be audited")
	} else {
		for _, n := range notes {
			nl := strings.ToLower(n)
			if strings.Contains(nl, "unknown") || strings.Contains(nl, "could not be read") {
				status = "partial"
				break
			}
		}
	}
	audit.Status = status
	return audit
}
