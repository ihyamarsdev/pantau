package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestCleanShortcuts(t *testing.T) {
	t.Parallel()
	got, err := cleanShortcuts([]shortcut{{Name: " Grafana ", URL: " https://grafana.example.com:3000/d/1 ", Group: " Monitoring "}}, nil)
	if err != nil {
		t.Fatalf("valid shortcut rejected: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d shortcuts, want 1", len(got))
	}
	// Every shortcut gets the stable id a tile with a saved password links to.
	if !validShortcutID(got[0].ID) {
		t.Errorf("id %q is not a 16 character hex handle", got[0].ID)
	}
	want := shortcut{ID: got[0].ID, Name: "Grafana", URL: "https://grafana.example.com:3000/d/1", Group: "Monitoring"}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}

	// Each of these would be a link that runs script, opens no site, or has no label.
	rejected := []shortcut{
		{Name: "x", URL: "javascript:alert(1)"},
		{Name: "x", URL: "https://javascript:alert(1)"},
		{Name: "x", URL: "data:text/html,<script>alert(1)</script>"},
		{Name: "x", URL: "ftp://files.example.com"},
		{Name: "x", URL: "example.com"},
		{Name: "x", URL: "https://"},
		{Name: "x", URL: ""},
		{Name: " ", URL: "https://example.com"},
	}
	for _, sc := range rejected {
		if _, err := cleanShortcuts([]shortcut{sc}, nil); err == nil {
			t.Errorf("accepted %+v", sc)
		}
	}

	if _, err := cleanShortcuts(make([]shortcut, maxShortcuts+1), nil); err == nil {
		t.Errorf("accepted more than %d shortcuts", maxShortcuts)
	}
}

func TestCleanShortcutsLogin(t *testing.T) {
	t.Parallel()
	// A first save stores the password and fills the defaults: the site's own address as
	// the form action, username/password as the field names.
	got, err := cleanShortcuts([]shortcut{{
		Name: "Panel", URL: "https://panel.example.com/",
		LoginUser: "admin", LoginPass: "s3cret", LoginExtra: " submit = Login &remember=1 ",
	}}, nil)
	if err != nil {
		t.Fatalf("auto login shortcut rejected: %v", err)
	}
	sc := got[0]
	if sc.LoginPass != "s3cret" || sc.LoginUser != "admin" {
		t.Fatalf("credentials lost: %+v", sc)
	}
	if sc.LoginAction != "https://panel.example.com/" || sc.LoginUserField != "username" || sc.LoginPassField != "password" {
		t.Fatalf("defaults not applied: %+v", sc)
	}
	if sc.LoginExtra != "remember=1&submit=Login" {
		t.Errorf("extra fields not canonical: %q", sc.LoginExtra)
	}
	if sc.LoginPassSet {
		t.Error("login_pass_set is derived for the browser, never stored")
	}

	// The browser never sees the password, only that there is one.
	red := redactShortcuts(got)
	if red[0].LoginPass != "" || !red[0].LoginPassSet {
		t.Fatalf("redacted shortcut leaked the password or lost the flag: %+v", red[0])
	}
	raw, _ := json.Marshal(red)
	if strings.Contains(string(raw), "s3cret") {
		t.Errorf("redacted list still holds the password: %s", raw)
	}

	// An edit that sends no password keeps the stored one, as long as the address is the same.
	kept, err := cleanShortcuts([]shortcut{{
		ID: sc.ID, Name: "Panel 2", URL: sc.URL, LoginUser: "admin",
	}}, got)
	if err != nil {
		t.Fatalf("edit rejected: %v", err)
	}
	if kept[0].LoginPass != "s3cret" || kept[0].Name != "Panel 2" {
		t.Fatalf("the stored password was not kept: %+v", kept[0])
	}

	// The same id pointed at another site must not carry the old site's credentials.
	moved, err := cleanShortcuts([]shortcut{{
		ID: sc.ID, Name: "Panel", URL: "https://other.example.com/", LoginUser: "admin",
	}}, got)
	if err != nil {
		t.Fatalf("move rejected: %v", err)
	}
	if moved[0].LoginPass != "" || moved[0].LoginAction != "" || moved[0].LoginUser != "" {
		t.Fatalf("credentials survived a move to another site: %+v", moved[0])
	}

	// The checkbox that forgets a password drops every login field with it.
	cleared, err := cleanShortcuts([]shortcut{{
		ID: sc.ID, Name: "Panel", URL: sc.URL, LoginClear: true,
	}}, got)
	if err != nil {
		t.Fatalf("clear rejected: %v", err)
	}
	if cleared[0].LoginPass != "" || cleared[0].LoginClear || cleared[0].LoginAction != "" {
		t.Fatalf("password not forgotten: %+v", cleared[0])
	}

	// Values that would break the posted form are refused.
	bad := []shortcut{
		{Name: "x", URL: "https://x.example.com", LoginPass: "p", LoginAction: "ftp://x.example.com"},
		{Name: "x", URL: "https://x.example.com", LoginPass: "p", LoginPassField: "pass word"},
		{Name: "x", URL: "https://x.example.com", LoginPass: "p", LoginPassField: "\"><script>alert(1)</script>"},
		{Name: "x", URL: "https://x.example.com", LoginPass: "p", LoginExtra: "%%%"},
		{Name: "x", URL: "https://x.example.com", LoginPass: "p", LoginPassField: "username"},
	}
	for _, sc := range bad {
		if _, err := cleanShortcuts([]shortcut{sc}, nil); err == nil {
			t.Errorf("accepted %+v", sc)
		}
	}
}

