package inspector

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pantau/internal/store"
)

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("testdata", "macos", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", filename, err)
	}
	return string(data)
}

func TestDarwin_SplitFixtureSections(t *testing.T) {
	content := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	sec := SplitFixtureSections(content)

	if sec["whoami"] != "macuser\n501\nshell=/bin/zsh\nPATH=/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Fatalf("unexpected whoami section: %q", sec["whoami"])
	}
	if !strings.Contains(sec["sw_vers"], "ProductName:\t\tmacOS") {
		t.Fatalf("expected sw_vers to contain macOS, got: %q", sec["sw_vers"])
	}
	if !strings.Contains(sec["vm_stat"], "Mach Virtual Memory Statistics:") {
		t.Fatalf("expected vm_stat to have Mach header, got: %q", sec["vm_stat"])
	}
}

func TestDarwin_Memory_MacMini(t *testing.T) {
	// Mac mini fixture: 24 GB RAM, 6 GB swap used
	content := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	sec := SplitFixtureSections(content)

	// Hand-calculated Activity Monitor formula from fixture:
	// Page size: 16384 bytes
	// Anonymous: 135970
	// Purgeable: 4266
	// Wired: 353576
	// Compressor: 7371
	// Used pages = 135970 - 4266 + 353576 + 7371 = 492651
	// Used bytes = 492651 * 16384 = 8,071,593,984 bytes
	// Total RAM = 25769803776 bytes
	// Swap usage: total = 7168.00M (7516192768 bytes), used = 6034.31M (6327432643 bytes)
	const expectedRAMUsed = int64(8071593984)
	const expectedRAMTotal = int64(25769803776)
	const expectedSwapTotal = int64(7516192768)
	const expectedSwapUsed = int64(6327432643)

	sysctlLines := strings.Split(sec["sysctl_basic"], "\n")
	memsize, err := strconv.ParseInt(strings.TrimSpace(sysctlLines[0]), 10, 64)
	if err != nil {
		t.Fatalf("parse hw.memsize: %v", err)
	}

	ramUsed, ramTot, swpUsed, swpTot := ParseDarwinVMStat(sec["vm_stat"], sec["swapusage"], memsize)
	if ramTot != expectedRAMTotal {
		t.Errorf("RAM Total: expected %d, got %d", expectedRAMTotal, ramTot)
	}
	if ramUsed != expectedRAMUsed {
		t.Errorf("RAM Used: expected %d, got %d", expectedRAMUsed, ramUsed)
	}
	if swpTot != expectedSwapTotal {
		t.Errorf("Swap Total: expected %d, got %d", expectedSwapTotal, swpTot)
	}
	if swpUsed != expectedSwapUsed {
		t.Errorf("Swap Used: expected %d, got %d", expectedSwapUsed, swpUsed)
	}

	// Verify parseFree works on the formatted Mem/Swap lines emitted by the Darwin batch script
	freeFormat := "Mem: 25769803776 8071593984 0\nSwap: 7516192768 6327432643 0\n"
	pfUsed, pfTot, pfSwpUsed, pfSwpTot := parseFree(freeFormat)
	if pfUsed != expectedRAMUsed || pfTot != expectedRAMTotal || pfSwpUsed != expectedSwapUsed || pfSwpTot != expectedSwapTotal {
		t.Errorf("parseFree result mismatch: got used=%d, tot=%d, swpUsed=%d, swpTot=%d", pfUsed, pfTot, pfSwpUsed, pfSwpTot)
	}
}

func TestDarwin_Memory_MacStudio(t *testing.T) {
	// Mac Studio fixture: 256 GB RAM, 0 swap used
	content := loadFixture(t, "macstudio-m3ultra-256gb.txt")
	sec := SplitFixtureSections(content)

	// Hand-calculated Activity Monitor formula from fixture:
	// Page size: 16384 bytes
	// Anonymous: 8526301
	// Purgeable: 4173
	// Wired: 287766
	// Compressor: 12538
	// Used pages = 8526301 - 4173 + 287766 + 12538 = 8822432
	// Used bytes = 8822432 * 16384 = 144,546,725,888 bytes
	// Total RAM = 274877906944 bytes
	// Swap usage: total = 0.00M, used = 0.00M
	const expectedRAMUsed = int64(144546725888)
	const expectedRAMTotal = int64(274877906944)
	const expectedSwapTotal = int64(0)
	const expectedSwapUsed = int64(0)

	sysctlLines := strings.Split(sec["sysctl_basic"], "\n")
	memsize, err := strconv.ParseInt(strings.TrimSpace(sysctlLines[0]), 10, 64)
	if err != nil {
		t.Fatalf("parse hw.memsize: %v", err)
	}

	ramUsed, ramTot, swpUsed, swpTot := ParseDarwinVMStat(sec["vm_stat"], sec["swapusage"], memsize)
	if ramTot != expectedRAMTotal {
		t.Errorf("RAM Total: expected %d, got %d", expectedRAMTotal, ramTot)
	}
	if ramUsed != expectedRAMUsed {
		t.Errorf("RAM Used: expected %d, got %d", expectedRAMUsed, ramUsed)
	}
	if swpTot != expectedSwapTotal {
		t.Errorf("Swap Total: expected %d, got %d", expectedSwapTotal, swpTot)
	}
	if swpUsed != expectedSwapUsed {
		t.Errorf("Swap Used: expected %d, got %d", expectedSwapUsed, swpUsed)
	}
}

