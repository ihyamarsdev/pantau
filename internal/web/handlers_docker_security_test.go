package web

import (
	"testing"

	"pantau/internal/store"
)

func TestIsCVECode(t *testing.T) {
	for _, id := range []string{"CVE-2024-0001", "CVE-2026-8932"} {
		if !isCVECode(id) {
			t.Errorf("%q must be a CVE id", id)
		}
	}
	for _, id := range []string{"", "CVE", "GHSA-xxxx", "CVE-2024", "CVE-2024-0001-EXTRA-LONG", "cve-2024-0001"} {
		if isCVECode(id) {
			t.Errorf("%q must not be a CVE id", id)
		}
	}
}

func TestDockerSecCVEs(t *testing.T) {
	scan := &store.DockerSecurityScan{
		Vulns: []store.DockerImageVuln{
			{ID: "CVE-2024-0001"},
			{ID: "CVE-2024-0001"},
			{ID: "GHSA-xxxx"},
			{ID: "CVE-2024-0002"},
		},
	}
	cves := dockerSecCVEs(scan)
	if len(cves) != 2 {
		t.Fatalf("cves = %v", cves)
	}
	if cves[0] != "CVE-2024-0001" || cves[1] != "CVE-2024-0002" {
		t.Errorf("cves = %v", cves)
	}
}
