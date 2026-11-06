package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// handleAnsiblePlaybookSurvey:
//
//	GET /api/ansible/playbooks/{id}/survey -> {"survey":[...]}
//	PUT /api/ansible/playbooks/{id}/survey {"survey":[...]} -> {"survey":[...]}
func (s *Server) handleAnsiblePlaybookSurvey(w http.ResponseWriter, r *http.Request, id int64) {
	pb, err := s.db.GetAnsiblePlaybook(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"survey":     surveyArray(pb.Survey),
			"has_survey": strings.TrimSpace(pb.Survey) != "",
		})

	case http.MethodPut:
		var req struct {
			Survey []ansible.SurveyField `json:"survey"`
		}
		if !decodeAnsibleBody(w, r, &req) {
			return
		}
		if err := ansible.ValidateSurvey(req.Survey); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		raw := ""
		if len(req.Survey) > 0 {
			b, err := json.Marshal(req.Survey)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			raw = string(b)
		}
		if err := s.db.SetAnsiblePlaybookSurvey(id, raw); err != nil {
			if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("ansible", pb.Name, "Updated playbook survey")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"survey":     surveyArray(raw),
			"has_survey": raw != "",
		})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// gitSyncTimeout bounds a Sync Now: a shallow clone of a healthy repository takes seconds.
const gitSyncTimeout = 60 * time.Second

// handleAnsiblePlaybookGitSync: POST /api/ansible/playbooks/{id}/git/sync
// Verifies a Git playbook with a shallow clone and records when and at which commit it last synced.
func (s *Server) handleAnsiblePlaybookGitSync(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	pb, err := s.db.GetAnsiblePlaybook(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if pb.Source != "git" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sync is only available for Git playbooks"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.ansibleGitSource(pb)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gitSyncTimeout)
	defer cancel()
	sync := s.ansibleSync
	if sync == nil {
		sync = ansible.SyncGit
	}
	res, err := sync(ctx, *g)
	if err != nil {
		logger.Error("ansible", pb.Name, "Git sync failed: "+err.Error())
		code := http.StatusBadGateway
		if errors.Is(err, ansible.ErrPlaybookPathMissing) {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	syncedAt := time.Now().UTC()
	if err := s.db.SetAnsiblePlaybookGitSync(id, syncedAt, res.Commit); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	logger.Info("ansible", pb.Name, "Synced git repository at "+res.Commit)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"commit":    res.Commit,
		"branch":    res.Branch,
		"files":     res.Files,
		"synced_at": syncedAt,
	})
}

// surveyArray parses the stored survey and always returns a non-nil slice, so the API answers [] not null.
func surveyArray(raw string) []ansible.SurveyField {
	if fields := ansible.ParseSurvey(raw); fields != nil {
		return fields
	}
	return []ansible.SurveyField{}
}

// playbookWithSurvey turns p into the API shape: the stored survey JSON becomes a parsed array and has_survey
// says whether the playbook has one (the UI keys on it).
func playbookWithSurvey(p *store.AnsiblePlaybook) map[string]interface{} {
	fields := ansible.ParseSurvey(p.Survey)
	b, _ := json.Marshal(p)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	if fields == nil {
		fields = []ansible.SurveyField{}
	}
	out["survey"] = fields
	out["has_survey"] = len(fields) > 0
	return out
}

func playbooksWithSurvey(list []store.AnsiblePlaybook) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(list))
	for i := range list {
		out = append(out, playbookWithSurvey(&list[i]))
	}
	return out
}