func TestDarwin_Disk_DataVolume(t *testing.T) {
	// Mac mini Data volume: total = 482797652 KB, avail = 296623976 KB
	// Used = (482797652 - 296623976) * 1024 = 190,641,844,224 bytes
	// Total = 482797652 * 1024 = 494,384,795,648 bytes
	miniContent := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	miniSec := SplitFixtureSections(miniContent)
	usedMini, totMini := parseDf(miniSec["df_data"])
	const expectedMiniTotal = int64(494384795648)
	const expectedMiniUsed = int64(190641844224)
	if totMini != expectedMiniTotal || usedMini != expectedMiniUsed {
		t.Errorf("Mac mini parseDf: expected used=%d tot=%d, got used=%d tot=%d", expectedMiniUsed, expectedMiniTotal, usedMini, totMini)
	}

	// Mac Studio Data volume: total = 971350180 KB, avail = 634071780 KB
	// Used = (971350180 - 634071780) * 1024 = 345,373,081,600 bytes
	// Total = 971350180 * 1024 = 994,662,584,320 bytes
	studioContent := loadFixture(t, "macstudio-m3ultra-256gb.txt")
	studioSec := SplitFixtureSections(studioContent)
	usedStudio, totStudio := parseDf(studioSec["df_data"])
	const expectedStudioTotal = int64(994662584320)
	const expectedStudioUsed = int64(345373081600)
	if totStudio != expectedStudioTotal || usedStudio != expectedStudioUsed {
		t.Errorf("Mac Studio parseDf: expected used=%d tot=%d, got used=%d tot=%d", expectedStudioUsed, expectedStudioTotal, usedStudio, totStudio)
	}

	// Also verify normalized df line emitted by the Darwin awk script:
	// "/dev/disk3s5 482797652 186173676 296623976 35% /"
	normalizedMini := "/dev/disk3s5 482797652 186173676 296623976 35% /\n"
	nUsed, nTot := parseDf(normalizedMini)
	if nTot != expectedMiniTotal || nUsed != expectedMiniUsed {
		t.Errorf("Normalized df: expected used=%d tot=%d, got used=%d tot=%d", expectedMiniUsed, expectedMiniTotal, nUsed, nTot)
	}
}

func TestDarwin_NetworkTotals_Netstat(t *testing.T) {
	// Mac mini fixture: physical en0 only
	// en0 Ibytes: 340465305228, Obytes: 343818145131
	miniContent := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	miniSec := SplitFixtureSections(miniContent)
	rxMini, txMini := ParseDarwinNetstatTotals(miniSec["netstat_ib"])
	const expectedRxMini = int64(340465305228)
	const expectedTxMini = int64(343818145131)
	if rxMini != expectedRxMini || txMini != expectedTxMini {
		t.Errorf("Mac mini Netstat totals: expected rx=%d tx=%d, got rx=%d tx=%d", expectedRxMini, expectedTxMini, rxMini, txMini)
	}

	// Extra probe: macmini-B has multiple en interfaces (en0, en5, en6, en7, en2)
	// en0: 340479383728 rx, 343822768478 tx
	// en6: 75495898 rx, 75521020 tx
	// en2: 0 rx, 1936 tx
	// Sum rx = 340554879626, sum tx = 343898291434
	extraContent := loadFixture(t, "extra-probe-ioreg-netstat-defaults.txt")
	extraSec := SplitFixtureSections(extraContent)
	rxB, txB := ParseDarwinNetstatTotals(extraSec["netstat_ibn"])
	const expectedRxB = int64(340554879626)
	const expectedTxB = int64(343898291434)
	if rxB != expectedRxB || txB != expectedTxB {
		t.Errorf("macmini-B Netstat totals: expected rx=%d tx=%d, got rx=%d tx=%d", expectedRxB, expectedTxB, rxB, txB)
	}
}

