package credential_test

import (
	"encoding/hex"
	"testing"

	"github.com/acme/agent-wrapper/internal/credential"
)

func TestNewReturnsAPlainCredentialAndItsHash(t *testing.T) {
	plain, hash, err := credential.New()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) < 40 {
		t.Errorf("plain credential %q is too short for 32 random bytes", plain)
	}
	if hash != credential.Hash(plain) {
		t.Errorf("hash %q does not match Hash(plain) %q", hash, credential.Hash(plain))
	}
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 {
		t.Errorf("hash %q is not hex SHA-256", hash)
	}
}

func TestNewIsUnpredictable(t *testing.T) {
	a, _, _ := credential.New()
	b, _, _ := credential.New()
	if a == b {
		t.Error("two credentials are identical")
	}
}

func TestNewIDIsHexAndUnique(t *testing.T) {
	a, err := credential.NewID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := credential.NewID()
	if _, err := hex.DecodeString(a); err != nil || len(a) != 32 {
		t.Errorf("id %q is not 16 hex bytes", a)
	}
	if a == b {
		t.Error("two ids are identical")
	}
}
