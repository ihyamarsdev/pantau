package inspector

// Parsers for the output of the remote metric commands: processes, network,
// disk I/O, OS release, uptime, memory and filesystem.

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"pantau/internal/store"
)

// parseTopProcesses parses the output of ps -eo pid,user,%cpu,%mem,rss,comm.
func parseTopProcesses(out string) []store.ProcessInfo {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var list []store.ProcessInfo
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) < 6 {
			continue
		}
		if fields[0] == "PID" || fields[0] == "COMMAND" {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		user := fields[1]
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		rssKB, _ := strconv.ParseInt(fields[4], 10, 64)
		cmd := strings.Join(fields[5:], " ")
		list = append(list, store.ProcessInfo{
			PID:      pid,
			User:     user,
			CPU:      cpu,
			MEM:      mem,
			RSSBytes: rssKB * 1024,
			Command:  cmd,
		})
	}
	return list
}

// parseTopSwap parses the output of conditional /proc/*/status VmSwap reader.
func parseTopSwap(out string) []store.ProcessInfo {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var list []store.ProcessInfo
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		user := fields[1]
		swapKB, _ := strconv.ParseInt(fields[2], 10, 64)
		cmd := strings.Join(fields[3:], " ")
		list = append(list, store.ProcessInfo{
			PID:       pid,
			User:      user,
			SwapBytes: swapKB * 1024,
			Command:   cmd,
		})
	}
	return list
}

// sortAndTrimTopProcesses stable-sorts processes descending by metric ("cpu", "ram", or "swap") and caps at 5.
func sortAndTrimTopProcesses(list []store.ProcessInfo, metric string) []store.ProcessInfo {
	sort.SliceStable(list, func(i, j int) bool {
		switch metric {
		case "cpu":
			return list[i].CPU > list[j].CPU
		case "ram":
			return list[i].RSSBytes > list[j].RSSBytes
		case "swap":
			return list[i].SwapBytes > list[j].SwapBytes
		default:
			return false
		}
	})
	if len(list) > 5 {
		list = list[:5]
	}
	return list
}

func parseNetworkMetrics(h *store.Host, netDevSec, pingSec, pubIPSec, estSec, listenSec, failedSec string) {
	// 1. Bandwidth & Transfer
	fields := strings.Fields(strings.TrimSpace(netDevSec))
	if len(fields) >= 2 {
		rx, _ := strconv.ParseInt(fields[0], 10, 64)
		tx, _ := strconv.ParseInt(fields[1], 10, 64)

		if h.LastInspected != nil && h.NetRxBytes > 0 {
			elapsed := time.Since(*h.LastInspected).Seconds()
			if elapsed > 0 {
				deltaRx := rx - h.NetRxBytes
				deltaTx := tx - h.NetTxBytes
				if deltaRx >= 0 {
					h.NetRxSpeedBps = int64(float64(deltaRx) / elapsed)
				}
				if deltaTx >= 0 {
					h.NetTxSpeedBps = int64(float64(deltaTx) / elapsed)
				}
			}
		}
		h.NetRxBytes = rx
		h.NetTxBytes = tx
	}

	// 2. Internet Egress & Latency
	pingOut := strings.TrimSpace(pingSec)
	if pingOut == "" || strings.EqualFold(pingOut, "OFFLINE") {
		h.InternetOnline = false
		h.InternetLatencyMs = 0
	} else {
		h.InternetOnline = true
		latFloat, _ := strconv.ParseFloat(pingOut, 64)
		h.InternetLatencyMs = int64(math.Round(latFloat))
	}

	// 3. Public IP
	h.PublicIP = strings.TrimSpace(pubIPSec)

	// 4. Established Connections & Top Remote IPs
	ipCounts := make(map[string]int)
	totalActive := 0
	for _, line := range strings.Split(estSec, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			totalActive++
			remote := parts[1]
			host, _, err := net.SplitHostPort(remote)
			if err != nil {
				if lastDot := strings.LastIndex(remote, "."); lastDot != -1 {
					portPart := remote[lastDot+1:]
					if _, portErr := strconv.Atoi(portPart); portErr == nil {
						host = remote[:lastDot]
					} else {
						host = remote
					}
				} else {
					host = remote
				}
			}
			if host != "" {
				ipCounts[host]++
			}
		}
	}
	h.ActiveConnCount = totalActive

	type ipCountPair struct {
		ip    string
		count int
	}
	var pairs []ipCountPair
	for ip, count := range ipCounts {
		pairs = append(pairs, ipCountPair{ip: ip, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})

	topConns := []store.TopConn{}
	limit := 10
	if len(pairs) < limit {
		limit = len(pairs)
	}
	for i := 0; i < limit; i++ {
		topConns = append(topConns, store.TopConn{
			RemoteIP: pairs[i].ip,
			Count:    pairs[i].count,
		})
	}
	topBytes, _ := json.Marshal(topConns)
	h.TopConnections = string(topBytes)

	// 5. Listening Ports & Public Exposure
	// Services that should never face the internet directly (databases, caches, admin/control planes, legacy cleartext, file sharing, remote desktop).
	// The sensitive port list lives in the package scope (sensitivePorts) and is shared with the
	// Docker container audit.
	listening := []store.ListeningPort{}
	for _, line := range strings.Split(listenSec, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			addr := parts[1] // e.g. 0.0.0.0:80, *:22, 127.0.0.1:3306
			proc := ""
			if len(parts) >= 3 {
				proc = extractListenProcess(parts[2:])
			}
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				if lastDot := strings.LastIndex(addr, "."); lastDot != -1 {
					portPart := addr[lastDot+1:]
					if _, portErr := strconv.Atoi(portPart); portErr == nil {
						host = addr[:lastDot]
						port = portPart
						addr = host + ":" + port
						err = nil
					}
				}
			}
			if err != nil {
				continue
			}
			isPublic := host == "0.0.0.0" || host == "*" || host == "::" || host == ""
			risk := "low"
			if isPublic && sensitivePorts[port] {
				risk = "high"
			}
			source := ""
			if proc != "" {
				source = "process"
			}
			listening = append(listening, store.ListeningPort{
				Proto:         "tcp",
				Port:          port,
				Address:       addr,
				Public:        isPublic,
				Process:       proc,
				ProcessSource: source,
				Risk:          risk,
			})
		}
	}
	listenBytes, _ := json.Marshal(listening)
	h.ListeningPorts = string(listenBytes)

	// 6. Failed Logins Count (sshd, last 24h via journald; whole auth.log/secure otherwise). "NA" = no readable log → -1.
	if v := strings.TrimSpace(failedSec); v == "NA" {
		h.FailedLoginsCount = -1
	} else {
		h.FailedLoginsCount, _ = strconv.Atoi(v)
	}
}

