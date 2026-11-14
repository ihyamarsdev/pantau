package osv

import (
	"regexp"
	"strings"
)

// Source identifies which public vulnerability feed answers a host.
const (
	SourceOSV  = "osv"  // api.osv.dev (Debian, Ubuntu, Alpine)
	SourceArch = "arch" // Arch Linux Security Tracker (security.archlinux.org)
)

// Ecosystem is the OSV ecosystem string Pantau queries for a host, plus the feed that serves it.
type Ecosystem struct {
	Source string // SourceOSV or SourceArch
	Name   string // OSV ecosystem string, e.g. "Debian:12", "Alpine:v3.18", "Arch"
}

// Supported release lists, verified against the OSV data on 2026-11-14.
// A release outside these lists has no OSV data: Pantau reports "not assessed" instead of
// pretending the host is clean. The lists are extended when a new release appears in OSV.
// Source: https://google.github.io/osv.dev/data/ (covered ecosystems) and per-release probes.
var (
	debianMajors = map[int]bool{7: true, 8: true, 9: true, 10: true, 11: true, 12: true, 13: true, 14: true}
	ubuntuVers   = map[string]bool{
		"14.04": true, "16.04": true, "18.04": true, "20.04": true, "22.04": true, "24.04": true, "26.04": true,
		"23.10": true, "24.10": true, "25.04": true, "25.10": true,
	}
	alpineVers = map[string]bool{
		"3.2": true, "3.3": true, "3.4": true, "3.5": true, "3.6": true, "3.7": true, "3.8": true, "3.9": true,
		"3.10": true, "3.11": true, "3.12": true, "3.13": true, "3.14": true, "3.15": true, "3.16": true,
		"3.17": true, "3.18": true, "3.19": true, "3.20": true, "3.21": true, "3.22": true, "3.23": true,
		"3.24": true,
	}
)

var (
	debianRe = regexp.MustCompile(`Debian GNU/Linux\s+(\d+)`)
	ubuntuRe = regexp.MustCompile(`Ubuntu\s+(\d+\.\d+)`)
	alpineRe = regexp.MustCompile(`Alpine Linux\s+v(\d+\.\d+)`)
	archRe   = regexp.MustCompile(`Arch Linux`)
)

// EcosystemFor maps the host's OS description (the PRETTY_NAME Pantau already stores in
// hosts.os_info) to the OSV ecosystem to query.
//
// Returns ok=false with a reason when the host runs a distribution Pantau cannot map or a
// release OSV has no data for; the caller reports "not assessed" instead of an empty result.
func EcosystemFor(osInfo string) (Ecosystem, bool, string) {
	info := strings.TrimSpace(osInfo)
	switch {
	case archRe.MatchString(info):
		// Pacman is not an OSV ecosystem; the Arch Linux Security Tracker serves it.
		return Ecosystem{Source: SourceArch, Name: "Arch"}, true, ""
	case debianRe.MatchString(info):
		m := debianRe.FindStringSubmatch(info)
		major := 0
		for _, c := range m[1] {
			if c < '0' || c > '9' {
				return Ecosystem{}, false, "unreadable Debian release in OS description"
			}
			major = major*10 + int(c-'0')
		}
		if !debianMajors[major] {
			return Ecosystem{}, false, "OSV has no data for Debian " + m[1]
		}
		return Ecosystem{Source: SourceOSV, Name: "Debian:" + m[1]}, true, ""
	case ubuntuRe.MatchString(info):
		v := ubuntuRe.FindStringSubmatch(info)[1]
		if !ubuntuVers[v] {
			return Ecosystem{}, false, "OSV has no data for Ubuntu " + v
		}
		// The plain release ecosystem ("Ubuntu:22.04") is a superset of the ":LTS" one
		// (verified 2026-11-14), so it covers LTS hosts without a second query.
		return Ecosystem{Source: SourceOSV, Name: "Ubuntu:" + v}, true, ""
	case alpineRe.MatchString(info):
		v := alpineRe.FindStringSubmatch(info)[1]
		if !alpineVers[v] {
			return Ecosystem{}, false, "OSV has no data for Alpine " + v
		}
		return Ecosystem{Source: SourceOSV, Name: "Alpine:v" + v}, true, ""
	case info == "":
		return Ecosystem{}, false, "host OS is unknown (never inspected)"
	default:
		return Ecosystem{}, false, "no vulnerability feed covers this operating system"
	}
}
