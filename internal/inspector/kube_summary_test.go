package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pantau/internal/store"
	"pantau/internal/teleport"
)

// kubeFake stands in for kubectl. script receives the shell body of each call and returns what kubectl would print.
type kubeFake struct {
	calls  atomic.Int64
	script func(body string) (stdout, stderr string, exit int, err error)
}

func (f *kubeFake) exec(_ int64, body string, _ time.Duration) (string, string, int, error) {
	f.calls.Add(1)
	return f.script(body)
}

// constantScript answers every call with the same stdout.
func constantScript(stdout string) func(string) (string, string, int, error) {
	return func(string) (string, string, int, error) { return stdout, "", 0, nil }
}

// newKubeTestInspector returns an inspector on a fresh database holding one SSH host and one kubeconfig cluster.
func newKubeTestInspector(tb testing.TB, f *kubeFake) (ins *Inspector, hostTarget, clusterTarget int64) {
	tb.Helper()
	db, err := store.Open(filepath.Join(tb.TempDir(), "kube.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	hostID, err := db.CreateHost(&store.Host{Name: "k3s-1", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		tb.Fatal(err)
	}
	clusterID, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Provider: "eks", KubeMode: "full", Kubeconfig: "apiVersion: v1"})
	if err != nil {
		tb.Fatal(err)
	}
	return &Inspector{db: db, kubeExecOverride: f.exec}, hostID, KubeClusterTarget(clusterID)
}

// kubeFixtureNamespaces is the size of the multi-namespace fixture: 15 namespaces, 75 workloads.
const kubeFixtureNamespaces = 15

// kubeFixtureItems returns the JSON objects of the fixture. Every namespace has one Deployment, StatefulSet,
// DaemonSet, CronJob and Job. Deployments are unhealthy when i%3==0, DaemonSets when i%4==1, Jobs fail when i%5==2.
// Unhealthy totals: 5 deployments + 4 daemonsets + 3 jobs = 12.
func kubeFixtureItems() []string {
	const created = `"creationTimestamp":"2026-11-20T00:00:00Z"`
	var items []string
	for i := range kubeFixtureNamespaces {
		ns := fmt.Sprintf("ns-%02d", i)
		depReady := 2
		if i%3 == 0 {
			depReady = 1
		}
		dsReady := 3
		if i%4 == 1 {
			dsReady = 2
		}
		jobCond := `{"type":"Complete","status":"True"}`
		if i%5 == 2 {
			jobCond = `{"type":"Failed","status":"True"}`
		}
		items = append(items,
			fmt.Sprintf(`{"kind":"Deployment","metadata":{"name":"web","namespace":%q,%s},"spec":{"replicas":2},"status":{"readyReplicas":%d}}`, ns, created, depReady),
			fmt.Sprintf(`{"kind":"StatefulSet","metadata":{"name":"db","namespace":%q,%s},"spec":{"replicas":1},"status":{"readyReplicas":1}}`, ns, created),
			fmt.Sprintf(`{"kind":"DaemonSet","metadata":{"name":"agent","namespace":%q,%s},"status":{"desiredNumberScheduled":3,"numberReady":%d}}`, ns, created, dsReady),
			fmt.Sprintf(`{"kind":"CronJob","metadata":{"name":"backup","namespace":%q,%s},"spec":{"suspend":false}}`, ns, created),
			fmt.Sprintf(`{"kind":"Job","metadata":{"name":"migrate","namespace":%q,%s},"status":{"conditions":[%s]}}`, ns, created, jobCond),
		)
	}
	return items
}

// kubeList wraps objects in a kubectl List. Extra objects (nodes, pods...) are appended to the fixture.
func kubeFixtureList(extra ...string) string {
	return `{"kind":"List","items":[` + strings.Join(append(kubeFixtureItems(), extra...), ",") + `]}`
}

