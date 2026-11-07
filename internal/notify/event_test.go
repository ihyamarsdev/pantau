package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEvent_NoChannelsIsNoop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	d := New(db)
	d.SendEvent("src", "summary", "detail") // must not panic or block
	if got := d.SendEventSync("src", "summary", "detail"); len(got) != 0 {
		t.Fatalf("expected no results, got %+v", got)
	}
	var nilD *Dispatcher
	nilD.SendEvent("a", "b", "c")
}

func TestEvent_SendsToWebhookAndNtfy(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	got := make(chan recordedReq, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- recordedReq{Method: r.URL.Path, Header: r.Header, Body: string(b)}
	}))
	defer ts.Close()
	_ = db.SetSetting("webhook_url", ts.URL+"/hook")
	_ = db.SetSetting("ntfy_url", ts.URL+"/ntfy")
	_ = db.SetSetting("ntfy_token", "tok")

	New(db).SendEvent("Terraform web (dev)", "Plan awaiting approval", "run 7 +1 ~0 -0")

	seen := map[string]recordedReq{}
	for len(seen) < 2 {
		select {
		case r := <-got:
			seen[r.Method] = r
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout, got %v", seen)
		}
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(seen["/hook"].Body), &p); err != nil {
		t.Fatal(err)
	}
	if p["event"] != "terraform_event" || p["source"] != "Terraform web (dev)" || p["summary"] != "Plan awaiting approval" || p["detail"] != "run 7 +1 ~0 -0" {
		t.Fatalf("webhook payload: %v", p)
	}
	n := seen["/ntfy"]
	if n.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(n.Header.Get("Title"), "Plan awaiting approval") || !strings.Contains(n.Body, "run 7") {
		t.Fatalf("ntfy request: %+v", n)
	}
}

func TestEvent_SyncReportsFailureWithoutLeakingURL(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	_ = db.SetSetting("webhook_url", ts.URL+"/secret-path")
	res := New(db).SendEventSync("s", "x", "")
	if len(res) != 1 || res[0].OK || res[0].Error != "status 500" {
		t.Fatalf("unexpected: %+v", res)
	}
	ts.Close()
	res = New(db).SendEventSync("s", "x", "")
	if len(res) != 1 || res[0].OK || strings.Contains(res[0].Error, "secret-path") {
		t.Fatalf("transport error must not include the URL: %+v", res)
	}
}
