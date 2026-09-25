# Signature v2 and AWS KMS Signer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `awd` can sign bundles with a per-tenant AWS KMS Ed25519 key. It does this with a v2 signature over a short statement, chosen automatically per machine through a client-advertised formats header, while self-hosted seed-file deployments keep working unchanged.

**Architecture:**
- `internal/signing` gains:
  - a `Signer` interface (a seed implementation and a KMS implementation),
  - the v2 statement,
  - format negotiation helpers,
  - a v2-aware `VerifyFiles`.
- The bundle handler picks a format per request, signs through the interface behind a small cache, and answers 426 or 503 when it must.
- `aw-sync` advertises formats, verifies v2, and writes an `aw-ed25519-v2` signature line.
- `awd` loads `AWD_SIGNING_KEY` as either a seed path or `awskms:<ARN>`.

**Tech Stack:**
- Go 1.24 floor (raised from 1.22).
- `crypto/ed25519`.
- `github.com/aws/aws-sdk-go-v2` (`config`, `service/kms`, `aws/arn`) and `github.com/aws/smithy-go`.
- Existing handler and sync test harnesses.

**Spec:** `docs/superpowers/specs/2026-09-25-signature-v2-kms-design.md`

## Global Constraints

Pinned values:
- `go.mod` says `go 1.24`. CI uses `actions/setup-go@v5` with `go-version: stable`.
- Locally, `GOTOOLCHAIN=auto` fetches a newer toolchain; the machine has 1.22.8. Never run plain `go mod tidy` with `GOTOOLCHAIN=local` after this change.

v2 statement, exact bytes (UTF-8, each line ending in `\n`):
- `aw-bundle-v2\n<key id>\n<lowercase hex SHA-256 of the exact response body>\n`
- The signature is plain Ed25519 (not Ed25519ph), base64 standard with padding.

Headers:
- Request: `X-AW-Signature-Formats: v1, v2`. It is comma-separated, whitespace is optional, names are matched ignoring case, and unknown names are ignored.
- An absent header, or one naming no known format, means `v1`.
- Response on a 200: `X-AW-Signature-Format: v1|v2`, plus the existing `X-AW-Signature` and `X-AW-Key-Id`.
- A signed response without the format header is v1.

Negotiation:
- Seed signer: v2 when offered, v1 otherwise.
- KMS signer: v2 only. Otherwise answer `426` with body `{"error":"aw-sync too old for this control plane's signing; upgrade aw-sync to a build that supports signature format v2"}`.
- Unsigned deployment: no signature headers.

Signature file (`aw-bundle.json.sig`): one line.
- `aw-ed25519 <key id> <base64 sig>` for v1.
- `aw-ed25519-v2 <key id> <base64 sig>` for v2.

Rollover:
- `aw-key-rollover-v1` is unchanged.
- The previous key signs it through the `Signer` interface, so it can be a seed or KMS.

KMS signer:
- `SigningAlgorithm ED25519_SHA_512`, `MessageType RAW`.
- Refuses any message over 4096 bytes before calling KMS.
- The signature must be exactly 64 bytes.
- `KeySpec` must be `ECC_NIST_EDWARDS25519` and `KeyUsage` must be `SIGN_VERIFY`.
- `Formats()` is `["v2"]`.

Seed signer: `Formats()` is `["v2","v1"]`.

Signature cache: an LRU of **1024** entries, keyed by signer key ID + SHA-256 of the message. Failures are never cached.

Config:
- `AWD_SIGNING_KEY` and `AWD_SIGNING_KEY_PREVIOUS` are either a seed file path or `awskms:<key ARN>`.
- The region comes from the ARN; credentials come from the default AWS chain.
- `awd keygen` stays seed-only.

Security and simplicity:
- Never log credentials.
- No new operator configuration beyond the `awskms:` value.
- No per-machine format column, no migration.
- The user preference is simplicity for new users: add no knobs.

Failure modes, copied from the spec. Each row gets a test.

| Condition | Behavior |
|---|---|
| `AWD_SIGNING_KEY=awskms:…` and KMS unreachable or denied at startup | `awd` refuses to start, naming the ARN and the AWS error code |
| KMS key is not Ed25519 or not SIGN_VERIFY | `awd` refuses to start, naming the key spec and usage found |
| Malformed `awskms:` value (not an ARN) | `awd` refuses to start |
| KMS `Sign` fails or throttles on a 200 | 503 `signing unavailable`, never an unsigned body; logged with the AWS error code |
| KMS fails on a 304 whose rollover signature is not cached | 503 as above; cached, it is served |
| KMS returns a signature that is not 64 bytes | treated as a `Sign` failure (503) |
| KMS signer, client offers no v2 | 426 with the upgrade message; the machine keeps its current bundle |
| v2 response, `X-AW-Key-Id` ≠ pinned key ID | cycle fails naming both IDs; nothing written |
| v2 signature does not verify | cycle fails as a bad v1 signature does today |
| Unknown `X-AW-Signature-Format` in a response | cycle fails naming the value |
| `.sig` file with an unknown scheme token | `VerifyFiles` fails naming the token; `aw-policy` behaves as for a bad signature |
| Old `aw-sync` (no formats header) against a seed signer | v1, exactly as today |
| New `aw-sync` against an old `awd` (no format header) | v1 verification, exactly as today |
| Message over 4096 bytes passed to the KMS signer | error before any KMS call |

## Review Focus

1. **Rotating from a seed key onto KMS.** The current key is KMS (v2 only) and the previous one is a seed. A machine pinned to the seed must repin through the seed-signed rollover, then verify v2 with the KMS key. → Task 4, `TestFetchRepinsFromSeedOntoAV2OnlySigner`.
2. **Sloppy formats header** (`" V2 ,v1 "`, duplicates, unknown names). The best common format is chosen, and a header naming nothing known means v1. → Task 2, `TestParseFormats` plus the fuzz test.
3. **Large bundle with a KMS signer** (100 KB body). It signs fine, because only the ~90-byte statement reaches the signer. → Task 3, `TestKMSStyleSignerSignsALargeBundle`.
4. **Upgraded `aw-sync` on a stable fleet (every cycle 304).** The old v1 `.sig` file stays valid, and `aw doctor` still reports `verified (v1, key …)`. → Task 4, `TestUpgradedClientKeepsAV1SignatureOnA304`.
5. **KMS throttles once, then recovers.** The failed signature is never cached, and the next request succeeds. → Task 3, `TestSigningFailureIsNotCached`.

---

## File Structure

Create:
- `internal/signing/signer.go`: the `Signer` interface, the seed signer, format constants, `ParseFormats`, `Choose`, `BundleStatement`, `VerifyBundle`, `SignatureLine`.
- `internal/signing/signer_test.go`.
- `internal/signing/awskms/awskms.go` and `awskms_test.go`: the KMS signer, `Load`.
- `internal/handler/sigcache.go` and `sigcache_test.go`: the LRU signature cache.

Modify:
- `internal/signing/rollover.go`: `SignRollover` takes a `Signer`.
- `internal/signing/signing.go`: `VerifyFiles` dispatches on the token and returns the format.
- `internal/handler/handler.go`: the `Signer` struct becomes `{Current, Previous signing.Signer}`.
- `internal/handler/bundle.go`: negotiation, 426, 503, cache.
- `internal/handler/enroll.go` and `console.go`: unchanged call sites (`h.Signer.Public()` and `KeyID()` stay).
- `internal/sync/client.go`: the formats header, v2 verification, `Fetched.Format`.
- `internal/sync/sync.go`: the signature line via `signing.SignatureLine`.
- `internal/agent/claude/inspect.go`: the doctor message carries the format.
- `internal/policyhelper/helper.go`: the new `VerifyFiles` return.
- `cmd/awd/main.go`: `loadSigner`.
- `go.mod` and `go.sum`, `.github/workflows/console.yml`, `README.md`.
- Tests beside each file.

