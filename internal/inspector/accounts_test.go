package inspector

import (
	"strings"
	"testing"

	"pantau/internal/store"
)

// Fixtures generated with ssh-keygen; the expected fingerprints come from `ssh-keygen -lf`.
const (
	fpEd25519 = "SHA256:iwnxhDJ7Bu9+6fD+yUMknPueUKOb6C5rjg5A1nO8MEI"
	fpRSA1024 = "SHA256:siecPIm4of+aeHcEHGODVrnVCWFwuOhzEA+BpyJDGJU"
	fpRSA2048 = "SHA256:fQ0bUHcZ+EAmhQvw/0OzkH0K/zjKZofZN5jo3zIk11I"
	fpRSA4096 = "SHA256:Vxu7zwuOn3dnEaQQzuUmy62AKzhdXCUQaicdEn+nv60"

	keyEd25519 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAyDttlR5N6LzJpQggGTLaPwKD20njfygAnLl3Q5UMBD ed@example"
	keyRSA1024 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQDJVCWXwHUlqL+FByFxtANBUArjtsoS9GUtfe+b6DYDUqpG89w8wvyhdCz0YyUYYnskNtGgo10651AZEFa4eK2lVrExzU31DBo7qP0o3pls7y3OlfouiJt7ptz4+DSD/CZD/cE0vxp6Ej5Ez371ZHYP26pAWhRSQPag0SzOZDTiNQ== rsa1024@example"
	keyRSA2048 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC/otUEUIn9ViUZcCN8KBFcVRcEO1fV/8d2ppQ91o7X5hO3XmcLbVenduRuJAYxvnhVRvXneiG3gpjndROH6q/gjdVcXA4YR6F18R3hRY4s10BZ5eKLZ4kwA2ZjO0UdZy3PRZKxcohYDWxNfFsrxoZZPPQSO5IcSoqVU/uJM+tl17E7YMbuevMTNZNIgkHi+/qTKYl6ber/TVv9jasrJ7Y73TA/MNfkbsM4E9ug465TK1eDSfyDQDpgiZfI81wq8yYCrhQHBtJMg6WKC4LItxEEf6CvWUA7uqt/unYfzTckNNyucEEcr+GuOUbpXH8/zJ3gLY+kTQyst6kPtSfNv3Tx rsa2048@example"
	keyRSA4096 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQCgHu4TlsakmlmH5G7OJ+YiHY3O1Vn4O8vnV1B7/OKVxHOPfCeMeEyG1sUnExdeBeHYNiI1B64gWeCc8sCoHg5UPYHDcXfX4MJAyyIAlBATuwOzt8Th/yzz25y+wxzGySasauAcDoQo4nEWQqWMCGGxidAG+tCEqeFkWZUqyrmsyg4Z3Hd9Q8qiRz1Sd9F++SQP7vYxDqULkSOcYw1OlynINHNApvbgViv84iWaNyMhqnmVFWrPhD7AWK97jXGrzV//Svk4UMtNZf8ixGKElKAIO5/JaK64LiDsgzY9hICPtcn9vQItnT5HHBXzyFsnAEqCNsjnePtv7khdOIvq3aF+ZA3Tvz5oOo3uNxmdDo684M4WWVEfaT80YFtENUSz2LYXWYc/dHe+WkFwa3LWS0OY/gElXm0oIBie74OYlUkFqAESlpLHyLVhBLg75tg6Y1+9ovN79YP+hKHNhLeSjr1fqDMB74ApHfExsFOAUbmlz4ZwsmKiyiKZQjZHWTIOfIollOEIS5qgOz9xreIJB4uE/lCnUsOy5zTjYwJoRmmVYq/BuKfnfkOTKiceMI2DIHMOBizfxAx6kNobW9sIKULZ9Hfm71kkT6j0r2HWKI6Z2YvVDdZfsqAzYseAZg2O5mtppgLfheP12v8Jqt5HiCF8UdkyWeXw2c/X+KMmUS/wAw== rsa4096@example"
)

func TestParseAuthorizedKeyLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantType   string
		wantBits   int
		wantFP     string
		wantWeak   bool
		wantCom    string
		opts       []string
		restricted bool
		from       bool
	}{
		{name: "ed25519", line: keyEd25519, wantType: "ssh-ed25519", wantBits: 0, wantFP: fpEd25519, wantCom: "ed@example"},
		{name: "rsa1024", line: keyRSA1024, wantType: "ssh-rsa", wantBits: 1024, wantFP: fpRSA1024, wantWeak: true, wantCom: "rsa1024@example"},
		{name: "rsa2048", line: keyRSA2048, wantType: "ssh-rsa", wantBits: 2048, wantFP: fpRSA2048, wantCom: "rsa2048@example"},
		{name: "rsa4096", line: keyRSA4096, wantType: "ssh-rsa", wantBits: 4096, wantFP: fpRSA4096, wantCom: "rsa4096@example"},
		{
			name:     "options with quoted comma",
			line:     `command="/bin/backup --full,verbose",from="10.0.0.0/8" ` + keyEd25519,
			wantType: "ssh-ed25519", wantFP: fpEd25519, wantCom: "ed@example",
			opts:       []string{`command="/bin/backup --full,verbose"`, `from="10.0.0.0/8"`},
			restricted: true, from: true,
		},
		{
			name:     "no-pty only",
			line:     "no-pty " + keyEd25519,
			wantType: "ssh-ed25519", wantFP: fpEd25519, wantCom: "ed@example",
			opts: []string{"no-pty"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k, err := parseAuthorizedKeyLine(tc.line)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if k.Type != tc.wantType {
				t.Errorf("type = %q, want %q", k.Type, tc.wantType)
			}
			if k.Bits != tc.wantBits {
				t.Errorf("bits = %d, want %d", k.Bits, tc.wantBits)
			}
			if k.Fingerprint != tc.wantFP {
				t.Errorf("fingerprint = %q, want %q", k.Fingerprint, tc.wantFP)
			}
			if k.Weak != tc.wantWeak {
				t.Errorf("weak = %v, want %v", k.Weak, tc.wantWeak)
			}
			if k.Comment != tc.wantCom {
				t.Errorf("comment = %q, want %q", k.Comment, tc.wantCom)
			}
			if len(tc.opts) > 0 {
				if strings.Join(k.Options, ",") != strings.Join(tc.opts, ",") {
					t.Errorf("options = %v, want %v", k.Options, tc.opts)
				}
			}
			if k.Restricted != tc.restricted {
				t.Errorf("restricted = %v, want %v", k.Restricted, tc.restricted)
			}
			if k.SourceRestricted != tc.from {
				t.Errorf("source restricted = %v, want %v", k.SourceRestricted, tc.from)
			}
		})
	}
}

func TestParseAuthorizedKeyLineGarbage(t *testing.T) {
	for _, line := range []string{"not-a-key", "ssh-rsa AAAAshort", "ssh-ed25519 AAAAB3NzaC1lZDI1NTE5"} {
		if _, err := parseAuthorizedKeyLine(line); err == nil {
			t.Errorf("expected an error for %q", line)
		}
	}
}

// scriptOutput builds a raw script output for the parser tests.
func scriptOutput() string {
	var b strings.Builder
	b.WriteString("===MODE:root===\n===OS===\nLinux\n")
	b.WriteString("===PASSWD===\n")
	b.WriteString("U|root|0|0|/root|/bin/bash\n")
	b.WriteString("U|alice|1000|1000|/home/alice|/bin/bash\n")
	b.WriteString("U|bob|1001|1001|/home/bob|/bin/bash\n")
	b.WriteString("U|svc|999|999|/var/lib/svc|/bin/bash\n")
	b.WriteString("U|nobody|65534|65534|/nonexistent|/usr/sbin/nologin\n")
	b.WriteString("U|backdoor|0|0|/home/backdoor|/bin/bash\n")
	b.WriteString("U|empty|1002|1002|/home/empty|/bin/bash\n")
	b.WriteString("===GROUP===\n")
	b.WriteString("G|sudo|27|alice\n")
	b.WriteString("G|docker|999|alice\n")
	b.WriteString("G|wheel|10|\n")
	b.WriteString("===AKFILE===\n")
	b.WriteString("AK|alice|/home/alice/.ssh/authorized_keys|readable\n")
	b.WriteString("---CONTENT---\n")
	b.WriteString("K|" + keyEd25519 + "\n")
	b.WriteString("K|garbage line here\n")
	b.WriteString("---ENDCONTENT---\n")
	b.WriteString("AK|bob|/home/bob/.ssh/authorized_keys|readable\n")
	b.WriteString("---CONTENT---\n")
	b.WriteString("K|" + keyRSA2048 + "\n")
	b.WriteString("---ENDCONTENT---\n")
	b.WriteString("AK|svc|/var/lib/svc/.ssh/authorized_keys|unreadable\n")
	b.WriteString("===SHADOW===\n")
	b.WriteString("SH|root|$6$hash$hash\n")
	b.WriteString("SH|alice|$6$hash$hash\n")
	b.WriteString("SH|bob|!\n")
	b.WriteString("SH|svc|*\n")
	b.WriteString("SH|backdoor|$6$hash$hash\n")
	b.WriteString("SH|empty|\n")
	b.WriteString("===SUDO===\n")
	b.WriteString("SD|/etc/sudoers|root ALL=(ALL:ALL) ALL\n")
	b.WriteString("SD|/etc/sudoers|alice ALL=(ALL) NOPASSWD: ALL\n")
	b.WriteString("SD|/etc/sudoers.d/dev|%wheel ALL=(ALL) ALL\n")
	b.WriteString("===LASTLOG===\n")
	b.WriteString("LL|alice|2026-01-02T03:04:05Z\n")
	b.WriteString("===SSHD===\n")
	b.WriteString("AKF|.ssh/authorized_keys .ssh/authorized_keys2\n")
	return b.String()
}

