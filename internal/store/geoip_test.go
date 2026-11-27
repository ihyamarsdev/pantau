package store

import (
	"testing"
	"time"
)

func openGeoTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGeoIPCacheRoundTrip(t *testing.T) {
	t.Parallel()
	db := openGeoTestDB(t)
	now := time.Now()

	// Missing entry.
	e, fresh, err := db.GetGeoIP("198.51.100.10", now)
	if err != nil || e != nil || fresh {
		t.Fatalf("fresh db should have no geoip entry, got %+v fresh=%v err=%v", e, fresh, err)
	}

	// Upsert + read back.
	want := GeoIPEntry{
		IP: "198.51.100.10", Lat: -6.2, Lon: 106.8, City: "Jakarta",
		CountryCode: "ID", ASNOrg: "Example ISP", Source: "geoip",
	}
	if err := db.UpsertGeoIP(want, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	e, fresh, err = db.GetGeoIP(want.IP, now)
	if err != nil || e == nil {
		t.Fatalf("get after upsert: %+v err=%v", e, err)
	}
	if !fresh {
		t.Fatal("entry just written should be fresh")
	}
	if e.Lat != want.Lat || e.Lon != want.Lon || e.City != want.City || e.CountryCode != want.CountryCode || e.ASNOrg != want.ASNOrg {
		t.Fatalf("round-trip mismatch: got %+v want %+v", e, want)
	}

	// Stale after TTL.
	e, fresh, err = db.GetGeoIP(want.IP, now.Add(GeoIPCacheTTL+time.Hour))
	if err != nil || e == nil || fresh {
		t.Fatalf("entry past TTL should be stale but present: %+v fresh=%v err=%v", e, fresh, err)
	}

	// Upsert resets the TTL.
	if err := db.UpsertGeoIP(want, now.Add(GeoIPCacheTTL+time.Hour)); err != nil {
		t.Fatalf("refresh upsert: %v", err)
	}
	_, fresh, err = db.GetGeoIP(want.IP, now.Add(GeoIPCacheTTL+time.Hour))
	if err != nil || !fresh {
		t.Fatalf("refreshed entry should be fresh: fresh=%v err=%v", fresh, err)
	}
}

func TestDeleteStaleGeoIP(t *testing.T) {
	t.Parallel()
	db := openGeoTestDB(t)
	now := time.Now()
	_ = db.UpsertGeoIP(GeoIPEntry{IP: "1.1.1.1", Source: "geoip"}, now.Add(-40*24*time.Hour))
	_ = db.UpsertGeoIP(GeoIPEntry{IP: "2.2.2.2", Source: "geoip"}, now)

	if err := db.DeleteStaleGeoIP(now); err != nil {
		t.Fatalf("delete stale: %v", err)
	}
	if e, _, _ := db.GetGeoIP("1.1.1.1", now); e != nil {
		t.Fatal("stale entry should be deleted")
	}
	if e, _, _ := db.GetGeoIP("2.2.2.2", now); e == nil {
		t.Fatal("fresh entry should survive")
	}
}

func TestHostGeoOverrideRoundTrip(t *testing.T) {
	t.Parallel()
	db := openGeoTestDB(t)
	id, err := db.CreateHost(&Host{Name: "airgap", Host: "10.0.0.5"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := db.UpdateHostGeo(id, -7.25, 112.75, "Jawa Timur", "Surabaya DC"); err != nil {
		t.Fatalf("update geo: %v", err)
	}
	h, err := db.GetHost(id)
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if h.GeoLat != -7.25 || h.GeoLon != 112.75 || h.GeoRegion != "Jawa Timur" || h.GeoLabel != "Surabaya DC" {
		t.Fatalf("geo override not persisted: %+v", h)
	}
}

func TestProbeResultRoundTrip(t *testing.T) {
	t.Parallel()
	db := openGeoTestDB(t)
	now := time.Now()
	p, err := db.GetProbeLatency(0, 42)
	if err != nil || p != nil {
		t.Fatalf("fresh db should have no probe result: %+v err=%v", p, err)
	}
	if err := db.UpsertProbeResult(ProbeResult{SourceHostID: 0, TargetHostID: 42, LatencyMs: 18.5, Status: "healthy"}, now); err != nil {
		t.Fatalf("upsert probe: %v", err)
	}
	p, err = db.GetProbeLatency(0, 42)
	if err != nil || p == nil || p.LatencyMs != 18.5 || p.Status != "healthy" {
		t.Fatalf("probe round-trip mismatch: %+v err=%v", p, err)
	}
}
