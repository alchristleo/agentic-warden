# Signed Bundles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every bundle awd serves carries an Ed25519 signature; aw-sync verifies it against a key pinned at enrollment and leaves that proof on disk; aw-policy and the launch adapters refuse to apply a bundle whose signature does not check out.

**Architecture:** A new `internal/signing` package wraps `crypto/ed25519`. awd loads a seed file named by `AWD_SIGNING_KEY` and signs the exact bytes of each 200 on `GET /v1/bundle`, advertising the key in `X-AW-Key-Id` and, during a rotation, a statement in `X-AW-Key-Rollover` signed by the previous key. aw-sync pins the public key in `machine.json` at enrollment, verifies every fetch before it parses anything, and writes the **served bytes verbatim** plus `aw-bundle.json.sig` and `aw-trust.pub` into the state directory. aw-policy and `aw codex` / `aw gemini` verify that state-directory copy before compiling.

**Tech Stack:** Go 1.22, standard library only (`crypto/ed25519`, `crypto/sha256`, `encoding/base64`). No new module dependencies.

**Spec:** `docs/superpowers/specs/2026-09-22-signed-bundles-design.md`

## Global Constraints

- Go 1.22. **Never run `go mod tidy`** — this environment cannot reach the module proxy. No new dependencies; the standard library covers everything here.
- **Never run `go test -race`** — no gcc in this environment.
- Before every commit: `gofmt -l .` (must print nothing), `go vet ./...`, `go build ./...`, `go test ./...`.
- **Build only with `./...`.** A single-package build such as `go build ./cmd/aw-policy` drops a binary in the repo root. Cross-builds are `GOOS=darwin go build ./...` and `GOOS=windows go build ./...`.
- Public keys are written `aw-ed25519:<base64 std encoding>` wherever a human or a config file sees them. Signatures are base64 std encoding, no prefix.
- The key ID is the first 8 bytes of SHA-256 over the 32 raw public key bytes, lowercase hex — 16 characters.
- An unsigned deployment must keep working byte for byte. Every new check is skipped when no key is configured (awd) or pinned (aw-sync) or present (aw-policy).
- Comments explain *why*, never *what*. Match the surrounding files' density and voice.
- End every commit message with:

