package inspector

import (
	"encoding/json"
	"testing"

	"pantau/internal/store"
)

func TestParseOSCapabilityProfile(t *testing.T) {
	tests := []struct {
		name         string
		osReleaseSec string
		kernelSec    string
		capSec       string
		want         store.OSCapabilityProfile
	}{
		{
			name:         "Ubuntu 24.04 LTS systemd apt gnu sudo",
			osReleaseSec: "NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\nID=ubuntu\nID_LIKE=debian",
			kernelSec:    "6.8.0-45-generic",
			capSec:       "init=systemd pkg=apt shell=gnu sudo=1",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "ubuntu",
				InitSystem:      "systemd",
				PkgManager:      "apt",
				ShellFlavor:     "gnu",
				HasSudoNopasswd: true,
			},
		},
		{
			name:         "Debian 12 bookworm without sudo-n",
			osReleaseSec: "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nNAME=\"Debian GNU/Linux\"\nID=debian",
			kernelSec:    "6.1.0-25-amd64",
			capSec:       "init=systemd pkg=apt shell=gnu sudo=0",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "debian",
				InitSystem:      "systemd",
				PkgManager:      "apt",
				ShellFlavor:     "gnu",
				HasSudoNopasswd: false,
			},
		},
		{
			name:         "RHEL 9 systemd dnf gnu",
			osReleaseSec: "NAME=\"Red Hat Enterprise Linux\"\nVERSION_ID=\"9.4\"\nID=\"rhel\"\nID_LIKE=\"fedora\"\nPRETTY_NAME=\"Red Hat Enterprise Linux 9.4 (Plow)\"",
			kernelSec:    "5.14.0-427.el9.x86_64",
			capSec:       "init=systemd pkg=dnf shell=gnu sudo=1",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "rhel",
				InitSystem:      "systemd",
				PkgManager:      "dnf",
				ShellFlavor:     "gnu",
				HasSudoNopasswd: true,
			},
		},
		{
			name:         "Legacy CentOS 6 sysv yum gnu fallback",
			osReleaseSec: "CentOS release 6.10 (Final)",
			kernelSec:    "2.6.32-754.el6.x86_64",
			capSec:       "",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "rhel",
				InitSystem:      "sysv",
				PkgManager:      "yum",
				ShellFlavor:     "gnu",
				HasSudoNopasswd: false,
			},
		},
		{
			name:         "Alpine Linux 3.20 openrc apk busybox",
			osReleaseSec: "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.20.3\nPRETTY_NAME=\"Alpine Linux v3.20\"",
			kernelSec:    "6.6.52-0-virt",
			capSec:       "init=openrc pkg=apk shell=busybox sudo=1",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "alpine",
				InitSystem:      "openrc",
				PkgManager:      "apk",
				ShellFlavor:     "busybox",
				HasSudoNopasswd: true,
			},
		},
		{
			name:         "Arch Linux systemd pacman gnu",
			osReleaseSec: "NAME=\"Arch Linux\"\nPRETTY_NAME=\"Arch Linux\"\nID=arch\nBUILD_ID=rolling",
			kernelSec:    "6.11.5-arch1-1",
			capSec:       "init=systemd pkg=pacman shell=gnu sudo=0",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "arch",
				InitSystem:      "systemd",
				PkgManager:      "pacman",
				ShellFlavor:     "gnu",
				HasSudoNopasswd: false,
			},
		},
		{
			name:         "macOS Tahoe with Homebrew launchd bsd",
			osReleaseSec: "NAME=\"macOS\"\nVERSION_ID=\"26.2\"\nPRETTY_NAME=\"macOS 26.2 (Tahoe)\"\nID=macos",
			kernelSec:    "25.2.0",
			capSec:       "init=launchd pkg=brew shell=bsd sudo=0",
			want: store.OSCapabilityProfile{
				OSFamily:        "darwin",
				Distro:          "macos",
				InitSystem:      "launchd",
				PkgManager:      "brew",
				ShellFlavor:     "bsd",
				HasSudoNopasswd: false,
			},
		},
		{
			name:         "macOS without Homebrew launchd bsd",
			osReleaseSec: "ProductName:\tmacOS\nProductVersion:\t15.1",
			kernelSec:    "Darwin 24.1.0",
			capSec:       "init=launchd pkg=none shell=bsd sudo=1",
			want: store.OSCapabilityProfile{
				OSFamily:        "darwin",
				Distro:          "macos",
				InitSystem:      "launchd",
				PkgManager:      "none",
				ShellFlavor:     "bsd",
				HasSudoNopasswd: true,
			},
		},
		{
			name:         "Container without init system",
			osReleaseSec: "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.20.0",
			kernelSec:    "6.8.0-45-generic",
			capSec:       "init=none pkg=apk shell=busybox sudo=0",
			want: store.OSCapabilityProfile{
				OSFamily:        "linux",
				Distro:          "alpine",
				InitSystem:      "none",
				PkgManager:      "apk",
				ShellFlavor:     "busybox",
				HasSudoNopasswd: false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseOSCapabilityProfile(tc.osReleaseSec, tc.kernelSec, tc.capSec)
			if got != tc.want {
				t.Errorf("ParseOSCapabilityProfile() = %+v, want %+v", got, tc.want)
			}

			raw := MarshalOSCapabilityProfile(got)
			var roundTrip store.OSCapabilityProfile
			if err := json.Unmarshal([]byte(raw), &roundTrip); err != nil {
				t.Fatalf("MarshalOSCapabilityProfile produced invalid JSON %q: %v", raw, err)
			}
			if roundTrip != tc.want {
				t.Errorf("JSON round-trip = %+v, want %+v", roundTrip, tc.want)
			}
		})
	}
}

func TestFingerprintFromHost_UsesStoredOSProfile(t *testing.T) {
	prof := store.OSCapabilityProfile{
		OSFamily:        "linux",
		Distro:          "alpine",
		InitSystem:      "openrc",
		PkgManager:      "apk",
		ShellFlavor:     "busybox",
		HasSudoNopasswd: false,
	}
	h := &store.Host{
		ID:            42,
		Name:          "edge-alpine",
		OSInfo:        "Alpine Linux v3.20",
		HardwareModel: "x86_64",
		OSProfile:     MarshalOSCapabilityProfile(prof),
	}
	fp := fingerprintFromHost(nil, h)
	if fp.OSFamily != "linux" || fp.DistroSlug != "alpine" || fp.InitSystem != "openrc" ||
		fp.PackageManager != "apk" || fp.ShellFlavor != "busybox" || fp.HasSudoNopasswd {
		t.Fatalf("unexpected HostFingerprint from stored OSProfile: %+v", fp)
	}
}
