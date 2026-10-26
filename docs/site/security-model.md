---
title: Security model and limits
section: security-review
order: 1
description: What Agentic Warden enforces, what it only detects, and what it cannot stop. Read this before you buy.
---

# Security model and limits

Agentic Warden is a client-side control. It is not a security boundary against a person with administrator rights on their own machine. This page says what it holds, what it only detects, and where it stops. Each claim names the file or test that backs it, so you can check us. Paths are relative to the [repository](https://github.com/alchristleo/agentic-warden).

## What it holds

**Bare `claude` is governed, not just `aw claude`.** Claude Code runs the `policyHelper` named in a managed-settings drop-in and applies its output as managed settings. How managed settings rank against a developer's own is Claude Code's behavior, not ours; the recorded run in [quickstart](./quickstart.md) step 6, through `aw claude` and `--settings`, shows an organization deny beating a command-line allow; the repository has no recorded run of bare `claude` with the helper. Install `aw-sync` and `aw-policy` once and a developer who skips the wrapper still gets the policy. Evidence: `TestEmitsTheBundleAsManagedSettings` in `cmd/aw-policy/e2e_test.go`, and `deploy/managed-settings/README.md`.

**A developer without root cannot choose their own bundle.** For `aw-policy`, the `AW_POLICY_BUNDLE`, `AW_POLICY_CONFIG` and `AW_SYNC_STATE_DIR` overrides exist only in builds made with `-tags awtest`. A release build ignores them and says so on stderr. The wrapper `aw` honors `AW_SYNC_STATE_DIR` by design (`stateDirFor` in `cmd/aw/main.go`), because `aw` is a convenience a developer can bypass; `aw doctor` says when it reads a redirected directory. Evidence: `cmd/aw-policy/env_release.go`, `TestAReleaseBuildIgnoresTheEnvironmentOverrides` and `TestAReleaseBuildIgnoresASelfSignedStateDirectory` in `cmd/aw-policy/e2e_test.go`. `aw doctor` warns if a tagged helper is installed (`TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides` in `internal/agent/claude/inspect_test.go`).

**Settings that fail the schema are never emitted.** The helper validates its output against the vendored Claude Code settings schema first. Evidence: `TestASchemaViolationIsNeverEmitted` in `internal/policyhelper/helper_test.go`.

**A bundle is checked against a pinned key before it is used.** When the control plane signs bundles, a machine pins the key it is given at enrollment and verifies every fetch against it before parsing the body. The pin is trust on first use, but a token from a signing control plane carries the key id, so a different key is refused (see [design limits](./threat-model.md#design-limits)). Signing applies only to machines enrolled after the key is set; an earlier machine pins nothing and `aw doctor` warns "enrolled before bundle signing; re-enroll to pin a key" (`inspectPin` in `internal/agent/claude/inspect.go`). Only a rollover statement signed by the pinned key can move the pin, or a root re-enrollment (`aw-sync enroll --force`, `cmd/aw-sync/main.go`), which is trust on first use again. A failed check writes nothing. Evidence: `TestFetchRequiresASignatureWhenAKeyIsPinned`, `TestFetchRejectsATamperedBody` and `TestFetchRejectsAForgedRollover` in `internal/sync/client_test.go`; `TestRunWritesNothingWhenVerificationFails` in `internal/sync/sync_test.go`.

**On a signed deployment, `aw-policy` re-checks the bundle on disk at every launch.** It verifies the bundle, its signature and the trust file together before compiling. Evidence: `TestRunRefusesATamperedBundle`, `TestRunRefusesAMissingSignature` and `TestRunRefusesAnUnreadableTrustFile` in `internal/policyhelper/helper_test.go`.

**Sync stops before writing on a validation failure, and a failing renderer skips only its agent.** If the bundle fails validation, nothing is written for any agent. If one agent's renderer fails, aw-sync writes nothing for that agent, still writes the other agents, and records the error in state. If the server is unreachable, the files stay as they were. If a later write fails after earlier ones landed, those earlier files stay in place and state records what was written, so the set can be mixed until the next good cycle (`TestRunRecordsWhatItWroteWhenALaterWriteFails`). Evidence: `TestRunWritesOtherAgentsWhenOneRendererFails`, `TestRunWritesNothingWhenTheBundleFailsValidation` and `TestRunKeepsFilesWhenTheServerIsUnreachable` in `internal/sync/sync_test.go`.

**Signing keys are handled with care.** A seed file that others can read is refused, and `awd keygen` will not overwrite an existing key (`TestLoadSeedRefusesAReadableFile`, `TestWriteSeedDoesNotOverwrite` in `internal/signing/signing_test.go`). `awd` refuses a KMS key that is not `ECC_NIST_EDWARDS25519` with `SIGN_VERIFY` usage (`internal/signing/awskms/awskms.go`, `TestNewRejectsTheWrongKey`). A signer failure answers 503, never an unsigned bundle (`TestSignerFailureIs503NeverUnsigned` in `internal/handler/bundle_test.go`).

**Credentials are stored hashed, and enrollment tokens are single-use and capped at seven days.** `internal/credential/credential.go` keeps only a SHA-256 of each machine credential. Evidence for the token rules: `TestATokenEnrollsOneMachine` and `TestMintingATokenCapsTheTTL` in `internal/handler/enroll_test.go`. A revoked machine's next fetch gets a 401 (`TestARevokedMachineIs401` in `internal/handler/bundle_test.go`).

**Admin routes close when no admin token is set.** They answer 503 instead of opening (`TestAdminRoutesAre503WhenNoTokenIsConfigured` in `internal/handler/auth_test.go`). The token is compared in constant time (`internal/handler/auth.go`).

## What it only detects

**Signing detects tampering. It cannot stop a root user.** The signature shows that the bundle on disk is the one `aw-sync` fetched. The trust key and the bundle sit in the same root-owned state directory, and root can replace the binary that does the checking as easily as the bundle. Against genuine root, signing is detection. The trust key (`aw-trust.pub`), the signature and the bundle all sit in the state directory, so anyone who can write there can replace all three together, or delete the key (`inspectOwnership` in `internal/agent/claude/inspect.go`). The one thing signing adds is protection on the fetch path: it stops an impersonated control plane from feeding a machine a bundle, because the anchor there is the 0600 `machine.json` and not a file next to what it verifies.

`aw-sync install-timer` refuses a state directory a non-root user could write; `aw doctor` warns when the bundle or trust file is not root-owned or is group/other-writable. Root ownership of the directory is what keeps users out.

It does not prevent a bundle copied between machines, restored from a backup or served by a stale cache. A v2 signature covers the key id and the body hash (`BundleStatement` in `internal/signing/signer.go`); v1 signs the body bytes directly. The bundle's `Version` field is a label the verifier does not check, and there is no expiry or machine field (`internal/policy/bundle.go`), so a copied or restored bundle still verifies.

The fetch-path tests are the ones above. [How it works](./how-it-works.md) has the longer account.

**`aw doctor` reports ownership and mode, with one gap.** It warns when the bundle or the trust file is writable by more than its owner (`TestInspectWarnsWhenTheBundleIsWorldWritable`, `TestInspectWarnsWhenTheTrustFileIsWorldWritable` in `internal/agent/claude/inspect_test.go`). On Windows it cannot read the ACL and says so (`TestInspectReportsOwnershipAsUncheckedOnWindows`). Check the ACLs of `C:\ProgramData\agent-wrapper` by hand.

**`aw-sync` refuses paths a user could swap under a root timer.** To install the timer, the state directory and binary must be owned by root and not writable by group or others (`cmd/aw-sync/rootonly_unix.go`, `TestRootOnlyRefusesAUserOwnedPath`). On Windows the check only warns (`TestCheckRootOnlyOnlyWarnsOnWindows`).

## What it does not stop

- **A developer with local administrator rights** can edit the managed source or remove the drop-in. Pair Agentic Warden with scheduled MDM redeployment and network-level egress restriction.
- **Repository-scoped rules key on `.git/config`.** The `origin` remote there is the developer's to edit, so a rule that targets one repository can be avoided by changing it. `internal/repo` reads it (`TestDetectReadsTheOriginRemote`). There is no trusted answer to "which repository is this" on a developer machine.
- **Server-managed settings from the claude.ai console shadow the helper.** Use one channel or the other. `aw doctor` detects the collision (`TestInspectDetectsServerManagedSettingsShadowingTheHelper`).
- **`aw codex` is advisory.** It applies repository rules at launch. Bare `codex` sees only the machine-wide files `aw-sync` renders.
- **agy permissions, MCP servers, hooks and repository-scoped rules are not enforced.** aw governs only agy's gateway and models. See [How it works](./how-it-works.md#antigravity-cli-gateway-and-models-only).

The design limits, with the evidence for each, are in the [threat model](./threat-model.md#design-limits).

## What the control plane sees

Enrollment and sync requests: who enrolled, from which machine, and which policy they were sent. It never sees prompts, code or model traffic, because `awd` is not in the path of an agent session: its routes (`Routes` in `internal/handler/handler.go`) are policy, bundle, enrollment, groups, audit, SCIM and the console, with no proxy to a model provider. The [trust statement](./trust.md) covers the hosted version.

Next: the [threat model](./threat-model.md) goes component by component.
