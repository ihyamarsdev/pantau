package osv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeOSV serves a querybatch endpoint and a vuln detail endpoint.
func fakeOSV(t *testing.T, vulnIDs map[string][]string, details map[string]vulnDetail) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Queries []osvQuery `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		out := osvBatchResponse{}
		for _, q := range payload.Queries {
			res := struct {
				Vulns []struct {
					ID string `json:"id"`
				} `json:"vulns"`
			}{}
			for _, id := range vulnIDs[q.Package.Name+"|"+q.Package.Ecosystem+"|"+q.Version] {
				res.Vulns = append(res.Vulns, struct {
					ID string `json:"id"`
				}{ID: id})
			}
			out.Results = append(out.Results, res)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/v1/vulns/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		d, ok := details[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d)
	})
	return httptest.NewServer(mux)
}

func TestEcosystemFor(t *testing.T) {
	tests := []struct {
		name       string
		osInfo     string
		wantSource string
		wantEco    string
		wantOK     bool
	}{
		{"debian", "Debian GNU/Linux 12 (bookworm)", SourceOSV, "Debian:12", true},
		{"debian 11", "Debian GNU/Linux 11 (bullseye)", SourceOSV, "Debian:11", true},
		{"debian unsupported", "Debian GNU/Linux 99 (ninetynine)", "", "", false},
		{"ubuntu", "Ubuntu 22.04.4 LTS", SourceOSV, "Ubuntu:22.04", true},
		{"ubuntu unsupported", "Ubuntu 99.04 LTS", "", "", false},
		{"alpine", "Alpine Linux v3.18", SourceOSV, "Alpine:v3.18", true},
		{"alpine unsupported", "Alpine Linux v9.9", "", "", false},
		{"arch", "Arch Linux", SourceArch, "Arch", true},
		{"rhel unsupported", "AlmaLinux 9.2 (Purple Manul)", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eco, ok, reason := EcosystemFor(tc.osInfo)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reason %q)", ok, tc.wantOK, reason)
			}
			if !ok {
				if reason == "" {
					t.Error("expected a reason for the unsupported host")
				}
				return
			}
			if eco.Source != tc.wantSource || eco.Name != tc.wantEco {
				t.Errorf("got %s/%s, want %s/%s", eco.Source, eco.Name, tc.wantSource, tc.wantEco)
			}
		})
	}
}

func TestCheckOSVChunking(t *testing.T) {
	// 1001 packages must be split into two batch requests (the endpoint accepts 1000).
	var batchSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/querybatch" {
			var payload struct {
				Queries []osvQuery `json:"queries"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			batchSizes = append(batchSizes, len(payload.Queries))
			out := osvBatchResponse{}
			for range payload.Queries {
				out.Results = append(out.Results, struct {
					Vulns []struct {
						ID string `json:"id"`
					} `json:"vulns"`
				}{})
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	pkgs := make([]Package, 1001)
	for i := range pkgs {
		pkgs[i] = Package{Name: "pkg", Version: "1.0"}
	}
	res, err := c.Check(context.Background(), Ecosystem{Source: SourceOSV, Name: "Debian:12"}, pkgs)
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if res.CheckedPackages != 1001 {
		t.Errorf("checked packages = %d, want 1001", res.CheckedPackages)
	}
	if len(batchSizes) != 2 || batchSizes[0] != 1000 || batchSizes[1] != 1 {
		t.Errorf("batch sizes = %v, want [1000 1]", batchSizes)
	}
}