---

### Task 1: Raise the Go floor to 1.24

**Files:**
- Modify: `go.mod`, `go.sum`, `.github/workflows/console.yml`

**Interfaces:** none.

- [ ] **Step 1: Bump the directive**

Run: `GOTOOLCHAIN=auto go mod edit -go=1.24 && GOTOOLCHAIN=auto go mod tidy`
Then: `grep -n '^go \|^toolchain' go.mod`
Expected: `go 1.24` (a `toolchain` line may appear; delete it with `go mod edit -toolchain=none`).

- [ ] **Step 2: CI**

In `.github/workflows/console.yml`, change the `setup-go` `go-version: "1.22"` to `go-version: stable`.

- [ ] **Step 3: Verify**

Run: `GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./...`
Expected: all `ok`. Fix only what the newer toolchain's vet newly flags, if anything, and name each fix in the commit body.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum .github/workflows/console.yml
git commit -m "build: raise the Go floor to 1.24 for the AWS SDK"
```

---

### Task 2: `signing`: Signer interface, v2 statement, formats, VerifyFiles

**Files:**
- Create: `internal/signing/signer.go`, `internal/signing/signer_test.go`
- Modify: `internal/signing/rollover.go`, `internal/signing/signing.go`, `internal/signing/*_test.go` (rollover and VerifyFiles call sites), `internal/agent/claude/inspect.go`, `internal/policyhelper/helper.go`

**Interfaces:**
- Produces:
  - `type Signer interface { Public() ed25519.PublicKey; Sign(ctx context.Context, msg []byte) ([]byte, error); Formats() []string }`
  - `const FormatV1 = "v1"; const FormatV2 = "v2"`
  - `func NewSeedSigner(key ed25519.PrivateKey) Signer`
  - `func BundleStatement(keyID string, body []byte) []byte`
  - `func ParseFormats(header string) []string` returns the known offered formats, lowercase and deduplicated; `[]string{"v1"}` when none are known.
  - `func Choose(offered, supported []string) (string, bool)` returns the first of `supported` (best first) that is in `offered`.
  - `func VerifyBundle(format string, key ed25519.PublicKey, body []byte, sig string) bool`
  - `func SignatureLine(format, keyID, sig string) string` returns the `.sig` line, with a trailing `\n`.
  - `func SignRollover(ctx context.Context, previous Signer, next ed25519.PublicKey) (string, error)`
  - `func VerifyFiles(trustPath, bundlePath, sigPath string) (verified []byte, format, keyID string, trustMissing bool, err error)`

- [ ] **Step 1: Failing tests**

`internal/signing/signer_test.go`:

```go
package signing

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBundleStatementBytes(t *testing.T) {
	got := string(BundleStatement("3f9a1c22b0d41e77", []byte("{}")))
	want := "aw-bundle-v2\n3f9a1c22b0d41e77\n44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a\n"
	if got != want {
		t.Fatalf("statement:\n%q\nwant\n%q", got, want)
	}
}

func TestSeedSignerRoundTripsBothFormats(t *testing.T) {
	key, _ := Generate()
	s := NewSeedSigner(key)
	if !reflect.DeepEqual(s.Formats(), []string{FormatV2, FormatV1}) {
		t.Fatalf("formats %v", s.Formats())
	}
	body := []byte(`{"version":"v1"}`)
	for _, format := range []string{FormatV1, FormatV2} {
		msg := body
		if format == FormatV2 {
			msg = BundleStatement(KeyID(s.Public()), body)
		}
		raw, err := s.Sign(context.Background(), msg)
		if err != nil {
			t.Fatal(err)
		}
		sig := base64.StdEncoding.EncodeToString(raw)
		if !VerifyBundle(format, s.Public(), body, sig) {
			t.Fatalf("%s did not verify", format)
		}
		if VerifyBundle(format, s.Public(), []byte(`{"version":"v2"}`), sig) {
			t.Fatalf("%s verified a changed body", format)
		}
	}
	if VerifyBundle("v3", s.Public(), body, "x") {
		t.Fatal("unknown format verified")
	}
}

// A v2 signature is over the statement, not the body: it must not verify
// as v1, or a downgrade could replay it.
func TestV2SignatureDoesNotVerifyAsV1(t *testing.T) {
	key, _ := Generate()
	s := NewSeedSigner(key)
	body := []byte("{}")
	raw, _ := s.Sign(context.Background(), BundleStatement(KeyID(s.Public()), body))
	if VerifyBundle(FormatV1, s.Public(), body, base64.StdEncoding.EncodeToString(raw)) {
		t.Fatal("v2 signature accepted as v1")
	}
}

func TestParseFormats(t *testing.T) {
	cases := map[string][]string{
		"":             {"v1"},
		"v1, v2":       {"v1", "v2"},
		" V2 ,v1 ":     {"v2", "v1"},
		"v2,v2,v1":     {"v2", "v1"},
		"v9, bogus":    {"v1"},
		"v9, v2":       {"v2"},
		",,":           {"v1"},
	}
	for in, want := range cases {
		if got := ParseFormats(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseFormats(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestChoose(t *testing.T) {
	if f, ok := Choose([]string{"v1", "v2"}, []string{"v2", "v1"}); !ok || f != "v2" {
		t.Fatalf("seed+new client = %q %v", f, ok)
	}
	if f, ok := Choose([]string{"v1"}, []string{"v2", "v1"}); !ok || f != "v1" {
		t.Fatalf("seed+old client = %q %v", f, ok)
	}
	if _, ok := Choose([]string{"v1"}, []string{"v2"}); ok {
		t.Fatal("kms+old client chose something")
	}
}

func FuzzParseFormats(f *testing.F) {
	for _, s := range []string{"", "v1, v2", " V2 ,v1 ", "v9", ",,,"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := ParseFormats(s)
		if len(got) == 0 {
			t.Fatal("empty result")
		}
		for _, g := range got {
			if g != FormatV1 && g != FormatV2 {
				t.Fatalf("unknown format %q in %v", g, got)
			}
		}
	})
}

func TestSignatureLine(t *testing.T) {
	if got := SignatureLine(FormatV1, "k", "s"); got != "aw-ed25519 k s\n" {
		t.Fatalf("v1 line %q", got)
	}
	if got := SignatureLine(FormatV2, "k", "s"); got != "aw-ed25519-v2 k s\n" {
		t.Fatalf("v2 line %q", got)
	}
}

func writeSigned(t *testing.T, format string, tamperToken string) (dir string, s Signer) {
	t.Helper()
	dir = t.TempDir()
	key, _ := Generate()
	s = NewSeedSigner(key)
	body := []byte(`{"version":"v1"}`)
	msg := body
	if format == FormatV2 {
		msg = BundleStatement(KeyID(s.Public()), body)
	}
	raw, _ := s.Sign(context.Background(), msg)
	line := SignatureLine(format, KeyID(s.Public()), base64.StdEncoding.EncodeToString(raw))
	if tamperToken != "" {
		line = tamperToken + line[strings.Index(line, " "):]
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "trust"), []byte(FormatPublic(s.Public())+"\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "bundle"), body, 0o644))
	must(os.WriteFile(filepath.Join(dir, "sig"), []byte(line), 0o644))
	return dir, s
}

func TestVerifyFilesBothFormats(t *testing.T) {
	for _, format := range []string{FormatV1, FormatV2} {
		dir, s := writeSigned(t, format, "")
		body, gotFormat, keyID, missing, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig"))
		if err != nil || missing || gotFormat != format || keyID != KeyID(s.Public()) || string(body) != `{"version":"v1"}` {
			t.Fatalf("%s: body=%q format=%q key=%q missing=%v err=%v", format, body, gotFormat, keyID, missing, err)
		}
	}
}

func TestVerifyFilesUnknownTokenIsNamed(t *testing.T) {
	dir, _ := writeSigned(t, FormatV2, "aw-ed25519-v9")
	_, _, _, _, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig"))
	if err == nil || !strings.Contains(err.Error(), "aw-ed25519-v9") {
		t.Fatalf("err = %v, want it to name the token", err)
	}
}

func TestVerifyFilesV2RejectsAChangedBody(t *testing.T) {
	dir, _ := writeSigned(t, FormatV2, "")
	if err := os.WriteFile(filepath.Join(dir, "bundle"), []byte(`{"version":"evil"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig")); err == nil {
		t.Fatal("changed body verified")
	}
}

// A v2 line claiming the right key ID but signed by another key fails.
func TestVerifyFilesV2WrongKey(t *testing.T) {
	dir, s := writeSigned(t, FormatV2, "")
	other, _ := Generate()
	if err := os.WriteFile(filepath.Join(dir, "trust"), []byte(FormatPublic(other.Public().(ed25519.PublicKey))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s
	if _, _, _, _, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig")); err == nil {
		t.Fatal("wrong key verified")
	}
}

func TestSignRolloverThroughASigner(t *testing.T) {
	prev, _ := Generate()
	next, _ := Generate()
	header, err := SignRollover(context.Background(), NewSeedSigner(prev), next.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	r, err := VerifyRollover(prev.Public().(ed25519.PublicKey), header)
	if err != nil || r.KeyID != KeyID(next.Public().(ed25519.PublicKey)) {
		t.Fatalf("rollover = %+v, %v", r, err)
	}
}
```

Before relying on `TestBundleStatementBytes`, compute the golden hash with `printf '{}' | sha256sum` and paste the real value if it differs.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/signing/`
Expected: compile errors (undefined `NewSeedSigner`, `BundleStatement`, and so on).

- [ ] **Step 3: Implement**

`internal/signing/signer.go`:

```go
package signing

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Bundle signature formats. v1 signs the body; v2 signs a short statement
// naming the key and the body's SHA-256, which is what lets a KMS key sign
// a bundle of any size.
const (
	FormatV1 = "v1"
	FormatV2 = "v2"
)

const (
	bundleV2Prefix = "aw-bundle-v2"
	lineV1         = "aw-ed25519"
	lineV2         = "aw-ed25519-v2"
)

// Signer produces plain Ed25519 signatures. An implementation may be
// remote, so Sign takes a context and can fail.
type Signer interface {
	Public() ed25519.PublicKey
	Sign(ctx context.Context, msg []byte) ([]byte, error)
	// Formats lists the bundle formats this signer can produce, best first.
	Formats() []string
}

type seedSigner struct{ key ed25519.PrivateKey }

// NewSeedSigner wraps a key read by LoadSeed. A local key signs a message
// of any size, so it speaks both formats and prefers v2.
func NewSeedSigner(key ed25519.PrivateKey) Signer { return seedSigner{key: key} }

func (s seedSigner) Public() ed25519.PublicKey { return s.key.Public().(ed25519.PublicKey) }
func (s seedSigner) Formats() []string         { return []string{FormatV2, FormatV1} }
func (s seedSigner) Sign(_ context.Context, msg []byte) ([]byte, error) {
	return ed25519.Sign(s.key, msg), nil
}

// BundleStatement is the exact byte sequence a v2 signature covers.
func BundleStatement(keyID string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(bundleV2Prefix + "\n" + keyID + "\n" + hex.EncodeToString(sum[:]) + "\n")
}

// ParseFormats reads an X-AW-Signature-Formats header. Unknown names are
// ignored, and a header naming nothing known — or no header at all — means
// v1, which is what every aw-sync before v2 understood.
func ParseFormats(header string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(header, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if (name == FormatV1 || name == FormatV2) && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return []string{FormatV1}
	}
	return out
}

// Choose returns the first of supported, best first, that the client
// offered.
func Choose(offered, supported []string) (string, bool) {
	for _, s := range supported {
		for _, o := range offered {
			if s == o {
				return s, true
			}
		}
	}
	return "", false
}

// VerifyBundle checks sig over body in the given format. Any unknown
// format is false.
func VerifyBundle(format string, key ed25519.PublicKey, body []byte, sig string) bool {
	switch format {
	case FormatV1:
		return Verify(key, body, sig)
	case FormatV2:
		return Verify(key, BundleStatement(KeyID(key), body), sig)
	}
	return false
}

// SignatureLine is the one-line signature file aw-sync writes beside the
// bundle; the first token names the format.
func SignatureLine(format, keyID, sig string) string {
	token := lineV1
	if format == FormatV2 {
		token = lineV2
	}
	return token + " " + keyID + " " + sig + "\n"
}
```

In `signing.go`, delete the `signatureLine` const. Change `VerifyFiles` to return `(verified []byte, format, keyID string, trustMissing bool, err error)`, with the doc comment updated to mention the format. Its tail becomes:

```go
	fields := strings.Fields(string(line))
	if len(fields) != 3 {
		return nil, "", keyID, false, fmt.Errorf("%s is not signed by key %s", bundlePath, keyID)
	}
	var format string
	switch fields[0] {
	case lineV1:
		format = FormatV1
	case lineV2:
		format = FormatV2
	default:
		return nil, "", keyID, false, fmt.Errorf("%s uses an unknown signature scheme %q", sigPath, fields[0])
	}
	if !VerifyBundle(format, key, body, fields[2]) {
		return nil, "", keyID, false, fmt.Errorf("%s is not signed by key %s", bundlePath, keyID)
	}
	return body, format, keyID, false, nil
```

Adjust the earlier returns to the five-value shape: `return nil, "", "", true, nil` for a missing trust file, and so on.

In `rollover.go`:

```go
// SignRollover builds the header value announcing next, signed by previous.
// previous may be remote (KMS), so this can fail.
func SignRollover(ctx context.Context, previous Signer, next ed25519.PublicKey) (string, error) {
	statement := rolloverStatement(next)
	raw, err := previous.Sign(ctx, []byte(statement))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString([]byte(statement)) + " " + base64.StdEncoding.EncodeToString(raw), nil
}
```

Update the existing signing tests that call `SignRollover` or `VerifyFiles` to the new signatures.

Callers:
- `internal/agent/claude/inspect.go`: `_, format, keyID, trustMissing, err := signing.VerifyFiles(...)`, and the verified message becomes `"bundle signature: verified (" + format + ", key " + keyID + ")"`. Update the doctor tests that assert the old text to expect `verified (v1, key …)`.
- `internal/policyhelper/helper.go`: `body, _, _, trustMissing, err := signing.VerifyFiles(...)`.
- `internal/handler/bundle.go` calls `SignRollover`; Task 3 rewrites that call. For now, make it compile: `header, err := signing.SignRollover(r.Context(), signing.NewSeedSigner(h.Signer.Previous), h.Signer.Public()); if err == nil { w.Header().Set(...) }`. Task 3 replaces it.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/signing/ ./internal/agent/... ./internal/policyhelper/ ./internal/handler/ && go test ./internal/signing/ -run XXX -fuzz FuzzParseFormats -fuzztime 10s`
Expected: PASS, and the fuzz run finds no failures.

- [ ] **Step 5: Commit**

```bash
git add internal/signing internal/agent internal/policyhelper internal/handler
git commit -m "feat(signing): Signer interface, v2 bundle statement and format negotiation"
```

---

### Task 3: Handler: negotiate the format, sign through the Signer, cache, 426/503

**Files:**
- Create: `internal/handler/sigcache.go`, `internal/handler/sigcache_test.go`
- Modify: `internal/handler/handler.go` (the `Signer` struct), `internal/handler/bundle.go`, `internal/handler/bundle_test.go`, `internal/handler/enroll_test.go`, `cmd/awd/main.go`

**Interfaces:**
- Consumes (Task 2): `signing.Signer`, `NewSeedSigner`, `ParseFormats`, `Choose`, `BundleStatement`, `SignRollover`, `FormatV1/V2`.
- Produces:
  - `type Signer struct { Current signing.Signer; Previous signing.Signer }` with methods `Public() ed25519.PublicKey` and `KeyID() string` (same names as today, so `enroll.go` and `console.go` don't change).
  - `newSigCache(size int) *sigCache`, `(*sigCache).sign(ctx, s signing.Signer, msg []byte) ([]byte, error)`.
  - `cmd/awd`: `loadSigner(ctx context.Context, value string) (signing.Signer, error)` (seed only in this task; Task 5 adds `awskms:`).

- [ ] **Step 1: Failing tests**

`internal/handler/sigcache_test.go`:

```go
package handler

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/acme/agent-wrapper/internal/signing"
)

// countingSigner wraps a seed and counts Sign calls; fail makes the next
// call fail once.
type countingSigner struct {
	signing.Signer
	calls atomic.Int32
	fail  atomic.Bool
}

func (c *countingSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	c.calls.Add(1)
	if c.fail.CompareAndSwap(true, false) {
		return nil, errors.New("ThrottlingException: rate exceeded")
	}
	return c.Signer.Sign(ctx, msg)
}

func newCounting(t *testing.T) *countingSigner {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return &countingSigner{Signer: signing.NewSeedSigner(key)}
}

func TestSigCacheHitsOnTheSameMessage(t *testing.T) {
	c := newSigCache(1024)
	s := newCounting(t)
	for i := 0; i < 3; i++ {
		if _, err := c.sign(context.Background(), s, []byte("same")); err != nil {
			t.Fatal(err)
		}
	}
	if s.calls.Load() != 1 {
		t.Fatalf("Sign called %d times, want 1", s.calls.Load())
	}
}

// Review Focus 5.
func TestSigningFailureIsNotCached(t *testing.T) {
	c := newSigCache(1024)
	s := newCounting(t)
	s.fail.Store(true)
	if _, err := c.sign(context.Background(), s, []byte("m")); err == nil {
		t.Fatal("want failure")
	}
	sig, err := c.sign(context.Background(), s, []byte("m"))
	if err != nil || !ed25519.Verify(s.Public(), []byte("m"), sig) {
		t.Fatalf("retry = %v", err)
	}
}

func TestSigCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newSigCache(2)
	s := newCounting(t)
	ctx := context.Background()
	_, _ = c.sign(ctx, s, []byte("a"))
	_, _ = c.sign(ctx, s, []byte("b"))
	_, _ = c.sign(ctx, s, []byte("a")) // a is now most recent
	_, _ = c.sign(ctx, s, []byte("c")) // evicts b
	_, _ = c.sign(ctx, s, []byte("a")) // hit
	_, _ = c.sign(ctx, s, []byte("b")) // miss
	if got := s.calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4 (a, b, c, b)", got)
	}
}

// Two keys signing the same message are two entries.
func TestSigCacheKeysBySigner(t *testing.T) {
	c := newSigCache(1024)
	a, b := newCounting(t), newCounting(t)
	sa, _ := c.sign(context.Background(), a, []byte("m"))
	sb, _ := c.sign(context.Background(), b, []byte("m"))
	if !ed25519.Verify(b.Public(), []byte("m"), sb) || string(sa) == string(sb) {
		t.Fatal("cache returned one signer's signature for another")
	}
}
```

Add to `internal/handler/bundle_test.go`, next to `newSignedServer`, and update the existing `&handler.Signer{Key: key}` literals to `&handler.Signer{Current: signing.NewSeedSigner(key)}` and `Previous: signing.NewSeedSigner(previous)`:

```go
// v2OnlySigner stands in for KMS: a seed that only speaks v2, fails on
// demand, and refuses messages over 4096 bytes like the real one.
type v2OnlySigner struct {
	signing.Signer
	fail bool
}

func (v *v2OnlySigner) Formats() []string { return []string{signing.FormatV2} }
func (v *v2OnlySigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	if len(msg) > 4096 {
		return nil, fmt.Errorf("message of %d bytes exceeds 4096", len(msg))
	}
	if v.fail {
		return nil, errors.New("KMSInternalException: boom")
	}
	return v.Signer.Sign(ctx, msg)
}

func bundleReq(t *testing.T, srv *httptest.Server, credential, formats, etag string) *http.Response {
	t.Helper()
	h := http.Header{"Authorization": {"Bearer " + credential}}
	if formats != "" {
		h.Set("X-AW-Signature-Formats", formats)
	}
	if etag != "" {
		h.Set("If-None-Match", etag)
	}
	return get(t, srv, "/v1/bundle", h)
}

func TestNegotiationTable(t *testing.T) {
	key, _ := signing.Generate()
	seed := signing.NewSeedSigner(key)
	kms := &v2OnlySigner{Signer: seed}
	cases := []struct {
		name       string
		signer     signing.Signer
		formats    string
		wantStatus int
		wantFormat string
	}{
		{"seed, new client", seed, "v1, v2", 200, "v2"},
		{"seed, old client", seed, "", 200, "v1"},
		{"seed, v1 only", seed, "v1", 200, "v1"},
		{"seed, unknown only", seed, "v9", 200, "v1"},
		{"kms, new client", kms, "v1, v2", 200, "v2"},
		{"kms, old client", kms, "", 426, ""},
		{"kms, unknown only", kms, "v9", 426, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: tc.signer})
			resp := bundleReq(t, srv, credential, tc.formats, "")
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == 426 {
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), "upgrade aw-sync to a build that supports signature format v2") {
					t.Fatalf("426 body %s", body)
				}
				return
			}
			got := resp.Header.Get("X-AW-Signature-Format")
			if got != tc.wantFormat {
				t.Fatalf("format %q, want %q", got, tc.wantFormat)
			}
			body, _ := io.ReadAll(resp.Body)
			if !signing.VerifyBundle(got, tc.signer.Public(), body, resp.Header.Get("X-AW-Signature")) {
				t.Fatal("signature does not verify")
			}
		})
	}
}

func TestUnsignedServerSendsNoSignatureHeaders(t *testing.T) {
	srv, credential := newSignedServerWithMachine(t, nil)
	resp := bundleReq(t, srv, credential, "v1, v2", "")
	for _, h := range []string{"X-AW-Signature", "X-AW-Signature-Format", "X-AW-Key-Id"} {
		if resp.Header.Get(h) != "" {
			t.Fatalf("%s set on an unsigned server", h)
		}
	}
}

// Review Focus 3.
func TestKMSStyleSignerSignsALargeBundle(t *testing.T) {
	key, _ := signing.Generate()
	kms := &v2OnlySigner{Signer: signing.NewSeedSigner(key)}
	srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: kms})
	postLargePolicy(t, srv, 100<<10) // 100 KiB of managed settings
	resp := bundleReq(t, srv, credential, "v1, v2", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(body) < 100<<10 || !signing.VerifyBundle("v2", kms.Public(), body, resp.Header.Get("X-AW-Signature")) {
		t.Fatalf("status %d, len %d", resp.StatusCode, len(body))
	}
}

func TestSignerFailureIs503NeverUnsigned(t *testing.T) {
	key, _ := signing.Generate()
	kms := &v2OnlySigner{Signer: signing.NewSeedSigner(key), fail: true}
	srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: kms})
	resp := bundleReq(t, srv, credential, "v1, v2", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 || !strings.Contains(string(body), "signing unavailable") || strings.Contains(string(body), `"version"`) {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
}

func TestRolloverOn304UsesCacheAndFailsClosedWhenUncached(t *testing.T) {
	cur, _ := signing.Generate()
	prevKey, _ := signing.Generate()
	prev := &v2OnlySigner{Signer: signing.NewSeedSigner(prevKey)}
	srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: signing.NewSeedSigner(cur), Previous: prev})
	first := bundleReq(t, srv, credential, "v1, v2", "")
	etag := first.Header.Get("ETag")
	if first.Header.Get("X-AW-Key-Rollover") == "" {
		t.Fatal("no rollover on the 200")
	}
	prev.fail = true
	cached := bundleReq(t, srv, credential, "v1, v2", etag)
	if cached.StatusCode != 304 || cached.Header.Get("X-AW-Key-Rollover") == "" {
		t.Fatalf("cached 304 = %d rollover %q", cached.StatusCode, cached.Header.Get("X-AW-Key-Rollover"))
	}
	// A fresh server has an empty cache: the same failure is a 503.
	srv2, credential2 := newSignedServerWithMachine(t, &handler.Signer{Current: signing.NewSeedSigner(cur), Previous: prev})
	if resp := bundleReq(t, srv2, credential2, "v1, v2", etag); resp.StatusCode != 503 {
		t.Fatalf("uncached rollover failure = %d, want 503", resp.StatusCode)
	}
}
```

Add these helpers in `bundle_test.go`, reusing existing helpers where equivalents exist:
- `newSignedServerWithMachine(t, signer *handler.Signer) (*httptest.Server, credential string)`. It is `newSignedServer` plus an enrolled machine: POST an enrollment token as admin, then `/v1/machines/enroll`, returning the credential. A nil signer means unsigned.
- `postLargePolicy(t, srv, n int)`. It POSTs a revision whose baseline rule's claude managed settings hold one `env` value of `n` bytes (for example `{"version":"big","rules":[{"name":"baseline","agents":{"claude":{"managed":{"env":{"PAD":"xxxx…"}}}}}]}`). Check `examples/org-policy.yaml` for the rule shape, and use a key the managed-settings validator accepts. `newServer` sets no validator, so any key works in handler tests.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run 'SigCache|Negotiation|Unsigned|KMSStyle|SignerFailure|Rollover'`
Expected: compile errors (`newSigCache` undefined; `handler.Signer` has no field `Current`).

- [ ] **Step 3: Implement**

`internal/handler/sigcache.go`:

```go
package handler

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/acme/agent-wrapper/internal/signing"
)

// sigCache remembers signatures by key and message digest. Machines on the
// same policy receive the same bytes, so a steady fleet asks a remote
// signer (KMS) for a handful of signatures per policy change instead of
// one per request. Signatures are public, so there is nothing to expire;
// failures are never stored, so a throttled call is simply retried.
type sigCache struct {
	mu    sync.Mutex
	size  int
	order *list.List // front = most recent; values are cache keys
	items map[string]*list.Element
	sigs  map[string][]byte
}

func newSigCache(size int) *sigCache {
	return &sigCache{size: size, order: list.New(), items: map[string]*list.Element{}, sigs: map[string][]byte{}}
}

func (c *sigCache) sign(ctx context.Context, s signing.Signer, msg []byte) ([]byte, error) {
	sum := sha256.Sum256(msg)
	key := signing.KeyID(s.Public()) + ":" + hex.EncodeToString(sum[:])

	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		sig := c.sigs[key]
		c.mu.Unlock()
		return sig, nil
	}
	c.mu.Unlock()

	sig, err := s.Sign(ctx, msg)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; !ok {
		c.items[key] = c.order.PushFront(key)
		c.sigs[key] = sig
		if c.order.Len() > c.size {
			oldest := c.order.Back()
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(string))
			delete(c.sigs, oldest.Value.(string))
		}
	}
	return sig, nil
}
```

In `handler.go`, replace the `Signer` struct and its methods:

```go
// Signer holds the control plane's signing key, and during a rotation the
// key it is replacing. Previous signs nothing but the rollover statement.
// Either may be a local seed or a remote key (KMS).
type Signer struct {
	Current  signing.Signer
	Previous signing.Signer
}

// Public is the key machines pin.
func (s *Signer) Public() ed25519.PublicKey { return s.Current.Public() }

// KeyID names that key in headers and listings.
func (s *Signer) KeyID() string { return signing.KeyID(s.Public()) }
```

Add a `sigs *sigCache` field to `Handler`, initialized in `New` with `newSigCache(1024)`.

In `bundle.go`, replace everything from `// The rollover statement…` down to the `X-AW-Key-Id` header line with:

```go
	format := ""
	if h.Signer != nil {
		chosen, ok := signing.Choose(signing.ParseFormats(r.Header.Get("X-AW-Signature-Formats")), h.Signer.Current.Formats())
		if !ok {
			// Only a v2-only signer (KMS) gets here: it cannot sign the
			// body itself, so an old aw-sync must upgrade. It keeps the
			// bundle it has meanwhile.
			writeError(w, http.StatusUpgradeRequired, "aw-sync too old for this control plane's signing; upgrade aw-sync to a build that supports signature format v2")
			return
		}
		format = chosen
	}

	// The rollover statement is self-contained and signed by the outgoing
	// key, so it rides on a 304 as well as on a 200 — on a stable fleet
	// every cycle is a 304, and a rotation announced only with changed
	// bytes would never finish there.
	if h.Signer != nil && h.Signer.Previous != nil {
		header, err := signing.SignRollover(r.Context(), cachingSigner{h.sigs, h.Signer.Previous}, h.Signer.Public())
		if err != nil {
			h.signingUnavailable(w, r, err)
			return
		}
		w.Header().Set("X-AW-Key-Rollover", header)
	}
	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if h.Signer != nil {
		msg := body
		if format == signing.FormatV2 {
			msg = signing.BundleStatement(h.Signer.KeyID(), body)
		}
		sig, err := h.sigs.sign(r.Context(), h.Signer.Current, msg)
		if err != nil {
			h.signingUnavailable(w, r, err)
			return
		}
		w.Header().Set("X-AW-Signature", base64.StdEncoding.EncodeToString(sig))
		w.Header().Set("X-AW-Signature-Format", format)
		w.Header().Set("X-AW-Key-Id", h.Signer.KeyID())
	}
```

Add at the bottom of `bundle.go`:

```go
// cachingSigner routes a Signer through the handler's cache, so the
// constant rollover statement is signed once, not once per request.
type cachingSigner struct {
	cache *sigCache
	signing.Signer
}

func (c cachingSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	return c.cache.sign(ctx, c.Signer, msg)
}

// signingUnavailable answers 503 rather than ever serving an unsigned or
// unannounced response. err carries the backend's error code (for KMS,
// the AWS error name); it is logged, never sent.
func (h *Handler) signingUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	h.log.ErrorContext(r.Context(), "signing a bundle response", "err", err)
	writeError(w, http.StatusServiceUnavailable, "signing unavailable")
}
```

Update the imports: `context` and `encoding/base64`.

In `cmd/awd/main.go`, replace the signing block with:

```go
	if value := os.Getenv("AWD_SIGNING_KEY"); value != "" {
		current, err := loadSigner(context.Background(), value)
		if err != nil {
			return fmt.Errorf("awd: AWD_SIGNING_KEY: %w", err)
		}
		s := &handler.Signer{Current: current}
		if previous := os.Getenv("AWD_SIGNING_KEY_PREVIOUS"); previous != "" {
			// A rotation that cannot vouch for its new key leaves every
			// machine pinned to a key nothing signs with any more.
			old, err := loadSigner(context.Background(), previous)
			if err != nil {
				return fmt.Errorf("awd: AWD_SIGNING_KEY_PREVIOUS: %w", err)
			}
			s.Previous = old
		}
		h.Signer = s
		log.Info("signing bundles", "keyId", s.KeyID(), "formats", strings.Join(current.Formats(), ","))
	} else {
		log.Warn("bundles are not signed; set AWD_SIGNING_KEY to sign them")
	}
```

```go
// loadSigner reads AWD_SIGNING_KEY's value: a path to a seed file written
// by keygen.
func loadSigner(_ context.Context, value string) (signing.Signer, error) {
	key, err := signing.LoadSeed(value)
	if err != nil {
		return nil, err
	}
	return signing.NewSeedSigner(key), nil
}
```

Fix `internal/handler/enroll_test.go`'s `&handler.Signer{Key: key}` in the same way. Grep for any other `handler.Signer{` literal in tests and `cmd/awd` and update it.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handler/ ./cmd/awd/ && go test ./...`
Expected: PASS. The existing signed-bundle tests still pass, since an old client with a seed gets v1 exactly as before.

- [ ] **Step 5: Commit**

```bash
git add internal/handler cmd/awd
git commit -m "feat(awd): negotiate the bundle signature format and sign through a Signer"
```

---

### Task 4: `aw-sync`: advertise formats, verify v2, write the v2 signature line

**Files:**
- Modify: `internal/sync/client.go`, `internal/sync/sync.go`, `internal/sync/client_test.go`, `internal/sync/sync_test.go`

**Interfaces:**
- Consumes: `signing.VerifyBundle`, `signing.SignatureLine`, `signing.FormatV1/V2`, `signing.KeyID`, `handler.Signer{Current, Previous}` (tests only).
- Produces: `Fetched.Format string` (`"v1"` or `"v2"`, empty when unsigned).

- [ ] **Step 1: Failing tests**

Look at the existing `client_test.go` helpers first (the ones used by `TestFetchVerifiesTheSignature` and `TestFetchFollowsARollover`), and write these tests against real `handler` servers the same way. Where a test needs a hand-crafted response (bad format, mismatched key ID), use an `httptest.Server` whose handler writes the headers directly.

```go
func TestFetchAdvertisesBothFormats(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-AW-Signature-Formats")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	_, _ = (&Client{Server: srv.URL}).Fetch(context.Background(), "cred", "etag", nil)
	if got != "v1, v2" {
		t.Fatalf("header %q", got)
	}
}

func TestFetchVerifiesV2AgainstASeedServer(t *testing.T) {
	/* real handler with Signer{Current: seed}; pinned = seed public key;
	   Fetch → Format "v2", no error; Fetched.Signature verifies with
	   signing.VerifyBundle("v2", …). */
}

