package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/topology"
)

const topologyCacheTTL = 20 * time.Second

// GET /api/topology returns the tiny macro graph from the in-memory TTL cache.
// No SSH or vendor API call happens on this path; structural config writes
// invalidate the cache so the map never shows stale structure for long.
func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	g, err := s.topologyCache.Get(time.Now())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok": false, "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// GET /api/topology/host?id=N returns service detail for one host, loaded only
// when a node card is clicked. Containers and listening ports never enter the
// global graph.
func (s *Server) handleTopologyHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host id"})
		return
	}
	host, err := s.db.GetHost(id)
	if err != nil || host == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}

	detail := topology.HostDetail{HostID: id, Containers: []topology.ServiceRow{}, Listening: []topology.ServiceRow{}}
	detail.Listening = topology.ParseListening(host.ListeningPorts)
	if cs, err := s.db.ListHostContainers(id); err == nil {
		for _, c := range cs {
			detail.Containers = append(detail.Containers, topology.ServiceRow{
				Name: c.Name, Detail: c.Image, Status: c.State,
			})
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

// invalidateTopology is called after host/network/cluster config writes.
func (s *Server) invalidateTopology() {
	if s.topologyCache != nil {
		s.topologyCache.Invalidate()
	}
}
