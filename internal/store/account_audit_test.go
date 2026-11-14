package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountAuditStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-accountaudit-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "srv1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	// No audit yet.
	got, err := db.GetAccountAudit(hostID)
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no audit, got %+v", got)
	}

	audit := &AccountAudit{
		HostID:    hostID,
		ScannedAt: time.Now().Truncate(time.Second),
		Status:    "partial",
		Summary:   AccountAuditSummary{Users: 2, Keys: 3, UnknownKeys: 1, Critical: 1},
		Users: []AccountUser{
			{Name: "root", UID: 0, Shell: "/bin/bash", LoginCapable: true, Groups: []string{"sudo"}, PasswordState: "locked"},
			{Name: "alice", UID: 1000, Shell: "/bin/bash", LoginCapable: true, Groups: []string{}, PasswordState: "set", KeyCount: 1},
		},
		Keys: []AccountKey{
			{Fingerprint: "SHA256:abc", Type: "ssh-ed25519", Valid: true, User: "alice", File: "/home/alice/.ssh/authorized_keys", Options: []string{"no-pty"}},
			{Fingerprint: "SHA256:def", Type: "ssh-rsa", Bits: 1024, Valid: true, Weak: true, Unknown: true, User: "alice"},
		},
		Findings: []AccountFinding{{ID: "uid0_account", Severity: "critical", Title: "Extra UID 0 account", User: "backdoor"}},
		Notes:    []string{"/etc/shadow is not readable"},
	}
	if err := db.SaveAccountAudit(audit); err != nil {
		t.Fatalf("save audit: %v", err)
	}

	got, err = db.GetAccountAudit(hostID)
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if got == nil {
		t.Fatal("expected an audit")
	}
	if got.Status != "partial" {
		t.Errorf("status = %q, want partial", got.Status)
	}
	if got.Summary.Users != 2 || got.Summary.Keys != 3 {
		t.Errorf("summary = %+v", got.Summary)
	}
	if len(got.Users) != 2 || got.Users[0].Name != "root" {
		t.Errorf("users = %+v", got.Users)
	}
	if got.Users[0].Groups == nil || len(got.Users[0].Groups) != 1 {
		t.Errorf("root groups = %v", got.Users[0].Groups)
	}
	if len(got.Keys) != 2 || got.Keys[1].Weak != true || got.Keys[1].Unknown != true {
		t.Errorf("keys = %+v", got.Keys)
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "uid0_account" {
		t.Errorf("findings = %+v", got.Findings)
	}
	if len(got.Notes) != 1 {
		t.Errorf("notes = %+v", got.Notes)
	}

	// Upsert replaces the previous result.
	audit.Status = "ok"
	audit.Notes = nil
	if err := db.SaveAccountAudit(audit); err != nil {
		t.Fatalf("save audit again: %v", err)
	}
	got, err = db.GetAccountAudit(hostID)
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.Notes == nil {
		t.Error("notes must never be nil in JSON")
	}

	// List.
	list, err := db.ListAccountAudits()
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(list) != 1 || list[0].HostID != 1 {
		t.Errorf("list = %+v", list)
	}
}
