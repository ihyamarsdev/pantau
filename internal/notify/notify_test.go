package notify

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/store"
)

type recordedReq struct {
	Method string
	Header http.Header
	Body   string
}

func setupTestDB(t *testing.T) (*store.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "pantau-notify-test-*")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatal(err)
	}
	return db, func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
}

func TestNotify_NtfySendAlert(t *testing.T) {
	var gotReq recordedReq
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotReq = recordedReq{
			Method: r.Method,
			Header: r.Header.Clone(),
			Body:   string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	_ = db.SetSetting("ntfy_url", ts.URL+"/pantau-alerts")
	_ = db.SetSetting("ntfy_token", "tk_test_secret_123")

	d := New(db)
	host := &store.Host{ID: 1, Name: "web-01", Host: "10.0.0.1"}
	rule := &store.DesiredRule{Kind: "service", Target: "nginx", Expected: "active"}

	err := d.sendNtfy(ts.URL+"/pantau-alerts", "tk_test_secret_123", host.Name, "Service nginx down", "Unit nginx not found")
	if err != nil {
		t.Fatalf("sendNtfy failed: %v", err)
	}

	if gotReq.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", gotReq.Method)
	}
	if gotReq.Header.Get("Title") != "Pantau Alert: web-01" {
		t.Errorf("unexpected Title header: %s", gotReq.Header.Get("Title"))
	}
	if gotReq.Header.Get("Priority") != "high" {
		t.Errorf("unexpected Priority header: %s", gotReq.Header.Get("Priority"))
	}
	if gotReq.Header.Get("Tags") != "rotating_light" {
		t.Errorf("unexpected Tags header: %s", gotReq.Header.Get("Tags"))
	}
	if gotReq.Header.Get("Authorization") != "Bearer tk_test_secret_123" {
		t.Errorf("unexpected Authorization header: %s", gotReq.Header.Get("Authorization"))
	}
	if !strings.Contains(gotReq.Body, "Host: web-01") || !strings.Contains(gotReq.Body, "Service nginx down") {
		t.Errorf("unexpected body: %s", gotReq.Body)
	}
	_ = rule
}

func TestNotify_SlackSendAlert(t *testing.T) {
	var gotReq recordedReq
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotReq = recordedReq{
			Method: r.Method,
			Header: r.Header.Clone(),
			Body:   string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	d := New(db)
	err := d.sendSlack(ts.URL, "db-prod", "Disk / full", "98% used")
	if err != nil {
		t.Fatalf("sendSlack failed: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(gotReq.Body), &payload); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}
	text := payload["text"]
	if !strings.Contains(text, "db-prod") || !strings.Contains(text, "Disk / full") {
		t.Errorf("unexpected slack text: %s", text)
	}
}

func TestNotify_DiscordSendAlert(t *testing.T) {
	var gotReq recordedReq
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotReq = recordedReq{
			Method: r.Method,
			Header: r.Header.Clone(),
			Body:   string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	d := New(db)
	// Long root cause to test <= 1800 char truncation
	longRootCause := strings.Repeat("A", 2500)
	err := d.sendDiscord(ts.URL, "k8s-node-01", "Memory high", longRootCause)
	if err != nil {
		t.Fatalf("sendDiscord failed: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(gotReq.Body), &payload); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}
	content := payload["content"]
	if len(content) > 1800 {
		t.Errorf("discord content length %d exceeds 1800", len(content))
	}
	if !strings.Contains(content, "...[truncated]") {
		t.Errorf("expected truncated marker in content")
	}
}

func TestNotify_SMTPSend(t *testing.T) {
	// Tiny TCP server speaking minimal SMTP
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	host, port, _ := net.SplitHostPort(ln.Addr().String())

	var receivedLines []string
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		writer := bufio.NewWriter(conn)
		reader := bufio.NewReader(conn)

		// Greeting
		fmt.Fprintf(writer, "220 smtp.local ESMTP PantauTest\r\n")
		writer.Flush()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimRight(line, "\r\n")
			receivedLines = append(receivedLines, line)

			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
				fmt.Fprintf(writer, "250-smtp.local\r\n250 HELP\r\n")
				writer.Flush()
			case strings.HasPrefix(upper, "MAIL FROM:"):
				fmt.Fprintf(writer, "250 2.1.0 Ok\r\n")
				writer.Flush()
			case strings.HasPrefix(upper, "RCPT TO:"):
				fmt.Fprintf(writer, "250 2.1.5 Ok\r\n")
				writer.Flush()
			case strings.HasPrefix(upper, "DATA"):
				fmt.Fprintf(writer, "354 End data with <CR><LF>.<CR><LF>\r\n")
				writer.Flush()
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					receivedLines = append(receivedLines, strings.TrimRight(dataLine, "\r\n"))
					if strings.TrimRight(dataLine, "\r\n") == "." {
						fmt.Fprintf(writer, "250 2.0.0 Ok: queued\r\n")
						writer.Flush()
						break
					}
				}
			case strings.HasPrefix(upper, "QUIT"):
				fmt.Fprintf(writer, "221 2.0.0 Bye\r\n")
				writer.Flush()
				return
			default:
				fmt.Fprintf(writer, "250 Ok\r\n")
				writer.Flush()
			}
		}
	}()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	d := New(db)
	cfg := SMTPConfig{
		Host:     host,
		Port:     port,
		From:     "pantau@example.com",
		To:       []string{"ops@example.com", "alerts@example.com"},
		Security: "none",
	}

	err = d.sendSMTP(cfg, "web-01", "Port 80 unreachable", "Connection refused")
	if err != nil {
		t.Fatalf("sendSMTP failed: %v", err)
	}

	<-serverDone

	joined := strings.Join(receivedLines, "\n")
	if !strings.Contains(joined, "Subject: [Pantau] web-01: Port 80 unreachable") {
		t.Errorf("expected Subject in SMTP data, got:\n%s", joined)
	}
	if !strings.Contains(joined, "ops@example.com") {
		t.Errorf("expected ops@example.com in SMTP lines, got:\n%s", joined)
	}
}

type fakeSMTPSender struct {
	called bool
	err    error
}

func (f *fakeSMTPSender) Send(cfg SMTPConfig, subject, body string) error {
	f.called = true
	return f.err
}

func TestNotify_TestNotification_PerChannelResults(t *testing.T) {
	// Fake Slack & Discord endpoints
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slack" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/discord" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	_ = db.SetSetting("slack_webhook_url", ts.URL+"/slack")
	_ = db.SetSetting("discord_webhook_url", ts.URL+"/discord")
	_ = db.SetSetting("smtp_host", "mail.example.com")
	_ = db.SetSetting("smtp_to", "ops@example.com")

	d := New(db)
	fakeSMTP := &fakeSMTPSender{}
	d.SetSMTPSender(fakeSMTP)

	resp := d.TestNotification()

	if resp.OK {
		t.Errorf("expected overall OK=false due to discord error")
	}
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Results))
	}

	resMap := make(map[string]ChannelResult)
	for _, r := range resp.Results {
		resMap[r.Channel] = r
	}

	if !resMap["slack"].OK {
		t.Errorf("expected slack to succeed, got error: %s", resMap["slack"].Error)
	}
	if resMap["discord"].OK {
		t.Errorf("expected discord to fail")
	}
	if !resMap["smtp"].OK {
		t.Errorf("expected smtp to succeed with fake sender")
	}
	if !fakeSMTP.called {
		t.Errorf("expected fakeSMTPSender to be called")
	}
}
