package netdiag

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// Tracepath regex: e.g. " 1:  192.168.1.1   1.234ms" or " 3:  no reply" or " 4:  1.1.1.1   12.345ms reached"
	tracepathHopRegex = regexp.MustCompile(`^\s*(\d+)(?:\?)?:\s+(?:\[[^\]]+\]\s+)?([^\s]+)(?:\s+([0-9.]+)\s*ms)?(.*)$`)

	// Traceroute regex: e.g. " 1  192.168.1.1  1.234 ms" or " 3  *" or " 2  gateway (10.0.0.1)  2.34 ms"
	tracerouteHopRegex = regexp.MustCompile(`^\s*(\d+)\s+([^\s]+)(?:\s+\(([0-9a-fA-F.:]+)\))?(?:\s+([0-9.]+)\s*ms|\s+\*|\s+no reply)?`)

	// Traceroute header: "traceroute to google.com (142.250.4.100), 20 hops max" — captures the resolved destination IP.
	tracerouteHeaderRegex = regexp.MustCompile(`^traceroute6? to \S+ \(([0-9a-fA-F.:]+)\)`)

	// Ping regexes
	pingStatsRegex = regexp.MustCompile(`(\d+)\s+packets transmitted,\s+(\d+)\s+(?:packets\s+)?received,.*?(?:([0-9.]+)%\s+packet loss)`)
	pingRTTRegex   = regexp.MustCompile(`(?:rtt|round-trip)\s+min/avg/max(?:/(?:mdev|stddev))?\s*=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)(?:/([0-9.]+|nan|-))?\s*ms`)

	// DNS answer regexes. IPv6 candidates are confirmed with net.ParseIP.
	dnsIPv4Regex      = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	dnsIPv6Regex      = regexp.MustCompile(`(?:^|[\s(])([0-9a-fA-F]{0,4}(?::[0-9a-fA-F]{0,4}){2,7})(?:[\s)]|$)`)
	dnsResolverLineRe = regexp.MustCompile(`(?i)^\s*(?:server:|name:|using domain server|;;|.*#53\b)`)

	// dig regexes: answer record "google.com.  22  IN  MX  10 smtp.google.com." and comment/stat lines.
	digRecordRegex    = regexp.MustCompile(`^(\S+)\s+(\d+)\s+IN\s+(\S+)\s+(.+)$`)
	digStatusRegex    = regexp.MustCompile(`status:\s*([A-Z]+)`)
	digQueryTimeRegex = regexp.MustCompile(`;; Query time:\s*(\d+)\s*msec`)
	digServerRegex    = regexp.MustCompile(`;; SERVER:\s*([^#\s]+)`)

	// IP CIDRs for private/internal classification
	privateCIDRs []*net.IPNet
)

func init() {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"100.64.0.0/10",  // CGNAT RFC 6598
		"169.254.0.0/16", // Link Local
		"::1/128",        // IPv6 Loopback
		"fc00::/7",       // IPv6 Unique Local
		"fe80::/10",      // IPv6 Link Local
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			privateCIDRs = append(privateCIDRs, ipNet)
		}
	}
}

// ClassifyIPZone classifies an IP into private, transit, destination, or timeout.
func ClassifyIPZone(ipStr, target string) string {
	if ipStr == "" || ipStr == "*" || strings.EqualFold(ipStr, "no reply") {
		return "timeout"
	}
	cleanIP := strings.Trim(ipStr, "()")
	if target != "" && (cleanIP == target || strings.EqualFold(cleanIP, target)) {
		return "destination"
	}

	parsed := net.ParseIP(cleanIP)
	if parsed == nil {
		return "transit"
	}

	for _, block := range privateCIDRs {
		if block.Contains(parsed) {
			return "private"
		}
	}
	return "transit"
}

