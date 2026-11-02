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
	"pantau/internal/store"
)

func TestRulesValidation_HTTP_TLS_Backup(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	ins := inspector.New(db, nil, nil)
	srv := NewServer(db, ins, nil)
	token := "rules_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}

	hID, err := db.CreateHost(&store.Host{
		Name: "Test Server",
		Host: "192.168.1.10",
		User: "root",
		Port: 22,
	})
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	rulesURL := fmt.Sprintf("/api/hosts/%d/rules", hID)

	tests := []struct {
		name       string
		rule       store.DesiredRule
		wantStatus int
	}{
		// HTTP valid
		{
			name:       "valid http rule",
			rule:       store.DesiredRule{Kind: "http", Target: "https://example.com/healthz", Expected: "200,<1500ms"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid http default expected",
			rule:       store.DesiredRule{Kind: "http", Target: "http://10.0.0.1:8080/metrics", Expected: ""},
			wantStatus: http.StatusOK,
		},
		// HTTP invalid target
		{
			name:       "http target missing scheme",
			rule:       store.DesiredRule{Kind: "http", Target: "example.com/healthz", Expected: "200"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "http target shell metacharacter",
			rule:       store.DesiredRule{Kind: "http", Target: "https://example.com;rm -rf /", Expected: "200"},
			wantStatus: http.StatusBadRequest,
		},
		// HTTP invalid expected
		{
			name:       "http invalid status expected",
			rule:       store.DesiredRule{Kind: "http", Target: "https://example.com", Expected: "99"},
			wantStatus: http.StatusBadRequest,
		},
		// TLS valid
		{
			name:       "valid tls rule default port",
			rule:       store.DesiredRule{Kind: "tls", Target: "example.com", Expected: ">=14d"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid tls rule https prefix",
			rule:       store.DesiredRule{Kind: "tls", Target: "https://example.com:8443", Expected: ">=30d"},
			wantStatus: http.StatusOK,
		},
		// TLS invalid target
		{
			name:       "tls target invalid scheme",
			rule:       store.DesiredRule{Kind: "tls", Target: "http://example.com", Expected: ">=14d"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "tls target path not allowed",
			rule:       store.DesiredRule{Kind: "tls", Target: "https://example.com/api", Expected: ">=14d"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "tls target shell injection",
			rule:       store.DesiredRule{Kind: "tls", Target: "example.com;id", Expected: ">=14d"},
			wantStatus: http.StatusBadRequest,
		},
		// TLS invalid expected
		{
			name:       "tls invalid expected format",
			rule:       store.DesiredRule{Kind: "tls", Target: "example.com", Expected: "14d"},
			wantStatus: http.StatusBadRequest,
		},
		// Backup valid
		{
			name:       "valid backup legacy rule",
			rule:       store.DesiredRule{Kind: "backup", Target: "/var/backups/db-*.sql.gz", Expected: "fresh_24h"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid backup with verify gzip",
			rule:       store.DesiredRule{Kind: "backup", Target: "/var/backups/db-*.sql.gz", Expected: "fresh_24h,verify=gzip"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid backup with verify sha256",
			rule:       store.DesiredRule{Kind: "backup", Target: "/data/backup-2026.tar", Expected: "fresh_12h,verify=sha256"},
			wantStatus: http.StatusOK,
		},
		// Backup invalid target
		{
			name:       "backup relative path",
			rule:       store.DesiredRule{Kind: "backup", Target: "var/backups/db.sql", Expected: "fresh_24h"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "backup directory traversal",
			rule:       store.DesiredRule{Kind: "backup", Target: "/var/backups/../etc/passwd", Expected: "fresh_24h"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "backup shell injection semicolon",
			rule:       store.DesiredRule{Kind: "backup", Target: "/var/backups/db;rm", Expected: "fresh_24h"},
			wantStatus: http.StatusBadRequest,
		},
		// Backup invalid expected
		{
			name:       "backup invalid verify type",
			rule:       store.DesiredRule{Kind: "backup", Target: "/var/backups/db.sql.gz", Expected: "fresh_24h,verify=zip"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.rule)
			req := httptest.NewRequest(http.MethodPost, rulesURL, bytes.NewReader(body))
			authReq(req)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("POST %s status = %d, want %d (body: %s)", rulesURL, w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
