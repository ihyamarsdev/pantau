package ansible

import "testing"

func TestParseFailedTasks(t *testing.T) {
	logFixture := `
PLAY [Deploy web] **************************************************************

TASK [Gathering Facts] *********************************************************
ok: [web-1]
ok: [db-1]

TASK [Install nginx] ***********************************************************
changed: [web-1]
` + "\x1b[0;31mfatal: [db-1]: UNREACHABLE! => {\"changed\": false, \"msg\": \"Failed to connect to the host via ssh: Connection timed out\", \"unreachable\": true}\x1b[0m" + `

TASK [Restart app] *************************************************************
` + "\x1b[0;31mfatal: [web-1]: FAILED! => {\"changed\": false, \"msg\": \"Unable to restart service app: unit not found\"}\x1b[0m" + `

PLAY RECAP *********************************************************************
web-1                      : ok=2    changed=1    unreachable=0    failed=1
db-1                       : ok=1    changed=0    unreachable=1    failed=0
`

	got := ParseFailedTasks(logFixture)
	if len(got) != 2 {
		t.Fatalf("expected 2 failed/unreachable tasks, got %d: %+v", len(got), got)
	}
	if got[0].Play != "Deploy web" || got[0].Task != "Install nginx" || got[0].Host != "db-1" || got[0].Status != "unreachable" {
		t.Fatalf("unexpected first task: %+v", got[0])
	}
	if got[1].Task != "Restart app" || got[1].Host != "web-1" || got[1].Status != "failed" {
		t.Fatalf("unexpected second task: %+v", got[1])
	}
}
