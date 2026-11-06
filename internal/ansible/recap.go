package ansible

import (
	"regexp"
	"strconv"
	"strings"
)

// RecapHost is one host's PLAY RECAP counters.
type RecapHost struct {
	OK          int `json:"ok"`
	Changed     int `json:"changed"`
	Unreachable int `json:"unreachable"`
	Failed      int `json:"failed"`
	Skipped     int `json:"skipped"`
	Rescued     int `json:"rescued"`
	Ignored     int `json:"ignored"`
}

var (
	recapANSIMatch = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")
	recapMark      = "PLAY RECAP"
	recapHosts     = regexp.MustCompile(`^(\S+)\s+:\s+(.*)$`)
	recapField     = regexp.MustCompile(`(ok|changed|unreachable|failed|skipped|rescued|ignored)=(\d+)`)
)

// ParseRecap extracts the PLAY RECAP block (the last one wins: Ansible prints it once per playbook)
// and returns alias -> counters. ANSI escapes are stripped first. Missing keys are zero.
func ParseRecap(log string) map[string]RecapHost {
	recap := map[string]RecapHost{}
	// Everything before the last "PLAY RECAP" is irrelevant; Ansible's task output can't contain the marker at column 0.
	idx := strings.LastIndex(log, recapMark)
	if idx < 0 {
		return recap
	}
	for _, line := range strings.Split(log[idx:], "\n") {
		line = strings.TrimSpace(recapANSIMatch.ReplaceAllString(line, ""))
		m := recapHosts.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var h RecapHost
		for _, f := range recapField.FindAllStringSubmatch(m[2], -1) {
			n, _ := strconv.Atoi(f[2])
			switch f[1] {
			case "ok":
				h.OK = n
			case "changed":
				h.Changed = n
			case "unreachable":
				h.Unreachable = n
			case "failed":
				h.Failed = n
			case "skipped":
				h.Skipped = n
			case "rescued":
				h.Rescued = n
			case "ignored":
				h.Ignored = n
			}
		}
		recap[m[1]] = h
	}
	return recap
}

// RecapTotals sums recap entries. ok/changed/skipped are excluded from 4xx-style "bad" counts on purpose.
func RecapTotals(recap map[string]RecapHost) (ok, changed, unreachable, failed, skipped int) {
	for _, h := range recap {
		ok += h.OK
		changed += h.Changed
		unreachable += h.Unreachable
		failed += h.Failed
		skipped += h.Skipped
	}
	return
}