func TestFetchV2KeyIDMismatchNamesBothIDs(t *testing.T) {
	/* hand-crafted 200: X-AW-Signature-Format v2, X-AW-Key-Id "0000000000000000",
	   a valid v2 signature by the pinned key over the body; Fetch error contains
	   both "0000000000000000" and signing.KeyID(pinned). */
}

func TestFetchV2BadSignatureFails(t *testing.T) {
	/* hand-crafted 200 with format v2, correct key id, signature by another key → error
	   "not signed by key <pinned id>". */
}

func TestFetchUnknownFormatIsNamed(t *testing.T) {
	/* hand-crafted 200 with X-AW-Signature-Format "v7" and pinned key → error contains "v7". */
}

func TestFetchNoFormatHeaderIsV1(t *testing.T) {
	/* hand-crafted 200 like an old awd: X-AW-Signature = v1 signature over the body,
	   no X-AW-Signature-Format; Fetch succeeds with Format "v1". */
}

// Review Focus 1.
func TestFetchRepinsFromSeedOntoAV2OnlySigner(t *testing.T) {
	/* real handler with Signer{Current: v2-only signer (local test type, as in the
	   handler tests: Formats() → ["v2"]), Previous: seed}; pinned = seed public key;
	   Fetch → Rollover set to the v2-only key, Format "v2", verifies with the new key. */
}