func TestParseAccountAuditOutput(t *testing.T) {
	library := map[string]string{fpEd25519: "Ops Key"}
	audit := parseAccountAuditOutput(scriptOutput(), 7, library)

	if audit.HostID != 7 {
		t.Errorf("host id = %d, want 7", audit.HostID)
	}
	if audit.Status == "error" {
		t.Fatalf("unexpected error status: %v", audit.Error)
	}

	// Users: only login-capable accounts.
	names := map[string]store.AccountUser{}
	for _, u := range audit.Users {
		names[u.Name] = u
	}
	for _, want := range []string{"root", "alice", "bob", "svc", "backdoor", "empty"} {
		if _, ok := names[want]; !ok {
			t.Errorf("user %q missing from the audit", want)
		}
	}
	if _, ok := names["nobody"]; ok {
		t.Errorf("nologin account should not be listed")
	}
	if g := names["alice"].Groups; len(g) != 2 || g[0] != "docker" || g[1] != "sudo" {
		t.Errorf("alice groups = %v, want [docker sudo]", g)
	}
	if names["alice"].SudoGrant != "NOPASSWD: ALL" {
		t.Errorf("alice sudo grant = %q, want NOPASSWD: ALL", names["alice"].SudoGrant)
	}
	if names["bob"].SudoGrant != "none" {
		t.Errorf("bob sudo grant = %q, want none", names["bob"].SudoGrant)
	}
	if names["alice"].PasswordState != "set" {
		t.Errorf("alice password state = %q, want set", names["alice"].PasswordState)
	}
	if names["bob"].PasswordState != "locked" {
		t.Errorf("bob password state = %q, want locked", names["bob"].PasswordState)
	}
	if names["empty"].PasswordState != "empty" {
		t.Errorf("empty password state = %q, want empty", names["empty"].PasswordState)
	}
	if names["svc"].PasswordState != "locked" {
		t.Errorf("svc password state = %q, want locked", names["svc"].PasswordState)
	}
	if names["svc"].KeyFilesUnread == nil || len(names["svc"].KeyFilesUnread) != 1 {
		t.Errorf("svc unreadable key files = %v, want one entry", names["svc"].KeyFilesUnread)
	}

	// Keys.
	var ed, rsa *store.AccountKey
	for i := range audit.Keys {
		k := &audit.Keys[i]
		switch k.Fingerprint {
		case fpEd25519:
			ed = k
		case fpRSA2048:
			rsa = k
		}
	}
	if ed == nil {
		t.Fatal("ed25519 key not parsed")
	}
	if ed.LibraryKey != "Ops Key" || ed.Unknown {
		t.Errorf("library cross-reference failed: library_key=%q unknown=%v", ed.LibraryKey, ed.Unknown)
	}
	if rsa == nil {
		t.Fatal("rsa2048 key not parsed")
	}
	if rsa.LibraryKey != "" || !rsa.Unknown {
		t.Errorf("rsa2048 should be unknown: library_key=%q unknown=%v", rsa.LibraryKey, rsa.Unknown)
	}
	if rsa.Bits != 2048 {
		t.Errorf("rsa2048 bits = %d, want 2048", rsa.Bits)
	}
	if audit.Summary.InvalidLines != 1 {
		t.Errorf("invalid lines = %d, want 1", audit.Summary.InvalidLines)
	}
	if audit.Summary.UnknownKeys != 1 {
		t.Errorf("unknown keys = %d, want 1", audit.Summary.UnknownKeys)
	}

	// Findings.
	found := map[string]store.AccountFinding{}
	for _, f := range audit.Findings {
		found[f.ID] = f
	}
	if f, ok := found["uid0_account"]; !ok || f.Severity != "critical" {
		t.Errorf("expected a critical uid0_account finding, got %v", found["uid0_account"])
	}
	if f, ok := found["empty_password"]; !ok || f.Severity != "critical" {
		t.Errorf("expected a critical empty_password finding, got %v", found["empty_password"])
	}
	if f, ok := found["sudo_nopasswd"]; !ok || f.Severity != "high" {
		t.Errorf("expected a high sudo_nopasswd finding, got %v", found["sudo_nopasswd"])
	}
	if f, ok := found["unknown_key"]; !ok || f.Severity != "medium" {
		t.Errorf("expected a medium unknown_key finding, got %v", found["unknown_key"])
	}
	if f, ok := found["service_account_login_shell"]; !ok || f.Severity != "low" {
		t.Errorf("expected a low service_account_login_shell finding, got %v", found["service_account_login_shell"])
	}
	if f, ok := found["key_without_from"]; !ok || f.Severity != "info" {
		t.Errorf("expected an info key_without_from finding, got %v", found["key_without_from"])
	}
	if audit.Summary.UID0Accounts != 1 {
		t.Errorf("uid0 accounts = %d, want 1", audit.Summary.UID0Accounts)
	}
}

