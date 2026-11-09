package vulnintel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const kevFixture = `{"dateReleased":"2026-11-06T19:44:45Z","vulnerabilities":[
{"cveID":"CVE-2024-6387","vulnerabilityName":"OpenSSH regreSSHion","dateAdded":"2026-01-10","dueDate":"2026-01-31","knownRansomwareCampaignUse":"Unknown"},
{"cveID":"CVE-2023-4911","vulnerabilityName":"glibc Looney Tunables","dateAdded":"2023-11-21","dueDate":"2023-12-12","knownRansomwareCampaignUse":"Known"}]}`

type feeds struct {
	srv        *httptest.Server
	kevHits    int64
	kevFail    atomic.Bool
	epssHits   int64
	epssMaxLen int64
}

func fakeFeeds(t *testing.T) *feeds {
	t.Helper()
	f := &feeds{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/kev":
			atomic.AddInt64(&f.kevHits, 1)
			if f.kevFail.Load() {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(kevFixture))
		case "/epss":
			atomic.AddInt64(&f.epssHits, 1)
			ids := strings.Split(r.URL.Query().Get("cve"), ",")
			if n := int64(len(ids)); n > atomic.LoadInt64(&f.epssMaxLen) {
				atomic.StoreInt64(&f.epssMaxLen, n)
			}
			var data []map[string]string
			for _, id := range ids {
				if id == "CVE-2024-6387" {
					data = append(data, map[string]string{"cve": id, "epss": "0.995060000", "percentile": "0.999450000", "date": "2026-11-08"})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestLookup(t *testing.T) {
	f := fakeFeeds(t)
	c := New(f.srv.URL+"/kev", f.srv.URL+"/epss")
	cves := []string{"CVE-2024-6387", "CVE-2023-4911", "RHSA-2026:1", "CVE-2024-6387"}
	for i := range 150 {
		cves = append(cves, fmt.Sprintf("CVE-2026-%05d", i))
	}
	res := c.Lookup(context.Background(), cves)

	if res.CheckedCVEs != 152 || res.KEVError != "" || res.EPSSError != "" || res.KEVCatalog == "" {
		t.Fatalf("result header = %+v", res)
	}
	ssh, glibc := res.CVEs["CVE-2024-6387"], res.CVEs["CVE-2023-4911"]
	if ssh.KEV == nil || ssh.EPSS == nil || *ssh.EPSS < 0.99 || ssh.KEV.Ransomware {
		t.Errorf("CVE-2024-6387 = %+v", ssh)
	}
	if glibc.KEV == nil || !glibc.KEV.Ransomware || glibc.EPSS != nil {
		t.Errorf("CVE-2023-4911 must be in KEV with ransomware use and no EPSS score: %+v", glibc)
	}
	if len(res.CVEs) != 2 {
		t.Errorf("only CVEs with intelligence are listed, got %d", len(res.CVEs))
	}
	if f.epssMaxLen > epssBatch || f.epssHits != 2 {
		t.Errorf("EPSS asked %d times with up to %d CVEs, want 2 batches of at most %d", f.epssHits, f.epssMaxLen, epssBatch)
	}

	// Within a day nothing is downloaded again.
	c.Lookup(context.Background(), cves)
	if f.kevHits != 1 || f.epssHits != 2 {
		t.Errorf("cached lookup downloaded again: kev=%d epss=%d", f.kevHits, f.epssHits)
	}
}

// A failed daily refresh keeps showing the last good catalog (with the error), and is not retried on every view.
func TestLookupKeepsLastCatalogWhenRefreshFails(t *testing.T) {
	f := fakeFeeds(t)
	c := New(f.srv.URL+"/kev", f.srv.URL+"/epss")
	c.Lookup(context.Background(), []string{"CVE-2023-4911"})

	f.kevFail.Store(true)
	c.mu.Lock()
	c.kevAt = time.Now().Add(-25 * time.Hour)
	c.mu.Unlock()
	res := c.Lookup(context.Background(), []string{"CVE-2023-4911"})
	if res.CVEs["CVE-2023-4911"].KEV == nil || !strings.Contains(res.KEVError, "HTTP 502") {
		t.Errorf("stale catalog must still answer and report the failure: %+v", res)
	}
	c.Lookup(context.Background(), []string{"CVE-2023-4911"})
	if f.kevHits != 2 {
		t.Errorf("KEV downloaded %d times, want 2 (no retry right after a failure)", f.kevHits)
	}
}

func TestLookupWithoutCVEsStaysOffline(t *testing.T) {
	f := fakeFeeds(t)
	c := New(f.srv.URL+"/kev", f.srv.URL+"/epss")
	if res := c.Lookup(context.Background(), []string{"", "RHSA-2026:1"}); res.CheckedCVEs != 0 || f.kevHits+f.epssHits != 0 {
		t.Errorf("no CVE IDs must mean no download: %+v kev=%d epss=%d", res, f.kevHits, f.epssHits)
	}
}
