package inspector

import (
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestParseHTTPExpected(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantCodes []string
		wantLat   time.Duration
		wantErr   bool
	}{
		{"empty default", "", []string{"2xx"}, 0, false},
		{"single status", "200", []string{"200"}, 0, false},
		{"status class", "2xx", []string{"2xx"}, 0, false},
		{"multiple statuses", "200,3xx", []string{"200", "3xx"}, 0, false},
		{"status and latency", "200,<1500ms", []string{"200"}, 1500 * time.Millisecond, false},
		{"latency only default status", "<500ms", []string{"2xx"}, 500 * time.Millisecond, false},
		{"latency in seconds", "2xx,<2s", []string{"2xx"}, 2 * time.Second, false},
		{"invalid status", "99", nil, 0, true},
		{"invalid status 600", "600", nil, 0, true},
		{"invalid latency", "<abc", nil, 0, true},
		{"negative latency", "<-10ms", nil, 0, true},
		{"unknown token", "foo", nil, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHTTPExpected(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseHTTPExpected(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil {
				if len(got.Statuses) != len(tt.wantCodes) {
					t.Fatalf("statuses length mismatch: got %v, want %v", got.Statuses, tt.wantCodes)
				}
				for i := range got.Statuses {
					if got.Statuses[i] != tt.wantCodes[i] {
						t.Errorf("status[%d]: got %s, want %s", i, got.Statuses[i], tt.wantCodes[i])
					}
				}
				if got.MaxLatency != tt.wantLat {
					t.Errorf("latency: got %v, want %v", got.MaxLatency, tt.wantLat)
				}
			}
		})
	}
}

func TestValidateHTTPTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"valid http", "http://example.com", false},
		{"valid https with port and path", "https://api.example.com:8443/healthz", false},
		{"valid query params", "https://example.com/check?service=auth", false},
		{"missing scheme", "example.com/healthz", true},
		{"ftp scheme", "ftp://example.com", true},
		{"shell injection semicolon", "https://example.com;rm -rf /", true},
		{"shell injection dollar", "https://example.com/$(whoami)", true},
		{"shell injection backtick", "https://example.com/`id`", true},
		{"shell injection space", "https://example.com/foo bar", true},
		{"shell injection pipe", "https://example.com|cat", true},
		{"shell injection ampersand", "https://example.com&whoami", true},
		{"newline injection", "https://example.com\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHTTPTarget(tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHTTPTarget(%q) err = %v, wantErr %v", tt.target, err, tt.wantErr)
			}
		})
	}
}

func TestParseHTTPOutput(t *testing.T) {
	code, dur, err := ParseHTTPOutput("200 0.143\n")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 200 {
		t.Errorf("code: got %d, want 200", code)
	}
	if dur < 140*time.Millisecond || dur > 150*time.Millisecond {
		t.Errorf("duration: got %v", dur)
	}

	_, _, err = ParseHTTPOutput("not_found")
	if err == nil {
		t.Errorf("expected error on malformed curl output")
	}
}

func TestBuildHTTPCommand(t *testing.T) {
	cmd := BuildHTTPCommand("https://example.com/api?a=1&b=2")
	expected := "curl -sS -o /dev/null -m 10 -w '%{http_code} %{time_total}' -- 'https://example.com/api?a=1&b=2'"
	if cmd != expected {
		t.Errorf("got %q, want %q", cmd, expected)
	}
}

func TestParseTLSTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{"hostname default port", "example.com", "example.com", 443, false},
		{"hostname with port", "example.com:8443", "example.com", 8443, false},
		{"https prefix", "https://example.com", "example.com", 443, false},
		{"https prefix with trailing slash", "https://example.com/", "example.com", 443, false},
		{"https prefix with port", "https://example.com:8443", "example.com", 8443, false},
		{"invalid scheme http", "http://example.com", "", 0, true},
		{"path in target", "https://example.com/path", "", 0, true},
		{"invalid port", "example.com:70000", "", 0, true},
		{"invalid host chars semicolon", "example.com;rm", "", 0, true},
		{"invalid host chars dollar", "example.com$(id)", "", 0, true},
		{"invalid host chars space", "example .com", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTLSTarget(tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTLSTarget(%q) err = %v, wantErr %v", tt.target, err, tt.wantErr)
			}
			if err == nil {
				if got.Host != tt.wantHost || got.Port != tt.wantPort {
					t.Errorf("got (%s, %d), want (%s, %d)", got.Host, got.Port, tt.wantHost, tt.wantPort)
				}
			}
		})
	}
}

