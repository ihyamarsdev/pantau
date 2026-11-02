package inspector

import (
	"strings"
	"testing"
)

func TestBuildUpgradeCommand(t *testing.T) {
	tests := []struct {
		name        string
		manager     string
		mode        string
		wantErr     bool
		errContains string
		wantCmd     string
	}{
		{
			name:    "apt mode all",
			manager: "apt",
			mode:    "all",
			wantErr: false,
			wantCmd: "DEBIAN_FRONTEND=noninteractive apt-get update && apt-get -y -o Dpkg::Options::=--force-confold upgrade",
		},
		{
			name:        "apt mode security rejected",
			manager:     "apt",
			mode:        "security",
			wantErr:     true,
			errContains: "apt does not have a reliable security-only upgrade mode",
		},
		{
			name:        "apt invalid mode",
			manager:     "apt",
			mode:        "foo",
			wantErr:     true,
			errContains: "apt does not have a reliable security-only upgrade mode",
		},
		{
			name:    "dnf mode all",
			manager: "dnf",
			mode:    "all",
			wantErr: false,
			wantCmd: "dnf -y upgrade",
		},
		{
			name:    "dnf mode security",
			manager: "dnf",
			mode:    "security",
			wantErr: false,
			wantCmd: "dnf -y upgrade --security",
		},
		{
			name:        "dnf invalid mode",
			manager:     "dnf",
			mode:        "foo",
			wantErr:     true,
			errContains: "invalid mode",
		},
		{
			name:    "yum mode all",
			manager: "yum",
			mode:    "all",
			wantErr: false,
			wantCmd: "yum -y upgrade",
		},
		{
			name:    "yum mode security",
			manager: "yum",
			mode:    "security",
			wantErr: false,
			wantCmd: "yum -y upgrade --security",
		},
		{
			name:        "yum invalid mode",
			manager:     "yum",
			mode:        "other",
			wantErr:     true,
			errContains: "invalid mode",
		},
		{
			name:        "unsupported manager",
			manager:     "pacman",
			mode:        "all",
			wantErr:     true,
			errContains: "unsupported package manager",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := BuildUpgradeCommand(tc.manager, tc.mode)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(cmd, tc.wantCmd) {
				t.Errorf("expected command to contain %q, got:\n%s", tc.wantCmd, cmd)
			}
			if !strings.Contains(cmd, "timeout 900") {
				t.Errorf("expected command to include timeout 900 wrapper")
			}
			if !strings.Contains(cmd, "sudo -n") {
				t.Errorf("expected command to include sudo -n privilege fallback")
			}
			if !strings.Contains(cmd, "[pantau] exit=") {
				t.Errorf("expected command to include exit marker")
			}
		})
	}
}

func TestParseUpgradePreview(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		res := ParseUpgradePreview("")
		if res.OK || res.Supported {
			t.Errorf("expected OK=false, Supported=false")
		}
	})

	t.Run("macos output", func(t *testing.T) {
		out := "===OS_RELEASE===\nNAME=\"macOS\"\nVERSION_ID=\"14.0\"\n===REBOOT===\n===UPDATES===\nMGR:softwareupdate\n"
		res := ParseUpgradePreview(out)
		if !res.OK || res.Supported {
			t.Errorf("expected OK=true, Supported=false for macOS, got %+v", res)
		}
		if res.Manager != "macos" {
			t.Errorf("expected Manager=macos, got %s", res.Manager)
		}
	})

	t.Run("apt preview with security and regular updates", func(t *testing.T) {
		out := `===OS_RELEASE===
NAME="Ubuntu"
ID=ubuntu
VERSION_ID="22.04"
===REBOOT===
REBOOT_REQUIRED
===UPDATES===
MGR:apt
Reading package lists...
Inst bash [5.0-6ubuntu1.1] (5.0-6ubuntu1.2 Ubuntu:22.04/jammy-security [amd64])
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
`
		res := ParseUpgradePreview(out)
		if !res.OK || !res.Supported {
			t.Fatalf("expected OK=true, Supported=true, got %+v", res)
		}
		if res.Manager != "apt" {
			t.Errorf("expected manager apt, got %s", res.Manager)
		}
		if res.PendingTotal != 2 {
			t.Errorf("expected pending_total=2, got %d", res.PendingTotal)
		}
		if res.PendingSecurity != 1 {
			t.Errorf("expected pending_security=1, got %d", res.PendingSecurity)
		}
		if !res.RebootRequired {
			t.Errorf("expected reboot_required=true")
		}
		if len(res.Packages) != 2 {
			t.Fatalf("expected 2 packages, got %d", len(res.Packages))
		}
		if res.Packages[0].Name != "bash" || !res.Packages[0].Security || res.Packages[0].From != "5.0-6ubuntu1.1" || res.Packages[0].To != "5.0-6ubuntu1.2" {
			t.Errorf("unexpected package 0: %+v", res.Packages[0])
		}
		if res.Packages[1].Name != "curl" || res.Packages[1].Security || res.Packages[1].From != "7.81.0-1ubuntu1.14" || res.Packages[1].To != "7.81.0-1ubuntu1.15" {
			t.Errorf("unexpected package 1: %+v", res.Packages[1])
		}
	})

	t.Run("dnf preview with security and check-update", func(t *testing.T) {
		out := `===OS_RELEASE===
NAME="Fedora Linux"
ID=fedora
===REBOOT===
NEEDS_RESTARTING:1
===UPDATES===
MGR:dnf
---SEC---
RHSA-2023:1234 Important/Sec. openssl-1:1.1.1k-9.el8_7.x86_64
---TOTAL---
openssl.x86_64           1:1.1.1k-9.el8_7          baseos
curl.x86_64              7.76.1-26.fc38            baseos
---RPM_QA---
openssl	1:1.1.1k-7.el8
curl	7.76.1-20.fc38
`
		res := ParseUpgradePreview(out)
		if !res.OK || !res.Supported {
			t.Fatalf("expected OK=true, Supported=true, got %+v", res)
		}
		if res.Manager != "dnf" {
			t.Errorf("expected manager dnf, got %s", res.Manager)
		}
		if res.PendingTotal != 2 {
			t.Errorf("expected pending_total=2, got %d", res.PendingTotal)
		}
		if res.PendingSecurity != 1 {
			t.Errorf("expected pending_security=1, got %d", res.PendingSecurity)
		}
		if !res.RebootRequired {
			t.Errorf("expected reboot_required=true")
		}
		if len(res.Packages) != 2 {
			t.Fatalf("expected 2 packages, got %d", len(res.Packages))
		}
		if res.Packages[0].Name != "openssl" || !res.Packages[0].Security {
			t.Errorf("expected openssl security package, got %+v", res.Packages[0])
		}
		if res.Packages[1].Name != "curl" || res.Packages[1].Security {
			t.Errorf("expected curl non-security package, got %+v", res.Packages[1])
		}
	})

	t.Run("unknown package manager", func(t *testing.T) {
		out := `===OS_RELEASE===
NAME="Arch Linux"
ID=arch
===REBOOT===
===UPDATES===
MGR:pacman
`
		res := ParseUpgradePreview(out)
		if !res.OK || res.Supported {
			t.Errorf("expected OK=true, Supported=false for pacman, got %+v", res)
		}
		if res.Manager != "pacman" {
			t.Errorf("expected manager pacman, got %s", res.Manager)
		}
	})
}