// ParseTracepathOrTracerouteLine parses a single line from tracepath or traceroute.
func ParseTracepathOrTracerouteLine(line string, target string) *HopInfo {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "traceroute to") || strings.HasPrefix(trimmed, "Resume:") || strings.HasPrefix(trimmed, "Too many hops") {
		return nil
	}

	// 1. Try tracepath format: e.g. " 1:  192.168.1.1  1.234ms" or " 3:  no reply"
	if matches := tracepathHopRegex.FindStringSubmatch(line); len(matches) > 2 {
		hopNum, err := strconv.Atoi(matches[1])
		if err != nil || hopNum < 1 {
			return nil
		}
		rawHost := matches[2]
		rawRTT := matches[3]
		extra := matches[4]

		if rawHost == "no" || strings.EqualFold(rawHost, "no reply") || rawHost == "*" {
			return &HopInfo{
				Hop:    hopNum,
				Host:   "* * *",
				IP:     "",
				RTTMs:  0,
				Zone:   "timeout",
				Status: "timeout",
			}
		}

		var rtt float64
		if rawRTT != "" {
			rtt, _ = strconv.ParseFloat(rawRTT, 64)
		}

		zone := ClassifyIPZone(rawHost, target)
		if strings.Contains(strings.ToLower(extra), "reached") {
			zone = "destination"
		}

		status := "ok"
		if rtt > 100.0 {
			status = "warning"
		}

		return &HopInfo{
			Hop:    hopNum,
			Host:   rawHost,
			IP:     rawHost,
			RTTMs:  rtt,
			Zone:   zone,
			Status: status,
		}
	}

	// 2. Try traceroute format: e.g. " 1  192.168.1.1  1.234 ms" or " 2  gateway (10.0.0.1)  2.34 ms" or " 3  *"
	if matches := tracerouteHopRegex.FindStringSubmatch(line); len(matches) > 2 {
		hopNum, err := strconv.Atoi(matches[1])
		if err != nil || hopNum < 1 {
			return nil
		}
		rawHost := matches[2]
		ipInParens := matches[3]
		rawRTT := matches[4]

		if rawHost == "*" || strings.EqualFold(rawHost, "no reply") {
			return &HopInfo{
				Hop:    hopNum,
				Host:   "* * *",
				IP:     "",
				RTTMs:  0,
				Zone:   "timeout",
				Status: "timeout",
			}
		}

		effectiveIP := rawHost
		if ipInParens != "" {
			effectiveIP = ipInParens
		}

		var rtt float64
		if rawRTT != "" {
			rtt, _ = strconv.ParseFloat(rawRTT, 64)
		}

		zone := ClassifyIPZone(effectiveIP, target)
		status := "ok"
		if rtt > 100.0 {
			status = "warning"
		}

		return &HopInfo{
			Hop:    hopNum,
			Host:   rawHost,
			IP:     effectiveIP,
			RTTMs:  rtt,
			Zone:   zone,
			Status: status,
		}
	}

	return nil
}