func TestDarwin_NetworkTotals_BridgeMembersExcluded(t *testing.T) {
	// Real-host fixture with Thunderbolt Bridge (bridge0) and members (en2, en3, en4, en5, en6, en7).
	// bridge0: rx=2151472379900, tx=5949384788484
	// en4: rx=0, tx=3103960642697 (must be EXCLUDED)
	// en5: rx=0, tx=32160374576 (must be EXCLUDED)
	// en2, en3, en6, en7: rx=0, tx=0
	// en10..17: rx=63300000, tx=75960000
	// Expected:
	// rx = 2151472379900 + 63300000 = 2151535679900
	// tx = 5949384788484 + 75960000 = 5949460748484
	content := loadFixture(t, "netstat-bridge-members.txt")
	sec := SplitFixtureSections(content)

	members := ParseDarwinBridgeMembers(sec["bridge_members"])
	if len(members) != 6 {
		t.Fatalf("expected 6 bridge members, got %d: %v", len(members), members)
	}

	rx, tx := ParseDarwinNetstatTotals(sec["netstat_ibn"], members...)
	const expectedRx = int64(2151535679900)
	const expectedTx = int64(5949460748484)
	if rx != expectedRx {
		t.Errorf("Bridge network totals rx: expected %d, got %d", expectedRx, rx)
	}
	if tx != expectedTx {
		t.Errorf("Bridge network totals tx: expected %d, got %d", expectedTx, tx)
	}

	// Also test the shell awk fragment with the bridge members passed
	awkCmd := `awk -v bm=" en2 en3 en4 en5 en6 en7 " 'BEGIN {rx=0; tx=0} ($1 ~ /^en[0-9]+/ || $1 ~ /^bridge[0-9]+/) && $3 ~ /^<Link#/ {name=$1; sub(/\*$/, "", name); if (index(bm, " " name " ") == 0) {rx+=$(NF-4); tx+=$(NF-1)}} END {printf "%.0f %.0f\n", (rx?rx:0), (tx?tx:0)}'`
	cmd := exec.Command("sh", "-c", awkCmd)
	cmd.Stdin = strings.NewReader(sec["netstat_ibn"])
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("run bridge netstat awk fragment: %v", err)
	}
	fields := strings.Fields(out.String())
	if len(fields) != 2 || fields[0] != "2151535679900" || fields[1] != "5949460748484" {
		t.Errorf("unexpected bridge awk output: %q", out.String())
	}
}

func TestDarwin_ZshCompatibility(t *testing.T) {
	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed on local host, skipping zsh compatibility test")
	}

	content := loadFixture(t, "netstat-bridge-members.txt")
	sec := SplitFixtureSections(content)

	// In zsh, unquoted variable expansion does not word-split.
	// This test verifies our `tr '\n' ' '` pipeline produces clean, single-line
	// arguments to awk without any "newline in string" errors under zsh.
	script := `
bm=$(printf "en2\nen3\nen4\nen5\nen6\nen7\n" | tr '\n' ' ')
awk -v bm=" $bm " 'BEGIN {rx=0; tx=0} ($1 ~ /^en[0-9]+/ || $1 ~ /^bridge[0-9]+/) && $3 ~ /^<Link#/ {name=$1; sub(/\*$/, "", name); if (index(bm, " " name " ") == 0) {rx+=$(NF-4); tx+=$(NF-1)}} END {printf "%.0f %.0f\n", (rx?rx:0), (tx?tx:0)}'
`

	// Run under /bin/sh
	shCmd := exec.Command("sh", "-c", script)
	shCmd.Stdin = strings.NewReader(sec["netstat_ibn"])
	var shOut, shErr bytes.Buffer
	shCmd.Stdout = &shOut
	shCmd.Stderr = &shErr
	if err := shCmd.Run(); err != nil {
		t.Fatalf("sh failed: %v, stderr: %s", err, shErr.String())
	}

	// Run under zsh
	zshCmd := exec.Command(zshPath, "-c", script)
	zshCmd.Stdin = strings.NewReader(sec["netstat_ibn"])
	var zshOut, zshErr bytes.Buffer
	zshCmd.Stdout = &zshOut
	zshCmd.Stderr = &zshErr
	if err := zshCmd.Run(); err != nil {
		t.Fatalf("zsh failed: %v, stderr: %s", err, zshErr.String())
	}

	if shOut.String() != zshOut.String() {
		t.Errorf("sh and zsh outputs differ:\nsh:  %q\nzsh: %q", shOut.String(), zshOut.String())
	}

	fields := strings.Fields(zshOut.String())
	if len(fields) != 2 || fields[0] != "2151535679900" || fields[1] != "5949460748484" {
		t.Errorf("unexpected zsh output: %q", zshOut.String())
	}
}

