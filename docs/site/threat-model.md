---
title: Threat model
section: security-review
order: 2
description: STRIDE over each component. For every threat, the mitigation in code and the residual risk.
---

# Threat model

We walked STRIDE over each component: Spoofing, Tampering, Repudiation, Information disclosure, Denial of service, Elevation of privilege. Each row gives the threat, the mitigation with the file or test that enforces it, and what is left. A row with no mitigation says so.

The attacker we design against is a developer without root on their own machine, plus anyone on the network between the machine and the control plane. Root on the endpoint is out of scope; see [Security model and limits](./security-model.md). Paths are relative to the [repository](https://github.com/alchristleo/agentic-warden).

## awd, the control plane

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | Someone calls an admin route without the admin token | Constant-time token check; admin routes answer 503 when no token is set (`internal/handler/auth.go`; `TestApplyWithTheWrongAdminTokenIs401`, `TestAdminRoutesAre503WhenNoTokenIsConfigured` in `auth_test.go`) | One shared token for all token-path admins. Treat it like a root password. |
| S | A stolen or guessed enrollment token | Single-use, 24h default, 7 days maximum (`internal/handler/enroll.go`; `TestATokenEnrollsOneMachine`, `TestMintingATokenCapsTheTTL`) | Whoever holds an unspent token can enroll as that user until it expires. Deliver tokens out of band. |
| S | A stolen machine credential | Only its SHA-256 is stored, so a database copy yields no working credential (`internal/credential/credential.go`); `awd revoke` makes the next fetch a 401 (`TestRevokingAMachine`, `TestARevokedMachineIs401`) | The credential sits in `machine.json` on the endpoint. On Windows its 0600 mode does nothing, so restrict the directory (`deploy/aw-sync/README.md`). |
| T | An attacker changes the policy | Policy writes need the admin token or a console admin session; each write is audited, and a failed audit write fails the request (`TestEachAdminWriteIsAuditedWithTheTokenActor`, `TestAuditFailureFailsTheWrite` in `audit_test.go`) | Anyone with the admin token or console admin rights can change policy. Revisions are kept, so changes are visible. |
| R | An admin denies a change | Audit events record the actor. Console admins are recorded by their identity (`TestConsoleSessionCanUseAdminAPIAndIsTheActor`) | On the token path the actor label is `token:` plus the caller's `X-Applied-By` header, which the caller chooses (`internal/handler/auth.go`). It is a label, not proof of who acted. |
| I | `GET /v1/policy` leaks the policy | None. The route is open on purpose as a preview (`TestReadingPolicyNeedsNoToken`) | Anyone who can reach `awd` can read the compiled policy for any group and repository. Keep `awd` off the public internet or put it behind your network controls. The policy lists rules, not secrets, but the rules are not public. |
| I | A bundle fetch reveals another user's policy | A bundle is resolved from the calling machine's credential (`TestBundleIsResolvedForTheMachinesUser`) | None found beyond the open preview route above. |
| D | Large or malformed requests | Request bodies are size-capped on policy, enrollment, groups and SCIM routes (`internal/handler/handler.go`, `enroll.go`, `groups.go`, `scim.go`; `TestSCIMBodyOverTheCapIs413`) | We found no rate limiting in `awd`. Put a limiter in front of it. |
| D | `awd` is down | `aw-policy` is offline, and `aw-sync` leaves files as they were (`TestRunKeepsFilesWhenTheServerIsUnreachable`) | Policy changes stop reaching machines until `awd` returns. |
| E | A non-admin gets console admin | OIDC sign-in, an admin group that must come from the IdP, `email_verified` required, re-check of group membership on each request, state and origin checks (`TestConsoleNonAdminIs403AtLogin`, `TestConsoleUnverifiedEmailIs403`, `TestConsoleAdminRemovedMidSessionIs403AndSessionsDeleted`, `TestConsoleCallbackStateMismatchIs400`, `TestConsoleWriteNeedsOrigin` in `console_test.go`; cookie flags in `console_auth.go`) | The console is only as good as your IdP and its group data. Matching is exact; no case folding (`TestConsoleLoginCaseMismatchIsDenied`). |
| – | Traffic is read or altered in transit | `awd` listens on plain HTTP (`cmd/awd/main.go`). TLS is expected from whatever sits in front of it. The hosted cell terminates TLS at the load balancer and requires TLS to its database (`database_url_requires_tls` in `deploy/aws/cell/tests/cell.tftest.hcl`) | A self-hosted `awd` without TLS exposes credentials and tokens. Signed bundles still detect a changed body, but a stolen credential works. |

## The bundle

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | A forged bundle is served to a machine | Ed25519 signature, key pinned at enrollment, checked before the body is parsed; a missing signature is refused once a key is pinned (`internal/signing`, `internal/sync/client.go`; `TestFetchRejectsATamperedBody`, `TestFetchRequiresASignatureWhenAKeyIsPinned`) | Signing is optional. With `AWD_SIGNING_KEY` unset, bundles are unsigned and a machine has nothing to check (`TestUnsignedServerSendsNoSignatureHeaders`, `TestFetchWithNoPinnedKeySkipsVerification`). |
| T | The file on disk is edited after sync | `aw-policy` verifies bundle, signature and trust file at every launch; `aw` refuses to launch on a failed check (`TestResolvePolicyRefusesATamperedBundle` in `cmd/aw/doctor_test.go`; `TestRunRefusesATamperedBundle`); `aw doctor` warns on loose permissions (`TestInspectWarnsWhenTheBundleIsWorldWritable`) | Root can replace the bundle, the trust file and the checker together. Detection, not prevention. |
| T | A key rollover is forged | Only a statement signed by the currently pinned key moves the pin (`TestFetchRejectsAForgedRollover`, `TestRunRepinsOnARollover`) | Whoever holds the current signing key can hand the fleet to a new one. |
| T | An old signed bundle is replayed | None found. The bundle's `Version` is signed but the verifier never checks it, and there is no expiry (`internal/policy/bundle.go`; `BundleStatement` in `internal/signing/signer.go`) | A writer of the state directory, or a stale cache, can restore an older signed policy. Root-owned directories limit who can. |
| I | The bundle is read by a developer | By design. It is mode 0644 so the developer's `aw-policy` can read it (`deploy/managed-settings/README.md`) | The bundle holds the organization's rules, not secrets. Do not put secrets in rules. |
| D | A huge bundle | The client caps what it reads (`maxBundleBytes` in `internal/sync/client.go`); the helper checks Claude Code's output limit (`TestOutputStaysUnderClaudeCodesLimit`) | A bundle over the 4 MiB cap fails the fetch and leaves the old files in force. |

## aw-sync

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | A fake control plane at the enrolled address | The pinned signing key (see the bundle) | Unsigned deployments have no defense here beyond TLS to the real host. |
| T | A half-written or mixed set of governance files | All-or-nothing writes; nothing is written when any renderer or validation fails (`TestRunWritesNothingForAnyAgentWhenOneRendererFails`, `TestRunWritesNothingWhenTheBundleFailsValidation`) | If a later write fails after earlier ones landed, state records what was written (`TestRunRecordsWhatItWroteWhenALaterWriteFails`); the files can be mixed until the next good cycle. |
| T | A user swaps the files a root timer uses | The timer refuses state directories and binaries that a non-root user could replace (`cmd/aw-sync/rootonly_unix.go`; `TestRootOnlyRefusesAUserOwnedPath`, `TestRootOnlyRefusesAWritableParent`) | On Windows this only warns. Check ACLs by hand. |
| T | Drift: someone edits a rendered file | The next cycle rewrites it, even if the ETag matches (`TestRunRepairsDriftEvenWhenTheETagStillMatches`) | Between cycles the edit is in force. |
| E | The sync runs as root | Writes only the paths its renderers name; a renderer path outside its root is rejected (`TestRunRejectsAnUnsafeRendererPath`) | It is a root process fed by network input. Keep it updated and keep the signing on. |
| D | The control plane revokes or loses a machine | A revoked credential is reported (`TestRunReportsARevokedCredential`) | The machine keeps its last rendered policy. |

## aw and aw-policy on the developer machine

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| T | A developer points the helper at a bundle they wrote | The overrides are compiled out of release builds (`cmd/aw-policy/env_release.go`, `TestAReleaseBuildIgnoresTheEnvironmentOverrides`, `TestAReleaseBuildIgnoresASelfSignedStateDirectory`); `aw doctor` flags a tagged build (`TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides`) | A packager who ships a `-tags awtest` binary defeats this. Check `aw-policy build-info` prints `env-overrides=off`. |
| T | A developer edits the repository origin to dodge a repo rule | None. `.git/config` is the developer's file (`internal/repo`) | Repository-scoped rules can be avoided. Put what must hold everywhere in organization-wide rules. |
| T | A developer runs the agent without the wrapper | The helper runs on `claude` itself (`TestEmitsTheBundleAsManagedSettings`) | `codex` and `gemini` have no helper. Bare use sees only the machine-wide files. |
| T | Bad settings stop Claude Code or slip through | Output is validated against the Claude Code schema and never emitted if invalid (`TestASchemaViolationIsNeverEmitted`); the helper always exits 0 unless `requireBundle` is set (`TestAMissingBundleStillExitsZero`) | The default fails open. A missing, bad or tampered bundle yields an empty envelope. `requireBundle` fails closed. |
| R | Nobody can tell what policy a session ran under | Each helper run attempts to append one JSON line with source, `version`, groups, repository and exit code to `<user-cache>/agent-wrapper/aw-policy/aw-policy.log` (`TestEveryRunIsAudited`). It skips silently if the directory is unset or a mkdir, open or marshal step fails (`internal/policyhelper/audit.go`, `TestNoAuditDirMeansNoAuditAndNoFailure`) | The log is a local file and can be missing. We found no shipping of it off the machine. |
| E | A shadowing channel replaces the helper | `aw doctor` detects server-managed settings that shadow it (`TestInspectDetectsServerManagedSettingsShadowingTheHelper`) | An organization that enables both channels loses the helper's rules. |

## The IdP sync (groups and SCIM)

| STRIDE | Threat | Mitigation | Residual risk |
|---|---|---|---|
| S | Someone posts groups or SCIM data as the IdP | SCIM is off without `AWD_SCIM_TOKEN` and uses a constant-time bearer check (`internal/handler/scim.go`; `TestSCIMIsDisabledWithoutAToken`, `TestSCIMRefusesAWrongToken`); the snapshot route needs the admin token | The SCIM token is a shared secret. A holder can add any user to any group, including the console admin group, if that group comes from SCIM. |
| T | A user keeps group access after leaving | Deactivating a user drops their SCIM groups on the next sync (README, SCIM section) | It does not revoke their machines. Run `awd revoke` too. This is documented behavior; we have no test for it. |
| T | Group names that differ only by case | Matching is exact on the enrolled email (`TestConsoleLoginCaseMismatchIsDenied`); `awd groups resolve` warns on a SCIM case mismatch (README) | A mismatched case means the user gets fewer groups than expected, not more. |
| E | Console admin from a policy-authored group | The admin group must come from the IdP, not the authored `groups` map (README, "Admin console") | We have no test that names this rule. |

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
