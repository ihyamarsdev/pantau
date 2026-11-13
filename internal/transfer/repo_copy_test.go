package transfer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// captureRunner records what is piped into the host's `tar -x`.
type captureRunner struct {
	*sshrunner.MockRunner
	mu     sync.Mutex
	piped  bytes.Buffer
	cmd    string
	tarErr error  // returned by the host's tar after it read the input
	tarOut string // what tar printed
}

func (c *captureRunner) PipeCommand(cmd string, in io.Reader, out io.Writer) error {
	c.mu.Lock()
	c.cmd = cmd
	c.mu.Unlock()
	if c.tarErr != nil {
		_, _ = io.WriteString(out, c.tarOut)
		return c.tarErr
	}
	b, err := io.ReadAll(in)
	c.mu.Lock()
	c.piped.Write(b)
	c.mu.Unlock()
	return err
}

func newRepoCopyManager(t *testing.T) (*Manager, int64, *captureRunner) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hostID, err := db.CreateHost(&store.Host{Name: "web1", Host: "10.0.0.1", Port: 22, User: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	cr := &captureRunner{MockRunner: sshrunner.NewMockRunner()}
	return NewManager(db, func(string, int, string, string) (sshrunner.Runner, error) { return cr, nil }), hostID, cr
}

func TestStartRepoCopyStreamsTheArchiveIntoTar(t *testing.T) {
	m, hostID, cr := newRepoCopyManager(t)
	job, err := m.StartRepoCopy(RepoCopy{
		Label: "Torcons/api", Total: 100, Note: "1 symlink skipped", DstHostID: hostID, DstPath: "/srv/app/",
		Write: func(ctx context.Context, w io.Writer) error { _, err := io.WriteString(w, "TARBYTES"); return err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if job = waitDone(t, m, job.ID); job.Status != "completed" || job.Kind != "repo" || job.DestPath != "/srv/app" || job.Note != "1 symlink skipped" {
		t.Fatalf("job = %+v", job)
	}
	if cr.piped.String() != "TARBYTES" || !strings.Contains(cr.cmd, `tar -xf - -C '/srv/app'`) {
		t.Errorf("host received %q via %q", cr.piped.String(), cr.cmd)
	}
	if !strings.Contains(strings.Join(cr.ExecHistory, "\n"), "mkdir -p '/srv/app'") {
		t.Errorf("destination was not created first: %v", cr.ExecHistory)
	}
}

func TestStartRepoCopyReportsTheHostsReason(t *testing.T) {
	m, hostID, cr := newRepoCopyManager(t)
	wrote := false
	spec := RepoCopy{Label: "r", DstHostID: hostID, DstPath: "/srv/app", Write: func(ctx context.Context, w io.Writer) error {
		wrote = true
		_, err := io.WriteString(w, "x")
		return err
	}}

	cr.Handlers["mkdir -p"] = func() (string, string, int, error) {
		return "", "mkdir: cannot create directory '/srv/app': Permission denied", 1, nil
	}
	job, _ := m.StartRepoCopy(spec)
	if job = waitDone(t, m, job.ID); job.Status != "failed" || !strings.Contains(job.Error, "Permission denied") || wrote {
		t.Errorf("preflight: status=%s err=%q wrote=%v", job.Status, job.Error, wrote)
	}

	delete(cr.Handlers, "mkdir -p")
	cr.tarErr, cr.tarOut = errors.New("Process exited with status 2"), "tar: app/x: Cannot open: Disk quota exceeded"
	spec.Write = func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, strings.Repeat("x", 1<<20))
		return err
	}
	job, _ = m.StartRepoCopy(spec)
	if job = waitDone(t, m, job.ID); job.Status != "failed" || !strings.Contains(job.Error, "Disk quota exceeded") {
		t.Errorf("tar failure: status=%s err=%q (the host's message must win over the broken pipe)", job.Status, job.Error)
	}
}

func TestStartRepoCopyRejectsBadInput(t *testing.T) {
	m, hostID, _ := newRepoCopyManager(t)
	w := func(context.Context, io.Writer) error { return nil }
	for _, p := range []string{"relative/dir", "", "/srv/a\nb", "/srv/\x00"} {
		if _, err := m.StartRepoCopy(RepoCopy{DstHostID: hostID, DstPath: p, Write: w}); err == nil {
			t.Errorf("destination %q must be rejected", p)
		}
	}
	if _, err := m.StartRepoCopy(RepoCopy{DstHostID: 999, DstPath: "/srv/app", Write: w}); err == nil {
		t.Error("unknown host must be rejected")
	}
}
