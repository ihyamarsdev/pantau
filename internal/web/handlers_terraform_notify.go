package web

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	tfNotifySetting = "tf_notify_events"
	tfNotifyDefault = "awaiting_approval,errored,drift"
	tfNotifyNone    = "none" // an empty setting means "never configured", so "all off" needs a token
)

var tfNotifyNames = []string{"awaiting_approval", "errored", "applied", "drift"}

// tfNotifyParse turns the stored comma list into the event map; unknown tokens are ignored.
func tfNotifyParse(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		raw = tfNotifyDefault
	}
	on := map[string]bool{}
	for _, n := range tfNotifyNames {
		on[n] = false
	}
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if _, ok := on[tok]; ok {
			on[tok] = true
		}
	}
	return on
}

func tfNotifyFormat(on map[string]bool) string {
	var out []string
	for _, n := range tfNotifyNames {
		if on[n] {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return tfNotifyNone
	}
	return strings.Join(out, ",")
}

func tfNotifyEnabled(db *store.DB, event string) bool {
	raw, _ := db.GetSetting(tfNotifySetting)
	return tfNotifyParse(raw)[event]
}

// validateTFPlanAuto checks the schedule/auto-apply fields shared by workspace POST and PUT.
func validateTFPlanAuto(planInterval int, autoApply bool, environment string) error {
	if planInterval < 0 {
		return errors.New("plan_interval_minutes must be >= 0")
	}
	if autoApply && tfIsProduction(environment) {
		return errors.New("auto_apply is not allowed for production workspaces")
	}
	return nil
}

// wireTFEvents points the run manager's notification sink at the dispatcher.
func (s *Server) wireTFEvents() {
	if s.tfMgr != nil && s.dispatcher != nil {
		s.tfMgr.events = s.dispatcher.SendEvent
	}
}

// handleTerraformNotifications serves GET/PUT /api/terraform/notifications.
func (s *Server) handleTerraformNotifications(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}
	raw, _ := s.db.GetSetting(tfNotifySetting)
	on := tfNotifyParse(raw)
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var req struct {
			Events map[string]bool `json:"events"`
		}
		if !decodeTerraformBody(w, r, &req) {
			return
		}
		for k, v := range req.Events { // omitted events keep their current value
			if _, ok := on[k]; !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown event " + k})
				return
			}
			on[k] = v
		}
		if err := s.db.SetSetting(tfNotifySetting, tfNotifyFormat(on)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("terraform", "", "Terraform notification events updated: "+tfNotifyFormat(on))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": on})
}

// handleTerraformNotificationsTest serves POST /api/terraform/notifications/test.
func (s *Server) handleTerraformNotificationsTest(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := s.dispatcher.SendEventSync("Pantau Terraform", "Test notification", "Sample Terraform event from Pantau.")
	var errs []string
	for _, c := range res {
		if !c.OK {
			errs = append(errs, c.Channel+": "+c.Error)
		}
	}
	sort.Strings(errs)
	switch {
	case len(res) == 0:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "no notification channels configured"})
	case len(errs) > 0:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": strings.Join(errs, "; ")})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
