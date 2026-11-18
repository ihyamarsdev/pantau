package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrCorrupt is returned whenever a ciphertext is malformed, truncated,
// tampered with, or decrypted with the wrong key.
var ErrCorrupt = errors.New("invalid or corrupted vault ciphertext")

var magicHeader = []byte("PANTAU_VAULT_V1\n")

const (
	nonceLen = 12
	keyLen   = 32
)

// NewMasterKey returns a fresh 32-byte cryptographically random master key.
func NewMasterKey() ([]byte, error) {
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	return key, nil
}

// MasterKeyToB64 encodes a master key using standard base64.
func MasterKeyToB64(k []byte) string {
	return base64.StdEncoding.EncodeToString(k)
}

// MasterKeyFromB64 decodes a standard-base64 master key and validates its
// length.
func MasterKeyFromB64(s string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(k) != keyLen {
		return nil, fmt.Errorf("invalid master key length: got %d, want %d", len(k), keyLen)
	}
	return k, nil
}

// Cipher is an AES-256-GCM envelope cipher for vault values.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a 32-byte master key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("vault master key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Cipher{aead: gcm}, nil
}

// Encrypt seals plaintext into the deterministic envelope layout:
//
//	magicHeader | 12-byte random nonce | AES-256-GCM sealed plaintext
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, plaintext, nil)

	buf := make([]byte, 0, len(magicHeader)+nonceLen+len(sealed))
	buf = append(buf, magicHeader...)
	buf = append(buf, nonce...)
	buf = append(buf, sealed...)
	return buf, nil
}

// Decrypt opens a vault envelope. Any corruption or authentication failure is
// reported as ErrCorrupt so callers never distinguish failure modes.
func (c *Cipher) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) < len(magicHeader)+nonceLen || !bytes.HasPrefix(blob, magicHeader) {
		return nil, ErrCorrupt
	}
	nonce := blob[len(magicHeader) : len(magicHeader)+nonceLen]
	ciphertext := blob[len(magicHeader)+nonceLen:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plaintext, nil
}
