---
title: Upgrading
section: security-review
order: 5
description: Four behavior changes in the security hardening release, and what to do about each before you upgrade.
---

# Upgrading

The security hardening release changes four behaviors. Each one fails closed: it refuses and leaves the old files in place. Read them before you upgrade `awd` and the clients. Upgrade `awd` first.

## 1. Plain HTTP to a remote host is refused

`aw-sync enroll`, `aw-sync run` and the `awd` admin commands that send the admin token refuse an `http://` URL unless the host is `localhost`, an address in `127.0.0.0/8` or `::1`. They send no request. `aw-sync run` checks the server saved at enrollment too, so a machine that enrolled over plain HTTP to a remote host stops syncing after the upgrade. It keeps the files it has.

Fix: put TLS in front of `awd` and use an `https://` URL. For a machine already enrolled over HTTP, re-enroll it with an `https://` server and `--force`. The quickstart uses `http://localhost:8080` and is not affected.

Evidence: `TestCheckTransport`, `TestRunRefusesASavedPlainHTTPServer` in `internal/sync/transport_test.go`; `TestEnrollRefusesPlainHTTP` in `cmd/aw-sync/transport_test.go`; `TestAdminCLIRefusesPlainHTTP` in `cmd/awd/transport_test.go`.

## 2. An older policy revision is refused

`aw-sync` now keeps the newest `revisedAt` it has rendered. When a key is pinned, it refuses a bundle with an older time, keeps the old files and reports: `policy revision time T is older than last rendered U; apply the policy again on the control plane`. A bundle from an older `awd` has no `revisedAt`, so it is refused too once a time is stored. That matters if you downgrade `awd`.

Fix: apply the policy once on the control plane (`awd apply`). A new apply gets a later time, including an apply of old rules. Run it after you restore a database from backup. Do not downgrade `awd` under a fleet that has synced. If you must, run `aw-sync enroll --force` on each machine, which clears the stored time.

Evidence: `TestBundleCarriesRevisedAt`, `TestRollbackApplyMovesRevisedAtForward` in `internal/handler/bundle_test.go`; `TestRunRefusesAnOlderRevision`, `TestRunRefusesMissingRevisedAtOnceStored` in `internal/sync/sync_test.go`; `TestForceReenrollResetsRevisedAt` in `cmd/aw-sync/e2e_test.go`.

## 3. SSO checks that the email is verified

When `AWD_CONSOLE_USER_CLAIM` is `email` (the default), the console sign-in refuses an ID token unless `email_verified` or `xms_edov` is JSON `true` or the string `"true"` (403). Other user claims are not checked, because the IdP controls them.

Microsoft Entra ID cannot send `email_verified`. Its equivalent is the optional claim `xms_edov`, which Entra only sends together with `email`.

- Entra with `AWD_CONSOLE_USER_CLAIM=preferred_username` (the documented setup): no change.
- Entra with `email`: in the app registration, open Token configuration, choose Add optional claim, pick the ID token type and add `xms_edov` (and `email`). The portal may flag `xms_edov` as unrecognized; that is expected. Add the claim before you upgrade, or switch to `preferred_username`.

Workaround if you are locked out: the admin token still works. Use it to run `awd` commands and the API while you fix the IdP.

Evidence: `TestXmsEdovCountsAsVerified`, `TestNonEmailUserClaimSkipsEmailVerified` in `internal/console/sso/sso_test.go`; `TestConsoleMissingEmailVerifiedIs403`, `TestConsoleUnverifiedEmailIs403` in `internal/handler/console_test.go`; `TestEmailVerifiedParsingIsStrict` in `internal/console/sso/sso_test.go`.

## 4. `aw-policy` fails closed when a key is pinned

With no `requireBundle` setting in `aw-policy.json`, `aw-policy` now exits non-zero on a missing, bad or tampered bundle when the machine has a trust file (a key is pinned). Claude Code then refuses to start. Without a trust file it still fails open. A trust file it cannot read counts as pinned.

Fix: make sure every pinned machine has synced a good bundle before you upgrade (`aw doctor` shows it). To keep the old behavior, set `"requireBundle": false` in `aw-policy.json`. Set `"requireBundle": true` to fail closed on every machine, pinned or not.

Evidence: `TestFailsClosedWhenAKeyIsPinned`, `TestExplicitRequireBundleFalseFailsOpen` in `internal/policyhelper/helper_test.go`.

## Also changed

- `GET /v1/policy` needs the admin token or a console admin session. Send `Authorization: Bearer $AWD_ADMIN_TOKEN` (`TestReadingPolicyNeedsAdmin` in `internal/handler/auth_test.go`). No client in the repository calls it.
- A token from a signing `awd` ends in `.<keyId>`. `aw-sync enroll` refuses a server that presents a different key (`TestEnrollRefusesAKeyIDMismatch` in `internal/sync/client_test.go`). Tokens from an older `awd` have no suffix and enroll as before.
