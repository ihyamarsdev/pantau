package transfer

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pantau/internal/s3"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type TransferJob struct {
	ID               int64      `json:"id"`
	Kind             string     `json:"kind,omitempty"` // "" = host-to-host copy, "s3" = upload to S3
	SourceHostID     int64      `json:"source_host_id"`
	SourceHostName   string     `json:"source_host_name"`
	SourcePath       string     `json:"source_path"`
	DestHostID       int64      `json:"dest_host_id"`
	DestHostName     string     `json:"dest_host_name"`
	DestPath         string     `json:"dest_path"`
	Mode             string     `json:"mode"`   // "fast", "verified"
	Status           string     `json:"status"` // "queued", "transferring", "completed", "failed", "cancelled"
	IsDirectory      bool       `json:"is_directory"`
	TotalBytes       int64      `json:"total_bytes"`
	TransferredBytes int64      `json:"transferred_bytes"`
	SpeedBytesPerSec int64      `json:"speed_bytes_per_sec"`
	Percentage       float64    `json:"percentage"`
	ETASeconds       int64      `json:"eta_seconds"`
	Error            string     `json:"error,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`

	cancelFunc context.CancelFunc
}

type Manager struct {
	db       *store.DB
	factory  sshrunner.RunnerFactory
	mu       sync.RWMutex
	jobs     map[int64]*TransferJob
	jobOrder []int64
	nextID   int64
}

func NewManager(db *store.DB, factory sshrunner.RunnerFactory) *Manager {
	if factory == nil {
		factory = sshrunner.DefaultFactory()
	}
	return &Manager{
		db:      db,
		factory: factory,
		jobs:    make(map[int64]*TransferJob),
	}
}

func (m *Manager) getRunnerForHost(h *store.Host) (sshrunner.Runner, error) {
	key, err := m.db.ResolveHostPrivateKey(h)
	if err != nil {
		return nil, err
	}
	return m.factory(h.Host, h.Port, h.User, key)
}

func (m *Manager) StartTransfer(srcHostID int64, srcPath string, dstHostID int64, dstPath string, mode string) (*TransferJob, error) {
	if srcHostID == dstHostID && srcPath == dstPath {
		return nil, fmt.Errorf("source and destination path cannot be identical on the same host")
	}

	srcHost, err := m.db.GetHost(srcHostID)
	if err != nil || srcHost == nil {
		return nil, fmt.Errorf("source host %d not found", srcHostID)
	}

	dstHost, err := m.db.GetHost(dstHostID)
	if err != nil || dstHost == nil {
		return nil, fmt.Errorf("destination host %d not found", dstHostID)
	}

	if mode != "verified" {
		mode = "fast"
	}

	job := &TransferJob{
		SourceHostID:   srcHostID,
		SourceHostName: srcHost.Name,
		SourcePath:     srcPath,
		DestHostID:     dstHostID,
		DestHostName:   dstHost.Name,
		DestPath:       dstPath,
		Mode:           mode,
		Status:         "queued",
		StartedAt:      time.Now(),
	}
	ctx := m.register(job)

	go m.runTransfer(ctx, job, srcHost, dstHost)

	return job, nil
}

// register assigns an ID, makes the job visible in the job list and returns the context that CancelTransfer cancels.
func (m *Manager) register(job *TransferJob) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancelFunc = cancel
	m.mu.Lock()
	m.nextID++
	job.ID = m.nextID
	m.jobs[job.ID] = job
	m.jobOrder = append([]int64{job.ID}, m.jobOrder...)
	m.mu.Unlock()
	return ctx
}