// AnalyzeHops evaluates a sequence of hops and produces automated diagnostic insights.
// ponytail: Heuristic hop interpretation with latency jump and blackhole detection.
func AnalyzeHops(hops []*HopInfo, target string, targetReached bool) *HopAnalysis {
	analysis := &HopAnalysis{
		TotalHops:   len(hops),
		GeneratedAt: time.Now(),
		Status:      "optimal",
	}

	if len(hops) == 0 {
		analysis.Status = "failed"
		analysis.Summary = "No hops detected. Target might be unreachable or interface is down."
		analysis.SummaryID = "Tidak ada hop yang terdeteksi. Target tidak dapat dijangkau atau interface mati."
		return analysis
	}

	var lastReplyingHop *HopInfo
	var lastRespondingHopIndex = -1
	var maxRTT float64
	var maxDiff float64
	var spikeHop *HopInfo
	var reachedHop *HopInfo

	for i, h := range hops {
		if h.Zone == "destination" || targetReached || (target != "" && (h.IP == target || h.Host == target)) {
			reachedHop = h
			analysis.DestinationFound = true
		}

		if h.Status != "timeout" && h.RTTMs > 0 {
			if lastReplyingHop != nil {
				diff := h.RTTMs - lastReplyingHop.RTTMs
				if diff > 60.0 && diff > maxDiff {
					maxDiff = diff
					spikeHop = h
				}
			}
			lastReplyingHop = h
			lastRespondingHopIndex = i
			if h.RTTMs > maxRTT {
				maxRTT = h.RTTMs
			}
		}
	}

	if lastReplyingHop != nil {
		analysis.FinalRTTMs = lastReplyingHop.RTTMs
	}

	// 1. Check if destination was reached
	if reachedHop != nil || analysis.DestinationFound {
		analysis.DestinationFound = true
		finalMs := analysis.FinalRTTMs
		if reachedHop != nil && reachedHop.RTTMs > 0 {
			finalMs = reachedHop.RTTMs
		}

		if spikeHop != nil && maxDiff >= 70.0 {
			analysis.Status = "degraded"
			analysis.LatencySpikeHop = spikeHop.Hop
			analysis.Summary = fmt.Sprintf("Route reached destination (%d hops, final RTT %.1f ms), but latency spike (+%.1f ms) detected at Hop %d (%s).",
				len(hops), finalMs, maxDiff, spikeHop.Hop, spikeHop.Host)
			analysis.SummaryID = fmt.Sprintf("Rute mencapai tujuan (%d hop, latensi akhir %.1f ms), namun terdeteksi lonjakan latensi (+%.1f ms) pada Hop %d (%s).",
				len(hops), finalMs, maxDiff, spikeHop.Hop, spikeHop.Host)
		} else {
			analysis.Status = "optimal"
			analysis.Summary = fmt.Sprintf("Route healthy and optimal (Total %d hops, final RTT %.1f ms).",
				len(hops), finalMs)
			analysis.SummaryID = fmt.Sprintf("Rute sehat dan optimal (Total %d hop, latensi akhir %.1f ms).",
				len(hops), finalMs)
		}
		return analysis
	}

	// 2. Destination not reached: check if dropped midway
	if lastRespondingHopIndex >= 0 && lastRespondingHopIndex < len(hops)-1 {
		dropHop := hops[lastRespondingHopIndex]
		analysis.Status = "failed"
		analysis.DropAtHop = dropHop.Hop
		analysis.Summary = fmt.Sprintf("Traffic failed to reach destination; halted after Hop %d (%s) - likely upstream firewall drop or routing blackhole.",
			dropHop.Hop, dropHop.Host)
		analysis.SummaryID = fmt.Sprintf("Trafik tidak mencapai tujuan; terhenti setelah Hop %d (%s) - kemungkinan firewall upstream atau blackhole rute.",
			dropHop.Hop, dropHop.Host)
		return analysis
	}

	// 3. All timeouts or initial failure
	analysis.Status = "failed"
	analysis.Summary = fmt.Sprintf("Target did not respond (All hops timed out) - host offline or ICMP blocked.")
	analysis.SummaryID = fmt.Sprintf("Target tidak merespons (Semua hop timeout) - host offline atau ICMP diblokir.")
	return analysis
}

