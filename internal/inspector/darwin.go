package inspector

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// LinuxSystemMetricsBatchCmd is the untouched, byte-for-byte original batch metrics command
// executed on Linux hosts to maintain 100% backward compatibility.
const LinuxSystemMetricsBatchCmd = `uname -r; echo "---"; cat /etc/os-release 2>/dev/null || cat /usr/lib/os-release 2>/dev/null || cat /etc/redhat-release 2>/dev/null || cat /etc/centos-release 2>/dev/null || cat /etc/issue 2>/dev/null; echo "---"; uptime; echo "---"; free -b 2>/dev/null; echo "---"; (timeout -k 2s 5s df -lPk / 2>/dev/null || df -lPk / 2>/dev/null); echo "---"; (dmesg 2>/dev/null || cat /var/log/dmesg 2>/dev/null) | grep -iE 'I/O error|EXT4-fs error|BTRFS error' | wc -l; echo "---"; cat /proc/net/dev 2>/dev/null | grep -vE 'lo|Inter-|face' | awk '{rx+=$2; tx+=$10} END {print rx, tx}'; echo "---"; (ping -c 1 -W 2 1.1.1.1 2>/dev/null | grep -oE 'time=[0-9.]+' | cut -d= -f2) || echo "OFFLINE"; echo "---"; curl -s --connect-timeout 2 -m 4 https://icanhazip.com 2>/dev/null || curl -s --connect-timeout 2 -m 4 https://ifconfig.me 2>/dev/null || echo ""; echo "---"; (ss -nt state established 2>/dev/null || ss -nt 2>/dev/null || netstat -nt 2>/dev/null) | awk '!/Recv-Q|Proto|Active/ {if (NF>=5) print $4, $5; else if (NF>=4) print $3, $4}' | head -n 50; echo "---"; (ss -tlpn 2>/dev/null || netstat -tlpn 2>/dev/null) | awk '!/State|Proto|Active/ {print $1, $4, $6}' | head -n 30; echo "---"; (grep -i "Failed password" /var/log/auth.log 2>/dev/null || grep -i "Failed password" /var/log/secure 2>/dev/null || true) | wc -l; echo "---"; (nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1); echo "---"; (cat /sys/class/dmi/id/bios_date 2>/dev/null || echo ""); echo "---"; (cat /sys/class/dmi/id/sys_vendor 2>/dev/null || cat /sys/class/dmi/id/product_name 2>/dev/null || echo ""); echo "---"; (stat -c %Y /etc/machine-id 2>/dev/null || stat -c %Y /etc/ssh/ssh_host_rsa_key 2>/dev/null || stat -c %Y /var/log 2>/dev/null || echo 0); echo "---"; cat /proc/diskstats 2>/dev/null | awk '{if ($3 ~ /^([hsv]d[a-z]|nvme[0-9]+n[0-9]+|xvd[a-z]|mmcblk[0-9]+)$/) {r+=$6; w+=$10}} END {print (r?r:0)*512, (w?w:0)*512}'; echo "---"; (ps -eo pid,user,%cpu,%mem,rss,comm --sort=-%cpu 2>/dev/null || ps -eo pid,user,%cpu,%mem,rss,args 2>/dev/null) | head -n 6; echo "---"; (ps -eo pid,user,%cpu,%mem,rss,comm --sort=-rss 2>/dev/null || ps -eo pid,user,%cpu,%mem,rss,args 2>/dev/null) | head -n 6; echo "---"; ([ $(awk '/SwapTotal/ {t=$2} /SwapFree/ {f=$2} END {print (t?t:0)-(f?f:0)}' /proc/meminfo 2>/dev/null || echo 0) -gt 0 ] && grep -s -H -E '^(VmSwap|Name|Uid):' /proc/[0-9]*/status 2>/dev/null | awk -F'[: \t]+' '{if ($2=="VmSwap" && $3>0) {split($1,a,"/"); print a[3], (uid[a[3]]?uid[a[3]]:"0"), $3, name[a[3]]} else if ($2=="Name") {split($1,a,"/"); n=$0; sub(/^.+:Name:[ \t]+/, "", n); name[a[3]]=n} else if ($2=="Uid") {split($1,a,"/"); uid[a[3]]=$3}}' | sort -k3 -rn | head -n 5) || true`

