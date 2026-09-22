package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// rolloverPrefix versions the statement. A machine that meets a statement
// it does not recognise must ignore it rather than guess at its meaning,
// and a version line is how it tells.
const rolloverPrefix = "aw-key-rollover-v1"

// Rollover is a control plane's announcement, signed by the key a machine
// already trusts, that it has begun signing with a new one.
type Rollover struct {
	// KeyID is the new key's ID, as the statement declares it.
	KeyID string
	// PublicKey is the new key itself.
	PublicKey ed25519.PublicKey
}

// SignRollover builds the header value announcing next, signed by previous.
// A header value holds no newlines, so the statement travels base64 encoded
// with its signature beside it.
func SignRollover(previous ed25519.PrivateKey, next ed25519.PublicKey) string {
	statement := rolloverStatement(next)
	return base64.StdEncoding.EncodeToString([]byte(statement)) + " " + Sign(previous, []byte(statement))
}

// VerifyRollover checks a rollover header against the key this machine has
// pinned. Only a statement the pinned key signed can move the pin, so trust
// chains back to the key pinned at enrollment.
func VerifyRollover(pinned ed25519.PublicKey, header string) (Rollover, error) {
	fields := strings.Split(strings.TrimSpace(header), " ")
	if len(fields) != 2 {
		return Rollover{}, errors.New("signing: a rollover header holds a statement and a signature")
	}
	statement, err := base64.StdEncoding.DecodeString(fields[0])
	if err != nil {
		return Rollover{}, fmt.Errorf("signing: decoding the rollover statement: %w", err)
	}
	if !Verify(pinned, statement, fields[1]) {
		return Rollover{}, errors.New("signing: the rollover statement is not signed by the pinned key")
	}
	lines := strings.Split(strings.TrimSpace(string(statement)), "\n")
	if len(lines) != 3 || lines[0] != rolloverPrefix {
		return Rollover{}, fmt.Errorf("signing: unrecognised rollover statement %q", lines[0])
	}
	key, err := ParsePublic(lines[2])
	if err != nil {
		return Rollover{}, err
	}
	// The ID is a convenience for humans and listings; the key is the fact.
	// A statement where they disagree is malformed, and repinning from it
	// would leave the machine reporting an ID it is not using.
	if lines[1] != KeyID(key) {
		return Rollover{}, errors.New("signing: the rollover statement's key ID does not match its key")
	}
	return Rollover{KeyID: lines[1], PublicKey: key}, nil
}

func rolloverStatement(next ed25519.PublicKey) string {
	return rolloverPrefix + "\n" + KeyID(next) + "\n" + FormatPublic(next)
}
