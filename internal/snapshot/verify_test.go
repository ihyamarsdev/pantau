package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

// testKDFIterations keeps unit tests fast; production always uses kdfIter.
const testKDFIterations = 1_000

func sampleVerifyPayload() *store.SnapshotPayload {
	return &store.SnapshotPayload{
		Version:      1,
		CreatedAt:    time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC),
		Settings:     map[string]string{"telegram_token": "must-not-leak"},
		Hosts:        []store.Host{{Name: "web-01", Host: "10.0.0.1", Port: 22, User: "root"}, {Name: "db-01", Host: "10.0.0.2", Port: 22, User: "root"}},
		DesiredRules: []store.DesiredRule{{}, {}, {}},
		VPNNetworks:  []store.VPNNetwork{{Name: "mesh"}},
	}
}

func TestVerifySnapshot(t *testing.T) {
	t.Parallel()
	valid, err := encryptWith(sampleVerifyPayload(), "correct horse", testKDFIterations)
	if err != nil {
		t.Fatal(err)
	}
	truncated := valid[:len(valid)-10]
	flipped := bytes.Clone(valid)
	flipped[len(flipped)/2] ^= 0xFF
	badMagic := append([]byte("NOT_PANTAU\n"), valid[len(MagicHeader):]...)
	noPayload, err := encryptWith(&store.SnapshotPayload{Version: 0}, "correct horse", testKDFIterations)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		data       []byte
		passphrase string
		wantErr    error
		wantAnyErr bool
		wantHosts  int
	}{
		{name: "valid snapshot", data: valid, passphrase: "correct horse", wantHosts: 2},
		{name: "wrong passphrase", data: valid, passphrase: "wrong", wantErr: ErrPassphrase},
		{name: "blank passphrase rejected", data: valid, passphrase: "  ", wantAnyErr: true},
		{name: "truncated ciphertext", data: truncated, passphrase: "correct horse", wantErr: ErrPassphrase},
		{name: "flipped byte in body", data: flipped, passphrase: "correct horse", wantErr: ErrPassphrase},
		{name: "shorter than header", data: valid[:20], passphrase: "correct horse", wantErr: ErrCorrupt},
		{name: "bad magic header", data: badMagic, passphrase: "correct horse", wantErr: ErrCorrupt},
		{name: "structurally invalid payload", data: noPayload, passphrase: "correct horse", wantErr: ErrInvalidStructure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := verifyWith(tt.data, tt.passphrase, testKDFIterations)
			if tt.wantAnyErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got err %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Counts.Hosts != tt.wantHosts || res.Counts.DesiredRules != 3 || res.Counts.VPNNetworks != 1 {
				t.Errorf("counts = %+v", res.Counts)
			}
			if !res.CreatedAt.Equal(sampleVerifyPayload().CreatedAt) {
				t.Errorf("timestamp = %v", res.CreatedAt)
			}
			raw, _ := json.Marshal(res)
			if bytes.Contains(raw, []byte("must-not-leak")) {
				t.Error("verify result leaked a setting value")
			}
		})
	}
}

// TestVerifyLeavesLiveDatabaseUnchanged proves verification is read-only against the active database.
func TestVerifyLeavesLiveDatabaseUnchanged(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.CreateHost(&store.Host{Name: "live", Host: "10.1.1.1", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}
	snapshotBytes := func() []byte {
		hosts, err := db.ListHosts()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(hosts)
		return raw
	}
	before := snapshotBytes()

	data, err := encryptWith(sampleVerifyPayload(), "pw", testKDFIterations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWith(data, "pw", testKDFIterations); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, snapshotBytes()) {
		t.Fatal("verification modified the live hosts table")
	}
}

// TestVerifyProductionParameters exercises the real PBKDF2 cost once for compatibility with files Pantau writes.
func TestVerifyProductionParameters(t *testing.T) {
	t.Parallel()
	data, err := Encrypt(sampleVerifyPayload(), "prod-pass")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Verify(data, "prod-pass")
	if err != nil {
		t.Fatalf("production verify failed: %v", err)
	}
	if res.Counts.Hosts != 2 {
		t.Errorf("hosts = %d", res.Counts.Hosts)
	}
	if _, err := Verify(data, "wrong"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: got %v", err)
	}
}

// FuzzVerifySnapshot ensures random or damaged input never panics.
func FuzzVerifySnapshot(f *testing.F) {
	valid, err := encryptWith(sampleVerifyPayload(), "pw", testKDFIterations)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add([]byte(""))
	f.Add(MagicHeader)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = verifyWith(data, "pw", testKDFIterations)
	})
}