// DarwinSystemMetricsBatchCmd is the Darwin/macOS single-shot metrics batching command.
// Emits 20 sections matching the existing SystemMetricsBatchCmd layout.
const DarwinSystemMetricsBatchCmd = `uname -r; echo "---"; v=$(sw_vers -productVersion 2>/dev/null || sysctl -n kern.osproductversion 2>/dev/null || echo ""); m=$(echo "$v" | cut -d. -f1); case "$m" in 26) c="Tahoe";; 15) c="Sequoia";; 14) c="Sonoma";; 13) c="Ventura";; 12) c="Monterey";; 11) c="Big Sur";; *) c="";; esac; if [ -n "$c" ]; then p="macOS $v ($c)"; else p="macOS $v"; fi; echo "NAME=\"macOS\""; echo "VERSION_ID=\"$v\""; echo "PRETTY_NAME=\"$p\""; echo "ID=macos"; echo "---"; uptime; echo "---"; { vm_stat 2>/dev/null; sysctl vm.swapusage 2>/dev/null; } | awk -v tm="$(sysctl -n hw.memsize 2>/dev/null || echo 0)" 'BEGIN { ps = 4096; st = 0; su = 0 } /page size of/ { for (i=1; i<=NF; i++) if ($i ~ /^[0-9]+$/) ps = $i + 0 } /Anonymous pages:/ { anon = $NF; gsub(/[^0-9]/, "", anon) } /Pages purgeable:/ { purg = $NF; gsub(/[^0-9]/, "", purg) } /Pages wired down:/ { wired = $NF; gsub(/[^0-9]/, "", wired) } /Pages occupied by compressor:/ { comp = $NF; gsub(/[^0-9]/, "", comp) } /total =/ { for (i=1; i<=NF; i++) { if ($i == "total") { v = $(i+2); sub(/,/, "", v); st = pbytes(v) } if ($i == "used") { v = $(i+2); sub(/,/, "", v); su = pbytes(v) } } } function pbytes(s, unit, num) { unit = substr(s, length(s), 1); num = substr(s, 1, length(s)-1) + 0; if (unit == "G" || unit == "g") return num * 1073741824; if (unit == "M" || unit == "m") return num * 1048576; if (unit == "K" || unit == "k") return num * 1024; return (s + 0) } END { used = (anon - purg + wired + comp) * ps; printf "Mem: %s %.0f 0\n", (tm ? tm : 0), used; printf "Swap: %.0f %.0f 0\n", st, su }'; echo "---"; (perl -e 'alarm 3; exec @ARGV' df -lPk /System/Volumes/Data 2>/dev/null || df -lPk /System/Volumes/Data 2>/dev/null || df -lPk / 2>/dev/null) | awk 'NR>1 {tot=$2; avail=$4; used=tot-avail; if (used<0) used=$3; printf "%s %s %s %s %s /\n", $1, tot, used, avail, $5; exit}'; echo "---"; echo "0"; echo "---"; bm=$(for b in $(ifconfig -l 2>/dev/null); do case "$b" in bridge*) ifconfig "$b" 2>/dev/null | awk '/member:/ {print $2}';; esac; done | tr '\n' ' '); netstat -ibn 2>/dev/null | awk -v bm=" $bm " 'BEGIN {rx=0; tx=0} ($1 ~ /^en[0-9]+/ || $1 ~ /^bridge[0-9]+/) && $3 ~ /^<Link#/ {name=$1; sub(/\*$/, "", name); if (index(bm, " " name " ") == 0) {rx+=$(NF-4); tx+=$(NF-1)}} END {printf "%.0f %.0f\n", (rx?rx:0), (tx?tx:0)}'; echo "---"; (ping -c 1 -t 2 1.1.1.1 2>/dev/null | awk -F'=' '/round-trip/ {split($2, a, "/"); sub(/^[ \t]+/, "", a[2]); ans = a[2]; found = 1} /time=/ && !found {for (i=1; i<=NF; i++) if ($i ~ /^time=/) {split($i, b, "="); ans = b[2]; found = 1}} END {if (found && ans != "") print ans; else print "OFFLINE"}') || echo "OFFLINE"; echo "---"; curl -s --connect-timeout 2 -m 4 https://icanhazip.com 2>/dev/null || curl -s --connect-timeout 2 -m 4 https://ifconfig.me 2>/dev/null || echo ""; echo "---"; (netstat -an -f inet -p tcp 2>/dev/null || netstat -an 2>/dev/null) | awk 'function d2c(s, idx) {idx=match(s, /\.[0-9]+$/); if (idx>0) return substr(s,1,idx-1) ":" substr(s,idx+1); return s} /ESTABLISHED/ {if (NF>=5) {l=d2c($4); r=d2c($5); if (l!="" && r!="") print l, r}}' | head -n 50; echo "---"; (netstat -an -f inet -p tcp 2>/dev/null || netstat -an 2>/dev/null) | awk 'function d2c(s, idx) {idx=match(s, /\.[0-9]+$/); if (idx>0) return substr(s,1,idx-1) ":" substr(s,idx+1); return s} /LISTEN/ {if (NF>=4) {addr=d2c($4); if (addr!="") print "tcp", addr}}' | head -n 30; echo "---"; echo "0"; echo "---"; (sysctl -n hw.ncpu 2>/dev/null || echo 1); echo "---"; echo ""; echo "---"; { perl -e 'alarm 2; exec @ARGV' system_profiler SPHardwareDataType 2>/dev/null || system_profiler SPHardwareDataType 2>/dev/null; } | awk -v hm="$(sysctl -n hw.model 2>/dev/null)" -v cb="$(sysctl -n machdep.cpu.brand_string 2>/dev/null)" 'BEGIN { name=""; id=""; chip="" } /Model Name:/ { sub(/^[ \t]*Model Name:[ \t]*/, ""); name = $0 } /Model Identifier:/ { sub(/^[ \t]*Model Identifier:[ \t]*/, ""); id = $0 } /Chip:/ { sub(/^[ \t]*Chip:[ \t]*/, ""); chip = $0 } END { if (name != "" && id != "" && chip != "") { printf "Apple %s (%s) - %s\n", name, id, chip } else if (name != "" && id != "") { printf "Apple %s (%s)\n", name, id } else if (name != "") { printf "Apple %s\n", name } else if (hm != "" && cb != "") { printf "Apple %s - %s\n", hm, cb } else if (hm != "") { printf "Apple %s\n", hm } }'; echo "---"; (stat -f %B /var/db/.AppleSetupDone 2>/dev/null || stat -f %m /var/db/.AppleSetupDone 2>/dev/null || echo 0); echo "---"; ioreg -rd1 -c IOBlockStorageDriver 2>/dev/null | awk 'BEGIN {r=0; w=0} /"Bytes \(Read\)"/ {if (match($0, /"Bytes \(Read\)"=[0-9]+/)) {s=substr($0, RSTART, RLENGTH); sub(/.*=/, "", s); r+=s+0}} /"Bytes \(Write\)"/ {if (match($0, /"Bytes \(Write\)"=[0-9]+/)) {s=substr($0, RSTART, RLENGTH); sub(/.*=/, "", s); w+=s+0}} END {printf "%.0f %.0f\n", r, w}'; echo "---"; (ps -Aro pid,user,%cpu,%mem,rss,ucomm 2>/dev/null || ps -ro pid,user,%cpu,%mem,rss,ucomm 2>/dev/null) | head -n 6; echo "---"; (ps -Amo pid,user,%cpu,%mem,rss,ucomm 2>/dev/null || ps -mo pid,user,%cpu,%mem,rss,ucomm 2>/dev/null) | head -n 6; echo "---"; echo ""`

