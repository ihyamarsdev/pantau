package inspector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestParseOSFamily(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantFamily  string
		wantManager string
	}{
		{
			name: "Ubuntu 22.04",
			content: `NAME="Ubuntu"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="22.04"`,
			wantFamily:  "ubuntu",
			wantManager: "apt",
		},
		{
			name: "Debian 12",
			content: `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
ID=debian`,
			wantFamily:  "debian",
			wantManager: "apt",
		},
		{
			name: "Rocky Linux 9",
			content: `NAME="Rocky Linux"
ID="rocky"
ID_LIKE="rhel centos fedora"
VERSION_ID="9.2"`,
			wantFamily:  "rhel",
			wantManager: "dnf",
		},
		{
			name: "CentOS 7",
			content: `NAME="CentOS Linux"
ID="centos"
ID_LIKE="rhel fedora"
VERSION_ID="7"`,
			wantFamily:  "rhel",
			wantManager: "yum",
		},
		{
			name: "Alpine 3.18",
			content: `NAME="Alpine Linux"
ID=alpine
VERSION_ID=3.18.4`,
			wantFamily:  "alpine",
			wantManager: "apk",
		},
		{
			name: "Arch Linux",
			content: `NAME="Arch Linux"
ID=arch`,
			wantFamily:  "arch",
			wantManager: "pacman",
		},
		{
			name:        "Legacy CentOS 6 without os-release",
			content:     "CentOS release 6.10 (Final)",
			wantFamily:  "rhel",
			wantManager: "yum",
		},
		{
			name:        "Legacy Debian 7 without os-release",
			content:     "Debian GNU/Linux 7.11 (wheezy)",
			wantFamily:  "debian",
			wantManager: "apt",
		},
		{
			name: "macOS Tahoe with ID=macos",
			content: `NAME="macOS"
ID=macos
VERSION_ID="26.5.1"
PRETTY_NAME="macOS 26.5.1 (Tahoe)"`,
			wantFamily:  "macos",
			wantManager: "softwareupdate",
		},
		{
			name:        "macOS from uname",
			content:     "Darwin Mac-Host 25.5.0 Darwin Kernel Version",
			wantFamily:  "macos",
			wantManager: "softwareupdate",
		},
		{
			name:        "Unknown distro",
			content:     "Custom Embedded Linux",
			wantFamily:  "unknown",
			wantManager: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fam, mgr := parseOSFamily(tt.content)
			if fam != tt.wantFamily || mgr != tt.wantManager {
				t.Fatalf("parseOSFamily() = (%q, %q), want (%q, %q)", fam, mgr, tt.wantFamily, tt.wantManager)
			}
		})
	}
}

func TestParseAptUpdates(t *testing.T) {
	fixture := `Reading package lists...
Building dependency tree...
Reading state information...
The following packages will be upgraded:
  curl libssl3 bash nginx-common
Inst bash [5.0-6ubuntu1.1] (5.0-6ubuntu1.2 Ubuntu:20.04/focal-security [amd64])
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
Inst libssl3 [3.0.2-0ubuntu1.10] (3.0.2-0ubuntu1.12 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])
Inst nginx-common [1.18.0-0ubuntu1] (1.18.0-0ubuntu1.4 ESM-Apps:20.04/focal-apps-security [amd64])
Inst linux-libc-dev [5.10.179-1] (5.10.197-1 Debian-Security:11/bullseye-security [amd64])
Conf libssl3 (3.0.2-0ubuntu1.12 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])`

	findings, pendingSec, pendingTot, notes := parseAptUpdates(fixture)

	if pendingTot != 5 {
		t.Errorf("expected pendingTot 5, got %d", pendingTot)
	}
	if pendingSec != 4 {
		t.Errorf("expected pendingSec 4, got %d", pendingSec)
	}
	if len(findings) != 4 {
		t.Fatalf("expected 4 security findings, got %d", len(findings))
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes, got %v", notes)
	}

	// Verify bash finding
	if findings[0].Package != "bash" || findings[0].InstalledVersion != "5.0-6ubuntu1.1" || findings[0].FixedVersion != "5.0-6ubuntu1.2" {
		t.Errorf("unexpected bash finding: %+v", findings[0])
	}
	// Verify libssl3 finding
	if findings[1].Package != "libssl3" || findings[1].FixedVersion != "3.0.2-0ubuntu1.12" {
		t.Errorf("unexpected libssl3 finding: %+v", findings[1])
	}
	// Verify ESM finding
	if findings[2].Package != "nginx-common" || findings[2].FixedVersion != "1.18.0-0ubuntu1.4" {
		t.Errorf("unexpected nginx-common ESM finding: %+v", findings[2])
	}
	// Verify Debian-Security finding
	if findings[3].Package != "linux-libc-dev" || findings[3].FixedVersion != "5.10.197-1" {
		t.Errorf("unexpected linux-libc-dev finding: %+v", findings[3])
	}
}