func TestFetchAgainstV2OnlyServerWithOldClientBehaviour(t *testing.T) {
	/* request WITHOUT the formats header can't be produced by Fetch; instead assert the
	   426 path: hand-crafted 426 with the upgrade body → Fetch error contains
	   "upgrade aw-sync". (Documents what an old client sees; the machine keeps its bundle.) */
}
```

Write each `/* … */` test in full. Its comment gives the exact setup and assertions.

In `sync_test.go`:

```go
func TestRunWritesAV2SignatureLine(t *testing.T) {
	/* like TestRunWritesTheServedBytesAndTheSignature but against a seed-signed awd:
	   aw-bundle.json.sig starts with "aw-ed25519-v2 <keyid> "; signing.VerifyFiles
	   on the state dir returns format "v2". */
}

// Review Focus 4.
func TestUpgradedClientKeepsAV1SignatureOnA304(t *testing.T) {
	/* seed the state dir with a v1 .sig/trust/bundle written as an old aw-sync would
	   (SignatureLine("v1", …)), with the etag stored so the server answers 304; run
	   one cycle; the .sig file is byte-identical afterwards and VerifyFiles returns
	   format "v1" with no error. */
}
```

Write both in full, following the existing `TestRunWritesTheServedBytesAndTheSignature` and `TestRunRepinsOnARollover` setups.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/sync/ -run 'Fetch|V2|V1Signature'`
Expected: FAIL (no header sent, `Fetched.Format` undefined).