// ParsePingOutput parses the output of `ping -c 4 -W 2`.
func ParsePingOutput(output string) *PingStats {
	stats := &PingStats{
		Status: "down",
	}

	statsMatches := pingStatsRegex.FindStringSubmatch(output)
	if len(statsMatches) >= 4 {
		stats.PacketsTransmitted, _ = strconv.Atoi(statsMatches[1])
		stats.PacketsReceived, _ = strconv.Atoi(statsMatches[2])
		stats.PacketLossPct, _ = strconv.ParseFloat(statsMatches[3], 64)
	}

	rttMatches := pingRTTRegex.FindStringSubmatch(output)
	if len(rttMatches) >= 4 {
		stats.MinMs, _ = strconv.ParseFloat(rttMatches[1], 64)
		stats.AvgMs, _ = strconv.ParseFloat(rttMatches[2], 64)
		stats.MaxMs, _ = strconv.ParseFloat(rttMatches[3], 64)
	}

	if stats.PacketsTransmitted > 0 && stats.PacketsReceived == 0 {
		stats.Status = "down"
		stats.Summary = fmt.Sprintf("Target unreachable (100%% packet loss, %d packets sent).", stats.PacketsTransmitted)
		stats.SummaryID = fmt.Sprintf("Target tidak merespons (100%% packet loss, %d paket terkirim).", stats.PacketsTransmitted)
	} else if stats.PacketLossPct > 0 {
		stats.Status = "degraded"
		stats.Summary = fmt.Sprintf("Connection degraded (%.0f%% packet loss, avg RTT %.1f ms).", stats.PacketLossPct, stats.AvgMs)
		stats.SummaryID = fmt.Sprintf("Koneksi mengalami degradasi (%.0f%% packet loss, RTT rata-rata %.1f ms).", stats.PacketLossPct, stats.AvgMs)
	} else if stats.PacketsReceived > 0 {
		stats.Status = "healthy"
		stats.Summary = fmt.Sprintf("Connection stable (0%% packet loss, avg RTT %.1f ms, min/max %.1f/%.1f ms).", stats.AvgMs, stats.MinMs, stats.MaxMs)
		stats.SummaryID = fmt.Sprintf("Koneksi stabil (0%% packet loss, RTT rata-rata %.1f ms, min/max %.1f/%.1f ms).", stats.AvgMs, stats.MinMs, stats.MaxMs)
	} else {
		stats.Status = "down"
		stats.Summary = "Ping command failed or output could not be parsed."
		stats.SummaryID = "Perintah ping gagal atau output tidak dapat diuraikan."
	}

	return stats
}

// ParsePortOutput parses port probe output.
func ParsePortOutput(output string, port int) *PortStats {
	isOpen := strings.Contains(output, "PORT_OPEN")
	stats := &PortStats{
		Port: port,
		Open: isOpen,
	}

	if isOpen {
		stats.Status = "healthy"
		stats.Summary = fmt.Sprintf("Port %d is OPEN and reachable.", port)
		stats.SummaryID = fmt.Sprintf("Port %d TERBUKA dan dapat dijangkau.", port)
	} else {
		stats.Status = "down"
		stats.Summary = fmt.Sprintf("Port %d is CLOSED or filtered by firewall.", port)
		stats.SummaryID = fmt.Sprintf("Port %d TERTUTUP atau diblokir firewall.", port)
	}
	return stats
}

// ParseDNSOutput parses DNS resolution results across multiple platform tools, IPv4 and IPv6:
// - macOS dscacheutil: "ip_address: <v4>" / "ipv6_address: <v6>"
// - Linux getent ahosts: "<ip> STREAM <domain>"
// - nslookup: "Address: <ip>" (resolver "Server:" / "Address: x#53" lines are skipped)
// - host: "<domain> has address <ip>" / "has IPv6 address <ip>"
// - dig +short: one IP per line (CNAME targets ignored)
func ParseDNSOutput(output string, target string) *DNSStats {
	stats := &DNSStats{
		Target: target,
		Status: "down",
	}

	seen := make(map[string]bool)
	add := func(ip string) {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.IsUnspecified() || ip == "127.0.0.53" || seen[ip] {
			return
		}
		seen[ip] = true
		stats.ResolvedIPs = append(stats.ResolvedIPs, ip)
	}

	// nslookup prints the resolver as "Server:" plus "Address: <ip>#53" before the answer.
	afterServer := false
	for _, line := range strings.Split(output, "\n") {
		if dnsResolverLineRe.MatchString(line) {
			afterServer = strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "server:")
			continue
		}
		if afterServer {
			afterServer = false
			if strings.HasPrefix(strings.TrimSpace(line), "Address") {
				continue
			}
		}
		for _, ip := range dnsIPv4Regex.FindAllString(line, -1) {
			add(ip)
		}
		for _, m := range dnsIPv6Regex.FindAllStringSubmatch(line, -1) {
			add(m[1])
		}
	}

	if len(stats.ResolvedIPs) > 0 {
		stats.Status = "healthy"
		via := ""
		viaID := ""
		if strings.Contains(output, dnsPublicFallbackMarker) {
			via = " (via public resolver 1.1.1.1; the host's system resolver returned no answer)"
			viaID = " (via resolver publik 1.1.1.1; resolver sistem host tidak memberi jawaban)"
		}
		stats.Summary = fmt.Sprintf("Successfully resolved %s to: %s%s", target, strings.Join(stats.ResolvedIPs, ", "), via)
		stats.SummaryID = fmt.Sprintf("Berhasil me-resolve %s ke: %s%s", target, strings.Join(stats.ResolvedIPs, ", "), viaID)
	} else {
		stats.Status = "down"
		stats.Summary = fmt.Sprintf("Failed to resolve domain name '%s' (system resolver and public resolver 1.1.1.1). Check the domain name and the host's DNS/outbound UDP 53.", target)
		stats.SummaryID = fmt.Sprintf("Gagal me-resolve nama domain '%s' (resolver sistem dan resolver publik 1.1.1.1). Periksa nama domain serta DNS/akses UDP 53 keluar dari host.", target)
	}
	return stats
}

