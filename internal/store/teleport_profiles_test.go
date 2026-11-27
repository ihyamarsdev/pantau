package store

import (
	"errors"
	"testing"
)

func newTeleportProfile(name string) *TeleportProfile {
	return &TeleportProfile{
		Name:            name,
		Proxy:           "teleport.setoranku.com:443",
		AuthConnector:   "local",
		Username:        "alice",
		TeleportCluster: "teleport.internal.verihubs.com",
		DefaultPolicy:   "read-only",
	}
}

func TestCreateTeleportProfilePolicyAndNameRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		policy     string
		wantPolicy string
	}{
		{name: "full kept", policy: "full", wantPolicy: "full"},
		{name: "no-exec kept", policy: "no-exec", wantPolicy: "no-exec"},
		{name: "unknown falls back to read-only", policy: "root", wantPolicy: "read-only"},
		{name: "empty falls back to read-only", policy: "", wantPolicy: "read-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := openTestDB(t)
			p := newTeleportProfile("prod-" + tt.wantPolicy)
			p.DefaultPolicy = tt.policy
			id, err := db.CreateTeleportProfile(p)
			if err != nil {
				t.Fatalf("CreateTeleportProfile() error = %v", err)
			}
			got, err := db.GetTeleportProfile(id)
			if err != nil || got == nil {
				t.Fatalf("GetTeleportProfile() = %v, %v", got, err)
			}
			if got.DefaultPolicy != tt.wantPolicy {
				t.Fatalf("policy = %q, want %q", got.DefaultPolicy, tt.wantPolicy)
			}
		})
	}

	t.Run("duplicate name is refused case-insensitively", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		if _, err := db.CreateTeleportProfile(newTeleportProfile("Prod")); err != nil {
			t.Fatal(err)
		}
		_, err := db.CreateTeleportProfile(newTeleportProfile("prod"))
		if !errors.Is(err, ErrTeleportProfileNameTaken) {
			t.Fatalf("error = %v, want ErrTeleportProfileNameTaken", err)
		}
	})
}

func TestGetTeleportProfileMissingIsNil(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	got, err := db.GetTeleportProfile(999)
	if err != nil || got != nil {
		t.Fatalf("GetTeleportProfile(missing) = %v, %v; want nil, nil", got, err)
	}
}

func TestUpdateTeleportProfile(t *testing.T) {
	t.Parallel()

	t.Run("fields are replaced", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		id, err := db.CreateTeleportProfile(newTeleportProfile("staging"))
		if err != nil {
			t.Fatal(err)
		}
		p := newTeleportProfile("staging-eu")
		p.ID = id
		p.DefaultPolicy = "full"
		p.TeleportHome = "/srv/tsh-staging"
		if err := db.UpdateTeleportProfile(p); err != nil {
			t.Fatalf("UpdateTeleportProfile() error = %v", err)
		}
		got, err := db.GetTeleportProfile(id)
		if err != nil || got == nil {
			t.Fatalf("GetTeleportProfile() = %v, %v", got, err)
		}
		if got.Name != "staging-eu" || got.DefaultPolicy != "full" || got.TeleportHome != "/srv/tsh-staging" {
			t.Fatalf("updated profile = %+v", got)
		}
	})

	t.Run("rename onto another profile is refused", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		if _, err := db.CreateTeleportProfile(newTeleportProfile("alpha")); err != nil {
			t.Fatal(err)
		}
		id, err := db.CreateTeleportProfile(newTeleportProfile("beta"))
		if err != nil {
			t.Fatal(err)
		}
		p := newTeleportProfile("ALPHA")
		p.ID = id
		if err := db.UpdateTeleportProfile(p); !errors.Is(err, ErrTeleportProfileNameTaken) {
			t.Fatalf("error = %v, want ErrTeleportProfileNameTaken", err)
		}
	})
}

// Deleting a profile removes the Teleport clusters synced from it and nothing else: a cluster from another
// profile, or an external kubeconfig cluster, must stay.
func TestDeleteTeleportProfileRemovesOnlyItsClusters(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	p1, err := db.CreateTeleportProfile(newTeleportProfile("one"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := db.CreateTeleportProfile(newTeleportProfile("two"))
	if err != nil {
		t.Fatal(err)
	}
	mustCreate := func(c *KubeCluster) {
		t.Helper()
		if _, err := db.CreateKubeCluster(c); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate(&KubeCluster{Name: "from-one", KubeMode: "full", SourceType: KubeSourceTeleport, TeleportProfileID: p1})
	mustCreate(&KubeCluster{Name: "from-two", KubeMode: "full", SourceType: KubeSourceTeleport, TeleportProfileID: p2})
	mustCreate(&KubeCluster{Name: "external", KubeMode: "full", Kubeconfig: "apiVersion: v1"})

	if err := db.DeleteTeleportProfile(p1); err != nil {
		t.Fatalf("DeleteTeleportProfile() error = %v", err)
	}
	if got, _ := db.GetTeleportProfile(p1); got != nil {
		t.Fatal("profile one still exists")
	}
	clusters, err := db.ListKubeClusters()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, c := range clusters {
		names[c.Name] = true
	}
	if names["from-one"] || !names["from-two"] || !names["external"] {
		t.Fatalf("remaining clusters = %v, want from-two and external only", names)
	}
}

func TestKubeClusterTeleportFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	pid, err := db.CreateTeleportProfile(newTeleportProfile("prod"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("new teleport cluster keeps its metadata", func(t *testing.T) {
		t.Parallel()
		id, err := db.CreateKubeCluster(&KubeCluster{
			Name: "prod-eu", KubeMode: "read-only", SourceType: KubeSourceTeleport,
			TeleportProfileID: pid, TeleportKubeName: "prod-eu", EnvBadge: "prod", IsFavorite: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetKubeClusterContextName(id, "tc/prod-eu"); err != nil {
			t.Fatal(err)
		}
		got, err := db.GetKubeCluster(id)
		if err != nil || got == nil {
			t.Fatalf("GetKubeCluster() = %v, %v", got, err)
		}
		if got.SourceType != KubeSourceTeleport || got.TeleportProfileID != pid || got.TeleportKubeName != "prod-eu" {
			t.Fatalf("teleport fields = %+v", got)
		}
		if got.KubeContextName != "tc/prod-eu" || got.EnvBadge != "prod" || !got.IsFavorite || got.KubeMode != "read-only" {
			t.Fatalf("metadata = %+v", got)
		}
		if got.LastUsedAt != nil {
			t.Fatalf("LastUsedAt = %v, want nil before first use", got.LastUsedAt)
		}
	})

	t.Run("cluster created without a source is a kubeconfig cluster", func(t *testing.T) {
		t.Parallel()
		id, err := db.CreateKubeCluster(&KubeCluster{Name: "legacy-cluster", KubeMode: "full", Kubeconfig: "apiVersion: v1"})
		if err != nil {
			t.Fatal(err)
		}
		got, err := db.GetKubeCluster(id)
		if err != nil || got == nil {
			t.Fatalf("GetKubeCluster() = %v, %v", got, err)
		}
		if got.SourceType != KubeSourceKubeconfig {
			t.Fatalf("SourceType = %q, want kubeconfig", got.SourceType)
		}
	})
}
