package vault_test

import (
	"strings"
	"testing"

	"pantau/internal/vault"
)

func TestRender(t *testing.T) {
	values := map[string]string{
		"DB_PASSWORD": "hunter2",
		"API_TOKEN":   "tok-abc",
		"EMPTY":       "",
	}
	cases := []struct {
		name     string
		template string
		want     string
	}{
		{"simple", "password={{ vault.DB_PASSWORD }}", "password=hunter2"},
		{"whitespace", "a={{vault.API_TOKEN}} b={{  vault.DB_PASSWORD   }}", "a=tok-abc b=hunter2"},
		{"repeated", "{{ vault.DB_PASSWORD }}/{{ vault.DB_PASSWORD }}", "hunter2/hunter2"},
		{"empty value", "x={{ vault.EMPTY }}y", "x=y"},
		{"no placeholders", "plain text with {{ vaultx }} and { vault.DB_PASSWORD }", "plain text with {{ vaultx }} and { vault.DB_PASSWORD }"},
		{"mixed", "{{ vault.API_TOKEN }}:{{ vault.DB_PASSWORD }}", "tok-abc:hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := vault.Render(tc.template, values)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Render = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderUnknownKey(t *testing.T) {
	_, err := vault.Render("x={{ vault.MISSING }}", map[string]string{"OTHER": "v"})
	if err == nil {
		t.Fatal("Render with unknown key succeeded, want error")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("error %q does not name the unknown key", err)
	}
}

func TestRenderDoesNotMutateValues(t *testing.T) {
	values := map[string]string{"A": "1"}
	_, err := vault.Render("{{ vault.A }} {{ vault.B }}", values)
	if err == nil {
		t.Fatal("expected unknown-key error")
	}
	if len(values) != 1 || values["A"] != "1" {
		t.Fatalf("Render mutated values map: %#v", values)
	}
}

func TestPlaceholders(t *testing.T) {
	cases := []struct {
		template string
		want     []string
	}{
		{"", nil},
		{"no placeholders", nil},
		{"{{ vault.A }}", []string{"A"}},
		{"{{ vault.B }} then {{ vault.A }} then {{ vault.B }}", []string{"B", "A"}},
		{"{{vault.A}}{{ vault.A }} {{ vault.A_1 }}", []string{"A", "A_1"}},
		{"{{ vault.MIXED_123 }}", []string{"MIXED_123"}},
		// Invalid keys must not be captured.
		{"{{ vault.1BAD }} {{ vault.foo-bar }} {{ vault.foo.bar }}", nil},
	}
	for _, tc := range cases {
		got := vault.Placeholders(tc.template)
		if len(got) != len(tc.want) {
			t.Fatalf("Placeholders(%q) = %v, want %v", tc.template, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("Placeholders(%q) = %v, want %v", tc.template, got, tc.want)
			}
		}
	}
}

func TestValidateTemplate(t *testing.T) {
	template := "line1 {{ vault.KNOWN }}\n" +
		"line2 {{ vault.UNKNOWN_1 }} and {{ vault.UNKNOWN_1 }}\n" +
		"line3 {{vault.UNKNOWN_2}}\n"
	known := map[string]bool{"KNOWN": true}
	issues := vault.ValidateTemplate(template, known)
	if len(issues) != 3 {
		t.Fatalf("got %d issues, want 3: %#v", len(issues), issues)
	}
	want := []vault.TemplateIssue{
		{Line: 2, Key: "UNKNOWN_1"},
		{Line: 2, Key: "UNKNOWN_1"},
		{Line: 3, Key: "UNKNOWN_2"},
	}
	for i, w := range want {
		if issues[i].Line != w.Line || issues[i].Key != w.Key {
			t.Errorf("issue %d = %#v, want line=%d key=%s", i, issues[i], w.Line, w.Key)
		}
		if !strings.Contains(issues[i].Message, w.Key) {
			t.Errorf("issue %d message %q does not name key", i, issues[i].Message)
		}
	}
}

func TestValidateTemplateClean(t *testing.T) {
	known := map[string]bool{"A": true, "B": true}
	if got := vault.ValidateTemplate("{{ vault.A }}\n{{vault.B}}", known); len(got) != 0 {
		t.Fatalf("expected no issues, got %#v", got)
	}
	if got := vault.ValidateTemplate("plain", nil); len(got) != 0 {
		t.Fatalf("expected no issues for plain text, got %#v", got)
	}
}
