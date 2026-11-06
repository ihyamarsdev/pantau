package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// MaxConcurrentAdhocRuns bounds ad-hoc executions; they are short and ad-hoc, so they get a separate budget from
// playbook runs.
const MaxConcurrentAdhocRuns = 3

type activeAdhoc struct {
	cancel context.CancelFunc
	logBuf *logBuffer
}

// AnsibleAdhocManager runs ad-hoc modules concurrently without a playbook. Logs stream from memory while a run is
// active and come from the DB once FinishAnsibleAdhocRun stored them.
type AnsibleAdhocManager struct {
	db     *store.DB
	runner func(ctx context.Context, spec ansible.AdhocSpec, log io.Writer) (int, error)

	mu     sync.Mutex
	active map[int64]*activeAdhoc
}

func NewAnsibleAdhocManager(db *store.DB, runner func(ctx context.Context, spec ansible.AdhocSpec, log io.Writer) (int, error)) *AnsibleAdhocManager {
	if runner == nil {
		runner = ansible.RunAdhoc
	}
	return &AnsibleAdhocManager{db: db, runner: runner, active: make(map[int64]*activeAdhoc)}
}

// Start registers the run and launches it; returns an error when MaxConcurrentAdhocRuns are active.
func (m *AnsibleAdhocManager) Start(run *store.AnsibleAdhocRun, spec ansible.AdhocSpec) error {
	m.mu.Lock()
	if len(m.active) >= MaxConcurrentAdhocRuns {
		m.mu.Unlock()
		return fmt.Errorf("too many concurrent ad-hoc runs (max %d)", MaxConcurrentAdhocRuns)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ar := &activeAdhoc{cancel: cancel, logBuf: newLogBuffer()}
	m.active[run.ID] = ar
	m.mu.Unlock()

	if err := m.db.StartAnsibleAdhocRun(run.ID); err != nil {
		logger.Warn("ansible", fmt.Sprintf("adhoc %d", run.ID), fmt.Sprintf("Failed to mark run as running: %v", err))
	}
	logger.Info("ansible", fmt.Sprintf("adhoc %d", run.ID), fmt.Sprintf("Starting ad-hoc %s on %d targets", spec.Module, len(spec.Targets)))

	go m.execute(ctx, run, spec, ar)
	return nil
}

func (m *AnsibleAdhocManager) execute(ctx context.Context, run *store.AnsibleAdhocRun, spec ansible.AdhocSpec, ar *activeAdhoc) {
	defer func() {
		ar.cancel()
		m.mu.Lock()
		delete(m.active, run.ID)
		m.mu.Unlock()
	}()

	exitCode, err := m.runner(ctx, spec, ar.logBuf)

	status := "success"
	var exit *int
	switch {
	case err != nil && ctx.Err() == context.Canceled:
		status = "cancelled"
	case err != nil:
		status = "failed"
		_, _ = fmt.Fprintf(ar.logBuf, "\n[pantau] run error: %v\n", err)
	default:
		exit = &exitCode
		if exitCode != 0 {
			status = "failed"
		}
	}

	finalLog := ar.logBuf.String()
	resultsJSON := ""
	if results := ansible.ParseAdhoc(finalLog); len(results) > 0 {
		if b, marshalErr := json.Marshal(results); marshalErr == nil {
			resultsJSON = string(b)
		}
	}

	if dbErr := m.db.FinishAnsibleAdhocRun(run.ID, status, exit, finalLog, resultsJSON); dbErr != nil {
		logger.Warn("ansible", fmt.Sprintf("adhoc %d", run.ID), fmt.Sprintf("Failed to finish run: %v", dbErr))
	}
	if pruneErr := m.db.PruneAnsibleAdhocRuns(100); pruneErr != nil {
		logger.Warn("ansible", fmt.Sprintf("adhoc %d", run.ID), fmt.Sprintf("Failed to prune runs: %v", pruneErr))
	}
	logger.Info("ansible", fmt.Sprintf("adhoc %d", run.ID), fmt.Sprintf("Finished ad-hoc run with status %s", status))
}

// Cancel stops an active run; false when it is not running.
func (m *AnsibleAdhocManager) Cancel(runID int64) bool {
	m.mu.Lock()
	ar, ok := m.active[runID]
	m.mu.Unlock()
	if !ok {
		return false
	}
	ar.cancel()
	logger.Info("ansible", fmt.Sprintf("adhoc %d", runID), "Cancelled ad-hoc run")
	return true
}

// ReadLog streams the live buffer while the run is active, then the stored log.
func (m *AnsibleAdhocManager) ReadLog(runID int64, offset int) (data string, next int, status string, finished bool, err error) {
	m.mu.Lock()
	ar, active := m.active[runID]
	m.mu.Unlock()
	if active {
		data, next = ar.logBuf.ReadFrom(offset)
		return data, next, "running", false, nil
	}

	run, err := m.db.GetAnsibleAdhocRun(runID)
	if err != nil {
		return "", 0, "", false, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(run.Log) {
		return "", len(run.Log), run.Status, true, nil
	}
	return run.Log[offset:], len(run.Log), run.Status, true, nil
}
