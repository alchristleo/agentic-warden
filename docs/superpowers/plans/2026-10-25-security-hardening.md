# Security hardening — plan

Spec: `docs/superpowers/specs/2026-10-25-security-hardening-design.md` (read it first; decisions D1–D8, ACs 1–14).
Branch: `security-hardening`. Constraint for every task: no new env var or CLI flag (AC14); every new check fails closed; match existing test naming (`TestXxx` sentences).

## Parallel group 1 (no shared files): T1, T2, T3, T5, T6

### T1 — Admin-only policy preview (D1, AC1)
- Files: `internal/handler/handler.go`, `internal/handler/handler_test.go`, `cmd/awd/e2e_test.go` (update callers of `GET /v1/policy` to send the admin token), comment in `internal/handler/bundle.go:20` if it now misstates.
- Interface: `mux.HandleFunc("GET /v1/policy", h.requireAdmin(h.getPolicy))`.
- Tests: `TestReadingPolicyNeedsAdmin` (anonymous → 401; admin token → 200; console admin session → 200) replaces `TestReadingPolicyNeedsNoToken`.
- Agent: sdd-implementer. Depends-on: none.

### T2 — Key id in enrollment token (D2, AC2–4)
- Files: `internal/handler/enroll.go` (+ test), `internal/sync/client.go` `Enroll` (+ test), `cmd/aw-sync` enroll tests.
- Server: when `h.Signer != nil`, token = `plain + "." + h.Signer.KeyID()`; hash the full string.
- Client: split on last `.`; if suffix present, response `keyId` must equal it, else error before any state is written. No suffix → today's behavior.
- Tests: `TestEnrollmentTokenCarriesKeyID`, `TestATokenEnrollsOneMachine` (still passes), `TestEnrollRefusesAKeyIDMismatch`, `TestEnrollRefusesAMissingKeyWhenTokenNamesOne`, `TestEnrollAcceptsAMatchingKeyID`, `TestEnrollWithoutKeyIDSuffixStillPins`, `TestForceReenrollChecksKeyID` (existing enrollment + `--force` + mismatching token → refused, old state intact).
- Agent: sdd-implementer. Depends-on: none.

### T3 — Refuse plain HTTP to non-loopback (D3, AC5)
- Files: new `internal/sync/transport.go` (+ `transport_test.go`), call sites in `cmd/aw-sync/main.go` (enroll, and run on the saved server) and `cmd/awd/main.go` admin CLI subcommands that send `AWD_ADMIN_TOKEN` (+ tests).
- Interface: `func CheckTransport(rawURL string) error` — nil for `https`, nil for `http` when host is `localhost` (case-insensitive) or `net.ParseIP(host).IsLoopback()`; error otherwise naming the URL and saying to use https. No DNS.
- Tests: `TestCheckTransport` (table: https remote ok; http localhost/LOCALHOST/127.0.0.1/127.5.5.5/[::1] ok; http remote, `localhost.evil.com`, `user@evil.com`, `localhost@evil.com` refused), `TestEnrollRefusesPlainHTTP` (no request reaches a test server), `TestRunRefusesASavedPlainHTTPServer` (files untouched), `TestAdminCLIRefusesPlainHTTP` (no request sent).
- Constraint: existing tests using `httptest.NewServer` (127.0.0.1) keep passing.
- Agent: sdd-implementer. Depends-on: none.

### T5 — Strict email_verified (D5, AC9)
- Files: `internal/console/sso/sso.go` (+ tests), `internal/console/oidctest` if a helper to omit/set the claim is needed.
- Rule: verified only if claim is JSON `true` or string `"true"` (case-insensitive); missing/other → 403 path.
- Tests: `TestConsoleMissingEmailVerifiedIs403`, `TestConsoleUnverifiedEmailIs403` (still passes), `TestEmailVerifiedParsingIsStrict` (table: true, "true", "TRUE" ok; false, "false", null, 1, "yes", missing refused).
- Agent: sdd-implementer. Depends-on: none.

