# Hardening round 2: design

## Goal

Close the gaps a self-hosting admin meets first. `awd` serves TLS itself, slows repeated failed authentication, and names the admin behind each change from the token, not from a client header. Also: a redirect cap on every client, a doctor check that `aw-policy` is root-owned, an SVG parse check in CI, and the security-hardening backlog nits.

## Non-goals

- Co-signed policy revisions (two-person rule). Backlog; needs per-admin identity first, which this milestone adds.
- Same-revision group-membership replay detection. Backlog; needs a writer on the state directory or the control plane host.
- TLS certificate hot reload. Restart `awd` to rotate. Backlog.
- Admin tokens stored in the database, with create/revoke over the API. Backlog. Revoke = delete the line, restart.
- Rate limiting successful requests. Only failed authentication counts.
- A custom CA option in `aw-sync` or the `awd` CLI. A self-signed `awd` certificate works through Go's `SSL_CERT_FILE`/`SSL_CERT_DIR` on Linux; docs say so.
- A permission warning on the tokens file. It holds hashes only.

## Decisions

- **D1 Native TLS.** `AWD_TLS_CERT_FILE` and `AWD_TLS_KEY_FILE`, both or neither. Both set: `awd serve` loads the pair before listening (bad pair = startup error, nothing bound) and serves HTTPS, minimum TLS 1.2. One set: startup error naming the missing one. Neither: plain HTTP as today. Order: `tls.LoadX509KeyPair` runs before `net.Listen`, then `srv.ServeTLS(listener, "", "")` with `TLSConfig.MinVersion = tls.VersionTLS12` (Go's server default already, kept as a guard); HTTP/2 stays on. The console cookie and `AWD_PUBLIC_URL` are unchanged (`AWD_PUBLIC_URL` is already https). Shapes considered: (a) env file pair (chosen, matches every other `AWD_*` setting); (b) autocert/ACME (needs a public port 80/443 and a cache dir; Backlog).
- **D2 Failed-auth limiter** (owner: per-IP, opt-in proxy header). Middleware around the whole mux. Each client key has a bucket: burst 10 failures, refill 1 per 6 s. Constants, not knobs. Counted failures (owner 2026-10-26: skip machine 401s): 401 from the admin token path and the console, 401 from SCIM, and 404 or 409 from `POST /v1/machines/enroll` (`postEnroll` answers an unknown token 404 and a used or expired one 409, `internal/handler/handler.go:213-216`; wire codes unchanged). Machine credential 401s (`GET /v1/bundle`) do not count: revoked machines poll forever, and a 256-bit credential cannot be guessed. A key with an empty bucket gets `429`, `Retry-After: <seconds>`, body `{"error":"too many failed attempts"}` on every route except `/healthz` and `/readyz`, and the handler does not run. Client key: the `RemoteAddr` IP; IPv6 keys are the /64. If `AWD_TRUSTED_PROXIES` (comma-separated CIDRs) contains `RemoteAddr`, the key is the rightmost `X-Forwarded-For` entry not inside a trusted CIDR; no such entry or a malformed one = `RemoteAddr`. Several `X-Forwarded-For` header lines are joined in order before the walk; entries may be `ip`, `ip:port` or `[ipv6]:port`. Entries left of the rightmost untrusted one are client-written and never read, so a forged trusted-looking entry gains nothing. With `AWD_TRUSTED_PROXIES` unset, the first request that carries `X-Forwarded-For` logs one warning: every client behind that proxy shares one bucket. Without `AWD_TRUSTED_PROXIES`, `X-Forwarded-For` is ignored. Bad CIDR = startup error. `aw-sync` treats 429 like any other server error: it keeps its files and tries again on the next timer cycle, with no immediate retry. Memory: at most 10 000 keys; at the cap, keys whose bucket is full are dropped first, then the oldest. Shapes considered: (a) per-IP with opt-in trusted proxies (chosen); (b) one global failed-auth bucket (a flood locks out real admins).
- **D3 Per-admin tokens** (owner: file of name and hash). `AWD_ADMIN_TOKENS_FILE`: one `<name> <sha256-hex>` per line, `#` comments and blank lines allowed. Name matches `^[a-z0-9][a-z0-9._@-]{0,63}$`, unique. Any bad line = startup error with the line number and no token text. `AWD_ADMIN_TOKEN` still works and is the name `admin`; a file entry named `admin` alongside it = startup error. The presented token is hashed (`credential.Hash`) and compared in constant time against every entry, no early exit. `AWD_ADMIN_TOKEN` is hashed at startup and goes into the same list. Neither set = admin routes answer 503, as today. New subcommand `awd admin-token <name>` prints the token (stdout line 1) and the file line to append (line 2); it reads no config and touches no file. Shapes considered: (a) file (chosen); (b) database + API (Backlog).
- **D4 Verified actor.** Token path actor is `token:<name>`. The server no longer reads `X-Applied-By`; the groups snapshot `appliedBy` is the same verified actor. Console sessions are unchanged, and a request with no `Authorization` header still goes to the console path. The server ignores the header, so old `awd` CLIs that still send it keep working. The `awd` CLI stops sending the header. Upgrade note: audit rows for tokens now read `token:admin` (single-token setups) instead of the claimed name.
- **D5 Redirect cap.** One helper `sync.CheckRedirect(req, via)`: refuse when `len(via) >= 10`, then `CheckTransport`. Used by `internal/sync/client.go` and both `awd` CLI clients. `len(via) >= 10` restores Go's default cap, which a custom `CheckRedirect` replaces. An injected `cfg.HTTP` with no `CheckRedirect` is used through a value copy with the helper set (`Transport`, `Jar` and `Timeout` kept); the caller's client is not mutated.
- **D6 Doctor checks the helper.** On Linux and macOS, `aw doctor` warns when `aw-policy` (at `helperPath`) or its directory is not owned by uid 0, or is group- or world-writable. It checks the file the path resolves to (`os.Stat`), and the directory of the path as written. Windows: no check. The finding names the path and owner and gives the `chown`/`chmod` fix. Missing helper: existing behavior.
- **D7 Backlog nits folded in.** (a) Repin before the `revisedAt` check, so a refused older bundle that carries a rollover still moves the pin. `Fetch` already verifies the rollover against the current pin (`internal/sync/client.go:250`), so a replayed bundle cannot move the pin to a key the current pin did not sign. (b) `emptyEffect` reads `aw-policy`'s explicit `requireBundle` before the trust-file guess. (c) threat-model rows :43/:49 name `TestEnrollRefusesAKeyThatDoesNotMatchTheTokensKeyID`, `TestRedirectToPlainHTTPIsRefused`, `TestAdminCLIRefusesRedirectToPlainHTTP`. (d) security-hardening spec D3/AC3 text says the presented public key must hash to the token's key id.
- **D8 SVG check (retro escape, adapter cleanup).** `scripts/check-svg.sh` parses every git-tracked `*.svg` as XML and fails naming the file. Runs in `docs.yml`.
- **D9 Docs.** threat-model: TLS row and "Known issues" reflect D1 to D4 (limiter replaces "put a limiter in front"; per-admin attribution). `docs/site/upgrading.md`: D4 audit-actor change. Self-host docs: the four new env vars and `awd admin-token`. Hosted cell sets `AWD_TRUSTED_PROXIES` to the VPC CIDR.

## Design

- `internal/config`: `TLSCertFile`, `TLSKeyFile`, `TrustedProxies []netip.Prefix`, `AdminTokensFile`; validation errors as in D1 to D3.
- `internal/handler/limit.go`: `Limiter` middleware (D2), clock injected for tests. Wired in `Routes()` outside `Logging`/`Recovery`'s handler so a 429 is still logged.
- `internal/handler/auth.go`: `AdminTokens` (name to hash) replaces the single `AdminToken`; `tokenAdmin` sets actor `token:<name>`.
- `cmd/awd/main.go`: TLS serve (D1), `admin-token` subcommand (D3), drop `X-Applied-By`, use `sync.CheckRedirect`.
- `internal/sync`: `CheckRedirect` (D5), repin order (D7a).
- `internal/agent/claude/inspect.go`: helper ownership finding (D6), `emptyEffect` (D7b). Stat goes through a function value so tests fake uid and mode.

## Acceptance criteria

1. With both TLS files set, `awd serve` answers HTTPS and refuses a TLS 1.1 handshake; one file set fails startup naming the missing variable; a bad pair binds nothing. `go test ./cmd/awd -run 'TestServeTLS' -v`
2. 10 failed admin-token requests from one IP then the 11th, even with the right token, gets 429 with `Retry-After`; after 6 s (fake clock) one more attempt is allowed; `/healthz` stays 200; another IP is unaffected. `go test ./internal/handler -run 'TestLimiter' -v`
3. A failed enrollment token, machine credential and SCIM token each take a token from the bucket (enroll: its 404 and 409); a revoked machine credential's 401 does not. `go test ./internal/handler -run 'TestLimiterCounts' -v`
4. `X-Forwarded-For` is ignored unless `RemoteAddr` is in `AWD_TRUSTED_PROXIES`; with it, the rightmost untrusted entry is the key, also across two header lines and with `ip:port` entries; a forged entry left of it is ignored; two IPv6 addresses in one /64 share a bucket. `go test ./internal/handler -run 'TestLimiterKey' -v`
5. The limiter holds at most 10 000 keys under 20 000 distinct failing IPs. `go test ./internal/handler -run 'TestLimiterBounded' -v`
6. Two named tokens from the file both pass; the audit actor is `token:<name>` even when `X-Applied-By: mallory` is sent; a revoked (removed) name fails 401; `AWD_ADMIN_TOKEN` alone audits as `token:admin`. `go test ./internal/handler -run 'TestAdminTokens' -v`
7. A tokens file with a bad name, bad hash, duplicate name, or `admin` next to `AWD_ADMIN_TOKEN` fails startup with the line number and without the token text. `go test ./internal/config -run 'TestAdminTokensFile' -v`
8. `awd admin-token alice` prints a token and a line `alice <hash>` where the hash is `credential.Hash(token)`; that line in a tokens file admits that token. `go test ./cmd/awd -run 'TestAdminTokenCommand' -v`
9. The sync client and both `awd` CLI clients refuse the 10th redirect, and an injected HTTP client still refuses a redirect to plain HTTP. `go test ./internal/sync ./cmd/awd -run 'TestRedirectCap|TestInjectedClientChecksRedirect' -v`
10. `aw doctor` warns for a helper or helper directory owned by a non-root uid or with mode 0o775/0o757, and does not warn for root:0o755. `go test ./internal/agent/claude -run 'TestDoctorHelperOwnership' -v`
11. A refused older bundle carrying a rollover moves the pin; `emptyEffect` reports fail-closed for `requireBundle: true` with no trust file. `go test ./internal/sync ./internal/agent/claude -run 'TestRefusedOlderBundleStillRepins|TestEmptyEffectReadsRequireBundle' -v`
12. `scripts/check-svg.sh` passes on the tree and fails on a copy with a broken `</text>`, naming the file; `docs.yml` runs it. `scripts/test-checks.sh && grep -q check-svg.sh .github/workflows/docs.yml`
13. Full suite green. `go vet ./... && go test ./...`
14. threat-model, upgrading and self-host docs match D9 (no "put a limiter in front", no "plain HTTP only", actor change listed, four env vars and `awd admin-token` documented). `check: seat panel-security`

## Security notes

- New trust input: `X-Forwarded-For`. Read only from trusted CIDRs; otherwise any client could pick its own bucket and dodge the limit.
- The limiter must not become an oracle: a blocked key gets 429 before the handler runs, so no token comparison happens.
- The tokens file holds hashes only; `credential.Hash` is SHA-256 over a 256-bit random secret, so no salt or slow hash is needed. Startup errors never print a line's hash or a token.
- Constant-time comparison over all entries; timing reveals neither which name matched nor the entry count beyond the file size the operator controls.
- Lockout risk: one bad client behind a shared NAT can lock out admin, enroll and SCIM callers on that IP for up to a minute. Machine syncs are not affected (their 401s are not counted, and a blocked IP's bundle fetch waits for the next timer cycle). Documented.