func TestDarwin_IOStat_IOReg(t *testing.T) {
	extraContent := loadFixture(t, "extra-probe-ioreg-netstat-defaults.txt")
	extraSec := SplitFixtureSections(extraContent)

	// macmini-B ioreg_stats:
	// Bytes (Read)=94217433088, Bytes (Write)=233157414912
	readBytes, writeBytes := ParseDarwinIOStat(extraSec["ioreg_stats"])
	const expectedRead = int64(94217433088)
	const expectedWrite = int64(233157414912)
	if readBytes != expectedRead || writeBytes != expectedWrite {
		t.Errorf("ioreg sum: expected read=%d write=%d, got read=%d write=%d", expectedRead, expectedWrite, readBytes, writeBytes)
	}
}

func TestDarwin_CPUCores(t *testing.T) {
	miniContent := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	miniSec := SplitFixtureSections(miniContent)
	sysctlMini := strings.Split(miniSec["sysctl_basic"], "\n")
	coresMini, _ := strconv.Atoi(strings.TrimSpace(sysctlMini[1]))
	if coresMini != 12 {
		t.Errorf("Mac mini cores: expected 12, got %d", coresMini)
	}

	studioContent := loadFixture(t, "macstudio-m3ultra-256gb.txt")
	studioSec := SplitFixtureSections(studioContent)
	sysctlStudio := strings.Split(studioSec["sysctl_basic"], "\n")
	coresStudio, _ := strconv.Atoi(strings.TrimSpace(sysctlStudio[1]))
	if coresStudio != 32 {
		t.Errorf("Mac Studio cores: expected 32, got %d", coresStudio)
	}
}

func TestDarwin_HardwareModel(t *testing.T) {
	miniContent := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	miniSec := SplitFixtureSections(miniContent)
	modelMini := ParseDarwinHardwareModel(miniSec["sysprof_time"], "Mac16,11", "Apple M4 Pro")
	const expectedMiniModel = "Apple Mac mini (Mac16,11) - Apple M4 Pro"
	if modelMini != expectedMiniModel {
		t.Errorf("Mac mini model: expected %q, got %q", expectedMiniModel, modelMini)
	}

	// Fallback to sysctl hw.model + machdep.cpu.brand_string
	fallbackStudio := ParseDarwinHardwareModel("", "Mac15,14", "Apple M3 Ultra")
	const expectedStudioModel = "Apple Mac15,14 - Apple M3 Ultra"
	if fallbackStudio != expectedStudioModel {
		t.Errorf("Mac Studio fallback model: expected %q, got %q", expectedStudioModel, fallbackStudio)
	}
}

func TestDarwin_OSInstallEpoch(t *testing.T) {
	miniContent := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	miniSec := SplitFixtureSections(miniContent)
	statBirthLines := strings.Split(miniSec["stat_birth"], "\n")
	epochMini, _ := strconv.ParseInt(strings.Fields(statBirthLines[0])[0], 10, 64)
	if epochMini != 1754405985 {
		t.Errorf("Mac mini install epoch: expected 1754405985, got %d", epochMini)
	}

	extraContent := loadFixture(t, "extra-probe-ioreg-netstat-defaults.txt")
	extraSec := SplitFixtureSections(extraContent)
	osInstallLines := strings.Split(extraSec["os_install"], "\n")
	epochStudio, _ := strconv.ParseInt(strings.TrimSpace(osInstallLines[0]), 10, 64)
	if epochStudio != 1792054964 && epochStudio != 1754405985 {
		t.Errorf("Unexpected install epoch: %d", epochStudio)
	}
}