- [ ] **Step 3: Implement**

In `client.go`, add a `Format string` field to `Fetched` with this doc comment:

```go
	// Format is the signature format the server used ("v1" or "v2"), empty
	// when the response was unsigned. It decides the signature file's
	// first token.
	Format string
```

In `Fetch`, set the header unconditionally after `User-Agent`:

```go
	// Advertising v2 is what lets a control plane whose key lives in KMS
	// sign for this machine; one that holds a seed picks v2 too.
	req.Header.Set("X-AW-Signature-Formats", signing.FormatV1+", "+signing.FormatV2)
```

Add a case for `http.StatusUpgradeRequired` alongside `default`. It needs no special handling: the default branch already reports the status and body. Leave it as is.

Replace the `if pinned != nil { … }` verification block with:

```go
	format := ""
	if signature != "" {
		format = resp.Header.Get("X-AW-Signature-Format")
		if format == "" {
			format = signing.FormatV1 // an awd from before v2
		}
	}
	if pinned != nil {
		verifier := pinned
		if rollover != nil {
			verifier = rollover.PublicKey
		}
		if signature == "" {
			return Fetched{}, errors.New("sync: this machine pins a signing key but the control plane sent no signature")
		}
		if format != signing.FormatV1 && format != signing.FormatV2 {
			return Fetched{}, fmt.Errorf("sync: the control plane used an unknown signature format %q", format)
		}
		if format == signing.FormatV2 {
			if served := resp.Header.Get("X-AW-Key-Id"); served != signing.KeyID(verifier) {
				return Fetched{}, fmt.Errorf("sync: the bundle is signed by key %s but this machine trusts key %s", served, signing.KeyID(verifier))
			}
		}
		if !signing.VerifyBundle(format, verifier, payload, signature) {
			return Fetched{}, fmt.Errorf("sync: the bundle is not signed by key %s", signing.KeyID(verifier))
		}
	}
```