// inspectSource reports whether path is a directory and its size in bytes (0 when unknown).
func inspectSource(r sshrunner.Runner, srcPath string) (isDir bool, totalBytes int64) {
	typeOut, _, _, _ := r.Exec(fmt.Sprintf(`test -d %s && echo "DIR" || echo "FILE"`, shellQuote(srcPath)))
	isDir = strings.TrimSpace(typeOut) == "DIR"

	sizeOut, _, _, _ := r.Exec(fmt.Sprintf(`du -sb %s 2>/dev/null | cut -f1`, shellQuote(srcPath)))
	totalBytes, _ = strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
	if totalBytes <= 0 {
		statOut, _, _, _ := r.Exec(fmt.Sprintf(`stat -c "%%s" %s 2>/dev/null`, shellQuote(srcPath)))
		totalBytes, _ = strconv.ParseInt(strings.TrimSpace(statOut), 10, 64)
	}
	return isDir, totalBytes
}

// reportProgress publishes throughput, percentage and ETA of job every 500ms until ctx ends.
func (m *Manager) reportProgress(ctx context.Context, job *TransferJob, transferred *int64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastBytes := int64(0)
	lastTime := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := atomic.LoadInt64(transferred)
			now := time.Now()
			elapsedSec := now.Sub(lastTime).Seconds()
			if elapsedSec <= 0 {
				elapsedSec = 0.5
			}
			speed := int64(float64(current-lastBytes) / elapsedSec)
			lastBytes = current
			lastTime = now

			m.mu.Lock()
			job.TransferredBytes = current
			job.SpeedBytesPerSec = speed
			if job.TotalBytes > 0 {
				job.Percentage = min(float64(current)/float64(job.TotalBytes)*100.0, 100.0)
				remaining := job.TotalBytes - current
				if remaining > 0 && speed > 0 {
					job.ETASeconds = remaining / speed
				} else {
					job.ETASeconds = 0
				}
			}
			m.mu.Unlock()
		}
	}
}

// StartS3Upload uploads a file (as is) or a folder (as <name>.tar.gz) from srcHostID to the bucket of S3 account
// accountID. prefix overrides the account's default key prefix when non-nil ("" = bucket root).
func (m *Manager) StartS3Upload(srcHostID int64, srcPath string, accountID int64, prefix *string) (*TransferJob, error) {
	acct, err := m.db.GetS3Account(accountID)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		return nil, fmt.Errorf("S3 account %d not found; add one in Settings > S3 Storage", accountID)
	}
	cfg := s3.NewConfig(acct.Endpoint, acct.Region, acct.Bucket, acct.AccessKey, acct.SecretKey, acct.PathStyle)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("S3 account %q: %w", acct.Name, err)
	}
	srcPath = path.Clean(srcPath)
	if !path.IsAbs(srcPath) || srcPath == "/" {
		return nil, fmt.Errorf("source must be an absolute path other than /")
	}
	srcHost, err := m.db.GetHost(srcHostID)
	if err != nil || srcHost == nil {
		return nil, fmt.Errorf("source host %d not found", srcHostID)
	}
	keyPrefix := s3.NormalizePrefix(acct.Prefix)
	if prefix != nil {
		keyPrefix = s3.NormalizePrefix(*prefix)
	}

	job := &TransferJob{
		Kind:           "s3",
		SourceHostID:   srcHostID,
		SourceHostName: srcHost.Name,
		SourcePath:     srcPath,
		DestHostName:   acct.Name,
		DestPath:       "s3://" + cfg.Bucket + "/" + keyPrefix,
		Mode:           "fast",
		Status:         "queued",
		StartedAt:      time.Now(),
	}
	ctx := m.register(job)
	go m.runS3Upload(ctx, job, srcHost, cfg, keyPrefix)
	return job, nil
}

