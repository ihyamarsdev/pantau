package web

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

// fleetServer builds a server with one host and the agent enabled.
func fleetServer(t *testing.T) (*Server, *store.DB, int64) {
	t.Helper()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	_ = db.SetSetting("agent_enabled", "1")
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	return srv, db, hostID
}

// Every tool of this area must be registered with an object schema and a description.
func TestAgentFleetToolsListed(t *testing.T) {
	want := []string{"fleet_hosts", "fleet_host_action", "fleet_host_groups", "fleet_host_import", "fleet_inspect_all",
		"fleet_rules", "fleet_rule_action", "fleet_alerts_ack", "fleet_notes", "fleet_presets",
		"fleet_shortcuts", "fleet_forwards", "ssh_keys", "fleet_registry",
		"fleet_settings", "fleet_s3_accounts", "fleet_notify_channels", "fleet_snapshot"}
	have := map[string]agentTool{}
	for _, tl := range agentTools {
		if _, dup := have[tl.Name]; dup {
			t.Fatalf("duplicate tool %s", tl.Name)
		}
		have[tl.Name] = tl
	}
	for _, n := range want {
		tl, ok := have[n]
		if !ok || tl.run == nil || tl.InputSchema["type"] != "object" {
			t.Fatalf("tool %s missing or malformed", n)
		}
		if strings.TrimSpace(tl.Description) == "" {
			t.Fatalf("tool %s has no description", n)
		}
	}
	// A gate-3 action must never be skipped by the auto mode.
	for _, n := range []string{"fleet_host_action", "fleet_host_groups", "fleet_host_import", "fleet_rule_action",
		"fleet_notes", "fleet_presets", "fleet_shortcuts", "fleet_settings", "fleet_s3_accounts", "fleet_notify_channels", "fleet_snapshot", "ssh_keys"} {
		if have[n].gate != gateInfra {
			t.Fatalf("%s must be gate 3", n)
		}
	}
}

// The read tools answer from the stored data, and a name in the result is never a secret.
func TestAgentFleetReads(t *testing.T) {
	srv, db, hostID := fleetServer(t)
	_ = db.SetSetting("telegram_token", "tg-secret-123")
	_ = db.SetSetting("smtp_pass", "smtp-secret-456")
	_ = db.SetSetting("snapshot_passphrase", "snap-secret-789")

	if out, isErr := callTool(t, srv, "fleet_hosts", `{}`); isErr || !strings.Contains(out, "web-1") || !strings.Contains(out, "prod") {
		t.Fatalf("fleet_hosts: %s", out)
	}
	if out, _ := callTool(t, srv, "fleet_hosts", `{}`); strings.Contains(out, "secret") {
		t.Fatalf("fleet_hosts leaked a secret: %s", out)
	}

	// The settings read shows the general keys but never a secret value.
	out, isErr := callTool(t, srv, "fleet_settings", `{}`)
	if isErr {
		t.Fatalf("fleet_settings: %s", out)
	}
	if !strings.Contains(out, "poll_interval_sec") {
		t.Fatalf("fleet_settings must show the general settings: %s", out)
	}
	for _, secret := range []string{"tg-secret-123", "smtp-secret-456", "snap-secret-789"} {
		if strings.Contains(out, secret) {
			t.Fatalf("fleet_settings leaked %s: %s", secret, out)
		}
	}

	// The SSH Key Library lists the public key only.
	if _, err := db.CreateSSHKey(&store.SSHKey{Name: "deploy", PrivateKey: "priv-secret-key", PublicKey: "ssh-ed25519 AAAA deploy", Fingerprint: "SHA256:abc"}); err != nil {
		t.Fatal(err)
	}
	out, isErr = callTool(t, srv, "ssh_keys", `{}`)
	if isErr || !strings.Contains(out, "deploy") || !strings.Contains(out, "SHA256:abc") {
		t.Fatalf("ssh_keys: %s", out)
	}
	if strings.Contains(out, "priv-secret-key") {
		t.Fatalf("ssh_keys leaked a private key: %s", out)
	}

	// The rules of a host are listed with kind, target and expected.
	if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: "container", Target: "nginx", Expected: "running", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	out, isErr = callTool(t, srv, "fleet_rules", `{"host_id":`+fleetItoa(hostID)+`}`)
	if isErr || !strings.Contains(out, "nginx") || !strings.Contains(out, "running") {
		t.Fatalf("fleet_rules: %s", out)
	}

	// The global note is read back.
	if err := db.SetGlobalNotes("runbook: check the disk"); err != nil {
		t.Fatal(err)
	}
	if out, isErr := callTool(t, srv, "fleet_notes", `{}`); isErr || !strings.Contains(out, "check the disk") {
		t.Fatalf("fleet_notes: %s", out)
	}

	// The S3 accounts are listed without their secret key.
	if _, err := db.CreateS3Account(&store.S3Account{Name: "backup", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "AKIA123", SecretKey: "s3-secret"}); err != nil {
		t.Fatal(err)
	}
	out, isErr = callTool(t, srv, "fleet_s3_accounts", `{}`)
	if isErr || !strings.Contains(out, "backup") {
		t.Fatalf("fleet_s3_accounts: %s", out)
	}
	if strings.Contains(out, "s3-secret") {
		t.Fatalf("fleet_s3_accounts leaked the secret key: %s", out)
	}
}