```
Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S
```

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/signing/signing.go` (create) | Ed25519 sign, verify, key IDs, key encoding, seed loading |
| `internal/signing/rollover.go` (create) | the rollover statement's encoding and verification |
| `internal/signing/signing_test.go`, `rollover_test.go` (create) | unit tests for both |
| `internal/handler/handler.go` (modify) | `Signer` field on `Handler` |
| `internal/handler/bundle.go` (modify) | signature and key headers on 200 |
| `internal/handler/enroll.go` (modify) | `publicKey` and `keyId` in the enroll response |
| `internal/model/model.go` (modify) | `Machine.LastKeyID` |
| `internal/store/*.go`, `migrations/0004_machine_key_id.sql` (modify/create) | persist the key ID a machine last presented |
| `cmd/awd/main.go` (modify) | `awd keygen`, key wiring for `serve`, key ID column in `machines` |
| `internal/sync/client.go` (modify) | `Fetched.Raw/Signature/KeyID`, request key header, verification, rollover |
| `internal/sync/files.go` (modify) | `Machine.PublicKey/KeyID`, `SignatureFile`, `TrustFile` |
| `internal/sync/sync.go` (modify) | write the served bytes and the two new files |
| `internal/policyhelper/helper.go` (modify) | verify before compiling |
| `cmd/aw-policy/main.go` (modify) | resolve the state directory and pass it in |
| `cmd/aw/main.go` (modify) | verify the bundle `aw` compiles from |
| `internal/agent/claude/inspect.go` (modify) | the doctor finding |
| `README.md`, `deploy/aw-sync/README.md` (modify) | signing, rotation runbook, honest limits |

---

### Task 1: The `signing` package

**Files:**
- Create: `internal/signing/signing.go`
- Create: `internal/signing/signing_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `signing.Sign(key ed25519.PrivateKey, body []byte) string`, `signing.Verify(key ed25519.PublicKey, body []byte, sig string) bool`, `signing.KeyID(key ed25519.PublicKey) string`, `signing.FormatPublic(key ed25519.PublicKey) string`, `signing.ParsePublic(s string) (ed25519.PublicKey, error)`, `signing.LoadSeed(path string) (ed25519.PrivateKey, error)`, `signing.WriteSeed(path string, key ed25519.PrivateKey) error`, `signing.Generate() (ed25519.PrivateKey, error)`, and the constant `signing.PublicKeyPrefix = "aw-ed25519:"`.

- [ ] **Step 1: Write the failing tests**

Create `internal/signing/signing_test.go`:

```go
package signing

import (
	"os"
	"path/filepath"
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
```

Add the imports the tests need (`crypto/ed25519`, `strings`) to the import block.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/signing/`
Expected: the package does not compile — `undefined: Generate`.

- [ ] **Step 3: Write `internal/signing/signing.go`**

```go
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
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/signing/`
Expected: PASS. Then `gofmt -l .` and `go vet ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/signing
git commit -m "feat(signing): Ed25519 signing, verification and key handling"
```

---

### Task 2: The rollover statement

**Files:**
- Create: `internal/signing/rollover.go`
- Create: `internal/signing/rollover_test.go`

**Interfaces:**
- Consumes: Task 1's `Sign`, `Verify`, `KeyID`, `FormatPublic`, `ParsePublic`.
- Produces: `signing.Rollover struct { KeyID string; PublicKey ed25519.PublicKey }`, `signing.SignRollover(previous ed25519.PrivateKey, next ed25519.PublicKey) string`, `signing.VerifyRollover(pinned ed25519.PublicKey, header string) (Rollover, error)`.

The header value carries two base64 fields separated by one space: the statement, then the previous key's signature over the statement's bytes. A header holds no newlines, hence the encoding.

- [ ] **Step 1: Write the failing test**

Create `internal/signing/rollover_test.go`:

```go
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
		"signed by a stranger":  forged,
		"no space":              base64.StdEncoding.EncodeToString([]byte("x")),
		"statement not base64":  "!!! " + fields[1],
		"signature not base64":  fields[0] + " !!!",
		"empty":                 "",
		"three fields":          header + " extra",
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
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/signing/ -run Rollover`
Expected: FAIL — `undefined: SignRollover`.

- [ ] **Step 3: Write `internal/signing/rollover.go`**

```go
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
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/signing/`
Expected: PASS. Then `gofmt -l .`, `go vet ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/signing
git commit -m "feat(signing): key rollover statements signed by the outgoing key"
```

---

### Task 3: awd signs the bundle and advertises its key

**Files:**
- Modify: `internal/handler/handler.go` (the `Handler` struct)
- Modify: `internal/handler/bundle.go:59-68` (the response headers)
- Modify: `internal/handler/enroll.go:101` (the enroll response)
- Test: `internal/handler/bundle_test.go`, `internal/handler/enroll_test.go`

**Interfaces:**
- Consumes: Task 1 and 2's `signing` package.
- Produces: `handler.Signer struct { Key ed25519.PrivateKey; Previous ed25519.PrivateKey }` with methods `func (s *Signer) Public() ed25519.PublicKey`, `func (s *Signer) KeyID() string`, and the `Handler.Signer *Signer` field. A nil `Signer` means an unsigned deployment and every new header is omitted.

- [ ] **Step 1: Write the failing tests**

Append to `internal/handler/bundle_test.go` (follow the file's existing helper for building a handler with an enrolled machine; name the new tests as below):

```go
func TestBundleCarriesASignature(t *testing.T) {
	// ... build the handler and an enrolled machine as the neighbouring
	// tests do, then:
	key, _ := signing.Generate()
	h.Signer = &handler.Signer{Key: key}

	rec := fetchBundle(t, h, credential, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	pub := key.Public().(ed25519.PublicKey)
	if got := rec.Header().Get("X-AW-Key-Id"); got != signing.KeyID(pub) {
		t.Fatalf("X-AW-Key-Id = %q, want %q", got, signing.KeyID(pub))
	}
	if !signing.Verify(pub, rec.Body.Bytes(), rec.Header().Get("X-AW-Signature")) {
		t.Fatal("the signature does not verify over the response body")
	}
	if rec.Header().Get("X-AW-Key-Rollover") != "" {
		t.Fatal("a rollover header appeared with no previous key configured")
	}
}

func TestUnsignedDeploymentSendsNoSignatureHeaders(t *testing.T) {
	rec := fetchBundle(t, h, credential, "")
	for _, name := range []string{"X-AW-Signature", "X-AW-Key-Id", "X-AW-Key-Rollover"} {
		if rec.Header().Get(name) != "" {
			t.Errorf("%s was sent by an unsigned deployment", name)
		}
	}
}

func TestNotModifiedCarriesNoSignature(t *testing.T) {
	key, _ := signing.Generate()
	h.Signer = &handler.Signer{Key: key}
	first := fetchBundle(t, h, credential, "")
	second := fetchBundle(t, h, credential, first.Header().Get("ETag"))
	if second.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.Code)
	}
	if second.Header().Get("X-AW-Signature") != "" {
		t.Fatal("a 304 carried a signature; there is no body to sign")
	}
}

func TestRolloverHeaderIsSignedByThePreviousKey(t *testing.T) {
	previous, _ := signing.Generate()
	current, _ := signing.Generate()
	h.Signer = &handler.Signer{Key: current, Previous: previous}
	rec := fetchBundle(t, h, credential, "")
	got, err := signing.VerifyRollover(previous.Public().(ed25519.PublicKey), rec.Header().Get("X-AW-Key-Rollover"))
	if err != nil {
		t.Fatalf("VerifyRollover: %v", err)
	}
	if !got.PublicKey.Equal(current.Public().(ed25519.PublicKey)) {
		t.Fatal("the rollover announces the wrong key")
	}
}
```

In `internal/handler/enroll_test.go`, extend the successful-enrollment test to assert that, with `h.Signer` set, the response body carries `publicKey` equal to `signing.FormatPublic(pub)` and `keyId` equal to `signing.KeyID(pub)`, and that both fields are absent when `h.Signer` is nil.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/handler/`
Expected: FAIL — `undefined: handler.Signer`.

- [ ] **Step 3: Add the `Signer` type and the `Handler` field**

In `internal/handler/handler.go`, after the `AdminToken` field:

```go
	// Signer signs every bundle this server serves, so a machine can check
	// on disk that its policy came from here. Nil is an unsigned
	// deployment: no headers, and every client behaves as it did before
	// signing existed.
	Signer *Signer
```

And, in the same file:

```go
// Signer holds the control plane's signing key, and during a rotation the
// key it is replacing. Previous signs nothing but the rollover statement:
// its only remaining job is to vouch for its successor to machines that
// still pin it.
type Signer struct {
	Key      ed25519.PrivateKey
	Previous ed25519.PrivateKey
}

// Public is the key machines pin.
func (s *Signer) Public() ed25519.PublicKey { return s.Key.Public().(ed25519.PublicKey) }

// KeyID names that key in headers and listings.
func (s *Signer) KeyID() string { return signing.KeyID(s.Public()) }
```

- [ ] **Step 4: Sign the bundle response**

In `internal/handler/bundle.go`, replace the block from `etag := etagOf(body)` through the `WriteHeader(http.StatusOK)` call with:

```go
	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		// A 304 has no body to sign, and the machine still holds the
		// signature it verified when it first received these bytes.
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if h.Signer != nil {
		w.Header().Set("X-AW-Signature", signing.Sign(h.Signer.Key, body))
		w.Header().Set("X-AW-Key-Id", h.Signer.KeyID())
		if h.Signer.Previous != nil {
			w.Header().Set("X-AW-Key-Rollover", signing.SignRollover(h.Signer.Previous, h.Signer.Public()))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
```

- [ ] **Step 5: Return the key at enrollment**

In `internal/handler/enroll.go`, replace the `writeJSON` call at line 101 with:

```go
	response := map[string]any{"machineId": id, "credential": plain, "user": user}
	if h.Signer != nil {
		// The machine pins this key now, while it is talking to a server it
		// has just authenticated to with a single-use token. Everything the
		// machine verifies later chains back to this moment.
		response["publicKey"] = signing.FormatPublic(h.Signer.Public())
		response["keyId"] = h.Signer.KeyID()
	}
	writeJSON(w, http.StatusCreated, response)
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/handler/`
Expected: PASS. Then `gofmt -l .`, `go vet ./...`, `go build ./...`.

- [ ] **Step 7: Commit**

```bash
git add internal/handler
git commit -m "feat(awd): sign bundles and hand machines the public key at enrollment"
```

---

### Task 4: `awd keygen`, key wiring, and the key ID column

**Files:**
- Modify: `cmd/awd/main.go` (subcommand table around line 77-90, the `serve` wiring, and `machines`)
- Modify: `internal/model/model.go:89-104` (`Machine`)
- Modify: `internal/store/store.go:42-43`, `internal/store/memory.go:148`, `internal/store/postgres.go`
- Create: `internal/store/migrations/0004_machine_key_id.sql`
- Modify: `internal/store/storetest/conformance.go`
- Modify: `internal/handler/bundle.go` (pass the key ID through to `TouchMachine`)
- Test: `cmd/awd/e2e_test.go`, `internal/store/storetest/conformance.go`

**Interfaces:**
- Consumes: `signing`, `handler.Signer`.
- Produces: `model.Machine.LastKeyID string`, `store.Store.TouchMachine(ctx, id string, seenAt time.Time, bundleVersion, keyID string) error`, the `awd keygen` subcommand.

Why the column: an operator finishing a rotation needs to know when the last machine has repinned. The machine sends `X-AW-Key-Id` on every fetch; recording it makes `awd machines` the answer.

- [ ] **Step 1: Write the failing store-conformance case**

In `internal/store/storetest/conformance.go`, extend the machine-touch case so it passes a key ID and asserts it is read back:

```go
	if err := s.TouchMachine(ctx, m.ID, seen, "7", "3f9a1c22b0d41e77"); err != nil {
		t.Fatalf("TouchMachine: %v", err)
	}
	got, err := s.Machine(ctx, m.ID)
	if err != nil {
		t.Fatalf("Machine: %v", err)
	}
	if got.LastKeyID != "3f9a1c22b0d41e77" {
		t.Fatalf("LastKeyID = %q, want the key the machine presented", got.LastKeyID)
	}
	// An unsigned deployment sends no key ID, and a machine that stops
	// sending one must not appear to be still pinning the old key.
	if err := s.TouchMachine(ctx, m.ID, seen, "8", ""); err != nil {
		t.Fatalf("TouchMachine: %v", err)
	}
	if got, _ := s.Machine(ctx, m.ID); got.LastKeyID != "" {
		t.Fatalf("LastKeyID = %q, want it cleared", got.LastKeyID)
	}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/store/...`
Expected: FAIL — too many arguments to `TouchMachine`.

- [ ] **Step 3: Widen the model, the interface and both stores**

`internal/model/model.go`, after `LastBundleVersion`:

```go
	// LastKeyID is the signing key the machine presented at that fetch, so
	// an operator can see which machines have picked up a rotation. Empty
	// for a machine that pins no key.
	LastKeyID string `json:"lastKeyId,omitempty"`
```

`internal/store/store.go`:

```go
	// TouchMachine records a successful bundle fetch, or model.ErrNotFound.
	// keyID is the signing key the machine presented, empty when it pins
	// none; it is stored as given, so a machine that stops presenting one
	// stops reporting one.
	TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion, keyID string) error
```

Update `internal/store/memory.go` to assign `m.LastKeyID = keyID`, and `internal/store/postgres.go` to set `last_key_id = $4` (shifting the existing parameter numbering), with the scan in `Machine` and `ListMachines` reading the new column through a `sql.NullString` if that is how the file handles the other optional text columns — follow whatever pattern `last_bundle_version` already uses.

Create `internal/store/migrations/0004_machine_key_id.sql`:

```sql
-- The signing key a machine last presented, so an operator can tell when a
-- key rotation has reached every machine.
ALTER TABLE machines ADD COLUMN IF NOT EXISTS last_key_id text NOT NULL DEFAULT '';
```

In `internal/handler/bundle.go`, pass the header through:

```go
	if err := h.store.TouchMachine(r.Context(), machine.ID, h.Now(), bundle.Version, r.Header.Get("X-AW-Key-Id")); err != nil {
```

- [ ] **Step 4: Run the store and handler tests**

Run: `go test ./internal/store/... ./internal/handler/`
Expected: PASS.

- [ ] **Step 5: Add `awd keygen` and wire the keys into `serve`**

In `cmd/awd/main.go`, add `case "keygen":` to the subcommand switch and a `keygen` function:

```go
// keygen writes a new signing key and prints the public half. It never
// replaces an existing key: a control plane that quietly started signing
// with a different key would strand every machine that pinned the old one.
func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "", "where to write the signing key (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("awd keygen: --out names the file to write")
	}
	key, err := signing.Generate()
	if err != nil {
		return err
	}
	if err := signing.WriteSeed(*out, key); err != nil {
		return err
	}
	pub := key.Public().(ed25519.PublicKey)
	fmt.Printf("wrote %s\npublic key %s\nkey id     %s\n", *out, signing.FormatPublic(pub), signing.KeyID(pub))
	return nil
}
```

In the `serve` path, after the handler is built and `AdminToken` is set:

```go
	// Signing is opt-in: a deployment with no key keeps serving exactly as
	// it did, and every client keeps accepting what it serves.
	if path := os.Getenv("AWD_SIGNING_KEY"); path != "" {
		key, err := signing.LoadSeed(path)
		if err != nil {
			return err
		}
		s := &handler.Signer{Key: key}
		if previous := os.Getenv("AWD_SIGNING_KEY_PREVIOUS"); previous != "" {
			// A rotation that cannot vouch for its new key leaves every
			// machine pinned to a key nothing signs with any more. Refusing
			// to start says so while it is still one server's problem.
			old, err := signing.LoadSeed(previous)
			if err != nil {
				return fmt.Errorf("awd: AWD_SIGNING_KEY_PREVIOUS: %w", err)
			}
			s.Previous = old
		}
		h.Signer = s
		log.Info("signing bundles", "keyId", s.KeyID())
	} else {
		log.Warn("bundles are not signed; set AWD_SIGNING_KEY to sign them")
	}
```

Extend `machines` output with a `KEY` column showing `LastKeyID` or `-`, matching the alignment style already in that function, and add `keygen` to the help text.

- [ ] **Step 6: Extend the awd e2e test**

In `cmd/awd/e2e_test.go`, add a test that runs `awd keygen --out <temp>/key`, asserts exit 0 and that the printed key parses with `signing.ParsePublic`, asserts a second run fails, then starts `serve` with `AWD_SIGNING_KEY` set, enrolls, fetches the bundle and verifies `X-AW-Signature` against the printed key.

- [ ] **Step 7: Run everything**

Run: `go test ./...`, then `gofmt -l .`, `go vet ./...`, `go build ./...`.
Expected: PASS, no output from gofmt.

- [ ] **Step 8: Commit**

```bash
git add cmd/awd internal/model internal/store internal/handler
git commit -m "feat(awd): keygen, signing-key configuration, and the key ID a machine last presented"
```

---

### Task 5: aw-sync verifies the fetch and pins the key

**Files:**
- Modify: `internal/sync/client.go:39-62` (`Enrollment`, `Fetched`), `:64-150` (`Enroll`, `Fetch`)
- Modify: `internal/sync/files.go:36-49` (`Machine`)
- Modify: the `aw-sync enroll` path in `cmd/aw-sync/main.go` so the pinned key reaches `machine.json`
- Test: `internal/sync/client_test.go`

**Interfaces:**
- Consumes: `signing`.
- Produces: `sync.Fetched` gains `Raw []byte`, `Signature string`, `KeyID string`; `sync.Enrollment` gains `PublicKey string`, `KeyID string`; `sync.Machine` gains `PublicKey string` and `KeyID string` (JSON `publicKey`, `keyId`); `Client.Fetch` gains a pinned key: `Fetch(ctx context.Context, credential, etag string, pinned ed25519.PublicKey) (Fetched, error)`, and `Fetched` gains `Rollover *signing.Rollover` so the caller can repin.

- [ ] **Step 1: Write the failing tests**

In `internal/sync/client_test.go`, using the file's existing `httptest` fake server:

```go
func TestFetchVerifiesTheSignature(t *testing.T) {
	key, _ := signing.Generate()
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := signingServer(t, key, body, nil)
	c := &sync.Client{Server: srv.URL}

	got, err := c.Fetch(context.Background(), "cred", "", key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got.Raw, body) {
		t.Fatalf("Raw = %q, want the served bytes verbatim", got.Raw)
	}
	if got.Bundle.Version != "4" {
		t.Fatalf("Version = %q", got.Bundle.Version)
	}
}

func TestFetchRejectsATamperedBody(t *testing.T) {
	key, _ := signing.Generate()
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := signingServerSigningOver(t, key, body, []byte(`{"version":"9","user":"a@b.c","rules":[]}`))
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", key.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch accepted a body the signature does not cover")
	}
}

func TestFetchRequiresASignatureWhenAKeyIsPinned(t *testing.T) {
	key, _ := signing.Generate()
	srv := unsignedServer(t, []byte(`{"version":"4"}`))
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", key.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch accepted an unsigned bundle while a key was pinned")
	}
}

func TestFetchWithNoPinnedKeySkipsVerification(t *testing.T) {
	srv := unsignedServer(t, []byte(`{"version":"4","user":"a@b.c","rules":[]}`))
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetchFollowsARollover(t *testing.T) {
	old, _ := signing.Generate()
	next, _ := signing.Generate()
	body := []byte(`{"version":"5","user":"a@b.c","rules":[]}`)
	srv := signingServer(t, next, body, old) // signs with next, announces via old
	c := &sync.Client{Server: srv.URL}
	got, err := c.Fetch(context.Background(), "cred", "", old.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Rollover == nil || !got.Rollover.PublicKey.Equal(next.Public().(ed25519.PublicKey)) {
		t.Fatal("Fetch did not report the rollover it used")
	}
}

func TestFetchRejectsAForgedRollover(t *testing.T) {
	old, _ := signing.Generate()
	stranger, _ := signing.Generate()
	next, _ := signing.Generate()
	body := []byte(`{"version":"5"}`)
	srv := signingServer(t, next, body, stranger) // announced by a key we never pinned
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", old.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch followed a rollover the pinned key did not sign")
	}
}

func TestFetchSendsThePinnedKeyID(t *testing.T) {
	key, _ := signing.Generate()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-AW-Key-Id")
		body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
		w.Header().Set("X-AW-Signature", signing.Sign(key, body))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(key.Public().(ed25519.PublicKey)))
		w.Write(body)
	}))
	defer srv.Close()
	pub := key.Public().(ed25519.PublicKey)
	if _, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", pub); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if seen != signing.KeyID(pub) {
		t.Fatalf("the request sent X-AW-Key-Id %q, want %q", seen, signing.KeyID(pub))
	}
}
```

Write the three small server helpers (`signingServer`, `signingServerSigningOver`, `unsignedServer`) above the tests; each returns an `*httptest.Server` and registers `t.Cleanup(srv.Close)`. `signingServer(t, signer, body, announcer)` signs `body` with `signer` and, when `announcer` is non-nil, adds `X-AW-Key-Rollover` built with `signing.SignRollover(announcer, signer.Public())`.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/sync/ -run Fetch`
Expected: FAIL — not enough arguments to `Fetch`.

- [ ] **Step 3: Widen `Enrollment`, `Fetched` and `Machine`**

`internal/sync/client.go`, in `Enrollment`:

```go
	// PublicKey is the control plane's bundle signing key, pinned here at
	// the one moment this machine has authenticated to the server with a
	// token an operator minted. Empty when the server does not sign.
	PublicKey string `json:"publicKey,omitempty"`
	// KeyID names that key in listings and headers.
	KeyID string `json:"keyId,omitempty"`
```

In `Fetched`:

```go
	// Raw is the response body exactly as the server wrote it. The
	// signature covers these bytes, so this — not a re-encoding of Bundle —
	// is what goes on disk.
	Raw []byte
	// Signature and KeyID are what the server sent, for the caller to store
	// beside the bundle. Both empty for an unsigned deployment.
	Signature string
	KeyID     string
	// Rollover is the new key this fetch repinned to, or nil. The caller
	// persists it: the verification already happened here.
	Rollover *signing.Rollover
```

`internal/sync/files.go`, in `Machine`:

```go
	// PublicKey is the control plane's signing key, pinned at enrollment.
	// Empty means this machine enrolled before signing existed and verifies
	// nothing — the reason an old machine keeps working.
	PublicKey string `json:"publicKey,omitempty"`
	// KeyID names that key, so `aw doctor` and the control plane's listing
	// agree about which key this machine trusts.
	KeyID string `json:"keyId,omitempty"`
```

Have `Enroll` copy both fields from the response into `Enrollment`, and the `aw-sync enroll` command copy them into the `Machine` it saves.

- [ ] **Step 4: Verify inside `Fetch`**

Replace `Fetch`'s signature and its body from the `payload` read onward:

```go
func (c *Client) Fetch(ctx context.Context, credential, etag string, pinned ed25519.PublicKey) (Fetched, error) {
```

Add, beside the other request headers:

```go
	if pinned != nil {
		// The control plane records this, so an operator can see which
		// machines have picked up a rotation and when it is safe to retire
		// the old key.
		req.Header.Set("X-AW-Key-Id", signing.KeyID(pinned))
	}
```

And after the body is read and length-checked, before `json.Unmarshal`:

```go
	signature := resp.Header.Get("X-AW-Signature")
	var rollover *signing.Rollover
	if pinned != nil {
		verifier := pinned
		// A rollover is the only thing that may move the pin, and only the
		// pinned key can sign one, so trust chains back to enrollment.
		if header := resp.Header.Get("X-AW-Key-Rollover"); header != "" {
			next, err := signing.VerifyRollover(pinned, header)
			if err != nil {
				return Fetched{}, fmt.Errorf("sync: %w", err)
			}
			rollover, verifier = &next, next.PublicKey
		}
		if signature == "" {
			return Fetched{}, errors.New("sync: this machine pins a signing key but the control plane sent no signature")
		}
		if !signing.Verify(verifier, payload, signature) {
			return Fetched{}, fmt.Errorf("sync: the bundle is not signed by key %s", signing.KeyID(verifier))
		}
	}
```

Return `Fetched{Bundle: &bundle, Raw: payload, Signature: signature, KeyID: resp.Header.Get("X-AW-Key-Id"), ETag: resp.Header.Get("ETag"), Rollover: rollover}`.

Note the ordering: verification happens **before** `json.Unmarshal`, so an unverified body is never parsed.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/sync/`
Expected: the `Fetch` tests pass; `sync.Run`'s callers may not compile yet — Task 6 fixes them. If `sync.go` does not compile, make the minimal call-site change (`client.Fetch(ctx, machine.Credential, etag, nil)`) now and do the real work in Task 6.

- [ ] **Step 6: Commit**

```bash
git add internal/sync cmd/aw-sync
git commit -m "feat(aw-sync): verify every fetched bundle against the key pinned at enrollment"
```

---

### Task 6: aw-sync writes the served bytes, the signature and the trust key

**Files:**
- Modify: `internal/sync/sync.go:135` (the fetch call), `:180-190` (the state-directory bundle)
- Modify: `internal/sync/files.go` (new file-name constants)
- Test: `internal/sync/sync_test.go`

**Interfaces:**
- Consumes: Task 5's `Fetched.Raw/Signature/KeyID/Rollover`, `sync.Machine.PublicKey`.
- Produces: `sync.SignatureFile = "aw-bundle.json.sig"`, `sync.TrustFile = "aw-trust.pub"`.

- [ ] **Step 1: Write the failing tests**

In `internal/sync/sync_test.go`:

```go
func TestRunWritesTheServedBytesAndTheSignature(t *testing.T) {
	// Build the fake control plane so the served body is NOT what
	// json.MarshalIndent would produce — compact JSON is enough. The point
	// of the test is that the signature still verifies against the file.
	key, _ := signing.Generate()
	body := []byte(`{"version":"6","user":"a@b.c","rules":[]}`)
	// ... serve body with X-AW-Signature and X-AW-Key-Id, enroll a machine
	// whose machine.json pins key, then run one cycle.

	onDisk, err := os.ReadFile(filepath.Join(stateDir, sync.BundleFile))
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	if !bytes.Equal(onDisk, body) {
		t.Fatalf("the bundle on disk is a re-encoding, not the served bytes:\n%s", onDisk)
	}
	sig, err := os.ReadFile(filepath.Join(stateDir, sync.SignatureFile))
	if err != nil {
		t.Fatalf("reading the signature: %v", err)
	}
	fields := strings.Fields(string(sig))
	if len(fields) != 3 || fields[0] != "aw-ed25519" {
		t.Fatalf("signature file = %q", sig)
	}
	if !signing.Verify(key.Public().(ed25519.PublicKey), onDisk, fields[2]) {
		t.Fatal("the signature on disk does not verify over the bundle on disk")
	}
	trust, err := os.ReadFile(filepath.Join(stateDir, sync.TrustFile))
	if err != nil {
		t.Fatalf("reading the trust key: %v", err)
	}
	if strings.TrimSpace(string(trust)) != signing.FormatPublic(key.Public().(ed25519.PublicKey)) {
		t.Fatalf("trust file = %q", trust)
	}
	state, _ := sync.LoadState(stateDir)
	for _, name := range []string{sync.BundleFile, sync.SignatureFile, sync.TrustFile} {
		if _, ok := state.Files[filepath.Join(stateDir, name)]; !ok {
			t.Errorf("%s is not recorded in state.json, so drift in it goes unnoticed", name)
		}
	}
}

func TestRunWritesNothingWhenVerificationFails(t *testing.T) {
	// Serve a body signed by a different key. Assert Run returns an error,
	// no agent file exists, and neither does the signature file.
}

func TestRunRepinsOnARollover(t *testing.T) {
	// Serve signed by the new key with a rollover announced by the pinned
	// one; assert the cycle succeeds, machine.json now holds the new key
	// and key ID, and the trust file holds the new key.
}

func TestUnsignedDeploymentWritesNeitherNewFile(t *testing.T) {
	// A machine with no pinned key: the cycle succeeds, and neither
	// aw-bundle.json.sig nor aw-trust.pub is written.
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/sync/ -run Run`
Expected: FAIL — `undefined: sync.SignatureFile`, and the bundle on disk is a re-encoding.

- [ ] **Step 3: Add the constants**

In `internal/sync/files.go`, beside `BundleFile`:

```go
	// SignatureFile proves BundleFile came from the control plane. It holds
	// one line: the algorithm, the key ID, and the signature.
	SignatureFile = "aw-bundle.json.sig"
	// TrustFile is the public key BundleFile is signed with. It exists
	// because aw-policy runs as the developer and cannot read machine.json,
	// which is 0600 and holds the machine credential.
	TrustFile = "aw-trust.pub"
```

- [ ] **Step 4: Write the served bytes and the new files**

In `internal/sync/sync.go`, load the pinned key before fetching:

```go
	var pinned ed25519.PublicKey
	if machine.PublicKey != "" {
		key, err := signing.ParsePublic(machine.PublicKey)
		if err != nil {
			return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: the pinned key in machine.json: %w", err))
		}
		pinned = key
	}
	fetched, err := client.Fetch(ctx, machine.Credential, etag, pinned)
```

Replace the state-directory bundle block (the `json.MarshalIndent` of `fetched.Bundle` and the `plannedFile` built from it) with:

```go
	// The signature covers the bytes the server sent, so those bytes go to
	// disk unchanged. Re-encoding here would produce a file no verifier
	// could check.
	planned = append(planned, plannedFile{path: filepath.Join(cfg.StateDir, BundleFile), content: fetched.Raw, mode: 0o644})
	if fetched.Signature != "" && pinned != nil {
		verified := pinned
		if fetched.Rollover != nil {
			verified = fetched.Rollover.PublicKey
		}
		planned = append(planned,
			plannedFile{path: filepath.Join(cfg.StateDir, SignatureFile), content: []byte(fmt.Sprintf("aw-ed25519 %s %s\n", signing.KeyID(verified), fetched.Signature)), mode: 0o644},
			plannedFile{path: filepath.Join(cfg.StateDir, TrustFile), content: []byte(signing.FormatPublic(verified) + "\n"), mode: 0o644},
		)
	}
```

After the writes succeed, persist a rollover:

```go
	if fetched.Rollover != nil {
		// The pin moves only after the cycle's files are on disk: a machine
		// that crashed mid-cycle repins on the next one, from a statement
		// the old key still signs.
		machine.PublicKey = signing.FormatPublic(fetched.Rollover.PublicKey)
		machine.KeyID = fetched.Rollover.KeyID
		if err := SaveMachine(cfg.StateDir, machine); err != nil {
			notes = append(notes, "the new signing key could not be pinned: "+err.Error())
		}
	}
```

Note: `fetched.Raw` may be nil on the 304 path, which returns before any of this.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/sync/ ./...`
Expected: PASS. Then `gofmt -l .`, `go vet ./...`, `go build ./...`, `GOOS=darwin go build ./...`, `GOOS=windows go build ./...`.

- [ ] **Step 6: Commit**

```bash
git add internal/sync
git commit -m "feat(aw-sync): write the served bytes, their signature and the trusted key"
```

---

### Task 7: aw-policy verifies before it compiles

**Files:**
- Modify: `internal/policyhelper/helper.go:59-75` (`Config`) and its run path
- Modify: `cmd/aw-policy/main.go:113` (config construction)
- Test: `internal/policyhelper/helper_test.go`, `cmd/aw-policy/e2e_test.go`

**Interfaces:**
- Consumes: `signing`, `sync.BundleFile/SignatureFile/TrustFile`, `sync.StateDir`.
- Produces: `policyhelper.Config` gains `StateDir string`.

Behaviour, from the spec:
- No `<state dir>/aw-trust.pub`: unsigned deployment — read `BundlePath` and behave exactly as today.
- Trust file present: verify `<state dir>/aw-bundle.json` against `<state dir>/aw-bundle.json.sig` and compile from **that** file. It is a superset of the narrowed Claude copy, so the compiled settings do not change.
- Verification fails or the signature is missing: `{}` envelope, a note naming the file, the audit line, exit 0 — exit 1 under `RequireBundle`.

Do not import `internal/sync` from `internal/policyhelper` if that creates an import cycle; check with `go build ./...`. If it does, have `cmd/aw-policy` pass the three absolute paths in `Config` instead of a directory.

- [ ] **Step 1: Write the failing tests**

In `internal/policyhelper/helper_test.go`, four cases, each with a temporary state directory:

```go
func TestRunCompilesFromTheSignedBundle(t *testing.T)      // good signature: settings applied, no note
func TestRunRefusesATamperedBundle(t *testing.T)           // flipped byte: "{}" envelope, note names the file
func TestRunRefusesAMissingSignature(t *testing.T)         // trust file present, .sig absent: "{}" + note
func TestRequireBundleTurnsAFailureIntoAnError(t *testing.T) // same, RequireBundle: Result reports the failure
func TestNoTrustFileBehavesExactlyAsBefore(t *testing.T)   // unsigned: byte-identical output to the pre-signing path
```

Assert on the envelope's decoded JSON and on the notes, following the assertions the existing tests in this file already make.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/policyhelper/`
Expected: FAIL — `unknown field StateDir`.

- [ ] **Step 3: Implement the check**

Add to `Config`:

```go
	// StateDir is where aw-sync keeps the signed bundle, its signature and
	// the key they are checked against. Empty disables verification, which
	// is what a test aiming at a single bundle file wants.
	StateDir string
```

Before the bundle is read, add a helper in `helper.go`:

```go
// signedBundle returns the path to compile from, and a note when signing is
// configured but the proof does not hold. An unsigned deployment — no trust
// file — returns the configured bundle path and no note, so a machine that
// has never seen a signature behaves exactly as it did before signing
// existed.
func signedBundle(cfg Config) (path string, note string, ok bool) {
	if cfg.StateDir == "" {
		return cfg.BundlePath, "", true
	}
	trust, err := os.ReadFile(filepath.Join(cfg.StateDir, trustFile))
	if err != nil {
		return cfg.BundlePath, "", true
	}
	key, err := signing.ParsePublic(string(trust))
	if err != nil {
		return "", "the trusted key is unreadable: " + err.Error(), false
	}
	bundlePath := filepath.Join(cfg.StateDir, bundleFile)
	body, err := os.ReadFile(bundlePath)
	if err != nil {
		return "", "the signed bundle is unreadable: " + err.Error(), false
	}
	line, err := os.ReadFile(filepath.Join(cfg.StateDir, signatureFile))
	if err != nil {
		return "", "no signature beside " + bundlePath, false
	}
	fields := strings.Fields(string(line))
	if len(fields) != 3 || fields[0] != "aw-ed25519" || !signing.Verify(key, body, fields[2]) {
		return "", bundlePath + " is not signed by key " + signing.KeyID(key), false
	}
	return bundlePath, "", true
}
```

Wire it into the run path: on `ok == false`, emit the `{}` envelope with the note and the audit line, and report the failure the same way a missing bundle with `RequireBundle` already does. On success, read from the returned path.

In `cmd/aw-policy/main.go`, set `StateDir` from `AW_SYNC_STATE_DIR` when set, otherwise `sync.StateDir(runtime.GOOS)` — matching what `aw doctor` already does.

- [ ] **Step 4: Extend the e2e test**

In `cmd/aw-policy/e2e_test.go`, add: a signed state directory produces the repo-specific settings; flipping one byte of the bundle produces `{}` and a note on stderr; with `requireBundle` the same case exits 1; a state directory with no trust file produces output byte-identical to the unsigned run.

- [ ] **Step 5: Run everything**

Run: `go test ./...`, `gofmt -l .`, `go vet ./...`, `go build ./...`.

- [ ] **Step 6: Commit**

```bash
git add internal/policyhelper cmd/aw-policy
git commit -m "feat(aw-policy): compile only from a bundle whose signature checks out"
```

---

### Task 8: `aw` verifies the bundle it compiles from, and doctor reports it

**Files:**
- Modify: `cmd/aw/main.go` (`resolvePolicy`, the `policySource` doctor line)
- Modify: `internal/agent/claude/inspect.go` (around the existing bundle finding at line 159)
- Test: `cmd/aw/main_test.go` or `cmd/aw/e2e_test.go`, `internal/agent/claude/inspect_test.go`

**Interfaces:**
- Consumes: Task 7's verification helper. If it is unexported in `policyhelper`, add the same check as a small exported function there — `policyhelper.VerifyBundle(stateDir string) (path string, err error)` — and have both `cmd/aw` and the helper use it, rather than writing the logic twice.
- Produces: no new exported API in `cmd/aw`.

- [ ] **Step 1: Write the failing tests**

- `cmd/aw`: with `AW_SYNC_STATE_DIR` holding a signed bundle, `aw doctor --json` reports `policy: <path> (bundle <version>, repo …)` and a `signature verified` fact; with a tampered bundle, `aw codex` exits non-zero and names the file; with no trust file, behaviour is unchanged from today.
- `internal/agent/claude`: three doctor states — verified, unsigned deployment, FAILED — plus a Warn when the bundle or the trust file is writable by group or other.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./cmd/aw/ ./internal/agent/claude/`

- [ ] **Step 3: Implement**

In `resolvePolicy`'s bundle branch, verify before compiling and return an error naming the file when verification fails — for `aw`, the bundle is the only input, so an unverifiable one is a refusal to launch, not a fallback.

In `internal/agent/claude/inspect.go`, add the finding beside the existing bundle check:

```go
	// Three states, and only a positive failure is an Error: a deployment
	// that does not sign is a choice, not a fault.
	switch {
	case trustMissing:
		facts = append(facts, "bundle signature: unsigned deployment")
	case verified:
		facts = append(facts, "bundle signature: verified (key "+keyID+")")
	default:
		findings = append(findings, agent.Finding{Level: agent.Error, Message: "bundle signature: FAILED — " + bundlePath + " does not match key " + keyID})
	}
```

Add the permission Warn: on POSIX, `info.Mode().Perm()&0o022 != 0` on either file means anyone can rewrite what the check reads. Skip the check on Windows, where mode bits carry no ACL information, and say so in a comment.

- [ ] **Step 4: Run everything**

Run: `go test ./...`, `gofmt -l .`, `go vet ./...`, `go build ./...`, and both cross-builds.

- [ ] **Step 5: Commit**

```bash
git add cmd/aw internal/agent/claude
git commit -m "feat(aw): refuse an unverifiable bundle at launch and report signing in doctor"
```

---

### Task 9: Documentation

**Files:**
- Modify: `README.md` ("How policy reaches the agent")
- Modify: `deploy/aw-sync/README.md`
- Modify: `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` (the "Signed bundles" out-of-scope line)
- Modify: `docs/superpowers/specs/2026-09-22-signed-bundles-design.md` (status line)

- [ ] **Step 1: README**

Add a "Bundle signing" subsection: what is signed (the bytes awd serves), where the proof lives (`<state dir>/aw-bundle.json.sig` and `aw-trust.pub`), what is pinned where (the key in `machine.json` at enrollment), and — in the same voice as the existing `AW_POLICY_BUNDLE` caveat — **what it does not buy**: against genuine root this is detection, not prevention, because the trust anchor and the bundle share a directory and root can replace the verifier itself. Name the cases where it does prevent: loose `C:\ProgramData` ACLs, a mis-chmodded deploy, a copied or restored bundle, a compromised aw-sync.

- [ ] **Step 2: Deploy runbook**

In `deploy/aw-sync/README.md`, add `awd keygen --out /etc/agent-wrapper/signing.key`, the two environment variables, and the four-step rotation:

1. `awd keygen --out …/signing-2.key`
2. `AWD_SIGNING_KEY=…/signing-2.key AWD_SIGNING_KEY_PREVIOUS=…/signing.key`, restart
3. watch `awd machines` until every row shows the new key ID
4. drop `AWD_SIGNING_KEY_PREVIOUS`, restart, archive the old key

- [ ] **Step 3: Spec pointers**

In the parent design's "Out of scope" list, change the signed-bundles line to point at `2026-09-22-signed-bundles-design.md`. In the signed-bundles spec, change `Status: designed` to `Status: implemented 2026-09-22 (main <sha>)`.

- [ ] **Step 4: Commit**

```bash
git add README.md deploy docs
git commit -m "docs: bundle signing, the rotation runbook, and what signing does not buy"
```

---

## Self-Review

**Spec coverage:** cryptography → Task 1; rotation statement → Task 2; awd signing, enrollment key, rollover header → Task 3; `awd keygen`, key configuration, `awd machines` key column → Task 4; pinning and fetch verification → Task 5; on-disk files and the served-bytes requirement → Task 6; aw-policy → Task 7; launch adapters and doctor → Task 8; documentation → Task 9. Every failure-mode row is covered by a test in the task that implements it.

**Known interface changes that ripple:** `Store.TouchMachine` (Task 4) and `Client.Fetch` (Task 5) both gain a parameter; every caller and both store implementations change in the same task.

**Type consistency:** `signing.KeyID` takes `ed25519.PublicKey` everywhere; `Fetched.Raw` is `[]byte` and is what `sync.Run` writes; `Machine.PublicKey` is the formatted string, parsed at use. The signature file's three fields — `aw-ed25519 <keyId> <sig>` — are written in Task 6 and parsed in Tasks 7 and 8 with the same `strings.Fields` shape.
