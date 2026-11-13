package inspector

import "testing"

func TestParseSystemdServicesMergesEnabledStateAndOrdersProblemsFirst(t *testing.T) {
	out := `===UNITS===
ssh.service                       loaded active   running OpenBSD Secure Shell server
● backup.service                  loaded failed   failed  Nightly backup
cron.service                      loaded active   running Regular background program processing daemon
getty@tty1.service                loaded active   running Getty on tty1
apt-daily.service                 loaded inactive dead    Daily apt download activities
docker.service                    loaded activating start Docker Application Container Engine
ghost.service                     not-found inactive dead ghost.service
===FILES===
ssh.service                       enabled         enabled
backup.service                    disabled        enabled
cron.service                      enabled         enabled
getty@.service                    enabled         enabled
apt-daily.service                 static          -
docker.service                    enabled         enabled
aicare-api.service                disabled        enabled
aicare-old.service                masked          enabled
dbus-org.freedesktop.login1.service alias         -
`
	services, supported := parseSystemdServices(out)
	if !supported {
		t.Fatal("expected supported=true")
	}
	// The two aicare unit files are not loaded, so only list-unit-files knows them; the template and the alias stay out.
	if len(services) != 9 {
		t.Fatalf("expected 9 services, got %d: %+v", len(services), services)
	}
	wantOrder := []string{"backup.service", "docker.service", "cron.service", "getty@tty1.service", "ssh.service", "aicare-api.service", "aicare-old.service", "apt-daily.service", "ghost.service"}
	for i, name := range wantOrder {
		if services[i].Name != name {
			t.Fatalf("position %d: expected %s, got %s (%+v)", i, name, services[i].Name, services)
		}
	}
	byName := map[string]SystemdService{}
	for _, s := range services {
		byName[s.Name] = s
	}
	if s := byName["backup.service"]; s.Active != "failed" || s.Enabled != "disabled" || s.Description != "Nightly backup" {
		t.Errorf("backup.service parsed wrong: %+v", s)
	}
	if s := byName["getty@tty1.service"]; s.Enabled != "enabled" {
		t.Errorf("template instance must inherit the enabled state of getty@.service, got %q", s.Enabled)
	}
	if s := byName["apt-daily.service"]; s.Enabled != "static" {
		t.Errorf("apt-daily.service enabled state = %q, want static", s.Enabled)
	}
	if s := byName["aicare-api.service"]; s.Load != "unloaded" || s.Active != "inactive" || s.Enabled != "disabled" {
		t.Errorf("a unit file that is not loaded must be listed as inactive: %+v", s)
	}
	if s := byName["aicare-old.service"]; s.Load != "masked" {
		t.Errorf("a masked unit file must not be offered for start: %+v", s)
	}
	if s := byName["ghost.service"]; s.Load != "not-found" || s.Enabled != "" {
		t.Errorf("ghost.service parsed wrong: %+v", s)
	}
}

func TestParseSystemdServicesUnsupportedHost(t *testing.T) {
	services, supported := parseSystemdServices("===UNSUPPORTED===\n")
	if supported {
		t.Error("expected supported=false when systemd is not the init system")
	}
	if services == nil || len(services) != 0 {
		t.Errorf("expected an empty, non-nil list, got %#v", services)
	}
}

func TestParseListenOwnerOutput(t *testing.T) {
	sudoOut := "===SUDO===\n" +
		`LISTEN 0.0.0.0:22 users:(("sshd",pid=1,fd=3))` + "\n" +
		`LISTEN [::]:22 users:(("sshd",pid=1,fd=4))` + "\n" +
		`LISTEN *:5432 users:(("postgres",pid=9,fd=5),("postgres",pid=10,fd=5))` + "\n" +
		"tcp :::3306 LISTEN 77/mysqld\n"
	res := parseListenOwnerOutput(sudoOut)
	if !res.ok || !res.sudoUsed {
		t.Fatalf("expected ok+sudoUsed, got %+v", res)
	}
	want := map[string]string{"22": "sshd", "5432": "postgres", "3306": "mysqld"}
	for port, name := range want {
		if res.owners[port] != name {
			t.Errorf("port %s: expected %q, got %q (%v)", port, name, res.owners[port], res.owners)
		}
	}

	if res := parseListenOwnerOutput("===NOPRIV===\n"); res.ok || !res.noPriv || len(res.owners) != 0 {
		t.Errorf("NOPRIV must report no owners and noPriv=true, got %+v", res)
	}
	if res := parseListenOwnerOutput("===ROOT===\n"); res.ok || res.noPriv {
		t.Errorf("ROOT must be a no-op, got %+v", res)
	}

	darwin := parseListenOwnerOutput("===SUDOFAIL===\n===OK===\ntcp *:22 launchd\ntcp 127.0.0.1:631 cupsd\n")
	if !darwin.ok || !darwin.sudoFailed || darwin.owners["22"] != "launchd" || darwin.owners["631"] != "cupsd" {
		t.Errorf("lsof output parsed wrong: %+v", darwin)
	}
}
