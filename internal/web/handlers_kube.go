package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"

	"pantau/internal/inspector"
)

const kubeBodyLimit = 256 * 1024 // yaml manifests capped at 64 KB; the rest is JSON overhead

func writeKubeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, inspector.ErrKubeInvalid) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]interface{}{"ok": false, "error": err.Error()})
}

// writeKubeErrorOK reports read failures as 200/ok:false except for rejected input (which is 400).
func writeKubeErrorOK(w http.ResponseWriter, err error) {
	if errors.Is(err, inspector.ErrKubeInvalid) {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
}

func decodeKubeBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, kubeBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func kubeResult(w http.ResponseWriter, out string, err error) {
	if err != nil {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": out})
}

// handleKubeManage serves the Kubernetes endpoints below /api/hosts/{id}/kube/.
// It returns false when the sub-path is not one of them so the caller can fall through.
//
//	GET  detect | nodes | namespaces | workloads?ns= | pods?ns= | services?ns= | events?ns= | describe?kind=&ns=&name= | logs?ns=&pod=&container=&tail=
//	POST action | apply
func (s *Server) handleKubeManage(w http.ResponseWriter, r *http.Request, hostID int64, parts []string) bool {
	if len(parts) < 3 {
		return false
	}
	sub := strings.Join(parts[2:], "/")
	get := r.Method == http.MethodGet
	post := r.Method == http.MethodPost

	switch {
	case get && sub == "detect":
		cluster, err := s.inspector.KubeDetect(hostID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "cluster": cluster})

	case get && sub == "nodes":
		nodes, err := s.inspector.KubeNodes(hostID)
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "nodes": nodes})

	case get && sub == "namespaces":
		namespaces, err := s.inspector.KubeNamespaces(hostID)
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "namespaces": namespaces})

	case get && sub == "workloads":
		workloads, err := s.inspector.KubeWorkloads(hostID, r.URL.Query().Get("ns"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "workloads": workloads})

	case get && sub == "pods":
		pods, err := s.inspector.KubePods(hostID, r.URL.Query().Get("ns"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "pods": pods})

	case get && sub == "services":
		services, err := s.inspector.KubeServices(hostID, r.URL.Query().Get("ns"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "services": services})

	case get && sub == "events":
		events, err := s.inspector.KubeEvents(hostID, r.URL.Query().Get("ns"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "events": events})

	case get && sub == "describe":
		q := r.URL.Query()
		out, err := s.inspector.KubeDescribe(hostID, q.Get("kind"), q.Get("ns"), q.Get("name"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": out})
	case get && sub == "resources":
		q := r.URL.Query()
		cols, rows, err := s.inspector.KubeResources(hostID, q.Get("kind"), q.Get("ns"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "columns": cols, "rows": rows})

	case get && sub == "exec-cmd":
		q := r.URL.Query()
		cmd, err := s.inspector.KubeExecCmd(q.Get("ns"), q.Get("pod"), q.Get("container"))
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "cmd": cmd})


	case get && sub == "logs":
		q := r.URL.Query()
		tail := 200
		if tStr := q.Get("tail"); tStr != "" {
			tVal, err := strconv.Atoi(tStr)
			if err != nil {
				writeKubeError(w, inspector.ErrKubeInvalid)
				return true
			}
			tail = tVal
		}
		out, err := s.inspector.KubeLogs(hostID, q.Get("ns"), q.Get("pod"), q.Get("container"), tail)
		if err != nil {
			writeKubeErrorOK(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": out})

	case post && sub == "action":
		var req inspector.KubeActionReq
		if !decodeKubeBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.KubeAction(hostID, req)
		kubeResult(w, out, err)

	case post && sub == "apply":
		var req struct {
			YAML string `json:"yaml"`
		}
		if !decodeKubeBody(w, r, &req) {
			return true
		}
		out, err := s.inspector.KubeApply(hostID, req.YAML)
		kubeResult(w, out, err)

	default:
		return false
	}
	return true
}

// WebSocket Kubernetes Log Stream
func (s *Server) handleWSKubeLogs(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	q := r.URL.Query()
	hostIDStr := q.Get("host_id")
	hostID, _ := strconv.ParseInt(hostIDStr, 10, 64)
	ns := strings.TrimSpace(q.Get("ns"))
	pod := strings.TrimSpace(q.Get("pod"))
	container := strings.TrimSpace(q.Get("container"))
	previous := q.Get("previous") == "1" || q.Get("previous") == "true"

	tail := 200
	if tStr := q.Get("tail"); tStr != "" {
		tVal, err := strconv.Atoi(tStr)
		if err != nil || tVal < 1 || tVal > 2000 {
			_ = ws.WriteMessage(websocket.TextMessage, []byte("tail must be between 1 and 2000\n"))
			return
		}
		tail = tVal
	}

	if hostID <= 0 {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("invalid host_id\n"))
		return
	}
	if !inspector.ValidKubeNamespace(ns) {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("invalid namespace\n"))
		return
	}
	if !inspector.ValidKubeName(pod) {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("invalid pod name\n"))
		return
	}
	if container != "" && !inspector.ValidKubeName(container) {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("invalid container name\n"))
		return
	}

	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("Host not found\n"))
		return
	}

	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH failed: %v\n", err)))
		return
	}
	defer runner.Close()

	outReader, outWriter := io.Pipe()
	defer outWriter.Close()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := outReader.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				_ = ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}
	}()

	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				outWriter.Close()
				return
			}
		}
	}()

	cmd := s.inspector.KubeLogStreamCmd(ns, pod, container, tail, previous)
	_ = runner.Stream(cmd, outWriter)
}