func TestCheckOSVExtractsFixAndCVEs(t *testing.T) {
	detail := vulnDetail{
		ID:       "DEBIAN-CVE-2023-5678",
		Summary:  "openssl - security update",
		Upstream: []string{"CVE-2023-5678"},
		Severity: []severityEntry{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:L"}},
	}
	detail.Affected = append(detail.Affected, struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced   string `json:"introduced"`
				Fixed        string `json:"fixed"`
				LastAffected string `json:"last_affected"`
			} `json:"events"`
		} `json:"ranges"`
	}{})
	detail.Affected[0].Package.Name = "openssl"
	detail.Affected[0].Package.Ecosystem = "Debian:12"
	detail.Affected[0].Ranges = append(detail.Affected[0].Ranges, struct {
		Type   string `json:"type"`
		Events []struct {
			Introduced   string `json:"introduced"`
			Fixed        string `json:"fixed"`
			LastAffected string `json:"last_affected"`
		} `json:"events"`
	}{Type: "ECOSYSTEM"})
	detail.Affected[0].Ranges[0].Events = append(detail.Affected[0].Ranges[0].Events, struct {
		Introduced   string `json:"introduced"`
		Fixed        string `json:"fixed"`
		LastAffected string `json:"last_affected"`
	}{Introduced: "0", Fixed: "3.0.13-1~deb12u1"})

	srv := fakeOSV(t,
		map[string][]string{"openssl|Debian:12|3.0.10-1": {"DEBIAN-CVE-2023-5678"}},
		map[string]vulnDetail{"DEBIAN-CVE-2023-5678": detail},
	)
	defer srv.Close()

	c := New(srv.URL, "")
	res, err := c.Check(context.Background(), Ecosystem{Source: SourceOSV, Name: "Debian:12"},
		[]Package{{Name: "openssl", Version: "3.0.10-1"}})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(res.Findings))
	}
	f := res.Findings[0]
	if f.FixedVersion != "3.0.13-1~deb12u1" {
		t.Errorf("fixed version = %q, want 3.0.13-1~deb12u1", f.FixedVersion)
	}
	if len(f.CVEs) != 1 || f.CVEs[0] != "CVE-2023-5678" {
		t.Errorf("cves = %v, want [CVE-2023-5678]", f.CVEs)
	}
	if f.Severity == "" {
		t.Error("expected a severity")
	}
}

func TestCheckOSVFeedFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	res, err := c.Check(context.Background(), Ecosystem{Source: SourceOSV, Name: "Debian:12"},
		[]Package{{Name: "openssl", Version: "3.0.10-1"}})
	if err == nil {
		t.Fatal("expected an error when the feed fails")
	}
	if len(res.Findings) != 0 {
		t.Errorf("expected no findings on failure, got %d", len(res.Findings))
	}
}

func TestCheckArchMatching(t *testing.T) {
	feed := `[
	  {"name":"AVG-1","packages":["openssl"],"status":"Vulnerable","severity":"Critical","type":"denial of service","affected":"3.0.10-1","fixed":null,"issues":["CVE-2024-0001"]},
	  {"name":"AVG-2","packages":["curl"],"status":"Fixed","severity":"High","type":"code execution","affected":"8.2.1-1","fixed":"8.2.1-2","issues":["CVE-2024-0002"]},
	  {"name":"AVG-3","packages":["vim"],"status":"Not affected","severity":"Low","type":"x","affected":"9.0-1","fixed":"9.0-2","issues":["CVE-2024-0003"]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	c := New("http://unused", srv.URL)
	res, err := c.Check(context.Background(), Ecosystem{Source: SourceArch, Name: "Arch"},
		[]Package{
			{Name: "openssl", Version: "3.0.10-1"},
			{Name: "curl", Version: "8.2.1-1"},
			{Name: "curl", Version: "8.2.1-2"},
			{Name: "vim", Version: "9.0-1"},
		})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	// openssl matches exactly, curl matches the exact affected version, curl 8.2.1-2 is fixed,
	// vim is "Not affected".
	if len(res.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(res.Findings), res.Findings)
	}
	byPkg := map[string]Finding{}
	for _, f := range res.Findings {
		byPkg[f.Package] = f
	}
	if f, ok := byPkg["openssl"]; !ok || f.FixedVersion != "" || f.Severity != "critical" {
		t.Errorf("unexpected openssl finding: %+v", f)
	}
	if f, ok := byPkg["curl"]; !ok || f.FixedVersion != "8.2.1-2" || f.Severity != "high" {
		t.Errorf("unexpected curl finding: %+v", f)
	}
	if _, ok := byPkg["vim"]; ok {
		t.Error("vim is marked Not affected and must not be reported")
	}
}

func TestArchVersionAffected(t *testing.T) {
	tests := []struct {
		installed, affected, fixed string
		want                       bool
	}{
		{"8.2.1-1", "8.2.1-1", "8.2.1-2", true},  // exact affected version
		{"8.2.1-1", "8.2.0-1", "8.2.1-2", true},  // below the fixed version
		{"8.2.1-2", "8.2.0-1", "8.2.1-2", false}, // already fixed
		{"1.0-1", "1.0-1", "", true},             // vulnerable without a fix
		{"1.0-1", "2.0-1", "", false},            // different version, no fix known
	}
	for _, tc := range tests {
		got := archVersionAffected(tc.installed, tc.affected, tc.fixed)
		if got != tc.want {
			t.Errorf("archVersionAffected(%q, %q, %q) = %v, want %v",
				tc.installed, tc.affected, tc.fixed, got, tc.want)
		}
	}
}
