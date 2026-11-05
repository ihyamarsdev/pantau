package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	ansibleBodyLimit       = 512 * 1024 // 512 KiB (content up to 256 KiB)
	maxPlaybookNameLength  = 64
	maxPlaybookContentSize = 256 * 1024
	maxExtraVarsLength     = 64 * 1024
)

func decodeAnsibleBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, ansibleBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) checkAnsibleGate(w http.ResponseWriter) bool {
	avail := true
	if s.ansibleAvail != nil {
		_, _, err := s.ansibleAvail()
		if err != nil {
			avail = false
		}
	} else {
		_, _, err := ansible.Available()
		if err != nil {
			avail = false
		}
	}

	if !avail {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "ansible is not available on this server"})
		return false
	}

	if !s.db.GetAnsibleEnabled() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "ansible is disabled"})
		return false
	}
	return true
}

// handleAnsibleStatus:
//
//	GET /api/ansible/status -> {"available":bool,"version":"","error":"","enabled":bool,"max_concurrent":3}
//	PUT /api/ansible/status -> {"enabled":bool}
func (s *Server) handleAnsibleStatus(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var (
			path, ver string
			err       error
		)
		if s.ansibleAvail != nil {
			path, ver, err = s.ansibleAvail()
		} else {
			path, ver, err = ansible.Available()
		}

		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		_ = path

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available":      err == nil,
			"version":        ver,
			"error":          errStr,
			"enabled":        s.db.GetAnsibleEnabled(),
			"max_concurrent": ansible.MaxConcurrentRuns,
			"git_available":  gitAvailable(),
			"timezone":       time.Now().Format("MST (-07:00)"),
		})

	case http.MethodPut:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeAnsibleBody(w, r, &req) {
			return
		}

		if req.Enabled {
			var err error
			if s.ansibleAvail != nil {
				_, _, err = s.ansibleAvail()
			} else {
				_, _, err = ansible.Available()
			}
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": fmt.Sprintf("cannot enable ansible: %v", err),
				})
				return
			}
		}

		if err := s.db.SetAnsibleEnabled(req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var (
			path, ver string
			err       error
		)
		if s.ansibleAvail != nil {
			path, ver, err = s.ansibleAvail()
		} else {
			path, ver, err = ansible.Available()
		}
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		_ = path

		logger.Info("ansible", "", fmt.Sprintf("Ansible gate status updated: enabled=%v", req.Enabled))

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available":      err == nil,
			"version":        ver,
			"error":          errStr,
			"enabled":        req.Enabled,
			"max_concurrent": ansible.MaxConcurrentRuns,
			"git_available":  gitAvailable(),
			"timezone":       time.Now().Format("MST (-07:00)"),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAnsiblePlaybooks:
//
//	GET  /api/ansible/playbooks -> {"playbooks":[...]}
//	POST /api/ansible/playbooks -> 201 {"playbook":...}
func (s *Server) handleAnsiblePlaybooks(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		playbooks, err := s.db.ListAnsiblePlaybooks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if playbooks == nil {
			playbooks = []store.AnsiblePlaybook{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"playbooks": playbooks,
		})

	case http.MethodPost:
		pb, ok := s.decodePlaybookRequest(w, r, nil)
		if !ok {
			return
		}

		id, err := s.db.CreateAnsiblePlaybook(pb)
		if err != nil {
			if errors.Is(err, store.ErrAnsiblePlaybookNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": "an ansible playbook with this name already exists",
				})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		created, err := s.db.GetAnsiblePlaybook(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("ansible", pb.Name, fmt.Sprintf("Created playbook %s (id %d, source %s)", pb.Name, id, pb.Source))
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"playbook": created,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAnsiblePlaybookRoute handles /api/ansible/playbooks/{id} and subroutes like /run and /runs
func (s *Server) handleAnsiblePlaybookRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/api/ansible/playbooks/")
	tail = strings.TrimSuffix(tail, "/")
	parts := strings.Split(tail, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 1 {
		s.handleAnsiblePlaybookDetail(w, r, id)
		return
	}

	switch parts[1] {
	case "run":
		if r.Method == http.MethodPost {
			s.handleAnsiblePlaybookRun(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "runs":
		if r.Method == http.MethodGet {
			s.handleAnsiblePlaybookRunsList(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "contents":
		s.handleAnsiblePlaybookContents(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAnsiblePlaybookDetail(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		pb, err := s.db.GetAnsiblePlaybook(id)
		if err != nil {
			if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"playbook": pb,
		})

	case http.MethodPut:
		existing, err := s.db.GetAnsiblePlaybook(id)
		if err != nil {
			if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		pb, ok := s.decodePlaybookRequest(w, r, existing)
		if !ok {
			return
		}

		if err := s.db.UpdateAnsiblePlaybook(pb); err != nil {
			if errors.Is(err, store.ErrAnsiblePlaybookNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": "an ansible playbook with this name already exists",
				})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		updated, err := s.db.GetAnsiblePlaybook(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("ansible", pb.Name, fmt.Sprintf("Updated playbook %s (id %d)", pb.Name, id))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"playbook": updated,
		})

	case http.MethodDelete:
		// Check if playbook has an active run
		if s.ansibleMgr != nil && s.ansibleMgr.HasActiveRunForPlaybook(id) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "cannot delete playbook with an active run",
			})
			return
		}

		pb, err := s.db.GetAnsiblePlaybook(id)
		if err != nil {
			if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := s.db.DeleteAnsiblePlaybook(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("ansible", pb.Name, fmt.Sprintf("Deleted playbook %s (id %d)", pb.Name, id))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// handleAnsiblePlaybookRun: POST /api/ansible/playbooks/{id}/run
func (s *Server) handleAnsiblePlaybookRun(w http.ResponseWriter, r *http.Request, id int64) {
	pb, err := s.db.GetAnsiblePlaybook(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var req struct {
		Mode      string  `json:"mode"`
		HostIDs   []int64 `json:"host_ids"`
		Group     string  `json:"group"`
		ExtraVars string  `json:"extra_vars"`
		Tags      string  `json:"tags"`
		SkipTags  string  `json:"skip_tags"`
		Verbosity int     `json:"verbosity"`
		Become    bool    `json:"become"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return
	}

	run, lerr := s.launchAnsibleRun(pb, ansibleRunParams{
		Mode: req.Mode, HostIDs: req.HostIDs, Group: req.Group, ExtraVars: req.ExtraVars, Tags: req.Tags,
		SkipTags: req.SkipTags, Verbosity: req.Verbosity, Become: req.Become, Trigger: "manual",
	})
	if lerr != nil {
		writeJSON(w, lerr.Status, map[string]string{"error": lerr.Msg})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"run": run})
}

// handleAnsiblePlaybookRunsList: GET /api/ansible/playbooks/{id}/runs?limit=
func (s *Server) handleAnsiblePlaybookRunsList(w http.ResponseWriter, r *http.Request, playbookID int64) {
	limit := 50
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		if n, err := strconv.Atoi(limStr); err == nil && n > 0 {
			limit = n
		}
	}

	runs, err := s.db.ListAnsibleRuns(playbookID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []store.AnsibleRun{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs": runs,
	})
}

// handleAnsibleRuns: GET /api/ansible/runs?limit=
func (s *Server) handleAnsibleRuns(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		if n, err := strconv.Atoi(limStr); err == nil && n > 0 {
			limit = n
		}
	}

	runs, err := s.db.ListAnsibleRuns(0, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []store.AnsibleRun{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs": runs,
	})
}

// handleAnsibleRunRoute:
//
//	GET  /api/ansible/runs/{id}
//	GET  /api/ansible/runs/{id}/log?offset=N
//	POST /api/ansible/runs/{id}/cancel
func (s *Server) handleAnsibleRunRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/api/ansible/runs/")
	tail = strings.TrimSuffix(tail, "/")
	parts := strings.Split(tail, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			s.handleAnsibleRunDetail(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch parts[1] {
	case "log":
		if r.Method == http.MethodGet {
			s.handleAnsibleRunLog(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "cancel":
		if r.Method == http.MethodPost {
			s.handleAnsibleRunCancel(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAnsibleRunDetail(w http.ResponseWriter, r *http.Request, id int64) {
	run, err := s.db.GetAnsibleRun(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsibleRunNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run": run,
	})
}

func (s *Server) handleAnsibleRunLog(w http.ResponseWriter, r *http.Request, id int64) {
	offset := 0
	if offStr := r.URL.Query().Get("offset"); offStr != "" {
		if n, err := strconv.Atoi(offStr); err == nil && n >= 0 {
			offset = n
		}
	}

	if s.ansibleMgr == nil {
		http.Error(w, "ansible run manager not initialized", http.StatusInternalServerError)
		return
	}

	data, nextOffset, status, finished, err := s.ansibleMgr.ReadLog(id, offset)
	if err != nil {
		if errors.Is(err, store.ErrAnsibleRunNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":     data,
		"offset":   nextOffset,
		"status":   status,
		"finished": finished,
	})
}

func (s *Server) handleAnsibleRunCancel(w http.ResponseWriter, r *http.Request, id int64) {
	if s.ansibleMgr == nil {
		http.Error(w, "ansible run manager not initialized", http.StatusInternalServerError)
		return
	}

	if !s.ansibleMgr.CancelRun(id) {
		// Run is not active in manager -> check if it exists in DB
		run, err := s.db.GetAnsibleRun(id)
		if err != nil {
			if errors.Is(err, store.ErrAnsibleRunNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if run.Status != "running" && run.Status != "queued" {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "run has already finished",
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}