func (m *Manager) runS3Upload(ctx context.Context, job *TransferJob, srcHost *store.Host, cfg s3.Config, keyPrefix string) {
	srcRunner, err := m.getRunnerForHost(srcHost)
	if err != nil {
		m.failJob(job, fmt.Sprintf("connect source host failed: %v", err))
		return
	}
	defer srcRunner.Close()

	if _, _, code, _ := srcRunner.Exec(fmt.Sprintf(`test -r %s`, shellQuote(job.SourcePath))); code != 0 {
		m.failJob(job, "source path does not exist or is not readable by the SSH user")
		return
	}
	isDir, total := inspectSource(srcRunner, job.SourcePath)

	key := keyPrefix + path.Base(job.SourcePath)
	cmd := fmt.Sprintf(`cat %s`, shellQuote(job.SourcePath))
	if isDir {
		key += ".tar.gz"
		// tar exits 1 when a file changed while it was read (live logs); that archive is still usable, only 2+ is fatal.
		cmd = "sh -c " + shellQuote(fmt.Sprintf(`tar -czf - -C %s %s; rc=$?; [ $rc -le 1 ]`,
			shellQuote(path.Dir(job.SourcePath)), shellQuote(path.Base(job.SourcePath))))
	}

	m.mu.Lock()
	job.Status = "transferring"
	job.IsDirectory = isDir
	job.TotalBytes = total
	job.DestPath = "s3://" + cfg.Bucket + "/" + key
	m.mu.Unlock()

	pr, pw := io.Pipe()
	var sent int64
	cw := &countingWriter{w: pw, onWrite: func(n int) { atomic.AddInt64(&sent, int64(n)) }}

	pctx, stop := context.WithCancel(ctx)
	defer stop()
	go m.reportProgress(pctx, job, &sent)

	srcErr := make(chan error, 1)
	go func() {
		err := srcRunner.PipeCommand(cmd, nil, cw)
		_ = pw.CloseWithError(err) // nil -> EOF
		srcErr <- err
	}()

	upErr := s3.New(cfg).Upload(ctx, key, pr, total)
	_ = pr.CloseWithError(context.Canceled)
	if upErr != nil {
		_ = srcRunner.Close() // unblock a remote cat/tar still writing into the closed pipe
	}
	<-srcErr

	if ctx.Err() != nil { // CancelTransfer already marked the job cancelled
		return
	}
	if upErr != nil {
		m.failJob(job, fmt.Sprintf("S3 upload failed: %v", upErr))
		return
	}

	m.mu.Lock()
	job.Status = "completed"
	job.Percentage = 100.0
	job.TransferredBytes = atomic.LoadInt64(&sent)
	job.SpeedBytesPerSec = 0
	job.ETASeconds = 0
	now := time.Now()
	job.CompletedAt = &now
	m.mu.Unlock()
}