// cgnatBlock is RFC 6598 (100.64.0.0/10): Tailscale, NetBird and carrier NAT addresses
// that net.IP.IsPrivate does not cover but that are private enough to probe over.
var cgnatBlock = func() *net.IPNet {
	_, b, _ := net.ParseCIDR("100.64.0.0/10")
	return b
}()

// parsePrivateIPs keeps the private IPv4 addresses of a host as a JSON array, in interface
// order: the first entry is the primary address a peer should reach the host on.
func parsePrivateIPs(out string) string {
	ips := []string{}
	seen := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		ip := net.ParseIP(strings.TrimSpace(line))
		if ip == nil || ip.To4() == nil {
			continue
		}
		if !ip.IsPrivate() && !cgnatBlock.Contains(ip) {
			continue
		}
		s := ip.To4().String()
		if seen[s] {
			continue
		}
		seen[s] = true
		ips = append(ips, s)
		if len(ips) == 8 {
			break
		}
	}
	b, err := json.Marshal(ips)
	if err != nil {
		return ""
	}
	return string(b)
}

func parseDiskIOMetrics(h *store.Host, diskStatsSec string) {
	fields := strings.Fields(strings.TrimSpace(diskStatsSec))
	if len(fields) >= 2 {
		readBytes, _ := strconv.ParseInt(fields[0], 10, 64)
		writeBytes, _ := strconv.ParseInt(fields[1], 10, 64)

		if h.LastInspected != nil && h.DiskReadBytes > 0 {
			elapsed := time.Since(*h.LastInspected).Seconds()
			if elapsed > 0 {
				deltaRead := readBytes - h.DiskReadBytes
				deltaWrite := writeBytes - h.DiskWriteBytes

				if deltaRead >= 0 {
					h.DiskReadSpeedBps = int64(float64(deltaRead) / elapsed)
				} else {
					// Host rebooted / counter rolled over
					h.DiskReadSpeedBps = 0
				}

				if deltaWrite >= 0 {
					h.DiskWriteSpeedBps = int64(float64(deltaWrite) / elapsed)
				} else {
					// Host rebooted / counter rolled over
					h.DiskWriteSpeedBps = 0
				}
			}
		}
		h.DiskReadBytes = readBytes
		h.DiskWriteBytes = writeBytes
	}
}