// A wrong argument must be an error and must change nothing.
func TestAgentFleetValidation(t *testing.T) {
	srv, db, hostID := fleetServer(t)
	_ = db.SetSetting("agent_approval", "auto")

	if out, isErr := callTool(t, srv, "fleet_host_action", `{"host_id":1,"action":"reboot"}`); !isErr {
		t.Fatalf("unknown action must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "fleet_host_action", `{"host_id":999999,"action":"delete"}`); !isErr {
		t.Fatalf("unknown host must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "fleet_rule_action", `{"action":"save","host_id":1,"kind":"nope","target":"x","expected":"y"}`); !isErr {
		t.Fatalf("unknown rule kind must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "fleet_rule_action", `{"action":"save","host_id":1,"kind":"container"}`); !isErr {
		t.Fatalf("a rule without target must be an error: %s", out)
	}
	if out, isErr := callTool(t, srv, "fleet_settings", `{"settings":{"agent_llm_api_key":"x"}}`); !isErr {
		t.Fatalf("a secret setting must be refused: %s", out)
	}
	if out, isErr := callTool(t, srv, "fleet_settings", `{"settings":{"admin_password_hash":"x"}}`); !isErr {
		t.Fatalf("an auth setting must be refused: %s", out)
	}
	if out, isErr := callTool(t, srv, "fleet_s3_accounts", `{"action":"create","account_id":1}`); !isErr {
		t.Fatalf("creating an S3 account needs a secret key: %s", out)
	}
	if out, isErr := callTool(t, srv, "ssh_keys", `{"action":"create","key_id":1}`); !isErr {
		t.Fatalf("a key must not be created from the model: %s", out)
	}
	// Nothing ran, so the host is still there.
	h, err := db.GetHost(hostID)
	if err != nil || h == nil {
		t.Fatalf("the host must be untouched: %v", err)
	}
}

// Every change asks the operator first; a denial runs nothing.
func TestAgentFleetChangesAskAndDeny(t *testing.T) {
	srv, db, hostID := fleetServer(t)
	_ = db.SetSetting("agent_approval", "auto") // auto skips gate 1 only

	// Denied: the note is not written.
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "fleet_notes", `{"notes":"a new global note"}`); !strings.Contains(out, "denied") {
		t.Fatalf("a denied change must not run: %s", out)
	}
	notes, _ := db.GetGlobalNotes()
	if notes != "" {
		t.Fatalf("the global note must be untouched after a denial: %q", notes)
	}

	// Approved: the note is written and the card shows the action.
	card := make(chan string, 1)
	go func() { card <- decideNext(t, srv, true) }()
	out, isErr := callTool(t, srv, "fleet_notes", `{"notes":"a new global note"}`)
	if isErr || !strings.Contains(out, "saved") {
		t.Fatalf("the approved change must run: %s", out)
	}
	if c := <-card; !strings.Contains(c, "a new global note") {
		t.Fatalf("the approval card must show the action: %s", c)
	}
	notes, _ = db.GetGlobalNotes()
	if notes != "a new global note" {
		t.Fatalf("the global note must be saved: %q", notes)
	}

	// Denied: the host is not deleted.
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "fleet_host_action", `{"host_id":`+fleetItoa(hostID)+`,"action":"delete"}`); !strings.Contains(out, "denied") {
		t.Fatalf("a denied delete must not run: %s", out)
	}
	if h, _ := db.GetHost(hostID); h == nil {
		t.Fatal("the host must survive a denied delete")
	}

	// Denied: the rule is not deleted.
	ruleID := seedRule(t, db, hostID, "service", "redis", "active")
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "fleet_rule_action", `{"action":"delete","rule_id":`+fleetItoa(ruleID)+`}`); !strings.Contains(out, "denied") {
		t.Fatalf("a denied rule delete must not run: %s", out)
	}
	rules, _ := db.ListDesiredRules(hostID)
	if len(rules) != 1 {
		t.Fatalf("the rule must survive a denied delete: %d", len(rules))
	}
}

