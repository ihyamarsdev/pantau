package store

import (
	"reflect"
	"sort"
	"testing"
)

func TestSyncTeleportClusters(t *testing.T) {
	t.Parallel()

	t.Run("first sync adds clusters with the default policy and suggested badge", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		pid, err := db.CreateTeleportProfile(newTeleportProfile("p"))
		if err != nil {
			t.Fatal(err)
		}

		res, err := db.SyncTeleportClusters(pid, "no-exec", []TeleportSyncCluster{
			{Name: "prod-eu", EnvBadge: "prod"},
			{Name: "sandbox", EnvBadge: ""},
		})
		if err != nil {
			t.Fatalf("SyncTeleportClusters() error = %v", err)
		}
		if !reflect.DeepEqual(res.Added, []string{"prod-eu", "sandbox"}) {
			t.Fatalf("Added = %v", res.Added)
		}
		ids, _ := db.TeleportClusterIDs(pid)
		c, err := db.GetKubeCluster(ids["prod-eu"])
		if err != nil || c == nil {
			t.Fatalf("prod-eu not stored: %v", err)
		}
		if c.KubeMode != "no-exec" || c.EnvBadge != "prod" || c.SourceType != KubeSourceTeleport || c.TeleportProfileID != pid {
			t.Fatalf("new cluster = %+v", c)
		}
	})

	t.Run("resync keeps metadata and context, removes stale, skips name clashes", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		pid, err := db.CreateTeleportProfile(newTeleportProfile("p"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.SyncTeleportClusters(pid, "read-only", []TeleportSyncCluster{
			{Name: "prod-eu", EnvBadge: "prod"}, {Name: "old-one"},
		}); err != nil {
			t.Fatal(err)
		}
		ids, _ := db.TeleportClusterIDs(pid)
		if err := db.UpdateKubeClusterMeta(ids["prod-eu"], true, "staging", "full"); err != nil {
			t.Fatal(err)
		}
		if err := db.SetKubeClusterContextName(ids["prod-eu"], "tc/prod-eu"); err != nil {
			t.Fatal(err)
		}
		// A cluster from another source that already owns the name "fresh".
		if _, err := db.CreateKubeCluster(&KubeCluster{Name: "fresh", KubeMode: "full", Kubeconfig: "apiVersion: v1"}); err != nil {
			t.Fatal(err)
		}

		res, err := db.SyncTeleportClusters(pid, "read-only", []TeleportSyncCluster{
			{Name: "prod-eu", EnvBadge: "dev"}, {Name: "fresh"},
		})
		if err != nil {
			t.Fatalf("SyncTeleportClusters() error = %v", err)
		}
		if !reflect.DeepEqual(res.Kept, []string{"prod-eu"}) ||
			!reflect.DeepEqual(res.Removed, []string{"old-one"}) ||
			!reflect.DeepEqual(res.Skipped, []string{"fresh"}) ||
			len(res.Added) != 0 {
			t.Fatalf("result = %+v", res)
		}

		c, err := db.GetKubeCluster(ids["prod-eu"])
		if err != nil || c == nil {
			t.Fatalf("prod-eu lost: %v", err)
		}
		if !c.IsFavorite || c.EnvBadge != "staging" || c.KubeMode != "full" || c.KubeContextName != "tc/prod-eu" {
			t.Fatalf("kept cluster lost its metadata: %+v", c)
		}
		if other, _ := db.GetKubeCluster(ids["old-one"]); other != nil {
			t.Fatal("stale cluster still stored")
		}
		fresh, err := db.ListKubeClusters()
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, k := range fresh {
			names = append(names, k.Name)
		}
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"fresh", "prod-eu"}) {
			t.Fatalf("stored names = %v", names)
		}
	})
}

func TestTeleportClusterMetadataAndUse(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	pid, err := db.CreateTeleportProfile(newTeleportProfile("p"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SyncTeleportClusters(pid, "read-only", []TeleportSyncCluster{{Name: "dev-1"}}); err != nil {
		t.Fatal(err)
	}
	ids, _ := db.TeleportClusterIDs(pid)
	id := ids["dev-1"]

	t.Run("metadata update sets flags, badge and policy", func(t *testing.T) {
		t.Parallel()
		if err := db.UpdateKubeClusterMeta(id, true, "dev", "no-exec"); err != nil {
			t.Fatal(err)
		}
		c, _ := db.GetKubeCluster(id)
		if c == nil || !c.IsFavorite || c.EnvBadge != "dev" || c.KubeMode != "no-exec" {
			t.Fatalf("cluster = %+v", c)
		}
	})

	t.Run("touch records last use", func(t *testing.T) {
		t.Parallel()
		if err := db.TouchKubeClusterUsed(id); err != nil {
			t.Fatal(err)
		}
		c, _ := db.GetKubeCluster(id)
		if c == nil || c.LastUsedAt == nil {
			t.Fatalf("LastUsedAt not set: %+v", c)
		}
	})

	t.Run("clearing contexts forgets the names of the profile's clusters", func(t *testing.T) {
		t.Parallel()
		if err := db.SetKubeClusterContextName(id, "tc/dev-1"); err != nil {
			t.Fatal(err)
		}
		if err := db.ClearTeleportContexts(pid); err != nil {
			t.Fatal(err)
		}
		c, _ := db.GetKubeCluster(id)
		if c == nil || c.KubeContextName != "" {
			t.Fatalf("context not cleared: %+v", c)
		}
	})
}