// unhealthyFixture lists the 12 unhealthy objects of the fixture, sorted by namespace, kind and name.
var unhealthyFixture = []string{
	"ns-00/deployment/web:NotReady",
	"ns-01/daemonset/agent:NotReady",
	"ns-02/job/migrate:JobFailed",
	"ns-03/deployment/web:NotReady",
	"ns-05/daemonset/agent:NotReady",
	"ns-06/deployment/web:NotReady",
	"ns-07/job/migrate:JobFailed",
	"ns-09/daemonset/agent:NotReady",
	"ns-09/deployment/web:NotReady",
	"ns-12/deployment/web:NotReady",
	"ns-12/job/migrate:JobFailed",
	"ns-13/daemonset/agent:NotReady",
}

func refKey(r KubeObjectRef) string {
	return fmt.Sprintf("%s/%s/%s:%s", r.Namespace, r.Kind, r.Name, r.Reason)
}

func TestSummarizeWorkloadsSingleCall(t *testing.T) {
	t.Parallel()
	f := &kubeFake{script: constantScript(kubeFixtureList())}
	ins, host, _ := newKubeTestInspector(t, f)

	sum, err := ins.SummarizeWorkloads(context.Background(), host, KubeWorkloadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("kubectl calls = %d, want 1", got)
	}
	want := KubeWorkloadTotals{Deployments: 15, StatefulSets: 15, DaemonSets: 15, CronJobs: 15, Jobs: 15,
		TotalWorkloads: 75, ReadyWorkloads: 63, UnhealthyWorkloads: 12}
	if sum.Totals != want {
		t.Errorf("totals = %+v, want %+v", sum.Totals, want)
	}
	if len(sum.ByNamespace) != kubeFixtureNamespaces {
		t.Fatalf("by_namespace has %d rows, want %d", len(sum.ByNamespace), kubeFixtureNamespaces)
	}
	for _, ns := range sum.ByNamespace {
		if ns.Total != 5 || ns.Deployments != 1 || ns.Jobs != 1 {
			t.Errorf("namespace %s = %+v, want one of each kind", ns.Namespace, ns)
		}
	}
	var got []string
	for _, r := range sum.Unhealthy {
		got = append(got, refKey(r))
	}
	if !slices.Equal(got, unhealthyFixture) {
		t.Errorf("unhealthy =\n%v\nwant\n%v", got, unhealthyFixture)
	}
	if sum.UnhealthyTotal != 12 || sum.Items != nil {
		t.Errorf("unhealthy_total = %d, items = %v; want 12 and no items", sum.UnhealthyTotal, sum.Items)
	}
	if sum.Cluster.Name != "k3s-1" || sum.Cluster.Type != "ssh" || sum.Cluster.Cached {
		t.Errorf("cluster = %+v", sum.Cluster)
	}
	payload, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) >= 4096 {
		t.Errorf("payload is %d bytes, want < 4096", len(payload))
	}
}

