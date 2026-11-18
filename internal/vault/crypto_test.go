package vault_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"pantau/internal/vault"
)

func TestNewMasterKey(t *testing.T) {
	k1, err := vault.NewMasterKey()
	if err != nil {
		t.Fatalf("NewMasterKey: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("master key length = %d, want 32", len(k1))
	}
	k2, err := vault.NewMasterKey()
	if err != nil {
		t.Fatalf("NewMasterKey: %v", err)
	}
	if bytes.Equal(k1, k2) {
		t.Fatal("two master keys are identical; expected random keys")
	}
	if bytes.Equal(k1, make([]byte, 32)) {
		t.Fatal("master key is all zeroes")
	}
}

func TestMasterKeyB64RoundTrip(t *testing.T) {
	key, err := vault.NewMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	enc := vault.MasterKeyToB64(key)
	// std base64 (not URL, not raw) must be used.
	if _, err := base64.StdEncoding.DecodeString(enc); err != nil {
		t.Fatalf("MasterKeyToB64 did not use std base64: %v", err)
	}
	got, err := vault.MasterKeyFromB64(enc)
	if err != nil {
		t.Fatalf("MasterKeyFromB64: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatal("base64 round-trip changed the key")
	}
}

func TestMasterKeyFromB64Invalid(t *testing.T) {
	cases := []string{
		"not-valid-base64!!!",
		base64.StdEncoding.EncodeToString(make([]byte, 31)), // wrong length
		base64.StdEncoding.EncodeToString(make([]byte, 33)),
		"",
	}
	for _, s := range cases {
		if _, err := vault.MasterKeyFromB64(s); err == nil {
			t.Errorf("MasterKeyFromB64(%q) succeeded, want error", s)
		}
	}
}

func TestNewCipherKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33} {
		if _, err := vault.NewCipher(make([]byte, n)); err == nil {
			t.Errorf("NewCipher with %d-byte key succeeded, want error", n)
		}
	}
	if _, err := vault.NewCipher(make([]byte, 32)); err != nil {
		t.Fatalf("NewCipher with 32-byte key: %v", err)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, _ := vault.NewMasterKey()
	c, err := vault.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plaintexts := [][]byte{
		[]byte(""),
		[]byte("s3cr3t"),
		[]byte(strings.Repeat("long secret value ", 100)),
		{0x00, 0x01, 0xff, 0x10},
	}
	for _, pt := range plaintexts {
		blob, err := c.Encrypt(pt)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		// Envelope layout check.
		if !bytes.HasPrefix(blob, []byte("PANTAU_VAULT_V1\n")) {
			t.Errorf("ciphertext missing magic header: %x", blob[:min(len(blob), 16)])
		}
		const overhead = len("PANTAU_VAULT_V1\n") + 12 // header + nonce
		if len(blob) != overhead+len(pt)+16 {          // + GCM tag
			t.Errorf("ciphertext length = %d, want %d", len(blob), overhead+len(pt)+16)
		}
		got, err := c.Decrypt(blob)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("round-trip mismatch: got %q want %q", got, pt)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	key, _ := vault.NewMasterKey()
	c, _ := vault.NewCipher(key)
	a, _ := c.Encrypt([]byte("same plaintext"))
	b, _ := c.Encrypt([]byte("same plaintext"))
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext are identical; nonce may be reused")
	}
}

func TestDecryptCorrupt(t *testing.T) {
	key, _ := vault.NewMasterKey()
	c, _ := vault.NewCipher(key)
	blob, _ := c.Encrypt([]byte("top-secret-value"))

	otherKey, _ := vault.NewMasterKey()
	other, _ := vault.NewCipher(otherKey)

	// Flip a bit in the ciphertext body.
	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0x01
	assertCorrupt(t, c, tampered, "tampered ciphertext")

	// Flip a bit in the nonce.
	tampered = bytes.Clone(blob)
	tampered[len("PANTAU_VAULT_V1\n")] ^= 0x01
	assertCorrupt(t, c, tampered, "tampered nonce")

	// Corrupt the magic header.
	tampered = bytes.Clone(blob)
	tampered[0] = 'X'
	assertCorrupt(t, c, tampered, "tampered header")

	// Truncations.
	for _, n := range []int{0, 1, len("PANTAU_VAULT_V1\n") + 12 - 1, len(blob) - 1} {
		assertCorrupt(t, c, blob[:n], "truncated ciphertext")
	}

	// Wrong key.
	assertCorrupt(t, other, blob, "wrong key")

	// Random garbage.
	assertCorrupt(t, c, []byte("garbage"), "garbage input")
}

func assertCorrupt(t *testing.T, c *vault.Cipher, blob []byte, name string) {
	t.Helper()
	got, err := c.Decrypt(blob)
	if err == nil {
		t.Fatalf("%s: Decrypt succeeded, got %q; want error", name, got)
	}
	if !errors.Is(err, vault.ErrCorrupt) {
		t.Fatalf("%s: error = %v, want ErrCorrupt", name, err)
	}
}
