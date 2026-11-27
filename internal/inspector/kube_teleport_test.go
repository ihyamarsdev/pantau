package inspector

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"pantau/internal/store"
	"pantau/internal/teleport"
)

func TestTeleportExpiredError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stdout  string
		stderr  string
		wantErr bool
	}{
		{name: "certificate expired on stderr", stderr: "x509: certificate has expired", wantErr: true},
		{name: "unauthorized on stdout", stdout: "error: You must be logged in (Unauthorized)", wantErr: true},
		{name: "rbac forbidden keeps its own message", stderr: `Error from server (Forbidden): pods is forbidden`},
		{name: "not found keeps its own message", stderr: `Error from server (NotFound): pods "web" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := teleportExpiredError(tt.stdout, tt.stderr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("teleportExpiredError() = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, teleport.ErrTeleportSessionExpired) {
				t.Fatalf("error %v does not wrap ErrTeleportSessionExpired", err)
			}
		})
	}
}

func TestTeleportAccess(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	kubeconfigPath, err := teleport.KubeconfigPath(dataDir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		teleportDir string
		cluster     store.KubeCluster
		wantPath    string
		wantCtx     string
		wantErr     error
		wantAnyErr  bool
	}{
		{
			name:        "provisioned cluster with its profile file",
			teleportDir: dataDir,
			cluster:     store.KubeCluster{TeleportProfileID: 7, KubeContextName: "tc/prod-eu"},
			wantPath:    kubeconfigPath,
			wantCtx:     "tc/prod-eu",
		},
		{
			name:        "no context yet",
			teleportDir: dataDir,
			cluster:     store.KubeCluster{TeleportProfileID: 7},
			wantErr:     teleport.ErrContextNotProvisioned,
		},
		{
			name:        "profile file missing means the session must be renewed",
			teleportDir: dataDir,
			cluster:     store.KubeCluster{TeleportProfileID: 8, KubeContextName: "tc/prod-eu"},
			wantErr:     teleport.ErrTeleportSessionExpired,
		},
		{
			name:        "context name that is a flag is refused",
			teleportDir: dataDir,
			cluster:     store.KubeCluster{TeleportProfileID: 7, KubeContextName: "--kubeconfig=/etc/x"},
			wantErr:     teleport.ErrInvalidInput,
		},
		{
			name:        "data directory not configured",
			teleportDir: "",
			cluster:     store.KubeCluster{TeleportProfileID: 7, KubeContextName: "tc/prod-eu"},
			wantAnyErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ins := &Inspector{teleportDir: tt.teleportDir}
			c := tt.cluster
			path, ctxName, err := ins.teleportAccess(&c)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			case tt.wantAnyErr:
				if err == nil {
					t.Fatal("expected an error")
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if path != tt.wantPath || ctxName != tt.wantCtx {
					t.Fatalf("access = (%q, %q), want (%q, %q)", path, ctxName, tt.wantPath, tt.wantCtx)
				}
			}
		})
	}
}

// The kc() that Teleport clusters run must name the isolated file and the context on every call. The test runs the
// generated shell against a fake kubectl that prints its argv, so the check covers the real quoting and the real
// function, not a string match alone.
func TestTeleportPreludeNamesFileAndContextOnEveryKubectlCall(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}

	tests := []struct {
		name       string
		kubeconfig string
		kubeCtx    string
	}{
		{name: "plain paths", kubeconfig: "/data/teleport/profile-7.yaml", kubeCtx: "tc/prod-eu"},
		{name: "path with a quote", kubeconfig: "/data/it's/teleport/profile-7.yaml", kubeCtx: "tc:prod_eu@1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			binDir := t.TempDir()
			fake := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"
			if err := os.WriteFile(filepath.Join(binDir, "kubectl"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}

			body := "kc get pods -n web"
			wrapped := wrapWithPrelude(unwrapKubeShell(body), teleportPrelude("eks", tt.kubeconfig, tt.kubeCtx))

			cmd := exec.Command("sh", "-c", wrapped)
			cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("running the wrapped command: %v", err)
			}
			got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
			want := []string{"--kubeconfig", tt.kubeconfig, "--context", tt.kubeCtx, "get", "pods", "-n", "web"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("kubectl argv = %q, want %q", got, want)
			}
		})
	}
}

func TestUnwrapKubeShellKeepsPlainBodies(t *testing.T) {
	t.Parallel()

	body := "kc get nodes"
	if got := unwrapKubeShell(body); got != body {
		t.Fatalf("unwrapKubeShell(plain) = %q, want unchanged", got)
	}
	if got := unwrapKubeShell(KubeShell(body)); got != body {
		t.Fatalf("unwrapKubeShell(KubeShell) = %q, want %q", got, body)
	}
}

// Closing a runner over an isolated file must leave that file in place: the file belongs to the profile.
func TestTeleportRunnerCloseKeepsIsolatedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "profile-7.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &localKubeRunner{kubeconfigPath: path}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("isolated kubeconfig removed by Close: %v", err)
	}
}