func TestTryLockUpgrade(t *testing.T) {
	unlock1, err := TryLockUpgrade(99)
	if err != nil {
		t.Fatalf("expected lock success, got: %v", err)
	}

	_, err2 := TryLockUpgrade(99)
	if err2 == nil {
		t.Fatalf("expected lock collision, got nil error")
	}

	unlock1()

	unlock2, err3 := TryLockUpgrade(99)
	if err3 != nil {
		t.Fatalf("expected lock success after unlock, got: %v", err3)
	}
	unlock2()
}

// dnf prints "Error: ..." (repo metadata / GPG failure) instead of a package list. Reporting that as
// "0 pending" would tell the operator the host is up to date when dnf could not even check.
func TestParseUpgradePreview_DnfErrorIsNotZeroPending(t *testing.T) {
	out := "===OS_RELEASE===\nID=\"almalinux\"\n===REBOOT===\n===UPDATES===\nMGR:dnf\n---SEC---\n---TOTAL---\n" +
		"Importing GPG key 0xF748182B:\nIs this ok [y/N]: Error: Failed to download metadata for repo 'x': GPG signature verification error\n---RPM_QA---\n"
	p := ParseUpgradePreview(out)
	if p.OK || !strings.Contains(p.Note, "Error: Failed to download metadata") {
		t.Fatalf("dnf error must surface as ok=false with the message, got ok=%v note=%q pending=%d", p.OK, p.Note, p.PendingTotal)
	}
}

// One package listed under several advisories must count once; the old advisory-line count told the operator
// "545 security / 545 total" for a host with 148 security packages out of 283 pending.
func TestParseUpgradePreview_DnfCountsDistinctPackages(t *testing.T) {
	out := "===OS_RELEASE===\nID=\"almalinux\"\n===REBOOT===\n===UPDATES===\nMGR:dnf\n---SEC---\n" +
		"ALSA-2026:1 Important/Sec. openssl-1:3.2.2-1.el10.x86_64\n" +
		"ALSA-2026:2 Moderate/Sec. openssl-1:3.2.2-1.el10.x86_64\n" +
		"ALSA-2026:3 Low/Sec. python3.12-3.12.1-2.el10.x86_64\n" +
		"---TOTAL---\n\nopenssl.x86_64   1:3.2.2-1.el10   baseos\npython3.12.x86_64   3.12.1-2.el10   appstream\nbash.x86_64   5.2-3.el10   baseos\n" +
		"---RPM_QA---\nopenssl\t3.0-1\npython3.12\t3.12.0-1\nbash\t5.1-1\n"
	p := ParseUpgradePreview(out)
	if !p.OK || p.PendingSecurity != 2 || p.PendingTotal != 3 {
		t.Fatalf("want 2 security / 3 total distinct packages, got ok=%v sec=%d total=%d", p.OK, p.PendingSecurity, p.PendingTotal)
	}
	names := map[string]bool{}
	for _, pk := range p.Packages {
		names[pk.Name] = pk.Security
	}
	if !names["python3.12"] || names["bash"] {
		t.Errorf("dotted name must survive and bash must be non-security: %v", names)
	}
}
