package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

func TestRolloverRoundTrip(t *testing.T) {
	old, _ := Generate()
	next, _ := Generate()
	nextPub := next.Public().(ed25519.PublicKey)
	header := SignRollover(old, nextPub)
	if strings.Count(header, " ") != 1 {
		t.Fatalf("header %q should hold exactly one space", header)
	}
	got, err := VerifyRollover(old.Public().(ed25519.PublicKey), header)
	if err != nil {
		t.Fatalf("VerifyRollover: %v", err)
	}
	if !got.PublicKey.Equal(nextPub) || got.KeyID != KeyID(nextPub) {
		t.Fatalf("VerifyRollover returned %+v, want the next key", got)
	}
}

func TestRolloverRejections(t *testing.T) {
	old, _ := Generate()
	next, _ := Generate()
	stranger, _ := Generate()
	header := SignRollover(old, next.Public().(ed25519.PublicKey))
	fields := strings.Fields(header)

	forged := SignRollover(stranger, next.Public().(ed25519.PublicKey))
	cases := map[string]string{
		"signed by a stranger": forged,
		"no space":             base64.StdEncoding.EncodeToString([]byte("x")),
		"statement not base64": "!!! " + fields[1],
		"signature not base64": fields[0] + " !!!",
		"empty":                "",
		"three fields":         header + " extra",
	}
	for name, value := range cases {
		if _, err := VerifyRollover(old.Public().(ed25519.PublicKey), value); err == nil {
			t.Errorf("VerifyRollover accepted a header %s", name)
		}
	}

	// A statement whose declared key ID does not match its own key is a
	// forgery attempt against a reader that trusts the ID over the bytes.
	statement := "aw-key-rollover-v1\n" + KeyID(stranger.Public().(ed25519.PublicKey)) + "\n" + FormatPublic(next.Public().(ed25519.PublicKey))
	mismatched := base64.StdEncoding.EncodeToString([]byte(statement)) + " " + Sign(old, []byte(statement))
	if _, err := VerifyRollover(old.Public().(ed25519.PublicKey), mismatched); err == nil {
		t.Error("VerifyRollover accepted a statement whose key ID does not match its key")
	}
}
