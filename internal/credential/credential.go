// Package credential mints the secrets the control plane hands out and the
// hashes it stores.
//
// A credential is shown once, at enrollment, and only its hash is kept, so a
// copy of the database does not yield a working credential. Comparing hashes
// rather than plaintext also means the lookup key is safe to log.
package credential

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// New returns a fresh credential and the hash to store for it.
func New() (plain, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("credential: reading random bytes: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, Hash(plain), nil
}

// Hash returns the hex SHA-256 of a plaintext credential, which is what the
// store keeps and what a lookup uses.
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// NewID returns a random identifier for a stored object.
func NewID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("credential: reading random bytes: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