func TestDarwin_OSInfo_Parsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Synthetic os-release Tahoe 26.5.1",
			input: `NAME="macOS"
VERSION_ID="26.5.1"
PRETTY_NAME="macOS 26.5.1 (Tahoe)"
ID=macos`,
			expected: "macOS 26.5.1 (Tahoe)",
		},
		{
			name: "Synthetic os-release Tahoe 26.2 without parens",
			input: `NAME="macOS"
VERSION_ID="26.2"
PRETTY_NAME="macOS 26.2"
ID=macos`,
			expected: "macOS 26.2 (Tahoe)",
		},
		{
			name: "sw_vers output format",
			input: `ProductName:	macOS
ProductVersion:	26.2
BuildVersion:	25C56`,
			expected: "macOS 26.2 (Tahoe)",
		},
		{
			name: "macOS Sequoia",
			input: `NAME="macOS"
VERSION_ID="15.4"
ID=macos`,
			expected: "macOS 15.4 (Sequoia)",
		},
		{
			name: "macOS Sonoma",
			input: `NAME="macOS"
VERSION_ID="14.7"
ID=macos`,
			expected: "macOS 14.7 (Sonoma)",
		},
		{
			name: "macOS Ventura",
			input: `NAME="macOS"
VERSION_ID="13.6"
ID=macos`,
			expected: "macOS 13.6 (Ventura)",
		},
		{
			name: "Untouched Linux Ubuntu",
			input: `NAME="Ubuntu"
VERSION="22.04.4 LTS (Jammy Jellyfish)"
ID=ubuntu
PRETTY_NAME="Ubuntu 22.04.4 LTS"`,
			expected: "Ubuntu 22.04.4 LTS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOSInfo(tc.input)
			if got != tc.expected {
				t.Errorf("parseOSInfo: expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestDarwin_Uptime_Shapes(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectedUp string
		expectedLd string
	}{
		{
			name:       "macOS shape 1: days and HH:MM (Mac mini)",
			input:      "13:47  up 32 days, 17:07, 1 user, load averages: 1.04 1.21 1.52",
			expectedUp: "32 days, 17:07",
			expectedLd: "1.04, 1.21, 1.52",
		},
		{
			name:       "macOS shape 1: days and HH:MM (Mac Studio)",
			input:      "13:46  up 13 days, 20:09, 1 user, load averages: 1.83 1.61 1.53",
			expectedUp: "13 days, 20:09",
			expectedLd: "1.83, 1.61, 1.53",
		},
		{
			name:       "macOS shape 2: hrs",
			input:      "10:00  up 3 hrs, 1 user, load averages: 0.50 0.40 0.30",
			expectedUp: "3 hrs",
			expectedLd: "0.50, 0.40, 0.30",
		},
		{
			name:       "macOS shape 3: mins",
			input:      "10:00  up 45 mins, 1 user, load averages: 0.10 0.20 0.30",
			expectedUp: "45 mins",
			expectedLd: "0.10, 0.20, 0.30",
		},
		{
			name:       "macOS shape 4: 1 day and HH:MM",
			input:      "10:00  up 1 day, 2:03, 1 user, load averages: 0.15 0.25 0.35",
			expectedUp: "1 day, 2:03",
			expectedLd: "0.15, 0.25, 0.35",
		},
		{
			name:       "Linux uptime stays byte-for-byte unchanged",
			input:      " 14:00:01 up 12 days,  3:15,  1 user,  load average: 0.15, 0.08, 0.05",
			expectedUp: "12 days, 3:15",
			expectedLd: "0.15, 0.08, 0.05",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			up, ld := parseUptime(tc.input)
			if up != tc.expectedUp {
				t.Errorf("Uptime: expected %q, got %q", tc.expectedUp, up)
			}
			if ld != tc.expectedLd {
				t.Errorf("Load: expected %q, got %q", tc.expectedLd, ld)
			}
		})
	}
}

func TestDarwin_TopProcesses_Ucomm(t *testing.T) {
	content := loadFixture(t, "extra-probe-ioreg-netstat-defaults.txt")
	sec := SplitFixtureSections(content)

	// The extra-probe fixture holds two hosts back to back; the last section per name wins (the Mac mini).
	procs := parseTopProcesses(sec["ps_ucomm"])
	if len(procs) < 3 {
		t.Fatalf("expected at least 3 processes, got %d", len(procs))
	}

	if procs[0].Command != "sshd-session" || procs[0].PID != 61335 || procs[0].User != "root" {
		t.Errorf("unexpected first process: %+v", procs[0])
	}
	if procs[0].RSSBytes != 16304*1024 {
		t.Errorf("expected RSS bytes %d, got %d", 16304*1024, procs[0].RSSBytes)
	}
	// ucomm is a bare process name: never a path or a command line (macOS `comm` leaked tokens in arguments).
	for _, p := range procs {
		if strings.ContainsAny(p.Command, " /") && p.Command != "" {
			t.Errorf("process name must not contain a path or arguments: %q", p.Command)
		}
	}
	if procs[1].Command != "WindowServer" {
		t.Errorf("expected command 'WindowServer', got %q", procs[1].Command)
	}
}

