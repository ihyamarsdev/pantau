package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// withShortMCPTimeouts shortens the live and diagnose deadlines for one test. These tests are not parallel,
// so they never overlap with a reader of the variables.
func withShortMCPTimeouts(t *testing.T, d time.Duration) {
	t.Helper()
	oldLive, oldDiag := mcpLiveTimeout, mcpDiagnoseTimeout
	mcpLiveTimeout, mcpDiagnoseTimeout = d, d
	t.Cleanup(func() { mcpLiveTimeout, mcpDiagnoseTimeout = oldLive, oldDiag })
}

func TestMCPK8sOverviewIncludeLiveDegrades(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	withShortMCPTimeouts(t, 80*time.Millisecond)

	mk := func(name string) int64 {
		id, err := db.CreateKubeCluster(&store.KubeCluster{
			Name: name, Context: "ctx", Kubeconfig: "apiVersion: v1\nkind: Config\n", KubeMode: "full", Provider: "eks",
		})
		if err != nil {
			t.Fatalf("CreateKubeCluster(%s): %v", name, err)
		}
		return id
	}
	healthyID, offlineID, hungID := mk("healthy"), mk("offline"), mk("hung")

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	old := mcpKubeHealthLive
	mcpKubeHealthLive = func(_ *Server, target int64) (*inspector.KubeClusterHealth, error) {
		switch -target {
		case healthyID:
			return &inspector.KubeClusterHealth{PodTotal: 3, UnhealthyTotal: 1}, nil
		case offlineID:
			return nil, errors.New("kubectl: connection refused")
		default:
			<-release // a cluster that never answers: the deadline must cut it off
			return nil, errors.New("unreachable")
		}
	}
	t.Cleanup(func() { mcpKubeHealthLive = old })

	out, err := mcpGetK8sOverview(srv, context.Background(), map[string]any{"include_live": true})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	raw, _ := json.Marshal(out)
	var got struct {
		Clusters []struct {
			ID        int64 `json:"id"`
			Live      *inspector.KubeClusterHealth
			LiveError string `json:"live_error"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Clusters) != 3 {
		t.Fatalf("clusters = %d, want 3", len(got.Clusters))
	}
	byID := map[int64]struct {
		live bool
		err  string
	}{}
	for _, c := range got.Clusters {
		byID[c.ID] = struct {
			live bool
			err  string
		}{c.Live != nil, c.LiveError}
	}
	if h := byID[healthyID]; !h.live || h.err != "" {
		t.Errorf("healthy cluster: live=%v err=%q, want live data and no error", h.live, h.err)
	}
	if o := byID[offlineID]; o.live || !strings.Contains(o.err, "connection refused") {
		t.Errorf("offline cluster: live=%v err=%q, want error text", o.live, o.err)
	}
	if h := byID[hungID]; h.live || !strings.Contains(h.err, "did not finish") {
		t.Errorf("hung cluster: live=%v err=%q, want deadline error", h.live, h.err)
	}
}

func TestMCPK8sOverviewWithoutLiveSkipsQueries(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	if _, err := db.CreateKubeCluster(&store.KubeCluster{
		Name: "c", Context: "ctx", Kubeconfig: "apiVersion: v1\nkind: Config\n", KubeMode: "full", Provider: "eks",
	}); err != nil {
		t.Fatal(err)
	}
	old := mcpKubeHealthLive
	mcpKubeHealthLive = func(*Server, int64) (*inspector.KubeClusterHealth, error) {
		t.Error("live query must not run without include_live")
		return nil, nil
	}
	t.Cleanup(func() { mcpKubeHealthLive = old })

	out, err := mcpGetK8sOverview(srv, context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), `"live`) {
		t.Fatalf("default overview must not carry live fields: %s", raw)
	}
}

func TestMCPWithinHonoursDeadline(t *testing.T) {
	withShortMCPTimeouts(t, 50*time.Millisecond)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	tests := []struct {
		name    string
		fn      func() (any, error)
		wantErr string
		wantVal string
	}{
		{name: "fast answer passes through", fn: func() (any, error) { return "ok", nil }, wantVal: "ok"},
		{name: "errors pass through", fn: func() (any, error) { return nil, errors.New("boom") }, wantErr: "boom"},
		{name: "slow call is cut off", fn: func() (any, error) { <-release; return "late", nil }, wantErr: "did not finish"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := mcpWithin(context.Background(), mcpDiagnoseTimeout, tt.fn)
			switch {
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
			case err != nil:
				t.Fatalf("unexpected err: %v", err)
			default:
				if v != tt.wantVal {
					t.Fatalf("value = %v, want %v", v, tt.wantVal)
				}
			}
		})
	}
}

func TestMCPDiagnoseToolsRejectBadArguments(t *testing.T) {
	srv, _, _ := mcpTestServer(t)
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(map[string]any) (any, error)
		args map[string]any
	}{
		{name: "k8s pod without cluster_id", run: func(a map[string]any) (any, error) { return mcpDiagnoseK8sPod(srv, ctx, a) },
			args: map[string]any{"namespace": "app", "pod": "web-1"}},
		{name: "iac kind outside the allowed list", run: func(a map[string]any) (any, error) { return mcpDiagnoseIaC(srv, ctx, a) },
			args: map[string]any{"kind": "apply", "run_id": float64(1)}},
		{name: "iac without run_id", run: func(a map[string]any) (any, error) { return mcpDiagnoseIaC(srv, ctx, a) },
			args: map[string]any{"kind": "ansible"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.run(tt.args); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestMCPDiagnoseToolsAreRegisteredReadOnly(t *testing.T) {
	for _, name := range []string{"pantau_diagnose_k8s_pod", "pantau_diagnose_docker", "pantau_diagnose_iac"} {
		tool, ok := mcpToolByName(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if tool.run == nil {
			t.Fatalf("%s has no handler", name)
		}
	}
}