func TestSummarizeWorkloadsScopeAndOptions(t *testing.T) {
	t.Parallel()
	podFixture := kubeFixtureList(
		`{"kind":"Pod","metadata":{"name":"ok","namespace":"ns-00"},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0,"state":{"running":{}}}]}}`,
		`{"kind":"Pod","metadata":{"name":"crash","namespace":"ns-00"},"status":{"phase":"Running","containerStatuses":[{"ready":false,"restartCount":5,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}`,
		`{"kind":"Pod","metadata":{"name":"stuck","namespace":"ns-01"},"status":{"phase":"Pending"}}`,
	)
	tests := []struct {
		name         string
		opts         KubeWorkloadOpts
		wantCmd      string // substring of the kubectl script
		wantPods     *int   // pods_total when pods are requested
		wantPodsBad  *int
		wantItems    int
		wantTruncate bool
	}{
		{name: "all namespaces", opts: KubeWorkloadOpts{}, wantCmd: "kc get deployments,statefulsets,daemonsets,cronjobs,jobs -A -o json"},
		{name: "one namespace", opts: KubeWorkloadOpts{Namespace: "ns-03"}, wantCmd: "kc get deployments,statefulsets,daemonsets,cronjobs,jobs -n 'ns-03' -o json"},
		{name: "pods in the same call", opts: KubeWorkloadOpts{IncludePods: true}, wantCmd: "jobs,pods -A -o json", wantPods: intPtr(3), wantPodsBad: intPtr(2)},
		{name: "names limited", opts: KubeWorkloadOpts{IncludeNames: true, Limit: 10}, wantCmd: "-A -o json", wantItems: 10, wantTruncate: true},
		{name: "names default limit", opts: KubeWorkloadOpts{IncludeNames: true}, wantCmd: "-A -o json", wantItems: 50, wantTruncate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var cmd string
			f := &kubeFake{script: func(body string) (string, string, int, error) {
				cmd = body
				return podFixture, "", 0, nil
			}}
			ins, host, _ := newKubeTestInspector(t, f)
			sum, err := ins.SummarizeWorkloads(context.Background(), host, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(cmd, tt.wantCmd) {
				t.Errorf("command %q does not contain %q", cmd, tt.wantCmd)
			}
			if tt.wantPods != nil {
				if *sum.Totals.PodsTotal != *tt.wantPods || *sum.Totals.PodsUnhealthy != *tt.wantPodsBad {
					t.Errorf("pods = %d/%d, want %d/%d", *sum.Totals.PodsTotal, *sum.Totals.PodsUnhealthy, *tt.wantPods, *tt.wantPodsBad)
				}
			} else if sum.Totals.PodsTotal != nil {
				t.Errorf("pods reported without include_pods: %d", *sum.Totals.PodsTotal)
			}
			if tt.wantItems > 0 && len(sum.Items) != tt.wantItems {
				t.Errorf("items = %d, want %d", len(sum.Items), tt.wantItems)
			}
			if sum.ItemsTruncated != tt.wantTruncate {
				t.Errorf("items_truncated = %v, want %v", sum.ItemsTruncated, tt.wantTruncate)
			}
		})
	}
}

func TestSummarizeWorkloadsRejectsBadNamespaceWithoutKubectl(t *testing.T) {
	t.Parallel()
	f := &kubeFake{script: constantScript(kubeFixtureList())}
	ins, host, _ := newKubeTestInspector(t, f)
	_, err := ins.SummarizeWorkloads(context.Background(), host, KubeWorkloadOpts{Namespace: "ns;rm -rf /"})
	if !errors.Is(err, ErrKubeInvalid) {
		t.Fatalf("err = %v, want ErrKubeInvalid", err)
	}
	if f.calls.Load() != 0 {
		t.Errorf("kubectl ran %d times for an invalid namespace", f.calls.Load())
	}
}

func TestSummarizeWorkloadsCacheTTLAndRefresh(t *testing.T) {
	t.Parallel()
	f := &kubeFake{script: constantScript(kubeFixtureList())}
	ins, host, _ := newKubeTestInspector(t, f)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	ins.summaries.now = func() time.Time { return now }
	ctx := context.Background()

	steps := []struct {
		name       string
		advance    time.Duration
		opts       KubeWorkloadOpts
		wantCached bool
		wantCalls  int64
	}{
		{name: "first call runs kubectl", wantCached: false, wantCalls: 1},
		{name: "second call is cached", wantCached: true, wantCalls: 1},
		{name: "just inside TTL", advance: kubeSummaryTTL - time.Second, wantCached: true, wantCalls: 1},
		{name: "past TTL runs again", advance: 2 * time.Second, wantCached: false, wantCalls: 2},
		{name: "refresh bypasses cache", opts: KubeWorkloadOpts{Refresh: true}, wantCached: false, wantCalls: 3},
	}
	for _, s := range steps {
		now = now.Add(s.advance)
		sum, err := ins.SummarizeWorkloads(ctx, host, s.opts)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if sum.Cluster.Cached != s.wantCached || f.calls.Load() != s.wantCalls {
			t.Errorf("%s: cached=%v calls=%d, want cached=%v calls=%d",
				s.name, sum.Cluster.Cached, f.calls.Load(), s.wantCached, s.wantCalls)
		}
	}
}

