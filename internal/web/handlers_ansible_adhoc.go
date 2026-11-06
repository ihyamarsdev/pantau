package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// resolveAdhocTargets applies the same selection rules as a playbook launch: host_ids ∪ group, at most 200 hosts.
func (s *Server) resolveAdhocTargets(hostIDs []int64, group string) ([]ansible.Target, *ansibleLaunchError) {
	allHosts, err := s.db.ListHosts()
	if err != nil {
		return nil, launchErr(http.StatusInternalServerError, "%v", err)
	}
	hostLookup := make(map[int64]*store.Host, len(allHosts))
	for i := range allHosts {
		hostLookup[allHosts[i].ID] = &allHosts[i]
	}

	targetMap := make(map[int64]*store.Host)
	for _, hid := range hostIDs {
		h, ok := hostLookup[hid]
		if !ok {
			return nil, launchErr(http.StatusBadRequest, "unknown host id %d", hid)
		}
		targetMap[h.ID] = h
	}
	if g := strings.TrimSpace(group); g != "" {
		matched := 0
		for i := range allHosts {
			if allHosts[i].GroupName == g {
				targetMap[allHosts[i].ID] = &allHosts[i]
				matched++
			}
		}
		if matched == 0 {
			return nil, launchErr(http.StatusBadRequest, "group %q has no hosts", g)
		}
	}
	if len(targetMap) == 0 {
		return nil, launchErr(http.StatusBadRequest, "no targets selected")
	}
	if len(targetMap) > 200 {
		return nil, launchErr(http.StatusBadRequest, "too many targets (%d, max 200): narrow the selection with a group or pattern", len(targetMap))
	}

	targets := make([]ansible.Target, 0, len(targetMap))
	for _, h := range targetMap {
		privKey, err := s.db.ResolveHostPrivateKey(h)
		if err != nil {
			return nil, launchErr(http.StatusBadRequest, "failed to resolve private key for host %s: %v", h.Name, err)
		}
		targets = append(targets, ansible.Target{
			ID: h.ID, Name: h.Name, Address: h.Host, Port: h.Port, User: h.User, Group: h.GroupName, PrivateKey: privKey,
		})
	}
	return targets, nil
}

// adhocRunView is the API shape of a stored run: results parsed, targets parsed.
type adhocRunView struct {
	store.AnsibleAdhocRun
	Targets []targetSnapshot      `json:"targets"`
	Results []ansible.AdhocResult `json:"results"`
	Counts  ansible.AdhocCounts   `json:"counts"`
}

func adhocRunToView(run store.AnsibleAdhocRun) adhocRunView {
	view := adhocRunView{AnsibleAdhocRun: run, Targets: []targetSnapshot{}, Results: []ansible.AdhocResult{}}
	_ = json.Unmarshal([]byte(run.Targets), &view.Targets)
	_ = json.Unmarshal([]byte(run.Results), &view.Results)
	view.Counts = ansible.CountAdhoc(view.Results)
	return view
}

// handleAnsibleAdhoc: POST /api/ansible/adhoc, GET /api/ansible/adhoc?limit=
func (s *Server) handleAnsibleAdhoc(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		runs, err := s.db.ListAnsibleAdhocRuns(limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		views := make([]adhocRunView, 0, len(runs))
		for _, run := range runs {
			views = append(views, adhocRunToView(run))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"runs": views})

	case http.MethodPost:
		var req struct {
			Module    string  `json:"module"`
			Args      string  `json:"args"`
			Pattern   string  `json:"pattern"`
			HostIDs   []int64 `json:"host_ids"`
			Group     string  `json:"group"`
			ExtraVars string  `json:"extra_vars"`
			Become    bool    `json:"become"`
			Forks     int     `json:"forks"`
			Verbosity int     `json:"verbosity"`
		}
		if !decodeAnsibleBody(w, r, &req) {
			return
		}

		targets, lerr := s.resolveAdhocTargets(req.HostIDs, req.Group)
		if lerr != nil {
			writeJSON(w, lerr.Status, map[string]string{"error": lerr.Msg})
			return
		}

		spec := ansible.AdhocSpec{
			Module: req.Module, Args: req.Args, Pattern: strings.TrimSpace(req.Pattern), Targets: targets,
			Become: req.Become, Forks: req.Forks, Verbosity: req.Verbosity, ExtraVars: req.ExtraVars,
			LibraryDir: s.ansibleLibDir,
		}
		if err := spec.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		snapTargets := make([]targetSnapshot, 0, len(targets))
		for _, t := range targets {
			snapTargets = append(snapTargets, targetSnapshot{ID: t.ID, Name: t.Name})
		}
		snapJSON, _ := json.Marshal(snapTargets)
		pattern := spec.Pattern
		if pattern == "" {
			pattern = "all"
		}
		run := &store.AnsibleAdhocRun{
			Module: spec.Module, Args: spec.Args, Pattern: pattern, Targets: string(snapJSON),
			Become: spec.Become, Verbosity: spec.Verbosity, Results: "[]",
		}
		if _, err := s.db.CreateAnsibleAdhocRun(run); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.ansibleAdhocMgr == nil {
			http.Error(w, "ad-hoc manager is not initialised", http.StatusInternalServerError)
			return
		}
		if err := s.ansibleAdhocMgr.Start(run, spec); err != nil {
			_ = s.db.FinishAnsibleAdhocRun(run.ID, "failed", nil, "", "[]")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("ansible", run.Module, "Queued ad-hoc run "+strconv.FormatInt(run.ID, 10))
		writeJSON(w, http.StatusCreated, map[string]interface{}{"run": adhocRunToView(*run)})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAnsibleAdhocRoute: GET /api/ansible/adhoc/{id}, GET /api/ansible/adhoc/{id}/log, POST /api/ansible/adhoc/{id}/cancel
func (s *Server) handleAnsibleAdhocRoute(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}

	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ansible/adhoc/"), "/")
	parts := strings.Split(tail, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	run, err := s.db.GetAnsibleAdhocRun(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsibleAdhocRunNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"run": adhocRunToView(*run)})

	case action == "log" && r.Method == http.MethodGet:
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		data, next, status, finished, err := s.ansibleAdhocMgr.ReadLog(id, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"log": data, "offset": next, "status": status, "finished": finished,
		})

	case action == "cancel" && r.Method == http.MethodPost:
		if run.Status != "running" && run.Status != "queued" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "run is not active"})
			return
		}
		cancelled := s.ansibleAdhocMgr.Cancel(id)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "cancelled": cancelled})

	default:
		http.NotFound(w, r)
	}
}