func TestParseAccountAuditOutputUnreadableSections(t *testing.T) {
	out := "===MODE:none===\n===OS===\nLinux\n" +
		"===PASSWD===\nU|alice|1000|1000|/home/alice|/bin/bash\n" +
		"===GROUP===\n" +
		"===AKFILE===\nAK|alice|/home/alice/.ssh/authorized_keys|unreadable\n" +
		"===SHADOW===\nSHADOW_UNKNOWN\n" +
		"===SUDO===\nSUDO_UNKNOWN\n" +
		"===LASTLOG===\nLASTLOG_UNKNOWN\n" +
		"===SSHD===\nAKF|.ssh/authorized_keys\n"
	audit := parseAccountAuditOutput(out, 1, nil)
	if audit.Status != "partial" {
		t.Errorf("status = %q, want partial", audit.Status)
	}
	if len(audit.Notes) == 0 {
		t.Fatal("expected coverage notes for the unreadable sections")
	}
	joined := strings.Join(audit.Notes, " | ")
	for _, want := range []string{"/etc/shadow", "/etc/sudoers", "could not be read"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes %q do not mention %q", joined, want)
		}
	}
	if len(audit.Keys) != 1 || audit.Keys[0].Valid {
		t.Fatalf("expected one unreadable key file entry, got %+v", audit.Keys)
	}
	if audit.Keys[0].Error != "unreadable" {
		t.Errorf("key error = %q, want unreadable", audit.Keys[0].Error)
	}
	if audit.Users[0].PasswordState != "unknown" {
		t.Errorf("password state = %q, want unknown", audit.Users[0].PasswordState)
	}
	if audit.Users[0].SudoGrant != "unknown" {
		t.Errorf("sudo grant = %q, want unknown", audit.Users[0].SudoGrant)
	}
}

func TestParseAccountAuditOutputDarwin(t *testing.T) {
	audit := parseAccountAuditOutput("===MODE:none===\n===OS===\nDarwin\n===DARWIN===\n", 1, nil)
	if audit.Status != "unsupported" {
		t.Errorf("status = %q, want unsupported", audit.Status)
	}
	if len(audit.Notes) == 0 {
		t.Error("expected a note explaining the macOS limitation")
	}
}

func TestParseAccountAuditOutputEmpty(t *testing.T) {
	audit := parseAccountAuditOutput("", 1, nil)
	if audit.Status != "error" {
		t.Errorf("status = %q, want error", audit.Status)
	}
	if audit.Error == "" {
		t.Error("expected an error message")
	}
}

func TestParseSudoGrants(t *testing.T) {
	grants := parseSudoGrants([]string{
		"root ALL=(ALL:ALL) ALL",
		"alice ALL=(ALL) NOPASSWD: ALL",
		"%wheel ALL=(ALL) ALL",
		"Defaults env_reset",
	})
	if grants["alice"] != "NOPASSWD: ALL" {
		t.Errorf("alice grant = %q", grants["alice"])
	}
	if grants["%wheel"] != "ALL" {
		t.Errorf("%%wheel grant = %q", grants["%wheel"])
	}
	if _, ok := grants["root"]; !ok {
		t.Error("root grant missing")
	}
	if _, ok := grants["Defaults"]; ok {
		t.Error("Defaults line must not be treated as a grant")
	}
}
