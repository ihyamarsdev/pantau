package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"strconv"
	"strings"
	"testing"
	"time"
)

// aptPreviewOutput is what the detection script prints on an apt host with two pending updates.
const aptPreviewOutput = `===OS_RELEASE===
NAME="Ubuntu"
ID=ubuntu
===REBOOT===
===CACHE_AGE===
1700000000
---APT---
1700000000
---DNF---
---APK---
---PACMAN---
===UPDATES===
MGR:apt
Inst bash [5.0-6ubuntu1.1] (5.0-6ubuntu1.2 Ubuntu:22.04/jammy-security [amd64])
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
`

func TestPackageUpdatesCheckEndpoint(t *testing.T) {
	t.Parallel()
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "===OS_RELEASE===") {
			return aptPreviewOutput, "", 0, nil
		}
		return "", "", 0, nil
	}
	srv, db := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)

	hostID, _ := db.CreateHost(&store.Host{Name: "host-apt", Host: "10.0.0.1", Port: 22, User: "root"})
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/packages/updates", hostID), nil)
	req.AddCookie(authCookie)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Checked bool `json:"checked"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if body.Checked {
		t.Errorf("a host without a check must answer checked=false, got %+v", body)
	}

	// 2. Run the check: the pending packages and the manager come back.
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/packages/updates/check", hostID), nil)
	req.AddCookie(authCookie)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on check, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		OK    bool `json:"ok"`
		Check struct {
			Manager         string `json:"manager"`
			PendingTotal    int    `json:"pending_total"`
			PendingSecurity int    `json:"pending_security"`
			Packages        []struct {
				Name     string `json:"name"`
				From     string `json:"from"`
				To       string `json:"to"`
				Security bool   `json:"security"`
			} `json:"packages"`
		} `json:"check"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !out.OK || out.Check.Manager != "apt" {
		t.Fatalf("unexpected check result: %+v", out)
	}
	if out.Check.PendingTotal != 2 || out.Check.PendingSecurity != 1 {
		t.Errorf("want 2 pending / 1 security, got %d / %d", out.Check.PendingTotal, out.Check.PendingSecurity)
	}
	if len(out.Check.Packages) != 2 || out.Check.Packages[0].Name != "bash" || !out.Check.Packages[0].Security {
		t.Errorf("unexpected packages: %+v", out.Check.Packages)
	}

	// 3. The stored result is readable and the cooldown is advertised.
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/packages/updates", hostID), nil)
	req.AddCookie(authCookie)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if rem := w.Header().Get("X-Cooldown-Remaining"); rem == "" {
		t.Errorf("expected the cooldown header after a check, got none")
	}

	// 4. A second check right away is refused (anti-flood), not run.
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/packages/updates/check", hostID), nil)
	req.AddCookie(authCookie)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on the second check, got %d: %s", w.Code, w.Body.String())
	}
	var cd struct {
		CooldownRemaining int `json:"cooldown_remaining"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cd); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if cd.CooldownRemaining <= 0 {
		t.Errorf("expected a positive cooldown_remaining, got %d", cd.CooldownRemaining)
	}
}

// A host whose package manager cannot be queried must not look clean: the stored check keeps the error.
func TestPackageUpdatesCheckKeepsTheError(t *testing.T) {
	t.Parallel()
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "===OS_RELEASE===") {
			return "===OS_RELEASE===\nID=ubuntu\n===REBOOT===\n===CACHE_AGE===\n===UPDATES===\nMGR:apt\nE: Unable to locate package\n", "", 0, nil
		}
		return "", "", 0, nil
	}
	srv, db := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)
	hostID, _ := db.CreateHost(&store.Host{Name: "host-broken", Host: "10.0.0.2", Port: 22, User: "root"})

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/packages/updates/check", hostID), nil)
	req.AddCookie(authCookie)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	stored, err := db.GetPackageUpdateCheck(hostID)
	if err != nil {
		t.Fatalf("GetPackageUpdateCheck failed: %v", err)
	}
	if stored == nil {
		t.Fatal("expected a stored check")
	}
	if stored.Error == "" {
		t.Errorf("a failed check must keep its error, got %+v", stored)
	}
}

// The cooldown is measured from the stored check, so a check older than the window is allowed again.
func TestPackageUpdateCooldownExpires(t *testing.T) {
	t.Parallel()
	old := &store.PackageUpdateCheck{HostID: 1, CheckedAt: time.Now().Add(-2 * time.Minute), Supported: true}
	if rem := pkgUpdateCooldownRemaining(old); rem != 0 {
		t.Errorf("an expired cooldown must allow another check, got %d seconds", rem)
	}
	fresh := &store.PackageUpdateCheck{HostID: 1, CheckedAt: time.Now(), Supported: true}
	rem := pkgUpdateCooldownRemaining(fresh)
	if rem <= 0 || rem > 60 {
		t.Errorf("a fresh check must block for up to 60 seconds, got %d", rem)
	}
	if pkgUpdateCooldownRemaining(nil) != 0 {
		t.Errorf("no stored check must not block, got %d", pkgUpdateCooldownRemaining(nil))
	}
}

// The store keeps the last check per host and replaces it on the next one.
func TestPackageUpdateCheckStore(t *testing.T) {
	t.Parallel()
	dbPath := testDBCopy(t)
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	hostID, _ := db.CreateHost(&store.Host{Name: "h", Host: "10.0.0.9", Port: 22, User: "root"})

	got, err := db.GetPackageUpdateCheck(hostID)
	if err != nil {
		t.Fatalf("GetPackageUpdateCheck failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected no check before the first save, got %+v", got)
	}

	first := &store.PackageUpdateCheck{
		HostID: hostID, CheckedAt: time.Now().UTC(), Supported: true, Manager: "apt",
		PendingTotal: 2, PendingSecurity: 1, RepoCacheAgeHours: 3,
		Packages: []store.PackageUpdate{{Name: "bash", From: "1", To: "2", Security: true}},
	}
	if err := db.SavePackageUpdateCheck(first); err != nil {
		t.Fatalf("SavePackageUpdateCheck failed: %v", err)
	}
	second := &store.PackageUpdateCheck{
		HostID: hostID, CheckedAt: time.Now().UTC(), Supported: true, Manager: "apt", PendingTotal: 0,
	}
	if err := db.SavePackageUpdateCheck(second); err != nil {
		t.Fatalf("SavePackageUpdateCheck failed: %v", err)
	}

	got, err = db.GetPackageUpdateCheck(hostID)
	if err != nil {
		t.Fatalf("GetPackageUpdateCheck failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected a stored check")
	}
	if got.PendingTotal != 0 {
		t.Errorf("the second check must replace the first, got pending_total=%d", got.PendingTotal)
	}
	if got.Packages == nil {
		t.Errorf("packages must never be nil in JSON, got %v", got.Packages)
	}
	if len(got.Packages) != 0 {
		t.Errorf("expected the second check to have no packages, got %+v", got.Packages)
	}
}

// The check endpoint must not answer for a host that does not exist.
func TestPackageUpdatesUnknownHost(t *testing.T) {
	t.Parallel()
	runner := sshrunner.NewMockRunner()
	srv, _ := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/hosts/"+strconv.FormatInt(999999, 10)+"/packages/updates", nil)
	req.AddCookie(authCookie)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown host, got %d: %s", w.Code, w.Body.String())
	}
}
