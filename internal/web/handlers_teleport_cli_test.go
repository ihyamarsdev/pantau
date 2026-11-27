package web

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"pantau/internal/teleport"
)

// teleportCLIFake is a local TLS server standing in for the GitHub release API, a proxy's /webapi/ping and the
// Teleport CDN. Nothing here reaches the internet, and the profile the tests create points at this server.
type teleportCLIFake struct {
	srv          *httptest.Server
	tag          string
	proxyVersion string

	mu      sync.Mutex
	files   map[string][]byte
	gate    chan struct{} // when set, tarball downloads wait until it is closed
	started chan struct{} // receives one value when a tarball download begins
}

func newTeleportCLIFake(t *testing.T, tag, proxyVersion string) *teleportCLIFake {
	t.Helper()
	f := &teleportCLIFake{tag: tag, proxyVersion: proxyVersion, files: map[string][]byte{}, started: make(chan struct{}, 1)}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/releases/latest":
			if f.tag == "" {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintf(w, `{"tag_name":%q}`, f.tag)
		case "/webapi/ping":
			fmt.Fprintf(w, `{"server_version":%q}`, f.proxyVersion)
		default:
			body, ok := f.files[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if f.gate != nil && strings.HasSuffix(r.URL.Path, ".tar.gz") {
				select {
				case f.started <- struct{}{}:
				default:
				}
				gate := f.gate
				f.mu.Unlock()
				<-gate
				f.mu.Lock()
			}
			w.Write(body)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// wire points the Server's Teleport manager at this fake, so no request leaves the test.
func (f *teleportCLIFake) wire(t *testing.T, srv *Server) {
	t.Helper()
	err := srv.teleportMgr.SetUpstream(teleport.Upstream{
		ReleaseAPI: f.srv.URL + "/releases/latest",
		CDN:        f.srv.URL,
		Client:     f.srv.Client(),
		ProxyPing:  f.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// host is the proxy address of the fake, as a profile stores it.
func (f *teleportCLIFake) host() string { return strings.TrimPrefix(f.srv.URL, "https://") }

// serveRelease publishes a tsh release: the tarball holding a tsh that reports version, and its .sha256 file.
func (f *teleportCLIFake) serveRelease(t *testing.T, version string) {
	t.Helper()
	name := fmt.Sprintf("teleport-v%s-%s-%s-bin.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	script := "#!/bin/sh\nprintf '%s' '{\"version\":\"" + version + "\"}'\n"
	data := buildTSHTarball(t, script)
	sum := sha256.Sum256(data)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files["/"+name] = data
	f.files["/"+name+".sha256"] = []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
}

// buildTSHTarball is a gzip tar holding teleport/tsh with the given body, as the Teleport release does.
func buildTSHTarball(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "teleport/tsh", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// createProfileAt stores a Teleport profile whose proxy is the fake, and returns its ID.
func createProfileAt(t *testing.T, srv *Server, auth func(*http.Request), proxy string) int64 {
	t.Helper()
	body := make(map[string]interface{}, len(prodProfileBody)+1)
	for k, v := range prodProfileBody {
		body[k] = v
	}
	body["proxy"] = proxy
	rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/profiles", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	return int64(decodeTeleportJSON(t, rec)["id"].(float64))
}

func TestTeleportCLIStatus(t *testing.T) {
	srv, auth, _ := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)

	rec := teleportCall(t, srv, auth, http.MethodGet, "/api/teleport/cli/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	body := decodeTeleportJSON(t, rec)
	tsh, ok := body["tsh"].(map[string]interface{})
	if !ok {
		t.Fatalf("no tsh object in %v", body)
	}
	if tsh["installed"] != true || tsh["source"] != teleport.SourceEnv || tsh["version"] != "18.10.0" {
		t.Fatalf("tsh = %v, want installed, source env, version 18.10.0", tsh)
	}
	if tsh["override_active"] != true {
		t.Fatalf("override_active = %v, want true while PANTAU_TSH_BIN is set", tsh["override_active"])
	}
	if _, ok := body["kubectl"].(map[string]interface{}); !ok {
		t.Fatalf("no kubectl object in %v", body)
	}
}

func TestTeleportCLICheckUpdate(t *testing.T) {
	tests := []struct {
		name         string
		tag          string // latest release; "" makes the upstream answer 503
		proxyVersion string
		profileID    string // "" omits profile_id; "missing" uses an unknown ID
		wantStatus   int
		wantTSH      string
		wantMismatch bool
	}{
		{name: "update available", tag: "v18.11.10", wantStatus: http.StatusOK, wantTSH: teleport.StatusUpdateAvailable},
		{name: "up to date", tag: "v18.10.0", wantStatus: http.StatusOK, wantTSH: teleport.StatusUpToDate},
		{name: "upstream outage is check_failed", tag: "", wantStatus: http.StatusOK, wantTSH: teleport.StatusCheckFailed},
		{name: "proxy major differs from tsh", tag: "v18.10.0", proxyVersion: "17.4.0", profileID: "use", wantStatus: http.StatusOK, wantTSH: teleport.StatusUpToDate, wantMismatch: true},
		{name: "unknown profile is 404", tag: "v18.10.0", profileID: "missing", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, auth, _ := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)
			fake := newTeleportCLIFake(t, tt.tag, firstNonEmpty(tt.proxyVersion, "18.10.0"))
			fake.wire(t, srv)

			req := map[string]interface{}{}
			switch tt.profileID {
			case "use":
				req["profile_id"] = createProfileAt(t, srv, auth, fake.host())
			case "missing":
				req["profile_id"] = 9999
			}
			rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/cli/check-update", req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			body := decodeTeleportJSON(t, rec)
			if body["status"] != tt.wantTSH {
				t.Fatalf("status field = %v, want %s (body %v)", body["status"], tt.wantTSH, body)
			}
			if body["major_version_mismatch"] != tt.wantMismatch {
				t.Fatalf("major_version_mismatch = %v, want %v", body["major_version_mismatch"], tt.wantMismatch)
			}
			if tt.wantTSH == teleport.StatusCheckFailed && body["error"] == nil {
				t.Fatal("check_failed must carry the reason")
			}
		})
	}
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// readNDJSON splits a streamed install answer into its lines.
func readNDJSON(t *testing.T, body string) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		var line map[string]interface{}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", sc.Text(), err)
		}
		out = append(out, line)
	}
	return out
}

func TestTeleportCLIInstall(t *testing.T) {
	t.Run("installs the requested release and reports the override", func(t *testing.T) {
		srv, auth, dataDir := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)
		fake := newTeleportCLIFake(t, "v18.11.10", "18.11.10")
		fake.wire(t, srv)
		fake.serveRelease(t, "18.11.10")

		rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/cli/install", map[string]string{"version": "18.11.10"})
		if rec.Code != http.StatusOK {
			t.Fatalf("install: %d %s", rec.Code, rec.Body.String())
		}
		lines := readNDJSON(t, rec.Body.String())
		last := lines[len(lines)-1]
		if last["done"] != true || last["error"] != nil {
			t.Fatalf("last line = %v, want a clean done", last)
		}
		var log strings.Builder
		for _, l := range lines {
			if s, ok := l["log"].(string); ok {
				log.WriteString(s)
			}
		}
		if !strings.Contains(log.String(), "PANTAU_TSH_BIN is set") {
			t.Fatalf("log does not tell the operator the override wins:\n%s", log.String())
		}
		if _, err := os.Stat(filepath.Join(dataDir, "cli", "tsh")); err != nil {
			t.Fatalf("managed tsh not installed: %v", err)
		}
	})

	t.Run("empty version installs the newest release", func(t *testing.T) {
		srv, auth, dataDir := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)
		fake := newTeleportCLIFake(t, "v18.11.10", "18.11.10")
		fake.wire(t, srv)
		fake.serveRelease(t, "18.11.10")

		rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/cli/install", map[string]string{})
		if rec.Code != http.StatusOK {
			t.Fatalf("install: %d %s", rec.Code, rec.Body.String())
		}
		lines := readNDJSON(t, rec.Body.String())
		if lines[len(lines)-1]["done"] != true {
			t.Fatalf("install did not finish: %v", lines)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "cli", "tsh")); err != nil {
			t.Fatalf("managed tsh not installed: %v", err)
		}
	})

	t.Run("a version that is not a release is 400 before anything runs", func(t *testing.T) {
		srv, auth, _ := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)
		fake := newTeleportCLIFake(t, "v18.11.10", "18.11.10")
		fake.wire(t, srv)

		for _, bad := range []string{"latest", "18.11", "../18.11.10"} {
			rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/cli/install", map[string]string{"version": bad})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("version %q: status %d, want 400", bad, rec.Code)
			}
		}
	})

	t.Run("a second install while one runs is 409", func(t *testing.T) {
		srv, auth, _ := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)
		fake := newTeleportCLIFake(t, "v18.11.10", "18.11.10")
		fake.wire(t, srv)
		fake.serveRelease(t, "18.11.10")
		gate := make(chan struct{})
		fake.mu.Lock()
		fake.gate = gate
		fake.mu.Unlock()

		first := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/api/teleport/cli/install", strings.NewReader(`{"version":"18.11.10"}`))
			req.Header.Set("Content-Type", "application/json")
			auth(req)
			rec := httptest.NewRecorder()
			srv.mux.ServeHTTP(rec, req)
			first <- rec
		}()
		<-fake.started

		rec := teleportCall(t, srv, auth, http.MethodPost, "/api/teleport/cli/install", map[string]string{"version": "18.11.10"})
		if rec.Code != http.StatusConflict {
			t.Fatalf("second install: %d %s, want 409", rec.Code, rec.Body.String())
		}
		if code := decodeTeleportJSON(t, rec)["code"]; code != "INSTALL_IN_PROGRESS" {
			t.Fatalf("code = %v, want INSTALL_IN_PROGRESS", code)
		}

		close(gate)
		done := <-first
		lines := readNDJSON(t, done.Body.String())
		if lines[len(lines)-1]["done"] != true {
			t.Fatalf("first install did not finish: %v", lines)
		}
	})
}

func TestTeleportCLIRoutesRequireSession(t *testing.T) {
	srv, _, _ := setupTeleportAPI(t, `printf '%s' '{"version":"18.10.0"}'`)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/teleport/cli/status"},
		{http.MethodPost, "/api/teleport/cli/check-update"},
		{http.MethodPost, "/api/teleport/cli/install"},
	}
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			req := httptest.NewRequest(r.method, r.path, strings.NewReader("{}"))
			rec := httptest.NewRecorder()
			srv.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}
