package web

// GET /api/network-radar/globe — the geo-located node/flow payload for the 3D
// Globe Network Radar (ADR 0090). No SSH or vendor API call happens here; the
// payload comes from a short-lived in-memory cache over stored inventory.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"pantau/internal/logger"
)

const globeCacheTTL = 20 * time.Second

func (s *Server) handleGlobe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.globeCache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "globe unavailable"})
		return
	}
	g, err := s.globeCache.Get(time.Now())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// StartGeoResolver runs the background GeoIP resolver once at startup and then
// on a slow ticker. It never blocks the HTTP read path; failures are logged and
// retried on the next tick. Cancellable via ctx (main.go owns the lifetime).
func (s *Server) StartGeoResolver(ctx context.Context) {
	if s.geoResolver == nil {
		return
	}
	go func() {
		_ = s.geoResolver.RunOnce(ctx)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.geoResolver.RunOnce(ctx); err != nil {
					logger.Warn("globe", "", "GeoIP refresh failed: "+err.Error())
				}
				s.invalidateGlobe()
			}
		}
	}()
}

func (s *Server) invalidateGlobe() {
	if s.globeCache != nil {
		s.globeCache.Invalidate()
	}
}

// POST /api/network-radar/globe/geo sets the manual geo override for a host.
// Body: {id, lat, lon, label}. Used for air-gapped/private-IP hosts (ADR 0077).
func (s *Server) handleGlobeSetGeo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID    int64   `json:"id"`
		Lat   float64 `json:"lat"`
		Lon   float64 `json:"lon"`
		Label string  `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid coordinates"})
		return
	}
	host, err := s.db.GetHost(req.ID)
	if err != nil || host == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "host not found"})
		return
	}
	if err := s.db.UpdateHostGeo(req.ID, req.Lat, req.Lon, req.Label); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateGlobe()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
