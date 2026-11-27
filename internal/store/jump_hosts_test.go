package store

import (
	"errors"
	"testing"
)

// newJumpHost stores a Host reached through bastion jumpID (0 for a direct Host) and returns its ID.
func newJumpHost(t *testing.T, db *DB, name string, jumpID int64) int64 {
	t.Helper()
	id, err := db.CreateHost(&Host{Name: name, Host: name + ".internal", Port: 22, User: "root", JumpHostID: jumpID})
	if err != nil {
		t.Fatalf("create %s via %d: %v", name, jumpID, err)
	}
	return id
}

// setJump points an existing Host at a new bastion, going through UpdateHost like the API does.
func setJump(db *DB, id, jumpID int64) error {
	h, err := db.GetHost(id)
	if err != nil || h == nil {
		return errors.New("host lookup failed")
	}
	h.JumpHostID = jumpID
	return db.UpdateHost(h)
}

// Every loop, self-jump and over-long chain is refused on both create and update; valid chains are accepted.
func TestJumpHostCycleDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		build   func(t *testing.T, db *DB) (hostID, jumpID int64) // the update to attempt
		wantErr bool
	}{
		{
			name: "self jump is refused",
			build: func(t *testing.T, db *DB) (int64, int64) {
				a := newJumpHost(t, db, "a", 0)
				return a, a
			},
			wantErr: true,
		},
		{
			name: "two-hop loop A to B to A is refused",
			build: func(t *testing.T, db *DB) (int64, int64) {
				a := newJumpHost(t, db, "a", 0)
				b := newJumpHost(t, db, "b", a)
				return a, b
			},
			wantErr: true,
		},
		{
			name: "three-hop loop A to B to C to A is refused",
			build: func(t *testing.T, db *DB) (int64, int64) {
				a := newJumpHost(t, db, "a", 0)
				b := newJumpHost(t, db, "b", a)
				c := newJumpHost(t, db, "c", b)
				return a, c
			},
			wantErr: true,
		},
		{
			name: "valid chain with two bastions is accepted",
			build: func(t *testing.T, db *DB) (int64, int64) {
				a := newJumpHost(t, db, "a", 0)
				b := newJumpHost(t, db, "b", a)
				c := newJumpHost(t, db, "c", 0)
				return c, b
			},
		},
		{
			name: "chain of exactly MaxJumpHops bastions is accepted",
			build: func(t *testing.T, db *DB) (int64, int64) {
				prev := newJumpHost(t, db, "h0", 0)
				for i := 1; i < MaxJumpHops; i++ {
					prev = newJumpHost(t, db, "h"+string(rune('0'+i)), prev)
				}
				target := newJumpHost(t, db, "target", 0)
				return target, prev
			},
		},
		{
			name: "chain over MaxJumpHops bastions is refused",
			build: func(t *testing.T, db *DB) (int64, int64) {
				prev := newJumpHost(t, db, "h0", 0)
				for i := 1; i <= MaxJumpHops; i++ {
					prev = newJumpHost(t, db, "h"+string(rune('0'+i)), prev)
				}
				target := newJumpHost(t, db, "target", 0)
				return target, prev
			},
			wantErr: true,
		},
		{
			name: "missing bastion is refused",
			build: func(t *testing.T, db *DB) (int64, int64) {
				a := newJumpHost(t, db, "a", 0)
				return a, 9999
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := openTestDB(t)
			hostID, jumpID := tt.build(t, db)
			err := setJump(db, hostID, jumpID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("setJump(%d, %d) error = %v, wantErr %v", hostID, jumpID, err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrJumpHostInvalid) {
				t.Fatalf("error = %v, want ErrJumpHostInvalid", err)
			}
		})
	}
}

// CreateHost applies the same rules, with no ID yet: a bastion that does not exist is refused.
func TestJumpHostCreateValidation(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	_, err := db.CreateHost(&Host{Name: "orphan", Host: "10.0.0.9", Port: 22, User: "root", JumpHostID: 404})
	if !errors.Is(err, ErrJumpHostInvalid) {
		t.Fatalf("create via missing bastion error = %v, want ErrJumpHostInvalid", err)
	}
}

// JumpChain returns the bastions in dial order: the first is reached directly, the last one is nearest the Host.
func TestJumpChainDialOrder(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	first := newJumpHost(t, db, "first", 0)
	second := newJumpHost(t, db, "second", first)
	target := newJumpHost(t, db, "target", second)

	h, err := db.GetHost(target)
	if err != nil || h == nil {
		t.Fatalf("GetHost: %v", err)
	}
	chain, err := db.JumpChain(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 2 || chain[0].ID != first || chain[1].ID != second {
		t.Fatalf("chain = %v, want [first second]", chainNames(chain))
	}

	direct, err := db.JumpChain(&Host{})
	if err != nil || len(direct) != 0 {
		t.Fatalf("direct host chain = %v, %v; want empty", chainNames(direct), err)
	}
}

func chainNames(hs []*Host) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Name)
	}
	return out
}