// ParseDigOutput parses `dig +noall +answer +comments +stats` output.
func ParseDigOutput(output, target, recordType string) *DigStats {
	stats := &DigStats{Target: target, RecordType: recordType, Status: "down", Answers: []DigRecord{}}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if m := digStatusRegex.FindStringSubmatch(line); m != nil && strings.HasPrefix(line, ";;") {
			stats.RCode = m[1]
		} else if m := digQueryTimeRegex.FindStringSubmatch(line); m != nil {
			stats.QueryTimeMs, _ = strconv.Atoi(m[1])
		} else if m := digServerRegex.FindStringSubmatch(line); m != nil {
			stats.Server = m[1]
		} else if m := digRecordRegex.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, ";") {
			ttl, _ := strconv.Atoi(m[2])
			stats.Answers = append(stats.Answers, DigRecord{Name: m[1], TTL: ttl, Type: m[3], Value: strings.TrimSpace(m[4])})
		}
	}

	server := stats.Server
	if server == "" {
		server = "system resolver"
	}
	switch {
	case strings.Contains(output, digMissingMarker):
		stats.Summary = "dig is not installed on the source host. Install it (dnsutils / bind-utils / bind-tools) or use DNS Resolve instead."
		stats.SummaryID = "dig belum terpasang di host sumber. Pasang paket dnsutils / bind-utils / bind-tools, atau gunakan DNS Resolve."
	case stats.RCode == "":
		stats.Summary = fmt.Sprintf("No response from DNS server for %s %s (timeout or server unreachable).", target, recordType)
		stats.SummaryID = fmt.Sprintf("Tidak ada respons dari server DNS untuk %s %s (timeout atau server tidak terjangkau).", target, recordType)
	case stats.RCode != "NOERROR":
		stats.Summary = fmt.Sprintf("DNS query %s %s failed with %s (server %s).", target, recordType, stats.RCode, server)
		stats.SummaryID = fmt.Sprintf("Query DNS %s %s gagal dengan %s (server %s).", target, recordType, stats.RCode, server)
	case len(stats.Answers) == 0:
		stats.Status = "degraded"
		stats.Summary = fmt.Sprintf("%s exists but has no %s records (server %s, %d ms).", target, recordType, server, stats.QueryTimeMs)
		stats.SummaryID = fmt.Sprintf("%s ada tetapi tidak memiliki record %s (server %s, %d ms).", target, recordType, server, stats.QueryTimeMs)
	default:
		stats.Status = "healthy"
		stats.Summary = fmt.Sprintf("%d %s record(s) for %s from %s in %d ms.", len(stats.Answers), recordType, target, server, stats.QueryTimeMs)
		stats.SummaryID = fmt.Sprintf("%d record %s untuk %s dari %s dalam %d ms.", len(stats.Answers), recordType, target, server, stats.QueryTimeMs)
	}
	return stats
}
