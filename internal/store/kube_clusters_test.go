package store_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"pantau/internal/store"
)

func setupTestDB(t *testing.T) (*store.DB, func()) {
	t.Helper()
	tmp, err := os.CreateTemp("", "pantau_store_test_*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	db, err := store.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		t.Fatalf("open db: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpPath)
	}
	return db, cleanup
}

func TestKubeClusters_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Initially empty
	clusters, err := db.ListKubeClusters()
	if err != nil {
		t.Fatalf("list clusters on fresh db: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(clusters))
	}

	// 2. Create cluster
	c1 := &store.KubeCluster{
		Name:          "prod-eks",
		Context:       "arn:aws:eks:us-east-1:123456789012:cluster/prod",
		Server:        "https://xyz.eks.amazonaws.com",
		Provider:      "eks",
		KubeMode:      "full",
		KubeClusterID: "uid-1234",
		Kubeconfig:    `{"apiVersion":"v1","clusters":[]}`,
	}
	id1, err := db.CreateKubeCluster(c1)
	if err != nil {
		t.Fatalf("create cluster c1: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive id, got %d", id1)
	}

	// 3. Get cluster
	got1, err := db.GetKubeCluster(id1)
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if got1 == nil {
		t.Fatalf("expected cluster %d, got nil", id1)
	}
	if got1.Name != "prod-eks" || got1.Provider != "eks" || got1.KubeMode != "full" || got1.KubeClusterID != "uid-1234" {
		t.Errorf("unexpected retrieved cluster: %+v", got1)
	}
	if got1.Kubeconfig != `{"apiVersion":"v1","clusters":[]}` {
		t.Errorf("expected kubeconfig preserved, got %q", got1.Kubeconfig)
	}

	// 4. Missing cluster returns (nil, nil)
	missing, err := db.GetKubeCluster(99999)
	if err != nil {
		t.Fatalf("get missing cluster returned error: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing cluster, got %+v", missing)
	}

	// 5. Duplicate name (case-insensitive) returns ErrKubeClusterNameTaken
	cDup := &store.KubeCluster{
		Name:     "PROD-EKS",
		Context:  "different-ctx",
		Server:   "https://different.example.com",
		Provider: "generic",
	}
	_, err = db.CreateKubeCluster(cDup)
	if !errors.Is(err, store.ErrKubeClusterNameTaken) {
		t.Fatalf("expected ErrKubeClusterNameTaken for duplicate name, got %v", err)
	}

	// 6. List ordering (case-insensitive by name)
	c2 := &store.KubeCluster{
		Name:     "alpha-gke",
		Context:  "gke-ctx",
		Server:   "https://gke.example.com",
		Provider: "gke",
	}
	c3 := &store.KubeCluster{
		Name:     "Beta-aks",
		Context:  "aks-ctx",
		Server:   "https://aks.example.com",
		Provider: "aks",
	}
	if _, err := db.CreateKubeCluster(c2); err != nil {
		t.Fatalf("create c2: %v", err)
	}
	if _, err := db.CreateKubeCluster(c3); err != nil {
		t.Fatalf("create c3: %v", err)
	}

	list, err := db.ListKubeClusters()
	if err != nil {
		t.Fatalf("list clusters: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 clusters, got %d", len(list))
	}
	// Expected alphabetical case-insensitive: alpha-gke, Beta-aks, prod-eks
	expectedOrder := []string{"alpha-gke", "Beta-aks", "prod-eks"}
	for i, exp := range expectedOrder {
		if list[i].Name != exp {
			t.Errorf("list[%d].Name = %q, expected %q", i, list[i].Name, exp)
		}
	}

	// 7. Update without kubeconfig (keeps old kubeconfig and context/server/provider)
	got1.Name = "prod-eks-renamed"
	got1.KubeMode = "read-only"
	got1.Kubeconfig = "" // empty means do not overwrite
	got1.Context = "should-not-change"
	if err := db.UpdateKubeCluster(got1); err != nil {
		t.Fatalf("update cluster without kubeconfig: %v", err)
	}

	updated1, err := db.GetKubeCluster(id1)
	if err != nil {
		t.Fatalf("get updated1: %v", err)
	}
	if updated1.Name != "prod-eks-renamed" || updated1.KubeMode != "read-only" {
		t.Errorf("expected name and mode updated, got %+v", updated1)
	}
	if updated1.Context != "arn:aws:eks:us-east-1:123456789012:cluster/prod" {
		t.Errorf("expected context retained, got %q", updated1.Context)
	}
	if updated1.Kubeconfig != `{"apiVersion":"v1","clusters":[]}` {
		t.Errorf("expected kubeconfig retained, got %q", updated1.Kubeconfig)
	}

	// 8. Update with kubeconfig (replaces config/context/server/provider)
	updated1.Kubeconfig = `{"apiVersion":"v1","clusters":[{"name":"new"}]}`
	updated1.Context = "new-ctx"
	updated1.Server = "https://new.server.com"
	updated1.Provider = "generic"
	updated1.KubeMode = "no-exec"
	if err := db.UpdateKubeCluster(updated1); err != nil {
		t.Fatalf("update cluster with kubeconfig: %v", err)
	}

	updatedAgain, err := db.GetKubeCluster(id1)
	if err != nil {
		t.Fatalf("get updatedAgain: %v", err)
	}
	if updatedAgain.Context != "new-ctx" || updatedAgain.Server != "https://new.server.com" ||
		updatedAgain.Provider != "generic" || updatedAgain.KubeMode != "no-exec" ||
		updatedAgain.Kubeconfig != `{"apiVersion":"v1","clusters":[{"name":"new"}]}` {
		t.Errorf("unexpected updatedAgain: %+v", updatedAgain)
	}

	// 9. Update to duplicate name returns ErrKubeClusterNameTaken
	updatedAgain.Name = "ALPHA-GKE" // matches c2
	if err := db.UpdateKubeCluster(updatedAgain); !errors.Is(err, store.ErrKubeClusterNameTaken) {
		t.Fatalf("expected ErrKubeClusterNameTaken on rename conflict, got %v", err)
	}

	// 10. SetKubeClusterUID
	if err := db.SetKubeClusterUID(id1, "new-system-uid-456"); err != nil {
		t.Fatalf("set cluster uid: %v", err)
	}
	afterUID, err := db.GetKubeCluster(id1)
	if err != nil || afterUID.KubeClusterID != "new-system-uid-456" {
		t.Fatalf("expected uid 'new-system-uid-456', got %q, err=%v", afterUID.KubeClusterID, err)
	}

	// 11. Delete cluster
	if err := db.DeleteKubeCluster(id1); err != nil {
		t.Fatalf("delete cluster: %v", err)
	}
	deleted, err := db.GetKubeCluster(id1)
	if err != nil {
		t.Fatalf("get deleted: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected nil after delete, got %+v", deleted)
	}
}

