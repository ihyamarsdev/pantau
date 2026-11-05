package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

// ansibleRunParams is everything needed to start one run; shared by the manual run endpoint and the scheduler.
type ansibleRunParams struct {
	Mode       string
	HostIDs    []int64
	Group      string
	ExtraVars  string
	Tags       string
	SkipTags   string
	Verbosity  int
	Become     bool
	Trigger    string // "manual" | "schedule"
	ScheduleID int64
}

// ansibleLaunchError carries the HTTP status the manual endpoint should answer with.
type ansibleLaunchError struct {
	Status int
	Msg    string
}

func (e *ansibleLaunchError) Error() string { return e.Msg }

func launchErr(status int, format string, a ...any) *ansibleLaunchError {
	return &ansibleLaunchError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

type targetSnapshot struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// launchAnsibleRun resolves targets and the playbook source, creates the run record and starts it.
func (s *Server) launchAnsibleRun(pb *store.AnsiblePlaybook, p ansibleRunParams) (*store.AnsibleRun, *ansibleLaunchError) {
	if p.Mode == "" {
		p.Mode = "run"
	}
	if p.Trigger == "" {
		p.Trigger = "manual"
	}

	// extra_vars empty -> fallback to playbook's default extra_vars
	extraVars := strings.TrimSpace(p.ExtraVars)
	if extraVars == "" {
		extraVars = pb.ExtraVars
	}
	if extraVars != "" {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(extraVars), &obj); err != nil {
			return nil, launchErr(http.StatusBadRequest, "extra_vars must be a valid JSON object")
		}
	}

	// Target resolution: union of host_ids + hosts matching group
	allHosts, err := s.db.ListHosts()
	if err != nil {
		return nil, launchErr(http.StatusInternalServerError, "%v", err)
	}
	hostLookup := make(map[int64]*store.Host, len(allHosts))
	for i := range allHosts {
		hostLookup[allHosts[i].ID] = &allHosts[i]
	}

	targetMap := make(map[int64]*store.Host)
	for _, hid := range p.HostIDs {
		h, ok := hostLookup[hid]
		if !ok {
			return nil, launchErr(http.StatusBadRequest, "unknown host id %d", hid)
		}
		targetMap[h.ID] = h
	}
	if g := strings.TrimSpace(p.Group); g != "" {
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
	if p.Mode != "syntax" && len(targetMap) == 0 {
		return nil, launchErr(http.StatusBadRequest, "no targets selected (required unless mode is syntax)")
	}

	targets := make([]ansible.Target, 0, len(targetMap))
	snapTargets := make([]targetSnapshot, 0, len(targetMap))
	for _, h := range targetMap {
		privKey, err := s.db.ResolveHostPrivateKey(h)
		if err != nil {
			return nil, launchErr(http.StatusBadRequest, "failed to resolve private key for host %s: %v", h.Name, err)
		}
		targets = append(targets, ansible.Target{
			ID: h.ID, Name: h.Name, Address: h.Host, Port: h.Port, User: h.User, Group: h.GroupName, PrivateKey: privKey,
		})
		snapTargets = append(snapTargets, targetSnapshot{ID: h.ID, Name: h.Name})
	}
	snapJSON, _ := json.Marshal(snapTargets)

	ansibleConfig, _ := s.db.GetSetting(ansibleConfigSetting)
	runSpec := ansible.RunSpec{
		Targets:    targets,
		Mode:       p.Mode,
		ExtraVars:  extraVars,
		Tags:       p.Tags,
		SkipTags:   p.SkipTags,
		Verbosity:  p.Verbosity,
		Become:     p.Become,
		Config:     ansibleConfig,
		LibraryDir: s.ansibleLibDir,
	}
	if pb.Source == "git" {
		g, err := s.ansibleGitSource(pb)
		if err != nil {
			return nil, launchErr(http.StatusBadRequest, "%v", err)
		}
		runSpec.Git = g
	} else {
		runSpec.Playbook = pb.Content
	}
	if err := runSpec.Validate(); err != nil {
		return nil, launchErr(http.StatusBadRequest, "invalid run spec: %v", err)
	}

	// Concurrency rules are checked before the DB insert so rejected runs leave no record.
	if s.ansibleMgr != nil {
		if err := s.ansibleMgr.CanStartRun(pb.ID); err != nil {
			return nil, mapManagerError(err)
		}
	}

	runRecord := &store.AnsibleRun{
		PlaybookID:   pb.ID,
		PlaybookName: pb.Name,
		Status:       "queued",
		Mode:         p.Mode,
		Targets:      string(snapJSON),
		ExtraVars:    extraVars,
		Tags:         p.Tags,
		SkipTags:     p.SkipTags,
		Verbosity:    p.Verbosity,
		Become:       p.Become,
		Trigger:      p.Trigger,
		ScheduleID:   p.ScheduleID,
	}
	runID, err := s.db.CreateAnsibleRun(runRecord)
	if err != nil {
		return nil, launchErr(http.StatusInternalServerError, "%v", err)
	}
	runRecord.ID = runID

	if s.ansibleMgr != nil {
		if err := s.ansibleMgr.StartRun(runRecord, runSpec); err != nil {
			return nil, mapManagerError(err)
		}
	}

	created, err := s.db.GetAnsibleRun(runID)
	if err != nil {
		created = runRecord
	}
	return created, nil
}

func mapManagerError(err error) *ansibleLaunchError {
	switch {
	case strings.Contains(err.Error(), "already has an active run"):
		return launchErr(http.StatusConflict, "%v", err)
	case strings.Contains(err.Error(), "too many concurrent runs"):
		return launchErr(http.StatusTooManyRequests, "%v", err)
	}
	return launchErr(http.StatusInternalServerError, "%v", err)
}
