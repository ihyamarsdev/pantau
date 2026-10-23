package sshconfig

import (
	"reflect"
	"strings"
	"testing"
)

func byAlias(t *testing.T, r Result) map[string]Entry {
	t.Helper()
	m := map[string]Entry{}
	for _, e := range r.Entries {
		m[e.Alias] = e
	}
	return m
}

func TestParseRealWorldLayout(t *testing.T) {
	// Trailing spaces, mixed-case keywords, comment headers, a block without
	// Port/User and a Windows line ending, as found in hand-edited configs.
	cfg := "# === Server ===\r\n" +
		"Host gettalk \r\n" +
		"    HostName 103.98.104.227\r\n" +
		"    User ihya \r\n" +
		"    Port 65522\r\n" +
		"\r\n" +
		"Host netbird\n" +
		"    Hostname 103.98.104.227\n" +
		"    User ihya \n" +
		"    Port 65522\n" +
		"\n" +
		"Host macstudio-3\n" +
		"    Hostname 100.100.20.20\n" +
		"    User macstudio3 \n" +
		"\n" +
		"Host sumopod\n" +
		"  HostName 43.129.55.200\n" +
		"  User ubuntu\n" +
		"  Port 22\n" +
		"  IdentityFile /home/uya/.ssh/id_rsa\n" +
		"  IdentitiesOnly yes\n"

	r := Parse(cfg)
	if len(r.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", r.Warnings)
	}
	got := []string{}
	for _, e := range r.Entries {
		got = append(got, e.Alias)
	}
	if want := []string{"gettalk", "netbird", "macstudio-3", "sumopod"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("aliases/order = %v, want %v", got, want)
	}
	m := byAlias(t, r)
	if e := m["gettalk"]; e.HostName != "103.98.104.227" || e.User != "ihya" || e.Port != 65522 {
		t.Errorf("gettalk = %+v", e)
	}
	if e := m["macstudio-3"]; e.Port != 22 || e.User != "macstudio3" {
		t.Errorf("macstudio-3 should default Port to 22, got %+v", e)
	}
	if e := m["sumopod"]; e.IdentityFile != "/home/uya/.ssh/id_rsa" {
		t.Errorf("sumopod identity = %q", e.IdentityFile)
	}
}

func TestParseWildcardDefaultsAndFirstValueWins(t *testing.T) {
	cfg := `
Host web
    HostName 10.0.0.1
    User deploy

Host *
    User fallback
    Port 2222

Host db
    HostName 10.0.0.2
`
	m := byAlias(t, Parse(cfg))
	if _, ok := m["*"]; ok {
		t.Fatal("wildcard pattern must not become an entry")
	}
	if e := m["web"]; e.User != "deploy" || e.Port != 2222 {
		t.Errorf("web: own User must win, Host * supplies Port; got %+v", e)
	}
	if e := m["db"]; e.User != "fallback" || e.Port != 2222 {
		t.Errorf("db must inherit Host * defaults; got %+v", e)
	}
}

func TestParseWildcardBeforeSpecificWins(t *testing.T) {
	// OpenSSH uses the first obtained value, so an earlier "Host *" overrides
	// a later specific block.
	cfg := "Host *\n  Port 2200\nHost app\n  HostName 1.1.1.1\n  Port 22\n"
	if e := byAlias(t, Parse(cfg))["app"]; e.Port != 2200 {
		t.Errorf("first value must win, got Port %d", e.Port)
	}
}

func TestParseMultiAliasPatternsAndNegation(t *testing.T) {
	cfg := `
Host a b !c *.internal
    HostName shared.example.com
    User ops

Host c
    HostName c.example.com

Host prod-*
    User prodops
`
	r := Parse(cfg)
	m := byAlias(t, r)
	for _, alias := range []string{"a", "b", "c"} {
		if _, ok := m[alias]; !ok {
			t.Fatalf("missing alias %q in %v", alias, r.Entries)
		}
	}
	if len(m) != 3 {
		t.Errorf("only concrete aliases a, b, c expected, got %v", r.Entries)
	}
	if e := m["a"]; e.HostName != "shared.example.com" || e.User != "ops" {
		t.Errorf("a = %+v", e)
	}
	if e := m["c"]; e.HostName != "c.example.com" || e.User != "" {
		t.Errorf("negated pattern must not apply to c; got %+v", e)
	}
}

func TestParseKeyValueFormsQuotesAndHostNameToken(t *testing.T) {
	cfg := "Host one\n  HostName=1.2.3.4\n  User = \"quoted user\"\n  Port=2022\n" +
		"Host two\n  HostName %h.example.com\n"
	m := byAlias(t, Parse(cfg))
	if e := m["one"]; e.HostName != "1.2.3.4" || e.User != "quoted user" || e.Port != 2022 {
		t.Errorf("one = %+v", e)
	}
	if e := m["two"]; e.HostName != "two.example.com" {
		t.Errorf("%%h must expand to the alias, got %q", e.HostName)
	}
}

func TestParseHostNameDefaultsToAlias(t *testing.T) {
	m := byAlias(t, Parse("Host server.example.com\n  User root\n"))
	if e := m["server.example.com"]; e.HostName != "server.example.com" {
		t.Errorf("HostName must default to the alias, got %q", e.HostName)
	}
}

func TestParseInvalidPortFallsBackWithWarning(t *testing.T) {
	r := Parse("Host bad\n  HostName 1.1.1.1\n  Port abc\nHost big\n  HostName 2.2.2.2\n  Port 70000\n")
	m := byAlias(t, r)
	if m["bad"].Port != 22 || m["big"].Port != 22 {
		t.Errorf("invalid ports must fall back to 22: %+v %+v", m["bad"], m["big"])
	}
	if len(r.Warnings) != 2 {
		t.Errorf("expected one warning per invalid port, got %v", r.Warnings)
	}
}

func TestParseMatchAndIncludeAreReportedNotApplied(t *testing.T) {
	cfg := `
Include ~/.ssh/extra.conf

Host kept
    HostName 9.9.9.9
    User real

Match host kept
    User overridden
    Port 9999

Host after
    HostName 8.8.8.8
`
	r := Parse(cfg)
	m := byAlias(t, r)
	if e := m["kept"]; e.User != "real" || e.Port != 22 {
		t.Errorf("Match options must not leak into the Host block: %+v", e)
	}
	if _, ok := m["after"]; !ok {
		t.Error("Host after a Match block must still be parsed")
	}
	joined := strings.Join(r.Warnings, "|")
	if !strings.Contains(joined, "Match") || !strings.Contains(joined, "Include") {
		t.Errorf("warnings must mention Match and Include, got %v", r.Warnings)
	}
}

func TestParseDuplicateAliasKeepsFirst(t *testing.T) {
	r := Parse("Host dup\n  HostName 1.1.1.1\nHost dup\n  HostName 2.2.2.2\n")
	if len(r.Entries) != 1 || r.Entries[0].HostName != "1.1.1.1" {
		t.Errorf("duplicate alias must yield one entry from the first block, got %v", r.Entries)
	}
}

func TestParseEmptyAndCommentsOnly(t *testing.T) {
	if r := Parse("\n# nothing here\n   \n"); len(r.Entries) != 0 || len(r.Warnings) != 0 {
		t.Errorf("got %+v", r)
	}
}