### T6 — Fail closed once a key is pinned (D6, AC10)
- Files: `internal/policyhelper/helper.go` (+ test), `cmd/aw-policy/main.go` (+ test).
- Interface: `Config.RequireBundle *bool`; `exitCode(cfg, source, pinned bool)` where pinned = trust file present or present-but-unreadable. nil → pinned decides; true → closed; false → open. JSON null → nil.
- Tests: `TestFailsClosedWhenAKeyIsPinned` (missing, bad, tampered bundle each non-zero), `TestFailsOpenWhenUnsigned`, `TestUnreadableTrustFileFailsClosed`, `TestNullRequireBundleIsAbsent`, `TestExplicitRequireBundleFalseFailsOpen`, `TestRequireBundleTrueFailsClosed`. Output is still a valid envelope on stdout even when exit is non-zero (existing contract).
- Agent: sdd-implementer. Depends-on: none.

## Group 2

### T4 — Signed revision time, refuse older (D4, AC6–8)
- Files: `internal/policy/bundle.go` (`RevisedAt int64 json:"revisedAt,omitempty"`), resolver in `internal/handler` that builds the bundle (fill from `Revision.CreatedAt.UnixMilli()`), `internal/sync/files.go` (`State.RevisedAt`), sync run cycle (`internal/sync/sync.go`), `cmd/aw-sync/main.go` (`--force` resets), tests in each plus `cmd/awd` e2e.
- Rule: only when a key is pinned: incoming `RevisedAt < State.RevisedAt` → error with D4 text, no files written, state's stored value unchanged; else render and store max. Rollover keeps the value.
- Tests: `TestBundleCarriesRevisedAt`, `TestRollbackApplyMovesRevisedAtForward`, `TestRunRefusesAnOlderRevision`, `TestRunAcceptsSameOrNewerRevision`, `TestRunRefusesMissingRevisedAtOnceStored`, `TestRolloverKeepsRevisedAt`, `TestForceReenrollResetsRevisedAt`, `TestRestartThenApplyIsAccepted` (memory-store awd: enroll, apply, sync; restart awd, apply again, sync → accepted).
- Constraint: use the real bundle JSON the handler produces as the sync-side fixture (shared contract). Confirm old aw-sync decoders tolerate the new field (no `DisallowUnknownFields` on the bundle) — state result in report.
- Agent: sdd-implementer. Depends-on: T2, T3 (share `cmd/aw-sync/main.go` and `internal/sync`).

## Group 3

### T7 — Docs (D7, D8, AC12–13)
- Files: `docs/site/threat-model.md` (Known issues → only design limits left: no rate limiter, `X-Applied-By` label, with workarounds; STRIDE rows updated with new test names; same-revision group replay and clock-backwards residuals), new `docs/site/upgrading.md` (four behavior changes, each with fix: use https / re-apply policy / add Entra optional claim `email_verified` + admin-token break-glass / `requireBundle: false`), docs nav entry wherever the `/docs` site lists pages, `deploy/aw-sync/README.md` (https rule, `enroll --force` recovery), admin-console doc (Entra claim), `docs/hosting/runbook.md:143` (curl `/v1/policy` needs `Authorization: Bearer $AWD_ADMIN_TOKEN`), `README.md:89,91` (drop "needs no token"; requireBundle default), `deploy/managed-settings/README.md:28,73-76` (new fail-closed default), `internal/sync/client.go` key-id mismatch error adds "mint a new token" (T2 review nit), `internal/agent/claude/inspect.go:188-196` doctor text conditional on trust file present ("refuses to start: a key is pinned") with a test (T6 review nit). Check `docs/site/security-model.md` for statements now false.
- Constraint: plain short sentences, match existing doc voice; `scripts/check-quickstart.sh` and docs CI must stay green.
- Agent: sdd-implementer (re-tiered: inspect.go change is code). Depends-on: T1–T6.

## Backlog

- Built-in rate limiter (needs X-Forwarded-For trust).
- Per-admin tokens / verified attribution on the token path.
- Co-signed policy revisions (policy write = fleet code exec).
- Anti-RE integrity package → paid-skills milestone (short-lived signed skill bundles, per-buyer watermark).
- Native TLS in awd.
- Same-revision group-membership replay detection.
- Repin before the revisedAt check, so a refused bundle carrying a rollover still moves the pin (panel-correctness nit; documented meanwhile).
- Sync `Client.CheckRedirect` skipped when `cfg.HTTP` is injected (tests only today; no production caller sets it).
- Doctor `emptyEffect` should read explicit `requireBundle` (T7 review nit).

