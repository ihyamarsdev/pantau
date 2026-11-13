package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

func TestAgentCheckArgs(t *testing.T) {
	sc := schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost, "tail": map[string]interface{}{"type": "integer"},
		"action": map[string]interface{}{"type": "string", "enum": []string{"start", "stop"}}, "force": map[string]interface{}{"type": "boolean"}})
	for args, want := range map[string]string{
		`{"host_id":1,"action":"start"}`:                 "",
		`{"host_id":1,"action":"start","tail":null}`:     "", // null is an omitted argument
		`{"action":"start"}`:                             "host_id is required",
		`{"host_id":"1","action":"start"}`:               "host_id must be of type integer",
		`{"host_id":1.5,"action":"start"}`:               "host_id must be of type integer",
		`{"host_id":1,"action":"remove"}`:                "action must be one of: start, stop",
		`{"host_id":1,"action":"start","force":"yes"}`:   "force must be of type boolean",
		`{"host_id":1,"action":"start","tail_lines":50}`: `unknown argument "tail_lines"`,
	} {
		got := ""
		if err := checkArgs(sc, jsonMap(t, args)); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", args, got, want)
		}
	}
}

func jsonMap(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A tool that forgets agentGate must not be able to change anything through the UI handlers.
func TestAgentLoopbackRefusesUngatedChange(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	if code, _ := srv.loopback(t.Context(), http.MethodPost, "/api/alerts/ack-all", nil); code != http.StatusForbidden {
		t.Fatalf("an ungated POST must be refused, got %d", code)
	}
	if code, _ := srv.loopback(t.Context(), http.MethodGet, "/api/hosts", nil); code != http.StatusOK {
		t.Fatalf("reads stay open, got %d", code)
	}
}

func TestAgentHostTools(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	host := &store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"}
	hostID, err := db.CreateHost(host)
	if err != nil {
		t.Fatal(err)
	}
	host.ID, host.TopCPUProcesses = hostID, `[{"name":"mysqld","cpu":93.5}]`
	if err := db.UpdateHostInspection(host); err != nil { // telemetry is only written by an inspection
		t.Fatal(err)
	}
	h := itoa(hostID)

	if out := callTool(t, srv, "host_info", `{"host_id":`+h+`,"what":"resources"}`); !strings.Contains(out, "mysqld") || strings.Contains(out, `"isError":true`) {
		t.Fatalf("resources must carry the stored process list: %s", out)
	}

	// An invalid firewall request is refused before the operator is asked.
	if out := callTool(t, srv, "firewall_action", `{"host_id":`+h+`,"backend":"ufw","action":"add"}`); !strings.Contains(out, `"isError":true`) || pendingCount(srv) != 0 {
		t.Fatalf("firewall add without a rule: %s (pending %d)", out, pendingCount(srv))
	}

	alertID, err := db.CreateAlert(&store.Alert{HostID: hostID, HostName: "web-1", Level: "critical", Message: "nginx.service failed"})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetSetting("agent_approval", "auto") // gate 1 passes without a card
	if out := callTool(t, srv, "alert_ack", `{"alert_id":`+itoa(alertID)+`}`); !strings.Contains(out, "acknowledged") {
		t.Fatalf("ack: %s", out)
	}
	if left, _ := db.ListActiveAlerts(); len(left) != 0 {
		t.Fatalf("alert still active: %+v", left)
	}
	if out := callTool(t, srv, "alert_ack", `{"alert_id":`+itoa(alertID)+`}`); !strings.Contains(out, "no active alert") {
		t.Fatalf("second ack must say the alert is gone: %s", out)
	}
}

// The approval card shows the whole action, so one that cannot be shown is refused instead of cut.
func TestAgentGateRefusesOversizedSummary(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	err := srv.agentGateAs(approvalReq(), 0, "Ansible", "ansible_run", strings.Repeat("x", agentSummaryMax+1), gateInfra)
	if err == nil || !strings.Contains(err.Error(), "too large") || pendingCount(srv) != 0 {
		t.Fatalf("oversized action: err=%v pending=%d", err, pendingCount(srv))
	}
}

func TestAgentCannotTouchOperatorNotes(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	note, err := db.AddAgentMemory(nil, "Prod database is host db-1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(note.ID)
	for _, c := range []struct{ tool, args string }{
		{"memory_save", `{"content":"Prod database is host evil","replaces":` + id + `}`},
		{"memory_forget", `{"id":` + id + `}`},
	} {
		if out := callTool(t, srv, c.tool, c.args); !strings.Contains(out, "written by the operator") {
			t.Fatalf("%s on an operator note must be refused: %s", c.tool, out)
		}
	}
	if notes, _ := db.ListAgentMemory(0, ""); len(notes) != 1 || notes[0].Content != "Prod database is host db-1" {
		t.Fatalf("operator note changed: %+v", notes)
	}
}

// list_hosts is tab-separated: a name with a tab or line break must stay one cell of one row.
func TestAgentListHostsRows(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	if _, err := db.CreateHost(&store.Host{Name: "web-1\n99\tevil", Host: "10.0.0.1", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			Content []struct{ Text string }
		}
	}
	if err := json.Unmarshal([]byte(callTool(t, srv, "list_hosts", `{}`)), &resp); err != nil || len(resp.Result.Content) != 1 {
		t.Fatalf("list_hosts: %v %+v", err, resp)
	}
	lines := strings.Split(strings.TrimSpace(resp.Result.Content[0].Text), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a header and one row, got %q", lines)
	}
	head, row := strings.Split(lines[0], "\t"), strings.Split(lines[1], "\t")
	if len(row) != len(head) || row[1] != "web-1 99 evil" || row[2] != "root@10.0.0.1:22" {
		t.Fatalf("row does not line up with the header: %q / %q", head, row)
	}
}

// Several units are one request: one approval card naming all of them, and nothing asked when one is invalid.
func TestAgentSystemdActionBatch(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h := `{"host_id":` + itoa(hostID) + `,"action":"stop",`

	many := strings.Repeat(`"a.service",`, agentSystemdBatchMax) + `"a.service"`
	for _, units := range []string{`"nginx.service","x;reboot.service"`, many, ``} {
		if out := callTool(t, srv, "systemd_action", h+`"units":[`+units+`]}`); !strings.Contains(out, `"isError":true`) || pendingCount(srv) != 0 {
			t.Fatalf("units [%s] must be refused before asking: %s (pending %d)", units, out, pendingCount(srv))
		}
	}

	card := make(chan string, 1)
	go func() { card <- decideNext(t, srv, false) }()
	out := callTool(t, srv, "systemd_action", h+`"units":["redis.service","rabbitmq-server.service"]}`)
	if c := <-card; c != "systemctl stop redis.service rabbitmq-server.service" {
		t.Fatalf("approval card: %q", c)
	}
	if !strings.Contains(out, "denied") || pendingCount(srv) != 0 {
		t.Fatalf("a denied batch must not run: %s", out)
	}
}

func TestAgentSystemdRows(t *testing.T) {
	svcs := []inspector.SystemdService{
		{Name: "nginx.service", Load: "loaded", Active: "active", Sub: "running", Enabled: "enabled", Description: "A high performance web server"},
		{Name: "php8.2-fpm.service", Load: "loaded", Active: "failed", Sub: "failed", Enabled: "enabled", Description: "The PHP FastCGI Process Manager"},
		{Name: "aicare.service", Load: "loaded", Active: "inactive", Sub: "dead", Description: "AI Care worker"},
	}
	units := func(only bool, filter string) string {
		var names []string
		for _, l := range strings.Split(strings.TrimSpace(systemdRows(svcs, only, filter)), "\n")[1:] {
			names = append(names, strings.Split(l, "\t")[0])
		}
		return strings.Join(names, " ")
	}
	for _, c := range []struct {
		only         bool
		filter, want string
	}{
		{false, "", "nginx.service php8.2-fpm.service aicare.service"},
		{true, "", "php8.2-fpm.service"},
		{false, "PHP", "php8.2-fpm.service"},   // case-insensitive, by name
		{false, "web server", "nginx.service"}, // by description
		{true, "nginx", ""},                    // both conditions must hold
	} {
		if got := units(c.only, c.filter); got != c.want {
			t.Errorf("only=%v filter=%q: got %q, want %q", c.only, c.filter, got, c.want)
		}
	}
	// An unknown enable state is an empty cell, so the columns still line up with the header.
	if rows := strings.Split(systemdRows(svcs, false, "aicare"), "\n"); len(strings.Split(rows[1], "\t")) != len(strings.Split(rows[0], "\t")) {
		t.Errorf("row does not line up with the header: %q", rows)
	}
}
