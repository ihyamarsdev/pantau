package teleport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeCall is one command the Manager asked the runner to run.
type fakeCall struct {
	Bin  string
	Args []string
	Env  []string
}

// fakeRunner stands in for tsh and kubectl. handle decides each result; a nil handle returns success with no output.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []fakeCall
	handle func(fakeCall) (Result, error)
}

func (f *fakeRunner) Run(_ context.Context, env []string, bin string, args ...string) (Result, error) {
	c := fakeCall{Bin: bin, Args: append([]string(nil), args...), Env: append([]string(nil), env...)}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if f.handle == nil {
		return Result{}, nil
	}
	return f.handle(c)
}

func (f *fakeRunner) Calls() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall(nil), f.calls...)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

// kubeconfigFor is the file `tsh kube login` leaves behind: current-context set to ctxName, with that context listed.
func kubeconfigFor(ctxName string) []byte {
	return []byte("apiVersion: v1\nkind: Config\ncurrent-context: " + ctxName +
		"\ncontexts:\n- name: " + ctxName + "\n  context:\n    cluster: c\n    user: u\n")
}

// newTestManager builds a Manager whose tsh is a real executable file in a temp dir. The fake runner never runs it.
func newTestManager(t *testing.T, fake *fakeRunner) (*Manager, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	tshPath := filepath.Join(t.TempDir(), "tsh")
	if err := os.WriteFile(tshPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dataDir, tshPath, fake)
	if err != nil {
		t.Fatal(err)
	}
	m.userHome = func() (string, error) { return "/home/tester", nil }
	return m, dataDir, tshPath
}

var testProfile = Profile{
	ID:              7,
	Proxy:           "teleport.setoranku.com:443",
	AuthConnector:   "local",
	Username:        "alice",
	TeleportCluster: "tc",
}

// loginHandler imitates `tsh kube login <name>`: it writes the isolated kubeconfig at the path in KUBECONFIG, mode
// 0644, so a test can see that Pantau tightens it to 0600. Names in failFor exit with the given result instead.
func loginHandler(failFor map[string]Result) func(fakeCall) (Result, error) {
	return func(c fakeCall) (Result, error) {
		if len(c.Args) < 3 || c.Args[0] != "kube" || c.Args[1] != "login" {
			return Result{}, nil
		}
		name := c.Args[len(c.Args)-1]
		if r, ok := failFor[name]; ok {
			return r, nil
		}
		if err := os.WriteFile(envValue(c.Env, "KUBECONFIG"), kubeconfigFor("tc/"+name), 0o644); err != nil {
			return Result{}, err
		}
		return Result{}, nil
	}
}

// listThen answers `tsh kube ls` with lsOut and hands every other command to next.
func listThen(lsOut string, next func(fakeCall) (Result, error)) func(fakeCall) (Result, error) {
	return func(c fakeCall) (Result, error) {
		if len(c.Args) > 1 && c.Args[0] == "kube" && c.Args[1] == "ls" {
			return Result{Stdout: []byte(lsOut)}, nil
		}
		if next == nil {
			return Result{}, nil
		}
		return next(c)
	}
}

const activeStatus = `{"active":{"profile_url":"https://teleport.setoranku.com:443","cluster":"tc","username":"alice","valid_until":"2030-01-02T03:04:05Z"},"profiles":[]}`

func TestNewManagerRequiresAbsoluteDataDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dataDir string
		wantErr bool
	}{
		{name: "absolute", dataDir: t.TempDir()},
		{name: "relative", dataDir: "data", wantErr: true},
		{name: "empty", dataDir: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewManager(tt.dataDir, "", &fakeRunner{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKubeconfigPath(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	tests := []struct {
		name    string
		id      int64
		want    string
		wantErr bool
	}{
		{name: "profile file under teleport dir", id: 7, want: filepath.Join(dataDir, "teleport", "profile-7.yaml")},
		{name: "zero id rejected", id: 0, wantErr: true},
		{name: "negative id rejected", id: -3, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := KubeconfigPath(dataDir, tt.id)
			if (err != nil) != tt.wantErr {
				t.Fatalf("KubeconfigPath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("KubeconfigPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	t.Parallel()

	t.Run("active session reads proxy and cluster match", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stdout: []byte(activeStatus)}, nil
		}}
		m, dataDir, tshPath := newTestManager(t, fake)

		st, err := m.Status(context.Background(), testProfile)
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if !st.Active || st.Username != "alice" {
			t.Fatalf("Status() = %+v, want active session for alice", st)
		}
		calls := fake.Calls()
		if len(calls) != 1 {
			t.Fatalf("got %d calls, want 1", len(calls))
		}
		if calls[0].Bin != tshPath || !reflect.DeepEqual(calls[0].Args, []string{"status", "--format=json"}) {
			t.Fatalf("call = %s %v, want tsh status --format=json", calls[0].Bin, calls[0].Args)
		}
		wantKube, _ := KubeconfigPath(dataDir, testProfile.ID)
		if got := envValue(calls[0].Env, "KUBECONFIG"); got != wantKube {
			t.Fatalf("KUBECONFIG = %q, want the isolated file %q", got, wantKube)
		}
		if envValue(calls[0].Env, "TELEPORT_HOME") != "" {
			t.Fatal("TELEPORT_HOME must stay unset when the profile has no home")
		}
	})

	t.Run("teleport home is passed as TELEPORT_HOME", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stdout: []byte(activeStatus)}, nil
		}}
		m, _, _ := newTestManager(t, fake)
		p := testProfile
		p.TeleportHome = "~/.tsh-staging"

		if _, err := m.Status(context.Background(), p); err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if got := envValue(fake.Calls()[0].Env, "TELEPORT_HOME"); got != "/home/tester/.tsh-staging" {
			t.Fatalf("TELEPORT_HOME = %q, want /home/tester/.tsh-staging", got)
		}
	})

	t.Run("not logged in is an inactive status, not an error", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stderr: []byte("ERROR: Not logged in.\n"), ExitCode: 1}, nil
		}}
		m, _, _ := newTestManager(t, fake)

		st, err := m.Status(context.Background(), testProfile)
		if err != nil || st.Active {
			t.Fatalf("Status() = %+v, %v; want inactive with nil error", st, err)
		}
	})

	t.Run("other failure carries bounded output", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stderr: []byte("boom"), ExitCode: 2}, nil
		}}
		m, _, _ := newTestManager(t, fake)

		_, err := m.Status(context.Background(), testProfile)
		if err == nil || errors.Is(err, ErrTeleportSessionExpired) {
			t.Fatalf("Status() error = %v, want a plain failure", err)
		}
		if !strings.Contains(err.Error(), "boom") {
			t.Fatalf("error %q should carry the tsh output", err)
		}
	})

	t.Run("expired certificate maps to the session sentinel", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stderr: []byte("ERROR: your credentials have expired"), ExitCode: 1}, nil
		}}
		m, _, _ := newTestManager(t, fake)

		_, err := m.Status(context.Background(), testProfile)
		if !errors.Is(err, ErrTeleportSessionExpired) {
			t.Fatalf("Status() error = %v, want ErrTeleportSessionExpired", err)
		}
	})

	t.Run("invalid profile never reaches tsh", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{}
		m, _, _ := newTestManager(t, fake)
		p := testProfile
		p.Proxy = "-oProxyCommand=id"

		if _, err := m.Status(context.Background(), p); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Status() error = %v, want ErrInvalidInput", err)
		}
		if n := len(fake.Calls()); n != 0 {
			t.Fatalf("tsh was called %d times for an invalid profile", n)
		}
	})

	t.Run("missing tsh is the sentinel error and runs nothing", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{}
		m, err := NewManager(t.TempDir(), filepath.Join(t.TempDir(), "absent-tsh"), fake)
		if err != nil {
			t.Fatal(err)
		}

		_, err = m.Status(context.Background(), testProfile)
		if !errors.Is(err, ErrTSHNotInstalled) {
			t.Fatalf("Status() error = %v, want ErrTSHNotInstalled", err)
		}
		if n := len(fake.Calls()); n != 0 {
			t.Fatalf("runner was called %d times without tsh", n)
		}
	})
}

func TestListKubeClusters(t *testing.T) {
	t.Parallel()

	t.Run("lists clusters for the root cluster", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stdout: []byte(`[{"kube_cluster_name":"prod-eu","labels":{"env":"prod"}},{"kube_cluster_name":"-o"}]`)}, nil
		}}
		m, _, _ := newTestManager(t, fake)

		clusters, err := m.ListKubeClusters(context.Background(), testProfile)
		if err != nil {
			t.Fatalf("ListKubeClusters() error = %v", err)
		}
		if len(clusters) != 1 || clusters[0].Name != "prod-eu" {
			t.Fatalf("ListKubeClusters() = %+v, want only prod-eu", clusters)
		}
		want := []string{"kube", "ls", "--format=json", "--cluster", "tc"}
		if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
			t.Fatalf("args = %v, want %v", got, want)
		}
	})

	t.Run("no cluster means no --cluster flag", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) { return Result{Stdout: []byte(`[]`)}, nil }}
		m, _, _ := newTestManager(t, fake)
		p := testProfile
		p.TeleportCluster = ""

		if _, err := m.ListKubeClusters(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		want := []string{"kube", "ls", "--format=json"}
		if got := fake.Calls()[0].Args; !reflect.DeepEqual(got, want) {
			t.Fatalf("args = %v, want %v", got, want)
		}
	})

	t.Run("expired session maps to the sentinel", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: func(fakeCall) (Result, error) {
			return Result{Stderr: []byte("ERROR: not logged in, please login using tsh login"), ExitCode: 1}, nil
		}}
		m, _, _ := newTestManager(t, fake)

		if _, err := m.ListKubeClusters(context.Background(), testProfile); !errors.Is(err, ErrTeleportSessionExpired) {
			t.Fatalf("ListKubeClusters() error = %v, want ErrTeleportSessionExpired", err)
		}
	})
}