// Approved changes land in the store, through the UI's own handlers.
func TestAgentFleetChangesApproved(t *testing.T) {
	srv, db, hostID := fleetServer(t)

	// Host notes.
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_host_action", `{"host_id":`+fleetItoa(hostID)+`,"action":"notes","notes":"primary web server"}`); isErr {
		t.Fatalf("host notes: %s", out)
	}
	h, _ := db.GetHost(hostID)
	if h.Notes != "primary web server" {
		t.Fatalf("the host note must be saved: %q", h.Notes)
	}

	// Host group.
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_host_action", `{"host_id":`+fleetItoa(hostID)+`,"action":"group","group":"staging"}`); isErr {
		t.Fatalf("host group: %s", out)
	}
	h, _ = db.GetHost(hostID)
	if h.GroupName != "staging" {
		t.Fatalf("the host group must be saved: %q", h.GroupName)
	}

	// Group rename touches every host of the group.
	if _, err := db.CreateHost(&store.Host{Name: "web-2", Host: "10.0.0.2", Port: 22, User: "root", GroupName: "staging"}); err != nil {
		t.Fatal(err)
	}

	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_host_groups", `{"action":"rename","from":"staging","to":"edge"}`); isErr {
		t.Fatalf("group rename: %s", out)
	}
	for _, name := range []string{"web-1", "web-2"} {
		hosts, _ := db.ListHosts()
		for _, hh := range hosts {
			if hh.Name == name && hh.GroupName != "edge" {
				t.Fatalf("%s must be in the renamed group: %q", name, hh.GroupName)
			}
		}
	}

	// A Desired State rule is created and then disabled.
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_rule_action", `{"action":"save","host_id":`+fleetItoa(hostID)+`,"kind":"container","target":"nginx","expected":"running"}`); isErr {
		t.Fatalf("rule save: %s", out)
	}
	rules, _ := db.ListDesiredRules(hostID)
	if len(rules) != 1 || rules[0].Target != "nginx" || !rules[0].Enabled {
		t.Fatalf("the rule must be saved and enabled: %+v", rules)
	}
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_rule_action", `{"action":"disable","rule_id":`+fleetItoa(rules[0].ID)+`}`); isErr {
		t.Fatalf("rule disable: %s", out)
	}
	rules, _ = db.ListDesiredRules(hostID)
	if len(rules) != 1 || rules[0].Enabled {
		t.Fatalf("the rule must be disabled: %+v", rules)
	}

	// A terminal preset is created.
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_presets", `{"action":"create","name":"logs","command":"journalctl -f"}`); isErr {
		t.Fatalf("preset create: %s", out)
	}
	presets, _ := db.ListTerminalPresets(nil)
	found := false
	for _, p := range presets {
		if p.Name == "logs" && p.Command == "journalctl -f" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the preset must be created: %+v", presets)
	}

	// The shortcut list is replaced.
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_shortcuts", `{"shortcuts":[{"name":"Wiki","url":"https://wiki.example.com","group":"docs"}]}`); isErr {
		t.Fatalf("shortcuts: %s", out)
	}
	if raw, _ := db.GetSetting("shortcuts"); !strings.Contains(raw, "wiki.example.com") {
		t.Fatalf("the shortcuts must be saved: %q", raw)
	}

	// A non-security setting is changed.
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_settings", `{"settings":{"poll_interval_sec":"600"}}`); isErr {
		t.Fatalf("settings: %s", out)
	}
	if v, _ := db.GetSetting("poll_interval_sec"); v != "600" {
		t.Fatalf("the setting must be saved: %q", v)
	}

	// An SSH key of the library is deleted.
	if _, err := db.CreateSSHKey(&store.SSHKey{Name: "old", PrivateKey: "p", PublicKey: "pub", Fingerprint: "SHA256:x"}); err != nil {
		t.Fatal(err)
	}
	keys, _ := db.ListSSHKeys()
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "ssh_keys", `{"action":"delete","key_id":`+fleetItoa(keys[0].ID)+`}`); isErr {
		t.Fatalf("ssh key delete: %s", out)
	}
	keys, _ = db.ListSSHKeys()
	if len(keys) != 0 {
		t.Fatalf("the key must be deleted: %+v", keys)
	}
}

