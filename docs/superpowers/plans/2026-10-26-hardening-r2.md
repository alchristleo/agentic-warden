# Hardening round 2: plan

Spec: `docs/superpowers/specs/2026-10-26-hardening-r2-design.md`. Branch `hardening-r2`.

Shared contracts:

```go
// internal/handler (T3): wraps any handler; clock injectable.
func NewLimiter(trusted []netip.Prefix, now func() time.Time, log *slog.Logger) *Limiter
func (l *Limiter) Wrap(next http.Handler) http.Handler
// internal/handler (T4): name -> credential.Hash(token). Replaces Handler.AdminToken.
type Handler struct { AdminTokens map[string]string; ... }
// internal/config (T2)
func ParseAdminTokens(r io.Reader) (map[string]string, error) // name -> sha256 hex
// internal/sync (T6)
func CheckRedirect(req *http.Request, via []*http.Request) error
```

Parallel group A: T1, T2, T3, T4, T6, T7 (no shared files). T5 after T2, T3, T4, T6. T8 after T5.

## T1 SVG parse check in CI (sdd-implementer-lite, depends-on: none)

- Files: `scripts/check-svg.sh` (new), `scripts/test-checks.sh`, `.github/workflows/docs.yml`.
- `check-svg.sh [dir]`: parse every `git ls-files '*.svg'` (under dir, default repo root) with `python3 -I -c` xml parse; on failure print `check-svg: <file>: <error>` and exit 1.
- Tests in `test-checks.sh`: passes on the tree; a temp copy of `docs/assets/architecture-light.svg` with one `</text>` turned into `</tex>` fails and names the file. AC12.
- `docs.yml`: step `scripts/check-svg.sh` beside `check-readme.sh`.

## T2 Config: TLS, trusted proxies, admin tokens file (sdd-implementer, depends-on: none)

- Files: `internal/config/config.go`, `internal/config/admintokens.go` (new), tests.
- `Config` gains `TLSCertFile`, `TLSKeyFile`, `TrustedProxies []netip.Prefix`, `AdminTokens map[string]string` (name to hash, includes `admin` from `AWD_ADMIN_TOKEN` hashed with `credential.Hash`). Keep `AdminToken` only if other callers need it; else remove.
- `FromEnv` errors: one TLS var set (names the missing one); bad CIDR in `AWD_TRUSTED_PROXIES`; tokens file unreadable; `ParseAdminTokens` errors: bad name (`^[a-z0-9][a-z0-9._@-]{0,63}$`), hash not 64 lower hex, duplicate name; `admin` in file alongside `AWD_ADMIN_TOKEN`. Errors carry `line N`, never the line's text.
- Tests: `TestAdminTokensFile*` (each error case asserts line number present and hash/token text absent; comments and blanks ok) AC7; `TestFromEnvTLSPair`, `TestFromEnvTrustedProxies`.

## T3 Failed-auth limiter (sdd-implementer, depends-on: none)

- Files: `internal/handler/limit.go`, `internal/handler/limit_test.go` (new). Do not touch `handler.go` or `Routes()`; T5 wires it.
- Behavior per spec D2: bucket burst 10, refill 1 per 6 s; counted = any 401 except `GET /v1/bundle`, plus 404/409 from `POST /v1/machines/enroll`; empty bucket = 429 + `Retry-After` + `{"error":"too many failed attempts"}` before `next` runs, except `/healthz` `/readyz`; key = RemoteAddr IP (IPv6 /64), XFF walk only when RemoteAddr in trusted (join header lines, accept `ip`, `ip:port`, `[v6]:port`); one warn log on first XFF seen with no trusted prefixes; cap 10 000 keys, evict full buckets first then oldest.
- Tests (fake clock, fake inner handler returning set statuses on real route paths): `TestLimiterBlocksAfterTenFailures` (11th with a 200-inner gets 429, inner not called, Retry-After set), `TestLimiterRefills`, `TestLimiterHealthExempt`, `TestLimiterOtherIPUnaffected` AC2; `TestLimiterCountsAdmin401`, `TestLimiterCountsSCIM401`, `TestLimiterCountsEnroll404And409`, `TestLimiterSkipsBundle401` AC3; `TestLimiterKey*` (untrusted XFF ignored, trusted rightmost-untrusted, two header lines, ip:port, forged left entry ignored, IPv6 /64 shared, warn logged once) AC4; `TestLimiterBounded` (20 000 IPs, size ≤ 10 000) AC5.
- For each negative assertion, break the code once and show it fails.

## T4 Per-admin tokens and verified actor (sdd-implementer, depends-on: none)