func parseOSInfo(osRelease string) string {
	var prettyName, name, version string
	for _, line := range strings.Split(osRelease, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			prettyName = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		} else if strings.HasPrefix(line, "NAME=") {
			name = strings.Trim(strings.TrimPrefix(line, "NAME="), `"`)
		} else if strings.HasPrefix(line, "VERSION=") {
			version = strings.Trim(strings.TrimPrefix(line, "VERSION="), `"`)
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			if version == "" {
				version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`)
			}
		} else if strings.HasPrefix(line, "ProductName:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "ProductName:"))
		} else if strings.HasPrefix(line, "ProductVersion:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "ProductVersion:"))
		}
	}
	if prettyName != "" {
		if strings.Contains(prettyName, "(") || !strings.Contains(prettyName, "macOS") {
			return prettyName
		}
		if major, _ := parseMacOSMajor(prettyName); major > 0 {
			if cName := MacOSVersionNames[major]; cName != "" {
				return fmt.Sprintf("%s (%s)", prettyName, cName)
			}
		}
		return prettyName
	}
	if name != "" && version != "" {
		if strings.EqualFold(name, "macOS") {
			if major, _ := parseMacOSMajor(version); major > 0 {
				if cName := MacOSVersionNames[major]; cName != "" {
					return fmt.Sprintf("macOS %s (%s)", version, cName)
				}
			}
			return fmt.Sprintf("macOS %s", version)
		}
		return name + " " + version
	}
	if name != "" {
		return name
	}
	// Fallback for non-systemd / legacy distributions (CentOS 6, Debian 7) without standard key=value os-release
	for _, line := range strings.Split(osRelease, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "release") || strings.Contains(line, "Linux") || strings.Contains(line, "Debian") || strings.Contains(line, "Ubuntu") {
			return line
		}
	}
	return "Linux"
}

func parseUptime(line string) (uptimeStr, loadStr string) {
	// e.g. " 14:00:01 up 12 days,  3:15,  1 user,  load average: 0.15, 0.08, 0.05"
	// or macOS: "13:46  up 13 days, 20:09, 1 user, load averages: 1.83 1.61 1.53"
	// or "up 3 hrs, 1 user, load averages: 0.50 0.40 0.30"
	loadDelimiter := "load average:"
	if strings.Contains(line, "load averages:") {
		loadDelimiter = "load averages:"
	}
	parts := strings.Split(line, loadDelimiter)
	if len(parts) == 2 {
		rawLoad := strings.TrimSpace(parts[1])
		if strings.Contains(rawLoad, ",") {
			loadStr = rawLoad
		} else {
			fields := strings.Fields(rawLoad)
			if len(fields) >= 3 {
				loadStr = fmt.Sprintf("%s, %s, %s", fields[0], fields[1], fields[2])
			} else {
				loadStr = strings.Join(fields, ", ")
			}
		}
	}

	upIdx := strings.Index(parts[0], "up ")
	if upIdx != -1 {
		afterUp := parts[0][upIdx+3:]
		rawPieces := strings.Split(afterUp, ",")
		var uptimePieces []string
		for _, piece := range rawPieces {
			trimmed := strings.TrimSpace(piece)
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && strings.HasPrefix(fields[1], "user") {
				break
			}
			uptimePieces = append(uptimePieces, trimmed)
		}
		if len(uptimePieces) >= 2 {
			uptimeStr = strings.TrimSpace(uptimePieces[0] + ", " + uptimePieces[1])
		} else if len(uptimePieces) == 1 {
			uptimeStr = uptimePieces[0]
		} else if len(rawPieces) > 0 {
			uptimeStr = strings.TrimSpace(rawPieces[0])
		}
	} else {
		uptimeStr = line
	}
	return uptimeStr, loadStr
}

func parseFree(output string) (ramUsed, ramTotal, swapUsed, swapTotal int64) {
	lines := strings.Split(output, "\n")
	var memTot, memUsd int64
	var swpTot, swpUsd int64
	var bufCacheUsed int64
	hasBufCache := false

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "Mem:") {
			fields := strings.Fields(l)
			if len(fields) >= 3 {
				memTot, _ = strconv.ParseInt(fields[1], 10, 64)
				memUsd, _ = strconv.ParseInt(fields[2], 10, 64)
			}
		} else if strings.HasPrefix(l, "Swap:") {
			fields := strings.Fields(l)
			if len(fields) >= 3 {
				swpTot, _ = strconv.ParseInt(fields[1], 10, 64)
				swpUsd, _ = strconv.ParseInt(fields[2], 10, 64)
			}
		} else if strings.Contains(l, "buffers/cache:") {
			// Legacy free format on pre-3.14 kernels (CentOS 6): "-/+ buffers/cache: used free"
			fields := strings.Fields(l)
			for i, f := range fields {
				if (f == "buffers/cache:" || strings.HasSuffix(f, "buffers/cache:")) && i+1 < len(fields) {
					if u, err := strconv.ParseInt(fields[i+1], 10, 64); err == nil {
						bufCacheUsed = u
						hasBufCache = true
						break
					}
				}
			}
		}
	}

	finalRamUsed := memUsd
	if hasBufCache && bufCacheUsed > 0 {
		finalRamUsed = bufCacheUsed
	}
	return finalRamUsed, memTot, swpUsd, swpTot
}

func parseDf(output string) (used, total int64) {
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 6 && (fields[5] == "/" || fields[5] == "/System/Volumes/Data" || strings.HasSuffix(fields[5], "/")) {
			totK, _ := strconv.ParseInt(fields[1], 10, 64)
			usdK, _ := strconv.ParseInt(fields[2], 10, 64)
			if fields[5] == "/System/Volumes/Data" && len(fields) >= 4 {
				availK, _ := strconv.ParseInt(fields[3], 10, 64)
				if totK > 0 && availK > 0 && totK >= availK {
					usdK = totK - availK
				}
			}
			return usdK * 1024, totK * 1024
		}
	}
	return 0, 0
}