Include `Format: format` in the returned `Fetched`.

In `sync.go`, change the signature file's content to `[]byte(signing.SignatureLine(fetched.Format, signing.KeyID(verified), fetched.Signature))`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/sync/ -v -run 'Fetch|Run' && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sync
git commit -m "feat(aw-sync): advertise and verify signature format v2"
```

---

### Task 5: AWS KMS signer, `awskms:` config, README

**Files:**
- Create: `internal/signing/awskms/awskms.go`, `internal/signing/awskms/awskms_test.go`
- Modify: `cmd/awd/main.go` (`loadSigner`, usage text), `cmd/awd/main_test.go`, `go.mod`, `go.sum`, `README.md`

**Interfaces:**
- Consumes: `signing.Signer`, `signing.FormatV2`, `signing.KeyID`, `signing.BundleStatement`, `signing.VerifyBundle`.
- Produces:
  - `type API interface { Sign(ctx, *kms.SignInput, ...func(*kms.Options)) (*kms.SignOutput, error); GetPublicKey(ctx, *kms.GetPublicKeyInput, ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) }`
  - `func New(ctx context.Context, api API, keyARN string) (*Signer, error)`
  - `func Load(ctx context.Context, keyARN string) (*Signer, error)`
  - `const MaxMessage = 4096`

- [ ] **Step 1: Dependencies**

Run: `GOTOOLCHAIN=auto go get github.com/aws/aws-sdk-go-v2/config github.com/aws/aws-sdk-go-v2/service/kms github.com/aws/aws-sdk-go-v2/aws github.com/aws/smithy-go`, then `GOTOOLCHAIN=auto go mod tidy`.
Check: `grep '^go ' go.mod` still says `go 1.24`. If the latest SDK needs higher, pin the newest `service/kms` whose `go.mod` says `go 1.24` (for example `@v1.61.1`) and matching versions of `config` and `aws`.

- [ ] **Step 2: Failing tests**

`internal/signing/awskms/awskms_test.go`:

```go
package awskms

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"

	"github.com/acme/agent-wrapper/internal/signing"
)

