package transfer

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pantau/internal/store"
)

// RepoCopy pushes a tar archive produced on the Pantau server (a cloned repository's files) into a directory of a host,
// as a Transfer Job of kind "repo". The archive is streamed over SSH into `tar -x`: nothing is stored on either side.
type RepoCopy struct {
	Label     string // shown as the source, e.g. "Torcons/api"
	Total     int64  // expected archive size in bytes, for progress (0 = unknown)
	Note      string
	DstHostID int64
	DstPath   string // absolute directory on the host; created if missing, existing files with the same name are overwritten
	Write     func(ctx context.Context, w io.Writer) error
}

// ValidateRepoCopyPath accepts an absolute path without control characters; trailing slashes are dropped.
func ValidateRepoCopyPath(p string) (string, error) {
	if !path.IsAbs(p) || len(p) > 1024 {
		return "", fmt.Errorf("destination must be an absolute path")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("destination must not contain control characters")
		}
	}
	return path.Clean(p), nil
}

// capBuf keeps the first bytes written to it (tar's error output on the host).
type capBuf struct {
	mu sync.Mutex
	b  []byte
}

func (c *capBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := 2000 - len(c.b); room > 0 {
		c.b = append(c.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (c *capBuf) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(string(c.b))
}

// StartRepoCopy queues the copy and returns its job; the transfer runs in the background.
func (m *Manager) StartRepoCopy(c RepoCopy) (*TransferJob, error) {
	dst, err := ValidateRepoCopyPath(c.DstPath)
	if err != nil {
		return nil, err
	}
	dstHost, err := m.db.GetHost(c.DstHostID)
	if err != nil || dstHost == nil {
		return nil, fmt.Errorf("destination host %d not found", c.DstHostID)
	}
	job := &TransferJob{
		Kind:           "repo",
		SourceHostName: "Repositories",
		SourcePath:     c.Label,
		DestHostID:     dstHost.ID,
		DestHostName:   dstHost.Name,
		DestPath:       dst,
		Mode:           "fast",
		Status:         "queued",
		IsDirectory:    true,
		TotalBytes:     c.Total,
		Note:           c.Note,
		StartedAt:      time.Now(),
	}
	ctx := m.register(job)
	go m.runRepoCopy(ctx, job, dstHost, c.Write)
	return job, nil
}

func (m *Manager) runRepoCopy(ctx context.Context, job *TransferJob, dstHost *store.Host, write func(context.Context, io.Writer) error) {
	dstRunner, err := m.getRunnerForHost(dstHost)
	if err != nil {
		m.failJob(job, fmt.Sprintf("connect dest host failed: %v", err))
		return
	}
	defer dstRunner.Close()

	m.mu.Lock()
	job.Status = "transferring"
	m.mu.Unlock()

	// Fail with the reason (permissions, read-only filesystem) before any data is sent.
	q := shellQuote(job.DestPath)
	if _, stderr, code, err := dstRunner.Exec(fmt.Sprintf(`mkdir -p %s && test -w %s`, q, q)); err != nil || code != 0 {
		reason := strings.TrimSpace(stderr)
		if reason == "" {
			reason = "directory is not writable for the SSH user"
		}
		m.failJob(job, fmt.Sprintf("cannot write to %s on %s: %s", job.DestPath, dstHost.Name, reason))
		return
	}

	pr, pw := io.Pipe()
	var transferred int64
	cw := &countingWriter{w: pw, onWrite: func(n int) { atomic.AddInt64(&transferred, int64(n)) }}
	pctx, stopProgress := context.WithCancel(ctx)
	defer stopProgress()
	go m.reportProgress(pctx, job, &transferred)

	srcErr, dstErr := make(chan error, 1), make(chan error, 1)
	go func() {
		err := write(ctx, cw)
		_ = pw.CloseWithError(err)
		srcErr <- err
	}()
	go func() {
		var out capBuf
		err := dstRunner.PipeCommand(fmt.Sprintf(`tar -xf - -C %s 2>&1`, q), pr, &out)
		if err != nil && out.String() != "" {
			err = fmt.Errorf("%v: %s", err, out.String())
		}
		_ = pr.CloseWithError(err)
		dstErr <- err
	}()

	var sErr, dErr error
	for pending := 2; pending > 0; pending-- {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(context.Canceled)
			_ = pr.CloseWithError(context.Canceled)
			m.mu.Lock()
			job.Status = "cancelled"
			now := time.Now()
			job.CompletedAt = &now
			m.mu.Unlock()
			return
		case sErr = <-srcErr:
		case dErr = <-dstErr:
		}
	}
	// The host's tar error explains a broken pipe on the sending side, so it wins.
	if dErr != nil || sErr != nil {
		err := dErr
		if err == nil {
			err = sErr
		}
		m.failJob(job, fmt.Sprintf("copy failed: %v", err))
		return
	}

	m.mu.Lock()
	job.Status = "completed"
	job.Percentage = 100.0
	job.TransferredBytes = atomic.LoadInt64(&transferred)
	job.SpeedBytesPerSec = 0
	job.ETASeconds = 0
	now := time.Now()
	job.CompletedAt = &now
	m.mu.Unlock()
}
