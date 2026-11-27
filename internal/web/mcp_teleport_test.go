package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/store"
	"pantau/internal/teleport"
)

// addTeleportCluster stores a profile and one cluster from it. dataDir is the teleport data directory the inspector
// reads. The isolated kubeconfig file is written only when withFile is set, so a test controls active or expired.
func addTeleportCluster(t *testing.T, srv *Server, db *store.DB, dataDir string, withFile bool) int64 {
	t.Helper()
	pid, err := db.CreateTeleportProfile(&store.TeleportProfile{Name: "prod", Proxy: "teleport.setoranku.com:443", DefaultPolicy: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	cid, err := db.CreateKubeCluster(&store.KubeCluster{
		Name: "prod-eu", KubeMode: "read-only", SourceType: store.KubeSourceTeleport, TeleportProfileID: pid,
		TeleportKubeName: "prod-eu", KubeContextName: "tc/prod-eu",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.inspector.SetTeleportDataDir(dataDir)
	if withFile {
		path, err := teleport.KubeconfigPath(dataDir, pid)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cid
}

// The overview reports an expired Teleport session as a structured code, with no raw tsh or kubectl output.
func TestMCPK8sOverviewReportsTeleportExpiry(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	addTeleportCluster(t, srv, db, t.TempDir(), false)

	text, isErr := mcpCallTool(t, srv, "pantau_get_k8s_overview", `{"include_live":true}`)
	if isErr {
		t.Fatalf("overview failed as a whole: %s", text)
	}
	var out struct {
		Clusters []struct {
			Name          string `json:"name"`
			LiveError     string `json:"live_error"`
			LiveErrorCode string `json:"live_error_code"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("overview is not JSON: %v", err)
	}
	if len(out.Clusters) != 1 {
		t.Fatalf("clusters = %+v, want one", out.Clusters)
	}
	c := out.Clusters[0]
	if c.LiveErrorCode != teleport.CodeSessionExpired {
		t.Fatalf("live_error_code = %q, want %s", c.LiveErrorCode, teleport.CodeSessionExpired)
	}
	if c.LiveError != mcpTeleportExpiredText {
		t.Fatalf("live_error = %q, want the fixed expiry text", c.LiveError)
	}
}

// Diagnosing a pod on an expired Teleport cluster fails with a structured payload that names the profile.
func TestMCPDiagnoseTeleportExpiry(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	cid := addTeleportCluster(t, srv, db, t.TempDir(), false)

	text, isErr := mcpCallTool(t, srv, "pantau_diagnose_k8s_pod", `{"cluster_id":`+itoa(cid)+`,"namespace":"app","pod":"web-1"}`)
	if !isErr {
		t.Fatalf("expired diagnose should be an error result: %s", text)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("error is not structured JSON: %v (%s)", err, text)
	}
	if payload["live_error_code"] != teleport.CodeSessionExpired || payload["teleport_profile_id"] == nil {
		t.Fatalf("payload = %v, want the expiry code and profile id", payload)
	}
	if strings.Contains(text, "kubectl") || strings.Contains(text, "stack") {
		t.Fatalf("raw command output leaked into the error: %s", text)
	}
}

// With a live session, the diagnosis runs kubectl with --kubeconfig and --context on the argv, and the output keeps
// its redaction. The fake kubectl records its argv so the test can check the flags.
func TestMCPDiagnoseTeleportActiveRedacts(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	dataDir := t.TempDir()
	cid := addTeleportCluster(t, srv, db, dataDir, true)
	argsFile := filepath.Join(t.TempDir(), "argv")

	writeFakeKubectlMCP(t, `
echo "$*" >> '`+argsFile+`'
if [ "$1" = "--kubeconfig" ]; then shift 4; fi
if [ "$1" = "get" ] && [ "$2" = "pod" ]; then
  cat <<'EOF'
{"metadata":{"name":"web-1","namespace":"app","creationTimestamp":"2026-11-26T08:00:00Z"},
 "spec":{"nodeName":"n1","containers":[{"name":"web","image":"nginx:1.27"}]},
 "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false,"restartCount":2,
   "state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"reason":"Error","exitCode":1}}}]}}
EOF
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "events" ]; then echo '{"items":[]}'; exit 0; fi
if [ "$1" = "logs" ]; then
  echo "DB_PASSWORD=dummy-teleport-pw-7"
  exit 0
fi
echo "unhandled: $@" >&2
exit 1
`)

	text, isErr := mcpCallTool(t, srv, "pantau_diagnose_k8s_pod", `{"cluster_id":`+itoa(cid)+`,"namespace":"app","pod":"web-1"}`)
	if isErr {
		t.Fatalf("active diagnose failed: %s", text)
	}
	if !strings.Contains(text, "CrashLoopBackOff") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("output missing diagnosis or redaction: %s", text)
	}
	if strings.Contains(text, "dummy-teleport-pw-7") {
		t.Fatalf("secret leaked through /mcp: %s", text)
	}

	argv, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(argv), "--kubeconfig") || !strings.Contains(string(argv), "profile-") ||
		!strings.Contains(string(argv), "--context tc/prod-eu") {
		t.Fatalf("kubectl argv lacks the isolated flags: %s", argv)
	}
}