const arn = "arn:aws:kms:eu-west-1:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab"

type fakeKMS struct {
	priv     ed25519.PrivateKey
	spec     types.KeySpec
	usage    types.KeyUsageType
	der      []byte
	getErr   error
	signErr  error
	shortSig bool
	lastSign *kms.SignInput
	signs    int
}

func newFake(t *testing.T) *fakeKMS {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeKMS{priv: priv, spec: types.KeySpecEccNistEdwards25519, usage: types.KeyUsageTypeSignVerify, der: der}
}

func (f *fakeKMS) GetPublicKey(_ context.Context, in *kms.GetPublicKeyInput, _ ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &kms.GetPublicKeyOutput{KeyId: in.KeyId, KeySpec: f.spec, KeyUsage: f.usage, PublicKey: f.der}, nil
}

func (f *fakeKMS) Sign(_ context.Context, in *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	f.signs++
	f.lastSign = in
	if f.signErr != nil {
		return nil, f.signErr
	}
	sig := ed25519.Sign(f.priv, in.Message)
	if f.shortSig {
		sig = sig[:63]
	}
	return &kms.SignOutput{Signature: sig}, nil
}

func TestNewAndSignProduceAVerifiableV2Signature(t *testing.T) {
	f := newFake(t)
	s, err := New(context.Background(), f, arn)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Formats(); len(got) != 1 || got[0] != signing.FormatV2 {
		t.Fatalf("formats %v", got)
	}
	body := []byte(strings.Repeat("x", 100<<10))
	raw, err := s.Sign(context.Background(), signing.BundleStatement(signing.KeyID(s.Public()), body))
	if err != nil {
		t.Fatal(err)
	}
	if f.lastSign.SigningAlgorithm != types.SigningAlgorithmSpecEd25519Sha512 || f.lastSign.MessageType != types.MessageTypeRaw || *f.lastSign.KeyId != arn {
		t.Fatalf("sign input %+v", f.lastSign)
	}
	if !signing.VerifyBundle(signing.FormatV2, s.Public(), body, base64.StdEncoding.EncodeToString(raw)) {
		t.Fatal("does not verify")
	}
}

func TestNewRejectsTheWrongKey(t *testing.T) {
	cases := map[string]func(f *fakeKMS){
		"spec":  func(f *fakeKMS) { f.spec = types.KeySpecEccNistP256 },
		"usage": func(f *fakeKMS) { f.usage = types.KeyUsageTypeEncryptDecrypt },
		"der":   func(f *fakeKMS) { f.der = []byte("not der") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			mutate(f)
			_, err := New(context.Background(), f, arn)
			if err == nil || !strings.Contains(err.Error(), arn) {
				t.Fatalf("err = %v", err)
			}
			if name != "der" && !strings.Contains(err.Error(), string(f.spec)) {
				t.Fatalf("err %v does not name the spec %s", err, f.spec)
			}
		})
	}
}

func TestNewNamesTheAWSErrorCode(t *testing.T) {
	f := newFake(t)
	f.getErr = &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "not allowed"}
	_, err := New(context.Background(), f, arn)
	if err == nil || !strings.Contains(err.Error(), "AccessDeniedException") || !strings.Contains(err.Error(), arn) {
		t.Fatalf("err = %v", err)
	}
}

func TestSignRefusesOversizeBeforeCallingKMS(t *testing.T) {
	f := newFake(t)
	s, _ := New(context.Background(), f, arn)
	if _, err := s.Sign(context.Background(), make([]byte, MaxMessage+1)); err == nil {
		t.Fatal("want error")
	}
	if f.signs != 0 {
		t.Fatalf("KMS called %d times", f.signs)
	}
	if _, err := s.Sign(context.Background(), make([]byte, MaxMessage)); err != nil {
		t.Fatalf("exactly 4096 bytes: %v", err)
	}
}

func TestSignFailuresAreErrors(t *testing.T) {
	f := newFake(t)
	s, _ := New(context.Background(), f, arn)
	f.shortSig = true
	if _, err := s.Sign(context.Background(), []byte("m")); err == nil {
		t.Fatal("63-byte signature accepted")
	}
	f.shortSig = false
	f.signErr = &smithy.GenericAPIError{Code: "ThrottlingException"}
	if _, err := s.Sign(context.Background(), []byte("m")); err == nil || !strings.Contains(err.Error(), "ThrottlingException") {
		t.Fatalf("err = %v", err)
	}
	_ = errors.New
}