// An update must not drop the host's own SSH key: the API never returns it, so the tool keeps it.
func TestAgentFleetHostUpdateKeepsCustomKey(t *testing.T) {
	srv, db, hostID := fleetServer(t)
	h, _ := db.GetHost(hostID)
	h.CustomKey = "PRIVATE-KEY-MATERIAL"
	if err := db.UpdateHost(h); err != nil {
		t.Fatal(err)
	}
	go func() { decideNext(t, srv, true) }()
	if out, isErr := callTool(t, srv, "fleet_host_action", `{"host_id":`+fleetItoa(hostID)+`,"action":"update","new_name":"web-1-renamed","notes":"moved"}`); isErr {
		t.Fatalf("host update: %s", out)
	}
	got, _ := db.GetHost(hostID)
	if got.Name != "web-1-renamed" || got.Notes != "moved" {
		t.Fatalf("the host must be updated: %+v", got)
	}
	if got.CustomKey != "PRIVATE-KEY-MATERIAL" {
		t.Fatalf("the stored ssh key must survive an update: %q", got.CustomKey)
	}
}

// A new host is registered with the key of the SSH Key Library, never with a key from the model.
func TestAgentFleetHostCreate(t *testing.T) {
	srv, db, _ := fleetServer(t)
	keyID, err := db.CreateSSHKey(&store.SSHKey{Name: "deploy", PrivateKey: "p", PublicKey: "pub", Fingerprint: "SHA256:x"})
	if err != nil {
		t.Fatal(err)
	}
	go func() { decideNext(t, srv, true) }()
	out, isErr := callTool(t, srv, "fleet_host_action", `{"action":"create","new_name":"web-9","address":"10.0.0.9","port":2222,"user":"deploy","key_id":`+fleetItoa(keyID)+`}`)
	if isErr || !strings.Contains(out, "web-9") {
		t.Fatalf("host create: %s", out)
	}
	hosts, _ := db.ListHosts()
	found := false
	for _, h := range hosts {
		if h.Name == "web-9" && h.Host == "10.0.0.9" && h.Port == 2222 && h.User == "deploy" && h.KeyID == keyID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the host must be created: %+v", hosts)
	}
}

// The notification channels are listed without their secrets.
func TestAgentFleetNotifyChannels(t *testing.T) {
	srv, db, _ := fleetServer(t)
	_ = db.SetSetting("telegram_token", "tg-secret-123")
	_ = db.SetSetting("telegram_chat_id", "42")
	_ = db.SetSetting("slack_webhook_url", "https://hooks.slack.com/secret-path")

	out, isErr := callTool(t, srv, "fleet_notify_channels", `{}`)
	if isErr || !strings.Contains(out, "telegram") || !strings.Contains(out, "slack") {
		t.Fatalf("fleet_notify_channels: %s", out)
	}
	for _, secret := range []string{"tg-secret-123", "hooks.slack.com/secret-path"} {
		if strings.Contains(out, secret) {
			t.Fatalf("fleet_notify_channels leaked %s: %s", secret, out)
		}
	}
}

// A key that still belongs to a host is refused by the UI's own rule, and the deletion does not happen.
func TestAgentFleetSSHKeyInUse(t *testing.T) {
	srv, db, hostID := fleetServer(t)
	keyID, err := db.CreateSSHKey(&store.SSHKey{Name: "deploy", PrivateKey: "p", PublicKey: "pub", Fingerprint: "SHA256:x"})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := db.GetHost(hostID)
	h.KeyID = keyID
	if err := db.UpdateHost(h); err != nil {
		t.Fatal(err)
	}
	go func() { decideNext(t, srv, true) }()
	out, _ := callTool(t, srv, "ssh_keys", `{"action":"delete","key_id":`+fleetItoa(keyID)+`}`)
	if !strings.Contains(out, "host") {
		t.Fatalf("a key in use must be refused: %s", out)
	}
	keys, _ := db.ListSSHKeys()
	if len(keys) != 1 {
		t.Fatalf("the key must survive: %+v", keys)
	}
}