func TestKubeClusters_KubeModeValidation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Invalid/empty kube_mode defaults to "full"
	c := &store.KubeCluster{
		Name:     "test-mode",
		KubeMode: "invalid-value",
	}
	id, err := db.CreateKubeCluster(c)
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	got, err := db.GetKubeCluster(id)
	if err != nil || got == nil {
		t.Fatalf("get cluster: %v", err)
	}
	if got.KubeMode != "full" {
		t.Errorf("expected default 'full', got %q", got.KubeMode)
	}

	// Update with empty mode defaults to "full"
	got.KubeMode = ""
	if err := db.UpdateKubeCluster(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = db.GetKubeCluster(id)
	if got.KubeMode != "full" {
		t.Errorf("expected reset to 'full', got %q", got.KubeMode)
	}
}

func TestKubeClusters_SnapshotRoundTrip(t *testing.T) {
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()

	c1ID, err := db1.CreateKubeCluster(&store.KubeCluster{
		Name:          "prod-cluster",
		Context:       "prod-context",
		Server:        "https://k8s.example.com:6443",
		Provider:      "eks",
		KubeMode:      "no-exec",
		KubeClusterID: "kube-uid-999",
		Kubeconfig:    "cluster-raw-kubeconfig-secret-yaml-or-json",
	})
	if err != nil {
		t.Fatalf("create cluster in db1: %v", err)
	}

	// Export snapshot
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	if len(payload.KubeClusters) != 1 {
		t.Fatalf("expected 1 kube cluster in snapshot, got %d", len(payload.KubeClusters))
	}
	snapC := payload.KubeClusters[0]
	if snapC.ID != c1ID || snapC.Name != "prod-cluster" || snapC.Kubeconfig != "cluster-raw-kubeconfig-secret-yaml-or-json" {
		t.Errorf("unexpected exported snapshot cluster: %+v", snapC)
	}

	// Import into new db
	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()

	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatalf("import snapshot to db2: %v", err)
	}

	restored, err := db2.GetKubeCluster(c1ID)
	if err != nil {
		t.Fatalf("get restored cluster: %v", err)
	}
	if restored == nil {
		t.Fatalf("expected restored cluster with id %d, got nil", c1ID)
	}
	if restored.Name != "prod-cluster" || restored.Context != "prod-context" ||
		restored.Server != "https://k8s.example.com:6443" || restored.Provider != "eks" ||
		restored.KubeMode != "no-exec" || restored.KubeClusterID != "kube-uid-999" ||
		restored.Kubeconfig != "cluster-raw-kubeconfig-secret-yaml-or-json" {
		t.Errorf("restored cluster does not match exported: %+v", restored)
	}

	// Backward compatibility: import older legacy snapshot without KubeClusters key
	legacyPayload := &store.SnapshotPayload{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Settings:  map[string]string{"foo": "bar"},
		Hosts: []store.Host{
			{
				ID:   42,
				Name: "Legacy Host",
				Host: "192.168.1.42",
				Port: 22,
				User: "root",
			},
		},
	}

	if err := db2.ImportSnapshot(legacyPayload); err != nil {
		t.Fatalf("import legacy payload failed: %v", err)
	}

	// Previous clusters should be cleared since ImportSnapshot wipes tables
	clustersAfterLegacy, err := db2.ListKubeClusters()
	if err != nil {
		t.Fatalf("list clusters after legacy import: %v", err)
	}
	if len(clustersAfterLegacy) != 0 {
		t.Fatalf("expected 0 kube clusters after legacy import wipe, got %d", len(clustersAfterLegacy))
	}

	// Verify legacy host was restored
	h, err := db2.GetHost(42)
	if err != nil || h == nil {
		t.Fatalf("expected legacy host 42, got %v, err=%v", h, err)
	}
}