- Files: `internal/handler/auth.go`, `handler.go` (field only), `groups.go`, tests in `internal/handler`.
- `Handler.AdminTokens map[string]string` replaces `AdminToken`. Empty map = 503 as today. `tokenAdmin`: hash presented, constant-time compare against every entry with no early exit, actor `token:<name>`. Ignore `X-Applied-By` everywhere server-side; groups snapshot `appliedBy` = verified actor. Fix existing tests that set `AdminToken`.
- Tests: `TestAdminTokensBothNamesPass`, `TestAdminTokensActorIgnoresAppliedBy` (header `mallory`, audit actor `token:alice`), `TestAdminTokensRemovedNameFails`, `TestAdminTokensSingleLegacyIsAdmin`, `TestGroupsAppliedByIsVerifiedActor` AC6.

## T5 awd wiring: TLS, admin-token command, limiter, CLI header (sdd-implementer, depends-on: T2, T3, T4, T6)

- Files: `cmd/awd/main.go`, tests in `cmd/awd`; `internal/handler/handler.go` only if wiring needs it.
- `serve`: load TLS pair before `net.Listen`; `ServeTLS` with MinVersion TLS 1.2; wrap `h.Routes()` with `NewLimiter(cfg.TrustedProxies, time.Now, log).Wrap`; set `h.AdminTokens = cfg.AdminTokens`.
- `awd admin-token <name>`: validate name like the file; print token (`credential.New`) line 1, `<name> <hash>` line 2; no config read. Help text lists it.
- CLI clients (`:396`, `:508` today): use `sync.CheckRedirect`; stop sending `X-Applied-By`; drop `appliedBy()` if unused.
- Tests: `TestServeTLS*` (HTTPS works, TLS 1.1 client refused, one var set errors naming it, bad pair binds nothing) AC1; `TestAdminTokenCommand` (line 2 hash = `credential.Hash(line 1)`; round trip through `ParseAdminTokens` admits the token via a served request) AC8; `TestRedirectCap` for both CLI clients (10th redirect refused) AC9; an e2e that 11 bad admin tokens to a served awd gets 429.

## T6 sync: redirect helper and repin order (sdd-implementer, depends-on: none)