func TestDarwin_LifecycleEOL(t *testing.T) {
	tests := []struct {
		osInfo         string
		expectedEOL    bool
		expectedDeduct int
	}{
		{"macOS 26.5.1 (Tahoe)", false, 0},
		{"macOS 26.2 (Tahoe)", false, 0},
		{"macOS 15.4 (Sequoia)", false, 0},
		{"macOS 14.7 (Sonoma)", false, 0},
		{"macOS 13.6 (Ventura)", true, -30},
		{"macOS 12.7 (Monterey)", true, -30},
		{"macOS 11.7 (Big Sur)", true, -30},
	}

	for _, tc := range tests {
		t.Run(tc.osInfo, func(t *testing.T) {
			isEOL, _ := isMacOSEOL(tc.osInfo)
			if isEOL != tc.expectedEOL {
				t.Errorf("isMacOSEOL(%q): expected %v, got %v", tc.osInfo, tc.expectedEOL, isEOL)
			}

			score, _, _ := calculateLifecycleScore(tc.osInfo, "0.10, 0.10, 0.10", 8, 1024, 8192, 1024, 8192, 0, "", "Apple Mac", 0, "")
			if tc.expectedEOL {
				if score > 70 {
					t.Errorf("expected EOL score <= 70, got %d", score)
				}
			} else {
				if score != 100 {
					t.Errorf("expected supported score 100, got %d", score)
				}
			}
		})
	}
}

func TestDarwin_LifecycleScore_MacMini_SwapPressure(t *testing.T) {
	// A 24 GB Mac with 6 GB swap used and ~8 GB RAM used:
	// RAM used = 8,071,593,984 bytes out of 25,769,803,776 bytes (31.3% RAM utilized)
	// Should score 100 sensibly without spurious penalty.
	ramTotal := int64(25769803776)
	ramUsed := int64(8071593984)
	diskTotal := int64(494384795648)
	diskUsed := int64(190641844224)
	epochMini := int64(1754405985)

	score, notes, _ := calculateLifecycleScore("macOS 26.2 (Tahoe)", "1.04, 1.21, 1.52", 12, ramUsed, ramTotal, diskUsed, diskTotal, 0, "", "Apple Mac mini (Mac16,11) - Apple M4 Pro", epochMini, "")
	if score != 100 {
		t.Fatalf("expected 100 lifecycle score for Mac mini, got %d (notes: %s)", score, notes)
	}
}

func TestDarwin_LinuxBatchCommandUntouched(t *testing.T) {
	// 1. Must start with the Darwin guard prefix
	if !strings.HasPrefix(SystemMetricsBatchCmd, "if [ \"$(uname -s)\" = \"Darwin\" ]; then ") {
		t.Fatalf("SystemMetricsBatchCmd does not have Darwin guard prefix")
	}

	// 2. Must contain LinuxSystemMetricsBatchCmd byte-for-byte
	if !strings.Contains(SystemMetricsBatchCmd, LinuxSystemMetricsBatchCmd) {
		t.Fatalf("SystemMetricsBatchCmd does not contain LinuxSystemMetricsBatchCmd")
	}

	// 3. Substring checks of original Linux commands
	requiredLinuxSnippets := []string{
		"cat /etc/os-release 2>/dev/null",
		"free -b 2>/dev/null",
		"timeout -k 2s 5s df -lPk / 2>/dev/null",
		"cat /proc/net/dev 2>/dev/null",
		"ping -c 1 -W 2 1.1.1.1 2>/dev/null",
		"ss -nt state established 2>/dev/null",
		"ss -tlpn 2>/dev/null",
		"/proc/cpuinfo",
		"/sys/class/dmi/id/bios_date",
		"/proc/diskstats",
		"ps -eo pid,user,%cpu,%mem,rss,comm --sort=-%cpu",
		"/proc/meminfo",
	}

	for _, snippet := range requiredLinuxSnippets {
		if !strings.Contains(LinuxSystemMetricsBatchCmd, snippet) {
			t.Errorf("LinuxSystemMetricsBatchCmd missing original snippet: %q", snippet)
		}
	}
}

