package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// A k3s/RKE2 cluster reached over SSH has no table of its own: it is the hosts sharing one kube_cluster_id.
// The operator's display name for it lives in settings as a JSON map uid -> name, so System Snapshot carries it.
const kubeHostClusterNamesKey = "kube_host_cluster_names"

type kubeHostCluster struct {
	UID     string       `json:"cluster_id"`
	Name    string       `json:"name"`
	Custom  bool         `json:"custom"` // name set by the operator, not derived
	Members []store.Host `json:"-"`
}

func (s *Server) kubeHostClusterNames() map[string]string {
	m := map[string]string{}
	if raw, _ := s.db.GetSetting(kubeHostClusterNamesKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

// Default name: the group shared by every member, else the first member's name (mirrors the Kubernetes picker).
func defaultKubeHostClusterName(m []store.Host) string {
	g := strings.TrimSpace(m[0].GroupName)
	for _, h := range m[1:] {
		if strings.TrimSpace(h.GroupName) != g {
			g = ""
			break
		}
	}
	if g != "" {
		return g
	}
	return m[0].Name
}

func (s *Server) kubeHostClusters() []kubeHostCluster {
	hosts, _ := s.db.ListHosts()
	names := s.kubeHostClusterNames()
	var out []kubeHostCluster
	idx := map[string]int{}
	for _, h := range hosts {
		if h.KubeClusterID == "" {
			continue
		}
		i, ok := idx[h.KubeClusterID]
		if !ok {
			i = len(out)
			idx[h.KubeClusterID] = i
			out = append(out, kubeHostCluster{UID: h.KubeClusterID})
		}
		out[i].Members = append(out[i].Members, h)
	}
	for i := range out {
		c := &out[i]
		if n := names[c.UID]; n != "" {
			c.Name, c.Custom = n, true
		} else {
			c.Name = defaultKubeHostClusterName(c.Members)
		}
	}
	return out
}

// target is the member to address first: the healthiest one (failover covers the rest).
func (c kubeHostCluster) target() *store.Host {
	best := &c.Members[0]
	for i := 1; i < len(c.Members); i++ {
		if statusPriority(c.Members[i].Status) < statusPriority(best.Status) {
			best = &c.Members[i]
		}
	}
	return best
}

// mode is the strictest kube_mode among members, so picking a member never loosens the policy.
func (c kubeHostCluster) mode() string {
	mode := "full"
	for _, h := range c.Members {
		if h.KubeMode == "read-only" {
			return "read-only"
		}
		if h.KubeMode == "no-exec" {
			mode = "no-exec"
		}
	}
	return mode
}

// handleKubeHostCluster handles PUT /api/kube/host-clusters/{cluster_id}: rename (empty name = back to the derived name).
func (s *Server) handleKubeHostCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid := strings.TrimPrefix(r.URL.Path, "/api/kube/host-clusters/")
	var req struct {
		Name string `json:"name"`
	}
	if !decodeKubeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)

	clusters := s.kubeHostClusters()
	found := false
	for _, c := range clusters {
		found = found || c.UID == uid
	}
	if !found {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}

	names := s.kubeHostClusterNames()
	if name == "" {
		delete(names, uid)
	} else {
		if err := validateClusterName(name); err != nil {
			writeKubeError(w, fmt.Errorf("%w: %s", inspector.ErrKubeInvalid, err.Error()))
			return
		}
		taken := false
		ext, _ := s.db.ListKubeClusters()
		for _, c := range ext {
			taken = taken || strings.EqualFold(c.Name, name)
		}
		for _, c := range clusters {
			taken = taken || (c.UID != uid && strings.EqualFold(c.Name, name))
		}
		if taken {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "error": store.ErrKubeClusterNameTaken.Error()})
			return
		}
		names[uid] = name
	}
	b, _ := json.Marshal(names)
	if err := s.db.SetSetting(kubeHostClusterNamesKey, string(b)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "host_clusters": s.kubeHostClusters()})
}
