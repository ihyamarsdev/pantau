package teleport

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode"
)

// maxLoginCommandBytes bounds a pasted command. A real `tsh login` line is a few hundred bytes.
const maxLoginCommandBytes = 8 << 10

// shellMetaChars chain, substitute or redirect commands. A tsh login line containing any of them is refused,
// even inside quotes, because no valid profile value needs them.
const shellMetaChars = ";&|`$<>(){}\\"

// valueFlags map a flag to the profile field it fills. "-u" is the short form of --user.
var valueFlags = map[string]string{"proxy": "proxy", "auth": "auth", "user": "user", "cluster": "cluster"}

// ignorableFlags are Teleport flags that do not select a profile. The value is whether the flag takes a value.
var ignorableFlags = map[string]bool{
	"browser": true, "ttl": true, "mfa-mode": true, "insecure": false, "request-id": true, "skip-version-check": false,
}

var envAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// LoginCommand is what a pasted `tsh login` command maps to. Pantau never runs the command; it only reads it.
type LoginCommand struct {
	Profile        Profile  // every field has passed ValidateProfile; ID is zero
	NameSuggestion string   // TeleportCluster, or the proxy host when no cluster was given
	IgnoredFlags   []string // flags that do not select a profile, reported so the mapping stays transparent
	Warnings       []string
}

// ParseLoginCommand reads one `tsh login` command out of pasted text, such as a Teleport Web UI copy. It accepts
// `--flag value` and `--flag=value`, quotes, a `$ ` prompt, backslash line continuations, a leading
// TELEPORT_HOME=... assignment or an `export TELEPORT_HOME=...` line. Other lines (text, `export KUBECONFIG`,
// `tsh kube login`, `kubectl`) are skipped. It is pure: it runs nothing, and its errors never echo the input.
func ParseLoginCommand(raw string) (LoginCommand, error) {
	if len(raw) > maxLoginCommandBytes {
		return LoginCommand{}, fmt.Errorf("%w: command is too long", ErrInvalidInput)
	}
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	for _, r := range text {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return LoginCommand{}, fmt.Errorf("%w: command contains control characters", ErrInvalidInput)
		}
	}
	var sc loginScan
	for _, line := range logicalLines(text) {
		if err := sc.scanLine(line); err != nil {
			return LoginCommand{}, err
		}
	}
	if !sc.found {
		return LoginCommand{}, fmt.Errorf("%w: no tsh login command found", ErrInvalidInput)
	}
	return sc.result()
}

