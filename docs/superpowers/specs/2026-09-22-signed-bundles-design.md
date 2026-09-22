# Signed bundles: Ed25519 over the bundle, pinned at enrollment

Date: 2026-09-22. Status: designed. Extends
`2026-09-21-multi-agent-bundle-sync-design.md`, whose "Out of scope" listed
signed bundles with "machine credential over TLS is the v1 integrity
story". This adds the second half: proof, carried by the bytes themselves,
that a bundle on disk came from this organization's control plane.

## Why this exists

Today a bundle is trusted because of where it came from — a TLS connection
authenticated with the machine credential — and after that, because of
where it sits: a root-owned file. Neither property survives the file. A
bundle copied, cached, restored from a backup, or written into a directory
whose permissions are weaker than the deploy assumed carries no evidence
of its origin at all, and `aw-policy` applies it without question.

The threat this closes is **tampering with the bundle after it is
written**. It is deliberately not a claim about root; see "What this does
not buy" below.

## Cryptography

`internal/signing`, on `crypto/ed25519` from the standard library — no new
dependency:

```go
// LoadSeed reads a 32-byte Ed25519 seed, base64 on a single line, and
// refuses a file that is readable by group or other.
func LoadSeed(path string) (ed25519.PrivateKey, error)

// Sign returns the base64 signature over body.
func Sign(key ed25519.PrivateKey, body []byte) string

// Verify reports whether sig is key's signature over body.
func Verify(key ed25519.PublicKey, body []byte, sig string) bool

// KeyID is the first 8 bytes of SHA-256 over the public key, hex encoded.
// It names a key in headers, files and doctor output so that a mismatch
// reads as "signed by a key this machine does not trust" rather than
// "bad signature".
func KeyID(key ed25519.PublicKey) string
```

Public keys are written as `aw-ed25519:<base64>` wherever a human or a
config file sees them. Ed25519 was chosen for small keys and signatures, a
stdlib implementation, and no parameter choices to get wrong.

## awd: holding and using the key

- `AWD_SIGNING_KEY` names the seed file: root-owned, 0600. awd refuses to
  start on a missing, malformed or group/world-readable file, naming the
  path. Unset means bundles are unsigned and awd logs one warning at start;
  an existing deployment keeps working untouched.
- `awd keygen [--out PATH]` writes a new seed at 0600 and prints the public
  key and its key ID. It never overwrites an existing file.
- The private key never reaches the database. A file suits systemd
  credentials, Docker secrets and Kubernetes secrets alike.

`GET /v1/bundle`, on a 200, gains two response headers:

    X-AW-Signature: <base64 signature over the exact response body bytes>
    X-AW-Key-Id: <key id>

The signature covers the bytes the server writes, so no canonicalization
question arises. A 304 carries neither header: there is no body, and the
machine keeps the bundle and signature it already verified. Signing costs
roughly 50µs per response, so there is nothing to cache.

## Pinning at enrollment

The enroll response gains `publicKey` and `keyId`, and `aw-sync` stores
both in `machine.json` — already root-owned and 0600, and already the file
that holds the machine credential. That file is the trust root for the
fetch path.

`aw-sync` verifies the fetched bytes **before** parsing or planning
anything. A failure is handled exactly as a malformed bundle is today:
nothing is written for any agent, the error is recorded, exit 1, the timer
retries.

A machine enrolled before signing existed has an empty pin. It performs no
verification — this is what makes the rollout safe — and `aw doctor` says
so: `enrolled before bundle signing; re-enroll to pin a key`.

Every bundle request sends `X-AW-Key-Id` with the machine's pinned key, so
`awd machines` can show which machines still pin an old key. That listing
is what tells an operator a rotation has finished.

## Rotation without re-enrolling

Verification is client-side against a pinned key, so a new signing key
would break every machine until it re-enrolled. Instead:

1. The operator generates a new seed, points `AWD_SIGNING_KEY` at it and
   `AWD_SIGNING_KEY_PREVIOUS` at the old one.
2. awd adds a third response header, `X-AW-Key-Rollover`: the statement

       aw-key-rollover-v1\n<new key id>\n<new public key>

   signed **by the previous key**, base64, alongside the statement itself.
3. `aw-sync` verifies that statement with its pinned key. On success it
   repins `machine.json` to the new key, then verifies the bundle with the
   new key. On failure it ignores the header; the bundle then fails its own
   check and the cycle exits 1.
4. When `awd machines` shows every machine on the new key ID, the operator
   drops `AWD_SIGNING_KEY_PREVIOUS`.

Trust chains from the original pin, so a rotation needs no re-enrollment
and no flag day. `AWD_SIGNING_KEY_PREVIOUS` set but unreadable is a refusal
to start: a half-configured rotation must not look like a finished one.

## On disk

`aw-sync` writes three more planned files — hashed into `state.json`,
reported by `aw-sync status`, rewritten on drift, all-or-nothing with the
rest of the cycle:

| File | Contents |
| --- | --- |
| `<agent system dir>/aw-bundle.json.sig` | one line, `aw-ed25519 <key id> <base64 signature>` |
| `<state dir>/aw-bundle.json.sig` | the same, for the launch adapters' copy |
| `<system dir>/aw-trust.pub` (0644) | `aw-ed25519:<base64>`, the pinned public key |

