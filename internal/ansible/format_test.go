package ansible

import (
	"strings"
	"testing"
)

func TestFormatPlaybook(t *testing.T) {
	play := "- name: app   # the play\n    hosts: web\n  tasks:\n    - name: hi   # keep this comment\n      debug: msg=hi\n"
	if _, err := FormatPlaybook(play); err == nil {
		t.Fatal("bad indent accepted")
	}

	ok := "- name: app # the play\n  hosts: web\n  tasks:\n    - name: hi # keep this comment\n      debug: msg=hi\n"
	out, err := FormatPlaybook(ok)
	if err != nil {
		t.Fatal(err)
	}
	want := ok // 2-space input is already formatted
	if out != want {
		t.Fatalf("round trip changed the playbook:\n%s", out)
	}
	if !strings.Contains(out, "# keep this comment") || !strings.Contains(out, "# the play") {
		t.Fatalf("comment lost:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Fatalf("trailing newline wrong: %q", out)
	}

	four := "-   name: app\n    hosts: web\n    vars:\n        a: 1\n"
	out, err = FormatPlaybook(four)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "  hosts: web") || !strings.Contains(out, "    a: 1") {
		t.Fatalf("not re-indented to 2 spaces:\n%s", out)
	}
	again, err := FormatPlaybook(out)
	if err != nil || again != out {
		t.Fatalf("not idempotent:\n%s", again)
	}

	marker := "---\n" + ok
	withMarker, err := FormatPlaybook(marker)
	if err != nil || !strings.HasPrefix(withMarker, "---\n") {
		t.Fatalf("--- marker lost: %q", withMarker)
	}
	if again, _ := FormatPlaybook(withMarker); again != withMarker {
		t.Fatalf("marker form not idempotent:\n%s", again)
	}
	if noMarker, err := FormatPlaybook(ok); err != nil || strings.HasPrefix(noMarker, "---") {
		t.Fatalf("--- marker added: %q", noMarker)
	}
}

func TestFormatPlaybookRejects(t *testing.T) {
	cases := map[string]string{
		"not a list":    "hosts: web\n",
		"empty":         "",
		"whitespace":    "  \n",
		"item not map":  "- hosts: web\n- task\n",
		"no hosts":      "- name: p\n  tasks: []\n",
		"broken yaml":   "- hosts: web\n tasks: [unclosed\n",
		"duplicate key": "- hosts: web\n  hosts: all\n",
	}
	for name, in := range cases {
		_, err := FormatPlaybook(in)
		if err == nil {
			t.Errorf("%s accepted: %q", name, in)
		}
	}
	if _, err := FormatPlaybook("- hosts: web\n- name: p\n"); err == nil || !strings.Contains(err.Error(), "play 2") {
		t.Errorf("item-not-mapping must name the play: %v", err)
	}
	if _, err := FormatPlaybook("- name: p\n  vars:\n"); err == nil || !strings.Contains(err.Error(), "play 1") {
		t.Errorf("no hosts must name the play: %v", err)
	}
	if _, err := FormatPlaybook("- hosts: web\n  hosts: all\n"); err == nil || !strings.Contains(err.Error(), "duplicate key 'hosts' at line 2") {
		t.Errorf("duplicate key must have line number: %v", err)
	}
	if _, err := FormatPlaybook("- hosts: web\n tasks: [unclosed\n"); err == nil || !strings.Contains(err.Error(), "line") {
		t.Errorf("broken yaml must mention a line: %v", err)
	}
}
