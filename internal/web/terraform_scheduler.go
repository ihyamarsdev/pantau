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
				s.terraformPlanSchedulerTick(now)
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
		s.launchScheduledRun(&ws, &store.TFRun{
			WorkspaceID: ws.ID,
			Type:        "drift",
			Speculative: true,
			TriggerKind: "drift",
			Message:     "scheduled drift detection",
		}, "drift check")
	}
}

// terraformPlanSchedulerTick creates a `schedule` plan run for workspaces whose plan_interval_minutes elapsed.
func (s *Server) terraformPlanSchedulerTick(now time.Time) {
	if s.tfMgr == nil || !s.db.GetTerraformEnabled() {
		return
	}
	workspaces, err := s.db.ListTFWorkspaces("", "", "")
	if err != nil {
		logger.Warn("terraform", "", fmt.Sprintf("Plan scheduler failed to list workspaces: %v", err))
		return
	}
	for _, ws := range workspaces {
		if ws.PlanIntervalMinutes <= 0 || ws.Locked || s.tfMgr.HasActiveRunForWorkspace(ws.ID) {
			continue
		}
		last, err := s.db.LastTFScheduledRunAt(ws.ID)
		if err != nil || (last != nil && now.Before(last.Add(time.Duration(ws.PlanIntervalMinutes)*time.Minute))) {
			continue
		}
		s.launchScheduledRun(&ws, &store.TFRun{
			WorkspaceID: ws.ID,
			Type:        "plan",
			TriggerKind: "schedule",
			Message:     "scheduled plan",
		}, "plan")
	}
}

// launchScheduledRun locks the workspace, records the run and starts its plan; false = skipped or failed.
func (s *Server) launchScheduledRun(ws *store.TFWorkspace, run *store.TFRun, reason string) bool {
	locked, err := s.db.TryLockTFWorkspace(ws.ID, "run:pending", reason+" scheduled")
	if err != nil || !locked {
		return false
	}
	run.Status = "queued"
	runID, err := s.db.CreateTFRun(run)
	if err != nil {
		_ = s.db.UnlockTFWorkspace(ws.ID)
		return false
	}
	run.ID = runID

	// Update lock with actual run id
	_ = s.db.SetTFWorkspaceLock(ws.ID, fmt.Sprintf("run:%d", runID), reason)

	fullWS, err := s.db.GetTFWorkspace(ws.ID)
	if err != nil {
		_ = s.db.UnlockTFWorkspace(ws.ID)
		return false
	}
	if err := s.tfMgr.StartPlan(fullWS, run); err != nil {
		_ = s.db.FinishTFRun(runID, "errored", nil, err.Error())
		_ = s.db.UnlockTFWorkspace(ws.ID)
		return false
	}
	logger.Info("terraform", ws.Name, fmt.Sprintf("Triggered scheduled %s run %d", reason, runID))
	return true
}
