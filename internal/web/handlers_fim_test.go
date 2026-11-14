package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// setupFIMServer builds a server whose SSH runner answers with a fixed manifest.
func setupFIMServer(t *testing.T, manifest string) (*Server, *store.DB, func(*http.Request)) {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		return manifest, "", 0, nil
	}
	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)
	srv := NewServer(db, ins, nil)
	token := "fim_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, db, authReq
}

const fimManifest = "FIM:OK /etc/passwd aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nFIM:DONE\n"

func TestFIMRuleValidation(t *testing.T) {
	srv, db, authReq := setupFIMServer(t, fimManifest)
	hID, err := db.CreateHost(&store.Host{Name: "Test", Host: "192.168.1.10", User: "root", Port: 22})
	if err != nil {
		t.Fatal(err)
	}
	rulesURL := fmt.Sprintf("/api/hosts/%d/rules", hID)
	valid, err := inspector.EncodeFIMBaseline([]inspector.FIMFile{{Path: "/etc/passwd", Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		rule store.DesiredRule
		want int
	}{
		{"valid file_integrity rule", store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: valid}, http.StatusOK},
		{"relative path", store.DesiredRule{Kind: "file_integrity", Target: "etc/passwd", Expected: valid}, http.StatusBadRequest},
		{"shell injection", store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd;rm -rf /", Expected: valid}, http.StatusBadRequest},
		{"missing baseline", store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: "baseline"}, http.StatusBadRequest},
		{"empty baseline", store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: ""}, http.StatusBadRequest},
		{"tampered baseline", store.DesiredRule{Kind: "file_integrity", Target: "/etc/passwd", Expected: "sha256:" + "a" + "aaa|m:/etc/passwd=bbbb"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.rule)
			req := httptest.NewRequest(http.MethodPost, rulesURL, bytes.NewReader(body))
			authReq(req)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("code = %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestFIMBaselineEndpointPreviewsWithoutSaving(t *testing.T) {
	srv, db, authReq := setupFIMServer(t, fimManifest)
	hID, _ := db.CreateHost(&store.Host{Name: "Test", Host: "192.168.1.10", User: "root", Port: 22})

	body, _ := json.Marshal(map[string]string{"target": "/etc/passwd"})
	url := fmt.Sprintf("/api/hosts/%d/rules/fim/baseline", hID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		OK       bool   `json:"ok"`
		Expected string `json:"expected"`
		Files    []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Expected == "" || len(resp.Files) != 1 {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	// Nothing must be stored yet: the preview is not a rule.
	rules, _ := db.ListDesiredRules(hID)
	if len(rules) != 0 {
		t.Fatalf("the baseline preview must not save a rule, got %d", len(rules))
	}
	// An invalid target is rejected before anything reaches the host.
	bad, _ := json.Marshal(map[string]string{"target": "etc/passwd;id"})
	req = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(bad))
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid target code = %d, want 400", w.Code)
	}
}