func TestDarwin_AwkFragmentsThroughSh(t *testing.T) {
	// 1. Test ioreg awk fragment through sh
	ioregAwk := `awk 'BEGIN {r=0; w=0} /"Bytes \(Read\)"/ {if (match($0, /"Bytes \(Read\)"=[0-9]+/)) {s=substr($0, RSTART, RLENGTH); sub(/.*=/, "", s); r+=s+0}} /"Bytes \(Write\)"/ {if (match($0, /"Bytes \(Write\)"=[0-9]+/)) {s=substr($0, RSTART, RLENGTH); sub(/.*=/, "", s); w+=s+0}} END {printf "%.0f %.0f\n", r, w}'`
	extraContent := loadFixture(t, "extra-probe-ioreg-netstat-defaults.txt")
	extraSec := SplitFixtureSections(extraContent)

	cmd := exec.Command("sh", "-c", ioregAwk)
	cmd.Stdin = strings.NewReader(extraSec["ioreg_stats"])
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("run ioreg awk fragment: %v", err)
	}

	fields := strings.Fields(out.String())
	if len(fields) != 2 || fields[0] != "94217433088" || fields[1] != "233157414912" {
		t.Errorf("unexpected ioreg awk output: %q", out.String())
	}

	// 2. Test netstat awk fragment through sh
	netstatAwk := `awk '$1 ~ /^en[0-9]+/ && $3 ~ /^<Link#/ {rx+=$(NF-4); tx+=$(NF-1)} END {printf "%.0f %.0f\n", (rx?rx:0), (tx?tx:0)}'`
	cmd = exec.Command("sh", "-c", netstatAwk)
	cmd.Stdin = strings.NewReader(extraSec["netstat_ibn"])
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("run netstat awk fragment: %v", err)
	}

	fields = strings.Fields(out.String())
	if len(fields) != 2 || fields[0] != "340554879626" || fields[1] != "343898291434" {
		t.Errorf("unexpected netstat awk output: %q", out.String())
	}

	// 3. Test df awk fragment through sh
	dfAwk := `awk 'NR>1 {tot=$2; avail=$4; used=tot-avail; if (used<0) used=$3; printf "%s %s %s %s %s /\n", $1, tot, used, avail, $5; exit}'`
	miniContent := loadFixture(t, "macmini-m4pro-24gb-swap.txt")
	miniSec := SplitFixtureSections(miniContent)
	cmd = exec.Command("sh", "-c", dfAwk)
	cmd.Stdin = strings.NewReader(miniSec["df_data"])
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("run df awk fragment: %v", err)
	}

	fields = strings.Fields(out.String())
	if len(fields) < 6 || fields[1] != "482797652" || fields[2] != "186173676" || fields[3] != "296623976" {
		t.Errorf("unexpected df awk output: %q", out.String())
	}
}

