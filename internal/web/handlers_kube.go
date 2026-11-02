package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

const kubeBodyLimit = 256 * 1024 // yaml manifests capped at 64 KB; the rest is JSON overhead

var kubeExecBlockRe = regexp.MustCompile(`(?i)\b(kubectl|kc|k3s\s+kubectl)\b.*\bexec\b`)

func isKubeExecCommand(cmd string) bool {
	return kubeExecBlockRe.MatchString(cmd)
}

func statusPriority(status string) int {
	switch strings.ToLower(status) {
	case "healthy":
		return 0
	case "degraded":
		return 1
	case "down":
		return 3
	default:
		return 2
	}
}

// selectKubeCandidateHosts orders hosts: requested primary host first, then peers with the same
// non-empty kube_cluster_id sorted by health (healthy -> degraded -> unknown -> down), then sort_order, then id.
func selectKubeCandidateHosts(primary *store.Host, allHosts []store.Host) []*store.Host {
	candidates := []*store.Host{primary}
	if primary.KubeClusterID == "" {
		return candidates
	}

	var peers []*store.Host
	for i := range allHosts {
		h := &allHosts[i]
		if h.ID == primary.ID {
			continue
		}
		if h.KubeClusterID == primary.KubeClusterID {
			peers = append(peers, h)
		}
	}

	sort.SliceStable(peers, func(i, j int) bool {
		pi := statusPriority(peers[i].Status)
		pj := statusPriority(peers[j].Status)
		if pi != pj {
			return pi < pj
		}
		if peers[i].SortOrder != peers[j].SortOrder {
			return peers[i].SortOrder < peers[j].SortOrder
		}
		return peers[i].ID < peers[j].ID
	})

	return append(candidates, peers...)
}

func makeEnvelope(primary, served *store.Host, primaryErr string) map[string]interface{} {
	failover := served.ID != primary.ID
	m := map[string]interface{}{
		"served_by_host_id":   served.ID,
		"served_by_host_name": served.Name,
		"failover":            failover,
		"primary_host_id":     primary.ID,
	}
	if failover && primaryErr != "" {
		m["primary_error"] = primaryErr
	}
	return m
}

func withEnvelope(resp map[string]interface{}, primary, served *store.Host, primaryErr string) map[string]interface{} {
	env := makeEnvelope(primary, served, primaryErr)
	for k, v := range env {
		resp[k] = v
	}
	return resp
}

func writeKubeError(w http.ResponseWriter, err error, extra ...map[string]interface{}) {
	status := http.StatusInternalServerError
	if errors.Is(err, inspector.ErrKubeInvalid) {
		status = http.StatusBadRequest
	}
	resp := map[string]interface{}{"ok": false, "error": err.Error()}
	for _, m := range extra {
		for k, v := range m {
			resp[k] = v
		}
	}
	writeJSON(w, status, resp)
}

