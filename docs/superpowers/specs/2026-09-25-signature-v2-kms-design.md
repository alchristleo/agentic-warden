# Signature format v2 and a pluggable signer with AWS KMS

Date: 2026-09-25. Status: approved for planning. Spec A of milestone (e)'s
second half, the hosted design-partner pilot. Spec B (hosted cells:
Terraform, tenant provisioning, runbook) builds on this one and comes next.
Extends `2026-09-22-signed-bundles-design.md`.

## Why this exists

The hosted pilot runs one `awd` cell per tenant. Each tenant's signing key
is held by us in AWS KMS: non-exportable, one key per tenant, every
signature logged in CloudTrail, each cell's IAM role limited to its own key.

A bundle carries Claude Code managed settings, hooks included. Hooks run
commands on developer machines, so whoever can sign a tenant's bundle can
run code on that tenant's fleet. The pilot's trust statement names that
risk plainly: the tenant trusts our hosting, as with any SaaS. The planned
upgrade is customer-co-signed revisions, and this spec leaves room for it.

Today `awd` signs the **whole bundle body** with plain Ed25519. KMS signs
plain Ed25519 over a raw message, and AWS caps a raw message at 4 KiB. A
real organization's bundle exceeds that. GCP does not document its limit.
So the current scheme cannot sign through KMS. v2 signs a short fixed-size
statement over the body's hash instead.

## Decisions

- **Signature format v2.** `awd` signs a ~90-byte statement naming the key
  and the SHA-256 of the body. It stays within every KMS limit, and it is
  simple enough to check by hand.
- **The client advertises the formats it accepts.** `aw-sync` says which
  formats it verifies, and `awd` picks the best one both support. Self-hosted
  fleets move to v2 as their `aw-sync` upgrades, with no switch to flip. The
  KMS signer only speaks v2 and refuses older clients loudly.
- **A pluggable `Signer`.** The seed file stays for self-hosted. AWS KMS is
  the first managed backend. A GCP backend can be added later without
  touching the protocol.
- **The rollover statement is unchanged.** `aw-key-rollover-v1` is under
  200 bytes, so KMS can sign it as-is.
- **The Go floor rises from 1.22 to 1.24.** Every AWS SDK for Go v2 KMS
  release that knows Ed25519 needs Go 1.23 or later, and current releases
  need 1.24. Go 1.22 has also been out of upstream security support since
  early 2025. The higher floor also ends plain `go mod tidy` rewriting the
  directive through older test dependencies.

## Protocol

### The v2 statement

These exact bytes, UTF-8, with every line ending in `\n`:

    aw-bundle-v2
    <key id>
    <lowercase hex SHA-256 of the exact response body bytes>

The signature is plain Ed25519 (RFC 8032, not Ed25519ph) over those bytes,
and travels base64 (standard, padded) as today. The key ID binds a
signature to one key. The domain tag stops a v2 bundle signature from being
replayed as any other statement this system signs. `aw-key-rollover-v1` has
its own tag.

### Negotiation on `GET /v1/bundle`

The request carries a new header:

    X-AW-Signature-Formats: v1, v2

- A comma-separated list, with optional whitespace. Format names are
  matched ignoring case.
- An absent header means `v1`. That is every `aw-sync` before this change.

On a 200, the response carries a new header, `X-AW-Signature-Format: v1|v2`,
alongside the existing `X-AW-Signature` and `X-AW-Key-Id`.

A response with signatures but no format header is v1, so a new `aw-sync`
keeps working against an old `awd`.

How `awd` chooses:

| Signer | Client offers v2 | Client offers only v1 (or no header) | Client offers only unknown formats |
|---|---|---|---|
| none (unsigned deployment) | no signature headers, as today | no signature headers, as today | no signature headers, as today |
| seed file | v2 | v1 (signature over the body, as today) | 400 naming the formats awd speaks |
| AWS KMS | v2 | 426 `aw-sync too old for this control plane's signing; upgrade aw-sync to a build that supports signature format v2` | 400 naming the formats awd speaks |

A 304 carries no bundle signature, as today. `X-AW-Key-Rollover` still
rides both the 200 and the 304.

### Client verification (`internal/sync`)

`aw-sync` always sends `X-AW-Signature-Formats: v1, v2`. On a 200 from a
signed server, it runs these checks in order before parsing anything:

