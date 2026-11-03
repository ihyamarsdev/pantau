package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

func validateClusterName(name string) error {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) == 0 {
		return errors.New("name is required")
	}
	if len(trimmed) > 64 {
		return errors.New("name must not exceed 64 characters")
	}
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			return errors.New("name contains invalid control characters")
		}
	}
	return nil
}

func validateClusterKubeMode(mode string) error {
	switch mode {
	case "full", "no-exec", "read-only":
		return nil
	default:
		return errors.New("invalid kube_mode: must be full, no-exec, or read-only")
	}
}

// handleKubeClustersContexts handles POST /api/kube/clusters/contexts
func (s *Server) handleKubeClustersContexts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kubeconfig string `json:"kubeconfig"`
	}
	if !decodeKubeBody(w, r, &req) {
		return
	}
	ctxs, err := inspector.ListKubeconfigContexts(req.Kubeconfig)
	if err != nil {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":              true,
		"contexts":        ctxs.Contexts,
		"current_context": ctxs.Current,
	})
}

// handleKubeClusters handles GET /api/kube/clusters and POST /api/kube/clusters
func (s *Server) handleKubeClusters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		clusters, err := s.db.ListKubeClusters()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":       true,
			"clusters": clusters,
		})

	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			Kubeconfig string `json:"kubeconfig"`
			Context    string `json:"context"`
			KubeMode   string `json:"kube_mode"`
		}
		if !decodeKubeBody(w, r, &req) {
			return
		}

		name := strings.TrimSpace(req.Name)
		if err := validateClusterName(name); err != nil {
			writeKubeError(w, fmt.Errorf("%w: %s", inspector.ErrKubeInvalid, err.Error()))
			return
		}

		kubeMode := strings.TrimSpace(req.KubeMode)
		if kubeMode == "" {
			kubeMode = "full"
		}
		if err := validateClusterKubeMode(kubeMode); err != nil {
			writeKubeError(w, fmt.Errorf("%w: %s", inspector.ErrKubeInvalid, err.Error()))
			return
		}

		prep, err := inspector.PrepareKubeconfig(req.Kubeconfig, strings.TrimSpace(req.Context))
		if err != nil {
			writeKubeError(w, err)
			return
		}

		cluster := &store.KubeCluster{
			Name:       name,
			Context:    prep.Context,
			Server:     prep.Server,
			Provider:   prep.Provider,
			KubeMode:   kubeMode,
			Kubeconfig: prep.Kubeconfig,
		}

		id, err := s.db.CreateKubeCluster(cluster)
		if err != nil {
			if errors.Is(err, store.ErrKubeClusterNameTaken) {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"ok":    false,
					"error": err.Error(),
				})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cluster.ID = id

		// Verify connectivity with KubeDetect
		targetID := inspector.KubeClusterTarget(id)
		info, err := s.inspector.KubeDetect(targetID)
		if err != nil {
			// Rollback cluster on verification failure
			_ = s.db.DeleteKubeCluster(id)
			logger.Error("kube", name, fmt.Sprintf("Cluster verification failed, removing cluster: %v", err))
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}

		if info != nil && info.ClusterID != "" {
			_ = s.db.SetKubeClusterUID(id, info.ClusterID)
		}
		if stored, _ := s.db.GetKubeCluster(id); stored != nil {
			cluster = stored
		}

		logger.Info("kube", name, fmt.Sprintf("Cluster %s added successfully (provider: %s)", name, cluster.Provider))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"cluster": cluster,
			"info":    info,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleKubeClusterRoute dispatches /api/kube/clusters/{id} and /api/kube/clusters/{id}/<sub...>
func (s *Server) handleKubeClusterRoute(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/kube/clusters/")
	parts := strings.Split(subpath, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	clusterID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || clusterID <= 0 {
		http.Error(w, "invalid cluster id", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		// /api/kube/clusters/{id}
		switch r.Method {
		case http.MethodPut:
			var req struct {
				Name       string  `json:"name"`
				KubeMode   string  `json:"kube_mode"`
				Kubeconfig *string `json:"kubeconfig"`
				Context    *string `json:"context"`
			}
			if !decodeKubeBody(w, r, &req) {
				return
			}

			name := strings.TrimSpace(req.Name)
			if err := validateClusterName(name); err != nil {
				writeKubeError(w, fmt.Errorf("%w: %s", inspector.ErrKubeInvalid, err.Error()))
				return
			}

			kubeMode := strings.TrimSpace(req.KubeMode)
			if kubeMode == "" {
				kubeMode = "full"
			}
			if err := validateClusterKubeMode(kubeMode); err != nil {
				writeKubeError(w, fmt.Errorf("%w: %s", inspector.ErrKubeInvalid, err.Error()))
				return
			}

			existing, err := s.db.GetKubeCluster(clusterID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if existing == nil {
				http.Error(w, "cluster not found", http.StatusNotFound)
				return
			}

			existing.Name = name
			existing.KubeMode = kubeMode

			if req.Kubeconfig != nil && *req.Kubeconfig != "" {
				ctxName := ""
				if req.Context != nil {
					ctxName = strings.TrimSpace(*req.Context)
				}
				prep, err := inspector.PrepareKubeconfig(*req.Kubeconfig, ctxName)
				if err != nil {
					writeKubeError(w, err)
					return
				}
				existing.Kubeconfig = prep.Kubeconfig
				existing.Context = prep.Context
				existing.Server = prep.Server
				existing.Provider = prep.Provider
			} else {
				existing.Kubeconfig = "" // Tells UpdateKubeCluster to keep old kubeconfig/server/etc.
			}

			if err := s.db.UpdateKubeCluster(existing); err != nil {
				if errors.Is(err, store.ErrKubeClusterNameTaken) {
					writeJSON(w, http.StatusConflict, map[string]interface{}{
						"ok":    false,
						"error": err.Error(),
					})
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			updated, err := s.db.GetKubeCluster(clusterID)
			if err != nil || updated == nil {
				http.Error(w, "failed to reload cluster", http.StatusInternalServerError)
				return
			}

			logger.Info("kube", updated.Name, fmt.Sprintf("Cluster %s updated", updated.Name))
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":      true,
				"cluster": updated,
			})

		case http.MethodDelete:
			cluster, _ := s.db.GetKubeCluster(clusterID)
			if err := s.db.DeleteKubeCluster(clusterID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			clusterName := fmt.Sprintf("cluster-%d", clusterID)
			if cluster != nil {
				clusterName = cluster.Name
			}
			logger.Info("kube", clusterName, fmt.Sprintf("Cluster %s deleted", clusterName))
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok": true,
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/kube/clusters/{id}/<sub...>
	// Construct parts as: []string{parts[0], "kube", parts[1], parts[2]...}
	kubeParts := append([]string{parts[0], "kube"}, parts[1:]...)
	targetID := inspector.KubeClusterTarget(clusterID)
	if s.handleKubeManage(w, r, targetID, kubeParts) {
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}
