package teleport

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	testProxy   = "teleport.setoranku.com:443"
	testCluster = "teleport.internal.verihubs.com"
)

func TestParseLoginCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		profile  Profile
		wantName string // NameSuggestion
		ignored  []string
	}{
		{
			name:     "exact Web UI line with equals flags and trailing cluster",
			raw:      "tsh login --proxy=teleport.setoranku.com:443 --auth=local --user=i.rakasurya teleport.internal.verihubs.com",
			profile:  Profile{Proxy: testProxy, AuthConnector: "local", Username: "i.rakasurya", TeleportCluster: testCluster},
			wantName: testCluster,
		},
		{
			name:     "space-separated flag values",
			raw:      "tsh login --proxy teleport.example.com:443 --auth local --user alice --cluster tc",
			profile:  Profile{Proxy: "teleport.example.com:443", AuthConnector: "local", Username: "alice", TeleportCluster: "tc"},
			wantName: "tc",
		},
		{
			name:     "short user flag in both forms",
			raw:      "tsh login -u=bob --proxy=p.example.com:443",
			profile:  Profile{Proxy: "p.example.com:443", Username: "bob"},
			wantName: "p.example.com",
		},
		{
			name:     "short user flag with separate value",
			raw:      "tsh login -u carol --proxy p.example.com",
			profile:  Profile{Proxy: "p.example.com", Username: "carol"},
			wantName: "p.example.com",
		},
		{
			name:     "cluster positional before flags",
			raw:      "tsh login teleport.internal.example.com --proxy=p.example.com:443 --user=dave",
			profile:  Profile{Proxy: "p.example.com:443", Username: "dave", TeleportCluster: "teleport.internal.example.com"},
			wantName: "teleport.internal.example.com",
		},
		{
			name:     "quoted values with single and double quotes",
			raw:      `tsh login --proxy="teleport.example.com:443" --user='eve' 'cluster.example.com'`,
			profile:  Profile{Proxy: "teleport.example.com:443", Username: "eve", TeleportCluster: "cluster.example.com"},
			wantName: "cluster.example.com",
		},
		{
			name:     "TELEPORT_HOME prefix on the tsh line",
			raw:      "TELEPORT_HOME=~/.tsh-prod tsh login --proxy=p.example.com:443 --user=eve",
			profile:  Profile{Proxy: "p.example.com:443", Username: "eve", TeleportHome: "~/.tsh-prod"},
			wantName: "p.example.com",
		},
		{
			name: "multi-line Web UI paste keeps only the tsh login line",
			raw: "Step 1: Log in to Teleport\n" +
				"$ export TELEPORT_HOME=/home/ops/.tsh-prod\n" +
				"$ tsh login --proxy=teleport.setoranku.com:443 --auth=local --user=i.rakasurya \\\n" +
				"    teleport.internal.verihubs.com\n" +
				"Step 2: Access the cluster\n" +
				"$ export KUBECONFIG=/tmp/kube.yaml\n" +
				"$ tsh kube login prod\n" +
				"$ kubectl get pods\n",
			profile: Profile{
				Proxy: testProxy, AuthConnector: "local", Username: "i.rakasurya",
				TeleportCluster: testCluster, TeleportHome: "/home/ops/.tsh-prod",
			},
			wantName: testCluster,
		},
		{
			name:     "CRLF line endings and a continuation with flags",
			raw:      "tsh login \\\r\n--proxy=p.example.com:443 \\\r\n--user=frank\r\n",
			profile:  Profile{Proxy: "p.example.com:443", Username: "frank"},
			wantName: "p.example.com",
		},
		{
			name:     "ignored Teleport flags are reported, not mapped",
			raw:      "tsh login --proxy=p.example.com:443 --browser=none --ttl=8h --mfa-mode=auto --insecure --user=alice",
			profile:  Profile{Proxy: "p.example.com:443", Username: "alice"},
			wantName: "p.example.com",
			ignored:  []string{"browser", "ttl", "mfa-mode", "insecure"},
		},
		{
			name:     "prompt-prefixed line with no other text",
			raw:      "$ tsh login --proxy=p.example.com:443",
			profile:  Profile{Proxy: "p.example.com:443"},
			wantName: "p.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLoginCommand(tt.raw)
			if err != nil {
				t.Fatalf("ParseLoginCommand() error = %v", err)
			}
			if !reflect.DeepEqual(got.Profile, tt.profile) {
				t.Errorf("Profile = %+v, want %+v", got.Profile, tt.profile)
			}
			if got.NameSuggestion != tt.wantName {
				t.Errorf("NameSuggestion = %q, want %q", got.NameSuggestion, tt.wantName)
			}
			if len(got.IgnoredFlags) != len(tt.ignored) || (len(tt.ignored) > 0 && !reflect.DeepEqual(got.IgnoredFlags, tt.ignored)) {
				t.Errorf("IgnoredFlags = %v, want %v", got.IgnoredFlags, tt.ignored)
			}
		})
	}
}

func TestParseLoginCommandRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty input", raw: ""},
		{name: "no tsh login line", raw: "Step 1: open the Web UI\nkubectl get pods"},
		{name: "semicolon chaining", raw: "tsh login --proxy=p.example.com:443; rm -rf /"},
		{name: "and-and chaining", raw: "tsh login --proxy=p.example.com:443 && id"},
		{name: "or-or chaining", raw: "tsh login --proxy=p.example.com:443 || id"},
		{name: "pipe", raw: "tsh login --proxy=p.example.com:443 | sh"},
		{name: "backtick substitution", raw: "tsh login --proxy=`id`:443"},
		{name: "dollar-paren substitution", raw: "tsh login --proxy=$(id):443"},
		{name: "redirect", raw: "tsh login --proxy=p.example.com:443 --user=a > /tmp/x"},
		{name: "shell metachar inside quotes", raw: `tsh login --proxy=p.example.com:443 --user="a;b"`},
		{name: "flag value starting with dash", raw: "tsh login --proxy -evil"},
		{name: "user value starting with dash", raw: "tsh login --proxy=p.example.com:443 --user=-x"},
		{name: "cluster value starting with dash", raw: "tsh login --proxy=p.example.com:443 --cluster=-x"},
		{name: "unknown short flag", raw: "tsh login --proxy=p.example.com:443 -x"},
		{name: "unknown long flag", raw: "tsh login --proxy=p.example.com:443 --unknown=1"},
		{name: "missing value at end", raw: "tsh login --proxy"},
		{name: "empty value", raw: "tsh login --proxy="},
		{name: "duplicate flag", raw: "tsh login --proxy=p.example.com:443 --auth=local --auth=oidc"},
		{name: "cluster given twice", raw: "tsh login --proxy=p.example.com:443 --cluster=a b"},
		{name: "proxy missing", raw: "tsh login --user=alice"},
		{name: "unterminated quote", raw: `tsh login --proxy=p.example.com:443 --user="alice`},
		{name: "TELEPORT_HOME with traversal", raw: "TELEPORT_HOME=/home/x/../../etc tsh login --proxy=p.example.com:443"},
		{name: "relative TELEPORT_HOME", raw: "export TELEPORT_HOME=relative/path\ntsh login --proxy=p.example.com:443"},
		{name: "conflicting TELEPORT_HOME", raw: "export TELEPORT_HOME=/a\nTELEPORT_HOME=/b tsh login --proxy=p.example.com:443"},
		{name: "unsupported environment variable", raw: "PATH=/x tsh login --proxy=p.example.com:443"},
		{name: "two tsh login commands", raw: "tsh login --proxy=a.example.com\ntsh login --proxy=b.example.com"},
		{name: "control character", raw: "tsh login --proxy=p.example.com\x00"},
		{name: "oversized input", raw: strings.Repeat("a", maxLoginCommandBytes+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLoginCommand(tt.raw)
			if err == nil {
				t.Fatalf("ParseLoginCommand() = %+v, want error", got)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
			if tt.raw != "" && strings.Contains(err.Error(), tt.raw) {
				t.Errorf("error echoes the raw command: %v", err)
			}
		})
	}
}

func FuzzParseLoginCommand(f *testing.F) {
	f.Add("tsh login --proxy=teleport.setoranku.com:443 --auth=local --user=i.rakasurya teleport.internal.verihubs.com")
	f.Add("tsh login --proxy p.example.com -u alice --cluster tc")
	f.Add("TELEPORT_HOME=~/.tsh tsh login --proxy='p:443' \\\n  cluster")
	f.Add("tsh login --proxy=p; rm -rf /")
	f.Add("")

	f.Fuzz(func(t *testing.T, raw string) {
		cmd, err := ParseLoginCommand(raw)
		if err != nil {
			return
		}
		// Anything accepted must already pass the profile allowlists the runner relies on.
		if verr := ValidateProfile(cmd.Profile); verr != nil {
			t.Fatalf("accepted command fails ValidateProfile: %v", verr)
		}
	})
}
