package teleport

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStatus(t *testing.T) {
	t.Parallel()

	const proxy = "teleport.setoranku.com:443"
	const cluster = "teleport.internal.verihubs.com"
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name    string
		raw     string
		cluster string
		want    Status
		wantErr bool
	}{
		{
			name:    "active profile matches proxy and cluster",
			raw:     `{"active":{"profile_url":"https://teleport.setoranku.com:443","cluster":"teleport.internal.verihubs.com","username":"alice","valid_until":"2030-01-02T03:04:05Z"},"profiles":[]}`,
			cluster: cluster,
			want:    Status{Active: true, Username: "alice", ProxyURL: "https://teleport.setoranku.com:443", Cluster: cluster, ValidUntil: until},
		},
		{
			name:    "profile found among others when active is another proxy",
			raw:     `{"active":{"profile_url":"https://other.example.com:443","cluster":"x","username":"bob","valid_until":"2030-01-02T03:04:05Z"},"profiles":[{"profile_url":"teleport.setoranku.com:443","cluster":"teleport.internal.verihubs.com","username":"alice","valid_until":"2030-01-02T03:04:05Z"}]}`,
			cluster: cluster,
			want:    Status{Active: true, Username: "alice", ProxyURL: "teleport.setoranku.com:443", Cluster: cluster, ValidUntil: until},
		},
		{
			name:    "cluster mismatch yields inactive status without error",
			raw:     `{"active":{"profile_url":"https://teleport.setoranku.com:443","cluster":"other-cluster","username":"alice","valid_until":"2030-01-02T03:04:05Z"}}`,
			cluster: cluster,
			want:    Status{},
		},
		{
			name: "no cluster filter accepts any cluster on the proxy",
			raw:  `{"active":{"profile_url":"https://teleport.setoranku.com","cluster":"any","username":"alice","valid_until":"2030-01-02T03:04:05Z"}}`,
			want: Status{Active: true, Username: "alice", ProxyURL: "https://teleport.setoranku.com", Cluster: "any", ValidUntil: until},
		},
		{
			name: "empty status document",
			raw:  `{"profiles":[]}`,
			want: Status{},
		},
		{
			name:    "invalid json",
			raw:     `{"active":`,
			wantErr: true,
		},
		{
			name:    "bad timestamp",
			raw:     `{"active":{"profile_url":"https://teleport.setoranku.com:443","valid_until":"yesterday"}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseStatus([]byte(tt.raw), proxy, tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseStatus() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStatusTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status Status
		want   time.Duration
	}{
		{name: "inactive has no ttl", status: Status{}, want: 0},
		{name: "one hour left", status: Status{Active: true, ValidUntil: now.Add(time.Hour)}, want: time.Hour},
		{name: "expired is negative", status: Status{Active: true, ValidUntil: now.Add(-time.Minute)}, want: -time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.TTL(now); got != tt.want {
				t.Fatalf("TTL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseKubeLS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    []KubeCluster
		wantErr bool
	}{
		{
			name: "map labels, sorted by name",
			raw:  `[{"kube_cluster_name":"prod-eu","labels":{"env":"prod"}},{"kube_cluster_name":"dev-1","labels":{"env":"dev"}}]`,
			want: []KubeCluster{
				{Name: "dev-1", Labels: map[string]string{"env": "dev"}},
				{Name: "prod-eu", Labels: map[string]string{"env": "prod"}},
			},
		},
		{
			name: "legacy name key and string labels",
			raw:  `[{"name":"staging","labels":"env=staging,region=eu"}]`,
			want: []KubeCluster{{Name: "staging", Labels: map[string]string{"env": "staging", "region": "eu"}}},
		},
		{
			name: "invalid names are dropped",
			raw:  `[{"kube_cluster_name":"-o"},{"kube_cluster_name":"ok"},{"kube_cluster_name":""}]`,
			want: []KubeCluster{{Name: "ok", Labels: map[string]string{}}},
		},
		{
			name: "duplicates are kept once",
			raw:  `[{"kube_cluster_name":"a"},{"kube_cluster_name":"a"}]`,
			want: []KubeCluster{{Name: "a", Labels: map[string]string{}}},
		},
		{name: "empty list", raw: `[]`, want: []KubeCluster{}},
		{name: "not an array", raw: `{"kube_cluster_name":"a"}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseKubeLS([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseKubeLS() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseKubeLS() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseCurrentContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "current context present in contexts",
			raw:  "apiVersion: v1\ncurrent-context: tc/prod-eu\ncontexts:\n- name: tc/dev\n- name: tc/prod-eu\n",
			want: "tc/prod-eu",
		},
		{
			name:    "current context missing from contexts",
			raw:     "current-context: tc/ghost\ncontexts:\n- name: tc/prod-eu\n",
			wantErr: true,
		},
		{
			name:    "invalid context name",
			raw:     "current-context: '-x'\ncontexts:\n- name: '-x'\n",
			wantErr: true,
		},
		{name: "empty document", raw: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCurrentContext([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseCurrentContext() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseCurrentContext() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseErrorsWrapNothingSecret(t *testing.T) {
	t.Parallel()

	// Parse errors are returned to the UI. They must not echo the token or certificate text that a
	// kubeconfig can contain.
	raw := []byte("current-context: tc/x\nusers:\n- user:\n    token: SECRET-TOKEN-VALUE\n  name: u\nclusters: [unterminated")
	_, err := ParseCurrentContext(raw)
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
	if errors.Is(err, ErrTeleportSessionExpired) {
		t.Fatal("a parse error must not be classified as an expired session")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN-VALUE") {
		t.Fatalf("parse error leaks kubeconfig content: %v", err)
	}
}

// FuzzTeleportOutputParsers feeds arbitrary bytes to every parser of tsh and kubeconfig output. The parsers may
// reject input, but they must never panic, and whatever they accept must already satisfy the allowlists.
func FuzzTeleportOutputParsers(f *testing.F) {
	f.Add([]byte(`{"active":{"profile_url":"https://a.example.com:443","cluster":"c","username":"u","valid_until":"2030-01-01T00:00:00Z"},"profiles":[]}`))
	f.Add([]byte(`[{"kube_cluster_name":"prod","labels":{"env":"prod"}},{"name":"dev","labels":"env=dev"}]`))
	f.Add([]byte("apiVersion: v1\ncurrent-context: tc/prod\ncontexts:\n- name: tc/prod\n"))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = ParseStatus(raw, "a.example.com:443", "c")

		clusters, err := ParseKubeLS(raw)
		if err == nil {
			for _, c := range clusters {
				if ValidateKubeName(c.Name) != nil {
					t.Fatalf("ParseKubeLS accepted invalid name %q", c.Name)
				}
			}
		}

		if name, err := ParseCurrentContext(raw); err == nil {
			if ValidateContextName(name) != nil {
				t.Fatalf("ParseCurrentContext accepted invalid name %q", name)
			}
		}
	})
}

// Shapes captured from tsh v18.11.10 (`tsh status --format=json`, `tsh kube ls --format=json`). Values are
// placeholders; the field names and types are the real ones.
func TestParseRealTshShapes(t *testing.T) {
	t.Parallel()

	status := []byte(`{"active":{"profile_url":"https://teleport.example.com:443","username":"user@example.com",
	  "cluster":"teleport.example.com","roles":["kube-view"],"kubernetes_enabled":true,
	  "valid_until":"2026-11-27T21:14:07+07:00","extensions":["permit-pty"]}}`)
	st, err := ParseStatus(status, "teleport.example.com:443", "teleport.example.com")
	if err != nil || !st.Active || st.Cluster != "teleport.example.com" {
		t.Fatalf("ParseStatus(real shape) = %+v, %v", st, err)
	}
	if st.ValidUntil.IsZero() {
		t.Fatal("valid_until with a UTC offset was not parsed")
	}

	kube := []byte(`[{"kube_cluster_name":"prod-eks","labels":{"cloud_provider":"aws","env":"production"},"selected":false,"scope":""},
	  {"kube_cluster_name":"staging-gke","labels":{"cloud_provider":"gcp","env":"staging"},"selected":true,"scope":""}]`)
	clusters, err := ParseKubeLS(kube)
	if err != nil || len(clusters) != 2 {
		t.Fatalf("ParseKubeLS(real shape) = %+v, %v", clusters, err)
	}
	if got := SuggestEnvBadge(clusters[0].Name, clusters[0].Labels); got != "prod" {
		t.Fatalf("badge for production label = %q, want prod", got)
	}
	if got := SuggestEnvBadge(clusters[1].Name, clusters[1].Labels); got != "staging" {
		t.Fatalf("badge for staging label = %q, want staging", got)
	}
}
