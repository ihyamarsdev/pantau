package inspector

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestKubeListResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kinds   []string
		want    []string
		wantErr bool
	}{
		{name: "one plural kind", kinds: []string{"deployments"}, want: []string{"deployments"}},
		{name: "singular and mixed case, deduplicated and sorted", kinds: []string{" Deployment ", "statefulsets", "DEPLOYMENTS"}, want: []string{"deployments", "statefulsets"}},
		{name: "short alias", kinds: []string{"svc"}, want: []string{"services"}},
		{name: "empty entries are skipped", kinds: []string{"deployments", "", " "}, want: []string{"deployments"}},
		{name: "no kind is refused", kinds: []string{" "}, wantErr: true},
		{name: "flag injection is refused", kinds: []string{"-A"}, wantErr: true},
		{name: "shell metacharacters are refused", kinds: []string{"pods;rm -rf /"}, wantErr: true},
		{name: "secrets are not listed", kinds: []string{"secrets"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := kubeListResources(tt.kinds)
			if tt.wantErr {
				if !errors.Is(err, ErrKubeInvalid) {
					t.Fatalf("err = %v, want ErrKubeInvalid", err)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("resources = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestKubeListRejectsBadInputWithoutKubectl(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts KubeListOpts
	}{
		{name: "label selector flag injection", opts: KubeListOpts{LabelSelector: "--all-namespaces"}},
		{name: "label selector shell metacharacters", opts: KubeListOpts{LabelSelector: "app=web;id"}},
		{name: "field selector flag injection", opts: KubeListOpts{FieldSelector: "-n kube-system"}},
		{name: "namespace shell metacharacters", opts: KubeListOpts{Namespace: "x y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &kubeFake{script: constantScript(kubeFixtureList())}
			ins, host, _ := newKubeTestInspector(t, f)
			_, err := ins.KubeList(host, []string{"deployments"}, tt.opts)
			if !errors.Is(err, ErrKubeInvalid) {
				t.Fatalf("err = %v, want ErrKubeInvalid", err)
			}
			if f.calls.Load() != 0 {
				t.Errorf("kubectl ran %d times", f.calls.Load())
			}
		})
	}
}

func TestKubeListCommandScope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    KubeListOpts
		wantCmd string
	}{
		{name: "star means every namespace", opts: KubeListOpts{Namespace: "*"}, wantCmd: "kc get deployments,pods -A -o json"},
		{name: "all means every namespace", opts: KubeListOpts{Namespace: "all"}, wantCmd: "kc get deployments,pods -A -o json"},
		{name: "empty means every namespace", opts: KubeListOpts{}, wantCmd: "kc get deployments,pods -A -o json"},
		{name: "one namespace", opts: KubeListOpts{Namespace: "ns-03"}, wantCmd: "kc get deployments,pods -n 'ns-03' -o json"},
		{name: "selectors are quoted", opts: KubeListOpts{LabelSelector: "app=web,tier in (front)", FieldSelector: "status.phase!=Running"},
			wantCmd: "--selector='app=web,tier in (front)' --field-selector='status.phase!=Running' -o json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var cmd string
			f := &kubeFake{script: func(body string) (string, string, int, error) {
				cmd = body
				return `{"kind":"List","items":[]}`, "", 0, nil
			}}
			ins, host, _ := newKubeTestInspector(t, f)
			if _, err := ins.KubeList(host, []string{"pods", "deployments"}, tt.opts); err != nil {
				t.Fatal(err)
			}
			if f.calls.Load() != 1 || !strings.Contains(cmd, tt.wantCmd) {
				t.Errorf("calls=%d command %q; want it to contain %q", f.calls.Load(), cmd, tt.wantCmd)
			}
		})
	}
}

func TestBuildKubeList(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	all := kubeFixtureList()
	tests := []struct {
		name      string
		out       string
		opts      KubeListOpts
		wantTotal int
		wantRows  int
		wantTrunc bool
		check     func(t *testing.T, res *KubeListResult)
	}{
		{
			name: "unhealthy only, limited rows keep the full total",
			out:  all, opts: KubeListOpts{UnhealthyOnly: true, Limit: 5},
			wantTotal: 12, wantRows: 5, wantTrunc: true,
			check: func(t *testing.T, res *KubeListResult) {
				if res.ByKind["deployment"] != 5 || res.ByKind["daemonset"] != 4 || res.ByKind["job"] != 3 {
					t.Errorf("by_kind = %v", res.ByKind)
				}
				if len(res.ByNamespace) != 10 {
					t.Errorf("by_namespace has %d namespaces, want 10", len(res.ByNamespace))
				}
				if first := res.Items[0]; first.Namespace != "ns-00" || first.Name != "web" || first.Status != "NotReady" {
					t.Errorf("first row = %+v", first)
				}
			},
		},
		{
			name: "summary only keeps names when they fit the limit",
			out:  all, opts: KubeListOpts{SummaryOnly: true, UnhealthyOnly: true},
			wantTotal: 12, wantRows: 12,
			check: func(t *testing.T, res *KubeListResult) {
				for _, row := range res.Items {
					if row.Name == "" || row.Kind == "" || row.Status != "" || row.Age != "" {
						t.Errorf("summary row carries detail: %+v", row)
					}
				}
			},
		},
		{
			name: "summary only drops names past the limit",
			out:  all, opts: KubeListOpts{SummaryOnly: true, UnhealthyOnly: true, Limit: 5},
			wantTotal: 12, wantRows: 0, wantTrunc: true,
		},
		{
			name: "full rows carry status, ready and age",
			out:  all, opts: KubeListOpts{Limit: 200},
			wantTotal: 75, wantRows: 75,
			check: func(t *testing.T, res *KubeListResult) {
				web := res.Items[2] // ns-00: cronjob, daemonset, deployment, job, statefulset
				if web.Kind != "deployment" || web.Name != "web" || web.Ready != "1/2" || web.Status != "NotReady" || web.Age != "7d" {
					t.Errorf("deployment row = %+v", web)
				}
				if healthy := res.Items[7]; healthy.Ready != "2/2" || healthy.Status != "Ready" {
					t.Errorf("healthy row = %+v", healthy)
				}
			},
		},
		{
			name: "pods carry ready containers and restarts",
			out: `{"kind":"List","items":[{"kind":"Pod","metadata":{"name":"crash","namespace":"ns-00","creationTimestamp":"2026-11-25T00:00:00Z"},` +
				`"status":{"phase":"Running","containerStatuses":[{"ready":false,"restartCount":5,"state":{"waiting":{"reason":"CrashLoopBackOff"}}},` +
				`{"ready":true,"restartCount":2,"state":{"running":{}}}]}}]}`,
			opts: KubeListOpts{}, wantTotal: 1, wantRows: 1,
			check: func(t *testing.T, res *KubeListResult) {
				row := res.Items[0]
				if row.Status != "CrashLoopBackOff" || row.Ready != "1/2" || row.Restarts != 7 || row.Age != "2d" {
					t.Errorf("pod row = %+v", row)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := buildKubeList(tt.out, tt.opts, now)
			if err != nil {
				t.Fatal(err)
			}
			if res.TotalCount != tt.wantTotal || len(res.Items) != tt.wantRows || res.Truncated != tt.wantTrunc {
				t.Errorf("total=%d rows=%d truncated=%v; want %d, %d, %v",
					res.TotalCount, len(res.Items), res.Truncated, tt.wantTotal, tt.wantRows, tt.wantTrunc)
			}
			if tt.check != nil {
				tt.check(t, res)
			}
		})
	}
}

func TestKubeAge(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		created string
		want    string
	}{
		{name: "missing timestamp", created: "", want: ""},
		{name: "seconds", created: "2026-11-27T11:59:20Z", want: "40s"},
		{name: "minutes", created: "2026-11-27T11:55:00Z", want: "5m"},
		{name: "hours", created: "2026-11-27T09:00:00Z", want: "3h"},
		{name: "days", created: "2026-11-22T12:00:00Z", want: "5d"},
		{name: "future timestamp", created: "2026-11-27T13:00:00Z", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := kubeAge(tt.created, now); got != tt.want {
				t.Errorf("kubeAge = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKubeListRunsOnceAcrossNamespaces(t *testing.T) {
	t.Parallel()
	// The fake returns every fixture kind, as one kubectl call with several kinds would; the 12 unhealthy objects
	// of the fixture are all counted, and the limit only trims the rows.
	f := &kubeFake{script: constantScript(kubeFixtureList())}
	ins, host, _ := newKubeTestInspector(t, f)
	res, err := ins.KubeList(host, []string{"deployments", "statefulsets", "daemonsets"}, KubeListOpts{Namespace: "*", UnhealthyOnly: true, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Errorf("kubectl calls = %d, want 1", f.calls.Load())
	}
	if res.TotalCount != 12 || len(res.Items) != 3 || !res.Truncated {
		t.Errorf("total_count = %d rows = %d truncated = %v, want 12, 3, true", res.TotalCount, len(res.Items), res.Truncated)
	}
}
