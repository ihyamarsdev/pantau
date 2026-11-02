package inspector

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

var (
	httpTargetRe  = regexp.MustCompile(`^https?://[^\s'"` + "`" + `$\\;&|<>]{1,512}$`)
	tlsHostnameRe = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)
)

// HTTPExpected holds parsed expected constraints for http rule.
type HTTPExpected struct {
	Statuses   []string      // e.g. ["200"] or ["2xx", "3xx"]
	MaxLatency time.Duration // 0 if not specified
}

// TLSSpec holds parsed target info for tls rule.
type TLSSpec struct {
	Host string
	Port int
}

// ParseHTTPExpected parses the Expected string for kind "http".
// Token comma-separated: status (e.g. "200", "2xx", "3xx") and optional latency "<1500ms"; default "2xx".
func ParseHTTPExpected(expected string) (*HTTPExpected, error) {
	exp := &HTTPExpected{}
	expected = strings.TrimSpace(expected)
	if expected == "" {
		exp.Statuses = []string{"2xx"}
		return exp, nil
	}

	tokens := strings.Split(expected, ",")
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, "<") {
			// Latency threshold, e.g. "<1500ms", "<2s"
			latStr := strings.TrimPrefix(tok, "<")
			d, err := time.ParseDuration(latStr)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("invalid latency threshold: %s", tok)
			}
			exp.MaxLatency = d
			continue
		}

		// Status pattern: exact 3-digit code (e.g. "200") or class (e.g. "2xx", "3xx", "4xx", "5xx")
		tokLower := strings.ToLower(tok)
		if len(tokLower) == 3 {
			if strings.HasSuffix(tokLower, "xx") && tokLower[0] >= '1' && tokLower[0] <= '5' {
				exp.Statuses = append(exp.Statuses, tokLower)
				continue
			}
			if code, err := strconv.Atoi(tokLower); err == nil && code >= 100 && code <= 599 {
				exp.Statuses = append(exp.Statuses, tokLower)
				continue
			}
		}
		return nil, fmt.Errorf("invalid http expected token: %s", tok)
	}

	if len(exp.Statuses) == 0 {
		exp.Statuses = []string{"2xx"}
	}
	return exp, nil
}

// MatchHTTPStatus checks whether a status code matches the expected statuses.
func MatchHTTPStatus(statusCode int, allowed []string) bool {
	codeStr := strconv.Itoa(statusCode)
	for _, a := range allowed {
		if strings.HasSuffix(a, "xx") {
			if len(codeStr) == 3 && codeStr[0] == a[0] {
				return true
			}
		} else if codeStr == a {
			return true
		}
	}
	return false
}

// BuildHTTPCommand builds the SSH command to check an HTTP endpoint from host perspective.
// Target must be validated beforehand.
func BuildHTTPCommand(rawURL string) string {
	return fmt.Sprintf(`curl -sS -o /dev/null -m 10 -w '%%{http_code} %%{time_total}' -- %s`, shellQuote(rawURL))
}

// ParseHTTPOutput parses curl output formatted as `%{http_code} %{time_total}`.
// Returns statusCode and latency.
func ParseHTTPOutput(stdout string) (int, time.Duration, error) {
	fields := strings.Fields(strings.TrimSpace(stdout))
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("unexpected curl output format: %q", stdout)
	}
	code, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid http status code %q: %w", fields[0], err)
	}
	sec, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid time_total %q: %w", fields[1], err)
	}
	dur := time.Duration(sec * float64(time.Second))
	return code, dur, nil
}

// FormatLatency formats duration to ms (or closest ms) e.g. "143ms".
func FormatLatency(d time.Duration) string {
	ms := d.Milliseconds()
	return fmt.Sprintf("%dms", ms)
}

// ValidateHTTPTarget validates target according to spec: ^https?://[^\s'"`$\\;&|<>]{1,512}$
func ValidateHTTPTarget(target string) error {
	if strings.ContainsAny(target, " \t\r\n") {
		return fmt.Errorf("invalid http target URL: cannot contain whitespace or newlines")
	}
	target = strings.TrimSpace(target)
	if !httpTargetRe.MatchString(target) {
		return fmt.Errorf("invalid http target URL: must be valid http(s) URL without shell metacharacters (max 512 chars)")
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid http target: must be a well-formed http or https URL")
	}
	return nil
}

