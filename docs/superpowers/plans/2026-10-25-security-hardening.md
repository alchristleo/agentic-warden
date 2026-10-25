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
- Files: `docs/site/threat-model.md` (Known issues → only design limits left: no rate limiter, `X-Applied-By` label, with workarounds; STRIDE rows updated with new test names; same-revision group replay and clock-backwards residuals), new `docs/site/upgrading.md` (four behavior changes, each with fix: use https / re-apply policy / add Entra optional claim `email_verified` + admin-token break-glass / `requireBundle: false`), docs nav entry wherever the `/docs` site lists pages, `deploy/aw-sync/README.md` (https rule, `enroll --force` recovery), admin-console doc (Entra claim). Check `docs/site/security-model.md` for statements now false.
- Constraint: plain short sentences, match existing doc voice; `scripts/check-quickstart.sh` and docs CI must stay green.
- Agent: sdd-implementer-lite. Depends-on: T1–T6.

## Ledger

USAGE start 61

## Backlog

- Built-in rate limiter (needs X-Forwarded-For trust).
- Per-admin tokens / verified attribution on the token path.
- Co-signed policy revisions (policy write = fleet code exec).
- Anti-RE integrity package → paid-skills milestone (short-lived signed skill bundles, per-buyer watermark).
- Native TLS in awd.
- Same-revision group-membership replay detection.
