package sshrunner

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

type memHostKeys struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memHostKeys) GetTrustedHostKey(addr string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[addr]
	return v, ok, nil
}

func (s *memHostKeys) InsertTrustedHostKey(addr, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[addr]; ok {
		return false, nil
	}
	s.m[addr] = key
	return true, nil
}

func (s *memHostKeys) DeleteTrustedHostKey(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, addr)
	return nil
}

func newEd25519Key(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// resetTOFU isolates the package-level trust state for one test.
func resetTOFU(t *testing.T) {
	t.Helper()
	tofuMu.Lock()
	tofuKnownHosts = make(map[string]ssh.PublicKey)
	tofuMu.Unlock()
	t.Cleanup(func() {
		SetHostKeyStore(nil)
		tofuMu.Lock()
		tofuKnownHosts = make(map[string]ssh.PublicKey)
		tofuMu.Unlock()
	})
}

func TestTOFUTrustSurvivesRestart(t *testing.T) {
	resetTOFU(t)
	store := &memHostKeys{m: map[string]string{}}
	SetHostKeyStore(store)
	cb := TOFUHostKeyCallback()
	addr := "10.0.0.5:22"
	key := newEd25519Key(t)
	if err := cb(addr, &net.TCPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 22}, key); err != nil {
		t.Fatalf("first connection refused: %v", err)
	}

	// Simulate a restart: the in-memory map is empty, the store still knows the host.
	tofuMu.Lock()
	tofuKnownHosts = make(map[string]ssh.PublicKey)
	tofuMu.Unlock()

	if err := cb(addr, nil, key); err != nil {
		t.Fatalf("same key after restart refused: %v", err)
	}
	if err := cb(addr, nil, newEd25519Key(t)); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("different key after restart accepted: %v", err)
	}
}

func TestTOFUForgetHostKeyAllowsReinstalledHost(t *testing.T) {
	resetTOFU(t)
	store := &memHostKeys{m: map[string]string{}}
	SetHostKeyStore(store)
	cb := TOFUHostKeyCallback()
	addr := "10.0.0.6:22"
	if err := cb(addr, nil, newEd25519Key(t)); err != nil {
		t.Fatal(err)
	}
	if err := ForgetHostKey(addr); err != nil {
		t.Fatal(err)
	}
	if err := cb(addr, nil, newEd25519Key(t)); err != nil {
		t.Fatalf("reinstalled host refused after reset: %v", err)
	}
}

// A key that cannot be recorded must not be trusted for this process only.
type failingStore struct{ memHostKeys }

func (s *failingStore) InsertTrustedHostKey(addr, key string) (bool, error) {
	return false, errTestStore
}

var errTestStore = errors.New("disk full")

func TestTOFURefusesWhenKeyCannotBeRecorded(t *testing.T) {
	resetTOFU(t)
	SetHostKeyStore(&failingStore{memHostKeys{m: map[string]string{}}})
	if err := TOFUHostKeyCallback()("10.0.0.7:22", nil, newEd25519Key(t)); err == nil {
		t.Fatal("connection accepted although the host key could not be recorded")
	}
}
