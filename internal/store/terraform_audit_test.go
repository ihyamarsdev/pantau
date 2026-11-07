package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestTFAuditAppendListTrim(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := tfAuditKeep
	tfAuditKeep = 5
	defer func() { tfAuditKeep = old }()

	ts := time.Now()
	for i := 1; i <= 99; i++ {
		host := "alpha"
		if i%2 == 0 {
			host = "beta"
		}
		if err := db.AppendTFAudit(ts, "INFO", host, fmt.Sprintf("line %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// Not trimmed before the 100th insert.
	if got, _ := db.ListTFAudit("", 1000); len(got) != 99 {
		t.Fatalf("pre-trim rows = %d", len(got))
	}
	if err := db.AppendTFAudit(ts, "WARN", "alpha", "line 100"); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListTFAudit("", 1000)
	if err != nil || len(got) != 5 {
		t.Fatalf("post-trim rows = %d, %v", len(got), err)
	}
	if got[0].Message != "line 100" || got[0].Level != "WARN" || got[4].Message != "line 96" || got[0].Time.IsZero() {
		t.Fatalf("bad order/fields: %+v", got)
	}
	if got, _ := db.ListTFAudit("beta", 10); len(got) != 2 || got[0].Message != "line 98" {
		t.Fatalf("host filter: %+v", got)
	}
	if got, _ := db.ListTFAudit("line 97", 10); len(got) != 1 {
		t.Fatalf("message-token filter: %+v", got)
	}
	if got, _ := db.ListTFAudit("", 2); len(got) != 2 {
		t.Fatalf("limit: %+v", got)
	}
}
