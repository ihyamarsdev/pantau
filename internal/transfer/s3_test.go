package transfer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func newS3TestManager(t *testing.T, endpoint string) (*Manager, int64, int64, *sshrunner.MockRunner) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hostID, err := db.CreateHost(&store.Host{Name: "web1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateS3Account(&store.S3Account{Name: "main", Endpoint: endpoint, Bucket: "bkt", AccessKey: "ak", SecretKey: "sk", Prefix: "backups/"})
	if err != nil {
		t.Fatal(err)
	}
	mock := sshrunner.NewMockRunner()
	return NewManager(db, func(string, int, string, string) (sshrunner.Runner, error) { return mock, nil }), hostID, id, mock
}

func waitDone(t *testing.T, m *Manager, id int64) *TransferJob {
	t.Helper()
	for range 100 {
		time.Sleep(50 * time.Millisecond)
		m.mu.RLock()
		status := m.jobs[id].Status
		m.mu.RUnlock()
		if status != "queued" && status != "transferring" {
			break
		}
	}
	return m.GetJob(id)
}

func TestStartS3UploadFileAndFolder(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.Method+" "+r.URL.Path] = string(body)
		mu.Unlock()
	}))
	defer srv.Close()

	m, hostID, acctID, mock := newS3TestManager(t, srv.URL)
	mock.Handlers[`test -r '/etc/app.conf'`] = func() (string, string, int, error) { return "", "", 0, nil }
	mock.Handlers[`test -d '/etc/app.conf' && echo "DIR" || echo "FILE"`] = func() (string, string, int, error) { return "FILE\n", "", 0, nil }
	mock.Handlers[`cat '/etc/app.conf'`] = func() (string, string, int, error) { return "conf-data", "", 0, nil }

	job, err := m.StartS3Upload(hostID, "/etc/app.conf", acctID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitDone(t, m, job.ID); job.Status != "completed" || job.Kind != "s3" {
		t.Fatalf("file upload: status=%s err=%s", job.Status, job.Error)
	}
	if job.DestPath != "s3://bkt/backups/app.conf" || got["PUT /bkt/backups/app.conf"] != "conf-data" {
		t.Fatalf("dest=%q uploaded=%v", job.DestPath, got)
	}

	// Folder: packed with tar and stored as <name>.tar.gz, under a prefix chosen in the dialog.
	mock.Handlers[`test -r '/var/www'`] = func() (string, string, int, error) { return "", "", 0, nil }
	mock.Handlers[`test -d '/var/www' && echo "DIR" || echo "FILE"`] = func() (string, string, int, error) { return "DIR\n", "", 0, nil }
	mock.Handlers[`tar -czf -`] = func() (string, string, int, error) { return "TARBALL", "", 0, nil }
	prefix := "/site//old/"
	job, err = m.StartS3Upload(hostID, "/var/www", acctID, &prefix)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitDone(t, m, job.ID); job.Status != "completed" || !job.IsDirectory {
		t.Fatalf("folder upload: status=%s err=%s", job.Status, job.Error)
	}
	if got["PUT /bkt/site/old/www.tar.gz"] != "TARBALL" {
		t.Fatalf("folder archive not uploaded, got %v", got)
	}
}

func TestStartS3UploadUsesChosenAccount(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
	}))
	defer srv.Close()
	m, hostID, _, mock := newS3TestManager(t, srv.URL)
	other, err := m.db.CreateS3Account(&store.S3Account{Name: "archive", Endpoint: srv.URL, Bucket: "cold", AccessKey: "ak2", SecretKey: "sk2", Prefix: "y/"})
	if err != nil {
		t.Fatal(err)
	}
	mock.Handlers[`test -r '/etc/app.conf'`] = func() (string, string, int, error) { return "", "", 0, nil }
	mock.Handlers[`test -d '/etc/app.conf' && echo "DIR" || echo "FILE"`] = func() (string, string, int, error) { return "FILE\n", "", 0, nil }
	mock.Handlers[`cat '/etc/app.conf'`] = func() (string, string, int, error) { return "x", "", 0, nil }

	job, err := m.StartS3Upload(hostID, "/etc/app.conf", other, nil)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitDone(t, m, job.ID); job.Status != "completed" || job.DestPath != "s3://cold/y/app.conf" || job.DestHostName != "archive" {
		t.Fatalf("status=%s dest=%q name=%q err=%s", job.Status, job.DestPath, job.DestHostName, job.Error)
	}
	if len(paths) != 1 || paths[0] != "/cold/y/app.conf" {
		t.Fatalf("uploaded to %v, want the second account's bucket", paths)
	}
}

func TestStartS3UploadFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>nope</Message></Error>`))
	}))
	defer srv.Close()
	m, hostID, acctID, mock := newS3TestManager(t, srv.URL)
	mock.Handlers[`test -r '/etc/app.conf'`] = func() (string, string, int, error) { return "", "", 0, nil }

	job, err := m.StartS3Upload(hostID, "/etc/app.conf", acctID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if job = waitDone(t, m, job.ID); job.Status != "failed" || !strings.Contains(job.Error, "AccessDenied") {
		t.Fatalf("expected failed job with S3 error, got status=%s err=%q", job.Status, job.Error)
	}

	mock.Handlers[`test -r '/nope'`] = func() (string, string, int, error) { return "", "", 1, nil }
	job, _ = m.StartS3Upload(hostID, "/nope", acctID, nil)
	if job = waitDone(t, m, job.ID); job.Status != "failed" || !strings.Contains(job.Error, "not readable") {
		t.Fatalf("expected unreadable-source failure, got status=%s err=%q", job.Status, job.Error)
	}

	for _, bad := range []string{"relative/path", "/", ""} {
		if _, err := m.StartS3Upload(hostID, bad, acctID, nil); err == nil {
			t.Errorf("source %q must be rejected", bad)
		}
	}
	if _, err := m.StartS3Upload(hostID, "/etc/app.conf", 9999, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown account must be rejected, got %v", err)
	}
	bare, _ := m.db.CreateS3Account(&store.S3Account{Name: "no-secret", Bucket: "bkt", AccessKey: "ak"})
	if _, err := m.StartS3Upload(hostID, "/etc/app.conf", bare, nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("account without secret must be rejected, got %v", err)
	}
}