func TestParseAptUpdates_NoSecurityUpdates(t *testing.T) {
	fixture := `Reading package lists...
The following packages will be upgraded:
  curl git
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
Inst git [1:2.34.1-1ubuntu1.10] (1:2.34.1-1ubuntu1.11 Ubuntu:22.04/jammy-updates [amd64])`

	findings, pendingSec, pendingTot, _ := parseAptUpdates(fixture)
	if pendingTot != 2 {
		t.Errorf("expected pendingTot 2, got %d", pendingTot)
	}
	if pendingSec != 0 {
		t.Errorf("expected pendingSec 0, got %d", pendingSec)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestParseRpmUpdates(t *testing.T) {
	secOut := `Loaded plugins: fastestmirror
Update Information Summary: available
    3 Security notice(s)
RHSA-2023:1234  Important/Sec.  openssl-1:1.1.1k-9.el8_7.x86_64
ALAS2023-2023-123 Critical/Sec. kernel-tools-libs-5.14.0-362.8.1.el9_3.x86_64
FEDORA-2023-456 Low/Sec.        curl-7.76.1-26.fc38.x86_64`

	totalOut := `Loaded plugins: fastestmirror
curl.x86_64              7.76.1-26.fc38            baseos
openssl.x86_64           1:1.1.1k-9.el8_7          baseos
kernel-tools-libs.x86_64 5.14.0-362.8.1.el9_3      baseos
vim-minimal.x86_64       8.2.2637-20.el9           baseos`

	rpmQaOut := "openssl\t1:1.1.1k-7.el8\nkernel-tools-libs\t5.14.0-284.11.1.el9_2\ncurl\t7.76.1-20.fc38\n"

	findings, pendingSec, pendingTot, notes := parseRpmUpdates(secOut, totalOut, rpmQaOut)

	if pendingSec != 3 {
		t.Errorf("expected pendingSec 3, got %d", pendingSec)
	}
	if pendingTot != 4 {
		t.Errorf("expected pendingTot 4, got %d", pendingTot)
	}
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes, got %v", notes)
	}

	// Verify openssl
	if findings[0].Package != "openssl" || findings[0].InstalledVersion != "1:1.1.1k-7.el8" ||
		findings[0].FixedVersion != "1:1.1.1k-9.el8_7" || findings[0].Severity != "important" || findings[0].Advisory != "RHSA-2023:1234" {
		t.Errorf("unexpected openssl finding: %+v", findings[0])
	}
	// Verify kernel-tools-libs
	if findings[1].Package != "kernel-tools-libs" || findings[1].InstalledVersion != "5.14.0-284.11.1.el9_2" ||
		findings[1].FixedVersion != "5.14.0-362.8.1.el9_3" || findings[1].Severity != "critical" || findings[1].Advisory != "ALAS2023-2023-123" {
		t.Errorf("unexpected kernel-tools-libs finding: %+v", findings[1])
	}
	// Verify curl
	if findings[2].Package != "curl" || findings[2].InstalledVersion != "7.76.1-20.fc38" ||
		findings[2].FixedVersion != "7.76.1-26.fc38" || findings[2].Severity != "low" {
		t.Errorf("unexpected curl finding: %+v", findings[2])
	}
}

func TestParseRpmUpdates_LegacyCentOS7(t *testing.T) {
	// Legacy yum --security check-update format
	secOut := `Loaded plugins: fastestmirror, security
Limiting package lists to security relevant ones
Needed 2 of 10 packages, for security

openssl.x86_64              1:1.0.1e-58.el6_10         updates
bind-libs.x86_64            32:9.8.2-0.68.rc1.el6_10.8 updates`

	totalOut := `openssl.x86_64              1:1.0.1e-58.el6_10         updates
bind-libs.x86_64            32:9.8.2-0.68.rc1.el6_10.8 updates
glibc.x86_64                2.12-1.212.el6_10.3        updates`

	rpmQaOut := "openssl\t1:1.0.1e-57.el6\nbind-libs\t32:9.8.2-0.62.rc1.el6\n"

	findings, pendingSec, pendingTot, _ := parseRpmUpdates(secOut, totalOut, rpmQaOut)

	if pendingSec != 2 {
		t.Errorf("expected pendingSec 2, got %d", pendingSec)
	}
	if pendingTot != 3 {
		t.Errorf("expected pendingTot 3, got %d", pendingTot)
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Package != "openssl" || findings[0].InstalledVersion != "1:1.0.1e-57.el6" || findings[0].FixedVersion != "1:1.0.1e-58.el6_10" {
		t.Errorf("unexpected openssl finding: %+v", findings[0])
	}
}

func TestParseRebootRequired(t *testing.T) {
	if !parseRebootRequired("REBOOT_REQUIRED\nNEEDS_RESTARTING:0") {
		t.Errorf("expected reboot required from REBOOT_REQUIRED")
	}
	if !parseRebootRequired("NEEDS_RESTARTING:1") {
		t.Errorf("expected reboot required from NEEDS_RESTARTING:1")
	}
	if parseRebootRequired("NEEDS_RESTARTING:0") {
		t.Errorf("expected no reboot required from NEEDS_RESTARTING:0")
	}
	if parseRebootRequired("") {
		t.Errorf("expected no reboot required from empty output")
	}
}

func TestParseRepoCacheAge(t *testing.T) {
	now := time.Now().Unix()

	// 1. Fresh apt cache (10 hours old)
	aptFresh := fmt.Sprintf("%d\n---APT---\n%d\n---DNF---\n", now, now-36000)
	age, notes := parseRepoCacheAge(aptFresh, "apt")
	if age != 10 {
		t.Errorf("expected age 10, got %d", age)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes for fresh cache, got %v", notes)
	}

	// 2. Stale cache (200 hours old)
	dnfStale := fmt.Sprintf("%d\n---APT---\n\n---DNF---\n%d\n", now, now-720000)
	age, notes = parseRepoCacheAge(dnfStale, "dnf")
	if age != 200 {
		t.Errorf("expected age 200, got %d", age)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "package metadata is 8 days old") {
		t.Errorf("expected stale note, got %v", notes)
	}

	// 3. Unknown cache
	unknownAge, _ := parseRepoCacheAge("0\n---APT---\n\n---DNF---\n", "apt")
	if unknownAge != -1 {
		t.Errorf("expected age -1 for unknown cache, got %d", unknownAge)
	}
}

func TestParseHardeningChecks(t *testing.T) {
	// 1. sshd_config with comments, drop-ins, and match blocks
	sshdConfig := `
FILE:/etc/ssh/sshd_config
# Global settings
PermitRootLogin prohibit-password
PasswordAuthentication no

# Match block should not overwrite globals
Match User admin
    PasswordAuthentication yes
    PermitRootLogin yes
`
	permitRoot, passAuth, notes := parseSshdConfig(sshdConfig)
	if permitRoot.Status != "pass" {
		t.Errorf("expected PermitRootLogin pass, got %s (%s)", permitRoot.Status, permitRoot.Detail)
	}
	if passAuth.Status != "pass" {
		t.Errorf("expected PasswordAuthentication pass, got %s (%s)", passAuth.Status, passAuth.Detail)
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes, got %v", notes)
	}

	// 2. sshd_config with PermitRootLogin yes and default PasswordAuthentication (unset -> warn)
	sshdBad := `
FILE:/etc/ssh/sshd_config
PermitRootLogin yes
`
	permitBad, passBad, _ := parseSshdConfig(sshdBad)
	if permitBad.Status != "fail" {
		t.Errorf("expected PermitRootLogin fail for yes, got %s", permitBad.Status)
	}
	if passBad.Status != "warn" {
		t.Errorf("expected PasswordAuthentication warn for unset default, got %s", passBad.Status)
	}

	// 3. Unreadable sshd_config
	permitUnread, passUnread, unreadNotes := parseSshdConfig("SSHD_UNREADABLE")
	if permitUnread.Status != "unknown" || passUnread.Status != "unknown" {
		t.Errorf("expected unknown for unreadable sshd, got %s/%s", permitUnread.Status, passUnread.Status)
	}
	if len(unreadNotes) == 0 {
		t.Error("expected note for unreadable sshd_config")
	}

	// 4. Auto updates tests
	autoCheck1, _ := parseAutoUpdates("UNATTENDED_ACTIVE:active\nUNATTENDED_ENABLED:enabled")
	if autoCheck1.Status != "pass" {
		t.Errorf("expected auto updates pass for unattended-upgrades, got %s", autoCheck1.Status)
	}

	autoCheck2, _ := parseAutoUpdates("DNF_TIMER_ACTIVE:active\n")
	if autoCheck2.Status != "pass" {
		t.Errorf("expected auto updates pass for dnf-automatic, got %s", autoCheck2.Status)
	}

	autoCheckNone, _ := parseAutoUpdates("UNATTENDED_ACTIVE:inactive\nDNF_TIMER_ACTIVE:inactive")
	if autoCheckNone.Status != "warn" {
		t.Errorf("expected auto updates warn when inactive, got %s", autoCheckNone.Status)
	}

	// 5. Firewall tests
	fwUfw, _ := parseFirewall("---UFW---\nStatus: active\n")
	if fwUfw.Status != "pass" {
		t.Errorf("expected firewall pass for ufw active, got %s", fwUfw.Status)
	}

	fwD, _ := parseFirewall("---UFW---\n---FIREWALLD---\nactive\n---NFT---\n")
	if fwD.Status != "pass" {
		t.Errorf("expected firewall pass for firewalld active, got %s", fwD.Status)
	}

	fwPerm, fwNotes := parseFirewall("---UFW---\n---FIREWALLD---\n---NFT---\n---IPTABLES---\nPermission denied (you must be root)\n")
	if fwPerm.Status != "unknown" {
		t.Errorf("expected firewall unknown for permission denied, got %s", fwPerm.Status)
	}
	if len(fwNotes) == 0 {
		t.Error("expected note for permission denied firewall")
	}
}

func TestStatusDerivation(t *testing.T) {
	now := time.Now().Unix()

	// 1. Supported Debian, fresh cache, all checks pass -> "ok"
	debianOk := fmt.Sprintf(`===OS_RELEASE===
ID=debian
===CACHE_AGE===
%d
---APT---
%d
===REBOOT===
===UPDATES===
MGR:apt
===SSHD_CONFIG===
FILE:/etc/ssh/sshd_config
PermitRootLogin no
PasswordAuthentication no
===AUTO_UPDATES===
UNATTENDED_ACTIVE:active
===FIREWALL===
Status: active
`, now, now-1000)

	scanOk := parseSecurityScanOutput(debianOk, 1)
	if scanOk.Status != "ok" {
		t.Errorf("expected status 'ok', got %q (notes: %v)", scanOk.Status, scanOk.Notes)
	}

	// 2. Unsupported Alpine -> "unsupported", hardening still runs
	alpineOut := fmt.Sprintf(`===OS_RELEASE===
ID=alpine
===CACHE_AGE===
%d
===REBOOT===
===UPDATES===
MGR:apk
===SSHD_CONFIG===
FILE:/etc/ssh/sshd_config
PermitRootLogin no
PasswordAuthentication no
===AUTO_UPDATES===
===FIREWALL===
Status: active
`, now)

	scanAlpine := parseSecurityScanOutput(alpineOut, 2)
	if scanAlpine.Status != "unsupported" {
		t.Errorf("expected status 'unsupported', got %q", scanAlpine.Status)
	}
	if len(scanAlpine.Hardening) != 4 {
		t.Errorf("expected 4 hardening checks even on unsupported distro, got %d", len(scanAlpine.Hardening))
	}
	if len(scanAlpine.Notes) == 0 || !strings.Contains(scanAlpine.Notes[0], "not supported") {
		t.Errorf("expected unsupported note, got %v", scanAlpine.Notes)
	}

	// 3. Stale cache (>168h) -> "partial"
	staleOut := fmt.Sprintf(`===OS_RELEASE===
ID=ubuntu
===CACHE_AGE===
%d
---APT---
%d
===REBOOT===
===UPDATES===
MGR:apt
===SSHD_CONFIG===
FILE:/etc/ssh/sshd_config
PermitRootLogin no
PasswordAuthentication no
===AUTO_UPDATES===
UNATTENDED_ACTIVE:active
===FIREWALL===
Status: active
`, now, now-700000)

	scanStale := parseSecurityScanOutput(staleOut, 3)
	if scanStale.Status != "partial" {
		t.Errorf("expected status 'partial' for stale cache, got %q", scanStale.Status)
	}

	// 4. Permission denied in hardening -> "partial"
	permOut := fmt.Sprintf(`===OS_RELEASE===
ID=ubuntu
===CACHE_AGE===
%d
---APT---
%d
===REBOOT===
===UPDATES===
MGR:apt
===SSHD_CONFIG===
SSHD_UNREADABLE
===AUTO_UPDATES===
UNATTENDED_ACTIVE:active
===FIREWALL===
Status: active
`, now, now-1000)

	scanPerm := parseSecurityScanOutput(permOut, 4)
	if scanPerm.Status != "ok" {
		t.Errorf("update collection was complete; expected status 'ok' even with unreadable sshd config, got %q", scanPerm.Status)
	}
	foundNote := false
	for _, n := range scanPerm.Notes {
		if strings.Contains(n, "hardening check(s) could not be evaluated") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected a coverage note about unevaluable hardening checks, got %v", scanPerm.Notes)
	}
}

func TestScanSecurityIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-sec-scan-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "Server Sec", Host: "10.0.0.12", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	mockOutput := fmt.Sprintf(`===OS_RELEASE===
ID=ubuntu
===CACHE_AGE===
%d
---APT---
%d
===REBOOT===
REBOOT_REQUIRED
===UPDATES===
MGR:apt
Inst libssl3 [3.0.2-0ubuntu1.10] (3.0.2-0ubuntu1.12 Ubuntu:22.04/jammy-security [amd64])
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
===SSHD_CONFIG===
FILE:/etc/ssh/sshd_config
PermitRootLogin no
PasswordAuthentication no
===AUTO_UPDATES===
UNATTENDED_ACTIVE:active
===FIREWALL===
Status: active
`, now, now-5000)

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return mockOutput, "", 0, nil
	}

	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}, nil)

	// 1. Run ScanSecurity
	if err := ins.ScanSecurity(hostID); err != nil {
		t.Fatalf("scan security failed: %v", err)
	}

	// 2. Verify stored scan
	saved, err := db.GetSecurityScan(hostID)
	if err != nil {
		t.Fatalf("get security scan failed: %v", err)
	}
	if saved == nil {
		t.Fatal("expected stored security scan, got nil")
	}
	if saved.Status != "ok" || saved.PackageManager != "apt" || saved.OSFamily != "ubuntu" {
		t.Errorf("unexpected metadata: %+v", saved)
	}
	if saved.PendingSecurity != 1 || saved.PendingTotal != 2 || !saved.RebootRequired {
		t.Errorf("unexpected scan counts: %+v", saved)
	}
	if len(saved.Findings) != 1 || saved.Findings[0].Package != "libssl3" {
		t.Errorf("unexpected findings: %+v", saved.Findings)
	}
	if len(saved.Hardening) != 4 {
		t.Errorf("expected 4 hardening checks, got %d", len(saved.Hardening))
	}

	// 3. Concurrency guard test
	execStarted := make(chan struct{})
	execFinish := make(chan struct{})

	mockBlockRunner := sshrunner.NewMockRunner()
	mockBlockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		close(execStarted)
		<-execFinish
		return mockOutput, "", 0, nil
	}

	insBlock := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockBlockRunner, nil
	}, nil)

	go func() {
		_ = insBlock.ScanSecurity(hostID)
	}()

	<-execStarted

	// During scan, IsSecurityScanning must be true
	if !insBlock.IsSecurityScanning(hostID) {
		t.Error("expected IsSecurityScanning to be true while scan in flight")
	}

	// Concurrent scan must return ErrSecurityScanInProgress
	err = insBlock.ScanSecurity(hostID)
	if !errors.Is(err, ErrSecurityScanInProgress) {
		t.Errorf("expected ErrSecurityScanInProgress, got %v", err)
	}

	close(execFinish)

	// Wait briefly for goroutine to finish
	for range 50 {
		if !insBlock.IsSecurityScanning(hostID) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 4. Connection failure keeps prior scan
	failRunner := sshrunner.NewMockRunner()
	insFail := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return nil, errors.New("ssh connection refused")
	}, nil)
	_ = failRunner

	err = insFail.ScanSecurity(hostID)
	if err == nil {
		t.Fatal("expected error on connection failure, got nil")
	}

	// Prior scan must still exist in DB unmodified
	scanAfterFail, err := db.GetSecurityScan(hostID)
	if err != nil {
		t.Fatalf("get scan after fail: %v", err)
	}
	if scanAfterFail == nil || scanAfterFail.PendingSecurity != 1 {
		t.Errorf("expected prior scan to remain preserved, got %+v", scanAfterFail)
	}
}

