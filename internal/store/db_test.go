package store

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDualBucketAutoPruningAndFilteredRuns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{
		Name: "Server Alpha",
		Host: "192.168.1.10",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Insert 105 OK runs
	for i := 1; i <= 105; i++ {
		err := db.RecordInspectionRun(&InspectionRun{
			HostID:     hostID,
			StartedAt:  time.Now().Add(time.Duration(i) * time.Minute),
			DurationMs: 150,
			Status:     "ok",
			Summary:    fmt.Sprintf("OK run #%d", i),
		})
		if err != nil {
			t.Fatalf("failed recording ok run: %v", err)
		}
	}

	// Verify only 100 OK runs are kept
	allRuns, err := db.ListInspectionRuns(hostID, 200, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(allRuns) != 100 {
		t.Fatalf("expected 100 ok runs after pruning, got %d", len(allRuns))
	}

	// 2. Insert 105 non-OK runs (drift / error)
	for i := 1; i <= 105; i++ {
		err := db.RecordInspectionRun(&InspectionRun{
			HostID:     hostID,
			StartedAt:  time.Now().Add(time.Duration(105+i) * time.Minute),
			DurationMs: 320,
			Status:     "drift",
			Summary:    fmt.Sprintf("Drift run #%d", i),
			Details:    "Service nginx inactive",
		})
		if err != nil {
			t.Fatalf("failed recording drift run: %v", err)
		}
	}

	// Verify dual bucket: 100 ok runs + 100 issue runs = 200 total in db
	issueRuns, err := db.ListInspectionRuns(hostID, 200, "issues")
	if err != nil {
		t.Fatal(err)
	}
	if len(issueRuns) != 100 {
		t.Fatalf("expected exactly 100 issue runs, got %d", len(issueRuns))
	}
	for _, r := range issueRuns {
		if r.Status == "ok" {
			t.Fatalf("expected non-ok status in issueRuns, got %s", r.Status)
		}
	}

	totalRuns, err := db.ListInspectionRuns(hostID, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(totalRuns) != 200 {
		t.Fatalf("expected 200 total runs (100 ok + 100 drift), got %d", len(totalRuns))
	}
}

func TestAlertAcknowledgmentOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-alert-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h1, _ := db.CreateHost(&Host{Name: "Host 1", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&Host{Name: "Host 2", Host: "10.0.0.2", Port: 22, User: "root"})

	// Create alerts for host 1 and host 2
	a1, _ := db.CreateAlert(&Alert{HostID: h1, Level: "warning", Message: "Disk high"})
	_, _ = db.CreateAlert(&Alert{HostID: h1, Level: "critical", Message: "Service down"})
	_, _ = db.CreateAlert(&Alert{HostID: h2, Level: "warning", Message: "CPU high"})

	active, err := db.ListActiveAlerts()
	if err != nil || len(active) != 3 {
		t.Fatalf("expected 3 active alerts, got %d (err: %v)", len(active), err)
	}

	// 1. Acknowledge single alert
	if err := db.AcknowledgeAlert(a1); err != nil {
		t.Fatal(err)
	}
	active, _ = db.ListActiveAlerts()
	if len(active) != 2 {
		t.Fatalf("expected 2 active alerts after single ack, got %d", len(active))
	}

	// 2. Acknowledge all alerts for host 1
	if err := db.AcknowledgeHostAlerts(h1); err != nil {
		t.Fatal(err)
	}
	active, _ = db.ListActiveAlerts()
	if len(active) != 1 || active[0].HostID != h2 {
		t.Fatalf("expected 1 active alert for host 2, got %d", len(active))
	}

	// 3. Acknowledge all alerts
	if err := db.AcknowledgeAllAlerts(); err != nil {
		t.Fatal(err)
	}
	active, _ = db.ListActiveAlerts()
	if len(active) != 0 {
		t.Fatalf("expected 0 active alerts after ack all, got %d", len(active))
	}
}

func TestHostDiskIOStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-disk-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{
		Name: "Storage Server",
		Host: "192.168.1.50",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	host, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}

	host.DiskReadBytes = 104857600
	host.DiskWriteBytes = 209715200
	host.DiskReadSpeedBps = 1048576
	host.DiskWriteSpeedBps = 2097152

	if err := db.UpdateHostInspection(host); err != nil {
		t.Fatalf("failed to update host inspection: %v", err)
	}

	retrieved, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}

	if retrieved.DiskReadBytes != 104857600 || retrieved.DiskWriteBytes != 209715200 {
		t.Fatalf("unexpected disk bytes: read=%d, write=%d", retrieved.DiskReadBytes, retrieved.DiskWriteBytes)
	}
	if retrieved.DiskReadSpeedBps != 1048576 || retrieved.DiskWriteSpeedBps != 2097152 {
		t.Fatalf("unexpected disk speeds: read=%d, write=%d", retrieved.DiskReadSpeedBps, retrieved.DiskWriteSpeedBps)
	}
}

