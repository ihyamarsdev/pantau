package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"pantau/internal/logger"
)

// Custom Shortcuts: operator-defined links to external web addresses, shown as
// grouped tiles in the Shortcuts section.
//
//	GET /api/shortcuts   the stored list (passwords redacted)
//	PUT /api/shortcuts   replace the whole list
//	GET /shortcut-login/<id>  auto login page for a shortcut that stores a password
//
// The list is one JSON value in the settings table, so it is part of the
// System Snapshot.
// shortcut: the list is replaced as a whole, so two administrators editing at
// the same time overwrite each other; move to a table with per-row writes when
// that happens.

const (
	shortcutsSettingKey = "shortcuts"
	maxShortcuts        = 200
	// loginFieldPattern is what a form field name may look like; it becomes the
	// name attribute of a hidden input.
	loginFieldPattern = `^[A-Za-z0-9_.\[\]]{1,64}$`
	shortcutLoginPath = "/shortcut-login/"
)

var loginFieldRegex = regexp.MustCompile(loginFieldPattern)

type shortcut struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Group string `json:"group"`

	// Auto login. A stored password turns the tile into a link to
	// /shortcut-login/<id>, a Pantau page that posts these fields to the site's
	// own login form (a cross-origin form POST that the browser performs as a
	// normal navigation) and then lets the site redirect as it always does.
	LoginUser      string `json:"login_user,omitempty"`
	LoginPass      string `json:"login_pass,omitempty"`     // write-only: never sent back to the browser
	LoginPassSet   bool   `json:"login_pass_set,omitempty"` // read-only: whether a password is stored
	LoginClear     bool   `json:"login_clear,omitempty"`    // write-only: forget the stored password
	LoginAction    string `json:"login_action,omitempty"`   // form action; defaults to the shortcut address
	LoginUserField string `json:"login_user_field,omitempty"`
	LoginPassField string `json:"login_pass_field,omitempty"`
	LoginExtra     string `json:"login_extra,omitempty"` // extra hidden fields, "remember=1&submit=Login"
}

func (s *Server) registerShortcutRoutes() {
	s.mux.HandleFunc("/api/shortcuts", s.authMiddleware(s.handleShortcuts))
	s.mux.HandleFunc(shortcutLoginPath, s.authMiddleware(s.handleShortcutLogin))
}

// loadShortcuts reads and decodes the stored list. An empty setting is an empty list.
func (s *Server) loadShortcuts() ([]shortcut, error) {
	raw, err := s.db.GetSetting(shortcutsSettingKey)
	if err != nil {
		return nil, err
	}
	list := []shortcut{}
	if raw == "" {
		return list, nil
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("stored shortcuts are not valid JSON: %w", err)
	}
	return list, nil
}

// redactShortcuts is the list as the browser may see it: the password never
// leaves the server, only that there is one.
func redactShortcuts(list []shortcut) []shortcut {
	out := make([]shortcut, len(list))
	for i, sc := range list {
		sc.LoginPassSet = sc.LoginPass != ""
		sc.LoginPass = ""
		out[i] = sc
	}
	return out
}

func (s *Server) handleShortcuts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.loadShortcuts()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"shortcuts": redactShortcuts(list)})

	case http.MethodPut:
		var body struct {
			Shortcuts []shortcut `json:"shortcuts"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body"})
			return
		}
		prev, err := s.loadShortcuts()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		list, err := cleanShortcuts(body.Shortcuts, prev)
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
		writeJSON(w, http.StatusOK, map[string]interface{}{"shortcuts": redactShortcuts(list)})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleShortcutLogin answers the address of a tile that has a stored password:
// a page whose form holds the credentials and submits itself to the site, so one
// click on the shortcut lands signed in. The page is never cached and carries no
// referrer, so the target site does not learn the Pantau address.
func (s *Server) handleShortcutLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, shortcutLoginPath)
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	list, err := s.loadShortcuts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var sc *shortcut
	for i := range list {
		if list[i].ID == id {
			sc = &list[i]
			break
		}
	}
	if sc == nil || sc.LoginPass == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(shortcutLoginPage(*sc)))
	logger.Info("shortcuts", "", fmt.Sprintf("auto login to %q (%s) from shortcut %q", sc.URL, sc.LoginAction, sc.Name))
}

// shortcutLoginPage builds the self-submitting form. Every value is escaped: the
// password is data in the page, never markup.
func shortcutLoginPage(sc shortcut) string {
	var fields strings.Builder
	writeField := func(name, value string) {
		fmt.Fprintf(&fields, "<input type=\"hidden\" name=\"%s\" value=\"%s\">\n",
			html.EscapeString(name), html.EscapeString(value))
	}
	if sc.LoginUser != "" {
		writeField(sc.LoginUserField, sc.LoginUser)
	}
	writeField(sc.LoginPassField, sc.LoginPass)
	if extra := parseExtraFields(sc.LoginExtra); extra != nil {
		for _, name := range extra.names {
			writeField(name, extra.values.Get(name))
		}
	}
	name := html.EscapeString(sc.Name)
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="referrer" content="no-referrer">
<title>Signing in to ` + name + `</title>
</head>
<body>
<form id="shortcutLogin" method="post" action="` + html.EscapeString(sc.LoginAction) + `" referrerpolicy="no-referrer">
` + fields.String() + `<noscript><button type="submit">Sign in to ` + name + `</button></noscript>
</form>
<script>document.getElementById('shortcutLogin').submit();</script>
</body>
</html>
`
}

