package inspector

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func webScanTestDB(t *testing.T) (*store.DB, int64) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	hostID, err := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	return db, hostID
}

func TestScanWebDomainsReturnsTheParsedScan(t *testing.T) {
	db, hostID := webScanTestDB(t)
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "__PANTAU_SECTION__ nginx host\nMETHOD nginx -T\nserver { listen 80; server_name a.example.com; }\n", "", 0, nil
	}
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)
	scan, err := ins.ScanWebDomains(hostID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(scan.Sites) != 1 || scan.Sites[0].Domain != "a.example.com" {
		t.Fatalf("unexpected sites: %+v", scan.Sites)
	}
	if scan.HostID != hostID || scan.ScannedAt.IsZero() {
		t.Fatalf("unexpected scan: %+v", scan)
	}
}

func TestScanWebDomainsSSHFailure(t *testing.T) {
	db, hostID := webScanTestDB(t)
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return nil, errors.New("connection refused")
	}, nil)
	_, err := ins.ScanWebDomains(hostID)
	if !errors.Is(err, ErrWebScanSSH) {
		t.Fatalf("expected an SSH error, got %v", err)
	}
}

func TestScanWebDomainsInProgress(t *testing.T) {
	db, hostID := webScanTestDB(t)
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		once.Do(func() { close(started) })
		<-release
		return "", "", 0, nil
	}
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)

	errCh := make(chan error, 1)
	go func() {
		_, err := ins.ScanWebDomains(hostID)
		errCh <- err
	}()
	<-started
	_, err := ins.ScanWebDomains(hostID)
	if !errors.Is(err, ErrWebScanInProgress) {
		t.Fatalf("expected an in-progress error, got %v", err)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("the first scan failed: %v", err)
	}
}

func TestScanWebDomainsUnknownHost(t *testing.T) {
	db, _ := webScanTestDB(t)
	ins := New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return nil, errors.New("must not be called")
	}, nil)
	if _, err := ins.ScanWebDomains(9999); err == nil {
		t.Fatal("expected an error for an unknown host")
	}
}
