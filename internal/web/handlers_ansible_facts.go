package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// factsGatherMu serialises gathers per host so two clicks cannot run `setup` twice on the same machine.
var factsGatherMu sync.Mutex
var factsGatherInFlight = map[int64]bool{}

// factsPathID extracts {hostId} from /api/ansible/hosts/{hostId}/facts.
func factsPathID(path string) (int64, bool) {
	tail := strings.Trim(strings.TrimPrefix(path, "/api/ansible/hosts/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) == 2 && parts[1] == "facts" {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		return id, err == nil && id > 0
	}
	return 0, false
}

// handleAnsibleHostFacts:
//
//	GET  /api/ansible/hosts/{hostId}/facts -> {gathered_at, summary, facts}
//	POST /api/ansible/hosts/{hostId}/facts -> {gathered_at, summary} (runs `setup` now)
func (s *Server) handleAnsibleHostFacts(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	hostID, ok := factsPathID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		facts, err := s.db.GetAnsibleHostFacts(hostID)
		if err != nil {
			if errors.Is(err, store.ErrAnsibleHostFactsNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "facts have not been gathered for this host yet"})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, factsResponse(facts))

	case http.MethodPost:
		s.gatherHostFacts(w, r, hostID)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func factsResponse(facts *store.AnsibleHostFacts) map[string]interface{} {
	var summary, raw any = map[string]any{}, map[string]any{}
	_ = json.Unmarshal([]byte(facts.Summary), &summary)
	_ = json.Unmarshal([]byte(facts.Facts), &raw)
	return map[string]interface{}{"gathered_at": facts.GatheredAt, "summary": summary, "facts": raw}
}

func (s *Server) gatherHostFacts(w http.ResponseWriter, r *http.Request, hostID int64) {
	host, err := s.db.GetHost(hostID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.NotFound(w, r)
		return
	}

	factsGatherMu.Lock()
	if factsGatherInFlight[hostID] {
		factsGatherMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "facts are already being gathered for this host"})
		return
	}
	factsGatherInFlight[hostID] = true
	factsGatherMu.Unlock()
	defer func() {
		factsGatherMu.Lock()
		delete(factsGatherInFlight, hostID)
		factsGatherMu.Unlock()
	}()

	privKey, err := s.db.ResolveHostPrivateKey(host)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("failed to resolve private key for host %s: %v", host.Name, err)})
		return
	}
	target := ansible.Target{ID: host.ID, Name: host.Name, Address: host.Host, Port: host.Port, User: host.User, Group: host.GroupName, PrivateKey: privKey}
	cfg, _ := s.db.GetSetting(ansibleConfigSetting) // galat dibiarkan: config kosong berarti default ansible

	gather := s.factsGather
	if gather == nil {
		gather = ansible.GatherFacts
	}
	var logBuf strings.Builder
	facts, err := gather(r.Context(), target, cfg, s.ansibleLibDir, &logBuf)
	if err != nil {
		// The error message is the operator-facing hint (usually a connection or python problem), keep the log out
		// of the JSON body and put it on the server log instead.
		logger.Warn("ansible", host.Name, fmt.Sprintf("Facts gather failed: %v", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	rawJSON, err := json.Marshal(facts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	summaryJSON, err := json.Marshal(ansible.SummarizeFacts(facts))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	at, err := s.db.SetAnsibleHostFacts(hostID, string(rawJSON), string(summaryJSON))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Info("ansible", host.Name, fmt.Sprintf("Gathered %d facts", len(facts)))
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"gathered_at": at,
		"summary":     json.RawMessage(summaryJSON),
	})
}
