package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

const maxAnsibleSchedules = 200

// handleAnsibleSchedules:
//
//	GET  /api/ansible/schedules[?playbook_id=N] -> {"schedules":[...]}
//	POST /api/ansible/schedules                 -> 201 {"schedule":...}
func (s *Server) handleAnsibleSchedules(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		pid, _ := strconv.ParseInt(r.URL.Query().Get("playbook_id"), 10, 64)
		list, err := s.db.ListAnsibleSchedules(pid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"schedules": list})

	case http.MethodPost:
		all, err := s.db.ListAnsibleSchedules(0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(all) >= maxAnsibleSchedules {
			writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("at most %d schedules are allowed", maxAnsibleSchedules)})
			return
		}
		sc, ok := s.decodeScheduleRequest(w, r)
		if !ok {
			return
		}
		id, err := s.db.CreateAnsibleSchedule(sc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		created, err := s.db.GetAnsibleSchedule(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("ansible", created.PlaybookName, fmt.Sprintf("Created schedule %d (%s)", id, created.Cron))
		writeJSON(w, http.StatusCreated, map[string]interface{}{"schedule": created})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAnsibleScheduleRoute: GET/PUT/DELETE /api/ansible/schedules/{id}
func (s *Server) handleAnsibleScheduleRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	id, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ansible/schedules/"), "/"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	existing, err := s.db.GetAnsibleSchedule(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsibleScheduleNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"schedule": existing})

	case http.MethodPut:
		sc, ok := s.decodeScheduleRequest(w, r)
		if !ok {
			return
		}
		sc.ID = id
		if err := s.db.UpdateAnsibleSchedule(sc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated, err := s.db.GetAnsibleSchedule(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("ansible", updated.PlaybookName, fmt.Sprintf("Updated schedule %d (%s, enabled=%v)", id, updated.Cron, updated.Enabled))
		writeJSON(w, http.StatusOK, map[string]interface{}{"schedule": updated})

	case http.MethodDelete:
		if err := s.db.DeleteAnsibleSchedule(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("ansible", existing.PlaybookName, fmt.Sprintf("Deleted schedule %d", id))
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// decodeScheduleRequest parses and validates a create/update body. next_run_at is derived here from the cron
// expression (server local time); a disabled schedule has none.
func (s *Server) decodeScheduleRequest(w http.ResponseWriter, r *http.Request) (*store.AnsibleSchedule, bool) {
	var req struct {
		PlaybookID int64   `json:"playbook_id"`
		Name       string  `json:"name"`
		Cron       string  `json:"cron"`
		Enabled    bool    `json:"enabled"`
		Mode       string  `json:"mode"`
		HostIDs    []int64 `json:"host_ids"`
		Group      string  `json:"group"`
		ExtraVars  string  `json:"extra_vars"`
		Tags       string  `json:"tags"`
		SkipTags   string  `json:"skip_tags"`
		Verbosity  int     `json:"verbosity"`
		Become     bool    `json:"become"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return nil, false
	}
	bad := func(msg string) (*store.AnsibleSchedule, bool) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return nil, false
	}

	if _, err := s.db.GetAnsiblePlaybook(req.PlaybookID); err != nil {
		return bad("unknown playbook")
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) > maxPlaybookNameLength {
		return bad("schedule name must not exceed 64 characters")
	}
	cr, err := ansible.ParseCron(req.Cron)
	if err != nil {
		return bad("invalid cron expression: " + err.Error())
	}
	if req.Mode == "" {
		req.Mode = "run"
	}
	// Validates mode-independent options (tags, verbosity, extra vars) with the same rules as a real run.
	if err := (ansible.RunSpec{Mode: "syntax", Playbook: "x", ExtraVars: strings.TrimSpace(req.ExtraVars), Tags: req.Tags, SkipTags: req.SkipTags, Verbosity: req.Verbosity}).Validate(); err != nil {
		return bad(err.Error())
	}
	switch req.Mode {
	case "run", "check", "diff", "syntax":
	default:
		return bad("mode must be run, check, diff or syntax")
	}

	hosts, err := s.db.ListHosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	known := make(map[int64]bool, len(hosts))
	groupHosts := 0
	req.Group = strings.TrimSpace(req.Group)
	for _, h := range hosts {
		known[h.ID] = true
		if req.Group != "" && h.GroupName == req.Group {
			groupHosts++
		}
	}
	for _, id := range req.HostIDs {
		if !known[id] {
			return bad(fmt.Sprintf("unknown host id %d", id))
		}
	}
	if req.Group != "" && groupHosts == 0 {
		return bad(fmt.Sprintf("group %q has no hosts", req.Group))
	}
	if req.Mode != "syntax" && len(req.HostIDs) == 0 && req.Group == "" {
		return bad("no targets selected (required unless mode is syntax)")
	}

	sc := &store.AnsibleSchedule{
		PlaybookID: req.PlaybookID, Name: req.Name, Cron: strings.TrimSpace(req.Cron), Enabled: req.Enabled, Mode: req.Mode,
		HostIDs: req.HostIDs, Group: req.Group, ExtraVars: strings.TrimSpace(req.ExtraVars), Tags: req.Tags, SkipTags: req.SkipTags,
		Verbosity: req.Verbosity, Become: req.Become,
	}
	if req.Enabled {
		if n := cr.Next(time.Now()); !n.IsZero() {
			n = n.UTC()
			sc.NextRunAt = &n
		} else {
			return bad("cron expression never matches a date")
		}
	}
	return sc, true
}