// logicalLines strips the prompt and joins backslash continuations, so each element is one shell line.
func logicalLines(text string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "$ "))
		if strings.HasSuffix(line, `\`) {
			cur.WriteString(strings.TrimSuffix(line, `\`) + " ")
			continue
		}
		cur.WriteString(line)
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// loginScan collects what the lines say before the flags are read.
type loginScan struct {
	found bool
	home  string
	args  []string // tokens after "tsh login"
}

func (sc *loginScan) scanLine(line string) error {
	words := strings.Fields(line)
	if len(words) == 0 {
		return nil
	}
	if words[0] != "export" && words[0] != "tsh" && !envAssignRe.MatchString(words[0]) {
		return nil
	}
	tokens, err := splitLoginWords(line)
	if err != nil {
		if len(words) > 1 && words[0] == "tsh" && words[1] == "login" {
			return fmt.Errorf("%w: unterminated quote in tsh login command", ErrInvalidInput)
		}
		return nil // prose with an apostrophe, not a command
	}
	i := 0
	for i < len(tokens) && envAssignRe.MatchString(tokens[i]) {
		i++
	}
	switch {
	case i+1 < len(tokens) && tokens[i] == "tsh" && tokens[i+1] == "login":
		if err := rejectShellSyntax(line); err != nil {
			return err
		}
		if sc.found {
			return fmt.Errorf("%w: more than one tsh login command", ErrInvalidInput)
		}
		sc.found = true
		for _, a := range tokens[:i] {
			if err := sc.setPrefixEnv(a); err != nil {
				return err
			}
		}
		sc.args = tokens[i+2:]
	case i == 0 && len(tokens) == 2 && tokens[0] == "export":
		if err := rejectShellSyntax(line); err != nil {
			return err
		}
		if name, val, ok := strings.Cut(tokens[1], "="); ok && name == "TELEPORT_HOME" {
			return sc.setHome(val)
		}
	}
	return nil
}

func (sc *loginScan) setPrefixEnv(assign string) error {
	name, val, _ := strings.Cut(assign, "=")
	switch name {
	case "TELEPORT_HOME":
		return sc.setHome(val)
	case "KUBECONFIG":
		return nil
	default:
		return fmt.Errorf("%w: unsupported environment variable before tsh login", ErrInvalidInput)
	}
}

func (sc *loginScan) setHome(val string) error {
	if sc.home != "" && val != "" && sc.home != val {
		return fmt.Errorf("%w: conflicting TELEPORT_HOME values", ErrInvalidInput)
	}
	if val != "" {
		sc.home = val
	}
	return nil
}

func rejectShellSyntax(line string) error {
	if strings.ContainsAny(line, shellMetaChars) {
		return fmt.Errorf("%w: command contains shell syntax that is not allowed", ErrInvalidInput)
	}
	return nil
}

// splitLoginWords splits on blanks outside quotes. Quotes are removed; no escapes are recognised.
func splitLoginWords(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	inWord := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, b.String())
				b.Reset()
				inWord = false
			}
		default:
			b.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("%w: unterminated quote", ErrInvalidInput)
	}
	if inWord {
		out = append(out, b.String())
	}
	return out, nil
}

func (sc *loginScan) result() (LoginCommand, error) {
	values := map[string]string{}
	ignored := []string{}
	set := func(key, val string) error {
		if _, dup := values[key]; dup {
			return fmt.Errorf("%w: flag --%s is given more than once", ErrInvalidInput, key)
		}
		values[key] = val
		return nil
	}
	args := sc.args
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if !strings.HasPrefix(tok, "-") {
			if tok == "" {
				return LoginCommand{}, fmt.Errorf("%w: empty argument", ErrInvalidInput)
			}
			if err := set("cluster", tok); err != nil {
				return LoginCommand{}, err
			}
			continue
		}
		long := strings.HasPrefix(tok, "--")
		body := tok[1:]
		if long {
			body = tok[2:]
		}
		name, val, hasVal := strings.Cut(body, "=")
		if !long && name != "u" {
			return LoginCommand{}, flagError(name)
		}
		if !long {
			name = "user"
		}
		if field, ok := valueFlags[name]; ok {
			if !hasVal {
				if i+1 >= len(args) {
					return LoginCommand{}, flagError(name)
				}
				i++
				val = args[i]
			}
			if val == "" {
				return LoginCommand{}, flagError(name)
			}
			if err := set(field, val); err != nil {
				return LoginCommand{}, err
			}
			continue
		}
		takesValue, ok := ignorableFlags[name]
		if !long || !ok {
			return LoginCommand{}, flagError(name)
		}
		if takesValue && !hasVal {
			if i+1 >= len(args) {
				return LoginCommand{}, flagError(name)
			}
			i++
		}
		ignored = append(ignored, name)
	}

	p := Profile{
		Proxy: values["proxy"], AuthConnector: values["auth"], Username: values["user"],
		TeleportCluster: values["cluster"], TeleportHome: sc.home,
	}
	if err := ValidateProfile(p); err != nil {
		return LoginCommand{}, err
	}
	name := p.TeleportCluster
	if name == "" {
		name = proxyHost(p.Proxy)
	}
	warnings := []string{}
	if p.AuthConnector == "" {
		warnings = append(warnings, "auth connector not set; tsh chooses its default")
	}
	if p.TeleportCluster == "" {
		warnings = append(warnings, "cluster not set; the profile name comes from the proxy host")
	}
	return LoginCommand{Profile: p, NameSuggestion: name, IgnoredFlags: ignored, Warnings: warnings}, nil
}

// flagError names the flag only when the name is a plain lowercase word, so arbitrary input is never echoed.
func flagError(name string) error {
	if safeFlagName.MatchString(name) {
		return fmt.Errorf("%w: unsupported or malformed tsh login flag --%s", ErrInvalidInput, name)
	}
	return fmt.Errorf("%w: unsupported or malformed tsh login flag", ErrInvalidInput)
}

var safeFlagName = regexp.MustCompile(`^[a-z][a-z-]{0,31}$`)

// proxyHost returns the host of a validated proxy ("host:port" or "host").
func proxyHost(proxy string) string {
	host, _, err := net.SplitHostPort(proxy)
	if err != nil {
		return proxy
	}
	return host
}
