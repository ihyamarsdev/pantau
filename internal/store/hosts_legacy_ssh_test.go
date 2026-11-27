package store

import (
	"encoding/json"
	"testing"
)

// AllowLegacySSH is opt-in per host: it round-trips through CRUD and the System Snapshot.
func TestHostAllowLegacySSHRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		allow bool
	}{
		{name: "default off", allow: false},
		{name: "opted in", allow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := openTestDB(t)
			id, err := db.CreateHost(&Host{Name: "legacy-box", Host: "10.0.0.5", Port: 22, User: "root", AllowLegacySSH: tt.allow})
			if err != nil {
				t.Fatal(err)
			}
			got, err := db.GetHost(id)
			if err != nil || got == nil {
				t.Fatalf("GetHost: %v %v", got, err)
			}
			if got.AllowLegacySSH != tt.allow {
				t.Fatalf("after create: got %v want %v", got.AllowLegacySSH, tt.allow)
			}

			got.AllowLegacySSH = !tt.allow
			if err := db.UpdateHost(got); err != nil {
				t.Fatal(err)
			}
			again, _ := db.GetHost(id)
			if again.AllowLegacySSH != !tt.allow {
				t.Fatalf("after update: got %v want %v", again.AllowLegacySSH, !tt.allow)
			}

			// Export via JSON, as the encrypted snapshot does, then import into a fresh database.
			payload, err := db.ExportSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var decoded SnapshotPayload
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			dst := openTestDB(t)
			if err := dst.ImportSnapshot(&decoded); err != nil {
				t.Fatal(err)
			}
			imported, err := dst.GetHost(id)
			if err != nil || imported == nil {
				t.Fatalf("imported host missing: %v", err)
			}
			if imported.AllowLegacySSH != !tt.allow {
				t.Fatalf("after snapshot import: got %v want %v", imported.AllowLegacySSH, !tt.allow)
			}
		})
	}
}