// ParseTLSTarget parses target for kind "tls".
// Target = host[:port] (default 443) or https://host[:port]; host ^[A-Za-z0-9.-]{1,253}$
func ParseTLSTarget(target string) (*TLSSpec, error) {
	target = strings.TrimSpace(target)
	raw := target
	if strings.HasPrefix(raw, "https://") {
		raw = strings.TrimPrefix(raw, "https://")
		if idx := strings.Index(raw, "/"); idx != -1 {
			// strip trailing path if any, but ensure no path was provided or it's just /
			if raw[idx:] != "/" && raw[idx:] != "" {
				return nil, fmt.Errorf("invalid tls target: path not allowed in tls host target")
			}
			raw = raw[:idx]
		}
	} else if strings.Contains(raw, "://") {
		return nil, fmt.Errorf("invalid tls target: scheme must be https:// or omitted")
	}

	host, portStr, err := net.SplitHostPort(raw)
	if err != nil {
		// No port specified
		host = raw
		portStr = "443"
	}

	if !tlsHostnameRe.MatchString(host) {
		return nil, fmt.Errorf("invalid tls target host: %s", host)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid tls target port: %s", portStr)
	}

	return &TLSSpec{Host: host, Port: port}, nil
}

// ParseTLSExpected parses expected for kind "tls": >=Nd (days left, default >=14d).
func ParseTLSExpected(expected string) (int, error) {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return 14, nil
	}
	if !strings.HasPrefix(expected, ">=") || !strings.HasSuffix(expected, "d") {
		return 0, fmt.Errorf("invalid tls expected: must be in format '>=Nd' (e.g. '>=14d')")
	}
	numStr := strings.TrimSuffix(strings.TrimPrefix(expected, ">="), "d")
	days, err := strconv.Atoi(numStr)
	if err != nil || days < 0 {
		return 0, fmt.Errorf("invalid tls expected days: %s", numStr)
	}
	return days, nil
}

// BuildTLSCommand builds the openssl command to check TLS expiry from host perspective.
func BuildTLSCommand(host string, port int) string {
	target := net.JoinHostPort(host, strconv.Itoa(port))
	// openssl s_client -connect H:P -servername H </dev/null 2>/dev/null | openssl x509 -noout -enddate (with timeout 10 if available)
	return fmt.Sprintf(`sh -c 'if command -v timeout >/dev/null 2>&1; then timeout 10 openssl s_client -connect %[1]s -servername %[2]s </dev/null 2>/dev/null | openssl x509 -noout -enddate; else openssl s_client -connect %[1]s -servername %[2]s </dev/null 2>/dev/null | openssl x509 -noout -enddate; fi'`, shellQuote(target), shellQuote(host))
}

// ParseOpenSSLDate parses openssl x509 -enddate output e.g. "notAfter=Jan 2 15:04:05 2027 GMT".
func ParseOpenSSLDate(output string) (time.Time, error) {
	lines := strings.Split(output, "\n")
	var line string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "notAfter=") {
			line = l
			break
		}
	}
	if line == "" {
		return time.Time{}, fmt.Errorf("notAfter= date not found in output: %q", output)
	}

	dateStr := strings.TrimSpace(strings.TrimPrefix(line, "notAfter="))
	// openssl formats:
	// "Jan  2 15:04:05 2027 GMT"
	// "Jan 02 15:04:05 2027 GMT"
	// We normalize multiple spaces to single space
	fields := strings.Fields(dateStr)
	normalized := strings.Join(fields, " ")

	layouts := []string{
		"Jan 2 15:04:05 2006 MST",
		"Jan _2 15:04:05 2006 MST",
		"Jan 02 15:04:05 2006 MST",
		"Jan 2 15:04:05 2006",
		"Jan 02 15:04:05 2006",
		time.RFC822,
		time.RFC822Z,
		time.ANSIC,
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, normalized); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse openssl date: %s", dateStr)
}

// DaysLeft calculates remaining whole days from now until expiry.
// If expiry is in the past, returns a negative value.
func DaysLeft(expiry time.Time, now time.Time) int {
	diff := expiry.Sub(now)
	return int(diff.Hours() / 24)
}

