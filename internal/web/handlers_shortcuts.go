package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Custom Shortcuts: operator-defined links to external web addresses, shown as
// grouped tiles in the Shortcuts section.
//
//	GET /api/shortcuts   the stored list
//	PUT /api/shortcuts   replace the whole list
//
// The list is one JSON value in the settings table, so it is part of the
// System Snapshot.
// shortcut: the list is replaced as a whole, so two administrators editing at
// the same time overwrite each other; move to a table with per-row writes when
// that happens.

const (
	shortcutsSettingKey = "shortcuts"
	maxShortcuts        = 200
)

type shortcut struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Group string `json:"group"`
}

func (s *Server) registerShortcutRoutes() {
	s.mux.HandleFunc("/api/shortcuts", s.authMiddleware(s.handleShortcuts))
}

func (s *Server) handleShortcuts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		raw, err := s.db.GetSetting(shortcutsSettingKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		list := []shortcut{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &list); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "stored shortcuts are not valid JSON: " + err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"shortcuts": list})

	case http.MethodPut:
		var body struct {
			Shortcuts []shortcut `json:"shortcuts"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body"})
			return
		}
		list, err := cleanShortcuts(body.Shortcuts)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		raw, err := json.Marshal(list)
		if err == nil {
			err = s.db.SetSetting(shortcutsSettingKey, string(raw))
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"shortcuts": list})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// cleanShortcuts trims and validates a list. Only http and https addresses are
// accepted: the address becomes a link in the UI, and a javascript: address
// would run in the administrator's session.
func cleanShortcuts(in []shortcut) ([]shortcut, error) {
	if len(in) > maxShortcuts {
		return nil, fmt.Errorf("at most %d shortcuts", maxShortcuts)
	}
	out := make([]shortcut, 0, len(in))
	for i, sc := range in {
		sc.Name = strings.TrimSpace(sc.Name)
		sc.URL = strings.TrimSpace(sc.URL)
		sc.Group = strings.TrimSpace(sc.Group)
		if sc.Name == "" || utf8.RuneCountInString(sc.Name) > 64 {
			return nil, fmt.Errorf("shortcut %d: the name must have 1 to 64 characters", i+1)
		}
		if utf8.RuneCountInString(sc.Group) > 64 {
			return nil, fmt.Errorf("shortcut %d: the group must have at most 64 characters", i+1)
		}
		u, err := url.Parse(sc.URL)
		if err != nil || len(sc.URL) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return nil, fmt.Errorf("shortcut %d: the address must be an http:// or https:// URL", i+1)
		}
		out = append(out, sc)
	}
	return out, nil
}
