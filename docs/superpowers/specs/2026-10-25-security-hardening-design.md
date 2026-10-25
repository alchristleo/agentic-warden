# Security hardening — design

Date: 2026-10-25. Size: Large (trust boundary). Owner decisions from the scope gate and question round, 2026-10-25.

## Goal

Close the known gaps an attacker can use, listed under "Known issues" in `docs/site/threat-model.md`. Add no new knobs.

1. `GET /v1/policy` needs no token.
2. The signing key is pinned on first use (TOFU), and `aw-sync enroll --force` pins on first use again.
3. Plain HTTP exposes credentials and lets an attacker in the path swap the key at enrollment.
4. Nothing checks a signed bundle's `Version`, so an old signed bundle can be replayed.
5. SSO accepts a token that has no `email_verified` claim.
6. `aw-policy` fails open by default.

## Non-goals

- A built-in rate limiter. Tokens are 256-bit, so brute force is infeasible. Behind a load balancer every client shares one IP, and per-IP limits would need an `X-Forwarded-For` trust knob. We document this as a deployment concern.
- Per-admin tokens. `X-Applied-By` stays a label. We document it as a design limit: console sessions give real attribution.
- Policy write = code execution on the fleet (co-signed revisions). Backlog.
- Anti reverse-engineering: signed short-lived skill bundles, watermarking. Moved to the paid-skills milestone. Backlog.
- Native TLS in `awd`. TLS stays at the proxy or load balancer.
- Rollback by someone who can write the root-owned state directory. That writer can also rewrite the stored revision time. This stays a documented residual risk.

## Decisions

- **D1 Policy preview route.** `GET /v1/policy` goes behind `requireAdmin` (admin token or console admin session). No client in the repo calls it. The only callers are tests.
- **D2 Key id in the enrollment token (one-way door: token format).** Two shapes were considered:
  - (A) `awd` mints `<secret>.<keyId>` when a signer is set. The stored hash covers the whole string, so `ConsumeEnrollmentToken` does not change. `aw-sync` splits on the last `.` and refuses an enrollment whose response `keyId` differs or is missing.
  - (B) a separate `aw-sync enroll --key-id` flag.

  We chose **A**: it adds no knob. The token travels out of band, so the pin is verified even over HTTP. A token without a suffix (no signer, or an older `awd`) behaves as before. `--force` takes the same path, so a re-pin is covered too. Key ids are hex (`signing.KeyID`) and the secret is base64 RawURL (`credential.New`), so the last `.` is unambiguous. A stripped suffix is only possible on a channel D3 already refuses (non-loopback http); over https it cannot be stripped.
