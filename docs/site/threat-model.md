---
title: Threat model
section: security-review
order: 2
description: STRIDE over each component. For every threat, the mitigation in code and the residual risk.
---

# Threat model

We walked STRIDE over each component: Spoofing, Tampering, Repudiation, Information disclosure, Denial of service, Elevation of privilege. Each row gives the threat, the mitigation with the file or test that enforces it, and what is left. A row with no mitigation says so.

The attacker we design against is a developer without root on their own machine, plus anyone on the network between the machine and the control plane. Root on the endpoint is out of scope; see [Security model and limits](./security-model.md). Paths are relative to the [repository](https://github.com/alchristleo/agentic-warden).

## Design limits

These are how the product works today, and the operator closes them. The full text, with evidence, is in [Security model and limits](./security-model.md). Behavior that changed in the hardening release is in [Upgrading](./upgrading.md).

- Fails open on a machine with no pinned key. Once a key is pinned it fails closed by default; `requireBundle` overrides either way. See [Upgrading](./upgrading.md).
- `awd` speaks plain HTTP. Terminate TLS in front of it. `aw-sync` and the `awd` admin commands refuse plain HTTP to any host but loopback.
- The signing key is pinned on first use. A token from a signing `awd` carries the key id, so the pin is checked out of band. A token with no key id (no signer, or an older `awd`) pins whatever the server presents; enroll those over TLS.
- `awd` has no rate limit. Tokens are 256-bit, so guessing is not feasible, but a flood is not slowed. Put a limiter in front of it. We did not build one because behind a load balancer every client shares one IP.
- The audit actor on the token path is the caller's `X-Applied-By` label. It is a label, not proof. Use console admin sessions where attribution matters.
- Two replays are not detected. A bundle for the same revision with older group membership, and a control plane whose clock is set backwards (new policy waits until the clock passes the old time). Both need a writer on the state directory or the control plane host.
- Writing policy is running code on every enrolled machine. Treat the admin token, console admin rights and the signing key as fleet-root credentials.
- It is a client-side control, not a boundary against a local administrator.
- Managed settings bind the Claude Code your organization installs. A developer who runs their own older or patched copy is outside them. Pair with executable allow-listing.

## awd, the control plane

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | Someone calls an admin route without the admin token | Constant-time token check; admin routes answer 503 when no token is set (`internal/handler/auth.go`; `TestApplyWithTheWrongAdminTokenIs401`, `TestAdminRoutesAre503WhenNoTokenIsConfigured` in `auth_test.go`) | One shared token for all token-path admins. Treat it like a root password. |
| S | A stolen or guessed enrollment token | Single-use, 24h default, 7 days maximum (`internal/handler/enroll.go`; `TestATokenEnrollsOneMachine`, `TestMintingATokenCapsTheTTL`). A token from a signing `awd` ends in the key id (`TestEnrollmentTokenCarriesKeyID`) | Whoever holds an unspent token can enroll as that user until it expires. Deliver tokens out of band. |
| S | A stolen machine credential | Only its SHA-256 is stored, so a database copy yields no working credential (`internal/credential/credential.go`); `awd revoke` makes the next fetch a 401 (`TestRevokingAMachine`, `TestARevokedMachineIs401`) | The credential sits in `machine.json` on the endpoint. On Windows its 0600 mode does nothing, so restrict the directory (`deploy/aw-sync/README.md`). |
| T | An attacker changes the policy | Policy writes need the admin token or a console admin session; each write is audited, and a failed audit write fails the request (`TestEachAdminWriteIsAuditedWithTheTokenActor`, `TestAuditFailureFailsTheWrite` in `audit_test.go`) | Anyone with the admin token or console admin rights can change policy. Revisions are kept, so changes are visible. |
| R | An admin denies a change | Audit events record the actor. Console admins are recorded by their identity (`TestConsoleSessionCanUseAdminAPIAndIsTheActor`) | On the token path the actor label is `token:` plus the caller's `X-Applied-By` header, which the caller chooses (`internal/handler/auth.go`). It is a label, not proof of who acted. Use console admin sessions where attribution matters. |
| I | `GET /v1/policy` leaks the policy | The route needs the admin token or a console admin session (`TestReadingPolicyNeedsAdmin` in `internal/handler/auth_test.go`) | Anyone with admin rights can read the compiled policy. The policy lists rules, not secrets, but the rules are not public. |
| I | A bundle fetch reveals another user's policy | A bundle is resolved from the calling machine's credential (`TestBundleIsResolvedForTheMachinesUser`) | None found. |
| D | Large or malformed requests | Request bodies are size-capped on policy, enrollment, groups and SCIM routes (`internal/handler/handler.go`, `enroll.go`, `groups.go`, `scim.go`; `TestSCIMBodyOverTheCapIs413`) | `awd` has no rate limit. Tokens are 256-bit, so brute force is not feasible, but a flood is not slowed. Put a limiter in front of it. |
| D | `awd` is down | `aw-policy` is offline, and `aw-sync` leaves files as they were (`TestRunKeepsFilesWhenTheServerIsUnreachable`) | Policy changes stop reaching machines until `awd` returns. |
| E | A non-admin gets console admin | OIDC sign-in, an admin group that must come from the IdP, a missing or false `email_verified` claim is refused, re-check of group membership on each request, state and origin checks (`TestEmailVerifiedParsingIsStrict` in `internal/console/sso/sso_test.go`; `TestConsoleNonAdminIs403AtLogin`, `TestConsoleUnverifiedEmailIs403`, `TestConsoleMissingEmailVerifiedIs403`, `TestConsoleAdminRemovedMidSessionIs403AndSessionsDeleted`, `TestConsoleCallbackStateMismatchIs400`, `TestConsoleWriteNeedsOrigin` in `console_test.go`; cookie flags in `console_auth.go`) | The console is only as good as your IdP and its group data. Matching is exact; no case folding (`TestConsoleLoginCaseMismatchIsDenied`). |
| E | A policy author runs code on enrolled machines | Policy writes need the admin token or a console admin session, and each write is audited (see the tampering row above). The managed settings a rule can set include `hooks`, `env` and `apiKeyHelper`, which the vendored schema accepts (`internal/agent/claude/schema/claude-code-settings.json`) | Not prevented. Whoever can write policy can run commands on every enrolled machine, so the admin token, console admin rights and the signing key are fleet-root credentials. Guard them like root. |
| – | Traffic is read or altered in transit | `awd` listens on plain HTTP (`cmd/awd/main.go`). TLS is expected from whatever sits in front of it. `aw-sync enroll` and `run` and the `awd` admin commands refuse plain HTTP to a non-loopback host and send nothing (`TestCheckTransport`, `TestRunRefusesASavedPlainHTTPServer` in `internal/sync/transport_test.go`; `TestEnrollRefusesPlainHTTP` in `cmd/aw-sync/transport_test.go`; `TestAdminCLIRefusesPlainHTTP` in `cmd/awd/transport_test.go`). The hosted cell terminates TLS at the load balancer and requires TLS to its database (`database_url_requires_tls` in `deploy/aws/cell/tests/cell.tftest.hcl`) | A self-hosted `awd` without TLS still exposes credentials and tokens to anyone on the path between it and a loopback client. Signed bundles still detect a changed body, but a stolen credential works. |

## The bundle

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | A forged bundle is served to a machine | Ed25519 signature, key pinned at enrollment, checked before the body is parsed; a missing signature is refused once a key is pinned (`internal/signing`, `internal/sync/client.go`; `TestFetchRejectsATamperedBody`, `TestFetchRequiresASignatureWhenAKeyIsPinned`) | Signing is optional. With `AWD_SIGNING_KEY` unset, bundles are unsigned and a machine has nothing to check (`TestUnsignedServerSendsNoSignatureHeaders`, `TestFetchWithNoPinnedKeySkipsVerification`). Only machines enrolled after the key is set pin it. The key is pinned on first use (`Enroll` in `internal/sync/client.go`). A token from a signing `awd` carries the key id and `Enroll` refuses a different one (`TestEnrollRefusesAKeyIDMismatch`, `TestEnrollRefusesAMissingKeyWhenTokenNamesOne`, `TestEnrollAcceptsAMatchingKeyID` in `internal/sync/client_test.go`; `TestForceReenrollChecksKeyID` in `cmd/aw-sync/e2e_test.go`). A token without a key id still pins what the server presents. |
| T | The file on disk is edited after sync | on a signed deployment `aw-policy` verifies bundle, signature and trust file at every launch; `aw` refuses to launch on a failed check (`TestResolvePolicyRefusesATamperedBundle` in `cmd/aw/doctor_test.go`; `TestRunRefusesATamperedBundle`); `aw doctor` warns on loose permissions (`TestInspectWarnsWhenTheBundleIsWorldWritable`) | Root can replace the bundle, the trust file and the checker together. Detection, not prevention. |
| T | A key rollover is forged | Only a statement signed by the currently pinned key moves the pin, or a root re-enrollment (`aw-sync enroll --force`, `cmd/aw-sync/main.go`), which is trust on first use again (`TestFetchRejectsAForgedRollover`, `TestRunRepinsOnARollover`) | Whoever holds the current signing key can hand the fleet to a new one. |
| T | An old signed bundle is replayed | The bundle carries `revisedAt`, the current revision's creation time, covered by the signature (`TestBundleCarriesRevisedAt`, `TestRollbackApplyMovesRevisedAtForward` in `internal/handler/bundle_test.go`). With a key pinned, `aw-sync` refuses a lower value and keeps the old files; a missing value is refused once one is stored (`TestRunRefusesAnOlderRevision`, `TestRunRefusesMissingRevisedAtOnceStored`, `TestRunAcceptsSameOrNewerRevision`, `TestRolloverKeepsRevisedAt` in `internal/sync/sync_test.go`). `enroll --force` resets it (`TestForceReenrollResetsRevisedAt`); a restart of `awd` followed by one apply is accepted (`TestRestartThenApplyIsAccepted` in `cmd/awd/e2e_test.go`) | Replay of the same revision with older group membership is not detected. A control plane clock set backwards delays new policy until it passes the old time. A writer of the state directory can also rewrite the stored time. After a database restore, apply the policy once; `enroll --force` also recovers a machine. |
| I | The bundle is read by a developer | By design. It is mode 0644 so the developer's `aw-policy` can read it (`deploy/managed-settings/README.md`) | The bundle holds the organization's rules, not secrets. Do not put secrets in rules. |
| D | A huge bundle | The client caps what it reads (`maxBundleBytes` in `internal/sync/client.go`); the helper checks Claude Code's output limit (`TestOutputStaysUnderClaudeCodesLimit`) | A bundle over the 4 MiB cap fails the fetch and leaves the old files in force. |

## aw-sync

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | A fake control plane at the enrolled address | The pinned signing key (see the bundle) | The pin is trust on first use: a machine enrolled over plain HTTP may have been given the wrong key. Unsigned deployments have no defense here beyond TLS to the real host. |
| T | A half-written or mixed set of governance files | Nothing is written unless every renderer and validation pass (`TestRunWritesNothingForAnyAgentWhenOneRendererFails`, `TestRunWritesNothingWhenTheBundleFailsValidation`) | If a later write fails after earlier ones landed, state records what was written (`TestRunRecordsWhatItWroteWhenALaterWriteFails`); the files can be mixed until the next good cycle. |
| T | A user swaps the files a root timer uses | The timer refuses state directories and binaries that a non-root user could replace (`cmd/aw-sync/rootonly_unix.go`; `TestRootOnlyRefusesAUserOwnedPath`, `TestRootOnlyRefusesAWritableParent`) | On Windows this only warns. Check ACLs by hand. |
| T | Drift: someone edits a rendered file | The next cycle rewrites it, even if the ETag matches (`TestRunRepairsDriftEvenWhenTheETagStillMatches`) | Between cycles the edit is in force. |
| E | The sync runs as root | Writes only the paths its renderers name; a renderer path outside its root is rejected (`TestRunRejectsAnUnsafeRendererPath`) | It is a root process fed by network input. Keep it updated and keep the signing on. |
| D | The control plane revokes or loses a machine | A revoked credential is reported (`TestRunReportsARevokedCredential`) | The machine keeps its last rendered policy. |

## aw and aw-policy on the developer machine

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| T | A developer points the helper at a bundle they wrote | The overrides are compiled out of release builds (`cmd/aw-policy/env_release.go`, `TestAReleaseBuildIgnoresTheEnvironmentOverrides`, `TestAReleaseBuildIgnoresASelfSignedStateDirectory`); `aw doctor` flags a tagged build (`TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides`) | A packager who ships a `-tags awtest` binary defeats this. Check `aw-policy build-info` prints `env-overrides=off`. |
| T | A developer edits the repository origin to dodge a repo rule | None. `.git/config` is the developer's file (`internal/repo`) | Repository-scoped rules can be avoided. Put what must hold everywhere in organization-wide rules. |
| T | A developer runs the agent without the wrapper | The helper runs on `claude` itself (`TestEmitsTheBundleAsManagedSettings`) | `codex` and `gemini` have no helper. Bare use sees only the machine-wide files. A developer who runs their own older or patched `claude` is outside managed settings; pair with executable allow-listing. |
| T | Bad settings stop Claude Code or slip through | Output is validated against the Claude Code schema and never emitted if invalid (`TestASchemaViolationIsNeverEmitted`); the helper exits 0 on a failed check only when no key is pinned or `requireBundle` is `false`. A pinned machine fails closed by default (`TestFailsClosedWhenAKeyIsPinned`, `TestFailsOpenWhenUnsigned`, `TestUnreadableTrustFileFailsClosed`, `TestNullRequireBundleIsAbsent`, `TestExplicitRequireBundleFalseFailsOpen`, `TestRequireBundleTrueFailsClosed` in `internal/policyhelper/helper_test.go`) | A machine with no pinned key (unsigned deployment, or enrolled before signing) fails open: a missing, bad or tampered bundle yields an empty envelope. `requireBundle: true` fails closed everywhere. |
| R | Nobody can tell what policy a session ran under | Each helper run attempts to append one JSON line with source, `version`, groups, repository and exit code to `<user-cache>/agent-wrapper/aw-policy/aw-policy.log` (`TestEveryRunIsAudited`). It skips silently if the directory is unset or a mkdir, open or marshal step fails (`internal/policyhelper/audit.go`, `TestNoAuditDirMeansNoAuditAndNoFailure`) | The log is a local file and can be missing. We found no shipping of it off the machine. |
| E | A shadowing channel replaces the helper | `aw doctor` detects server-managed settings that shadow it (`TestInspectDetectsServerManagedSettingsShadowingTheHelper`) | An organization that enables both channels loses the helper's rules. |

## The IdP sync (groups and SCIM)

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | Someone posts groups or SCIM data as the IdP | SCIM is off without `AWD_SCIM_TOKEN` and uses a constant-time bearer check (`internal/handler/scim.go`; `TestSCIMIsDisabledWithoutAToken`, `TestSCIMRefusesAWrongToken`); the snapshot route needs the admin token | The SCIM token is a shared secret. A holder can add any user to any group, including the console admin group, if that group comes from SCIM. |
| T | A user keeps group access after leaving | An inactive user's SCIM groups drop out of the next bundle (`TestBundleGroupsIncludeActiveSCIMGroups` in `internal/handler/resolve_test.go`; [Write your first policy](./write-first-policy.md)) | It does not revoke their machines. Run `awd revoke` too. |
| T | Group names that differ only by case | Matching is exact on group names (`GroupsFor` in `internal/policy/bundle.go`); `awd groups resolve` warns on a SCIM case mismatch (`TestResolveFlagsACaseMismatch` in `internal/handler/resolve_test.go`; [Write your first policy](./write-first-policy.md)) | User emails or group names that differ only by case do not match. The user gets fewer groups, so group-scoped rules, denies included, do not apply. |
| E | Console admin from a policy-authored group | The admin group must come from the IdP, not the authored `groups` map (`TestAuthoredGroupsNeverCount` in `internal/console/authz/authz_test.go`; [Admin console](./admin-console.md)) | The console is still only as good as the IdP's group data. |

## Hosted cells

Design-partner pilot. The trust statement, linked from [Trust statement](./trust.md), is the plain-language version.

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S / T | The signing key is used to sign a policy you did not write | KMS key; its policy denies `kms:Sign` to every principal except the cell's task role; the task role may only call `Sign` and `GetPublicKey` on its own key (`key_policy_confines_signing_to_the_task_role`, `task_role_signs_only_with_own_key` in `deploy/aws/cell/tests/cell.tftest.hcl`) | We hold the AWS account. A person with the right access can change the key policy or sign through the service, and a signature is valid whoever asked for it. Co-signed revisions, where a key you hold must also sign, are planned and not built. |
| R | We deny having signed something | Every `Sign` call is recorded in CloudTrail (stated in `docs/hosting/trust.md`; CloudTrail is AWS's, not configured by this repository) | We can share the events for the last 90 days of AWS event history. |
| I | Another partner reads your data | Each cell has its own database, secrets, key and IAM role. The database is private, encrypted and reachable only from the cell's task (`database_is_private_encrypted_protected`, `database_accepts_only_the_task`); secrets are secrets, not environment values (`secrets_are_secrets_not_environment`); the task image is pinned by digest (`image_is_pinned_by_digest`) | Cells share the network, load balancer, certificate, image repository, cluster and AWS account. The infrastructure state bucket holds every cell's generated secrets, encrypted. |
| I | Our operators read your admin token | Not prevented | Operators can read the cell's admin and SCIM tokens from state and Secrets Manager. We use them only to support you, when you ask. |
| D | Loss of the database or a cell | Deletion protection and 7-day automated backups (`database_is_private_encrypted_protected`) | One database instance per cell, no alarms or paging yet, no availability promise. |
| E | An administrator widens access | Changes to the key policy are recorded in CloudTrail (trust.md) | Not prevented. |

## Not covered here

- The Claude Code, Codex and Gemini binaries themselves, and the model providers.
- Prompt injection and what an agent does inside the rules it is allowed.
- Root or administrator on the developer machine.
