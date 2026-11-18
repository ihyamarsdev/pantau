package vault_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"pantau/internal/vault"
)

func TestRedactorSetValuesAndRedact(t *testing.T) {
	r := vault.NewRedactor()
	r.SetValues([]string{"secretA", "", "secretB", "secretA"})
	got := r.Redact("a=secretA b=secretB c=none")
	want := "a=[REDACTED] b=[REDACTED] c=none"
	if got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestRedactorSetValuesReplaces(t *testing.T) {
	r := vault.NewRedactor()
	r.SetValues([]string{"old"})
	r.SetValues([]string{"new"})
	if got := r.Redact("old new"); got != "old [REDACTED]" {
		t.Fatalf("SetValues did not replace old values: %q", got)
	}
}

func TestRedactorLongestFirst(t *testing.T) {
	// The shorter secret "token" is a substring of the longer one. If the
	// shorter value were replaced first, the longer secret would leak as
	// "[REDACTED]abc".
	r := vault.NewRedactor()
	r.SetValues([]string{"token", "token-abcdef"})
	got := r.Redact("value=token-abcdef")
	if got != "value=[REDACTED]" {
		t.Fatalf("Redact = %q, want fully redacted value", got)
	}
}

func TestRedactorMarkerNotCorrupted(t *testing.T) {
	// A secret that is a substring of "[REDACTED]" must not mutate an
	// already-inserted marker when a naive sequential ReplaceAll is used.
	r := vault.NewRedactor()
	r.SetValues([]string{"RED", "password"})
	got := r.Redact("password=secret")
	if got != "[REDACTED]=secret" {
		t.Fatalf("Redact = %q, want marker preserved", got)
	}
}

func TestRedactorAddValueAndClear(t *testing.T) {
	r := vault.NewRedactor()
	r.AddValue("one")
	r.AddValue("two")
	r.AddValue("one") // duplicate
	r.AddValue("")    // ignored
	if got := r.Redact("one two"); got != "[REDACTED] [REDACTED]" {
		t.Fatalf("AddValue: got %q", got)
	}
	r.Clear()
	if got := r.Redact("one two"); got != "one two" {
		t.Fatalf("Clear: got %q", got)
	}
}

func TestRedactorEmpty(t *testing.T) {
	r := vault.NewRedactor()
	if got := r.Redact("anything"); got != "anything" {
		t.Fatalf("empty redactor changed input: %q", got)
	}
}

func TestRedactorWriter(t *testing.T) {
	r := vault.NewRedactor()
	r.SetValues([]string{"password"})
	var buf bytes.Buffer
	w := r.Writer(&buf)
	input := "user=admin password=hunter2"
	n, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(input) {
		t.Fatalf("Write returned %d, want len(p)=%d", n, len(input))
	}
	if got := buf.String(); got != "user=admin [REDACTED]=hunter2" {
		t.Fatalf("writer output = %q", got)
	}
}

func TestRedactorWriterShortWrite(t *testing.T) {
	r := vault.NewRedactor()
	w := r.Writer(&shortWriter{})
	_, err := w.Write([]byte("some data"))
	if err == nil {
		t.Fatal("expected short-write error")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return 0, nil }

func TestRedactorConcurrent(t *testing.T) {
	r := vault.NewRedactor()
	r.SetValues([]string{"secret"})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				r.AddValue(strings.Repeat("x", i+1))
			case 1:
				r.SetValues([]string{"secret", "other"})
			case 2:
				_ = r.Redact("a secret and other")
			}
		}(i)
	}
	wg.Wait()
	// After the dust settles, the original value must still be redacted (the
	// last SetValues in some goroutine always includes it, and AddValue only
	// appends). Use a fresh deterministic check instead of relying on order.
	r.SetValues([]string{"secret"})
	if got := r.Redact("secret"); got != "[REDACTED]" {
		t.Fatalf("post-concurrency redaction = %q", got)
	}
}

func TestInjectEnv(t *testing.T) {
	base := map[string]string{"PATH": "/bin", "KEEP": "yes"}
	secrets := map[string]string{"db_password": "s3cr3t", "API_TOKEN": "tok"}

	out := vault.InjectEnv(base, secrets, "PFX_")

	// base must not be mutated.
	if len(base) != 2 || base["PATH"] != "/bin" || base["KEEP"] != "yes" {
		t.Fatalf("base map was mutated: %#v", base)
	}
	// secrets must not be mutated either.
	if len(secrets) != 2 {
		t.Fatalf("secrets map was mutated: %#v", secrets)
	}

	want := map[string]string{
		"PATH":            "/bin",
		"KEEP":            "yes",
		"PFX_DB_PASSWORD": "s3cr3t",
		"PFX_API_TOKEN":   "tok",
	}
	if len(out) != len(want) {
		t.Fatalf("result length = %d, want %d: %#v", len(out), len(want), out)
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("out[%q] = %q, want %q", k, out[k], v)
		}
	}

	// Empty prefix.
	out2 := vault.InjectEnv(nil, map[string]string{"mixed_Case": "v"}, "")
	if out2["MIXED_CASE"] != "v" {
		t.Fatalf("empty-prefix injection = %#v", out2)
	}

	// Result is a new map even when secrets is empty.
	empty := vault.InjectEnv(base, nil, "")
	empty["NEW"] = "x"
	if _, ok := base["NEW"]; ok {
		t.Fatal("result aliases base map")
	}
}