## Ledger

USAGE start 61
T5 done c55226f sdd-implementer strict email_verified; replaced TestMissingEmailVerifiedIsAccepted
T1 done 83034ea sdd-implementer policy preview admin-only; +console_test case
T2 done 66634e4 sdd-implementer key id in enroll token; mismatch spends token
T6 done 49c1d9c sdd-implementer tri-state requireBundle; follow-up: inspect.go:188 doctor says 'emits no settings' but pinned now fails closed
T3 done 108a0ca sdd-implementer CheckTransport; run re-check in internal/sync/sync.go Run
T2 review approve 66634e4 sdd-reviewer nit: mismatch error could say re-mint token (backlog/T7 docs)
T5 review approve c55226f sdd-reviewer 5 mutations caught
T1 review approve 83034ea sdd-reviewer should: runbook:143 + README:91 → T7; nit bundle.go:20 rewrap
T6 review approve 49c1d9c sdd-reviewer should: managed-settings README + README:89 → T7; inspect.go → T7 (T7 re-tiered to sdd-implementer)
T3 review approve 108a0ca sdd-reviewer nits: redact URL userinfo in error (coord), groups case optional
MERGE group1 ffd6daa go test ./... all ok
T3 coord 0b91724 coordinator redact userinfo in CheckTransport error (+test)
T4 done 3d445ec sdd-implementer revisedAt; old aw-policy/aw-sync tolerate field (plain Unmarshal; strict only on aw-policy config, checked by coordinator)
T4 review approve 3d445ec sdd-reviewer should: no unpinned-skip test; nits: snapshot all files, stale comment → fix round 1
T4 fix 2f1ac86 sdd-implementer unpinned-skip test + full snapshot + comment
T4 review approve 2f1ac86 sdd-reviewer-lite fix round 1 pass
T7 done da93f26 sdd-implementer docs+upgrading.md+inspect.go; also edited security-model, how-it-works, enforce-plain-claude; CONCERN Entra has no email_verified (uses xms_edov) — owner question
T5b done bd00d7a sdd-implementer email check only for user claim email; xms_edov accepted (owner decision)
T7+T5b review approve 551977e sdd-reviewer nits: doctor ignores explicit requireBundle; xms_edov-with-email source (in MS ref); threat-model path
PANEL design skipped: 144 non-test Go lines (<300)
PANEL security 32a14c3 must=1 should=1 nit=1
SEAT panel-security pass 32a14c3 AC12
SEAT panel-security pass 32a14c3 AC14
PANEL correctness 32a14c3 must=1 should=1 nit=2
SEAT panel-correctness fail 32a14c3 AC3
SEAT panel-correctness fail 32a14c3 AC4
TRIAGE fixed=4 dismissed=0 backlog=1
FIX wave a01f1e9 sdd-implementer(sonnet) key-id from real key; CheckRedirect on 3 clients; revoke hint; threat-model path + rollover note
FIX review fail→coord 91505b2 sdd-reviewer-lite threat-model:52 rollover wording fixed by coordinator; backlog CheckRedirect with injected client
COORD review approve 91505b2 sdd-reviewer-lite(haiku) 0b91724 + threat-model wording
CONVERGE 6fbdd9c spec-converge: AC1-11 VERIFIED; AC12/AC14 INCONCLUSIVE (security seat stale after a01f1e9); AC13 nav = front matter section/order (docs-nav.tsx), wording owner
PAUSED at step 7 seat re-run, next: panel-security re-run (Opus, fix diff only: review-pkg.sh 32a14c3; brief lists dismissed/triaged items + owns AC12/AC14) → spec-converge update AC12/14 → owner checks docs/site/upgrading.md wording (AC13) → merge gate (finishing-a-development-branch, PR, pr-checks-wait.sh) → retro (incl. asd-ste100 wiring proposal)
USAGE end 93