`aw-trust.pub` exists because `aw-policy` runs as the developer and cannot
read the 0600 `machine.json`. A cycle that cannot verify writes none of
these files, as it writes none of the others.

## aw-policy and the launch adapters

`policyhelper.Run` gains one step before compiling: read
`<system dir>/aw-trust.pub` and `<bundle path>.sig`.

- Verified: proceed exactly as today.
- `aw-trust.pub` present, signature missing or failing: `{}` envelope, a
  note naming the file, an audit line, exit 0 — and exit 1 under
  `requireBundle`, matching how a missing bundle already behaves. A key
  problem never bricks a launch that would otherwise have run unpoliced.
- No `aw-trust.pub`: an unsigned deployment; behave exactly as today.

`aw codex` and `aw gemini` apply the same three cases to the state-dir
copy, and refuse to launch on a failed verification, because for them the
bundle is the only input.

## aw doctor

The Claude inspection reports one more line:

    bundle signature: verified (key 3f9a1c22b0d41e77)
    bundle signature: unsigned deployment
    bundle signature: FAILED — /etc/claude-code/aw-bundle.json does not match key 3f9a1c22b0d41e77

A failure is an Error finding. Warn findings: `aw-trust.pub` or the bundle
writable by anyone but root (mode and owner on POSIX, ACL on Windows), and
a machine with an empty pin.

## What this does not buy

Apply-time verification is worth more than the bundle's own file
permissions only when the trust anchor is harder to write than the bundle
is. Both live in the root-owned system directory, so against genuine root
this is **detection, not prevention** — the same boundary the 2026-09-22
hardening design drew, for the same reason: root can replace the binary
that does the checking.

Where it prevents rather than detects:

- `C:\ProgramData` subtrees, whose inherited ACLs routinely let a standard
  user write files the deploy assumed were administrator-only.
- A deploy that chmods or chowns the system directory wrongly.
- A compromised or impersonated `aw-sync`, and a bundle restored from a
  backup, copied between machines, or served by a stale cache.
- The fetch path generally: there the anchor is the 0600 `machine.json`,
  and the check is strong.

Saying this plainly in the README is part of the milestone. An integrity
feature that is described as more than it is will be trusted for more than
it can do.

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| awd | `AWD_SIGNING_KEY` unset | bundles unsigned; one warning at start |
| awd | seed missing, malformed, or group/world-readable | refuses to start, naming the path |
| awd | `AWD_SIGNING_KEY_PREVIOUS` set but unreadable | refuses to start |
| aw-sync | signature absent while a key is pinned | error, nothing written, exit 1 |
| aw-sync | signature present and verification fails | error, nothing written, exit 1 |
| aw-sync | rollover statement fails under the pinned key | header ignored; the bundle then fails its own check, exit 1 |
| aw-sync | no key pinned (enrolled before signing) | no verification; doctor warns |
| aw-policy | `.sig` missing or bad with `aw-trust.pub` present | `{}` + note + audit; exit 1 under `requireBundle` |
| aw-policy | no `aw-trust.pub` | today's behaviour exactly |
| aw codex / aw gemini | state-dir copy fails verification | error, no launch |

## Migration

Three deploys, each safe on its own and in this order:

1. awd gets a key. Machines with no pin are unaffected.
2. Machines re-enroll, or rotate in through the rollover header, and pin.
3. Nothing further: enforcement is live per machine from the moment it
   pins. Unsigned deployments remain supported indefinitely.

## Testing

- `signing`: sign and verify round trip; a wrong key, a truncated
  signature and a flipped byte all fail; `KeyID` is stable and depends only
  on the public key; `LoadSeed` refuses a 0644 seed.
- handler: both headers present when a key is configured and absent when
  not; the signature verifies over the exact body; a 304 carries neither;
  the rollover statement verifies under the previous key; enroll returns
  `publicKey` and `keyId`.
- `sync`: a good signature applies; a tampered body writes nothing; a
  rollover repins `machine.json` and then applies; an unpinned machine
  skips verification; the three new planned files appear in `state.json`
  and drift is detected on each.
- `cmd/aw-policy` e2e: a good signature applies; a flipped byte yields `{}`
  with a note; `requireBundle` turns that into exit 1; with no
  `aw-trust.pub` the behaviour is byte-identical to today.
- `internal/agent/claude`: the three doctor states plus the
  non-root-writable warning.
- `cmd/aw` e2e: `aw codex` refuses to launch on a bad state-dir signature.
- `GOOS=darwin` and `GOOS=windows` builds.

## Documentation

- `README.md`, "How policy reaches the agent": what is signed, what is
  pinned where, and "What this does not buy" in the author's own words.
- `deploy/aw-sync/README.md`: `awd keygen`, the two environment variables,
  and the four-step rotation runbook keyed off `awd machines`.
- `2026-09-21-multi-agent-bundle-sync-design.md`: the "Signed bundles"
  out-of-scope line points here.

## Out of scope, deliberately

- Signing the rendered agent files (Codex `requirements.toml`, Gemini
  `settings.json`, Claude's managed settings). Those agents read their
  files natively and offer no verification hook; only Claude's path runs
  through a verifier we control.
- KMS or HSM signers, X.509, and transparency logs.
- More than two keys live at once. One current key and one previous is
  what a rotation needs.
- Encrypting the bundle. It is the organization's policy, not a secret;
  the parent design says so and that is unchanged.
