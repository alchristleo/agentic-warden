// Package signing carries the proof that a bundle came from an
// organization's own control plane. A bundle is trusted today because of
// the connection that delivered it; a signature travels with the bytes, so
// the file on disk can be checked long after that connection closed.
package signing

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// PublicKeyPrefix names the algorithm in every place a human or a
// configuration file sees a key, so a key pasted into the wrong field is
// recognisable as one.
const PublicKeyPrefix = "aw-ed25519:"

// Generate makes a new signing key.
func Generate() (ed25519.PrivateKey, error) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("signing: generating a key: %w", err)
	}
	return key, nil
}

// Sign returns the base64 signature over body.
func Sign(key ed25519.PrivateKey, body []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, body))
}

// Verify reports whether sig is key's signature over body. Every failure —
// a malformed signature, the wrong length, the wrong key — is one false, so
// a caller cannot accidentally treat "could not check" as "checked out".
func Verify(key ed25519.PublicKey, body []byte, sig string) bool {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(raw) != ed25519.SignatureSize || len(key) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(key, body, raw)
}

// KeyID names a key in headers, files and doctor output, so a mismatch
// reads as "signed by a key this machine does not trust" rather than the
// much less actionable "bad signature".
func KeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:8])
}

// FormatPublic renders a key for a file or a listing.
func FormatPublic(key ed25519.PublicKey) string {
	return PublicKeyPrefix + base64.StdEncoding.EncodeToString(key)
}

// ParsePublic reads what FormatPublic wrote, trimming the surrounding
// whitespace a file on disk collects.
func ParsePublic(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, PublicKeyPrefix) {
		return nil, fmt.Errorf("signing: a public key must start with %q", PublicKeyPrefix)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, PublicKeyPrefix))
	if err != nil {
		return nil, fmt.Errorf("signing: decoding the public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("signing: a public key is %d bytes, not %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// LoadSeed reads a private key from a file that only its owner may read.
// The permission check is part of the contract: a signing key another user
// can read is a signing key the organization no longer controls.
func LoadSeed(path string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("signing: reading the signing key: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("signing: %s is readable by group or other (mode %04o); it must be 0600", path, info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("signing: reading the signing key: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("signing: decoding %s: %w", path, err)
	}
	if len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("signing: %s holds %d bytes; an Ed25519 seed is %d", path, len(raw), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

// WriteSeed stores key at path, refusing to replace a key that is already
// there: overwriting one silently would strand every machine that pinned it.
func WriteSeed(path string, key ed25519.PrivateKey) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("signing: %s already holds a signing key", path)
		}
		return fmt.Errorf("signing: creating %s: %w", path, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(key.Seed())); err != nil {
		return fmt.Errorf("signing: writing %s: %w", path, err)
	}
	return nil
}
