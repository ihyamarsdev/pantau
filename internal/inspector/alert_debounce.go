package inspector

import (
	"strconv"
	"strings"
	"sync"
)

// ruleKey identifies a rule instance on a specific host.
type ruleKey struct {
	hostID int64
	kind   string
	target string
}

// AlertDebouncer tracks consecutive drift observations per (host, rule).
type AlertDebouncer struct {
	mu     sync.Mutex
	counts map[ruleKey]int
}

// NewAlertDebouncer constructs an empty in-memory AlertDebouncer.
func NewAlertDebouncer() *AlertDebouncer {
	return &AlertDebouncer{
		counts: make(map[ruleKey]int),
	}
}

// ParseDebounceRuns parses the alert_debounce_runs setting into 1..10 (default 1).
func ParseDebounceRuns(val string) int {
	v, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || v < 1 {
		return 1
	}
	if v > 10 {
		return 10
	}
	return v
}

// Observe records an observation of a rule's status.
// It returns shouldAlert=true if status is "drift" and the consecutive drift count reaches runs.
// If status is not "drift" (e.g. "ok"), the counter is cleared immediately.
// If runs <= 1, every drift alert triggers immediately (when transitioning or count == 1).
func (ad *AlertDebouncer) Observe(hostID int64, kind, target, status string, runs int) bool {
	if ad == nil {
		return status == "drift"
	}
	if runs < 1 {
		runs = 1
	} else if runs > 10 {
		runs = 10
	}

	key := ruleKey{hostID: hostID, kind: kind, target: target}

	ad.mu.Lock()
	defer ad.mu.Unlock()

	if status != "drift" {
		delete(ad.counts, key)
		return false
	}

	ad.counts[key]++
	return ad.counts[key] == runs
}

// Reset clears the counter for a specific (host, rule).
func (ad *AlertDebouncer) Reset(hostID int64, kind, target string) {
	if ad == nil {
		return
	}
	ad.mu.Lock()
	delete(ad.counts, ruleKey{hostID: hostID, kind: kind, target: target})
	ad.mu.Unlock()
}
