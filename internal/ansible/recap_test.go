package ansible

import "testing"

// Real `ansible-playbook` output (ansible-core 2.21, local connection, 2 plays, --limit, ANSIBLE_FORCE_COLOR=1).
const realColoredRecapLog = "\x1b[0;33mchanged: [localhost]\x1b[0m\n\n" +
	"PLAY RECAP *********************************************************************\n" +
	"\x1b[0;33mlocalhost\x1b[0m                  : \x1b[0;32mok=3   \x1b[0m \x1b[0;33mchanged=1   \x1b[0m unreachable=0    failed=0    \x1b[0;36mskipped=1   \x1b[0m rescued=0    \x1b[1;35mignored=1   \x1b[0m\n"

// Real multi-host output: one reachable host, one unreachable.
const realMultiHostRecapLog = "ok: [web-1]\n\n" +
	"PLAY RECAP *********************************************************************\n" +
	"web-1                      : ok=1    changed=0    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0   \n" +
	"web-2                      : ok=0    changed=0    unreachable=1    failed=0    skipped=0    rescued=0    ignored=0   \n"

func TestParseRecapRealColored(t *testing.T) {
	got := ParseRecap(realColoredRecapLog)
	want := RecapHost{OK: 3, Changed: 1, Skipped: 1, Ignored: 1}
	if len(got) != 1 || got["localhost"] != want {
		t.Fatalf("ParseRecap = %+v, want localhost=%+v", got, want)
	}
}

func TestParseRecapRealMultiHost(t *testing.T) {
	got := ParseRecap(realMultiHostRecapLog)
	if len(got) != 2 {
		t.Fatalf("got %d hosts, want 2: %+v", len(got), got)
	}
	if got["web-1"] != (RecapHost{OK: 1}) {
		t.Errorf("web-1 = %+v", got["web-1"])
	}
	if got["web-2"] != (RecapHost{Unreachable: 1}) {
		t.Errorf("web-2 = %+v", got["web-2"])
	}
}

func TestParseRecapEmptyAndUnreachable(t *testing.T) {
	if got := ParseRecap("TASK [x] ***\nok: [localhost]\n"); len(got) != 0 {
		t.Fatalf("no PLAY RECAP should be empty, got %+v", got)
	}
	// `--limit` that matches nothing prints the header with no hosts.
	s := "skipping: no hosts matched\n\nPLAY RECAP *********************************************************************\n\nWall time: 2.92 seconds\n"
	if got := ParseRecap(s); len(got) != 0 {
		t.Fatalf("empty recap block should be empty, got %+v", got)
	}
}

func TestParseRecapRescued(t *testing.T) {
	s := "PLAY RECAP *********************************************************************\n" +
		"localhost                  : ok=1    changed=0    unreachable=0    failed=0    skipped=0    rescued=1    ignored=0   \n"
	if got := ParseRecap(s)["localhost"]; got != (RecapHost{OK: 1, Rescued: 1}) {
		t.Fatalf("got %+v", got)
	}
}

func TestRecapTotals(t *testing.T) {
	ok, changed, unreachable, failed, skipped := RecapTotals(ParseRecap(realMultiHostRecapLog))
	if ok != 1 || changed != 0 || unreachable != 1 || failed != 0 || skipped != 0 {
		t.Fatalf("totals = %d,%d,%d,%d,%d", ok, changed, unreachable, failed, skipped)
	}
}

func TestValidatePattern(t *testing.T) {
	for _, good := range []string{"", "web", "web-1,web-2", "all:!db", "g1:&g2", "web[0-9]*"} {
		if err := ValidatePattern(good); err != nil {
			t.Errorf("ValidatePattern(%q) = %v", good, err)
		}
	}
	for _, bad := range []string{"web 1", "web;rm", "$(id)", "a`b`", "web\n", "x.y/z"} {
		if err := ValidatePattern(bad); err == nil {
			t.Errorf("ValidatePattern(%q) accepted", bad)
		}
	}
}
