package store

import (
	"encoding/json"
	"strings"
	"testing"
)

// A snapshot carries Teleport profiles and the metadata of Teleport clusters (favorite, env badge, policy, context
// name). It carries no certificate or kubeconfig content, because none of that is stored in the database.
func TestSnapshotRoundTripsTeleportProfilesAndClusterMetadata(t *testing.T) {
	t.Parallel()
	src := openTestDB(t)

	pid, err := src.CreateTeleportProfile(&TeleportProfile{
		Name: "prod", Proxy: "teleport.setoranku.com:443", AuthConnector: "oidc", Username: "alice",
		TeleportCluster: "teleport.internal.verihubs.com", TeleportHome: "/srv/tsh-prod", DefaultPolicy: "no-exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	cid, err := src.CreateKubeCluster(&KubeCluster{
		Name: "prod-eu", KubeMode: "no-exec", SourceType: KubeSourceTeleport, TeleportProfileID: pid,
		TeleportKubeName: "prod-eu", EnvBadge: "prod", IsFavorite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := src.SetKubeClusterContextName(cid, "teleport.internal.verihubs.com/prod-eu"); err != nil {
		t.Fatal(err)
	}

	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatalf("ExportSnapshot() error = %v", err)
	}
	if len(payload.TeleportProfiles) != 1 {
		t.Fatalf("snapshot has %d teleport profiles, want 1", len(payload.TeleportProfiles))
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "BEGIN CERTIFICATE") || strings.Contains(string(raw), "client-key-data") {
		t.Fatal("snapshot JSON contains certificate or key material")
	}

	dst := openTestDB(t)
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatalf("ImportSnapshot() error = %v", err)
	}

	p, err := dst.GetTeleportProfile(pid)
	if err != nil || p == nil {
		t.Fatalf("restored profile = %v, %v", p, err)
	}
	if p.Name != "prod" || p.AuthConnector != "oidc" || p.TeleportHome != "/srv/tsh-prod" || p.DefaultPolicy != "no-exec" {
		t.Fatalf("restored profile = %+v", p)
	}

	c, err := dst.GetKubeCluster(cid)
	if err != nil || c == nil {
		t.Fatalf("restored cluster = %v, %v", c, err)
	}
	if c.SourceType != KubeSourceTeleport || c.TeleportProfileID != pid || c.EnvBadge != "prod" || !c.IsFavorite {
		t.Fatalf("restored cluster metadata = %+v", c)
	}
	if c.KubeMode != "no-exec" || c.KubeContextName != "teleport.internal.verihubs.com/prod-eu" {
		t.Fatalf("restored policy/context = %+v", c)
	}
}

// A snapshot made before the Teleport integration has no Teleport fields. Its clusters restore as kubeconfig clusters.
func TestSnapshotWithoutTeleportFieldsRestoresAsKubeconfigClusters(t *testing.T) {
	t.Parallel()
	payload := &SnapshotPayload{
		Version: 1,
		KubeClusters: []SnapshotKubeCluster{
			{ID: 5, Name: "legacy", KubeMode: "full", Provider: "generic", Kubeconfig: "apiVersion: v1"},
		},
	}
	dst := openTestDB(t)
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatalf("ImportSnapshot() error = %v", err)
	}
	c, err := dst.GetKubeCluster(5)
	if err != nil || c == nil {
		t.Fatalf("restored cluster = %v, %v", c, err)
	}
	if c.SourceType != KubeSourceKubeconfig || c.EnvBadge != "" || c.IsFavorite {
		t.Fatalf("legacy cluster = %+v, want kubeconfig source and no metadata", c)
	}
}