// Apple de-facto OS lifecycle support policy:
// Apple maintains the current major macOS release (N) and the two preceding major
// releases (N-1, N-2) with security updates. Older versions cease receiving security
// patches and are considered End-Of-Life (EOL).
//
// In 2026:
// - macOS 26 (Tahoe): Current major release -> Supported
// - macOS 15 (Sequoia): N-1 release -> Supported
// - macOS 14 (Sonoma): N-2 release -> Supported
// - macOS 13 (Ventura) and older: End-Of-Life (EOL)
var (
	// CurrentMacOSMajor is the current active macOS major release number.
	CurrentMacOSMajor = 26

	// MacOSSupportedMajors explicitly enumerates supported major versions under the N-2 rule.
	MacOSSupportedMajors = map[int]bool{
		26: true, // macOS 26 Tahoe (current release)
		15: true, // macOS 15 Sequoia (N-1)
		14: true, // macOS 14 Sonoma (N-2)
	}

	// MacOSVersionNames maps macOS major version numbers to marketing code names.
	MacOSVersionNames = map[int]string{
		26: "Tahoe",
		15: "Sequoia",
		14: "Sonoma",
		13: "Ventura",
		12: "Monterey",
		11: "Big Sur",
	}
)

var macVersionRegex = regexp.MustCompile(`(?i)(?:macOS|Mac OS X|OS X)\s+([0-9]+(?:\.[0-9]+)*)`)

// parseMacOSMajor parses the major version integer and the full version string from an OS description.
func parseMacOSMajor(osInfo string) (int, string) {
	m := macVersionRegex.FindStringSubmatch(osInfo)
	if len(m) >= 2 {
		verStr := m[1]
		parts := strings.Split(verStr, ".")
		major, _ := strconv.Atoi(parts[0])
		return major, verStr
	}

	// Fallback for strings like "macOS 26" or simple "26.2" if context is macOS
	fields := strings.Fields(osInfo)
	for _, f := range fields {
		f = strings.Trim(f, `"(),`)
		if parts := strings.Split(f, "."); len(parts) > 0 {
			if val, err := strconv.Atoi(parts[0]); err == nil && val >= 10 && val <= 99 {
				return val, f
			}
		}
	}
	return 0, ""
}

