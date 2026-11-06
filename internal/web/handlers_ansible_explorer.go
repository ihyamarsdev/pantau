package web

import (
	"encoding/json"
	"net/http"
	"time"

	"pantau/internal/ansible"
)

type explorerHost struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	Status        string `json:"status"`
	GroupName     string `json:"group_name"`
	AnsibleAlias  string `json:"ansible_alias"`
	FactsAt       any    `json:"facts_at"`
	AnsibleStatus string `json:"ansible_status"` // online | offline from the host's latest finished ad-hoc ping, else unknown
	// AnsibleCheckedAt is when that ping finished; null when the host was never pinged.
	AnsibleCheckedAt *time.Time `json:"ansible_checked_at"`
}

type explorerGroup struct {
	Group string         `json:"group"`
	Hosts []explorerHost `json:"hosts"`
}

// handleAnsibleExplorer: GET /api/ansible/explorer -> {groups:[{group,hosts}], ungrouped:[host]}
// The alias comes from the same InventoryJSON call the runner uses, so the name shown here is the name ansible sees.
func (s *Server) handleAnsibleExplorer(w http.ResponseWriter, r *http.Request) {
	if !s.checkAnsibleGate(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hosts, err := s.db.ListHosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	targets := make([]ansible.Target, 0, len(hosts))
	for i := range hosts {
		// The key content is irrelevant to the alias map: keyPath is never called here.
		targets = append(targets, ansible.Target{ID: hosts[i].ID, Name: hosts[i].Name, Group: hosts[i].GroupName})
	}
	_, aliases, err := ansible.InventoryJSON(targets, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	factsAt, err := s.db.ListAnsibleHostFactsGatheredAt()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pings, err := s.latestPingByAlias()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliasByID := make(map[int64]string, len(aliases))
	for alias, id := range aliases {
		aliasByID[id] = alias
	}
	byID := make(map[int64]explorerHost, len(hosts))
	for i := range hosts {
		h := &hosts[i]
		eh := explorerHost{
			ID: h.ID, Name: h.Name, Address: h.Host, Port: h.Port, Status: h.Status,
			GroupName: h.GroupName, AnsibleAlias: aliasByID[h.ID], AnsibleStatus: "unknown",
		}
		if p, ok := pings[eh.AnsibleAlias]; ok {
			eh.AnsibleStatus, eh.AnsibleCheckedAt = p.status, &p.at
		}
		if at, ok := factsAt[h.ID]; ok {
			eh.FactsAt = at
		}
		byID[h.ID] = eh
	}

	groups := make([]explorerGroup, 0)
	index := make(map[string]int)
	ungrouped := make([]explorerHost, 0)
	for i := range hosts {
		eh := byID[hosts[i].ID]
		if eh.GroupName == "" {
			ungrouped = append(ungrouped, eh)
			continue
		}
		gi, ok := index[eh.GroupName]
		if !ok {
			groups = append(groups, explorerGroup{Group: eh.GroupName, Hosts: []explorerHost{}})
			gi = len(groups) - 1
			index[eh.GroupName] = gi
		}
		groups[gi].Hosts = append(groups[gi].Hosts, eh)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"groups": groups, "ungrouped": ungrouped})
}

type pingSeen struct {
	status string
	at     time.Time
}

// latestPingByAlias reads the newest finished ad-hoc `ping` result of every alias from the stored history, so the
// explorer shows reachability without opening a new SSH connection. Runs are newest first: the first hit wins.
func (s *Server) latestPingByAlias() (map[string]pingSeen, error) {
	runs, err := s.db.ListAnsibleAdhocRuns(200)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]pingSeen)
	for _, run := range runs {
		if !isPingModule(run.Module) || run.FinishedAt == nil || run.Results == "" {
			continue
		}
		var results []ansible.AdhocResult
		if json.Unmarshal([]byte(run.Results), &results) != nil {
			continue
		}
		for _, r := range results {
			if _, done := seen[r.Alias]; done {
				continue
			}
			status := "offline"
			if r.Status == "success" || r.Status == "changed" {
				status = "online"
			}
			seen[r.Alias] = pingSeen{status: status, at: *run.FinishedAt}
		}
	}
	return seen, nil
}

func isPingModule(m string) bool { return m == "ping" || m == "ansible.builtin.ping" }
