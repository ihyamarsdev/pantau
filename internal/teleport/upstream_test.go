package teleport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeUpstream is one local TLS server standing in for the GitHub release API, the proxy's /webapi/ping and the
// Teleport CDN. Nothing reaches the internet. Its fields are read under mu, so tests can change them between calls.
type fakeUpstream struct {
	srv  *httptest.Server
	hits int32

	mu           sync.Mutex
	release      string            // tag_name returned by the release API; "" answers 500
	releaseCode  int               // when set, the release API answers this status
	proxyVersion string            // server_version returned by /webapi/ping; "" answers 500
	files        map[string][]byte // CDN path -> body
	gate         chan struct{}     // when set, CDN tarball requests wait until it is closed
	started      chan struct{}     // receives one value when a tarball request begins (buffered)
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{files: map[string][]byte{}, started: make(chan struct{}, 1)}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpstream) serve(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&f.hits, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/releases/latest":
		if f.releaseCode != 0 {
			w.WriteHeader(f.releaseCode)
			return
		}
		if f.release == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q}`, f.release)
	case r.URL.Path == "/webapi/ping":
		if f.proxyVersion == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
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
			select {
			case <-gate:
			case <-r.Context().Done():
			}
			f.mu.Lock()
		}
		w.Write(body)
	}
}

func (f *fakeUpstream) set(fn func(f *fakeUpstream)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeUpstream) requests() int32 { return atomic.LoadInt32(&f.hits) }

// wire points m at this server for the release API, the CDN and the proxy ping.
func (f *fakeUpstream) wire(t *testing.T, m *Manager) {
	t.Helper()
	err := m.SetUpstream(Upstream{
		ReleaseAPI: f.srv.URL + "/releases/latest",
		CDN:        f.srv.URL,
		Client:     f.srv.Client(),
		ProxyPing:  f.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// versionRunner answers `tsh version --client --format=json` with the given version and nothing else.
func versionRunner(version string) *fakeRunner {
	return &fakeRunner{handle: func(c fakeCall) (Result, error) {
		if len(c.Args) > 0 && c.Args[0] == "version" {
			return Result{Stdout: []byte(fmt.Sprintf(`{"version":%q}`, version))}, nil
		}
		return Result{}, nil
	}}
}

// newUpdateManager returns a Manager whose tsh is an installed executable reporting installed, wired to up.
func newUpdateManager(t *testing.T, up *fakeUpstream, installed string) *Manager {
	t.Helper()
	m, _, _ := newTestManager(t, versionRunner(installed))
	up.wire(t, m)
	return m
}

func TestCheckUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		installed     string
		release       string // tag the release API returns
		releaseCode   int    // overrides release when set
		proxyVersion  string // "" answers the ping with 500 (proxy error)
		withProfile   bool
		wantStatus    string
		wantMajorMiss bool
		wantProxyErr  bool
	}{
		{name: "newer release is an update", installed: "18.10.0", release: "v18.11.10", wantStatus: StatusUpdateAvailable},
		{name: "same version is up to date", installed: "18.11.10", release: "v18.11.10", wantStatus: StatusUpToDate},
		{name: "installed ahead of latest is up to date", installed: "18.12.0", release: "v18.11.10", wantStatus: StatusUpToDate},
		{name: "upstream outage is check_failed, never up to date", installed: "18.11.10", releaseCode: http.StatusServiceUnavailable, wantStatus: StatusCheckFailed},
		{name: "a tag that is not a release is check_failed", installed: "18.11.10", release: "latest", wantStatus: StatusCheckFailed},
		{name: "proxy major mismatch is flagged", installed: "18.11.10", release: "v18.11.10", proxyVersion: "17.9.0", withProfile: true, wantStatus: StatusUpToDate, wantMajorMiss: true},
		{name: "matching proxy major is not flagged", installed: "18.10.0", release: "v18.11.10", proxyVersion: "18.11.2", withProfile: true, wantStatus: StatusUpdateAvailable},
		{name: "a failing proxy ping leaves the tsh status alone", installed: "18.11.10", release: "v18.11.10", withProfile: true, wantStatus: StatusUpToDate, wantProxyErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			up := newFakeUpstream(t)
			up.set(func(f *fakeUpstream) {
				f.release = tt.release
				f.releaseCode = tt.releaseCode
				f.proxyVersion = tt.proxyVersion
			})
			m := newUpdateManager(t, up, tt.installed)

			var profile *Profile
			if tt.withProfile {
				// The proxy is the test server itself, so the ping goes to a host that answers it.
				profile = &Profile{ID: 7, Proxy: strings.TrimPrefix(up.srv.URL, "https://")}
			}
			res := m.CheckUpdate(context.Background(), profile, true)

			if res.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q (error %q)", res.Status, tt.wantStatus, res.Error)
			}
			if res.Status == StatusCheckFailed && res.Error == "" {
				t.Fatal("check_failed must say why")
			}
			if res.Installed != tt.installed {
				t.Fatalf("installed = %q, want %q", res.Installed, tt.installed)
			}
			if res.MajorVersionMismatch != tt.wantMajorMiss {
				t.Fatalf("major_version_mismatch = %v, want %v", res.MajorVersionMismatch, tt.wantMajorMiss)
			}
			if profile != nil {
				if res.Proxy == nil || res.Proxy.ProfileID != 7 {
					t.Fatalf("proxy check missing: %+v", res.Proxy)
				}
				if (res.Proxy.Error != "") != tt.wantProxyErr {
					t.Fatalf("proxy error = %q, want error %v", res.Proxy.Error, tt.wantProxyErr)
				}
				if tt.proxyVersion != "" && res.Proxy.Version != tt.proxyVersion {
					t.Fatalf("proxy version = %q, want %q", res.Proxy.Version, tt.proxyVersion)
				}
			}
		})
	}
}

func TestCheckUpdateNotInstalled(t *testing.T) {
	t.Parallel()

	// No tsh at all: point the manager at an empty data directory and an override that does not exist would be an
	// error, so build a manager with no override and check the status function directly.
	if got := compareStatus(TSHInfo{Installed: false}, "18.11.10"); got != StatusNotInstalled {
		t.Fatalf("compareStatus(not installed) = %q, want %q", got, StatusNotInstalled)
	}
	if got := compareStatus(TSHInfo{Installed: true, Version: ""}, "18.11.10"); got != StatusUnknown {
		t.Fatalf("compareStatus(unreadable version) = %q, want %q", got, StatusUnknown)
	}
}

func TestLatestIsCachedUntilForced(t *testing.T) {
	t.Parallel()

	up := newFakeUpstream(t)
	up.set(func(f *fakeUpstream) { f.release = "v18.11.10" })
	m := newUpdateManager(t, up, "18.11.10")
	ctx := context.Background()

	first, cached, err := m.Latest(ctx, false)
	if err != nil || cached || first.Version != "18.11.10" {
		t.Fatalf("first lookup = (%+v, cached %v, %v)", first, cached, err)
	}
	before := up.requests()

	_, cached, err = m.Latest(ctx, false)
	if err != nil || !cached {
		t.Fatalf("second lookup should come from the cache: cached %v, err %v", cached, err)
	}
	if up.requests() != before {
		t.Fatalf("cached lookup reached the network")
	}

	if _, cached, _ = m.Latest(ctx, true); cached {
		t.Fatal("force must skip the cache")
	}
}

func TestProxyVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		proxy   func(up *fakeUpstream) string
		setup   func(f *fakeUpstream)
		want    string
		wantErr error
	}{
		{
			name:  "reads server_version from /webapi/ping",
			proxy: func(up *fakeUpstream) string { return strings.TrimPrefix(up.srv.URL, "https://") },
			setup: func(f *fakeUpstream) { f.proxyVersion = "18.11.10" },
			want:  "18.11.10",
		},
		{
			name:    "proxy address with a path is refused before any request",
			proxy:   func(*fakeUpstream) string { return "proxy.example.com/evil" },
			setup:   func(*fakeUpstream) {},
			wantErr: ErrInvalidInput,
		},
		{
			name:  "proxy answering with an error is reported",
			proxy: func(up *fakeUpstream) string { return strings.TrimPrefix(up.srv.URL, "https://") },
			setup: func(*fakeUpstream) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			up := newFakeUpstream(t)
			up.set(tt.setup)
			m := newUpdateManager(t, up, "18.11.10")

			got, err := m.ProxyVersion(context.Background(), tt.proxy(up))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if up.requests() != 0 {
					t.Fatal("an invalid proxy must not produce a request")
				}
				return
			}
			if tt.want == "" {
				if err == nil {
					t.Fatalf("expected an error, got version %q", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ProxyVersion() = (%q, %v), want (%q, nil)", got, err, tt.want)
			}
		})
	}
}

func TestSetUpstreamRequiresHTTPS(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestManager(t, versionRunner("18.11.10"))
	err := m.SetUpstream(Upstream{
		ReleaseAPI: "http://example.test/releases/latest",
		CDN:        "https://cdn.teleport.dev",
		Client:     http.DefaultClient,
		ProxyPing:  http.DefaultClient,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SetUpstream(http) error = %v, want ErrInvalidInput", err)
	}
}

func TestDefaultUpstreamUsesTeleportHosts(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	m, err := NewManager(dataDir, "", &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if m.up.ReleaseAPI != defaultReleaseAPI || m.up.CDN != defaultCDN {
		t.Fatalf("default upstream = %+v", m.up)
	}
	if got := m.cliDir(); got != filepath.Join(dataDir, "cli") {
		t.Fatalf("cliDir = %q", got)
	}
	if _, err := os.Stat(m.cliDir()); err == nil {
		t.Fatal("constructing a Manager must not create the cli directory")
	}
}

func TestCompareReleases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{a: "18.11.10", b: "18.11.9", want: 1},
		{a: "18.9.0", b: "18.10.0", want: -1},
		{a: "19.0.0", b: "18.99.99", want: 1},
		{a: "1.0.0", b: "1.0.0", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			t.Parallel()
			if got := compareReleases(tt.a, tt.b); got != tt.want {
				t.Fatalf("compareReleases(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMajorOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "release", in: "18.11.10", want: 18},
		{name: "single digit major", in: "9.0.1", want: 9},
		{name: "empty", in: "", want: -1},
		{name: "not a number", in: "latest.1", want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := majorOf(tt.in); got != tt.want {
				t.Fatalf("majorOf(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