1. Read `X-AW-Signature-Format` (absent means v1). An unknown value fails
   the cycle and names the value.
2. Handle `X-AW-Key-Rollover` exactly as today, repinning if the statement
   verifies.
3. **v1:** verify the signature over the body with the pinned key, as today.
4. **v2:** `X-AW-Key-Id` must equal the pinned key's ID; otherwise the cycle
   fails naming both IDs. Rebuild the statement from the pinned ID and the
   SHA-256 of the received body, then verify.
5. Any failure is handled exactly as a bad v1 signature is today: nothing
   is written for any agent, exit 1, and the timer retries.

A machine with no pinned key (enrolled before signing existed) still
verifies nothing. It does send the formats header.

### On disk

`<state dir>/aw-bundle.json.sig` is one line whose first token names the
scheme:

    aw-ed25519 <key id> <base64 signature>       # v1, as today
    aw-ed25519-v2 <key id> <base64 signature>    # v2

`signing.VerifyFiles`, shared by `aw-policy` and `aw doctor`, dispatches on
that token. It rebuilds the v2 statement from the key ID in the pinned
`aw-trust.pub` and the bundle file's SHA-256. An unknown token fails
verification and names the token, so `aw-policy`'s behaviour on a bad
signature stays exactly as today.

`aw doctor` prints `bundle signature: verified (v2, key 3f9a1c22b0d41e77)`.
v1 prints `v1` in the same place.

### Observability

Migration `0007_machine_signature_format.sql` adds
`machines.last_signature_format text NOT NULL DEFAULT ''`. `TouchMachine`
records the format served on each 200: `v1`, `v2`, or empty when unsigned.

`awd machines` shows the format beside the key ID. That column tells a
self-hosted operator whether the whole fleet is on v2.

## Signer

### Interface (`internal/signing`)

```go
// Signer produces plain Ed25519 signatures. Implementations may be remote
// (KMS), so Sign takes a context and can fail.
type Signer interface {
	Public() ed25519.PublicKey
	Sign(ctx context.Context, msg []byte) ([]byte, error)
	// Formats lists the bundle signature formats this signer can produce,
	// best first: a seed signs any size ("v2", "v1"); KMS signs only v2.
	Formats() []string
}

// NewSeedSigner wraps a key loaded by LoadSeed.
func NewSeedSigner(key ed25519.PrivateKey) Signer

// BundleStatement returns the exact v2 statement bytes.
func BundleStatement(keyID string, body []byte) []byte
```

The existing `Sign`/`Verify` helpers stay for v1 and for rollover.
`SignRollover` takes a `Signer` for the outgoing key, so the previous key
can be a seed or KMS.

### `internal/signing/awskms`

```go
type API interface { // the two KMS calls used, so tests use a fake
	Sign(ctx context.Context, in *kms.SignInput, opts ...func(*kms.Options)) (*kms.SignOutput, error)
	GetPublicKey(ctx context.Context, in *kms.GetPublicKeyInput, opts ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
}

func New(ctx context.Context, api API, keyARN string) (*Signer, error)
```

`New` calls `GetPublicKey` once and refuses a key whose `KeySpec` is not
`ECC_NIST_EDWARDS25519` or whose `KeyUsage` is not `SIGN_VERIFY`. It parses
the DER with `x509.ParsePKIXPublicKey` and requires an `ed25519.PublicKey`.

`Sign` calls KMS with `SigningAlgorithm ED25519_SHA_512` and
`MessageType RAW`. It refuses a message over 4096 bytes before calling KMS,
so a future caller that tries to sign a body directly fails in tests, not in
production. It checks the returned signature is 64 bytes.

`Formats()` returns `["v2"]`.

### Signature cache (`internal/handler`)

A bounded LRU of 1024 entries maps statement bytes to signature. It holds
only public data, needs no expiry, and is shared by the bundle and rollover
paths. Machines that receive the same policy produce the same body, so a
steady fleet makes a handful of KMS calls per policy change.

### Configuration

`AWD_SIGNING_KEY` and `AWD_SIGNING_KEY_PREVIOUS` accept either:
- a file path, the seed file exactly as today, or
- `awskms:<key ARN>`, which uses the default AWS credential chain (on hosted
  cells, the ECS task role; the region comes from the ARN).

