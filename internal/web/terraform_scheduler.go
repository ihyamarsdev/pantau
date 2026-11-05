package web

import (
	"context"
	"fmt"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	terraformDriftSchedulerInterval = 60 * time.Second
)

// StartTerraformDriftScheduler runs background drift checks every 60s for workspaces with drift_interval_minutes > 0.
func (s *Server) StartTerraformDriftScheduler(ctx context.Context) {
	if s.db == nil {
		return
	}
	go func() {
		t := time.NewTicker(terraformDriftSchedulerInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.terraformDriftSchedulerTick(now)
			}
		}
	}()
}

func (s *Server) terraformDriftSchedulerTick(now time.Time) {
	if !s.db.GetTerraformEnabled() {
		return
	}
	if s.tfMgr == nil {
		return
	}

	workspaces, err := s.db.ListTFWorkspaces("", "", "")
	if err != nil {
		logger.Warn("terraform", "", fmt.Sprintf("Drift scheduler failed to list workspaces: %v", err))
		return
	}

	for _, ws := range workspaces {
		if ws.DriftIntervalMinutes <= 0 {
			continue
		}
		if ws.Locked {
			continue
		}
		if s.tfMgr.HasActiveRunForWorkspace(ws.ID) {
			continue
		}

		// Check if overdue
		if ws.DriftCheckedAt != nil {
			interval := time.Duration(ws.DriftIntervalMinutes) * time.Minute
			if now.Before(ws.DriftCheckedAt.Add(interval)) {
				continue
			}
		}

		// Overdue: trigger drift check run
		locked, err := s.db.TryLockTFWorkspace(ws.ID, "run:pending", "drift check scheduled")
		if err != nil || !locked {
			continue
		}

		run := &store.TFRun{
			WorkspaceID: ws.ID,
			Type:        "drift",
			Speculative: true,
			Status:      "queued",
			TriggerKind: "drift",
			Message:     "scheduled drift detection",
		}
		runID, err := s.db.CreateTFRun(run)
		if err != nil {
			_ = s.db.UnlockTFWorkspace(ws.ID)
			continue
		}
		run.ID = runID

		// Update lock with actual run id
		_ = s.db.SetTFWorkspaceLock(ws.ID, fmt.Sprintf("run:%d", runID), "drift check")

		fullWS, err := s.db.GetTFWorkspace(ws.ID)
		if err != nil {
			_ = s.db.UnlockTFWorkspace(ws.ID)
			continue
		}

		if err := s.tfMgr.StartPlan(fullWS, run); err != nil {
			_ = s.db.FinishTFRun(runID, "errored", nil, err.Error())
			_ = s.db.UnlockTFWorkspace(ws.ID)
			continue
		}

		logger.Info("terraform", ws.Name, fmt.Sprintf("Triggered scheduled drift check run %d", runID))
	}
}