func TestEnsureKubeContext(t *testing.T) {
	t.Parallel()

	t.Run("login writes the isolated file and returns its current context", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: loginHandler(nil)}
		m, dataDir, tshPath := newTestManager(t, fake)

		name, err := m.EnsureKubeContext(context.Background(), testProfile, "prod-eu")
		if err != nil {
			t.Fatalf("EnsureKubeContext() error = %v", err)
		}
		if name != "tc/prod-eu" {
			t.Fatalf("context = %q, want tc/prod-eu", name)
		}
		call := fake.Calls()[0]
		if call.Bin != tshPath {
			t.Fatalf("bin = %q, want the resolved tsh path", call.Bin)
		}
		if want := []string{"kube", "login", "--cluster", "tc", "prod-eu"}; !reflect.DeepEqual(call.Args, want) {
			t.Fatalf("args = %v, want %v", call.Args, want)
		}
		path, _ := KubeconfigPath(dataDir, testProfile.ID)
		if got := envValue(call.Env, "KUBECONFIG"); got != path {
			t.Fatalf("KUBECONFIG = %q, want %q", got, path)
		}
		assertMode(t, path, 0o600)
		assertMode(t, filepath.Join(dataDir, "teleport"), 0o700)
	})

	t.Run("kube name that is a flag is rejected before tsh runs", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{}
		m, _, _ := newTestManager(t, fake)

		if _, err := m.EnsureKubeContext(context.Background(), testProfile, "--kubeconfig=/etc/x"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
		if n := len(fake.Calls()); n != 0 {
			t.Fatalf("tsh ran %d times for a rejected name", n)
		}
	})

	t.Run("expired credentials map to the sentinel", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: loginHandler(map[string]Result{
			"prod-eu": {Stderr: []byte("ERROR: your credentials have expired, please login using `tsh login`"), ExitCode: 1},
		})}
		m, _, _ := newTestManager(t, fake)

		if _, err := m.EnsureKubeContext(context.Background(), testProfile, "prod-eu"); !errors.Is(err, ErrTeleportSessionExpired) {
			t.Fatalf("error = %v, want ErrTeleportSessionExpired", err)
		}
	})

	t.Run("login failure reports exit code and output", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: loginHandler(map[string]Result{
			"prod-eu": {Stderr: []byte("cluster not found"), ExitCode: 3},
		})}
		m, _, _ := newTestManager(t, fake)

		_, err := m.EnsureKubeContext(context.Background(), testProfile, "prod-eu")
		if err == nil || !strings.Contains(err.Error(), "exit 3") || !strings.Contains(err.Error(), "cluster not found") {
			t.Fatalf("error = %v, want exit code and output", err)
		}
	})

	t.Run("concurrent logins for one profile each get their own context", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: loginHandler(nil)}
		m, _, _ := newTestManager(t, fake)

		const n = 8
		var wg sync.WaitGroup
		var mu sync.Mutex
		got := make(map[string]string, n)
		var errs []error
		for i := range n {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				ctxName, err := m.EnsureKubeContext(context.Background(), testProfile, name)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				got[name] = ctxName
			}(fmt.Sprintf("cluster-%d", i))
		}
		wg.Wait()

		if len(errs) != 0 {
			t.Fatalf("EnsureKubeContext errors: %v", errs)
		}
		for i := range n {
			name := fmt.Sprintf("cluster-%d", i)
			if got[name] != "tc/"+name {
				t.Fatalf("%s got context %q, want tc/%s: another login changed current-context", name, got[name], name)
			}
		}
	})
}

func TestSyncAllContexts(t *testing.T) {
	t.Parallel()

	t.Run("collects per-cluster failures and keeps the rest", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: listThen(`[{"kube_cluster_name":"a"},{"kube_cluster_name":"b"},{"kube_cluster_name":"c"}]`,
			loginHandler(map[string]Result{"b": {Stderr: []byte("boom"), ExitCode: 1}}))}
		m, _, _ := newTestManager(t, fake)

		contexts, err := m.SyncAllContexts(context.Background(), testProfile)
		if err == nil || !strings.Contains(err.Error(), "b:") {
			t.Fatalf("SyncAllContexts() error = %v, want a failure naming b", err)
		}
		want := map[string]string{"a": "tc/a", "c": "tc/c"}
		if !reflect.DeepEqual(contexts, want) {
			t.Fatalf("contexts = %v, want %v", contexts, want)
		}
	})

	t.Run("stops at the first expired session", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRunner{handle: listThen(`[{"kube_cluster_name":"a"},{"kube_cluster_name":"b"}]`,
			loginHandler(map[string]Result{"a": {Stderr: []byte("certificate has expired"), ExitCode: 1}}))}
		m, _, _ := newTestManager(t, fake)

		_, err := m.SyncAllContexts(context.Background(), testProfile)
		if !errors.Is(err, ErrTeleportSessionExpired) {
			t.Fatalf("SyncAllContexts() error = %v, want ErrTeleportSessionExpired", err)
		}
		logins := 0
		for _, c := range fake.Calls() {
			if len(c.Args) > 1 && c.Args[1] == "login" {
				logins++
			}
		}
		if logins != 1 {
			t.Fatalf("made %d login calls, want 1 (stop at expiry)", logins)
		}
	})
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