`awd` logs the signer kind and key ID at startup. It never logs
credentials. `awd keygen` stays seed-only: KMS keys are created in spec B's
Terraform.

The four combinations of current and previous key (seed or KMS each) all
work. A KMS previous key signs only the rollover statement.

## Failure modes

The implementation plan copies every row into its Global Constraints, and
each row gets a test.

| Condition | Behavior |
|---|---|
| `AWD_SIGNING_KEY=awskms:…` and KMS unreachable or denied at startup | `awd` refuses to start, naming the ARN and the AWS error code |
| KMS key is not Ed25519 or not SIGN_VERIFY | `awd` refuses to start, naming the key spec and usage found |
| Malformed `awskms:` value (not an ARN) | `awd` refuses to start |
| KMS `Sign` fails or throttles on a 200 | 503 `signing unavailable`, never an unsigned body; logged with the AWS error code |
| KMS fails on a 304 whose rollover signature is not cached | 503 as above; cached, it is served |
| KMS returns a signature that is not 64 bytes | treated as a `Sign` failure (503) |
| KMS signer, client offers no v2 | 426 with the upgrade message; the machine keeps its current bundle |
| Client offers only unknown formats | 400 naming the formats `awd` speaks |
| v2 response, `X-AW-Key-Id` ≠ pinned key ID | cycle fails naming both IDs; nothing written |
| v2 signature does not verify | cycle fails as a bad v1 signature does today |
| Unknown `X-AW-Signature-Format` in a response | cycle fails naming the value |
| `.sig` file with an unknown scheme token | `VerifyFiles` fails naming the token; `aw-policy` behaves as for a bad signature |
| Old `aw-sync` (no formats header) against a seed signer | v1, exactly as today |
| New `aw-sync` against an old `awd` (no format header) | v1 verification, exactly as today |
| Message over 4096 bytes passed to the KMS signer | error before any KMS call |

## Testing

- **`signing` units.**
  - `BundleStatement` golden bytes.
  - v2 sign and verify round trip with a seed.
  - `VerifyFiles`: v1, v2, unknown token, v2 with a body change, v2 with a
    mismatched key ID.
  - A fuzz test on format-header parsing.
- **`awskms` against a fake `API`.**
  - `New` rejects wrong spec, wrong usage, non-Ed25519 DER, and a
    `GetPublicKey` error.
  - `Sign` sends `ED25519_SHA_512` and `RAW`, rejects over 4096 bytes, and
    rejects a bad signature length.
  - A signature from a fake backed by a real `ed25519` key verifies through
    the client-side v2 code.
- **Handler.**
  - The negotiation table, every cell.
  - The cache: a second identical body makes no second `Sign` call.
  - 503 on signer failure.
  - The 304 rollover paths.
  - `last_signature_format` recorded.
- **Sync.** Client verification for v1 and v2 against a real `awd`
  handler, one test per failure row in this spec, and repin on rollover
  followed by v2 verification with the new key.
- **Store conformance.** `TouchMachine` stores the format, on memory and on
  Postgres.
- **Manual smoke test** (documented in the README, not run in CI):
  - create an Ed25519 KMS key,
  - run `awd` with `AWD_SIGNING_KEY=awskms:<arn>`,
  - enroll a machine with `aw-sync`,
  - check that `aw doctor` reports `verified (v2, …)`.
- **CI.** Workflows move to `go-version: stable`. `go.mod` says `go 1.24`.

## Documentation

- README: signing section covers v2, the formats header, `awskms:` keys
  with the IAM policy a cell needs (`kms:Sign` and `kms:GetPublicKey` on
  its own key only), and the self-hosted rollout path (upgrade `aw-sync`,
  then watch `awd machines` for v2).
- The trust statement for design partners, including the "our key can run
  hooks" risk and the co-signing upgrade path, is written in spec B, where
  the hosting it describes is designed.

## Out of scope, deliberately

- Terraform, cell provisioning and the runbook (spec B).
- A GCP KMS backend. The interface allows it; nothing needs it yet.
- Customer-co-signed revisions (the planned custody upgrade).
- Dropping v1. Self-hosted fleets keep it until their `aw-sync` upgrades.
- Ed25519ph.
