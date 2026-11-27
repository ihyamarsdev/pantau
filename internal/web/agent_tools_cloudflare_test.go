package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"pantau/internal/cloudflare"
	"pantau/internal/store"
)

// decideNext waits for the one pending approval, answers it and returns what the card showed.
func decideNext(t *testing.T, srv *Server, approve bool) string {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); pendingCount(srv) == 0; {
		if time.Now().After(end) {
			t.Fatal("no approval became pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
	srv.agent.mu.Lock()
	var id, summary string
	for k, p := range srv.agent.pending {
		id, summary = k, p.Summary
	}
	srv.agent.mu.Unlock()
	body := `{"approve":false}`
	if approve {
		body = `{"approve":true}`
	}
	srv.handleAgentApprovalDecide(newRec(), reqJSON(http.MethodPost, "/api/agent/approvals/"+id, body))
	return summary
}

func TestAgentCloudflareTools(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_approval", "auto") // auto skips gate 1 only: every Cloudflare change must still ask
	fake := fakeCloudflareServer(t, true)
	defer fake.Close()
	srv.cfClient = func(token string) *cloudflare.Client {
		c := cloudflare.New(token)
		c.BaseURL = fake.URL
		return c
	}
	if _, err := db.CreateCloudflareAccount(&store.CloudflareAccount{Name: "main", APIToken: "tok-secret-123"}); err != nil {
		t.Fatal(err)
	}
	const rid = "fedcba9876543210fedcba9876543210"

	if out, _ := callTool(t, srv, "cf_accounts", `{}`); !strings.Contains(out, "main") || strings.Contains(out, "tok-secret-123") {
		t.Fatalf("cf_accounts: %s", out)
	}
	// One account: the account argument is optional; the zone is found by name, case-insensitively.
	if out, _ := callTool(t, srv, "cf_dns_list", `{"zone":"EXAMPLE.com"}`); !strings.Contains(out, "1.2.3.4") {
		t.Fatalf("cf_dns_list: %s", out)
	}
	if out, _ := callTool(t, srv, "cf_dns_list", `{"zone":"example.com","type":"TXT"}`); strings.Contains(out, "1.2.3.4") {
		t.Fatalf("type filter ignored: %s", out)
	}
	if out, _ := callTool(t, srv, "cf_dns_list", `{"zone":"nope.org"}`); !strings.Contains(out, "not found") {
		t.Fatalf("unknown zone: %s", out)
	}

	// Rejected before asking: two purge modes, update without record_id.
	if out, _ := callTool(t, srv, "cf_purge_cache", `{"zone":"example.com","everything":true,"hosts":["a.example.com"]}`); !strings.Contains(out, "exactly one") || pendingCount(srv) != 0 {
		t.Fatalf("purge with two modes: %s", out)
	}
	if out, _ := callTool(t, srv, "cf_dns_change", `{"zone":"example.com","action":"update","content":"9.9.9.9"}`); !strings.Contains(out, "record_id") || pendingCount(srv) != 0 {
		t.Fatalf("update without record_id: %s", out)
	}

	// Denied: nothing runs. Even in auto mode the card is shown.
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "cf_dns_change", `{"zone":"example.com","action":"delete","record_id":"`+rid+`"}`); !strings.Contains(out, "denied") {
		t.Fatalf("denied delete must not run: %s", out)
	}

	// Update keeps the fields the agent did not mention (the API replaces the whole record).
	card := make(chan string, 1)
	go func() { card <- decideNext(t, srv, true) }()
	out, isErr := callTool(t, srv, "cf_dns_change", `{"zone":"example.com","action":"update","record_id":"`+rid+`","content":"9.9.9.9"}`)
	if isErr || !strings.Contains(out, "9.9.9.9") || !strings.Contains(out, `example.com`) || !strings.Contains(out, `"type":"A"`) {
		t.Fatalf("update: %s", out)
	}
	if c := <-card; !strings.Contains(c, "1.2.3.4") || !strings.Contains(c, "9.9.9.9") {
		t.Fatalf("approval card must show before and after: %s", c)
	}

	// A setting outside the allowlist is rejected before asking.
	if out, _ := callTool(t, srv, "cf_zone_setting_set", `{"zone":"example.com","setting":"waf","value":"off"}`); !strings.Contains(out, "non-editable") || pendingCount(srv) != 0 {
		t.Fatalf("bad setting: %s", out)
	}

	// Access rules: reads need no approval; a change shows what it does and an invalid one never asks.
	if out, _ := callTool(t, srv, "cf_access_rules", `{"zone":"example.com"}`); !strings.Contains(out, "203.0.113.7") {
		t.Fatalf("cf_access_rules: %s", out)
	}
	if out, _ := callTool(t, srv, "cf_access_rule_change", `{"zone":"example.com","action":"create","mode":"block","target":"ip","value":"nope"}`); !strings.Contains(out, "invalid IP") || pendingCount(srv) != 0 {
		t.Fatalf("invalid access rule: %s", out)
	}
	if out, _ := callTool(t, srv, "cf_access_rule_change", `{"zone":"example.com","action":"delete"}`); !strings.Contains(out, "rule_id") || pendingCount(srv) != 0 {
		t.Fatalf("delete without rule_id: %s", out)
	}
	card = make(chan string, 1)
	go func() { card <- decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "cf_access_rule_change", `{"zone":"example.com","action":"create","mode":"block","target":"asn","value":"13335"}`); isErr || !strings.Contains(out, "AS13335") {
		t.Fatalf("create access rule: %s", out)
	}
	if c := <-card; !strings.Contains(c, "block") || !strings.Contains(c, "AS13335") {
		t.Fatalf("access rule card: %s", c)
	}
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "cf_access_rule_change", `{"zone":"example.com","action":"delete","rule_id":"`+rid+`"}`); !strings.Contains(out, "denied") {
		t.Fatalf("denied access rule delete must not run: %s", out)
	}

	// Tunnels are read-only; the account id is derived from the zones.
	if out, _ := callTool(t, srv, "cf_tunnels", `{}`); !strings.Contains(out, "edge-tunnel") || !strings.Contains(out, "healthy") {
		t.Fatalf("cf_tunnels: %s", out)
	}
}
