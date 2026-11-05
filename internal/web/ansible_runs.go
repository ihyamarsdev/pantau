package web

import (
	"context"
	"fmt"
	"io"
	"sync"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	maxLogBufferBytes   = 2 * 1024 * 1024 // 2 MiB
	logTruncationMarker = "\n[pantau] log truncated\n"
)

// logBuffer is an in-memory buffer capped at 2 MiB.
// If writes exceed 2 MiB, the older data at the front is truncated,
// and the logTruncationMarker is inserted at the front.
type logBuffer struct {
	mu        sync.RWMutex
	buf       []byte
	truncated bool
}

func newLogBuffer() *logBuffer {
	return &logBuffer{buf: make([]byte, 0, 4096)}
}

func (lb *logBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.buf = append(lb.buf, p...)
	if len(lb.buf) > maxLogBufferBytes {
		excess := len(lb.buf) - maxLogBufferBytes
		marker := []byte(logTruncationMarker)
		// Retain the tail end of the buffer
		cutPoint := excess + len(marker)
		if cutPoint >= len(lb.buf) {
			cutPoint = len(lb.buf) - 1
			if cutPoint < 0 {
				cutPoint = 0
			}
		}
		newBuf := make([]byte, 0, maxLogBufferBytes)
		newBuf = append(newBuf, marker...)
		newBuf = append(newBuf, lb.buf[cutPoint:]...)
		lb.buf = newBuf
		lb.truncated = true
	}
	return len(p), nil
}

func (lb *logBuffer) ReadFrom(offset int) (data string, nextOffset int) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if offset < 0 {
		offset = 0
	}
	totalLen := len(lb.buf)
	if offset >= totalLen {
		return "", totalLen
	}
	chunk := string(lb.buf[offset:])
	return chunk, totalLen
}

func (lb *logBuffer) String() string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return string(lb.buf)
}

type activeRun struct {
	runID      int64
	playbookID int64
	cancel     context.CancelFunc
	logBuf     *logBuffer
	done       chan struct{}
}

// AnsibleRunManager coordinates running playbooks, enforcing concurrency limits,
// streaming logs in memory, and updating DB status.
type AnsibleRunManager struct {
	db     *store.DB
	runner func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error)

	mu         sync.Mutex
	activeRuns map[int64]*activeRun // runID -> activeRun
	byPlaybook map[int64]int64      // playbookID -> runID
}

func NewAnsibleRunManager(db *store.DB, runner func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error)) *AnsibleRunManager {
	if runner == nil {
		runner = ansible.Run
	}
	return &AnsibleRunManager{
		db:         db,
		runner:     runner,
		activeRuns: make(map[int64]*activeRun),
		byPlaybook: make(map[int64]int64),
	}
}

// CanStartRun checks concurrency limits before creating the run record in DB.
func (m *AnsibleRunManager) CanStartRun(playbookID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.activeRuns) >= ansible.MaxConcurrentRuns {
		return fmt.Errorf("too many concurrent runs (max %d)", ansible.MaxConcurrentRuns)
	}
	if activeID, exists := m.byPlaybook[playbookID]; exists {
		return fmt.Errorf("playbook already has an active run (run %d)", activeID)
	}
	return nil
}