func TestLoadRejectsMalformedARNs(t *testing.T) {
	for _, bad := range []string{"", "not-an-arn", "arn:aws:s3:::bucket", "arn:aws:kms:eu-west-1:1111:alias"} {
		if _, err := Load(context.Background(), bad); err == nil {
			t.Errorf("Load(%q) succeeded", bad)
		}
	}
}
```

Delete the stray `_ = errors.New` and the unused `errors` import if the linter complains; neither is needed.

In `cmd/awd/main_test.go`:

```go
func TestLoadSignerRejectsAMalformedKMSValue(t *testing.T) {
	if _, err := loadSigner(context.Background(), "awskms:not-an-arn"); err == nil || !strings.Contains(err.Error(), "not-an-arn") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `GOTOOLCHAIN=auto go test ./internal/signing/awskms/ ./cmd/awd/ -run 'KMS|New|Sign|Load'`
Expected: compile errors (package `awskms` has no `New`).

- [ ] **Step 4: Implement**

`internal/signing/awskms/awskms.go`:

```go
// Package awskms signs with an Ed25519 key held in AWS KMS, which never
// leaves KMS. Each call is logged by CloudTrail. KMS signs a raw message of
// at most 4096 bytes, which is why bundles are signed in format v2: a short
// statement over the body's hash.
package awskms

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"

	"github.com/acme/agent-wrapper/internal/signing"
)

// MaxMessage is KMS's limit on a RAW message for Ed25519 signing.
const MaxMessage = 4096

// API is the part of the KMS client this package calls, so tests use a fake.
type API interface {
	Sign(ctx context.Context, in *kms.SignInput, opts ...func(*kms.Options)) (*kms.SignOutput, error)
	GetPublicKey(ctx context.Context, in *kms.GetPublicKeyInput, opts ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
}

// Signer implements signing.Signer with a KMS key.
type Signer struct {
	api API
	arn string
	pub ed25519.PublicKey
}

// Load parses keyARN, builds a KMS client for its region from the default
// credential chain (on ECS, the task role), and checks the key.
func Load(ctx context.Context, keyARN string) (*Signer, error) {
	parsed, err := arn.Parse(keyARN)
	if err != nil || parsed.Service != "kms" || !strings.HasPrefix(parsed.Resource, "key/") {
		return nil, fmt.Errorf("awskms: %q is not a KMS key ARN (arn:aws:kms:<region>:<account>:key/<id>)", keyARN)
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(parsed.Region))
	if err != nil {
		return nil, fmt.Errorf("awskms: loading AWS configuration for %s: %w", keyARN, err)
	}
	return New(ctx, kms.NewFromConfig(cfg), keyARN)
}

// New checks that keyARN is an Ed25519 signing key and caches its public
// key. A wrong key is a refusal to start, never a signer that fails later.
func New(ctx context.Context, api API, keyARN string) (*Signer, error) {
	out, err := api.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: aws.String(keyARN)})
	if err != nil {
		return nil, fmt.Errorf("awskms: reading the public key of %s: %s", keyARN, describe(err))
	}
	if out.KeySpec != types.KeySpecEccNistEdwards25519 || out.KeyUsage != types.KeyUsageTypeSignVerify {
		return nil, fmt.Errorf("awskms: %s is a %s key for %s; it must be %s for %s",
			keyARN, out.KeySpec, out.KeyUsage, types.KeySpecEccNistEdwards25519, types.KeyUsageTypeSignVerify)
	}
	parsed, err := x509.ParsePKIXPublicKey(out.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("awskms: parsing the public key of %s: %w", keyARN, err)
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("awskms: the public key of %s is %T, not Ed25519", keyARN, parsed)
	}
	return &Signer{api: api, arn: keyARN, pub: pub}, nil
}

func (s *Signer) Public() ed25519.PublicKey { return s.pub }

// Formats is v2 only: KMS cannot sign a whole bundle body.
func (s *Signer) Formats() []string { return []string{signing.FormatV2} }

// Sign asks KMS for a plain Ed25519 signature over msg.
func (s *Signer) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	if len(msg) > MaxMessage {
		return nil, fmt.Errorf("awskms: a %d-byte message exceeds KMS's %d-byte limit", len(msg), MaxMessage)
	}
	out, err := s.api.Sign(ctx, &kms.SignInput{
		KeyId:            aws.String(s.arn),
		Message:          msg,
		MessageType:      types.MessageTypeRaw,
		SigningAlgorithm: types.SigningAlgorithmSpecEd25519Sha512,
	})
	if err != nil {
		return nil, fmt.Errorf("awskms: signing with %s: %s", s.arn, describe(err))
	}
	if len(out.Signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("awskms: %s returned a %d-byte signature, not %d", s.arn, len(out.Signature), ed25519.SignatureSize)
	}
	return out.Signature, nil
}

// describe names the AWS error code when there is one, since that is what
// an operator searches for.
func describe(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode() + ": " + api.ErrorMessage()
	}
	return err.Error()
}
```

Check the `types` constant names against the installed SDK with `go doc github.com/aws/aws-sdk-go-v2/service/kms/types KeySpec` and `SigningAlgorithmSpec`. Use the real names; they're expected to be `KeySpecEccNistEdwards25519` and `SigningAlgorithmSpecEd25519Sha512`.

In `cmd/awd/main.go`, extend `loadSigner`:

```go
// loadSigner reads AWD_SIGNING_KEY's value: a seed file written by keygen,
// or awskms:<key ARN> for a key held in AWS KMS.
func loadSigner(ctx context.Context, value string) (signing.Signer, error) {
	if arnValue, ok := strings.CutPrefix(value, "awskms:"); ok {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return awskms.Load(ctx, arnValue)
	}
	key, err := signing.LoadSeed(value)
	if err != nil {
		return nil, err
	}
	return signing.NewSeedSigner(key), nil
}
```

Update the usage lines for `AWD_SIGNING_KEY` and `AWD_SIGNING_KEY_PREVIOUS` to read: `path to a signing key written by keygen, or awskms:<key ARN>`.

- [ ] **Step 5: README**

In the signing section, add a subsection "Keys in AWS KMS" containing:
- One line explaining why v2 exists (KMS signs at most 4 KiB).
- `AWD_SIGNING_KEY=awskms:arn:aws:kms:<region>:<account>:key/<id>`.
- The key must be `ECC_NIST_EDWARDS25519` / `SIGN_VERIFY`.
- The IAM policy (JSON) granting `kms:Sign` and `kms:GetPublicKey` on that key ARN only.
- A note that self-hosted seed keys need no change: machines move to v2 by themselves once `aw-sync` is upgraded.
- A note that a KMS-signed control plane answers `426` to an `aw-sync` too old for v2.

Also add a short "Manual check" list: create the key (`aws kms create-key --key-spec ECC_NIST_EDWARDS25519 --key-usage SIGN_VERIFY`), start `awd` with it, enroll a machine, and confirm `aw doctor` shows `bundle signature: verified (v2, key …)`.

- [ ] **Step 6: Run tests**

Run: `GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./...` (plus the Postgres suite as usual).
Expected: PASS, with `go 1.24` still in `go.mod`.

- [ ] **Step 7: Commit**

```bash
git add internal/signing/awskms cmd/awd go.mod go.sum README.md
git commit -m "feat(awd): sign bundles with an Ed25519 key in AWS KMS (AWD_SIGNING_KEY=awskms:<arn>)"
```

---

## Failure-mode → test map (for the final review)

| Row | Test |
|---|---|
| KMS unreachable/denied at startup | `TestNewNamesTheAWSErrorCode` |
| Not Ed25519 / not SIGN_VERIFY | `TestNewRejectsTheWrongKey` |
| Malformed `awskms:` | `TestLoadRejectsMalformedARNs`, `TestLoadSignerRejectsAMalformedKMSValue` |
| Sign fails on a 200 → 503 | `TestSignerFailureIs503NeverUnsigned`, `TestSignFailuresAreErrors` |
| 304 rollover uncached → 503; cached served | `TestRolloverOn304UsesCacheAndFailsClosedWhenUncached` |
| Signature not 64 bytes | `TestSignFailuresAreErrors` |
| KMS + client without v2 → 426 | `TestNegotiationTable` (kms rows) |
| v2 key ID mismatch | `TestFetchV2KeyIDMismatchNamesBothIDs` |
| v2 signature bad | `TestFetchV2BadSignatureFails`, `TestVerifyFilesV2RejectsAChangedBody` |
| Unknown response format | `TestFetchUnknownFormatIsNamed` |
| Unknown `.sig` token | `TestVerifyFilesUnknownTokenIsNamed` |
| Old aw-sync + seed → v1 | `TestNegotiationTable` (seed, old client) |
| New aw-sync + old awd → v1 | `TestFetchNoFormatHeaderIsV1` |
| Over 4096 bytes to KMS | `TestSignRefusesOversizeBeforeCallingKMS` |