// writeKubeErrorOK reports read failures as 200/ok:false except for rejected input (which is 400).
func writeKubeErrorOK(w http.ResponseWriter, err error, extra ...map[string]interface{}) {
	if errors.Is(err, inspector.ErrKubeInvalid) {
		writeKubeError(w, err, extra...)
		return
	}
	resp := map[string]interface{}{"ok": false, "error": err.Error()}
	for _, m := range extra {
		for k, v := range m {
			resp[k] = v
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func decodeKubeBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, kubeBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
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

	primary, _ := s.db.GetHost(hostID)
	if primary == nil {
		primary = &store.Host{
			ID:       hostID,
			Name:     fmt.Sprintf("host-%d", hostID),
			KubeMode: "full",
		}
	}

	allHosts, _ := s.db.ListHosts()
	candidates := selectKubeCandidateHosts(primary, allHosts)

	// Mode enforcement
	if (sub == "action" || sub == "apply") && post {
		if primary.KubeMode == "read-only" {
			writeJSON(w, http.StatusForbidden, withEnvelope(map[string]interface{}{
				"ok":    false,
				"error": "kubernetes mode is read-only",
			}, primary, primary, ""))
			return true
		}
	}
	if sub == "exec-cmd" && get {
		if primary.KubeMode == "no-exec" || primary.KubeMode == "read-only" {
			writeJSON(w, http.StatusForbidden, withEnvelope(map[string]interface{}{
				"ok":    false,
				"error": "kubernetes mode prevents exec",
			}, primary, primary, ""))
			return true
		}
	}

	runRead := func(fn func(targetID int64) (map[string]interface{}, error)) {
		var primaryErr string
		for i, c := range candidates {
			resp, err := fn(c.ID)
			if err == nil {
				writeJSON(w, http.StatusOK, withEnvelope(resp, primary, c, primaryErr))
				return
			}
			if errors.Is(err, inspector.ErrKubeInvalid) {
				writeKubeError(w, err, makeEnvelope(primary, primary, ""))
				return
			}
			if i == 0 {
				primaryErr = err.Error()
			}
		}
		errToReport := errors.New("cluster unavailable")
		if primaryErr != "" {
			errToReport = errors.New(primaryErr)
		}
		writeKubeErrorOK(w, errToReport, makeEnvelope(primary, primary, ""))
	}

	switch {
	case get && sub == "detect":
		runRead(func(targetID int64) (map[string]interface{}, error) {
			cluster, err := s.inspector.KubeDetect(targetID)
			if err != nil {
				return nil, err
			}
			if cluster != nil && cluster.ClusterID != "" {
				_ = s.db.SetHostKubeClusterID(targetID, cluster.ClusterID)
			}
			return map[string]interface{}{"ok": true, "cluster": cluster}, nil
		})

	case get && sub == "nodes":
		runRead(func(targetID int64) (map[string]interface{}, error) {
			nodes, err := s.inspector.KubeNodes(targetID)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "nodes": nodes}, nil
		})

	case get && sub == "namespaces":
		runRead(func(targetID int64) (map[string]interface{}, error) {
			namespaces, err := s.inspector.KubeNamespaces(targetID)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "namespaces": namespaces}, nil
		})

	case get && sub == "workloads":
		ns := r.URL.Query().Get("ns")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			workloads, err := s.inspector.KubeWorkloads(targetID, ns)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "workloads": workloads}, nil
		})

	case get && sub == "pods":
		ns := r.URL.Query().Get("ns")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			pods, err := s.inspector.KubePods(targetID, ns)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "pods": pods}, nil
		})

	case get && sub == "services":
		ns := r.URL.Query().Get("ns")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			services, err := s.inspector.KubeServices(targetID, ns)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "services": services}, nil
		})

	case get && sub == "events":
		ns := r.URL.Query().Get("ns")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			events, err := s.inspector.KubeEvents(targetID, ns)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "events": events}, nil
		})

	case get && sub == "describe":
		q := r.URL.Query()
		kind := q.Get("kind")
		ns := q.Get("ns")
		name := q.Get("name")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			out, err := s.inspector.KubeDescribe(targetID, kind, ns, name)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "output": out}, nil
		})

	case get && sub == "resources":
		q := r.URL.Query()
		kind := q.Get("kind")
		ns := q.Get("ns")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			cols, rows, err := s.inspector.KubeResources(targetID, kind, ns)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "columns": cols, "rows": rows}, nil
		})

	case get && sub == "exec-cmd":
		q := r.URL.Query()
		cmd, err := s.inspector.KubeExecCmd(q.Get("ns"), q.Get("pod"), q.Get("container"))
		if err != nil {
			writeKubeErrorOK(w, err, makeEnvelope(primary, primary, ""))
			return true
		}
		effectiveHost := primary
		var primaryErr string
		dialErr := s.inspector.CheckKubeConnection(primary.ID)
		if dialErr != nil {
			primaryErr = dialErr.Error()
			for _, c := range candidates[1:] {
				if s.inspector.CheckKubeConnection(c.ID) == nil {
					effectiveHost = c
					break
				}
			}
		}
		writeJSON(w, http.StatusOK, withEnvelope(map[string]interface{}{
			"ok":      true,
			"cmd":     cmd,
			"host_id": effectiveHost.ID,
		}, primary, effectiveHost, primaryErr))

	case get && sub == "logs":
		q := r.URL.Query()
		tail := 200
		if tStr := q.Get("tail"); tStr != "" {
			tVal, err := strconv.Atoi(tStr)
			if err != nil {
				writeKubeError(w, inspector.ErrKubeInvalid, makeEnvelope(primary, primary, ""))
				return true
			}
			tail = tVal
		}
		ns := q.Get("ns")
		pod := q.Get("pod")
		container := q.Get("container")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			out, err := s.inspector.KubeLogs(targetID, ns, pod, container, tail)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "output": out}, nil
		})

	case get && sub == "yaml":
		q := r.URL.Query()
		kind, ns, name := q.Get("kind"), q.Get("ns"), q.Get("name")
		runRead(func(targetID int64) (map[string]interface{}, error) {
			out, err := s.inspector.KubeYAML(targetID, kind, ns, name)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "yaml": out, "kind": kind, "ns": ns, "name": name}, nil
		})

	case post && sub == "drain-preview":
		var req struct {
			Name string `json:"name"`
		}
		if !decodeKubeBody(w, r, &req) {
			return true
		}
		runRead(func(targetID int64) (map[string]interface{}, error) {
			prev, err := s.inspector.KubeDrainPreview(targetID, req.Name)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "node": prev.Node, "pods": prev.Pods,
				"pod_count": prev.PodCount, "daemonset_skipped": prev.DaemonsetSkipped, "blocked": prev.Blocked}, nil
		})

	case post && sub == "diff":
		var req struct {
			YAML string `json:"yaml"`
		}
		if !decodeKubeBody(w, r, &req) {
			return true
		}
		if err := inspector.ValidateKubeApply(req.YAML); err != nil {
			writeKubeError(w, err, makeEnvelope(primary, primary, ""))
			return true
		}
		runRead(func(targetID int64) (map[string]interface{}, error) {
			out, changed, err := s.inspector.KubeDiff(targetID, req.YAML)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "changed": changed, "output": out}, nil
		})

	case post && sub == "action":
		var req inspector.KubeActionReq
		if !decodeKubeBody(w, r, &req) {
			return true
		}
		if err := inspector.ValidateKubeAction(req); err != nil {
			writeKubeError(w, err, makeEnvelope(primary, primary, ""))
			return true
		}
		effectiveHost := primary
		var primaryErr string
		dialErr := s.inspector.CheckKubeConnection(primary.ID)
		if dialErr != nil {
			primaryErr = dialErr.Error()
			found := false
			for _, c := range candidates[1:] {
				if c.KubeMode == "read-only" {
					continue
				}
				if s.inspector.CheckKubeConnection(c.ID) == nil {
					effectiveHost = c
					found = true
					break
				}
			}
			if !found {
				writeKubeError(w, dialErr, makeEnvelope(primary, primary, ""))
				return true
			}
		}
		out, err := s.inspector.KubeAction(effectiveHost.ID, req)
		if err != nil {
			writeKubeError(w, err, makeEnvelope(primary, effectiveHost, primaryErr))
			return true
		}
		writeJSON(w, http.StatusOK, withEnvelope(map[string]interface{}{"ok": true, "output": out}, primary, effectiveHost, primaryErr))

	case post && sub == "apply":
		var req struct {
			YAML string `json:"yaml"`
		}
		if !decodeKubeBody(w, r, &req) {
			return true
		}
		if err := inspector.ValidateKubeApply(req.YAML); err != nil {
			writeKubeError(w, err, makeEnvelope(primary, primary, ""))
			return true
		}
		effectiveHost := primary
		var primaryErr string
		dialErr := s.inspector.CheckKubeConnection(primary.ID)
		if dialErr != nil {
			primaryErr = dialErr.Error()
			found := false
			for _, c := range candidates[1:] {
				if c.KubeMode == "read-only" {
					continue
				}
				if s.inspector.CheckKubeConnection(c.ID) == nil {
					effectiveHost = c
					found = true
					break
				}
			}
			if !found {
				writeKubeError(w, dialErr, makeEnvelope(primary, primary, ""))
				return true
			}
		}
		out, err := s.inspector.KubeApply(effectiveHost.ID, req.YAML)
		if err != nil {
			writeKubeError(w, err, makeEnvelope(primary, effectiveHost, primaryErr))
			return true
		}
		writeJSON(w, http.StatusOK, withEnvelope(map[string]interface{}{"ok": true, "output": out}, primary, effectiveHost, primaryErr))

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
	timestamps := q.Get("timestamps") == "1"
	workload := strings.TrimSpace(q.Get("workload"))

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
	var cmd string
	if pod == "" && workload == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("pod or workload is required\n"))
		return
	}
	if workload != "" {
		if previous {
			_ = ws.WriteMessage(websocket.TextMessage, []byte("previous logs are not supported for workloads\n"))
			return
		}
		kind, wname, found := strings.Cut(workload, "/")
		if !found {
			_ = ws.WriteMessage(websocket.TextMessage, []byte("workload must be kind/name\n"))
			return
		}
		var err error
		cmd, err = s.inspector.KubeWorkloadLogStreamCmd(kind, wname, ns, tail, timestamps)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(err.Error()+"\n"))
			return
		}
	} else {
		if !inspector.ValidKubeName(pod) {
			_ = ws.WriteMessage(websocket.TextMessage, []byte("invalid pod name\n"))
			return
		}
		if container != "" && !inspector.ValidKubeName(container) {
			_ = ws.WriteMessage(websocket.TextMessage, []byte("invalid container name\n"))
			return
		}
		cmd = s.inspector.KubeLogStreamCmd(ns, pod, container, tail, previous, timestamps)
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

	_ = runner.Stream(cmd, outWriter)
}
