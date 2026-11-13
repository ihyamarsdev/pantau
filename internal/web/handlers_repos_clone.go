package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// Clone jobs: a clone can take minutes, so POST /api/repos starts a background job and the browser polls
// GET /api/repos/clone/{job} for git's progress lines and the outcome (DELETE cancels). Jobs live in memory
// and are dropped 10 minutes after they finish; a restart of Pantau loses running ones (the partial folder is removed).

const (
	repoJobLogLines = 300
	repoJobKeep     = 10 * time.Minute
)

type repoCloneJob struct {
	id, name string
	cancel   context.CancelFunc

	mu       sync.Mutex
	lines    []string // finished output lines, oldest dropped past repoJobLogLines
	cur      []byte   // line being written; a carriage return starts it over (git progress updates in place)
	cr       bool
	state    string // running | done | error | canceled
	errMsg   string
	repo     map[string]interface{}
	finished time.Time
}

// Write receives git's stderr. It keeps only the latest state of "\r"-updated progress lines.
func (j *repoCloneJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, b := range p {
		if j.cr && b != '\n' {
			j.cur = j.cur[:0]
		}
		j.cr = false
		switch b {
		case '\r':
			j.cr = true
		case '\n':
			j.lines = append(j.lines, string(j.cur))
			if len(j.lines) > repoJobLogLines {
				j.lines = j.lines[len(j.lines)-repoJobLogLines:]
			}
			j.cur = j.cur[:0]
		default:
			if len(j.cur) < 2000 {
				j.cur = append(j.cur, b)
			}
		}
	}
	return len(p), nil
}

type repoJobSet struct {
	mu   sync.Mutex
	jobs map[string]*repoCloneJob
}

// start registers a running job for name, or returns false if that name is already being cloned.
func (js *repoJobSet) start(name string) (*repoCloneJob, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.jobs == nil {
		js.jobs = map[string]*repoCloneJob{}
	}
	for id, j := range js.jobs {
		j.mu.Lock()
		running, old := j.state == "running", !j.finished.IsZero() && time.Since(j.finished) > repoJobKeep
		sameName := j.name == name
		j.mu.Unlock()
		if old {
			delete(js.jobs, id)
		} else if running && sameName {
			return nil, false
		}
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	j := &repoCloneJob{id: hex.EncodeToString(b), name: name, state: "running"}
	js.jobs[j.id] = j
	return j, true
}

func (js *repoJobSet) get(id string) *repoCloneJob {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.jobs[id]
}

// runRepoClone performs the clone for job, registers the repository on success and records the outcome.
func (s *Server) runRepoClone(ctx context.Context, cancel context.CancelFunc, job *repoCloneJob, repo *store.GitRepo, g ansible.GitSource) {
	defer cancel()
	clone := s.repoClone
	if clone == nil {
		clone = ansible.CloneLocalRepo
	}
	finish := func(state, msg string, res map[string]interface{}) {
		job.mu.Lock()
		job.state, job.errMsg, job.repo, job.finished = state, msg, res, time.Now()
		job.mu.Unlock()
	}

	branch, sha, err := clone(ctx, g, repo.Dir, job)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			finish("canceled", "canceled", nil)
			return
		}
		finish("error", sanitizeErrorMessage(err.Error()), nil)
		return
	}
	if _, err := s.db.CreateGitRepo(repo); err != nil {
		_ = os.RemoveAll(repo.Dir) // the clone was created by this job
		finish("error", err.Error(), nil)
		return
	}
	logger.Info("repos", repo.Name, fmt.Sprintf("Cloned repository %s into %s (branch %s, commit %s)", repo.Name, repo.Dir, branch, sha))
	finish("done", "", repoJSON(repo))
}

// handleRepoCloneJob: GET /api/repos/clone/{job} progress, DELETE cancels.
func (s *Server) handleRepoCloneJob(w http.ResponseWriter, r *http.Request, id string) {
	job := s.repoJobs.get(id)
	if job == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		job.mu.Lock()
		lines := make([]string, 0, len(job.lines)+1)
		for _, l := range job.lines {
			lines = append(lines, sanitizeErrorMessage(l))
		}
		if len(job.cur) > 0 {
			lines = append(lines, sanitizeErrorMessage(string(job.cur)))
		}
		out := map[string]interface{}{"state": job.state, "name": job.name, "lines": lines, "error": job.errMsg, "repo": job.repo}
		job.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
	case http.MethodDelete:
		if job.cancel != nil {
			job.cancel()
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