func TestHostPackagesAndProcessTelemetry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-store-pkgs-*")
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

	pkgs := []*HostPackage{
		{
			HostID:        hostID,
			Name:          "nginx",
			Version:       "1.18.0",
			SizeBytes:     1024000,
			InstallEpoch:  1600000000,
			IsManual:      true,
			ServiceName:   "nginx.service",
			ServiceStatus: "active",
		},
		{
			HostID:        hostID,
			Name:          "curl",
			Version:       "7.68.0",
			SizeBytes:     204800,
			InstallEpoch:  1590000000,
			IsManual:      false,
			ServiceName:   "",
			ServiceStatus: "",
		},
	}

	if err := db.ReplaceHostPackages(hostID, pkgs); err != nil {
		t.Fatalf("replace host packages: %v", err)
	}

	got, err := db.GetHostPackages(hostID)
	if err != nil {
		t.Fatalf("get host packages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(got))
	}
	if got[0].Name != "nginx" || !got[0].IsManual || got[0].ServiceStatus != "active" {
		t.Errorf("unexpected package 0: %+v", got[0])
	}

	// Test GetHostPackagesLastScanned
	lastScanned, err := db.GetHostPackagesLastScanned(hostID)
	if err != nil {
		t.Fatalf("get host packages last scanned: %v", err)
	}
	if lastScanned.IsZero() {
		t.Errorf("expected non-zero last scanned time, got %v", lastScanned)
	}

	// Test ReplaceHostPackages refuses empty slice and preserves existing packages
	err = db.ReplaceHostPackages(hostID, []*HostPackage{})
	if err == nil {
		t.Errorf("expected error when replacing with empty slice, got nil")
	}
	preserved, err := db.GetHostPackages(hostID)
	if err != nil || len(preserved) != 2 {
		t.Fatalf("expected 2 packages preserved after empty replace attempt, got %d (err: %v)", len(preserved), err)
	}
}

func TestFilePinsCRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-pins-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{
		Name: "Web Server",
		Host: "10.0.0.1",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial pins should be empty
	pins, err := db.GetFilePins(hostID)
	if err != nil {
		t.Fatalf("get file pins: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("expected 0 pins, got %d", len(pins))
	}

	// 2. Save pins
	if err := db.SaveFilePin(hostID, "/var/log/nginx", true, "Nginx log folder"); err != nil {
		t.Fatalf("save folder pin: %v", err)
	}
	if err := db.SaveFilePin(hostID, "/etc/nginx/nginx.conf", false, "Main nginx conf"); err != nil {
		t.Fatalf("save file pin: %v", err)
	}

	pins, err = db.GetFilePins(hostID)
	if err != nil {
		t.Fatalf("get file pins after save: %v", err)
	}
	if len(pins) != 2 {
		t.Fatalf("expected 2 pins, got %d", len(pins))
	}

	// 3. GetAllFilePins
	globalPins, err := db.GetAllFilePins()
	if err != nil {
		t.Fatalf("get all file pins: %v", err)
	}
	if len(globalPins) != 2 {
		t.Fatalf("expected 2 global pins, got %d", len(globalPins))
	}
	if globalPins[0].HostName != "Web Server" {
		t.Errorf("expected host name 'Web Server', got %q", globalPins[0].HostName)
	}

	// 4. Update note (upsert)
	if err := db.SaveFilePin(hostID, "/var/log/nginx", true, "Updated log note"); err != nil {
		t.Fatalf("update pin note: %v", err)
	}
	pins, err = db.GetFilePins(hostID)
	if err != nil {
		t.Fatalf("get file pins after update: %v", err)
	}
	if len(pins) != 2 {
		t.Fatalf("expected still 2 pins, got %d", len(pins))
	}
	var foundFolder bool
	for _, p := range pins {
		if p.Path == "/var/log/nginx" {
			foundFolder = true
			if p.Note != "Updated log note" {
				t.Errorf("expected updated note, got %q", p.Note)
			}
		}
	}
	if !foundFolder {
		t.Errorf("folder pin not found")
	}

	// 5. Delete pin
	if err := db.DeleteFilePin(hostID, "/var/log/nginx"); err != nil {
		t.Fatalf("delete file pin: %v", err)
	}
	pins, err = db.GetFilePins(hostID)
	if err != nil {
		t.Fatalf("get file pins after delete: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("expected 1 pin remaining, got %d", len(pins))
	}
	if pins[0].Path != "/etc/nginx/nginx.conf" {
		t.Errorf("expected remaining pin to be nginx.conf, got %q", pins[0].Path)
	}
}
func TestSSHKeyLibraryCRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-keylib-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	privPEM, pubKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	pub, fp, err := ParseSSHPrivateKey([]byte(privPEM))
	if err != nil {
		t.Fatalf("parse ssh private key: %v", err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("expected fingerprint starting with SHA256:, got %q", fp)
	}
	_ = pubKey

	// 1. Create key
	keyID, err := db.CreateSSHKey(&SSHKey{
		Name:        "Prod Cluster Key",
		PrivateKey:  privPEM,
		PublicKey:   pub,
		Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	if keyID <= 0 {
		t.Fatalf("expected positive key ID, got %d", keyID)
	}

	// 2. Duplicate name should fail
	_, err = db.CreateSSHKey(&SSHKey{
		Name:        "Prod Cluster Key",
		PrivateKey:  privPEM,
		PublicKey:   pub,
		Fingerprint: fp,
	})
	if err == nil {
		t.Fatal("expected duplicate key name to fail, got nil")
	}

	// 3. List keys - UsedBy must be 0 initially
	keys, err := db.ListSSHKeys()
	if err != nil {
		t.Fatalf("list ssh keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].UsedBy != 0 {
		t.Fatalf("expected used_by 0, got %d", keys[0].UsedBy)
	}
	if keys[0].Name != "Prod Cluster Key" {
		t.Errorf("expected name 'Prod Cluster Key', got %q", keys[0].Name)
	}

	// 4. Get key - verifies PrivateKey is populated in memory
	gotKey, err := db.GetSSHKey(keyID)
	if err != nil {
		t.Fatalf("get ssh key: %v", err)
	}
	if gotKey == nil {
		t.Fatal("expected key, got nil")
	}
	if gotKey.PrivateKey != privPEM {
		t.Errorf("expected private key to match in memory")
	}

	// 5. Attach host to this key
	hostID, err := db.CreateHost(&Host{
		Name:  "Worker-1",
		Host:  "10.0.0.1",
		Port:  22,
		User:  "root",
		KeyID: keyID,
	})
	if err != nil {
		t.Fatalf("create host with key_id: %v", err)
	}

	// Verify host has KeyID and custom_key is empty
	h, err := db.GetHost(hostID)
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if h.KeyID != keyID {
		t.Errorf("expected host key_id %d, got %d", keyID, h.KeyID)
	}
	if h.CustomKey != "" {
		t.Errorf("expected empty custom_key when key_id is set, got %q", h.CustomKey)
	}

	// 6. List keys - UsedBy must now be 1
	keys, err = db.ListSSHKeys()
	if err != nil {
		t.Fatalf("list ssh keys: %v", err)
	}
	if keys[0].UsedBy != 1 {
		t.Fatalf("expected used_by 1, got %d", keys[0].UsedBy)
	}

	// 7. Delete key while in use must fail with ErrSSHKeyInUse
	err = db.DeleteSSHKey(keyID)
	if !errors.Is(err, ErrSSHKeyInUse) {
		t.Fatalf("expected ErrSSHKeyInUse, got %v", err)
	}

	// 8. Update host to detach key
	h.KeyID = 0
	h.CustomKey = "custom-key-body"
	if err := db.UpdateHost(h); err != nil {
		t.Fatalf("update host: %v", err)
	}

	// 9. Delete key now succeeds
	if err := db.DeleteSSHKey(keyID); err != nil {
		t.Fatalf("delete ssh key: %v", err)
	}

	// 10. Delete non-existent key returns ErrSSHKeyNotFound
	err = db.DeleteSSHKey(keyID)
	if !errors.Is(err, ErrSSHKeyNotFound) {
		t.Fatalf("expected ErrSSHKeyNotFound, got %v", err)
	}
}

func TestResolveHostPrivateKeyPrecedence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-resolve-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Setup global key
	if err := db.SetSetting("ssh_private_key", "GLOBAL_PRIVATE_KEY"); err != nil {
		t.Fatalf("set global key: %v", err)
	}

	// Setup library key
	libKeyID, err := db.CreateSSHKey(&SSHKey{
		Name:        "Library Key 1",
		PrivateKey:  "LIBRARY_PRIVATE_KEY",
		PublicKey:   "ssh-rsa AAAA...",
		Fingerprint: "SHA256:fakefingerprint",
	})
	if err != nil {
		t.Fatalf("create lib key: %v", err)
	}

	// Case 1: Host has KeyID > 0 and CustomKey set. Library key MUST take precedence!
	h1 := &Host{
		KeyID:     libKeyID,
		CustomKey: "CUSTOM_PRIVATE_KEY",
	}
	resolved, err := db.ResolveHostPrivateKey(h1)
	if err != nil {
		t.Fatalf("resolve host 1: %v", err)
	}
	if resolved != "LIBRARY_PRIVATE_KEY" {
		t.Errorf("expected LIBRARY_PRIVATE_KEY, got %q", resolved)
	}

	// Case 2: Host has dangling/missing KeyID > 0. MUST error and NEVER silently fall back!
	h2 := &Host{
		KeyID:     99999,
		CustomKey: "CUSTOM_PRIVATE_KEY",
	}
	_, err = db.ResolveHostPrivateKey(h2)
	if err == nil {
		t.Fatal("expected error for dangling KeyID, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}

	// Case 3: Host has KeyID == 0 and CustomKey set. MUST return CustomKey.
	h3 := &Host{
		KeyID:     0,
		CustomKey: "CUSTOM_PRIVATE_KEY",
	}
	resolved, err = db.ResolveHostPrivateKey(h3)
	if err != nil {
		t.Fatalf("resolve host 3: %v", err)
	}
	if resolved != "CUSTOM_PRIVATE_KEY" {
		t.Errorf("expected CUSTOM_PRIVATE_KEY, got %q", resolved)
	}

	// Case 4: Host has KeyID == 0 and CustomKey empty. MUST return global key.
	h4 := &Host{
		KeyID:     0,
		CustomKey: "   ",
	}
	resolved, err = db.ResolveHostPrivateKey(h4)
	if err != nil {
		t.Fatalf("resolve host 4: %v", err)
	}
	if resolved != "GLOBAL_PRIVATE_KEY" {
		t.Errorf("expected GLOBAL_PRIVATE_KEY, got %q", resolved)
	}

	// Case 5: Nil host
	_, err = db.ResolveHostPrivateKey(nil)
	if err == nil {
		t.Fatal("expected error for nil host, got nil")
	}
}

func TestParseSSHPrivateKey(t *testing.T) {
	// 1. Valid RSA Key
	privPEM, _, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("generate rsa: %v", err)
	}
	pub, fp, err := ParseSSHPrivateKey([]byte(privPEM))
	if err != nil {
		t.Fatalf("parse valid rsa: %v", err)
	}
	if !strings.HasPrefix(pub, "ssh-rsa ") {
		t.Errorf("expected ssh-rsa prefix, got %q", pub)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("expected SHA256: fingerprint, got %q", fp)
	}

	// 2. Passphrase-protected key rejected
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(privKey)
	encBlock, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", privDER, []byte("secretpass"), x509.PEMCipherAES256)
	if err != nil {
		t.Fatal(err)
	}
	encPEM := pem.EncodeToMemory(encBlock)
	_, _, err = ParseSSHPrivateKey(encPEM)
	if err == nil {
		t.Fatal("expected passphrase-protected key to be rejected, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "passphrase") && !strings.Contains(strings.ToLower(err.Error()), "not supported") {
		t.Errorf("expected passphrase-related error, got: %v", err)
	}

	// 3. Invalid key rejected
	_, _, err = ParseSSHPrivateKey([]byte("NOT A VALID KEY CONTENT"))
	if err == nil {
		t.Fatal("expected invalid key to be rejected, got nil")
	}
}

func TestSnapshotRoundTripWithSSHKeys(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-snap-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db1, err := Open(filepath.Join(tmpDir, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()

	// 1. Add SSH key to DB 1
	privPEM, pubKey, _ := GenerateRSAKeyPair(2048)
	pub, fp, _ := ParseSSHPrivateKey([]byte(privPEM))
	_ = pubKey

	keyID, err := db1.CreateSSHKey(&SSHKey{
		Name:        "Backup Key",
		PrivateKey:  privPEM,
		PublicKey:   pub,
		Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("create ssh key in db1: %v", err)
	}

	// 2. Add host referencing this key
	hostID, err := db1.CreateHost(&Host{
		Name:  "Database Server",
		Host:  "192.168.1.50",
		Port:  22,
		User:  "admin",
		KeyID: keyID,
	})
	if err != nil {
		t.Fatalf("create host in db1: %v", err)
	}

	// 3. Export snapshot
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	if len(payload.SSHKeys) != 1 {
		t.Fatalf("expected 1 ssh key in snapshot, got %d", len(payload.SSHKeys))
	}
	if payload.SSHKeys[0].ID != keyID {
		t.Errorf("expected key ID %d in snapshot, got %d", keyID, payload.SSHKeys[0].ID)
	}
	if payload.SSHKeys[0].PrivateKey != privPEM {
		t.Errorf("expected snapshot to include private key for restoration")
	}

	// 4. Import snapshot into DB 2
	db2, err := Open(filepath.Join(tmpDir, "dest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatalf("import snapshot into db2: %v", err)
	}

	// Verify key restored in DB 2
	restoredKey, err := db2.GetSSHKey(keyID)
	if err != nil {
		t.Fatalf("get restored key: %v", err)
	}
	if restoredKey == nil {
		t.Fatal("restored key not found in db2")
	}
	if restoredKey.Name != "Backup Key" {
		t.Errorf("expected 'Backup Key', got %q", restoredKey.Name)
	}
	if restoredKey.PrivateKey != privPEM {
		t.Errorf("restored private key does not match")
	}

	// Verify host restored and links to key
	restoredHost, err := db2.GetHost(hostID)
	if err != nil {
		t.Fatalf("get restored host: %v", err)
	}
	if restoredHost == nil {
		t.Fatal("restored host not found in db2")
	}
	if restoredHost.KeyID != keyID {
		t.Errorf("expected restored host key_id %d, got %d", keyID, restoredHost.KeyID)
	}

	// Verify resolution on restored host
	resolved, err := db2.ResolveHostPrivateKey(restoredHost)
	if err != nil {
		t.Fatalf("resolve restored host key: %v", err)
	}
	if resolved != privPEM {
		t.Errorf("resolved key did not match restored private key")
	}

	// 5. Backward compatibility: import older snapshot without SSHKeys
	oldPayload := &SnapshotPayload{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Settings:  map[string]string{"foo": "bar"},
		Hosts: []Host{
			{
				ID:   99,
				Name: "Legacy Host",
				Host: "10.0.0.99",
				Port: 22,
				User: "root",
			},
		},
	}
	if err := db2.ImportSnapshot(oldPayload); err != nil {
		t.Fatalf("import backward compatible snapshot failed: %v", err)
	}
	legacyHost, err := db2.GetHost(99)
	if err != nil || legacyHost == nil {
		t.Fatalf("expected legacy host 99 in db2: %v", err)
	}
	if legacyHost.KeyID != 0 {
		t.Errorf("expected KeyID 0 for legacy host, got %d", legacyHost.KeyID)
	}
}