func TestDarwin_FullMetricsScrapeSimulation(t *testing.T) {
	// Simulate scraping a Darwin host using the 20-section format
	batchStdout := strings.Join([]string{
		"25.2.0", // 0: kernel
		"NAME=\"macOS\"\nVERSION_ID=\"26.2\"\nPRETTY_NAME=\"macOS 26.2 (Tahoe)\"\nID=macos", // 1: os-release
		"13:47  up 32 days, 17:07, 1 user, load averages: 1.04 1.21 1.52",                   // 2: uptime
		"Mem: 25769803776 8071593984 0\nSwap: 7516192768 6327432643 0",                      // 3: free
		"/dev/disk3s5 482797652 186173676 296623976 35% /",                                  // 4: df
		"0",                         // 5: ioErrors
		"340465305228 343818145131", // 6: net rx tx
		"2.049",                     // 7: ping ms
		"203.0.113.42",              // 8: public IP
		"192.168.88.6:22 192.168.88.1:52503\n192.168.88.6:64605 10.0.0.1:443", // 9: established
		"tcp *:22\ntcp *:40014", // 10: listening
		"0",                     // 11: failed logins
		"12",                    // 12: cores
		"",                      // 13: bios date
		"Apple Mac mini (Mac16,11) - Apple M4 Pro", // 14: hardware model
		"1754405985",               // 15: install epoch
		"94217433088 233157414912", // 16: disk IO
		"157 _windowserver 12.5 0.0 67344 WindowServer\n90358 root 12.2 0.0 20688 sshd-session", // 17: top CPU
		"21875 macuser 3.1 0.2 422928 Python\n73969 macuser 0.0 0.1 389088 Python",              // 18: top RAM
		"", // 19: top swap
	}, "\n---\n")

	sections := splitMetricsSections(batchStdout)
	if len(sections) != 20 {
		t.Fatalf("expected 20 sections, got %d", len(sections))
	}

	h := &store.Host{}
	h.Kernel = strings.TrimSpace(sections[0])
	h.OSInfo = parseOSInfo(sections[1])
	h.Uptime, h.CPULoad = parseUptime(strings.TrimSpace(sections[2]))
	h.RAMUsedBytes, h.RAMTotalBytes, h.SwapUsedBytes, h.SwapTotalBytes = parseFree(sections[3])
	h.DiskUsedBytes, h.DiskTotalBytes = parseDf(sections[4])
	cpuCores, _ := strconv.Atoi(strings.TrimSpace(sections[12]))
	h.CPUCores = cpuCores
	h.HardwareModel = strings.TrimSpace(sections[14])
	h.BIOSDate = strings.TrimSpace(sections[13])
	h.OSInstallEpoch, _ = strconv.ParseInt(strings.TrimSpace(sections[15]), 10, 64)

	parseNetworkMetrics(h, sections[6], sections[7], sections[8], sections[9], sections[10], sections[11])
	parseDiskIOMetrics(h, sections[16])

	score, notes, _ := calculateLifecycleScore(h.OSInfo, h.CPULoad, h.CPUCores, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, 0, h.BIOSDate, h.HardwareModel, h.OSInstallEpoch, "")
	h.LifecycleScore = score
	h.LifecycleNotes = notes

	if h.Kernel != "25.2.0" {
		t.Errorf("Kernel: expected 25.2.0, got %q", h.Kernel)
	}
	if h.OSInfo != "macOS 26.2 (Tahoe)" {
		t.Errorf("OSInfo: expected 'macOS 26.2 (Tahoe)', got %q", h.OSInfo)
	}
	if h.Uptime != "32 days, 17:07" {
		t.Errorf("Uptime: expected '32 days, 17:07', got %q", h.Uptime)
	}
	if h.CPULoad != "1.04, 1.21, 1.52" {
		t.Errorf("CPULoad: expected '1.04, 1.21, 1.52', got %q", h.CPULoad)
	}
	if h.RAMUsedBytes != 8071593984 || h.RAMTotalBytes != 25769803776 {
		t.Errorf("RAM: expected 8071593984/25769803776, got %d/%d", h.RAMUsedBytes, h.RAMTotalBytes)
	}
	if h.SwapUsedBytes != 6327432643 || h.SwapTotalBytes != 7516192768 {
		t.Errorf("Swap: expected 6327432643/7516192768, got %d/%d", h.SwapUsedBytes, h.SwapTotalBytes)
	}
	if h.DiskUsedBytes != 190641844224 || h.DiskTotalBytes != 494384795648 {
		t.Errorf("Disk: expected 190641844224/494384795648, got %d/%d", h.DiskUsedBytes, h.DiskTotalBytes)
	}
	if h.CPUCores != 12 {
		t.Errorf("CPUCores: expected 12, got %d", h.CPUCores)
	}
	if h.HardwareModel != "Apple Mac mini (Mac16,11) - Apple M4 Pro" {
		t.Errorf("HardwareModel: expected 'Apple Mac mini (Mac16,11) - Apple M4 Pro', got %q", h.HardwareModel)
	}
	if h.NetRxBytes != 340465305228 || h.NetTxBytes != 343818145131 {
		t.Errorf("Net: expected 340465305228/343818145131, got %d/%d", h.NetRxBytes, h.NetTxBytes)
	}
	if !h.InternetOnline || h.InternetLatencyMs != 2 {
		t.Errorf("Internet: expected online=true, latency=2, got online=%v, latency=%d", h.InternetOnline, h.InternetLatencyMs)
	}
	if h.ActiveConnCount != 2 {
		t.Errorf("ActiveConnCount: expected 2, got %d", h.ActiveConnCount)
	}
	if h.DiskReadBytes != 94217433088 || h.DiskWriteBytes != 233157414912 {
		t.Errorf("DiskIO: expected 94217433088/233157414912, got %d/%d", h.DiskReadBytes, h.DiskWriteBytes)
	}
	if h.LifecycleScore != 100 {
		t.Errorf("LifecycleScore: expected 100, got %d (notes: %s)", h.LifecycleScore, h.LifecycleNotes)
	}
}
