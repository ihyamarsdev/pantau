package web

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/inspector"
)

// Output must reach the client while the operation is still running, and the outcome is the final marker line.
func TestDockerStreamResultFlushesBeforeOperationEnds(t *testing.T) {
	proceed := make(chan struct{})
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		dockerStreamResult(w, func(live io.Writer) error {
			_, _ = io.WriteString(live, "Pulling web\n")
			<-proceed // the client must already have seen the first line
			return errors.New("docker compose up failed (exit 1)")
		})
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if got := string(buf[:n]); got != "Pulling web\n" {
		t.Fatalf("first chunk = %q", got)
	}
	close(proceed)
	rest, _ := io.ReadAll(resp.Body)
	<-done
	if !strings.HasSuffix(string(rest), "\n[pantau] exit=1\n") || !strings.Contains(string(rest), "exit 1)") {
		t.Fatalf("tail = %q", rest)
	}
}

// Input rejected before anything ran keeps the JSON error contract.
func TestDockerStreamResultRejectionIsJSON(t *testing.T) {
	w := httptest.NewRecorder()
	dockerStreamResult(w, func(io.Writer) error { return inspector.ErrDockerInvalid })
	if w.Code != http.StatusBadRequest || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("code=%d type=%q", w.Code, w.Header().Get("Content-Type"))
	}
}