func (m *Manager) runTransfer(ctx context.Context, job *TransferJob, srcHost, dstHost *store.Host) {
	srcRunner, err := m.getRunnerForHost(srcHost)
	if err != nil {
		m.failJob(job, fmt.Sprintf("connect source host failed: %v", err))
		return
	}
	defer srcRunner.Close()

	dstRunner, err := m.getRunnerForHost(dstHost)
	if err != nil {
		m.failJob(job, fmt.Sprintf("connect dest host failed: %v", err))
		return
	}
	defer dstRunner.Close()

	// 1. Inspect source: check if directory and calculate total size
	isDir, totalBytes := inspectSource(srcRunner, job.SourcePath)

	m.mu.Lock()
	job.Status = "transferring"
	job.IsDirectory = isDir
	job.TotalBytes = totalBytes
	m.mu.Unlock()

	// 2. Resolve destination. A file sent to an existing directory (or a path ending in "/") lands inside it under its own name.
	var destDir string
	var destFile string
	if isDir {
		destDir = job.DestPath
	} else {
		destFile = job.DestPath
		destTypeOut, _, _, _ := dstRunner.Exec(fmt.Sprintf(`test -d %s && echo "DIR" || echo "FILE"`, shellQuote(job.DestPath)))
		if strings.HasSuffix(job.DestPath, "/") || strings.TrimSpace(destTypeOut) == "DIR" {
			destFile = path.Join(job.DestPath, path.Base(job.SourcePath))
			destDir = job.DestPath
			m.mu.Lock()
			job.DestPath = destFile
			m.mu.Unlock()
		} else {
			destDir = path.Dir(job.DestPath)
		}
	}
	_, _, _, _ = dstRunner.Exec(fmt.Sprintf(`mkdir -p %s`, shellQuote(destDir)))

	// 3. Create stream pipe
	pr, pw := io.Pipe()

	var transferred int64
	cw := &countingWriter{
		w: pw,
		onWrite: func(n int) {
			atomic.AddInt64(&transferred, int64(n))
		},
	}

	pctx, stopProgress := context.WithCancel(ctx)
	defer stopProgress()
	go m.reportProgress(pctx, job, &transferred)

	// 4. Source command & Destination command
	var srcCmd string
	var dstCmd string

	if isDir {
		srcParent := path.Dir(job.SourcePath)
		srcBase := path.Base(job.SourcePath)
		srcCmd = fmt.Sprintf(`tar -cf - -C %s %s`, shellQuote(srcParent), shellQuote(srcBase))
		dstCmd = fmt.Sprintf(`tar -xf - -C %s`, shellQuote(job.DestPath))
	} else {
		srcCmd = fmt.Sprintf(`cat %s`, shellQuote(job.SourcePath))
		dstCmd = fmt.Sprintf(`cat > %s`, shellQuote(destFile))
	}

	errChan := make(chan error, 2)

	// Goroutine 1: Source reader -> writes to cw
	go func() {
		err := srcRunner.PipeCommand(srcCmd, nil, cw)
		_ = pw.CloseWithError(err)
		errChan <- err
	}()

	// Goroutine 2: Reads from pr -> sends to Destination
	go func() {
		err := dstRunner.PipeCommand(dstCmd, pr, nil)
		_ = pr.CloseWithError(err)
		errChan <- err
	}()

	// Wait for completion or context cancellation
	var execErr error
	for i := 0; i < 2; i++ {
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
		case err := <-errChan:
			if err != nil && execErr == nil {
				execErr = err
			}
		}
	}

	if execErr != nil {
		m.failJob(job, fmt.Sprintf("stream transfer failed: %v", execErr))
		return
	}

	// 5. Verification Phase (if Verified Mode requested)
	if job.Mode == "verified" {
		if !isDir {
			srcHashOut, _, _, _ := srcRunner.Exec(fmt.Sprintf(`sha256sum %s | cut -d' ' -f1`, shellQuote(job.SourcePath)))
			dstHashOut, _, _, _ := dstRunner.Exec(fmt.Sprintf(`sha256sum %s | cut -d' ' -f1`, shellQuote(destFile)))

			srcHash := strings.TrimSpace(srcHashOut)
			dstHash := strings.TrimSpace(dstHashOut)

			if srcHash == "" || srcHash != dstHash {
				m.failJob(job, fmt.Sprintf("SHA256 checksum mismatch: source=%s, dest=%s", srcHash, dstHash))
				return
			}
		} else {
			dstCountOut, _, _, _ := dstRunner.Exec(fmt.Sprintf(`find %s -type f | wc -l`, shellQuote(job.DestPath)))
			if strings.TrimSpace(dstCountOut) == "0" && job.TotalBytes > 0 {
				m.failJob(job, "directory transfer verification failed: destination has 0 files")
				return
			}
		}
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

func (m *Manager) failJob(job *TransferJob, errStr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Status = "failed"
	job.Error = errStr
	now := time.Now()
	job.CompletedAt = &now
}

func (m *Manager) CancelTransfer(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job %d not found", id)
	}
	if job.Status == "transferring" || job.Status == "queued" {
		if job.cancelFunc != nil {
			job.cancelFunc()
		}
		job.Status = "cancelled"
		now := time.Now()
		job.CompletedAt = &now
	}
	return nil
}

func (m *Manager) ListJobs() []*TransferJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*TransferJob
	for _, id := range m.jobOrder {
		if j, ok := m.jobs[id]; ok {
			list = append(list, j)
		}
	}
	return list
}

func (m *Manager) GetJob(id int64) *TransferJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

type countingWriter struct {
	w       io.Writer
	onWrite func(n int)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}
