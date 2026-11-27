package teleport

import (
	"errors"
	"testing"
)

func TestValidateProfile(t *testing.T) {
	t.Parallel()

	valid := Profile{
		ID:              1,
		Proxy:           "teleport.setoranku.com:443",
		AuthConnector:   "local",
		Username:        "alice@example.com",
		TeleportCluster: "teleport.internal.verihubs.com",
	}

	tests := []struct {
		name    string
		mutate  func(p *Profile)
		wantErr bool
	}{
		{name: "valid profile", mutate: func(*Profile) {}},
		{name: "empty optional fields are allowed", mutate: func(p *Profile) {
			p.AuthConnector, p.Username, p.TeleportCluster = "", "", ""
		}},
		{name: "proxy without port", mutate: func(p *Profile) { p.Proxy = "teleport.example.com" }},
		{name: "proxy required", mutate: func(p *Profile) { p.Proxy = "" }, wantErr: true},
		{name: "proxy flag injection", mutate: func(p *Profile) { p.Proxy = "-oProxyCommand=evil" }, wantErr: true},
		{name: "proxy with space", mutate: func(p *Profile) { p.Proxy = "a.example.com x" }, wantErr: true},
		{name: "proxy shell metacharacters", mutate: func(p *Profile) { p.Proxy = "a.example.com;id" }, wantErr: true},
		{name: "proxy command substitution", mutate: func(p *Profile) { p.Proxy = "$(id).example.com" }, wantErr: true},
		{name: "proxy port out of shape", mutate: func(p *Profile) { p.Proxy = "a.example.com:99999999" }, wantErr: true},
		{name: "connector uppercase", mutate: func(p *Profile) { p.AuthConnector = "LOCAL" }, wantErr: true},
		{name: "connector flag injection", mutate: func(p *Profile) { p.AuthConnector = "--auth=x" }, wantErr: true},
		{name: "username flag injection", mutate: func(p *Profile) { p.Username = "-u" }, wantErr: true},
		{name: "username backticks", mutate: func(p *Profile) { p.Username = "a`id`" }, wantErr: true},
		{name: "cluster flag injection", mutate: func(p *Profile) { p.TeleportCluster = "-x" }, wantErr: true},
		{name: "cluster path traversal", mutate: func(p *Profile) { p.TeleportCluster = "../etc" }, wantErr: true},
		{name: "home relative path", mutate: func(p *Profile) { p.TeleportHome = "tsh-home" }, wantErr: true},
		{name: "home traversal", mutate: func(p *Profile) { p.TeleportHome = "/srv/../../etc" }, wantErr: true},
		{name: "home tilde traversal", mutate: func(p *Profile) { p.TeleportHome = "~/../../etc" }, wantErr: true},
		{name: "home absolute", mutate: func(p *Profile) { p.TeleportHome = "/home/alice/.tsh-staging" }},
		{name: "home tilde", mutate: func(p *Profile) { p.TeleportHome = "~/.tsh-staging" }},
		{name: "home newline", mutate: func(p *Profile) { p.TeleportHome = "/home/alice\n" }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := valid
			tt.mutate(&p)
			err := ValidateProfile(p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateProfile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error %v does not wrap ErrInvalidInput", err)
			}
		})
	}
}

func TestValidateKubeAndContextNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		kube    bool
		wantErr bool
	}{
		{name: "kube plain", value: "prod-eu-1", kube: true},
		{name: "kube dotted", value: "k8s.internal", kube: true},
		{name: "kube empty", value: "", kube: true, wantErr: true},
		{name: "kube leading dash", value: "-o", kube: true, wantErr: true},
		{name: "kube slash rejected", value: "a/b", kube: true, wantErr: true},
		{name: "context with slash", value: "teleport.internal/prod-eu", kube: false},
		{name: "context with colon", value: "tc:prod", kube: false},
		{name: "context leading dash", value: "-ctx", kube: false, wantErr: true},
		{name: "context whitespace", value: "a b", kube: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tt.kube {
				err = ValidateKubeName(tt.value)
			} else {
				err = ValidateContextName(tt.value)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolveHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, home, userHome, want string
	}{
		{name: "tilde expands", home: "~/.tsh-prod", userHome: "/home/alice", want: "/home/alice/.tsh-prod"},
		{name: "absolute kept clean", home: "/srv//tsh/./a", userHome: "/home/alice", want: "/srv/tsh/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveHome(tt.home, tt.userHome); got != tt.want {
				t.Fatalf("ResolveHome() = %q, want %q", got, tt.want)
			}
		})
	}
}
