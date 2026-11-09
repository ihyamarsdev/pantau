package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/dockerhub"
)

// handleDockerHub serves the read-only Docker Hub browser (requests go from the Pantau server):
//
//	GET /api/dockerhub/search?q=&page=
//	GET /api/dockerhub/repo/{namespace}/{name}
//	GET /api/dockerhub/tags/{namespace}/{name}?name=&page=
func (s *Server) handleDockerHub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/dockerhub/"), "/"), "/")
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	var (
		res any
		err error
	)
	switch {
	case len(parts) == 1 && parts[0] == "search":
		res, err = s.hub.Search(r.Context(), q.Get("q"), page)
	case len(parts) == 3 && parts[0] == "repo":
		res, err = s.hub.Repository(r.Context(), parts[1], parts[2])
	case len(parts) == 3 && parts[0] == "tags":
		res, err = s.hub.Tags(r.Context(), parts[1], parts[2], q.Get("name"), page)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		dockerHubFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func dockerHubFail(w http.ResponseWriter, err error) {
	var se *dockerhub.StatusError
	switch {
	case errors.Is(err, dockerhub.ErrBadArg):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case dockerhub.IsNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found on Docker Hub"})
	case errors.As(err, &se) && se.Status == http.StatusTooManyRequests:
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}

// dockerImageUpdates compares the host's Docker Hub images with the digests Docker Hub serves for their tags.
func (s *Server) dockerImageUpdates(w http.ResponseWriter, r *http.Request, hostID int64) {
	refs, err := s.inspector.DockerImageRefs(hostID)
	if err != nil {
		writeDockerErrorOK(w, err)
		return
	}
	local := make([]dockerhub.Local, len(refs))
	for i, ref := range refs {
		local[i] = dockerhub.Local{ID: ref.ID, Tags: ref.Tags, Digests: ref.Digests}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updates": s.hub.CheckUpdates(ctx, local), "checked_at": time.Now()})
}