func TestEvaluateSecurityUpdatesRule(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-rule-eval-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "Server Rule", Host: "10.0.0.15", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}

	ins := New(db, nil, nil)
	rule := &store.DesiredRule{HostID: hostID, Kind: "security_updates", Target: "security", Expected: "none", Enabled: true}

	// 1. Not scanned yet -> drift, current: not_scanned
	curr, status, _, rc := ins.evaluateRule(h, nil, rule)
	if status != "drift" || curr != "not_scanned" {
		t.Errorf("expected drift/not_scanned for unscanned host, got %s/%s (rc: %s)", status, curr, rc)
	}

	// 2. Scan status unsupported -> drift, current: unsupported
	unsupportedScan := &store.SecurityScan{
		HostID:         hostID,
		ScannedAt:      time.Now(),
		Status:         "unsupported",
		PackageManager: "apk",
	}
	_ = db.SaveSecurityScan(unsupportedScan)

	curr, status, _, _ = ins.evaluateRule(h, nil, rule)
	if status != "drift" || curr != "unsupported" {
		t.Errorf("expected drift/unsupported, got %s/%s", status, curr)
	}

	// 3. Scan status error -> drift, current: error
	errorScan := &store.SecurityScan{
		HostID:    hostID,
		ScannedAt: time.Now(),
		Status:    "error",
		Error:     "remote command failure",
	}
	_ = db.SaveSecurityScan(errorScan)

	curr, status, _, _ = ins.evaluateRule(h, nil, rule)
	if status != "drift" || curr != "error" {
		t.Errorf("expected drift/error, got %s/%s", status, curr)
	}

	// 4. Stale scan (>72h) -> drift, current: stale
	staleScan := &store.SecurityScan{
		HostID:          hostID,
		ScannedAt:       time.Now().Add(-80 * time.Hour),
		Status:          "ok",
		PendingSecurity: 0,
	}
	_ = db.SaveSecurityScan(staleScan)

	curr, status, _, _ = ins.evaluateRule(h, nil, rule)
	if status != "drift" || curr != "stale" {
		t.Errorf("expected drift/stale, got %s/%s", status, curr)
	}

	// 5. Clean scan with 0 pending security updates and Expected "none" -> ok
	cleanScan := &store.SecurityScan{
		HostID:          hostID,
		ScannedAt:       time.Now(),
		Status:          "ok",
		PendingSecurity: 0,
	}
	_ = db.SaveSecurityScan(cleanScan)

	curr, status, details, _ := ins.evaluateRule(h, nil, rule)
	if status != "ok" || curr != "0 pending" {
		t.Errorf("expected ok/0 pending, got %s/%s (%s)", status, curr, details)
	}

	// 6. 3 pending security updates with Expected "none" -> drift, rootCause lists findings
	driftScan := &store.SecurityScan{
		HostID:          hostID,
		ScannedAt:       time.Now(),
		Status:          "ok",
		PendingSecurity: 3,
		Findings: []store.SecurityFinding{
			{Package: "openssl", InstalledVersion: "1.1.1k", FixedVersion: "1.1.1m", Severity: "important", Advisory: "RHSA-2023:111"},
			{Package: "glibc", InstalledVersion: "2.28-1", FixedVersion: "2.28-2", Severity: "critical", Advisory: "RHSA-2023:222"},
			{Package: "curl", InstalledVersion: "7.61-1", FixedVersion: "7.61-2", Severity: "low", Advisory: ""},
		},
	}
	_ = db.SaveSecurityScan(driftScan)

	curr, status, _, rc = ins.evaluateRule(h, nil, rule)
	if status != "drift" || curr != "3 pending" {
		t.Errorf("expected drift/3 pending, got %s/%s", status, curr)
	}
	if !strings.Contains(rc, "openssl") || !strings.Contains(rc, "glibc") || !strings.Contains(rc, "critical") {
		t.Errorf("expected rootCause to include finding details, got %s", rc)
	}

	// 7. Expected "<=3" with 3 pending updates -> ok
	ruleLe3 := &store.DesiredRule{HostID: hostID, Kind: "security_updates", Target: "security", Expected: "<=3", Enabled: true}
	curr, status, _, _ = ins.evaluateRule(h, nil, ruleLe3)
	if status != "ok" || curr != "3 pending" {
		t.Errorf("expected ok for <=3 with 3 pending, got %s/%s", status, curr)
	}

	// 8. Expected "<=2" with 3 pending updates -> drift
	ruleLe2 := &store.DesiredRule{HostID: hostID, Kind: "security_updates", Target: "security", Expected: "<=2", Enabled: true}
	curr, status, _, _ = ins.evaluateRule(h, nil, ruleLe2)
	if status != "drift" || curr != "3 pending" {
		t.Errorf("expected drift for <=2 with 3 pending, got %s/%s", status, curr)
	}

	// 9. Invalid expected value -> drift with explanatory message
	ruleInvalid := &store.DesiredRule{HostID: hostID, Kind: "security_updates", Target: "security", Expected: "invalid_val", Enabled: true}
	curr, status, _, rc = ins.evaluateRule(h, nil, ruleInvalid)
	if status != "drift" || curr != "invalid_expected" {
		t.Errorf("expected drift/invalid_expected, got %s/%s", status, curr)
	}
	if !strings.Contains(rc, "Invalid expected value") {
		t.Errorf("expected explanatory rootCause for invalid expected, got %s", rc)
	}
}