// parsedExtra holds the hidden fields of a validated "a=1&b=2" string with its
// keys in a stable order, so the page is the same on every request.
type parsedExtra struct {
	names  []string
	values url.Values
}

func parseExtraFields(extra string) *parsedExtra {
	if extra == "" {
		return nil
	}
	values, err := url.ParseQuery(extra)
	if err != nil || len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Strings(names)
	return &parsedExtra{names: names, values: values}
}

// cleanShortcuts trims and validates a list. Only http and https addresses are
// accepted: the address becomes a link in the UI, and a javascript: address
// would run in the administrator's session.
//
// A password sent empty is not a request to clear it: the browser only ever sees
// that a password is set, so an empty one keeps what prev holds for the same id,
// and only while the address is unchanged (a shortcut moved to another site must
// not carry the old site's credentials).
func cleanShortcuts(in []shortcut, prev []shortcut) ([]shortcut, error) {
	if len(in) > maxShortcuts {
		return nil, fmt.Errorf("at most %d shortcuts", maxShortcuts)
	}
	kept := make(map[string]shortcut, len(prev))
	for _, p := range prev {
		kept[p.ID] = p
	}
	usedID := make(map[string]bool, len(in))
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
		if !validShortcutURL(sc.URL) {
			return nil, fmt.Errorf("shortcut %d: the address must be an http:// or https:// URL", i+1)
		}

		sc.ID = strings.TrimSpace(sc.ID)
		if !validShortcutID(sc.ID) || usedID[sc.ID] {
			sc.ID = newShortcutID()
		}
		usedID[sc.ID] = true

		if err := cleanShortcutLogin(&sc, kept[sc.ID]); err != nil {
			return nil, fmt.Errorf("shortcut %d: %w", i+1, err)
		}
		sc.LoginPassSet = false // derived when the list is sent to the browser
		out = append(out, sc)
	}
	return out, nil
}

// cleanShortcutLogin validates the auto login part of one shortcut and applies
// the keep-the-stored-password rule.
func cleanShortcutLogin(sc *shortcut, prev shortcut) error {
	sc.LoginUser = strings.TrimSpace(sc.LoginUser)
	sc.LoginAction = strings.TrimSpace(sc.LoginAction)
	sc.LoginUserField = strings.TrimSpace(sc.LoginUserField)
	sc.LoginPassField = strings.TrimSpace(sc.LoginPassField)
	sc.LoginExtra = strings.TrimSpace(sc.LoginExtra)

	if sc.LoginClear {
		sc.LoginPass = ""
	} else if sc.LoginPass == "" && prev.ID == sc.ID && prev.URL == sc.URL {
		sc.LoginPass = prev.LoginPass
	}
	sc.LoginClear = false // write-only, derived on the next save
	if sc.LoginPass == "" {
		// No password, no login: a half-configured form is dropped whole.
		sc.LoginUser, sc.LoginAction, sc.LoginUserField, sc.LoginPassField, sc.LoginExtra = "", "", "", "", ""
		return nil
	}

	if utf8.RuneCountInString(sc.LoginUser) > 256 {
		return fmt.Errorf("the user name must have at most 256 characters")
	}
	if len(sc.LoginPass) > 1024 {
		return fmt.Errorf("the password must have at most 1024 characters")
	}
	if sc.LoginAction == "" {
		sc.LoginAction = sc.URL
	} else if !validShortcutURL(sc.LoginAction) {
		return fmt.Errorf("the login address must be an http:// or https:// URL")
	}
	if sc.LoginUserField == "" {
		sc.LoginUserField = "username"
	}
	if sc.LoginPassField == "" {
		sc.LoginPassField = "password"
	}
	if !loginFieldRegex.MatchString(sc.LoginUserField) {
		return fmt.Errorf("the user field name may hold letters, digits, _ . [ ] only")
	}
	if !loginFieldRegex.MatchString(sc.LoginPassField) {
		return fmt.Errorf("the password field name may hold letters, digits, _ . [ ] only")
	}
	if sc.LoginUser == "" && sc.LoginUserField == sc.LoginPassField {
		return fmt.Errorf("without a user name the user and password fields must not be the same")
	}
	if sc.LoginExtra != "" {
		if len(sc.LoginExtra) > 1024 {
			return fmt.Errorf("the extra fields must have at most 1024 characters")
		}
		values, err := url.ParseQuery(sc.LoginExtra)
		if err != nil {
			return fmt.Errorf("the extra fields must be a query string such as remember=1&submit=Login")
		}
		clean := url.Values{}
		for name, vals := range values {
			name = strings.TrimSpace(name)
			if !loginFieldRegex.MatchString(name) {
				return fmt.Errorf("the extra field %q may hold letters, digits, _ . [ ] only", name)
			}
			if len(vals) > 1 {
				return fmt.Errorf("the extra field %q may appear only once", name)
			}
			for _, v := range vals {
				clean.Add(name, strings.TrimSpace(v))
			}
		}
		if len(clean) > 16 {
			return fmt.Errorf("at most 16 extra fields")
		}
		sc.LoginExtra = clean.Encode() // canonical, so the page is stable
	}
	return nil
}

func validShortcutURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

func validShortcutID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// newShortcutID is the stable handle a tile links to; guids are not needed, 8
// random bytes over 200 shortcuts do not collide in practice, and the caller
// regenerates a repeat anyway.
func newShortcutID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