- Files: `internal/sync/client.go`, `internal/sync/transport.go` (or where `CheckTransport` lives), `internal/sync/sync.go`, tests.
- `CheckRedirect`: `len(via) >= 10` error, then `CheckTransport(req.URL.String())`. Client uses it; injected `cfg.HTTP` with nil `CheckRedirect` used via value copy with it set, caller's client untouched.
- Repin before the `revisedAt` refusal when `fetched.Rollover != nil` (rollover already verified in `Fetch` against the pin); the refusal still returns an error and writes no files.
- Tests: `TestRedirectCap` (10 hops refused, 9 ok), `TestInjectedClientChecksRedirect` (redirect to plain http refused; caller's client CheckRedirect still nil) AC9; `TestRefusedOlderBundleStillRepins` (pin file moved, files unchanged, error returned) AC11.

## T7 doctor: helper ownership and requireBundle (sdd-implementer, depends-on: none)

- Files: `internal/agent/claude/inspect.go`, tests.
- Finding (Warn) on linux/darwin when `os.Stat(helperPath)` or `os.Stat(filepath.Dir(helperPath))` has uid ≠ 0 or `mode&0o022 != 0`. Message names path, uid, mode and the fix `sudo chown root:root <path> && sudo chmod 755 <path>` (copy-pasteable). Missing helper: existing behavior. Stat via a package func value so tests fake uid/mode. Windows: skip.
- `emptyEffect`: read `aw-policy` config's explicit `requireBundle` first (true = fail closed, false = no settings), then the trust-file guess. Find where aw-policy reads its config (`cmd/aw-policy/main.go:160-175`) and reuse the path/parse, don't copy.
- Tests: `TestDoctorHelperOwnership*` (uid 1000 warns, 0o775 warns, dir 0o757 warns, root 0o755 clean) AC10; `TestEmptyEffectReadsRequireBundle` (true with no trust file = fail closed; false with trust file = no settings) AC11.

## T8 Docs and hosted cell (sdd-implementer, depends-on: T5)

- Files: `docs/site/threat-model.md`, `docs/site/upgrading.md`, `docs/site/reference.md`, self-host docs that list `AWD_ADMIN_TOKEN`, `deploy/aws/cell/main.tf` + `tests/cell.tftest.hcl`, `docs/superpowers/specs/2026-10-25-security-hardening-design.md` (D3/AC3 text only).
- threat-model: TLS row (native TLS option, `SSL_CERT_FILE` for self-signed), limiter replaces "put a limiter in front" (lines 19, 21, 39), per-admin attribution, NAT lockout note, rows :43/:49 test names per spec D7c. upgrading: audit actor change (D4). reference: `AWD_TLS_CERT_FILE`, `AWD_TLS_KEY_FILE`, `AWD_TRUSTED_PROXIES`, `AWD_ADMIN_TOKENS_FILE`, `awd admin-token`. Every factual claim cites code or test.
- Cell: `AWD_TRUSTED_PROXIES` = VPC CIDR in the task env; tftest asserts it. Run `tofu test` in `deploy/aws/cell` if tofu is present; else say so.
- AC14 (seat panel-security).

## Backlog

- Co-signed policy revisions (two-person rule).
- Same-revision group-membership replay detection.
- TLS certificate hot reload.
- Admin tokens in the database with create/revoke API.
- Custom CA option for `aw-sync` / `awd` CLI.
- One exported admin-tokens parse function taking the env hash; config exports the name-rule message for `awd admin-token` (panel-design nits).
- From adapter cleanup: known-agent allowlist for rule keys; merged-document validation per agent; skip-with-note for foreign agy admin file; back-off for always-failing render; prune parent-dir ownership check.

## Ledger

PLAN 316fbc6 spec approved (owner 2026-10-26: TLS + limiter + per-admin tokens; per-IP opt-in proxy; tokens file; skip machine 401s)
USAGE start 52
T1 done 212278f sdd-implementer-lite check-svg.sh + test-checks cases + docs.yml step
T1 review approve 212278f sdd-reviewer-lite none
T7 done b81016d sdd-implementer helper ownership + emptyEffect; deviation: aw-policy is main pkg, one-key parse repeated in inspect.go
T6 done 90600dc sdd-implementer CheckRedirect in transport.go, injected client copy, repin before revisedAt; caller CheckRedirect kept as is
T2 done b114724 sdd-implementer config TLS/proxies/tokens; Config.AdminToken kept for main; trailing # comment accepted
T7 review approve b81016d sdd-reviewer nits: cross-ref comment in aw-policy main.go; no sticky-dir test
T2 review approve b114724 sdd-reviewer nits: trailing # comment undocumented; T5 must drop Config.AdminToken
T4 done 42d7bd5 sdd-implementer AdminTokens + verified actor; main.go minimal map; internal/sync tests touched (T6 overlap)
T6 review approve 90600dc sdd-reviewer nits: CLI clients to T5; caller-own CheckRedirect branch untested
T4 review approve 42d7bd5 sdd-reviewer nits to T5: reject duplicate token hash in config; drop CLI X-Applied-By
T3 done b18c88d sdd-implementer limiter 17 tests, mutation-checked; eviction O(n) scan at cap
T3 fix b18c88d sdd-reviewer fix-needed O(n) eviction under lock, eviction order untested, wait/fail overshoot, no concurrency test
MERGE group A a702cd4 T1,T2,T4,T6,T7 clean; go vet+test green; T3 in fix round 1
T3 fix c3a72b5 sdd-reviewer-lite fix-needed must stale-spec (owner kept plain LRU, resolved); should no test blocked-no-reserve, refund cap; nit refund identity
COORD owner approved spec D2 plain LRU eviction 2026-10-26
T3 fix f682e9d sdd-reviewer-lite fix-needed should refund test misses burst cap (mutation survives); nits double now(), comment wrap
COORD owner approved coord fix past T3 cap
T3 coord 4f80a12 TestLimiterRefillCapsAtBurst (mutation: cap removed -> fails); one now() in refund; comment rewrap
T3 done 4f80a12 sdd-reviewer-lite coord re-review approve
COORD review approve 4f80a12 sdd-reviewer-lite refill cap test mutation-checked
MERGE T3 cca7143 clean; go vet+test green
T5 done f8d383d sdd-implementer TLS, admin-token, limiter wiring, CLI redirect, dup-hash reject; exported config.ValidAdminName; appliedBy removed
T5 fix f8d383d sdd-reviewer fix-needed must limiter outside Logging/Recovery (429 not logged); should no served /healthz bypass check; nits serveFailure comment
T5 done c80f2d5 sdd-reviewer-lite re-review approve
T8 done 0a8ebe9 sdd-implementer docs + cell AWD_TRUSTED_PROXIES via data aws_vpc (new ec2:DescribeVpcs); tofu test 28 pass; AWD_APPLIED_BY docs dropped
T8 review approve 0a8ebe9 sdd-reviewer nits: data aws_vpc ok; reference 11th attempt
T8 coord 183c888 reference: 11th attempt not failure
COORD fix-wave re-review 0023547 sdd-reviewer-lite fail: must 183c888 sed missed (backticked 429), reference.md unchanged; edit deferred to panel fix wave (AC14 seat)
PANEL design 0023547 must=1 should=5 nit=4
PANEL correctness 0023547 must=1 should=2 nit=3
COORD usage 89 budget mode: no design re-run, no Opus escalation
PANEL security 0023547 must=1 should=1 nit=2
SEAT panel-security pass 0023547 AC14 docs match D9; reference 11th-attempt edit still pending
DISMISSED design should LRU->arbitrary eviction: owner chose plain LRU, both O(1)
DISMISSED design should Limit hook->*Limiter: preference, no failure
DISMISSED design nit statPath global: seat rated acceptable
TRIAGE fixed=15 dismissed=3 backlog=2