func TestParseSoftwareUpdate_Clean(t *testing.T) {
	output := `Software Update Tool

Finding available software
No new software available.
`
	findings, pendingSec, pendingTot, rebootReq, notes := parseSoftwareUpdate(output, "26.5.1")
	if pendingTot != 0 {
		t.Errorf("expected 0 pendingTot, got %d", pendingTot)
	}
	if pendingSec != 0 {
		t.Errorf("expected 0 pendingSec, got %d", pendingSec)
	}
	if rebootReq {
		t.Errorf("expected rebootReq false, got true")
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "heuristic") && !strings.Contains(notes[0], "security updates") {
		t.Errorf("expected honesty note, got %v", notes)
	}
}

func TestParseSoftwareUpdate_SafariAndCLT(t *testing.T) {
	output := `Software Update Tool

Finding available software
Software Update found the following new or updated software:
* Label: Command Line Tools for Xcode 26.6-26.6
	Title: Command Line Tools for Xcode 26.6, Version: 26.6, Size: 920431KiB, Recommended: YES,
* Label: Safari27.2TahoeAuto-27.2
	Title: Safari, Version: 27.2, Size: 250130KiB, Recommended: YES,
`
	findings, pendingSec, pendingTot, rebootReq, _ := parseSoftwareUpdate(output, "26.2")
	if pendingTot != 2 {
		t.Errorf("expected pendingTot 2, got %d", pendingTot)
	}
	if pendingSec != 1 {
		t.Errorf("expected pendingSec 1 (Safari only), got %d", pendingSec)
	}
	if rebootReq {
		t.Errorf("expected rebootReq false, got true")
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Package != "Safari" {
		t.Errorf("expected Package 'Safari', got %q", findings[0].Package)
	}
	if findings[0].FixedVersion != "27.2" {
		t.Errorf("expected FixedVersion '27.2', got %q", findings[0].FixedVersion)
	}
	if findings[0].Severity != "unknown" {
		t.Errorf("expected Severity 'unknown', got %q", findings[0].Severity)
	}
}

func TestParseSoftwareUpdate_PointUpdateWithRestart(t *testing.T) {
	output := `Software Update Tool

Finding available software
Software Update found the following new or updated software:
* Label: macOS Tahoe 26.6-26.6
	Title: macOS Tahoe 26.6, Version: 26.6, Size: 3123456KiB, Recommended: YES, Action: restart,
`
	findings, pendingSec, pendingTot, rebootReq, _ := parseSoftwareUpdate(output, "26.5.1")
	if pendingTot != 1 {
		t.Errorf("expected pendingTot 1, got %d", pendingTot)
	}
	if pendingSec != 1 {
		t.Errorf("expected pendingSec 1, got %d", pendingSec)
	}
	if !rebootReq {
		t.Errorf("expected rebootReq true, got false")
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Package != "macOS Tahoe 26.6" {
		t.Errorf("expected Package 'macOS Tahoe 26.6', got %q", findings[0].Package)
	}
	if findings[0].FixedVersion != "26.6" {
		t.Errorf("expected FixedVersion '26.6', got %q", findings[0].FixedVersion)
	}
	if findings[0].InstalledVersion != "26.5.1" {
		t.Errorf("expected InstalledVersion '26.5.1', got %q", findings[0].InstalledVersion)
	}
}

func TestParseSoftwareUpdate_UnreachableAndTimeout(t *testing.T) {
	unreachable := `Software Update Tool

Finding available software
Can't connect to the Apple Software Update server.
`
	findings, pendingSec, pendingTot, rebootReq, notes := parseSoftwareUpdate(unreachable, "26.5.1")
	if pendingTot != 0 || pendingSec != 0 || rebootReq || len(findings) != 0 {
		t.Errorf("expected zero results for unreachable, got tot=%d sec=%d reboot=%v findings=%d", pendingTot, pendingSec, rebootReq, len(findings))
	}
	if len(notes) == 0 || !strings.Contains(strings.ToLower(notes[0]), "timed out") && !strings.Contains(strings.ToLower(notes[0]), "failed") {
		t.Errorf("expected error note for unreachable, got %v", notes)
	}

	timeoutText := `Software Update Tool

Finding available software
softwareupdate: timed out
`
	_, _, _, _, timeoutNotes := parseSoftwareUpdate(timeoutText, "26.5.1")
	if len(timeoutNotes) == 0 || !strings.Contains(strings.ToLower(timeoutNotes[0]), "timed out") && !strings.Contains(strings.ToLower(timeoutNotes[0]), "failed") {
		t.Errorf("expected error note for timeout, got %v", timeoutNotes)
	}
}

func TestParseSecurityScanOutput_MacOS(t *testing.T) {
	// 1. Clean macOS scan: status MUST be "partial" (never "ok") due to Apple's unclassified updates
	cleanBatch := `===OS_RELEASE===
ID=macos
NAME="macOS"
VERSION_ID="26.5.1"
PRETTY_NAME="macOS 26.5.1 (Tahoe)"
===CACHE_AGE===
1793256603
===REBOOT===
===UPDATES===
MGR:softwareupdate
Software Update Tool

Finding available software
No new software available.
===SSHD_CONFIG===
FILE:/etc/ssh/sshd_config
UsePAM yes
===AUTO_UPDATES===
    AutomaticDownload = 1;
    AutomaticallyInstallMacOSUpdates = 1;
    ConfigDataInstall = 1;
    CriticalUpdateInstall = 1;
===FIREWALL===
Firewall is enabled. (State = 1)
`
	scan := parseSecurityScanOutput(cleanBatch, 10)
	if scan.OSFamily != "macos" {
		t.Errorf("expected OSFamily 'macos', got %q", scan.OSFamily)
	}
	if scan.PackageManager != "softwareupdate" {
		t.Errorf("expected PackageManager 'softwareupdate', got %q", scan.PackageManager)
	}
	if scan.Status != "partial" {
		t.Errorf("expected Status 'partial', got %q", scan.Status)
	}
	if scan.RepoCacheAgeHours != -1 {
		t.Errorf("expected RepoCacheAgeHours -1, got %d", scan.RepoCacheAgeHours)
	}
	if scan.PendingSecurity != 0 || scan.PendingTotal != 0 {
		t.Errorf("expected 0 pending, got sec=%d tot=%d", scan.PendingSecurity, scan.PendingTotal)
	}
	if len(scan.Hardening) != 4 {
		t.Fatalf("expected 4 hardening checks, got %d", len(scan.Hardening))
	}
	for _, h := range scan.Hardening {
		if h.Status != "pass" && h.ID != "sshd_password_auth" {
			t.Errorf("expected pass for %s, got %s", h.ID, h.Status)
		}
	}

	// 2. Unreachable softwareupdate: status MUST be "partial" (never "ok")
	unreachBatch := `===OS_RELEASE===
ID=macos
NAME="macOS"
VERSION_ID="26.2"
===CACHE_AGE===
1793256603
===REBOOT===
===UPDATES===
MGR:softwareupdate
Software Update Tool

Finding available software
Can't connect to the Apple Software Update server.
===SSHD_CONFIG===
FILE:/etc/ssh/sshd_config
UsePAM yes
===AUTO_UPDATES===
    AutomaticDownload = 1;
===FIREWALL===
Firewall is disabled. (State = 0)
`
	unreachScan := parseSecurityScanOutput(unreachBatch, 11)
	if unreachScan.Status != "partial" {
		t.Errorf("expected Status 'partial' for unreachable, got %q", unreachScan.Status)
	}
	hasErrorNote := false
	for _, note := range unreachScan.Notes {
		if strings.Contains(strings.ToLower(note), "failed") || strings.Contains(strings.ToLower(note), "timed out") {
			hasErrorNote = true
			break
		}
	}
	if !hasErrorNote {
		t.Errorf("expected error/timeout note for unreachable softwareupdate, got notes: %v", unreachScan.Notes)
	}
}

func TestParseHardeningChecks_MacOS_Fixtures(t *testing.T) {
	// Fixture A: macstudio-A (only AutomaticDownload = 1, Firewall State = 0)
	autoSecA := "    AutomaticDownload = 1;\n"
	fwSecA := "Firewall is disabled. (State = 0)\n"
	sshdSecA := "FILE:/etc/ssh/sshd_config\nUsePAM yes\n"

	checksA, _ := parseHardeningChecks(sshdSecA, autoSecA, fwSecA)
	if len(checksA) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(checksA))
	}
	mA := make(map[string]store.HardeningCheck)
	for _, c := range checksA {
		mA[c.ID] = c
	}
	if mA["sshd_permit_root_login"].Status != "pass" {
		t.Errorf("expected sshd_permit_root_login pass, got %s", mA["sshd_permit_root_login"].Status)
	}
	if mA["sshd_password_auth"].Status != "warn" {
		t.Errorf("expected sshd_password_auth warn, got %s", mA["sshd_password_auth"].Status)
	}
	if mA["auto_security_updates"].Status != "warn" {
		t.Errorf("expected auto_security_updates warn for macstudio-A, got %s", mA["auto_security_updates"].Status)
	}
	if mA["firewall_active"].Status != "warn" {
		t.Errorf("expected firewall_active warn for State=0, got %s", mA["firewall_active"].Status)
	}

	// Fixture B: macmini-B (all 4 auto keys = 1, Firewall State = 1)
	autoSecB := `    AutomaticDownload = 1;
    AutomaticallyInstallMacOSUpdates = 1;
    ConfigDataInstall = 1;
    CriticalUpdateInstall = 1;`
	fwSecB := "Firewall is enabled. (State = 1)\n"
	sshdSecB := "FILE:/etc/ssh/sshd_config\nUsePAM yes\n"

	checksB, _ := parseHardeningChecks(sshdSecB, autoSecB, fwSecB)
	mB := make(map[string]store.HardeningCheck)
	for _, c := range checksB {
		mB[c.ID] = c
	}
	if mB["auto_security_updates"].Status != "pass" {
		t.Errorf("expected auto_security_updates pass for macmini-B, got %s", mB["auto_security_updates"].Status)
	}
	if mB["firewall_active"].Status != "pass" {
		t.Errorf("expected firewall_active pass for State=1, got %s", mB["firewall_active"].Status)
	}

	// Unreadable checks
	autoUnread, notesAuto := parseAutoUpdates("DEFAULTS_UNREADABLE")
	if autoUnread.Status != "unknown" {
		t.Errorf("expected unknown for unreadable auto updates, got %s", autoUnread.Status)
	}
	if len(notesAuto) == 0 {
		t.Error("expected note for unreadable auto updates")
	}

	fwUnread, notesFw := parseFirewall("SOCKETFILTERFW_UNREADABLE")
	if fwUnread.Status != "unknown" {
		t.Errorf("expected unknown for unreadable firewall, got %s", fwUnread.Status)
	}
	if len(notesFw) == 0 {
		t.Error("expected note for unreadable firewall")
	}
}
