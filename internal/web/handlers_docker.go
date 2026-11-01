package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"pantau/internal/inspector"
)

const dockerBodyLimit = 256 * 1024 // compose files are capped at 64 KB; the rest is JSON overhead

func writeDockerError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, inspector.ErrDockerInvalid) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]interface{}{"ok": false, "error": err.Error()})
}

func decodeDockerBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, dockerBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

// handleDockerManage serves the Portainer-style Docker endpoints below /api/hosts/{id}/docker/. It returns
// false when the sub-path is not one of them so the caller can fall through to the older read-only routes.
//
//	GET  images | system | stacks | inspect?name= | stack/file?project=
//	POST action | run | image | volume | network | stack | stack/deploy | prune
func (s *Server) handleDockerManage(w http.ResponseWriter, r *http.Request, hostID int64, parts []string) bool {
	if len(parts) < 3 {
		return false
	}
	sub := strings.Join(parts[2:], "/")
	get := r.Method == http.MethodGet
	post := r.Method == http.MethodPost

	switch {
	case get && sub == "images":
		images, err := s.inspector.DockerListImages(hostID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "images": images})

	case get && sub == "system":
		info, err := s.inspector.DockerSystem(hostID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "system": info})

	case get && sub == "stacks":
		stacks, err := s.inspector.DockerListStacks(hostID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "stacks": stacks})

	case get && sub == "inspect":
		detail, err := s.inspector.DockerInspectContainer(hostID, r.URL.Query().Get("name"))
		if err != nil {
			writeDockerErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "container": detail})

	case get && sub == "stack/file":
		path, content, err := s.inspector.DockerStackFile(hostID, r.URL.Query().Get("project"))
		if err != nil {
			writeDockerErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": path, "content": content})

	case post && sub == "action":
		var req struct {
			Container string `json:"container"`
			Action    string `json:"action"`
			inspector.DockerContainerOpts
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerContainerOp(hostID, req.Container, req.Action, req.DockerContainerOpts)
		dockerResult(w, out, err)

	case post && sub == "run":
		var spec inspector.DockerRunSpec
		if !decodeDockerBody(w, r, &spec) {
			return true
		}
		id, err := s.inspector.DockerRunContainer(hostID, spec)
		if err != nil {
			writeDockerError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": id})

	case post && sub == "image":
		var req struct {
			Action string `json:"action"`
			Image  string `json:"image"`
			Force  bool   `json:"force"`
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerImageOp(hostID, req.Action, req.Image, req.Force)
		dockerResult(w, out, err)

	case post && sub == "volume":
		var req struct {
			Action string `json:"action"`
			Name   string `json:"name"`
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerVolumeOp(hostID, req.Action, req.Name)
		dockerResult(w, out, err)

	case post && sub == "network":
		var req struct {
			Action    string `json:"action"`
			Container string `json:"container"`
			inspector.DockerNetworkSpec
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerNetworkOp(hostID, req.Action, req.DockerNetworkSpec, req.Container)
		dockerResult(w, out, err)

	case post && sub == "stack":
		var req struct {
			Project string `json:"project"`
			Action  string `json:"action"`
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerStackOp(hostID, req.Project, req.Action)
		dockerResult(w, out, err)

	case post && sub == "stack/deploy":
		var req struct {
			Name string `json:"name"`
			YAML string `json:"yaml"`
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerStackDeploy(hostID, req.Name, req.YAML)
		dockerResult(w, out, err)

	case post && sub == "prune":
		var req struct {
			Target string `json:"target"`
		}
		if !decodeDockerBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.DockerPrune(hostID, req.Target)
		dockerResult(w, out, err)

	default:
		return false
	}
	return true
}

func dockerResult(w http.ResponseWriter, out string, err error) {
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": out})
}

// writeDockerErrorOK reports read failures as 200/ok:false (like the other Docker tab loaders) except for
// rejected input.
func writeDockerErrorOK(w http.ResponseWriter, err error) {
	if errors.Is(err, inspector.ErrDockerInvalid) {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
}