func TestParseTLSExpected(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 14, false},
		{">=14d", 14, false},
		{">=30d", 30, false},
		{">=0d", 0, false},
		{"14d", 0, true},
		{">=14", 0, true},
		{">=-5d", 0, true},
		{">=abcd", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTLSExpected(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTLSExpected(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBuildTLSCommand(t *testing.T) {
	cmd := BuildTLSCommand("example.com", 443)
	if !strings.Contains(cmd, "openssl s_client -connect 'example.com:443' -servername 'example.com'") {
		t.Errorf("BuildTLSCommand generated unexpected command: %s", cmd)
	}
}

func TestParseOpenSSLDate_AndDaysLeft(t *testing.T) {
	// openssl output sample with odd spacing / GMT
	out := "notAfter=Jan  2 15:04:05 2027 GMT\n"
	expiry, err := ParseOpenSSLDate(out)
	if err != nil {
		t.Fatalf("ParseOpenSSLDate failed: %v", err)
	}

	expected := time.Date(2027, time.January, 2, 15, 4, 5, 0, time.UTC)
	if !expiry.Equal(expected) {
		t.Errorf("expiry: got %v, want %v", expiry, expected)
	}

	// Clock injection
	now := time.Date(2026, time.November, 2, 15, 4, 5, 0, time.UTC)
	days := DaysLeft(expiry, now)
	if days != 61 {
		t.Errorf("DaysLeft: got %d, want 61", days)
	}

	// Test past expiry
	nowFuture := time.Date(2027, time.January, 5, 15, 4, 5, 0, time.UTC)
	daysPast := DaysLeft(expiry, nowFuture)
	if daysPast != -2 && daysPast != -3 {
		t.Errorf("DaysLeft in past: got %d", daysPast)
	}
}

func TestEvaluateHTTPRule(t *testing.T) {
	mockRunner := sshrunner.NewMockRunner()

	// 1. Success case: 200 OK within 500ms
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "curl ") {
			return "200 0.143\n", "", 0, nil
		}
		return "", "", 1, nil
	}
	rule := &store.DesiredRule{Kind: "http", Target: "https://example.com/api", Expected: "200,<500ms"}
	curr, status, _, rc := evaluateHTTPRule(mockRunner, rule)
	if status != "ok" || curr != "200 in 143ms" {
		t.Errorf("expected ok/200 in 143ms, got %s/%s (rc: %s)", status, curr, rc)
	}

	// 2. Status mismatch: got 500
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "500 0.050\n", "", 0, nil
	}
	curr, status, _, rc = evaluateHTTPRule(mockRunner, rule)
	if status != "drift" || !strings.Contains(rc, "HTTP status 500 does not match expected 200") {
		t.Errorf("expected drift for status 500, got %s/%s (rc: %s)", status, curr, rc)
	}

	// 3. Latency exceeded: 1.2s > 500ms
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "200 1.200\n", "", 0, nil
	}
	curr, status, _, rc = evaluateHTTPRule(mockRunner, rule)
	if status != "drift" || !strings.Contains(rc, "exceeds threshold <500ms") {
		t.Errorf("expected drift for latency, got %s/%s (rc: %s)", status, curr, rc)
	}

	// 4. curl missing
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "sh: curl: command not found\n", 127, nil
	}
	curr, status, _, rc = evaluateHTTPRule(mockRunner, rule)
	if status != "drift" || curr != "missing_curl" || rc != "curl not found on host" {
		t.Errorf("expected missing_curl drift, got %s/%s (rc: %s)", status, curr, rc)
	}
}

func TestEvaluateTLSRule(t *testing.T) {
	mockRunner := sshrunner.NewMockRunner()
	fixedNow := time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return fixedNow }

	// 1. Success case: certificate expires 2027-01-02 (61 days left >= 14d)
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "openssl ") {
			return "notAfter=Jan  2 00:00:00 2027 GMT\n", "", 0, nil
		}
		return "", "", 1, nil
	}
	rule := &store.DesiredRule{Kind: "tls", Target: "example.com:443", Expected: ">=14d"}
	curr, status, details, rc := evaluateTLSRule(mockRunner, rule, nowFn)
	if status != "ok" || curr != "61d left (expires 2027-01-02)" {
		t.Errorf("expected ok/61d left, got %s/%s (details: %s, rc: %s)", status, curr, details, rc)
	}

	// 2. Expiry drift: expires in 5 days < 14d
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "notAfter=Nov  7 00:00:00 2026 GMT\n", "", 0, nil
	}
	curr, status, _, rc = evaluateTLSRule(mockRunner, rule, nowFn)
	if status != "drift" || !strings.Contains(rc, "below threshold >=14d") {
		t.Errorf("expected drift for expiry < 14d, got %s/%s (rc: %s)", status, curr, rc)
	}

	// 3. openssl missing
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "sh: openssl: not found\n", 127, nil
	}
	curr, status, _, rc = evaluateTLSRule(mockRunner, rule, nowFn)
	if status != "drift" || curr != "missing_openssl" || rc != "openssl not found on host" {
		t.Errorf("expected missing_openssl drift, got %s/%s (rc: %s)", status, curr, rc)
	}

	// 4. Handshake failed
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "connect: Connection refused\n", 1, nil
	}
	curr, status, _, rc = evaluateTLSRule(mockRunner, rule, nowFn)
	if status != "drift" || curr != "handshake_failed" || !strings.Contains(rc, "Connection refused") {
		t.Errorf("expected handshake_failed, got %s/%s (rc: %s)", status, curr, rc)
	}
}
