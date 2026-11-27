package inspector

import (
	"strings"
	"testing"
	"unicode"
)

// fuzzSecret keeps only ASCII letters and digits, so the redaction regexes consume the whole value and
// the leak oracle has no delimiter edge cases to reason about.
func fuzzSecret(s string, minLen int) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	v := b.String()
	return v, len(v) >= minLen
}

// leaksSecret reports whether the secret survived redaction. A secret that is also a substring of the
// surrounding literal text (or of the [REDACTED] marker) is skipped, because it would match without leaking.
func leaksSecret(out, secret string, literals ...string) bool {
	for _, lit := range literals {
		if strings.Contains(lit, secret) {
			return false
		}
	}
	return strings.Contains(out, secret)
}

// FuzzRedactAgentSecrets is the no-panic guard for arbitrary remote output reaching the agent.
func FuzzRedactAgentSecrets(f *testing.F) {
	f.Add("password=hunter2 ok")
	f.Add("Authorization: Bearer abcdefgh12345678")
	f.Add("-----BEGIN RSA PRIVATE KEY-----\nMIIEsecret\n-----END RSA PRIVATE KEY-----")
	f.Add("api_key: sk-123456789 token=abc")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		_ = RedactAgentSecrets(s)
	})
}

// FuzzRedactAgentAssignment checks that a KEY=value secret never survives redaction.
func FuzzRedactAgentAssignment(f *testing.F) {
	f.Add("hunter2hunter2")
	f.Add("sk-live-0123456789")
	f.Fuzz(func(t *testing.T, value string) {
		v, ok := fuzzSecret(value, 6)
		if !ok {
			t.Skip()
		}
		out := RedactAgentSecrets("password=" + v + " end")
		if leaksSecret(out, v, "password= end[REDACTED]") {
			t.Errorf("assignment value leaked: input %q -> %q", v, out)
		}
	})
}

// FuzzRedactAgentBearer checks that a bearer token never survives redaction.
func FuzzRedactAgentBearer(f *testing.F) {
	f.Add("abcdefgh12345678")
	f.Fuzz(func(t *testing.T, token string) {
		v, ok := fuzzSecret(token, 8)
		if !ok {
			t.Skip()
		}
		out := RedactAgentSecrets("Authorization: Bearer " + v + " done")
		if leaksSecret(out, v, "Authorization: Bearer  done[REDACTED]") {
			t.Errorf("bearer token leaked: input %q -> %q", v, out)
		}
	})
}

// FuzzRedactAgentPrivateKey checks that a PEM body never survives redaction.
func FuzzRedactAgentPrivateKey(f *testing.F) {
	f.Add("MIIEowIBAAKCAQEAu1")
	f.Fuzz(func(t *testing.T, body string) {
		v, ok := fuzzSecret(body, 6)
		if !ok {
			t.Skip()
		}
		const header = "-----BEGIN RSA PRIVATE KEY-----"
		const footer = "-----END RSA PRIVATE KEY-----"
		out := RedactAgentSecrets(header + "\n" + v + "\n" + footer)
		if leaksSecret(out, v, header+footer+"[REDACTED]") {
			t.Errorf("private key body leaked: input %q -> %q", v, out)
		}
	})
}