// The ssh-config import previews without asking and registers only after an approval.
func TestAgentFleetHostImport(t *testing.T) {
	srv, db, _ := fleetServer(t)
	cfg := "Host web3\n  HostName 10.0.0.3\n  User deploy\n  Port 2222\n"
	out, isErr := callTool(t, srv, "fleet_host_import", `{"action":"preview","content":"`+strings.ReplaceAll(cfg, "\n", "\\n")+`"}`)
	if isErr || !strings.Contains(out, "10.0.0.3") || !strings.Contains(out, "2222") {
		t.Fatalf("import preview: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatal("the preview must not ask for approval")
	}

	go func() { decideNext(t, srv, true) }()
	out, isErr = callTool(t, srv, "fleet_host_import", `{"action":"commit","hosts":[{"name":"web3","host":"10.0.0.3","port":2222,"user":"deploy"}]}`)
	if isErr || !strings.Contains(out, "web3") {
		t.Fatalf("import commit: %s", out)
	}
	hosts, _ := db.ListHosts()
	found := false
	for _, h := range hosts {
		if h.Name == "web3" && h.Host == "10.0.0.3" && h.Port == 2222 && h.User == "deploy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the host must be created: %+v", hosts)
	}
}

// Gate 3 is never skipped by the auto mode and never covered by a session grant.
func TestAgentFleetGate3NeverAutoOrGranted(t *testing.T) {
	srv, db, hostID := fleetServer(t)
	_ = db.SetSetting("agent_approval", "auto")

	// A gate-3 session grant exists on the host; gate 3 must still wait for the operator.
	srv.agent.mu.Lock()
	srv.agent.grants = map[int64]agentGrant{hostID: {HostID: hostID, Gate: 3, Expires: time.Now().Add(time.Hour)}}
	srv.agent.mu.Unlock()

	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "fleet_host_action", `{"host_id":`+fleetItoa(hostID)+`,"action":"delete"}`); !strings.Contains(out, "denied") {
		t.Fatalf("gate 3 must ask even with a grant: %s", out)
	}
	if h, _ := db.GetHost(hostID); h == nil {
		t.Fatal("the host must survive")
	}

	// The same for a host-less scope (the global note).
	go func() { decideNext(t, srv, false) }()
	if out, _ := callTool(t, srv, "fleet_notes", `{"notes":"nope"}`); !strings.Contains(out, "denied") {
		t.Fatalf("a host-less gate 3 must ask: %s", out)
	}
	notes, _ := db.GetGlobalNotes()
	if notes != "" {
		t.Fatalf("the global note must be untouched: %q", notes)
	}
}

// seedRule stores one Desired State rule and returns its id.
func seedRule(t *testing.T, db *store.DB, hostID int64, kind, target, expected string) int64 {
	t.Helper()
	if err := db.SaveDesiredRule(&store.DesiredRule{HostID: hostID, Kind: kind, Target: target, Expected: expected, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	rules, _ := db.ListDesiredRules(hostID)
	for _, r := range rules {
		if r.Target == target {
			return r.ID
		}
	}
	t.Fatal("the rule was not stored")
	return 0
}

// itoa renders an int64 for the JSON arguments of a test.
func fleetItoa(v int64) string { return strconv.FormatInt(v, 10) }

// No tool of this area accepts a credential: the schema has no room for one.
func TestAgentFleetNoCredentialArgument(t *testing.T) {
	for _, tl := range agentTools {
		if !strings.HasPrefix(tl.Name, "fleet_") && tl.Name != "ssh_keys" {
			continue
		}
		b, _ := json.Marshal(tl.InputSchema)
		for _, bad := range []string{"password", "private_key", "secret_key", "api_key", "token", "passphrase"} {
			if strings.Contains(string(b), `"`+bad+`"`) {
				t.Errorf("tool %s must not accept %q", tl.Name, bad)
			}
		}
	}
}