func TestSummarizeWorkloadsSingleflightRunsOnce(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	f := &kubeFake{}
	f.script = func(string) (string, string, int, error) {
		entered <- struct{}{}
		<-release
		return kubeFixtureList(), "", 0, nil
	}
	ins, host, _ := newKubeTestInspector(t, f)
	ctx := context.Background()

	type answer struct {
		cached bool
		err    error
	}
	const followers = 8
	results := make(chan answer, followers+1)
	run := func() {
		sum, err := ins.SummarizeWorkloads(ctx, host, KubeWorkloadOpts{})
		a := answer{err: err}
		if sum != nil {
			a.cached = sum.Cluster.Cached
		}
		results <- a
	}

	go run() // the leader blocks inside kubectl until release is closed
	<-entered
	for range followers {
		go run()
	}
	close(release)

	leaderCached := 0
	for range followers + 1 {
		a := <-results
		if a.err != nil {
			t.Fatal(a.err)
		}
		if !a.cached {
			leaderCached++
		}
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("kubectl calls = %d, want 1 for %d concurrent callers", got, followers+1)
	}
	if leaderCached != 1 {
		t.Errorf("%d callers reported a fresh run, want exactly 1", leaderCached)
	}
}

func TestSummarizeWorkloadsErrorsAreNotCached(t *testing.T) {
	t.Parallel()
	calls := 0
	f := &kubeFake{script: func(string) (string, string, int, error) {
		calls++
		if calls == 1 {
			return "", "the server is down", 1, nil
		}
		return kubeFixtureList(), "", 0, nil
	}}
	ins, host, _ := newKubeTestInspector(t, f)

	_, err := ins.SummarizeWorkloads(context.Background(), host, KubeWorkloadOpts{})
	if err == nil || !strings.Contains(err.Error(), "the server is down") {
		t.Fatalf("first err = %v, want the kubectl stderr", err)
	}
	if _, err := ins.SummarizeWorkloads(context.Background(), host, KubeWorkloadOpts{}); err != nil {
		t.Fatalf("second call after a failure: %v", err)
	}
	if f.calls.Load() != 2 {
		t.Errorf("kubectl calls = %d, want 2 (the failure must not be cached)", f.calls.Load())
	}
}

func TestSummarizeWorkloadsTeleportExpiry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		stderr     string
		wantExpiry bool
	}{
		{name: "rejected certificate maps to the expired session", stderr: "error: Unauthorized", wantExpiry: true},
		{name: "other failure keeps kubectl text", stderr: "connection refused", wantExpiry: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &kubeFake{script: func(string) (string, string, int, error) { return "", tt.stderr, 1, nil }}
			ins, _, clusterTarget := newKubeTestInspector(t, f)
			// Turn the kubeconfig cluster into a Teleport one bound to profile 7.
			c, err := ins.db.GetKubeCluster(-clusterTarget)
			if err != nil || c == nil {
				t.Fatalf("cluster lookup: %v", err)
			}
			c.SourceType, c.TeleportProfileID = store.KubeSourceTeleport, 7
			if _, err := ins.db.Exec(`UPDATE kube_clusters SET source_type = ?, teleport_profile_id = ? WHERE id = ?`,
				c.SourceType, c.TeleportProfileID, c.ID); err != nil {
				t.Fatal(err)
			}

			_, err = ins.SummarizeWorkloads(context.Background(), clusterTarget, KubeWorkloadOpts{})
			var expired *TeleportSessionError
			if got := errors.As(err, &expired); got != tt.wantExpiry {
				t.Fatalf("expired = %v (err %v), want %v", got, err, tt.wantExpiry)
			}
			if tt.wantExpiry {
				if expired.ProfileID != 7 || !errors.Is(err, teleport.ErrTeleportSessionExpired) {
					t.Errorf("expired error = %+v, want profile 7 wrapping ErrTeleportSessionExpired", expired)
				}
			} else if err == nil || !strings.Contains(err.Error(), "connection refused") {
				t.Errorf("err = %v, want the kubectl text", err)
			}
		})
	}
}

