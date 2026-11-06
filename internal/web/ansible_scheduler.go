package web

import (
	"context"
	"fmt"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
)

const (
	ansibleSchedulerInterval = 15 * time.Second
	// An occurrence overdue by more than this (Pantau was down, or the Ansible Gate was off) is skipped instead of
	// fired late: waking up must not replay a backlog of mutating runs.
	ansibleMissedGrace = 5 * time.Minute
)

// StartAnsibleScheduler fires due Ansible schedules until ctx is cancelled.
func (s *Server) StartAnsibleScheduler(ctx context.Context) {
	if s.db == nil {
		return
	}
	go func() {
		t := time.NewTicker(ansibleSchedulerInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.ansibleSchedulerTick(now)
			}
		}
	}()
}

// ansibleSchedulerTick evaluates every enabled schedule once. Schedules are evaluated in server local time.
func (s *Server) ansibleSchedulerTick(now time.Time) {
	if !s.db.GetAnsibleEnabled() {
		return
	}
	avail := s.ansibleAvail
	if avail == nil {
		avail = ansible.Available
	}
	if _, _, err := avail(); err != nil {
		return
	}
	list, err := s.db.ListAnsibleSchedules(0)
	if err != nil {
		logger.Warn("ansible", "", fmt.Sprintf("Scheduler: failed to list schedules: %v", err))
		return
	}

	for _, sc := range list {
		if !sc.Enabled {
			continue
		}
		cr, err := ansible.ParseCron(sc.Cron)
		if err != nil {
			_ = s.db.RecordAnsibleScheduleFire(sc.ID, nil, 0, "invalid cron: "+err.Error(), nil)
			continue
		}
		var next *time.Time
		if n := cr.Next(now); !n.IsZero() {
			n = n.UTC()
			next = &n
		}
		if sc.NextRunAt == nil { // freshly enabled / restored: plan the first occurrence
			_ = s.db.SetAnsibleScheduleNext(sc.ID, next)
			continue
		}
		if sc.NextRunAt.After(now) {
			continue
		}
		if now.Sub(*sc.NextRunAt) > ansibleMissedGrace {
			msg := fmt.Sprintf("missed occurrence at %s skipped", sc.NextRunAt.Local().Format("2006-01-02 15:04"))
			_ = s.db.RecordAnsibleScheduleFire(sc.ID, nil, 0, msg, next)
			logger.Warn("ansible", sc.PlaybookName, fmt.Sprintf("Schedule %d: %s", sc.ID, msg))
			continue
		}

		pb, err := s.db.GetAnsiblePlaybook(sc.PlaybookID)
		if err != nil {
			_ = s.db.RecordAnsibleScheduleFire(sc.ID, &now, 0, "playbook not found", next)
			continue
		}
		run, lerr := s.launchAnsibleRun(pb, ansibleRunParams{
			Mode: sc.Mode, HostIDs: sc.HostIDs, Group: sc.Group, ExtraVars: sc.ExtraVars, Tags: sc.Tags, SkipTags: sc.SkipTags,
			Limit: sc.Limit, Verbosity: sc.Verbosity, Become: sc.Become, Trigger: "schedule", ScheduleID: sc.ID,
		})
		if lerr != nil {
			_ = s.db.RecordAnsibleScheduleFire(sc.ID, &now, 0, "not started: "+lerr.Msg, next)
			logger.Warn("ansible", sc.PlaybookName, fmt.Sprintf("Schedule %d not started: %s", sc.ID, lerr.Msg))
			continue
		}
		_ = s.db.RecordAnsibleScheduleFire(sc.ID, &now, run.ID, "", next)
		logger.Info("ansible", sc.PlaybookName, fmt.Sprintf("Schedule %d started run %d", sc.ID, run.ID))
	}
}
