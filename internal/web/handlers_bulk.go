package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

type bulkExecRequest struct {
	HostIDs    []int64 `json:"host_ids"`
	Command    string  `json:"command"`
	TimeoutSec int     `json:"timeout_sec"`
	Sudo       bool    `json:"sudo"`
}

type bulkHostResult struct {
	HostID     int64  `json:"host_id"`
	HostName   string `json:"host_name"`
	OK         bool   `json:"ok"`
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type bulkDoneResult struct {
	Done   bool `json:"done"`
	Total  int  `json:"total"`
	Failed int  `json:"failed"`
}

// BuildBulkCommand wraps command for non-interactive execution:
// sh -c <command> with shellQuote, DarwinPathPrelude for macOS.
// If sudo is true, uses `sudo -n` only (fails if sudo requires password).
func BuildBulkCommand(cmd string, sudo bool) string {
	script := inspector.DarwinPathPrelude
	if sudo {
		script += `if [ "$(id -u)" != "0" ]; then
  if sudo -n true 2>/dev/null; then
    sudo -n sh -c ` + shellQuote(cmd) + `
    exit $?
  else
    echo "sudo without password required but not available" >&2
    exit 1
  fi
else
  sh -c ` + shellQuote(cmd) + `
  exit $?
fi
`
	} else {
		script += "sh -c " + shellQuote(cmd) + "\n"
	}

	return "sh -c " + shellQuote(script)
}

func truncateOutput64KB(s string) (string, bool) {
	const maxBytes = 64 * 1024
	if len(s) > maxBytes {
		return s[:maxBytes], true
	}
	return s, false
}

func (s *Server) handleExecBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	var req bulkExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.HostIDs) < 1 || len(req.HostIDs) > 20 {
		http.Error(w, "host_ids count must be between 1 and 20", http.StatusBadRequest)
		return
	}

	if len(req.Command) < 1 || len(req.Command) > 4096 || strings.ContainsRune(req.Command, 0) {
		http.Error(w, "command must be between 1 and 4096 bytes and contain no NUL bytes", http.StatusBadRequest)
		return
	}

	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 30
	} else if req.TimeoutSec > 120 {
		req.TimeoutSec = 120
	}

	// Validate duplicate IDs and verify existence of all hosts up front
	seen := make(map[int64]bool)
	hosts := make([]*store.Host, 0, len(req.HostIDs))
	for _, id := range req.HostIDs {
		if id <= 0 || seen[id] {
			http.Error(w, fmt.Sprintf("invalid or duplicate host_id: %d", id), http.StatusBadRequest)
			return
		}
		seen[id] = true

		h, err := s.db.GetHost(id)
		if err != nil || h == nil {
			http.Error(w, fmt.Sprintf("host not found: %d", id), http.StatusBadRequest)
			return
		}
		hosts = append(hosts, h)
	}

	// Set NDJSON headers
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	concurrency := 5
	sem := make(chan struct{}, concurrency)
	resultsChan := make(chan bulkHostResult, len(hosts))

	var wg sync.WaitGroup
	timeoutDuration := time.Duration(req.TimeoutSec) * time.Second

	for _, h := range hosts {
		// If client cancelled, abort queued hosts
		if ctx.Err() != nil {
			break
		}

		// Check Kube Mode guard
		if (h.KubeMode == "no-exec" || h.KubeMode == "read-only") && isKubeExecCommand(req.Command) {
			resultsChan <- bulkHostResult{
				HostID:   h.ID,
				HostName: h.Name,
				OK:       false,
				ExitCode: 126,
				Error:    "blocked by Kube Mode",
			}
			continue
		}

		wg.Add(1)
		go func(targetHost *store.Host) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			start := time.Now()
			res := s.runBulkHost(ctx, targetHost, req.Command, req.Sudo, timeoutDuration)
			res.DurationMS = time.Since(start).Milliseconds()
			resultsChan <- res
		}(h)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var mu sync.Mutex
	encoder := json.NewEncoder(w)

	totalCount := len(hosts)
	processedCount := 0
	failedCount := 0

	for res := range resultsChan {
		processedCount++
		if !res.OK {
			failedCount++
		}

		mu.Lock()
		_ = encoder.Encode(res)
		flusher.Flush()
		mu.Unlock()
	}

	// Done line
	doneRes := bulkDoneResult{
		Done:   true,
		Total:  totalCount,
		Failed: failedCount + (totalCount - processedCount),
	}

	mu.Lock()
	_ = encoder.Encode(doneRes)
	flusher.Flush()
	mu.Unlock()

	// Activity Console logging
	cmdSnippet := req.Command
	if len(cmdSnippet) > 200 {
		cmdSnippet = cmdSnippet[:200] + "..."
	}
	logger.Info("exec", "", fmt.Sprintf("Bulk command to %d host(s) completed (failed: %d): %s", totalCount, doneRes.Failed, cmdSnippet),
		"hosts", totalCount,
		"failed", doneRes.Failed,
	)
}

func (s *Server) runBulkHost(ctx context.Context, h *store.Host, userCmd string, sudo bool, timeout time.Duration) bulkHostResult {
	runner, err := s.inspector.GetRunnerForHost(h)
	if err != nil {
		return bulkHostResult{
			HostID:   h.ID,
			HostName: h.Name,
			OK:       false,
			ExitCode: 1,
			Error:    fmt.Sprintf("ssh connect failed: %v", err),
		}
	}
	defer runner.Close()

	wrappedCmd := BuildBulkCommand(userCmd, sudo)

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type execRes struct {
		stdout   string
		stderr   string
		exitCode int
		err      error
	}

	doneChan := make(chan execRes, 1)
	go func() {
		stdout, stderr, exitCode, err := runner.ExecTimeout(wrappedCmd, timeout)
		doneChan <- execRes{stdout: stdout, stderr: stderr, exitCode: exitCode, err: err}
	}()

	select {
	case <-execCtx.Done():
		_ = runner.Close()
		errMsg := "command timed out"
		if ctx.Err() != nil {
			errMsg = "client disconnected"
		}
		return bulkHostResult{
			HostID:   h.ID,
			HostName: h.Name,
			OK:       false,
			ExitCode: 124,
			Error:    errMsg,
		}
	case r := <-doneChan:
		out := r.stdout
		if r.stderr != "" {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += r.stderr
		}

		truncOut, isTruncated := truncateOutput64KB(out)
		ok := r.err == nil && r.exitCode == 0
		var errStr string
		if !ok {
			if r.err != nil {
				errStr = r.err.Error()
			} else {
				errStr = fmt.Sprintf("exit code %d", r.exitCode)
			}
		}

		return bulkHostResult{
			HostID:    h.ID,
			HostName:  h.Name,
			OK:        ok,
			ExitCode:  r.exitCode,
			Output:    truncOut,
			Truncated: isTruncated,
			Error:     errStr,
		}
	}
}
