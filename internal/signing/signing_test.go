package signing

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"3"}`)
	sig := Sign(key, body)
	if !Verify(pub, body, sig) {
		t.Fatal("a fresh signature did not verify")
	}
	if Verify(pub, []byte(`{"version":"4"}`), sig) {
		t.Fatal("a signature verified over a different body")
	}
	if Verify(pub, body, sig[:len(sig)-4]) {
		t.Fatal("a truncated signature verified")
	}
	if Verify(pub, body, "not base64!") {
		t.Fatal("a malformed signature verified")
	}
	other, _ := Generate()
	if Verify(other.Public().(ed25519.PublicKey), body, sig) {
		t.Fatal("a signature verified under the wrong key")
	}
}

func TestKeyIDIsStableAndSixteenHex(t *testing.T) {
	key, _ := Generate()
	pub := key.Public().(ed25519.PublicKey)
	id := KeyID(pub)
	if len(id) != 16 {
		t.Fatalf("KeyID = %q, want 16 hex characters", id)
	}
	if again := KeyID(pub); again != id {
		t.Fatalf("KeyID is not stable: %q then %q", id, again)
	}
	other, _ := Generate()
	if KeyID(other.Public().(ed25519.PublicKey)) == id {
		t.Fatal("two keys share a key ID")
	}
}

func TestPublicKeyRoundTrip(t *testing.T) {
	key, _ := Generate()
	pub := key.Public().(ed25519.PublicKey)
	text := FormatPublic(pub)
	if !strings.HasPrefix(text, PublicKeyPrefix) {
		t.Fatalf("FormatPublic = %q, want the %q prefix", text, PublicKeyPrefix)
	}
	back, err := ParsePublic(text)
	if err != nil {
		t.Fatalf("ParsePublic: %v", err)
	}
	if !back.Equal(pub) {
		t.Fatal("the parsed key differs from the formatted one")
	}
	for _, bad := range []string{"", "aw-ed25519:", "ssh-ed25519:AAAA", "aw-ed25519:!!!", "aw-ed25519:AAAA"} {
		if _, err := ParsePublic(bad); err == nil {
			t.Fatalf("ParsePublic(%q) succeeded", bad)
		}
	}
}

func TestLoadSeedRefusesAReadableFile(t *testing.T) {
	dir := t.TempDir()
	key, _ := Generate()
	path := filepath.Join(dir, "seed")
	if err := WriteSeed(path, key); err != nil {
		t.Fatalf("WriteSeed: %v", err)
	}
	loaded, err := LoadSeed(path)
	if err != nil {
		t.Fatalf("LoadSeed: %v", err)
	}
	if !loaded.Equal(key) {
		t.Fatal("the loaded key differs from the written one")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if _, err := LoadSeed(path); err == nil {
		t.Fatal("LoadSeed accepted a world-readable seed")
	}
	if _, err := LoadSeed(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("LoadSeed accepted a missing file")
	}
	if err := os.WriteFile(filepath.Join(dir, "short"), []byte("AAAA\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadSeed(filepath.Join(dir, "short")); err == nil {
		t.Fatal("LoadSeed accepted a seed of the wrong length")
	}
}

func TestWriteSeedDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seed")
	key, _ := Generate()
	if err := WriteSeed(path, key); err != nil {
		t.Fatalf("WriteSeed: %v", err)
	}
	if err := WriteSeed(path, key); err == nil {
		t.Fatal("WriteSeed overwrote an existing seed")
	}
}
