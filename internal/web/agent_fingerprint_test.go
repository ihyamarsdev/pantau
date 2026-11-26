package web

import (
	"strings"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

func TestAgentFingerprintPrompt_InjectsJSONAndSyntaxRules(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	if srv.inspector == nil {
		srv.inspector = inspector.New(db, nil, nil)
	}

	alpineProf := store.OSCapabilityProfile{
		OSFamily:        "linux",
		Distro:          "alpine",
		InitSystem:      "openrc",
		PkgManager:      "apk",
		ShellFlavor:     "busybox",
		HasSudoNopasswd: true,
	}
	alpineID, err := db.CreateHost(&store.Host{
		Name: "edge-alpine",
		Host: "10.0.0.10",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatalf("CreateHost alpine: %v", err)
	}
	h, err := db.GetHost(alpineID)
	if err != nil || h == nil {
		t.Fatalf("GetHost alpine: %v", err)
	}
	h.OSInfo = "Alpine Linux v3.20"
	h.OSProfile = inspector.MarshalOSCapabilityProfile(alpineProf)
	if err := db.UpdateHostInspection(h); err != nil {
		t.Fatalf("UpdateHostInspection alpine: %v", err)
	}

	macProf := store.OSCapabilityProfile{
		OSFamily:        "darwin",
		Distro:          "macos",
		InitSystem:      "launchd",
		PkgManager:      "brew",
		ShellFlavor:     "bsd",
		HasSudoNopasswd: false,
	}
	macID, err := db.CreateHost(&store.Host{
		Name: "mac-studio",
		Host: "10.0.0.20",
		Port: 22,
		User: "admin",
	})
	if err != nil {
		t.Fatalf("CreateHost mac: %v", err)
	}
	mh, err := db.GetHost(macID)
	if err != nil || mh == nil {
		t.Fatalf("GetHost mac: %v", err)
	}
	mh.OSInfo = "macOS 26.2 (Tahoe)"
	mh.Kernel = "25.2.0"
	mh.OSProfile = inspector.MarshalOSCapabilityProfile(macProf)
	if err := db.UpdateHostInspection(mh); err != nil {
		t.Fatalf("UpdateHostInspection mac: %v", err)
	}

	t.Run("host named in question injects JSON profile and rules", func(t *testing.T) {
		got := srv.agentFingerprintPrompt("check nginx on edge-alpine")
		wantSubstrs := []string{
			"<host_os_capability_profile>",
			"</host_os_capability_profile>",
			`"os_family":"linux"`,
			`"distro":"alpine"`,
			`"init_system":"openrc"`,
			`"pkg_manager":"apk"`,
			`"shell_flavor":"busybox"`,
			`"has_sudo_nopasswd":true`,
			"NEVER use `systemctl` on `openrc`",
			"stat -c",
			"stat -f",
		}
		for _, w := range wantSubstrs {
			if !strings.Contains(got, w) {
				t.Errorf("prompt missing %q\nGot:\n%s", w, got)
			}
		}
	})

	t.Run("thread-scoped host injects profile even when message omits host name", func(t *testing.T) {
		th, err := db.CreateAgentThread("host", macID, "sess-1", "mac check")
		if err != nil {
			t.Fatalf("CreateAgentThread: %v", err)
		}
		got := srv.agentFingerprintPrompt("check disk usage", th.ID)
		wantSubstrs := []string{
			"<host_os_capability_profile>",
			`"os_family":"darwin"`,
			`"distro":"macos"`,
			`"init_system":"launchd"`,
			`"pkg_manager":"brew"`,
			`"shell_flavor":"bsd"`,
			`"has_sudo_nopasswd":false`,
		}
		for _, w := range wantSubstrs {
			if !strings.Contains(got, w) {
				t.Errorf("thread-scoped prompt missing %q\nGot:\n%s", w, got)
			}
		}
	})

	t.Run("global question without host returns empty", func(t *testing.T) {
		if got := srv.agentFingerprintPrompt("list all alerts"); got != "" {
			t.Errorf("expected empty prompt for fleet-wide question, got %q", got)
		}
	})
}
