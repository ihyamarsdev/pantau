package inspector

// System metrics collection: the batched remote command and the section split
// that turns its output into host metrics.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// SystemMetricsBatchCmd is the single-shot batch command to gather system, network, and security metrics.
// ponytail: Universal polyglot batching keeps agentless overhead near zero on modern & legacy Linux.
// ponytail: Uses df -lPk with timeout fallback to guard against hung NFS / network storage.
const SystemMetricsBatchCmd = "if [ \"$(uname -s)\" = \"Darwin\" ]; then " + DarwinSystemMetricsBatchCmd + " ; else " + LinuxSystemMetricsBatchCmd + " ; fi"

func splitMetricsSections(stdout string) []string {
	lines := strings.Split(stdout, "\n")
	var sections []string
	var cur strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			sections = append(sections, cur.String())
			cur.Reset()
		} else {
			if cur.Len() > 0 {
				cur.WriteByte('\n')
			}
			cur.WriteString(line)
		}
	}
	sections = append(sections, cur.String())
	return sections
}

func (ins *Inspector) scrapeSystemMetrics(h *store.Host, runner sshrunner.Runner) error {
	stdout, _, _, err := runner.Exec(SystemMetricsBatchCmd)
	if err != nil {
		return fmt.Errorf("exec metrics batch: %w", err)
	}

	sections := splitMetricsSections(stdout)
	if len(sections) >= 1 {
		h.Kernel = strings.TrimSpace(sections[0])
	}
	if len(sections) >= 2 {
		h.OSInfo = parseOSInfo(sections[1])
	}
	if len(sections) >= 3 {
		uptimeOut := strings.TrimSpace(sections[2])
		h.Uptime, h.CPULoad = parseUptime(uptimeOut)
	}
	if len(sections) >= 4 {
		h.RAMUsedBytes, h.RAMTotalBytes, h.SwapUsedBytes, h.SwapTotalBytes = parseFree(sections[3])
	}
	if len(sections) >= 5 {
		h.DiskUsedBytes, h.DiskTotalBytes = parseDf(sections[4])
	}

	ioErrors := 0
	if len(sections) >= 6 {
		ioErrors, _ = strconv.Atoi(strings.TrimSpace(sections[5]))
	}

	cpuCores := 1
	if len(sections) >= 13 {
		if c, err := strconv.Atoi(strings.TrimSpace(sections[12])); err == nil && c > 0 {
			cpuCores = c
		}
	}
	h.CPUCores = cpuCores

	biosDateStr := ""
	if len(sections) >= 14 {
		biosDateStr = strings.TrimSpace(sections[13])
	}
	vendorStr := ""
	if len(sections) >= 15 {
		vendorStr = strings.TrimSpace(sections[14])
	}
	h.HardwareModel = vendorStr
	h.BIOSDate = biosDateStr
	osInstallEpoch := int64(0)
	if len(sections) >= 16 {
		osInstallEpoch, _ = strconv.ParseInt(strings.TrimSpace(sections[15]), 10, 64)
	}
	h.OSInstallEpoch = osInstallEpoch

	// Calculate Lifecycle Score (0-100) and transparent breakdown
	score, notes, breakdownJSON := calculateLifecycleScore(h.OSInfo, h.CPULoad, cpuCores, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, ioErrors, biosDateStr, vendorStr, osInstallEpoch, h.CommissionDate)
	h.LifecycleScore = score
	h.LifecycleNotes = notes
	h.LifecycleBreakdown = breakdownJSON

	// Parse Network & Security Observability Metrics
	var netDevSec, pingSec, pubIPSec, estSec, listenSec, failedSec string
	if len(sections) >= 7 {
		netDevSec = sections[6]
	}
	if len(sections) >= 8 {
		pingSec = sections[7]
	}
	if len(sections) >= 9 {
		pubIPSec = sections[8]
	}
	if len(sections) >= 10 {
		estSec = sections[9]
	}
	if len(sections) >= 11 {
		listenSec = sections[10]
	}
	if len(sections) >= 12 {
		failedSec = sections[11]
	}
	parseNetworkMetrics(h, netDevSec, pingSec, pubIPSec, estSec, listenSec, failedSec)
	ins.enrichListeningProcesses(h, runner)

	// Private IPv4 addresses (RFC 1918 + CGNAT), used as inter-host probe targets over a private network.
	if len(sections) >= 21 {
		h.PrivateIPs = parsePrivateIPs(sections[20])
	}

	var diskStatsSec string
	if len(sections) >= 17 {
		diskStatsSec = sections[16]
	}
	parseDiskIOMetrics(h, diskStatsSec)

	// Process Telemetry: Top CPU, Top RAM, Top Swap
	if len(sections) >= 18 {
		topCPU := sortAndTrimTopProcesses(parseTopProcesses(sections[17]), "cpu")
		if b, err := json.Marshal(topCPU); err == nil {
			h.TopCPUProcesses = string(b)
		}
	}
	if len(sections) >= 19 {
		topRAM := sortAndTrimTopProcesses(parseTopProcesses(sections[18]), "ram")
		if b, err := json.Marshal(topRAM); err == nil {
			h.TopRAMProcesses = string(b)
		}
	}
	if len(sections) >= 20 {
		topSwap := sortAndTrimTopProcesses(parseTopSwap(sections[19]), "swap")
		if b, err := json.Marshal(topSwap); err == nil {
			h.TopSwapProcesses = string(b)
		}
	}

	return nil
}

var ssProcessNameRe = regexp.MustCompile(`\("([^"]+)"`)

// extractListenProcess returns the owning process name(s) of a listening socket from the trailing
// columns of `ss -tlpn` (users:(("nginx",pid=12,fd=6),...)) or `netstat -tlpn` (12/nginx).
// Returns "" when the SSH user lacks permission to see the owner (no -p information).
func extractListenProcess(cols []string) string {
	for _, c := range cols {
		if strings.HasPrefix(c, "users:") {
			var names []string
			seen := map[string]bool{}
			for _, m := range ssProcessNameRe.FindAllStringSubmatch(c, -1) {
				if !seen[m[1]] {
					seen[m[1]] = true
					names = append(names, m[1])
				}
			}
			return strings.Join(names, ", ")
		}
		if i := strings.Index(c, "/"); i > 0 {
			if _, err := strconv.Atoi(c[:i]); err == nil {
				return c[i+1:]
			}
		}
	}
	return ""
}
