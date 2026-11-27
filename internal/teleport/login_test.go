package teleport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoginArgs(t *testing.T) {
	t.Parallel()

	base := Profile{ID: 1, Proxy: "teleport.setoranku.com:443", AuthConnector: "local", Username: "alice", TeleportCluster: "tc"}

	tests := []struct {
		name    string
		mutate  func(p *Profile)
		want    []string
		wantErr bool
	}{
		{
			name: "full profile",
			want: []string{"login", "--proxy=teleport.setoranku.com:443", "--browser=none", "--auth=local", "--user=alice", "tc"},
		},
		{
			name:   "no connector, user or cluster",
			mutate: func(p *Profile) { p.AuthConnector, p.Username, p.TeleportCluster = "", "", "" },
			want:   []string{"login", "--proxy=teleport.setoranku.com:443", "--browser=none"},
		},
		{
			name:    "proxy that is a flag is refused",
			mutate:  func(p *Profile) { p.Proxy = "--proxy=evil.example.com" },
			wantErr: true,
		},
		{
			name:    "username that is a flag is refused",
			mutate:  func(p *Profile) { p.Username = "--user=root" },
			wantErr: true,
		},
		{
			name:    "cluster that is a flag is refused",
			mutate:  func(p *Profile) { p.TeleportCluster = "-x" },
			wantErr: true,
		},
		{
			name:    "connector with a space is refused",
			mutate:  func(p *Profile) { p.AuthConnector = "oidc --insecure" },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := base
			if tt.mutate != nil {
				tt.mutate(&p)
			}
			got, err := LoginArgs(p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoginArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("LoginArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}

// tsh logout without a profile home would log out every profile in the shared default home, so it runs only for a
// profile with its own TELEPORT_HOME.
func TestLogoutRunsOnlyForDedicatedHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		home     string
		wantRan  bool
		wantArgs []string
	}{
		{name: "shared default home is skipped", home: ""},
		{name: "dedicated home runs logout", home: "/srv/tsh-prod", wantRan: true, wantArgs: []string{"logout"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeRunner{}
			m, dataDir, _ := newTestManager(t, fake)
			p := testProfile
			p.TeleportHome = tt.home

			ran, err := m.Logout(context.Background(), p)
			if err != nil {
				t.Fatalf("Logout() error = %v", err)
			}
			if ran != tt.wantRan {
				t.Fatalf("Logout() ran = %v, want %v", ran, tt.wantRan)
			}
			calls := fake.Calls()
			if !tt.wantRan {
				if len(calls) != 0 {
					t.Fatalf("tsh ran %d times for a shared home", len(calls))
				}
				return
			}
			if !reflect.DeepEqual(calls[0].Args, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", calls[0].Args, tt.wantArgs)
			}
			want, _ := KubeconfigPath(dataDir, p.ID)
			if got := envValue(calls[0].Env, "KUBECONFIG"); got != want {
				t.Fatalf("KUBECONFIG = %q, want the isolated file", got)
			}
			if got := envValue(calls[0].Env, "TELEPORT_HOME"); got != tt.home {
				t.Fatalf("TELEPORT_HOME = %q, want %q", got, tt.home)
			}
		})
	}
}

func TestRemoveKubeconfig(t *testing.T) {
	t.Parallel()

	m, dataDir, _ := newTestManager(t, &fakeRunner{})
	path, err := KubeconfigPath(dataDir, testProfile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := m.RemoveKubeconfig(testProfile.ID); err != nil {
		t.Fatalf("RemoveKubeconfig() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("isolated kubeconfig still exists: %v", err)
	}
	if err := m.RemoveKubeconfig(testProfile.ID); err != nil {
		t.Fatalf("second RemoveKubeconfig() error = %v, want nil for a missing file", err)
	}
	if err := m.RemoveKubeconfig(0); err == nil {
		t.Fatal("RemoveKubeconfig(0) should be refused")
	}
}