// shortcutServer is a server with one stored shortcut that has a saved password.
func shortcutServer(t *testing.T) (*Server, *http.Cookie, shortcut) {
	t.Helper()
	db, err := store.Open(testDBCopy(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "shortcuts_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	list, err := cleanShortcuts([]shortcut{{
		Name: "Panel", URL: "https://panel.example.com/", Group: "Ops",
		LoginUser: "admin", LoginPass: `pa<ss>&"'`, LoginAction: "https://panel.example.com/login",
	}}, nil)
	if err != nil {
		t.Fatalf("seed shortcut: %v", err)
	}
	raw, _ := json.Marshal(list)
	if err := db.SetSetting(shortcutsSettingKey, string(raw)); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	return srv, &http.Cookie{Name: "pantau_session", Value: token}, list[0]
}

func TestShortcutLoginPagePostsCredentials(t *testing.T) {
	t.Parallel()
	srv, cookie, sc := shortcutServer(t)

	req := httptest.NewRequest("GET", shortcutLoginPath+sc.ID, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`action="https://panel.example.com/login"`,
		`name="username" value="admin"`,
		`name="password" value="pa&lt;ss&gt;&amp;&#34;&#39;"`, // the password is data in the page, never markup
		`document.getElementById('shortcutLogin').submit()`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login page misses %q:\n%s", want, body)
		}
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a page holding a password must not be cached, got %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("the target site must not learn the Pantau address, got %q", got)
	}
}

func TestShortcutLoginRequiresSessionAndKnownID(t *testing.T) {
	t.Parallel()
	srv, cookie, sc := shortcutServer(t)

	req := httptest.NewRequest("GET", shortcutLoginPath+sc.ID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", w.Code)
	}

	for _, path := range []string{shortcutLoginPath + "0123456789abcdef", shortcutLoginPath + "nope", shortcutLoginPath + sc.ID + "/x"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, w.Code)
		}
	}
}

// The list the browser receives never carries a password, and an edit keeps it.
func TestShortcutsAPIRedactsPassword(t *testing.T) {
	t.Parallel()
	srv, cookie, sc := shortcutServer(t)

	req := httptest.NewRequest("GET", "/api/shortcuts", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "pa<ss>") {
		t.Fatalf("GET leaked the password: %s", w.Body.String())
	}
	var got struct {
		Shortcuts []shortcut `json:"shortcuts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Shortcuts) != 1 || !got.Shortcuts[0].LoginPassSet || got.Shortcuts[0].ID != sc.ID {
		t.Fatalf("unexpected list: %+v", got.Shortcuts)
	}

	// What the dialog does: send the redacted list back with a new name and no password.
	got.Shortcuts[0].Name = "Panel (prod)"
	body, _ := json.Marshal(map[string]interface{}{"shortcuts": got.Shortcuts})
	req = httptest.NewRequest("PUT", "/api/shortcuts", strings.NewReader(string(body)))
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stored, err := srv.loadShortcuts()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(stored) != 1 || stored[0].Name != "Panel (prod)" || stored[0].LoginPass != `pa<ss>&"'` {
		t.Fatalf("the saved password must survive a rename: %+v", stored)
	}
}