// evaluateHTTPRule evaluates rule of kind "http" from host perspective via SSH.
func evaluateHTTPRule(runner sshrunner.Runner, rule *store.DesiredRule) (current, status, details, rootCause string) {
	if err := ValidateHTTPTarget(rule.Target); err != nil {
		return "invalid_target", "drift", "", err.Error()
	}
	exp, err := ParseHTTPExpected(rule.Expected)
	if err != nil {
		return "invalid_expected", "drift", "", err.Error()
	}

	cmd := BuildHTTPCommand(rule.Target)
	stdout, stderr, exitCode, _ := runner.Exec(cmd)
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)

	if exitCode == 127 || strings.Contains(stderr, "curl: command not found") || strings.Contains(stderr, "curl: not found") {
		return "missing_curl", "drift", "", "curl not found on host"
	}

	if exitCode != 0 {
		errMsg := stderr
		if errMsg == "" {
			errMsg = stdout
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("curl exited with code %d", exitCode)
		}
		if len(errMsg) > 500 {
			errMsg = errMsg[:500] + "..."
		}
		return "connection_error", "drift", "", errMsg
	}

	code, dur, err := ParseHTTPOutput(stdout)
	if err != nil {
		rc := stdout
		if rc == "" {
			rc = stderr
		}
		if len(rc) > 500 {
			rc = rc[:500] + "..."
		}
		return "parse_error", "drift", "", fmt.Sprintf("failed to parse curl output: %v\n%s", err, rc)
	}

	current = fmt.Sprintf("%d in %s", code, FormatLatency(dur))
	details = current

	statusMatch := MatchHTTPStatus(code, exp.Statuses)
	latencyMatch := exp.MaxLatency <= 0 || dur <= exp.MaxLatency

	if statusMatch && latencyMatch {
		status = "ok"
	} else {
		status = "drift"
		var reasons []string
		if !statusMatch {
			reasons = append(reasons, fmt.Sprintf("HTTP status %d does not match expected %s", code, strings.Join(exp.Statuses, ",")))
		}
		if !latencyMatch {
			reasons = append(reasons, fmt.Sprintf("latency %s exceeds threshold <%s", FormatLatency(dur), exp.MaxLatency))
		}
		rootCause = strings.Join(reasons, "; ")
	}
	return
}

// evaluateTLSRule evaluates rule of kind "tls" from host perspective via SSH.
func evaluateTLSRule(runner sshrunner.Runner, rule *store.DesiredRule, nowFn func() time.Time) (current, status, details, rootCause string) {
	spec, err := ParseTLSTarget(rule.Target)
	if err != nil {
		return "invalid_target", "drift", "", err.Error()
	}
	minDays, err := ParseTLSExpected(rule.Expected)
	if err != nil {
		return "invalid_expected", "drift", "", err.Error()
	}
	if nowFn == nil {
		nowFn = time.Now
	}

	cmd := BuildTLSCommand(spec.Host, spec.Port)
	stdout, stderr, exitCode, _ := runner.Exec(cmd)
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)

	if exitCode == 127 || strings.Contains(stderr, "openssl: command not found") || strings.Contains(stderr, "openssl: not found") {
		return "missing_openssl", "drift", "", "openssl not found on host"
	}

	expiry, err := ParseOpenSSLDate(stdout)
	if err != nil {
		rc := stderr
		if rc == "" {
			rc = stdout
		}
		if rc == "" {
			rc = fmt.Sprintf("openssl handshake or cert extraction failed (exit %d)", exitCode)
		}
		if len(rc) > 500 {
			rc = rc[:500] + "..."
		}
		return "handshake_failed", "drift", "", rc
	}

	now := nowFn()
	days := DaysLeft(expiry, now)
	dateStr := expiry.UTC().Format("2006-01-02")
	current = fmt.Sprintf("%dd left (expires %s)", days, dateStr)
	details = current

	if days >= minDays {
		status = "ok"
	} else {
		status = "drift"
		if days < 0 {
			rootCause = fmt.Sprintf("Certificate for %s:%d expired %d days ago on %s", spec.Host, spec.Port, -days, dateStr)
		} else {
			rootCause = fmt.Sprintf("Certificate for %s:%d expires in %dd (%s), below threshold >=%dd", spec.Host, spec.Port, days, dateStr, minDays)
		}
	}
	return
}
