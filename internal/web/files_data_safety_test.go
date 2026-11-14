package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

type rwc struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (c rwc) Close() error {
	for _, cl := range c.closers {
		_ = cl.Close()
	}
	return nil
}

// sftpOverPipe serves the local filesystem to an sftp client through in-memory pipes, so the file handlers run
// against real files in t.TempDir().
func sftpOverPipe() (*sftp.Client, error) {
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	srv, err := sftp.NewServer(rwc{Reader: sr, Writer: sw, closers: []io.Closer{sr, sw}})
	if err != nil {
		return nil, err
	}
	go func() { _ = srv.Serve(); _ = srv.Close() }()
	return sftp.NewClientPipe(cr, cw)
}

func filesTestServer(t *testing.T) (call func(method, action string, query url.Values, body string) *httptest.ResponseRecorder, runner *sshrunner.MockRunner) {
	t.Helper()
	runner = sshrunner.NewMockRunner()
	runner.SFTPProvider = sftpOverPipe
	srv, db, authReq := setupDiskUsageServer(t, runner)
	hostID, err := db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	return func(method, action string, query url.Values, body string) *httptest.ResponseRecorder {
		u := fmt.Sprintf("/api/hosts/%d/files/%s", hostID, action)
		if query != nil {
			u += "?" + query.Encode()
		}
		req := httptest.NewRequest(method, u, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		authReq(req)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}, runner
}

// The editor saves its whole buffer back, so a file over 2 MiB must be refused instead of silently truncated.
func TestFileReadRefusesFilesTheEditorCannotHold(t *testing.T) {
	call, _ := filesTestServer(t)
	dir := t.TempDir()

	small := filepath.Join(dir, "small.txt")
	content := strings.Repeat("0123456789", 20000) // 200 KB: several SFTP packets, not just one Read
	if err := os.WriteFile(small, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	w := call(http.MethodGet, "read", url.Values{"path": {small}}, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), content[:100]) || len(w.Body.String()) < len(content) {
		t.Fatalf("200 KB file: code=%d, body %d bytes, want the whole file", w.Code, w.Body.Len())
	}

	big := filepath.Join(dir, "big.log")
	if err := os.WriteFile(big, make([]byte, 2*1024*1024+1), 0o644); err != nil {
		t.Fatal(err)
	}
	w = call(http.MethodGet, "read", url.Values{"path": {big}}, "")
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("2 MiB + 1 byte: code=%d, want 413", w.Code)
	}
	if strings.Contains(w.Body.String(), "content") {
		t.Error("an oversized file must not return any content that the editor could save back")
	}

	exact := filepath.Join(dir, "exact.bin")
	if err := os.WriteFile(exact, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if w = call(http.MethodGet, "read", url.Values{"path": {exact}}, ""); w.Code != http.StatusOK {
		t.Fatalf("exactly 2 MiB: code=%d, want 200", w.Code)
	}
}

// Delete used to answer "deleted" whatever happened: a folder with content stayed and the UI said it was gone.
func TestFileDeleteReportsWhatHappened(t *testing.T) {
	call, _ := filesTestServer(t)
	dir := t.TempDir()
	del := func(p string, recursive bool) *httptest.ResponseRecorder {
		return call(http.MethodPost, "delete", nil, fmt.Sprintf(`{"path":%q,"recursive":%v}`, p, recursive))
	}
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }

	file := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	if w := del(file, false); w.Code != http.StatusOK || exists(file) {
		t.Errorf("file: code=%d exists=%v, want 200 and gone", w.Code, exists(file))
	}

	if w := del(filepath.Join(dir, "missing"), false); w.Code != http.StatusNotFound {
		t.Errorf("missing path: code=%d, want 404", w.Code)
	}

	empty := filepath.Join(dir, "empty")
	_ = os.Mkdir(empty, 0o755)
	if w := del(empty, false); w.Code != http.StatusOK || exists(empty) {
		t.Errorf("empty folder: code=%d exists=%v, want 200 and gone", w.Code, exists(empty))
	}

	full := filepath.Join(dir, "full")
	_ = os.MkdirAll(filepath.Join(full, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(full, "sub", "keep.txt"), []byte("x"), 0o644)
	w := del(full, false)
	if w.Code != http.StatusConflict {
		t.Errorf("folder with content, not recursive: code=%d, want 409", w.Code)
	}
	if !exists(filepath.Join(full, "sub", "keep.txt")) {
		t.Fatal("a refused delete must leave the content alone")
	}
	if w = del(full, true); w.Code != http.StatusOK || exists(full) {
		t.Errorf("folder with content, recursive: code=%d exists=%v, want 200 and gone", w.Code, exists(full))
	}

	// A symlink to a folder is removed as a link; the target keeps its content.
	target := filepath.Join(dir, "target")
	_ = os.Mkdir(target, 0o755)
	_ = os.WriteFile(filepath.Join(target, "f"), []byte("x"), 0o644)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not available")
	}
	if w = del(link, true); w.Code != http.StatusOK || exists(link) {
		t.Errorf("symlink: code=%d exists=%v, want 200 and gone", w.Code, exists(link))
	}
	if !exists(filepath.Join(target, "f")) {
		t.Error("deleting a symlink must not follow it into the target")
	}
}

// When the source size cannot be measured the disk-space guard cannot run: refuse instead of copying blind.
func TestFileDuplicateFailsClosedWhenSizeUnknown(t *testing.T) {
	call, runner := filesTestServer(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	_ = os.Mkdir(src, 0o755)

	runner.Handlers["du -sb"] = func() (string, string, int, error) { return "", "", -1, errors.New("command timed out after 30s") }
	w := call(http.MethodPost, "duplicate", nil, fmt.Sprintf(`{"path":%q,"new_name":"copy"}`, src))
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("code=%d, want 504 (%s)", w.Code, w.Body.String())
	}
	for _, c := range runner.ExecHistory {
		if strings.Contains(c, "cp -a") {
			t.Fatalf("cp ran without the disk-space check: %s", c)
		}
	}

	// A host whose du has no -b (macOS) prints nothing: that is "unsupported", not "unknown", and still copies.
	runner.Handlers["du -sb"] = func() (string, string, int, error) { return "", "", 0, nil }
	w = call(http.MethodPost, "duplicate", nil, fmt.Sprintf(`{"path":%q,"new_name":"copy"}`, src))
	if w.Code != http.StatusOK {
		t.Fatalf("unsupported du: code=%d, want 200 (%s)", w.Code, w.Body.String())
	}
}

type blockingPipeRunner struct {
	*sshrunner.MockRunner
	closed chan struct{}
	once   sync.Once
}

func (b *blockingPipeRunner) PipeCommand(cmd string, in io.Reader, out io.Writer) error {
	<-b.closed
	return errors.New("session closed")
}

func (b *blockingPipeRunner) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// A browser that goes away must not leave tar running on the host.
func TestPipeWithLimitStopsWhenClientLeaves(t *testing.T) {
	r := &blockingPipeRunner{MockRunner: sshrunner.NewMockRunner(), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pipeWithLimit(ctx, r, "tar -czf - /data", io.Discard) }()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pipeWithLimit did not return after the client went away")
	}
	select {
	case <-r.closed:
	default:
		t.Error("the SSH connection was not closed, so the remote command would keep running")
	}
}
