package inspector

import (
	"testing"

	"pantau/internal/store"
)

func TestDetectKernelDrift(t *testing.T) {
	cases := []struct {
		name       string
		running    string
		installed  string
		wantReboot bool
	}{
		{"patched kernel installed not booted", "5.15.0-91-generic", "5.15.0-97-generic", true},
		{"running latest", "6.5.0-15-generic", "6.5.0-15-generic", false},
		{"running newer than recorded", "6.6.0-1", "6.5.0-1", false},
		{"missing installed", "5.15.0-91-generic", "", false},
		{"missing running", "", "5.15.0-97-generic", false},
		{"nothing", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DetectKernelDrift(tc.running, tc.installed)
			if tc.running == "" && tc.installed == "" {
				if d != nil {
					t.Fatalf("expected nil for empty versions, got %+v", d)
				}
				return
			}
			if d == nil {
				t.Fatal("expected a drift result")
			}
			if d.RebootRequired != tc.wantReboot {
				t.Errorf("reboot required = %v, want %v (note: %s)", d.RebootRequired, tc.wantReboot, d.Note)
			}
			if tc.wantReboot && d.Note == "" {
				t.Error("reboot finding must carry an explanatory note")
			}
		})
	}
}

func TestParseKernelVersions(t *testing.T) {
	out := "KERNEL:RUNNING:5.15.0-91-generic\nKERNEL:INSTALLED:5.15.0-97-generic\n"
	r, i := ParseKernelVersions(out)
	if r != "5.15.0-91-generic" || i != "5.15.0-97-generic" {
		t.Errorf("versions = (%q,%q)", r, i)
	}
}

func TestPublicBind(t *testing.T) {
	cases := []struct {
		addr string
		pub  bool
	}{
		{"0.0.0.0:80", true},
		{"*:443", true},
		{"[::]:8080", true},
		{"127.0.0.1:3306", false},
		{"10.0.0.5:22", false},
	}
	for _, tc := range cases {
		got := PublicBind(store.ListeningPort{Address: tc.addr})
		if got != tc.pub {
			t.Errorf("PublicBind(%q) = %v, want %v", tc.addr, got, tc.pub)
		}
	}
}

func TestElevateSeverity(t *testing.T) {
	if got := ElevateSeverity("medium", store.Reachability{PublicBind: true}); got != store.ExposureCriticalExposed {
		t.Errorf("public medium should elevate to CRITICAL_EXPOSED, got %q", got)
	}
	if got := ElevateSeverity("high", store.Reachability{CloudflareProxied: true}); got != store.ExposureCriticalExposed {
		t.Errorf("CF-proxied high should elevate to CRITICAL_EXPOSED, got %q", got)
	}
	if got := ElevateSeverity("high", store.Reachability{}); got != "high" {
		t.Errorf("internal high should stay high, got %q", got)
	}
	if got := ElevateSeverity("something", store.Reachability{}); got != store.ExposureUnknown {
		t.Errorf("unknown base should stay unknown, got %q", got)
	}
}

func TestCorrelateReachability(t *testing.T) {
	host := &store.Host{ListeningPorts: `[{"proto":"tcp","port":"80","address":"0.0.0.0:80","public":true,"process":"nginx"},{"proto":"tcp","port":"3306","address":"127.0.0.1:3306","public":false,"process":"mysqld"}]`}
	domains := []store.WebSite{{Domain: "app.example.com", Upstreams: []string{"127.0.0.1:9000"}, Listen: []string{"80"}}}
	cf := map[string]bool{"app.example.com": true}

	r := CorrelateReachability(host, "nginx", []string{"80"}, domains, cf)
	if !r.PublicBind {
		t.Error("nginx on 0.0.0.0:80 should be public")
	}
	if !r.CloudflareProxied {
		t.Error("matching domain is Cloudflare-proxied")
	}
	if len(r.Domains) != 1 || r.Domains[0] != "app.example.com" {
		t.Errorf("domains = %v", r.Domains)
	}
	// A local-only component must not be public.
	r2 := CorrelateReachability(host, "mysqld", []string{"3306"}, nil, nil)
	if r2.PublicBind {
		t.Error("127.0.0.1:3306 should not be public")
	}
}