func TestSummarizeClusterInventory(t *testing.T) {
	t.Parallel()
	f := &kubeFake{script: constantScript(kubeFixtureList(
		`{"kind":"Namespace","metadata":{"name":"default"},"status":{"phase":"Active"}}`,
		`{"kind":"Namespace","metadata":{"name":"kube-system"},"status":{"phase":"Active"}}`,
		`{"kind":"Node","metadata":{"name":"n1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`,
		`{"kind":"Node","metadata":{"name":"n2"},"status":{"conditions":[{"type":"Ready","status":"False"}]}}`,
		`{"kind":"Service","metadata":{"name":"s1","namespace":"ns-00"},"spec":{"type":"ClusterIP"}}`,
		`{"kind":"Service","metadata":{"name":"s2","namespace":"ns-00"},"spec":{"type":"NodePort"}}`,
		`{"kind":"Ingress","metadata":{"name":"i1","namespace":"ns-00"}}`,
		`{"kind":"PersistentVolumeClaim","metadata":{"name":"data","namespace":"ns-00"},"status":{"phase":"Bound"}}`,
		`{"kind":"PersistentVolumeClaim","metadata":{"name":"cache","namespace":"ns-01"},"status":{"phase":"Pending"}}`,
	))}
	ins, host, _ := newKubeTestInspector(t, f)

	inv, err := ins.SummarizeClusterInventory(context.Background(), host, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("kubectl calls = %d, want 1", f.calls.Load())
	}
	if inv.Cluster.Name != "k3s-1" || inv.Cluster.Type != "ssh" {
		t.Errorf("cluster = %+v", inv.Cluster)
	}
	if inv.Nodes != (KubeNodeCounts{Total: 2, Ready: 1, NotReady: 1}) {
		t.Errorf("nodes = %+v", inv.Nodes)
	}
	if inv.Namespaces != 2 || inv.Services != 2 || inv.Ingresses != 1 || inv.PVCs != 2 {
		t.Errorf("namespaces=%d services=%d ingresses=%d pvcs=%d", inv.Namespaces, inv.Services, inv.Ingresses, inv.PVCs)
	}
	if inv.Workloads.TotalWorkloads != 75 || inv.Workloads.UnhealthyWorkloads != 12 {
		t.Errorf("workloads = %+v", inv.Workloads)
	}
	// 12 unhealthy workloads, 1 NotReady node and 1 Pending claim.
	if inv.AnomaliesTotal != 14 || len(inv.Anomalies) != 14 {
		t.Fatalf("anomalies total=%d listed=%d, want 14", inv.AnomaliesTotal, len(inv.Anomalies))
	}
	if inv.Anomalies[0].Kind != "node" || inv.Anomalies[0].Name != "n2" || inv.Anomalies[0].Reason != "NotReady" {
		t.Errorf("first anomaly = %+v, want the NotReady node", inv.Anomalies[0])
	}
}

func TestSummarizeClusterInventoryCommand(t *testing.T) {
	t.Parallel()
	var cmd string
	f := &kubeFake{script: func(body string) (string, string, int, error) {
		cmd = body
		return kubeFixtureList(), "", 0, nil
	}}
	ins, host, _ := newKubeTestInspector(t, f)
	if _, err := ins.SummarizeClusterInventory(context.Background(), host, false); err != nil {
		t.Fatal(err)
	}
	want := "kc get nodes,namespaces,services,ingresses,pvcs,deployments,statefulsets,daemonsets,cronjobs,jobs -A -o json"
	if !strings.Contains(cmd, want) {
		t.Errorf("command %q does not contain %q", cmd, want)
	}
}

func intPtr(n int) *int { return &n }

func BenchmarkSummarizeWorkloads(b *testing.B) {
	f := &kubeFake{script: constantScript(kubeFixtureList())}
	ins, host, _ := newKubeTestInspector(b, f)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		// Refresh makes each iteration parse the list, so the benchmark measures the aggregation, not the cache.
		if _, err := ins.SummarizeWorkloads(ctx, host, KubeWorkloadOpts{Refresh: true}); err != nil {
			b.Fatal(err)
		}
	}
}
