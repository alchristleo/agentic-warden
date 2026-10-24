---
title: Security model and limits
section: security-review
order: 1
description: What Agentic Warden enforces, what it only detects, and what it cannot stop. Read this before you buy.
---

# Security model and limits

Agentic Warden is a client-side control. It is not a security boundary against a person with administrator rights on their own machine. This page says what it holds, what it only detects, and where it stops. Each claim names the file or test that backs it, so you can check us. Paths are relative to the [repository](https://github.com/alchristleo/agentic-warden).

## What it holds

**Bare `claude` is governed, not just `aw claude`.** Claude Code runs the `policyHelper` named in a managed-settings drop-in and uses its output above the command line, project and user settings. Install `aw-sync` and `aw-policy` once and a developer who skips the wrapper still gets the policy. Evidence: `TestEmitsTheBundleAsManagedSettings` in `cmd/aw-policy/e2e_test.go`, and `deploy/managed-settings/README.md`.

**A developer without root cannot choose their own bundle.** The `AW_POLICY_BUNDLE`, `AW_POLICY_CONFIG` and `AW_SYNC_STATE_DIR` overrides exist only in builds made with `-tags awtest`. A release build ignores them and says so on stderr. Evidence: `cmd/aw-policy/env_release.go`, `TestAReleaseBuildIgnoresTheEnvironmentOverrides` and `TestAReleaseBuildIgnoresASelfSignedStateDirectory` in `cmd/aw-policy/e2e_test.go`. `aw doctor` warns if a tagged helper is installed (`TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides` in `internal/agent/claude/inspect_test.go`).

**Settings that fail the schema are never emitted.** The helper validates its output against the vendored Claude Code settings schema first. Evidence: `TestASchemaViolationIsNeverEmitted` in `internal/policyhelper/helper_test.go`.

**A bundle is checked against a pinned key before it is used.** When the control plane signs bundles, a machine pins the key at enrollment and verifies every fetch against it before parsing the body. Only a rollover statement signed by the pinned key can move the pin. A failed check writes nothing. Evidence: `TestFetchRequiresASignatureWhenAKeyIsPinned`, `TestFetchRejectsATamperedBody` and `TestFetchRejectsAForgedRollover` in `internal/sync/client_test.go`; `TestRunWritesNothingWhenVerificationFails` in `internal/sync/sync_test.go`.

**`aw-policy` re-checks the bundle on disk at every launch.** It verifies the bundle, its signature and the trust file together before compiling. Evidence: `TestRunRefusesATamperedBundle`, `TestRunRefusesAMissingSignature` and `TestRunRefusesAnUnreadableTrustFile` in `internal/policyhelper/helper_test.go`.

**Sync stops before writing on a renderer or validation failure.** If any agent's renderer fails, or the bundle fails validation, nothing is written for any agent. If the server is unreachable, the files stay as they were. If a later write fails after earlier ones landed, those earlier files stay in place and state records what was written, so the set can be mixed until the next good cycle (`TestRunRecordsWhatItWroteWhenALaterWriteFails`). Evidence: `TestRunWritesNothingForAnyAgentWhenOneRendererFails`, `TestRunWritesNothingWhenTheBundleFailsValidation` and `TestRunKeepsFilesWhenTheServerIsUnreachable` in `internal/sync/sync_test.go`.

**Signing keys are handled with care.** A seed file that others can read is refused, and `awd keygen` will not overwrite an existing key (`TestLoadSeedRefusesAReadableFile`, `TestWriteSeedDoesNotOverwrite` in `internal/signing/signing_test.go`). `awd` refuses a KMS key that is not `ECC_NIST_EDWARDS25519` with `SIGN_VERIFY` usage (`internal/signing/awskms/awskms.go`, `TestNewRejectsTheWrongKey`). A signer failure answers 503, never an unsigned bundle (`TestSignerFailureIs503NeverUnsigned` in `internal/handler/bundle_test.go`).

**Credentials are stored hashed, and enrollment tokens are single-use and capped at seven days.** `internal/credential/credential.go` keeps only a SHA-256 of each machine credential. Evidence for the token rules: `TestATokenEnrollsOneMachine` and `TestMintingATokenCapsTheTTL` in `internal/handler/enroll_test.go`. A revoked machine's next fetch gets a 401 (`TestARevokedMachineIs401` in `internal/handler/bundle_test.go`).

**Admin routes close when no admin token is set.** They answer 503 instead of opening (`TestAdminRoutesAre503WhenNoTokenIsConfigured` in `internal/handler/auth_test.go`). The token is compared in constant time (`internal/handler/auth.go`).

## What it only detects

**Signing detects tampering. It cannot stop a root user.** The signature shows that the bundle on disk is the one `aw-sync` fetched. The trust key and the bundle sit in the same root-owned state directory, and root can replace the binary that does the checking as easily as the bundle. Against genuine root, signing is detection. It does prevent:

- a `C:\ProgramData` subtree whose inherited ACLs let a standard user write files the deploy assumed were administrator-only (caught, though the loose ACL itself is not something `aw doctor` can read)
- a state directory with the wrong owner or mode
- a compromised or impersonated `aw-sync`, on the fetch path, where the anchor is the 0600 `machine.json` and not a file next to what it verifies

It does not prevent a bundle copied between machines, restored from a backup or served by a stale cache. A v2 signature covers the key id and the body hash (`BundleStatement` in `internal/signing/signer.go`); v1 signs the body bytes directly. The bundle's `Version` field is a label the verifier does not check, and there is no expiry or machine field (`internal/policy/bundle.go`), so a copied or restored bundle still verifies.

This list is from the "Signing and its limits" section of [How it works](./how-it-works.md). The fetch-path tests are the ones above.

**`aw doctor` reports ownership and mode, with one gap.** It warns when the bundle or the trust file is writable by more than its owner (`TestInspectWarnsWhenTheBundleIsWorldWritable`, `TestInspectWarnsWhenTheTrustFileIsWorldWritable` in `internal/agent/claude/inspect_test.go`). On Windows it cannot read the ACL and says so (`TestInspectReportsOwnershipAsUncheckedOnWindows`). Check the ACLs of `C:\ProgramData\agent-wrapper` by hand.

**`aw-sync` refuses paths a user could swap under a root timer.** To install the timer, the state directory and binary must be owned by root and not writable by group or others (`cmd/aw-sync/rootonly_unix.go`, `TestRootOnlyRefusesAUserOwnedPath`). On Windows the check only warns (`TestCheckRootOnlyOnlyWarnsOnWindows`).

## What it does not stop

- **A developer with local administrator rights** can edit the managed source, remove the drop-in or run a modified client. Pair Agentic Warden with scheduled MDM redeployment and network-level egress restriction.
- **Repository-scoped rules key on `.git/config`.** The `origin` remote there is the developer's to edit, so a rule that targets one repository can be avoided by changing it. `internal/repo` reads it (`TestDetectReadsTheOriginRemote`). There is no trusted answer to "which repository is this" on a developer machine.
- **It fails open by default.** With no bundle, an unusable bundle or one that fails verification, `aw-policy` prints an empty envelope and exits 0, and Claude Code falls back to the static managed-settings files. Set `requireBundle: true` in `aw-policy.json` and it exits non-zero, so Claude Code refuses to start. Evidence: `TestAMissingBundleStillExitsZero`, `TestATamperedSignedBundleProducesAnEmptyEnvelope` and `TestATamperedSignedBundleFailsClosedUnderRequireBundle` in `cmd/aw-policy/e2e_test.go`. Under `requireBundle`, a machine with a good verified bundle keeps launching. Machines that never synced, or whose bundle is bad, are blocked, so the choice is yours. Compiled output of 1 MiB or more gets the same treatment: an empty envelope with exit 0, or exit 1 under `requireBundle` (`internal/policyhelper/helper.go`).
- **A machine that cannot reach the control plane keeps the policy it has.** We found no expiry on a bundle. Evidence for the keeping: `TestRunKeepsFilesWhenTheServerIsUnreachable`.
- **A validly signed old bundle still verifies.** A v2 signature covers the key id and the SHA-256 of the bundle bytes (`BundleStatement` in `internal/signing/signer.go`). The verifier never compares the bundle's `Version` to anything and we found no expiry in the verify path, so signing alone does not stop someone with write access from putting an earlier signed bundle back.
- **Server-managed settings from the claude.ai console shadow the helper.** Use one channel or the other. `aw doctor` detects the collision (`TestInspectDetectsServerManagedSettingsShadowingTheHelper`).
- **`aw codex` and `aw gemini` are advisory.** They apply repository rules at launch. Bare `codex` or `gemini` sees only the machine-wide files `aw-sync` renders.
- **A machine-wide Gemini rule outranks a repo-scoped rule for the same tool.** Keep machine-wide `policies` to what must hold everywhere. Source: README, "How policy reaches the agent". We have no test for Gemini's tier ordering, because it is Gemini's behavior.

## What the control plane sees

Enrollment and sync requests: who enrolled, from which machine, and which policy they were sent. It never sees prompts, code or model traffic, because `awd` is not in the path of an agent session: its routes (`Routes` in `internal/handler/handler.go`) are policy, bundle, enrollment, groups, audit, SCIM and the console, with no proxy to a model provider. The [trust statement](./trust.md) covers the hosted version.

Next: the [threat model](./threat-model.md) goes component by component.