// isMacOSEOL checks whether a given OS string represents an EOL macOS version under Apple's N-2 support rule.
func isMacOSEOL(osInfo string) (bool, string) {
	lower := strings.ToLower(osInfo)
	if !strings.Contains(lower, "macos") && !strings.Contains(lower, "mac os") && !strings.Contains(lower, "os x") && !strings.Contains(lower, "darwin") {
		return false, ""
	}

	major, verStr := parseMacOSMajor(osInfo)
	if major <= 0 {
		return false, ""
	}

	// Any version >= CurrentMacOSMajor is supported (current or newer preview)
	if major >= CurrentMacOSMajor {
		return false, ""
	}

	// If listed in supported table, not EOL
	if MacOSSupportedMajors[major] {
		return false, ""
	}

	// Otherwise it is EOL
	name := MacOSVersionNames[major]
	if name != "" {
		if verStr != "" {
			return true, fmt.Sprintf("macOS %s (%s)", verStr, name)
		}
		return true, fmt.Sprintf("macOS %d (%s)", major, name)
	}
	if verStr != "" {
		return true, fmt.Sprintf("macOS %s", verStr)
	}
	return true, fmt.Sprintf("macOS %d", major)
}

// ParseDarwinVMStat parses vm_stat, sysctl vm.swapusage, and hw.memsize bytes.
// Memory Used matches Activity Monitor: (Anonymous - Purgeable + Wired + Compressor) * pagesize.
func ParseDarwinVMStat(vmStatOut, swapusageOut string, memsizeBytes int64) (ramUsed, ramTotal, swapUsed, swapTotal int64) {
	ramTotal = memsizeBytes
	pageSize := int64(4096)

	var anonPages, purgPages, wiredPages, compPages int64

	for _, line := range strings.Split(vmStatOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "page size of") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if num, err := strconv.ParseInt(f, 10, 64); err == nil && num > 0 {
					pageSize = num
					break
				}
			}
		} else if strings.HasPrefix(line, "Anonymous pages:") {
			anonPages = extractNumber(line)
		} else if strings.HasPrefix(line, "Pages purgeable:") {
			purgPages = extractNumber(line)
		} else if strings.HasPrefix(line, "Pages wired down:") {
			wiredPages = extractNumber(line)
		} else if strings.HasPrefix(line, "Pages occupied by compressor:") {
			compPages = extractNumber(line)
		}
	}

	usedPages := anonPages - purgPages + wiredPages + compPages
	if usedPages < 0 {
		usedPages = 0
	}
	ramUsed = usedPages * pageSize

	// Parse swapusage: "total = 7168.00M  used = 6034.31M  free = 1133.69M  (encrypted)"
	for _, line := range strings.Split(swapusageOut, "\n") {
		if strings.Contains(line, "total =") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "total" && i+2 < len(fields) {
					swapTotal = parseByteUnit(strings.Trim(fields[i+2], ","))
				}
				if f == "used" && i+2 < len(fields) {
					swapUsed = parseByteUnit(strings.Trim(fields[i+2], ","))
				}
			}
		}
	}

	return ramUsed, ramTotal, swapUsed, swapTotal
}

func extractNumber(s string) int64 {
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	n, _ := strconv.ParseInt(sb.String(), 10, 64)
	return n
}

func parseByteUnit(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	unit := strings.ToUpper(s[len(s)-1:])
	valStr := s[:len(s)-1]
	mult := float64(1)
	switch unit {
	case "G":
		mult = 1024 * 1024 * 1024
	case "M":
		mult = 1024 * 1024
	case "K":
		mult = 1024
	case "B":
		mult = 1
	default:
		valStr = s
	}
	f, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * mult))
}

// ParseDarwinBridgeMembers extracts member interface names from `ifconfig bridgeN` output.
func ParseDarwinBridgeMembers(bridgeSec string) []string {
	var members []string
	for _, line := range strings.Split(bridgeSec, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "member:" && i+1 < len(fields) {
				members = append(members, strings.TrimSpace(fields[i+1]))
			}
		}
	}
	return members
}

