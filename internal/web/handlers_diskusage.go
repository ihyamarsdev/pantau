package web

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/sshrunner"
)

const (
	duMaxPathLen   = 4096
	duMaxEntries   = 500
	duExecTimeout  = 60 * time.Second
	duRemoteBudget = 55 // seconds; the remote `timeout` kills du before the Go-side deadline
)

// duInFlight keeps one du per host: a scan is heavy on the remote side, so a second
// request for the same host is rejected instead of piling up SSH sessions.
var duInFlight sync.Map // hostID -> struct{}

type duEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type duResult struct {
	Path       string    `json:"path"`
	TotalBytes int64     `json:"total_bytes"`
	Entries    []duEntry `json:"entries"`
	Denied     int       `json:"denied"`
	TimedOut   bool      `json:"timed_out"`
	Truncated  bool      `json:"truncated"`
	DurationMs int64     `json:"duration_ms"`
}

func (s *Server) registerDiskUsageRoutes() {
	s.mux.HandleFunc("/api/du", s.authMiddleware(s.handleDiskUsage))
}

// validateDuPath accepts only absolute, clean paths without NUL or newline characters.
func validateDuPath(raw string) (string, error) {
	// The raw value is checked before trimming: a trailing newline must not be silently dropped.
	if strings.ContainsAny(raw, "\x00\n\r") {
		return "", errors.New("path contains invalid characters")
	}
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("path is required")
	}
	if len(p) > duMaxPathLen {
		return "", fmt.Errorf("path too long (max %d bytes)", duMaxPathLen)
	}
	if !strings.HasPrefix(p, "/") {
		return "", errors.New("path must be absolute")
	}
	return path.Clean(p), nil
}

// duRunAsCmd mirrors the file manager's elevated access: `sudo -n -u <user> sh -c <script>`.
func duRunAsCmd(runAs, cmd string) string {
	if runAs == "" {
		return cmd
	}
	return fmt.Sprintf("sudo -n -u %s sh -c %s", shellQuote(runAs), shellQuote(cmd))
}

// duScript builds the remote scan. `du -d` only exists in coreutils >= 8.6, so the depth
// flag is probed once in the same script (one round trip): a du without -d complains about
// the option, every other du only complains about the missing probe path.
func duScript(dir string) string {
	q := shellQuote(dir)
	return fmt.Sprintf(`if du -d 0 /nonexistent-pantau-du-probe 2>&1 | grep -qi 'option'; then depth='--max-depth=1'; else depth='-d 1'; fi
if command -v timeout >/dev/null 2>&1; then timeout %d du -x -k $depth %s; else du -x -k $depth %s; fi`,
		duRemoteBudget, q, q)
}

// parseDuOutput turns `du -k` output into the total of the scanned directory and its
// direct children, largest first. Pure function: no I/O, no globals.
func parseDuOutput(stdout, basePath string) (total int64, entries []duEntry, truncated bool) {
	base := strings.TrimRight(basePath, "/")
	if base == "" {
		base = "/"
	}
	total = -1
	var all []duEntry
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		tab := strings.Index(line, "\t")
		if tab <= 0 {
			continue
		}
		kib, err := strconv.ParseInt(strings.TrimSpace(line[:tab]), 10, 64)
		if err != nil {
			continue
		}
		p := line[tab+1:]
		if p == "" {
			continue
		}
		if p == base || (base != "/" && p == base+"/") {
			total = kib * 1024
			continue
		}
		if path.Dir(p) != base {
			continue
		}
		all = append(all, duEntry{Name: path.Base(p), Path: p, Bytes: kib * 1024})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Bytes > all[j].Bytes })
	if len(all) > duMaxEntries {
		all = all[:duMaxEntries]
		truncated = true
	}
	if total < 0 { // no total line (unusual du build): approximate with the children
		var sum int64
		for _, e := range all {
			sum += e.Bytes
		}
		total = sum
	}
	return total, all, truncated
}

// countDuDenied counts "Permission denied" lines du wrote to stderr.
func countDuDenied(stderr string) int {
	n := 0
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(strings.ToLower(line), "permission denied") {
			n++
		}
	}
	return n
}

func (s *Server) handleDiskUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("host_id")), 10, 64)
	if err != nil || hostID <= 0 {
		http.Error(w, "host_id is required", http.StatusBadRequest)
		return
	}
	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	dir, err := validateDuPath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	runAs := strings.TrimSpace(r.URL.Query().Get("as"))
	if runAs != "" {
		if err := sshrunner.ValidateRunAsUser(runAs); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if _, loaded := duInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error": "a disk usage scan is already running for this host",
		})
		return
	}
	defer duInFlight.Delete(hostID)

	if s.inspector == nil {
		http.Error(w, "inspector not available", http.StatusInternalServerError)
		return
	}
	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer runner.Close()

	start := time.Now()
	stdout, stderr, exitCode, execErr := runner.ExecTimeout(duRunAsCmd(runAs, duScript(dir)), duExecTimeout)
	duration := time.Since(start).Milliseconds()

	timedOut := exitCode == 124 // remote `timeout` killed du
	if execErr != nil {
		if strings.Contains(execErr.Error(), "timed out") {
			timedOut = true
		} else {
			http.Error(w, execErr.Error(), http.StatusInternalServerError)
			return
		}
	}
	total, entries, truncated := parseDuOutput(stdout, dir)
	writeJSON(w, http.StatusOK, duResult{
		Path:       dir,
		TotalBytes: total,
		Entries:    entries,
		Denied:     countDuDenied(stderr),
		TimedOut:   timedOut,
		Truncated:  truncated,
		DurationMs: duration,
	})
}