- **D3 Transport.** `aw-sync` (`enroll`, `run`) and the `awd` admin CLI subcommands that send `AWD_ADMIN_TOKEN` refuse an `http://` URL unless the host is loopback (`localhost`, `127.0.0.0/8`, `::1`). One shared helper does the check. Loopback means the host is exactly `localhost` (case-insensitive) or parses as a loopback IP; no DNS lookup, so `localhost.evil.com` fails. `run` re-checks the server saved at enrollment, so a machine enrolled over http to a remote host stops syncing (keeps its files) after upgrade: upgrade-note line. It adds no flag. The quickstart uses `http://localhost:8080` and is unaffected.
- **D4 Rollback (one-way door: bundle wire format).** Shapes considered: (A) signed store `Seq`; (B) signed per-fetch `issuedAt`; (C) signed `revisedAt` = the current revision's `CreatedAt` in Unix milliseconds. Chosen **C**. A is reset by the memory store on every restart (`memory.go` counter) and by a database restore, so a pinned fleet would refuse forever (pre-mortem 1, 2). B changes the body every fetch, breaking ETag caching and `cachingSigner` (a KMS `Sign` per fetch). C is stable per revision and only moves forward: any new apply, including a rollback to old rules, a restart-then-apply or a post-restore apply, gets a later time. The body's hash is signed, so no signature-format change. `aw-sync` keeps the highest `revisedAt` rendered in `state.json`; when a key is pinned it refuses a lower one, keeps the old files and reports `policy revision time T is older than last rendered U; apply the policy again on the control plane`. The stored value survives a key rollover. A missing field (older `awd`) reads 0 and is refused once a value is stored: upgrade-note line for downgrades. Recovery after a database restore: re-apply the policy (one `awd apply`); `enroll --force` also resets it. Residual: same-revision replay with older group membership is not detected; a control-plane clock set backwards delays new policy until it passes the old time. Both documented.
- **D5 SSO.** Revised 2026-10-25 (owner): Entra ID has no `email_verified` claim; its equivalent is `xms_edov`. The check applies only when `AWD_CONSOLE_USER_CLAIM` is `email` (the default), because the claim attests the email, not other identifiers. Then `email_verified` or `xms_edov` must be JSON `true` or the string `"true"` (case-insensitive); missing, `false`, `"false"`, `null`, numbers and anything else get 403. Other user claims (e.g. Entra's documented `preferred_username`) are not checked: the IdP controls them. Break-glass: the admin token path still works. Upgrade note: an Entra tenant using `email` must add the `xms_edov` optional claim before upgrading, or switch to `preferred_username`.
- **D6 Fail mode.** `requireBundle` becomes tri-state:
  - absent: fail closed when a trust file exists (a key is pinned), open otherwise;
  - `true`: always closed;
  - `false`: always open (explicit opt-out).

  "Closed" uses the existing non-zero exit path. JSON `null` reads as absent. A trust file that exists but cannot be read (permission, corruption) counts as present, so it fails closed. Upgrade note: deployments with a pinned key flip to fail-closed on upgrade; set `requireBundle: false` to keep the old behavior.
- **D8 Upgrade notes.** New page `docs/site/upgrading.md` (owner choice, 2026-10-25) lists the four upgrade behavior changes: http refusal (D3), downgrade refusal (D4), Entra claim (D5), fail-closed default (D6).
- **D7 Docs.** Each fixed gap leaves "Known issues". The STRIDE rows are updated. The rate limiter and `X-Applied-By` move to documented design limits.

## Design

- `internal/handler/handler.go`: wrap `getPolicy` in `requireAdmin`. Update the e2e and handler tests.
- `internal/handler/enroll.go`: append `.` + key id to `plain` when `h.Signer != nil`, and hash the full string.
- `internal/sync/client.go`: `Enroll` parses the suffix and checks the response `keyId`. A new `internal/sync` (or small shared package) `CheckTransport(url) error` is used by `aw-sync` and the `awd` CLI.
- `internal/policy/bundle.go`: `Bundle.RevisedAt int64 json:"revisedAt,omitempty"`, filled from `Revision.CreatedAt.UnixMilli()` at resolve time.
- `internal/sync` run cycle + `State.RevisedAt`: refuse a lower value when pinned; kept across rollover. `cmd/aw-sync` enroll `--force` resets it.
- `internal/console/sso/sso.go`: a missing claim is refused.
- `internal/policyhelper/helper.go` + `cmd/aw-policy/main.go`: `RequireBundle *bool`, plus `exitCode` using trust-file presence.
- Docs: `docs/site/threat-model.md`, the admin-console/SSO doc (Entra note), and the `aw-sync` README (https rule).

## Acceptance criteria

1. An anonymous `GET /v1/policy` returns 401. With the admin token it returns 200. Check: `go test ./internal/handler -run 'TestReadingPolicyNeedsAdmin' -v`.
2. With a signer configured, a minted enrollment token ends in `.<keyId>`, and the token still enrolls a machine exactly once. Check: `go test ./internal/handler -run 'TestEnrollmentTokenCarriesKeyID|TestATokenEnrollsOneMachine' -v`.
3. `aw-sync enroll` refuses, and writes no state, when the token's key id differs from the response's `keyId` or the response has none. It succeeds when they match, and with an unsuffixed token. Check: `go test ./internal/sync ./cmd/aw-sync -run 'TestEnrollRefusesAKeyIDMismatch|TestEnrollRefusesAMissingKeyWhenTokenNamesOne|TestEnrollAcceptsAMatchingKeyID|TestEnrollWithoutKeyIDSuffixStillPins' -v`.
4. `--force` re-enrollment applies the same key-id check. Check: `go test ./cmd/aw-sync -run 'TestForceReenrollChecksKeyID' -v`.
5. `aw-sync enroll`/`run` and the `awd` admin CLI refuse `http://` to a non-loopback host, and send no request. `http://localhost`, `http://127.0.0.1` and `http://[::1]` and `http://LOCALHOST` are allowed; `http://localhost.evil.com` and `http://user@evil.com` are refused; `run` re-checks the saved server. Check: `go test ./internal/sync ./cmd/aw-sync ./cmd/awd -run 'TestCheckTransport|TestRunRefusesASavedPlainHTTPServer|TestEnrollRefusesPlainHTTP|TestAdminCLIRefusesPlainHTTP' -v`.
6. A served bundle carries `revisedAt` equal to the current revision's `CreatedAt` in Unix ms, covered by the signature; a rollback apply (old rules re-applied) carries a later value. Check: `go test ./internal/handler -run 'TestBundleCarriesRevisedAt|TestRollbackApplyMovesRevisedAtForward' -v`.
7. With a key pinned, `aw-sync run` refuses a bundle with a lower `revisedAt` (including a missing one once a value is stored), keeps the old files, reports the D4 text, and accepts an equal or higher value; the stored value survives a key rollover. Check: `go test ./internal/sync -run 'TestRunRefusesAnOlderRevision|TestRunAcceptsSameOrNewerRevision|TestRunRefusesMissingRevisedAtOnceStored|TestRolloverKeepsRevisedAt' -v`.
8. `enroll --force` resets the stored value; an awd restart (memory store) followed by one apply is accepted by a pinned client. Check: `go test ./cmd/aw-sync ./cmd/awd -run 'TestForceReenrollResetsRevisedAt|TestRestartThenApplyIsAccepted' -v`.
9. With user claim `email`: a token with neither `email_verified` nor `xms_edov`, or with `false`, `"false"`, `null` or `1`, gets 403; `true` or `"true"` in either claim signs in. With user claim `preferred_username`, a token with no `email_verified` signs in. Check: `go test ./internal/console/... ./internal/handler -run 'TestConsoleMissingEmailVerifiedIs403|TestConsoleUnverifiedEmailIs403|TestEmailVerifiedParsingIsStrict|TestXmsEdovCountsAsVerified|TestNonEmailUserClaimSkipsEmailVerified' -v`.
10. `aw-policy` with no `requireBundle` and a trust file present exits non-zero on a missing, bad or tampered bundle. With no trust file it exits 0. An unreadable trust file fails closed; JSON `null` acts as absent. Explicit `false` exits 0 in both cases, and `true` exits non-zero in both. Check: `go test ./internal/policyhelper ./cmd/aw-policy -run 'TestFailsClosedWhenAKeyIsPinned|TestFailsOpenWhenUnsigned|TestUnreadableTrustFileFailsClosed|TestNullRequireBundleIsAbsent|TestExplicitRequireBundleFalseFailsOpen|TestRequireBundleTrueFailsClosed' -v`.
11. The full suite and the quickstart check pass. Check: `go test ./... && scripts/check-quickstart.sh`.
12. `threat-model.md` "Known issues" lists none of gaps 1–6. Each affected STRIDE row names its new test. The rate limiter and `X-Applied-By` are stated as design limits with workarounds. Check: `grep -c 'needs no token\|email_verified. claim is accepted\|is not checked' docs/site/threat-model.md` prints 0, plus `check: seat panel-security`.
13. `docs/site/upgrading.md` lists the four D8 behavior changes, each with its fix or workaround, and is linked from the docs nav. Check: `grep -c 'http://\|revisedAt\|email_verified\|requireBundle' docs/site/upgrading.md` ≥ 4, plus `check: owner at gate` for wording.
14. No new env var or CLI flag is added. Check: `git diff main --stat` review plus `check: seat panel-security` (it reads the diff for new `os.Getenv`/`fs.*` calls).

## Security notes

- Trust chain after this milestone: an out-of-band token gives a verified key pin; signed bundles give integrity; a monotonic signed revision time gives freshness against network replay. The key-id suffix is public data, and putting it in the token leaks nothing.
- Failure direction: every new check fails closed (it refuses and keeps the old files). The one exception is the explicit `requireBundle: false`.
- The database-restore case is the main operational risk of D4. The recovery (`enroll --force`) goes in the threat model and the `aw-sync` README.

## Status (converged 2026-10-25)

Branch `security-hardening` at 41ab0a1 against `main`. No earlier status table existed.

| # | Criterion (short) | Verdict | Evidence |
|---|---|---|---|
| 1 | Anonymous GET /v1/policy 401, admin 200 | VERIFIED | `go test ./internal/handler -run TestReadingPolicyNeedsAdmin -v`: `--- PASS` |
| 2 | Token ends `.<keyId>`, enrolls once | VERIFIED | `go test ./internal/handler -run 'TestEnrollmentTokenCarriesKeyID\|TestATokenEnrollsOneMachine' -v`: both PASS |
| 3 | enroll refuses on key id mismatch or missing; accepts match and unsuffixed | VERIFIED | AC command plus fix-wave tests `TestEnrollRefusesAKeyThatDoesNotMatchTheTokensKeyID`, `TestEnrollRefusesAnUnparseableKeyWhenTokenNamesOne`: 6 of 6 PASS in `internal/sync`. Earlier panel-correctness fail at 32a14c3 is addressed by a01f1e9. |
| 4 | `--force` applies the same check | VERIFIED | `go test ./cmd/aw-sync -run TestForceReenrollChecksKeyID -v`: PASS |
| 5 | http to non-loopback refused (enroll, run, awd CLI) | VERIFIED | AC command: `TestCheckTransport`, `TestRunRefusesASavedPlainHTTPServer`, `TestEnrollRefusesPlainHTTP`, `TestAdminCLIRefusesPlainHTTP` PASS; sync, aw-sync, awd `ok` |
| 6 | Bundle carries signed `revisedAt`; rollback apply later | VERIFIED | `go test ./internal/handler -run 'TestBundleCarriesRevisedAt\|TestRollbackApplyMovesRevisedAtForward' -v`: both PASS |
| 7 | Pinned `run` refuses lower or missing `revisedAt`, accepts equal or higher, survives rollover | VERIFIED | `go test ./internal/sync -run 'TestRunRefusesAnOlderRevision\|TestRunAcceptsSameOrNewerRevision\|TestRunRefusesMissingRevisedAtOnceStored\|TestRolloverKeepsRevisedAt' -v`: 4 of 4 PASS |
| 8 | `--force` resets value; restart then apply accepted | VERIFIED | `go test ./cmd/aw-sync ./cmd/awd -run 'TestForceReenrollResetsRevisedAt\|TestRestartThenApplyIsAccepted' -v`: both PASS |
| 9 | SSO strict `email_verified` / `xms_edov` | VERIFIED | AC command: all 5 tests PASS (sso and handler `ok`) |
| 10 | `aw-policy` tri-state fail mode | VERIFIED | AC command: all 6 tests PASS (policyhelper, aw-policy `ok`) |
| 11 | Full suite and quickstart check | VERIFIED | `go test ./...` rc 0, no FAIL lines. `scripts/check-quickstart.sh docs/site/quickstart.md`: `ok (10 run, 2 skipped, 24 expected lines found)`; the 2 skips need a logged-in Claude. |
| 12 | threat-model Known issues lists none of gaps 1-6; limits documented | INCONCLUSIVE | Grep prints 0 and the rate limiter and `X-Applied-By` are stated as limits with workarounds. The only seat line, `SEAT panel-security pass 32a14c3 AC12`, predates 91505b2 and a01f1e9, which edited `threat-model.md`. Needs panel-security re-pass after 41ab0a1. |
| 13 | upgrading.md lists four changes, linked from nav; wording | INCONCLUSIVE | `grep -c` prints 7 (>= 4). Linked from `admin-console.md`, `threat-model.md`, `security-model.md`; there is no nav file and `docs/site/index.md` does not link it (see deviations). Wording: `owner checks at merge gate`. |
| 14 | No new env var or CLI flag | INCONCLUSIVE | `git diff main..HEAD -U0 -- '*.go' ':!*_test.go'` filtered for `os.Getenv\|flag.\|fs.<Type>` additions: no matches. The only seat line, `SEAT panel-security pass 32a14c3 AC14`, predates a01f1e9, which touched `cmd/awd/main.go` and `internal/sync/client.go`. Needs panel-security re-pass. |

### Deviations

- AC13 asks for a link from the docs nav. The repo has no nav file. `docs/site/index.md` lists pages by hand and does not link `upgrading.md`. Add a link there or accept the three cross-links.
- The Check line of AC3 lists four tests. The fix wave added two more (named in row 3); they pass.
- AC12 and AC14 seats are stale after the fix wave (a01f1e9, 91505b2).

## Status (converged 2026-10-25), phase 2: AC12-14 after security re-run

Branch `security-hardening` at da6e06e against `main`. Phase 1 above is kept as the record. Code is unchanged since 6fbdd9c (`git diff 6fbdd9c --stat` lists only `docs/superpowers/plans/2026-10-25-security-hardening.md`), so AC1-11 carry their phase 1 rows.

| # | Criterion (short) | Verdict | Evidence |
|---|---|---|---|
| 1-11 | See phase 1 rows 1-11 | VERIFIED (carried) | Phase 1 rows 1-11 above; no code commit since 6fbdd9c. |
| 12 | threat-model Known issues lists none of gaps 1-6; limits documented; seat | VERIFIED | `grep -c 'needs no token\|email_verified. claim is accepted\|is not checked' docs/site/threat-model.md` prints `0`. `SEAT panel-security pass 1569a22 AC12` (plan ledger line 110, da6e06e). Last commit touching `threat-model.md` is 91505b2, which precedes 1569a22. |
| 13 | upgrading.md lists four changes, linked from nav; owner wording | INCONCLUSIVE | `grep -c 'http://\|revisedAt\|email_verified\|requireBundle' docs/site/upgrading.md` prints `7` (>= 4). Nav wiring is by code read only: front matter `section: security-review`, `order: 5`; `web/lib/docs.ts` line 11 defines the section and `web/app/docs/layout.tsx` feeds `navSections(loadDocs())` into `DocsNav`. No browser run of the nav. Owner wording: `OWNER approve AC13 docs/site/upgrading.md wording` (ledger line 108) is not a `SEAT owner pass` line; owner checks at merge gate. |
| 14 | No new env var or CLI flag; seat | VERIFIED | `git diff main..HEAD -U0 -- '*.go' ':!*_test.go' \| grep -E '^\+.*(os\.Getenv\|flag\.\|fs\.(String\|Bool\|Int\|Int64\|Duration\|Var\|Func\|Uint))'` prints nothing (rc 1). Positive control: the same diff has 173 added non-test Go lines. `git diff main --stat` shows 42 files changed. `SEAT panel-security pass 1569a22 AC14` (plan ledger line 111, da6e06e). Last commit touching Go files is a01f1e9, which precedes 1569a22. |

### Deviations (phase 2)

- AC13 nav: the phase 1 deviation is resolved. The nav is built from front matter (`section`, `order`), so `upgrading.md` appears in the Security review section without a hand-kept index link. The nav check is still code-read only, so AC13 stays INCONCLUSIVE.
- The phase 1 deviation "AC12 and AC14 seats are stale" is resolved by the 1569a22 seat lines. The phase 1 text is kept as the record.
- Panel-security 1569a22 raised 3 nits, and the ledger sends them to backlog. No must or should finding is open.