// StartRun registers and launches an ansible run in a background goroutine.
// Returns 409 error if playbook has an active run, 429 error if 3 active runs.
func (m *AnsibleRunManager) StartRun(run *store.AnsibleRun, spec ansible.RunSpec) error {
	m.mu.Lock()
	if len(m.activeRuns) >= ansible.MaxConcurrentRuns {
		m.mu.Unlock()
		return fmt.Errorf("too many concurrent runs (max %d)", ansible.MaxConcurrentRuns)
	}
	if activeID, exists := m.byPlaybook[run.PlaybookID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("playbook already has an active run (run %d)", activeID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	logBuf := newLogBuffer()
	ar := &activeRun{
		runID:      run.ID,
		playbookID: run.PlaybookID,
		cancel:     cancel,
		logBuf:     logBuf,
		done:       make(chan struct{}),
	}
	m.activeRuns[run.ID] = ar
	m.byPlaybook[run.PlaybookID] = run.ID
	m.mu.Unlock()

	// Update DB to running
	if err := m.db.StartAnsibleRun(run.ID); err != nil {
		logger.Warn("ansible", run.PlaybookName, fmt.Sprintf("Failed to mark run %d as running: %v", run.ID, err))
	}

	logger.Info("ansible", run.PlaybookName, fmt.Sprintf("Starting playbook run %d (%s mode)", run.ID, run.Mode))

	if spec.Git != nil && spec.OnCommit == nil {
		runID := run.ID
		spec.OnCommit = func(sha string) {
			if err := m.db.SetAnsibleRunCommit(runID, sha); err != nil {
				logger.Warn("ansible", run.PlaybookName, fmt.Sprintf("Failed to record commit for run %d: %v", runID, err))
			}
		}
	}

	go m.executeRun(ctx, ar, run, spec)
	return nil
}

func (m *AnsibleRunManager) executeRun(ctx context.Context, ar *activeRun, run *store.AnsibleRun, spec ansible.RunSpec) {
	defer close(ar.done)
	defer func() {
		m.mu.Lock()
		delete(m.activeRuns, ar.runID)
		if m.byPlaybook[ar.playbookID] == ar.runID {
			delete(m.byPlaybook, ar.playbookID)
		}
		m.mu.Unlock()
	}()

	runnerFunc := m.runner
	if runnerFunc == nil {
		runnerFunc = ansible.Run
	}

	exitCode, err := runnerFunc(ctx, spec, ar.logBuf)

	var finalStatus string
	var finalExit *int

	if err != nil {
		if ctx.Err() == context.Canceled {
			finalStatus = "cancelled"
		} else {
			finalStatus = "failed"
			_, _ = io.WriteString(ar.logBuf, fmt.Sprintf("\n[pantau] run error: %v\n", err))
		}
	} else {
		finalExit = &exitCode
		if exitCode == 0 {
			finalStatus = "success"
		} else {
			finalStatus = "failed"
		}
	}

	finalLog := ar.logBuf.String()
	if dbErr := m.db.FinishAnsibleRun(ar.runID, finalStatus, finalExit, finalLog); dbErr != nil {
		logger.Warn("ansible", run.PlaybookName, fmt.Sprintf("Failed to finish run %d in db: %v", ar.runID, dbErr))
	}

	// Prune older finished runs for this playbook (keep 100)
	if pruneErr := m.db.PruneAnsibleRuns(ar.playbookID, 100); pruneErr != nil {
		logger.Warn("ansible", run.PlaybookName, fmt.Sprintf("Failed to prune runs for playbook %d: %v", ar.playbookID, pruneErr))
	}

	logger.Info("ansible", run.PlaybookName, fmt.Sprintf("Finished playbook run %d with status %s", ar.runID, finalStatus))
}

// CancelRun attempts to cancel an active run.
func (m *AnsibleRunManager) CancelRun(runID int64) bool {
	m.mu.Lock()
	ar, exists := m.activeRuns[runID]
	m.mu.Unlock()

	if !exists {
		return false
	}
	ar.cancel()
	logger.Info("ansible", "", fmt.Sprintf("Cancelled playbook run %d", runID))
	return true
}

// HasActiveRun checks if a run is currently active.
func (m *AnsibleRunManager) HasActiveRun(runID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.activeRuns[runID]
	return exists
}

// HasActiveRunForPlaybook checks if a playbook has an active run.
func (m *AnsibleRunManager) HasActiveRunForPlaybook(playbookID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.byPlaybook[playbookID]
	return exists
}

// ReadLog reads log output from memory buffer if run is active, or from DB if finished.
func (m *AnsibleRunManager) ReadLog(runID int64, offset int) (data string, nextOffset int, status string, finished bool, err error) {
	m.mu.Lock()
	ar, active := m.activeRuns[runID]
	m.mu.Unlock()

	if active {
		data, nextOffset = ar.logBuf.ReadFrom(offset)
		return data, nextOffset, "running", false, nil
	}

	// If not active, read from DB
	run, err := m.db.GetAnsibleRun(runID)
	if err != nil {
		return "", 0, "", false, err
	}
	if run == nil {
		return "", 0, "", false, store.ErrAnsibleRunNotFound
	}

	logStr := run.Log
	totalLen := len(logStr)
	if offset < 0 {
		offset = 0
	}
	if offset >= totalLen {
		return "", totalLen, run.Status, true, nil
	}
	chunk := logStr[offset:]
	return chunk, totalLen, run.Status, true, nil
}