// ParseDarwinNetstatTotals calculates total cumulative rx and tx bytes from `netstat -ibn`
// considering physical en[0-9]+ and bridge[0-9]+ interfaces with <Link# headers,
// while excluding interfaces that belong to a bridge.
func ParseDarwinNetstatTotals(netstatOut string, bridgeMembers ...string) (rxBytes, txBytes int64) {
	excludeMap := make(map[string]bool)
	for _, m := range bridgeMembers {
		excludeMap[strings.TrimSpace(m)] = true
	}

	lines := strings.Split(netstatOut, "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 6 {
			continue
		}
		iface := strings.TrimSuffix(fields[0], "*")
		// Match physical en[0-9]+ or bridge[0-9]+
		if !isPhysicalEnOrBridge(iface) {
			continue
		}
		// Exclude interfaces that are members of a bridge
		if excludeMap[iface] {
			continue
		}
		// Match rows whose Network column starts with <Link#
		if !strings.HasPrefix(fields[2], "<Link#") {
			continue
		}
		// In BSD netstat: Ibytes is $(NF-4), Obytes is $(NF-1)
		rx, _ := strconv.ParseInt(fields[len(fields)-5], 10, 64)
		tx, _ := strconv.ParseInt(fields[len(fields)-2], 10, 64)
		rxBytes += rx
		txBytes += tx
	}
	return rxBytes, txBytes
}

func isPhysicalEnOrBridge(name string) bool {
	if strings.HasPrefix(name, "bridge") {
		numPart := strings.TrimPrefix(name, "bridge")
		numPart = strings.TrimSuffix(numPart, "*")
		if _, err := strconv.Atoi(numPart); err == nil {
			return true
		}
		return false
	}
	if strings.HasPrefix(name, "en") {
		numPart := strings.TrimPrefix(name, "en")
		numPart = strings.TrimSuffix(numPart, "*")
		if _, err := strconv.Atoi(numPart); err == nil {
			return true
		}
		return false
	}
	return false
}

// ParseDarwinIOStat sums cumulative read and write bytes from IOBlockStorageDriver statistics in ioreg.
func ParseDarwinIOStat(ioregOut string) (readBytes, writeBytes int64) {
	rRe := regexp.MustCompile(`"Bytes \(Read\)"=([0-9]+)`)
	wRe := regexp.MustCompile(`"Bytes \(Write\)"=([0-9]+)`)

	for _, line := range strings.Split(ioregOut, "\n") {
		if m := rRe.FindStringSubmatch(line); len(m) >= 2 {
			r, _ := strconv.ParseInt(m[1], 10, 64)
			readBytes += r
		}
		if m := wRe.FindStringSubmatch(line); len(m) >= 2 {
			w, _ := strconv.ParseInt(m[1], 10, 64)
			writeBytes += w
		}
	}
	return readBytes, writeBytes
}

// ParseDarwinHardwareModel builds the standard model string e.g. "Apple Mac Studio (Mac15,14) - Apple M3 Ultra".
func ParseDarwinHardwareModel(sysprofOut, hwModel, cpuBrand string) string {
	var modelName, modelID, chip string
	for _, line := range strings.Split(sysprofOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Model Name:") {
			modelName = strings.TrimSpace(strings.TrimPrefix(line, "Model Name:"))
		} else if strings.HasPrefix(line, "Model Identifier:") {
			modelID = strings.TrimSpace(strings.TrimPrefix(line, "Model Identifier:"))
		} else if strings.HasPrefix(line, "Chip:") {
			chip = strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
		}
	}

	if modelName != "" && modelID != "" && chip != "" {
		return fmt.Sprintf("Apple %s (%s) - %s", modelName, modelID, chip)
	}
	if modelName != "" && modelID != "" {
		return fmt.Sprintf("Apple %s (%s)", modelName, modelID)
	}
	if modelName != "" {
		return fmt.Sprintf("Apple %s", modelName)
	}
	if hwModel != "" && cpuBrand != "" {
		return fmt.Sprintf("Apple %s - %s", hwModel, cpuBrand)
	}
	if hwModel != "" {
		return fmt.Sprintf("Apple %s", hwModel)
	}
	return "Apple Mac"
}

// SplitFixtureSections splits a captured testdata fixture file delimited by "### name" sections.
func SplitFixtureSections(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")
	var currentName string
	var currentBody strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "### ") {
			if currentName != "" {
				result[currentName] = strings.TrimSpace(currentBody.String())
				currentBody.Reset()
			}
			currentName = strings.TrimSpace(strings.TrimPrefix(line, "### "))
		} else {
			if currentName != "" {
				currentBody.WriteString(line)
				currentBody.WriteByte('\n')
			}
		}
	}
	if currentName != "" {
		result[currentName] = strings.TrimSpace(currentBody.String())
	}
	return result
}
