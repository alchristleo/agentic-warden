# Admin Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every `awd` serves a web admin console at `/console`: OIDC single sign-on, admin rights from IdP-sourced groups, CLI-parity screens, and an audit log written in the same transaction as each admin change.

**Architecture:** Three small Go packages do the identity work: `internal/console/sso` (OIDC), `internal/console/session` (server-side sessions), and `internal/console/authz` (admin-group check). Handlers in `internal/handler` wire them into routes and into the existing `requireAdmin` middleware, so every `/v1` admin route accepts either the bearer token or a console session. The store gains sessions, audit events, and an audit argument on the four admin writes. A Vite + React SPA in `console/` builds into `internal/console/dist`, which is committed and embedded with `go:embed`.

**Tech Stack:** Go 1.22 (`net/http` ServeMux patterns), `github.com/coreos/go-oidc/v3` v3.12.0, `golang.org/x/oauth2` v0.24.0, `github.com/go-jose/go-jose/v4` v4.0.5, pgx v5, Postgres 16. Frontend: pnpm, Vite 7, React 19, TypeScript 5, Tailwind 4, shadcn base-nova (`@base-ui/react`), TanStack Query 5, TanStack Router 1, CodeMirror 6, `yaml` 2, `diff` 8, Vitest, Testing Library, MSW 2, axe-core, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-25-admin-console-design.md`

## Global Constraints

Pinned values:
- The `go` directive stays `go 1.22`, and no dependency may raise it. go-oidc v3.14+ and oauth2 v0.28+ require Go 1.23, so pin go-oidc **v3.12.0**, oauth2 **v0.24.0**, go-jose **v4.0.5**.
- Node 20.19+, managed with pnpm. `console/` is its own package with its own lockfile; it does not join `web/`.
- The session token is 32 random bytes, base64url, with no padding. The store keeps only the hex SHA-256 of it (`credential.Hash` style).
- Session lifetimes: **8 h absolute**, **1 h idle**. `last_seen_at` is written at most once per minute per session. Expired sessions are swept hourly.
- Cookies:
  - `aw_login`: HttpOnly, `SameSite=Lax`, path `/console/auth`, 10 minutes.
  - `aw_session`: HttpOnly, `SameSite=Strict`, path `/`, no `Max-Age`.
  - Both carry `Secure` unless `AWD_PUBLIC_URL` is `http://localhost[:port]` or `http://127.0.0.1[:port]`.
- `AWD_CONSOLE_USER_CLAIM` defaults to `email`. An `email_verified` claim that is present and false rejects the login. Matching the user against group data is exact; case is never folded.
- Admin means `AWD_CONSOLE_ADMIN_GROUP` ∈ `SCIMGroupsFor(user)` ∪ `snapshot.Members[user]`. **Authored policy groups never count.**
- Actor: the SSO user on the console path. On the bearer path it is `token:` + the `X-Applied-By` header (just `token:` when the header is empty). `Revision.CreatedBy` and `GroupSnapshot.AppliedBy` take the actor.
- Audited actions and targets:
  - `policy.revision.create` (target: version)
  - `enrollment_token.create` (target: user; the token is never recorded)
  - `machine.revoke` (target: machine id; detail: user, name, os)
  - `groups.snapshot.apply` (detail: source, users, groups)
  - `console.login`, `console.login_denied` (detail: reason), `console.logout`
  - SCIM writes and reads are not audited.
- `GET /v1/audit`: `limit` defaults to 50 with a maximum of 200. `before` is an event id. Results come newest first. A bad value is 400.
- Static responses carry exactly this CSP: `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`. They also carry `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. `/console/assets/*` is `Cache-Control: public, max-age=31536000, immutable`, and everything else under `/console/` is `no-store`.
- No inline scripts anywhere in the SPA: the CSP forbids them. Vite's module-preload polyfill is disabled.
- Never log a session token, client secret, enrollment token, or ID token.

Failure modes (copied from the spec; each row gets a test):

| Condition | Behavior |
|---|---|
| Console env partly set | `awd` refuses to start, naming the missing variables |
| Console env all unset | `/console/*` answers 404; token path unchanged |
| `AWD_PUBLIC_URL` plain http on a non-loopback host | `awd` refuses to start |
| IdP discovery fails at startup | `awd` starts; login answers 503 and retries discovery next attempt |
| Callback: state mismatch or login cookie missing | 400, no session |
| Callback: code exchange fails | 502, logged, no session |
| ID token invalid (signature, iss, aud, exp, nonce) | 401, warn log with reason, audit `console.login_denied` |
| `email_verified` present and false | 403, audit `console.login_denied` |
| User claim missing or empty | 403, audit `console.login_denied` |
| No group data (no SCIM groups, no snapshot) | 503 "console needs group data (SCIM or awd groups apply)" at login and on every request |
| User not in admin group at login | 403, audit `console.login_denied` |
| User leaves admin group or is deprovisioned mid-session | next request 403 and every session of that user deleted |
| Session past 8 h absolute or 1 h idle | 401, row deleted; SPA returns to sign-in |
| Non-GET console request with missing or foreign `Origin` | 403, no side effect |
| Wrong bearer token alongside a valid cookie | 401; the cookie is not consulted |
| Audit insert fails | the whole write fails with 500; no unaudited change |
| Store unavailable | 503 from session lookup; token path fails as it does today |
| Revision YAML does not parse (client) | editor shows the error at its line; nothing is sent |
| Revision rejected by server validation | 422 message shown above the editor; draft kept |

Deviations from the spec, decided while planning (the spec was written without these details):
- `requireAdmin` keeps its name and gains the console path. It is not renamed to `requireAdminOrConsole`, which spares churn in every route line.
- Machines have no "host" or "agents" fields. The machines table shows `name` and `os`, and the revoke dialog asks the admin to type the machine's `name` (its `id` when the name is empty).
- The admin API exposes no SCIM group listing. The Groups screen lists the snapshot's groups with member counts, and shows SCIM totals from `GET /v1/groups`.
- `GET /console/api/me` also returns `signingKeyId`, so the SPA can flag machines pinning an old key.

## Review Focus

These are input classes the spec implies but never spells out. Each has a test in the task named.

1. **An IdP email whose case differs from the SCIM `userName`** (`Alice@x.com` vs `alice@x.com`). The login is denied as not-an-admin, with the exact user in the audit detail. The login is never admitted through case folding. → Task 5, `TestConsoleLoginCaseMismatchIsDenied`.
2. **The browser Back button replaying the callback URL after a successful login.** The login cookie is gone, but the user holds a valid session, so they are redirected to `/console/` with no 400 page. → Task 5, `TestConsoleCallbackReplayWithSessionRedirects`.
3. **`AWD_PUBLIC_URL` with a trailing slash, or with a path.** A trailing slash is trimmed. A path other than `/`, a query, or a fragment is a startup error, because the redirect URI and the Origin check both assume a bare origin. → Task 6, `TestConsolePublicURLShape`.
4. **Policy YAML whose `version` parses as a number** (`version: 2026.10`). The server's 400 message is shown above the editor verbatim, and the draft is kept. → Task 9, `shows the server message when the version is not a string`.
5. **Revoking a machine another admin already revoked.** A 404 shows "Already revoked" and the list refreshes; no generic error appears. → Task 8, `treats a 404 on revoke as already revoked`.

---

## File Structure

Go (create):
- `internal/model/console.go`: `ConsoleSession`, `AuditEvent`, audit action constants.
- `internal/store/migrations/0006_console.sql`: `console_sessions`, `audit_events`.
- `internal/store/memory_console.go`, `internal/store/postgres_console.go`: session and audit storage.
- `internal/store/storetest/console.go`: conformance cases for sessions and audit.
- `internal/console/assets.go`: `go:embed all:dist`, `Assets() fs.FS`.
- `internal/console/dist/index.html`: a placeholder until Task 7 builds the real SPA.
- `internal/console/sso/sso.go` and `sso_test.go`: OIDC client.
- `internal/console/oidctest/oidctest.go`: fake IdP; `internal/console/oidctest/cmd/fakeidp/main.go` is its binary.
- `internal/console/session/session.go` and `session_test.go`.
- `internal/console/authz/authz.go` and `authz_test.go`.
- `internal/handler/actor.go`: actor context helpers.
- `internal/handler/audit.go` and `audit_test.go`: `GET /v1/audit`.
- `internal/handler/console.go`: `Console` config, the console middleware, `me`, and static files.
- `internal/handler/console_auth.go`: login, callback, logout.
- `internal/handler/console_test.go`: failure-mode tests.

Go (modify):
- `internal/store/store.go`: `ConsoleStore` interface, and an audit argument on four writes.
- `internal/store/memory.go`, `internal/store/postgres.go`: the four writes and `Truncate`.
- `internal/store/storetest/conformance.go`: call-site updates, and `RunConsole`.
- `internal/handler/auth.go`: `requireAdmin` gains the actor and the console path.
- `internal/handler/handler.go`, `enroll.go`, `groups.go`: audit events and routes.
- `internal/config/config.go` and `config_test.go`: console configuration.
- `cmd/awd/main.go`: wiring, sweeper, usage text.
- `go.mod`, `go.sum`.
- `README.md`: "Admin console" section.

Frontend (create), `console/`:
- `package.json`, `pnpm-lock.yaml`, `vite.config.ts`, `tsconfig.json`, `index.html`, `components.json`, `vitest.config.ts`, `playwright.config.ts`
- `src/main.tsx`, `src/app.tsx` (auth gate and layout), `src/router.tsx`, `src/api.ts`, `src/types.ts`, `src/theme.ts`, `src/index.css`
- `src/components/ui/*`: shadcn button, card, table, dialog, input, label, badge.
- `src/screens/{overview,policy,machines,enrollment,groups,audit,sign-in}.tsx`
- `src/policy/{editor.tsx,yaml.ts,diff.tsx}`
- `src/test/{setup.ts,server.ts,render.tsx}` and `src/**/*.test.tsx`
- `e2e/console.spec.ts`, `e2e/global-setup.ts`
- `.github/workflows/console.yml`

---

### Task 1: Store — sessions, audit events, audited writes

**Files:**
- Create: `internal/model/console.go`, `internal/store/migrations/0006_console.sql`, `internal/store/memory_console.go`, `internal/store/postgres_console.go`, `internal/store/storetest/console.go`, `internal/handler/actor.go`
- Modify: `internal/store/store.go`, `internal/store/memory.go`, `internal/store/postgres.go`, `internal/store/storetest/conformance.go`, `internal/handler/auth.go`, `internal/handler/handler.go`, `internal/handler/enroll.go`, `internal/handler/groups.go`

**Interfaces:**
- Produces:
  - `model.ConsoleSession{TokenHash, User string; CreatedAt, LastSeenAt time.Time}`
  - `model.AuditEvent{ID int64; At time.Time; Actor, Action, Target string; Detail map[string]any}` and `(AuditEvent).Validate() error`
  - action constants `model.AuditRevisionCreate`, `AuditTokenCreate`, `AuditMachineRevoke`, `AuditGroupsApply`, `AuditLogin`, `AuditLoginDenied`, `AuditLogout`
  - `store.ConsoleStore` (below), embedded in `store.Store`
  - `PutRuleSet(ctx, r, audit model.AuditEvent) error`, `PutEnrollmentToken(ctx, t, audit) error`, `DeleteMachine(ctx, id, audit) error`, `PutGroupSnapshot(ctx, s, audit) error`
  - `storetest.RunConsole(t, func(t) store.Store)`, which `storetest.Run` calls
  - `handler.withActor(r *http.Request, actor string) *http.Request` and `handler.actorOf(ctx context.Context) string`

- [ ] **Step 1: Model types**

`internal/model/console.go`:

```go
package model

import (
	"fmt"
	"time"
)

// ConsoleSession is one signed-in console user. The store holds only the
// hash of the session token, as it does for machine credentials, so a
// database read never yields a usable cookie.
type ConsoleSession struct {
	TokenHash  string
	User       string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Audit actions. Each admin change writes exactly one event, in the same
// transaction as the change.
const (
	AuditRevisionCreate = "policy.revision.create"
	AuditTokenCreate    = "enrollment_token.create"
	AuditMachineRevoke  = "machine.revoke"
	AuditGroupsApply    = "groups.snapshot.apply"
	AuditLogin          = "console.login"
	AuditLoginDenied    = "console.login_denied"
	AuditLogout         = "console.logout"
)

// AuditEvent records who changed what. Actor is the SSO user, or
// "token:<applied-by>" for a request made with the admin token.
type AuditEvent struct {
	ID     int64          `json:"id"`
	At     time.Time      `json:"at"`
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	Target string         `json:"target,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// Validate reports whether the event names an actor and an action. An
// event without either would be an audit row nobody can read.
func (e AuditEvent) Validate() error {
	if e.Actor == "" {
		return fmt.Errorf("audit event has no actor: %w", ErrBadInput)
	}
	if e.Action == "" {
		return fmt.Errorf("audit event has no action: %w", ErrBadInput)
	}
	return nil
}
```

- [ ] **Step 2: Store interface**

In `internal/store/store.go`, embed `ConsoleStore` in `Store` next to `SCIMStore`, change the four write signatures, and add:

```go
	// PutRuleSet stores a new revision and its audit event atomically. It
	// returns model.ErrBadInput for a revision without a version or an
	// audit event without an actor or action, and model.ErrConflict when
	// that version is already stored, because a revision is immutable once
	// written. Either failure stores neither.
	PutRuleSet(ctx context.Context, r model.Revision, audit model.AuditEvent) error
	// PutEnrollmentToken stores a token and its audit event atomically.
	PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken, audit model.AuditEvent) error
	// DeleteMachine revokes a machine and records audit atomically, or
	// model.ErrNotFound, in which case no event is written.
	DeleteMachine(ctx context.Context, id string, audit model.AuditEvent) error
	// PutGroupSnapshot stores a snapshot and its audit event atomically.
	PutGroupSnapshot(ctx context.Context, s model.GroupSnapshot, audit model.AuditEvent) error
```

```go
// ConsoleStore holds console sessions and the audit log.
type ConsoleStore interface {
	// CreateSession stores a session. model.ErrBadInput without a token
	// hash or user; model.ErrConflict when the hash exists.
	CreateSession(ctx context.Context, s model.ConsoleSession) error
	// SessionByHash returns a session, or model.ErrNotFound.
	SessionByHash(ctx context.Context, hash string) (model.ConsoleSession, error)
	// TouchSession sets LastSeenAt, or model.ErrNotFound.
	TouchSession(ctx context.Context, hash string, at time.Time) error
	// DeleteSession removes a session. Deleting one that does not exist is
	// not an error: logout is idempotent.
	DeleteSession(ctx context.Context, hash string) error
	// DeleteSessionsFor removes every session of a user.
	DeleteSessionsFor(ctx context.Context, user string) error
	// DeleteExpiredSessions removes sessions created before createdBefore
	// or last seen before seenBefore, and returns how many it removed.
	DeleteExpiredSessions(ctx context.Context, createdBefore, seenBefore time.Time) (int, error)
	// RecordAudit stores an event that accompanies no other write (login,
	// logout). model.ErrBadInput when Validate fails.
	RecordAudit(ctx context.Context, e model.AuditEvent) error
	// AuditEvents returns up to limit events, newest first. before, when
	// positive, returns only events with a smaller ID.
	AuditEvents(ctx context.Context, limit int, before int64) ([]model.AuditEvent, error)
}
```

- [ ] **Step 3: Write the failing conformance cases**

`internal/store/storetest/console.go`:

```go
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

// RunConsole checks console sessions and the audit log.
func RunConsole(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(t *testing.T, s store.Store)
	}{
		{"a session can be read back by its hash", sessionRoundTrip},
		{"a session needs a hash and a user", sessionRequired},
		{"a session hash is unique", sessionHashUnique},
		{"an unknown session is not found", sessionUnknown},
		{"touching a session moves last seen", sessionTouch},
		{"deleting a session is idempotent", sessionDeleteIdempotent},
		{"deleting a user's sessions leaves others", sessionDeleteFor},
		{"expired sessions are swept by either bound", sessionSweep},
		{"audit events list newest first with a before cursor", auditList},
		{"an audit event needs an actor and an action", auditRequired},
		{"each audited write stores exactly one event", auditedWrites},
		{"a bad audit event leaves the paired write undone", auditRollback},
		{"revoking an unknown machine writes no event", auditNotFoundWritesNothing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.fn(t, newStore(t)) })
	}
}

func audit(action, target string) model.AuditEvent {
	return model.AuditEvent{At: epoch, Actor: "token:tester", Action: action, Target: target}
}

func session(hash, user string, created, seen time.Time) model.ConsoleSession {
	return model.ConsoleSession{TokenHash: hash, User: user, CreatedAt: created, LastSeenAt: seen}
}

func sessionRoundTrip(t *testing.T, s store.Store) {
	ctx := context.Background()
	want := session("h1", "alice@example.com", epoch, epoch)
	if err := s.CreateSession(ctx, want); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.SessionByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("SessionByHash: %v", err)
	}
	if got.User != want.User || !got.CreatedAt.Equal(epoch) || !got.LastSeenAt.Equal(epoch) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func sessionRequired(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, bad := range []model.ConsoleSession{session("", "a", epoch, epoch), session("h", "", epoch, epoch)} {
		if err := s.CreateSession(ctx, bad); !errors.Is(err, model.ErrBadInput) {
			t.Errorf("CreateSession(%+v) = %v, want ErrBadInput", bad, err)
		}
	}
}

func sessionHashUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.CreateSession(ctx, session("h1", "a", epoch, epoch)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, session("h1", "b", epoch, epoch)); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second CreateSession = %v, want ErrConflict", err)
	}
}

func sessionUnknown(t *testing.T, s store.Store) {
	ctx := context.Background()
	if _, err := s.SessionByHash(ctx, "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("SessionByHash = %v, want ErrNotFound", err)
	}
	if err := s.TouchSession(ctx, "nope", epoch); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("TouchSession = %v, want ErrNotFound", err)
	}
}

func sessionTouch(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.CreateSession(ctx, session("h1", "a", epoch, epoch)); err != nil {
		t.Fatal(err)
	}
	later := epoch.Add(5 * time.Minute)
	if err := s.TouchSession(ctx, "h1", later); err != nil {
		t.Fatal(err)
	}
	got, _ := s.SessionByHash(ctx, "h1")
	if !got.LastSeenAt.Equal(later) || !got.CreatedAt.Equal(epoch) {
		t.Fatalf("got %+v, want last seen %v and created %v", got, later, epoch)
	}
}

func sessionDeleteIdempotent(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.CreateSession(ctx, session("h1", "a", epoch, epoch)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.DeleteSession(ctx, "h1"); err != nil {
			t.Fatalf("DeleteSession #%d: %v", i+1, err)
		}
	}
	if _, err := s.SessionByHash(ctx, "h1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("after delete = %v, want ErrNotFound", err)
	}
}

func sessionDeleteFor(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, sess := range []model.ConsoleSession{
		session("a1", "alice", epoch, epoch), session("a2", "alice", epoch, epoch), session("b1", "bob", epoch, epoch),
	} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteSessionsFor(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"a1", "a2"} {
		if _, err := s.SessionByHash(ctx, h); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s survived: %v", h, err)
		}
	}
	if _, err := s.SessionByHash(ctx, "b1"); err != nil {
		t.Errorf("bob's session went too: %v", err)
	}
}

func sessionSweep(t *testing.T, s store.Store) {
	ctx := context.Background()
	old := epoch.Add(-9 * time.Hour)
	for _, sess := range []model.ConsoleSession{
		session("fresh", "a", epoch, epoch),
		session("too-old", "a", old, epoch),                    // past the absolute bound
		session("idle", "a", epoch, epoch.Add(-2*time.Hour)),    // past the idle bound
	} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteExpiredSessions(ctx, epoch.Add(-8*time.Hour), epoch.Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("DeleteExpiredSessions = %d, %v; want 2, nil", n, err)
	}
	if _, err := s.SessionByHash(ctx, "fresh"); err != nil {
		t.Fatalf("fresh session swept: %v", err)
	}
}

func auditList(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, target := range []string{"one", "two", "three"} {
		if err := s.RecordAudit(ctx, audit(model.AuditLogin, target)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.AuditEvents(ctx, 10, 0)
	if err != nil || len(all) != 3 || all[0].Target != "three" || all[2].Target != "one" {
		t.Fatalf("AuditEvents = %+v, %v", all, err)
	}
	if all[0].ID <= all[1].ID {
		t.Fatalf("ids not decreasing: %d, %d", all[0].ID, all[1].ID)
	}
	page, err := s.AuditEvents(ctx, 1, all[0].ID)
	if err != nil || len(page) != 1 || page[0].Target != "two" {
		t.Fatalf("page after %d = %+v, %v", all[0].ID, page, err)
	}
	empty, err := s.AuditEvents(ctx, 10, all[2].ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("past the end = %#v, %v; want empty non-nil slice", empty, err)
	}
}

func auditRequired(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, bad := range []model.AuditEvent{{Action: "x"}, {Actor: "x"}} {
		if err := s.RecordAudit(ctx, bad); !errors.Is(err, model.ErrBadInput) {
			t.Errorf("RecordAudit(%+v) = %v, want ErrBadInput", bad, err)
		}
	}
}

func auditedWrites(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutRuleSet(ctx, revision("v1", epoch), audit(model.AuditRevisionCreate, "v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutEnrollmentToken(ctx, token("t1", "alice", epoch.Add(time.Hour)), audit(model.AuditTokenCreate, "alice")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMachine(ctx, machine("m1", "alice", "c1", epoch)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, "m1", audit(model.AuditMachineRevoke, "m1")); err != nil {
		t.Fatal(err)
	}
	withDetail := audit(model.AuditGroupsApply, "")
	withDetail.Detail = map[string]any{"source": "okta", "users": float64(1)}
	if err := s.PutGroupSnapshot(ctx, snapshot("okta", epoch, map[string][]string{"alice": {"devs"}}), withDetail); err != nil {
		t.Fatal(err)
	}
	events, err := s.AuditEvents(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	want := []string{model.AuditGroupsApply, model.AuditMachineRevoke, model.AuditTokenCreate, model.AuditRevisionCreate}
	if !equalStrings(actions, want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	if events[0].Detail["source"] != "okta" || events[0].Detail["users"] != float64(1) {
		t.Fatalf("detail round trip = %#v", events[0].Detail)
	}
}

// auditRollback proves the pairing is atomic. Postgres must not pre-check
// the event in Go: the CHECK constraint fails the insert inside the
// transaction, so this case exercises a real rollback.
func auditRollback(t *testing.T, s store.Store) {
	ctx := context.Background()
	bad := model.AuditEvent{Actor: "token:tester"} // no action
	if err := s.PutRuleSet(ctx, revision("v1", epoch), bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("PutRuleSet with bad audit = %v, want ErrBadInput", err)
	}
	if _, err := s.CurrentRuleSet(ctx); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("revision stored despite failed audit: %v", err)
	}
	if err := s.PutMachine(ctx, machine("m1", "alice", "c1", epoch)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, "m1", bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("DeleteMachine with bad audit = %v, want ErrBadInput", err)
	}
	if _, err := s.MachineByCredential(ctx, "c1"); err != nil {
		t.Fatalf("machine deleted despite failed audit: %v", err)
	}
	if err := s.PutEnrollmentToken(ctx, token("t1", "alice", epoch.Add(time.Hour)), bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("PutEnrollmentToken with bad audit = %v", err)
	}
	if _, err := s.ConsumeEnrollmentToken(ctx, "t1", epoch); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("token stored despite failed audit: %v", err)
	}
	if err := s.PutGroupSnapshot(ctx, snapshot("okta", epoch, map[string][]string{"a": {"g"}}), bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("PutGroupSnapshot with bad audit = %v", err)
	}
	if _, err := s.CurrentGroupSnapshot(ctx); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("snapshot stored despite failed audit: %v", err)
	}
	if events, _ := s.AuditEvents(ctx, 10, 0); len(events) != 0 {
		t.Fatalf("events written: %+v", events)
	}
}

func auditNotFoundWritesNothing(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.DeleteMachine(ctx, "ghost", audit(model.AuditMachineRevoke, "ghost")); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("DeleteMachine = %v, want ErrNotFound", err)
	}
	if events, _ := s.AuditEvents(ctx, 10, 0); len(events) != 0 {
		t.Fatalf("events written for a missing machine: %+v", events)
	}
}
```

In `conformance.go`:
- Call `RunConsole(t, newStore)` after `RunSCIM`.
- Every existing `PutRuleSet(ctx, r)` becomes `PutRuleSet(ctx, r, audit(model.AuditRevisionCreate, r.Version))`. Apply the same to `PutEnrollmentToken` (`audit(model.AuditTokenCreate, user)`), `DeleteMachine` (`audit(model.AuditMachineRevoke, id)`), and `PutGroupSnapshot` (`audit(model.AuditGroupsApply, "")`).
- `snapshotUserKeyRequired` and `snapshotGroupNameRequired` keep expecting ErrBadInput from the snapshot's own validation.

- [ ] **Step 4: Run to verify failure**

Run: `go vet ./internal/store/...`
Expected: compile errors. `Memory` and `Postgres` do not implement `ConsoleStore`, and the write signatures don't match.

- [ ] **Step 5: Migration**

`internal/store/migrations/0006_console.sql`:

```sql
-- Console sessions: only the token's hash is stored, as for machine
-- credentials. The audit log records every admin change; its CHECKs make an
-- event without an actor or action fail inside the change's transaction.
CREATE TABLE IF NOT EXISTS console_sessions (
    token_hash   text PRIMARY KEY CHECK (token_hash <> ''),
    user_name    text NOT NULL CHECK (user_name <> ''),
    created_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS console_sessions_user ON console_sessions (user_name);

CREATE TABLE IF NOT EXISTS audit_events (
    id     bigserial PRIMARY KEY,
    at     timestamptz NOT NULL,
    actor  text NOT NULL CHECK (actor <> ''),
    action text NOT NULL CHECK (action <> ''),
    target text NOT NULL DEFAULT '',
    detail jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS audit_events_at ON audit_events (at);
```

- [ ] **Step 6: Memory implementation**

In `memory.go`, add these fields to `Memory`: `sessions map[string]model.ConsoleSession` (initialize it in `NewMemory`), `audit []model.AuditEvent`, and `nextAudit int64`. In each of the four writes, call `audit.Validate()` **inside the lock, before mutating**, and on success call `m.appendAudit(audit)` in the same critical section:

```go
// PutRuleSet stores a revision and its audit event.
func (m *Memory) PutRuleSet(_ context.Context, r model.Revision, audit model.AuditEvent) error {
	if r.Version == "" {
		return fmt.Errorf("store: revision has no version: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := audit.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if m.versions[r.Version] {
		return fmt.Errorf("store: revision %q already exists: %w", r.Version, model.ErrConflict)
	}
	m.nextSeq++
	r.Seq = m.nextSeq
	m.versions[r.Version] = true
	m.revisions = append(m.revisions, r)
	m.appendAudit(audit)
	return nil
}
```

`DeleteMachine`: check that the machine exists (ErrNotFound) **before** validating the audit event, so a missing machine never reaches the audit path; then validate, delete, and append. Change `PutEnrollmentToken` and `PutGroupSnapshot` the same way.

`internal/store/memory_console.go`:

```go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// appendAudit records an event. Callers hold m.mu for writing and have
// validated the event.
func (m *Memory) appendAudit(e model.AuditEvent) {
	m.nextAudit++
	e.ID = m.nextAudit
	if e.At.IsZero() {
		e.At = time.Now()
	}
	m.audit = append(m.audit, e)
}

// CreateSession stores a session.
func (m *Memory) CreateSession(_ context.Context, s model.ConsoleSession) error {
	if s.TokenHash == "" || s.User == "" {
		return fmt.Errorf("store: session needs a token hash and a user: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.TokenHash]; ok {
		return fmt.Errorf("store: session exists: %w", model.ErrConflict)
	}
	m.sessions[s.TokenHash] = s
	return nil
}

// SessionByHash returns a session.
func (m *Memory) SessionByHash(_ context.Context, hash string) (model.ConsoleSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[hash]
	if !ok {
		return model.ConsoleSession{}, fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	return s, nil
}

// TouchSession records activity on a session.
func (m *Memory) TouchSession(_ context.Context, hash string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[hash]
	if !ok {
		return fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	s.LastSeenAt = at
	m.sessions[hash] = s
	return nil
}

// DeleteSession removes a session if it exists.
func (m *Memory) DeleteSession(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, hash)
	return nil
}

// DeleteSessionsFor removes every session of a user.
func (m *Memory) DeleteSessionsFor(_ context.Context, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, s := range m.sessions {
		if s.User == user {
			delete(m.sessions, h)
		}
	}
	return nil
}

// DeleteExpiredSessions sweeps sessions past either bound.
func (m *Memory) DeleteExpiredSessions(_ context.Context, createdBefore, seenBefore time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for h, s := range m.sessions {
		if s.CreatedAt.Before(createdBefore) || s.LastSeenAt.Before(seenBefore) {
			delete(m.sessions, h)
			n++
		}
	}
	return n, nil
}

// RecordAudit stores a standalone event.
func (m *Memory) RecordAudit(_ context.Context, e model.AuditEvent) error {
	if err := e.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendAudit(e)
	return nil
}

// AuditEvents lists events newest first.
func (m *Memory) AuditEvents(_ context.Context, limit int, before int64) ([]model.AuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.AuditEvent, 0)
	for i := len(m.audit) - 1; i >= 0 && len(out) < limit; i-- {
		if before > 0 && m.audit[i].ID >= before {
			continue
		}
		out = append(out, m.audit[i])
	}
	return out, nil
}
```

- [ ] **Step 7: Postgres implementation**

`internal/store/postgres_console.go`:

```go
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/acme/agent-wrapper/internal/model"
)

// checkViolation is the Postgres error code for a failed CHECK constraint:
// here, an audit event without an actor or action.
const checkViolation = "23514"

// insertAudit writes an event inside tx. It deliberately does not call
// Validate: the table's CHECKs reject a bad event inside the transaction,
// so the paired write rolls back with it. The conformance suite relies on
// that.
func insertAudit(ctx context.Context, tx pgx.Tx, e model.AuditEvent) error {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	detail := e.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("store: encoding audit detail: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO audit_events (at, actor, action, target, detail) VALUES ($1, $2, $3, $4, $5)`,
		at, e.Actor, e.Action, e.Target, encoded)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == checkViolation {
		return fmt.Errorf("store: audit event needs an actor and an action: %w", model.ErrBadInput)
	}
	if err != nil {
		return fmt.Errorf("store: recording audit event: %w", err)
	}
	return nil
}

// CreateSession stores a session.
func (p *Postgres) CreateSession(ctx context.Context, s model.ConsoleSession) error {
	if s.TokenHash == "" || s.User == "" {
		return fmt.Errorf("store: session needs a token hash and a user: %w", model.ErrBadInput)
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO console_sessions (token_hash, user_name, created_at, last_seen_at) VALUES ($1, $2, $3, $4)`,
		s.TokenHash, s.User, s.CreatedAt, s.LastSeenAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return fmt.Errorf("store: session exists: %w", model.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("store: storing session: %w", err)
	}
	return nil
}

// SessionByHash returns a session.
func (p *Postgres) SessionByHash(ctx context.Context, hash string) (model.ConsoleSession, error) {
	s := model.ConsoleSession{TokenHash: hash}
	err := p.pool.QueryRow(ctx,
		`SELECT user_name, created_at, last_seen_at FROM console_sessions WHERE token_hash = $1`, hash).
		Scan(&s.User, &s.CreatedAt, &s.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ConsoleSession{}, fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	if err != nil {
		return model.ConsoleSession{}, fmt.Errorf("store: reading session: %w", err)
	}
	return s, nil
}

// TouchSession records activity on a session.
func (p *Postgres) TouchSession(ctx context.Context, hash string, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE console_sessions SET last_seen_at = $2 WHERE token_hash = $1`, hash, at)
	if err != nil {
		return fmt.Errorf("store: touching session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	return nil
}

// DeleteSession removes a session if it exists.
func (p *Postgres) DeleteSession(ctx context.Context, hash string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM console_sessions WHERE token_hash = $1`, hash); err != nil {
		return fmt.Errorf("store: deleting session: %w", err)
	}
	return nil
}

// DeleteSessionsFor removes every session of a user.
func (p *Postgres) DeleteSessionsFor(ctx context.Context, user string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM console_sessions WHERE user_name = $1`, user); err != nil {
		return fmt.Errorf("store: deleting sessions for %q: %w", user, err)
	}
	return nil
}

// DeleteExpiredSessions sweeps sessions past either bound.
func (p *Postgres) DeleteExpiredSessions(ctx context.Context, createdBefore, seenBefore time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM console_sessions WHERE created_at < $1 OR last_seen_at < $2`, createdBefore, seenBefore)
	if err != nil {
		return 0, fmt.Errorf("store: sweeping sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RecordAudit stores a standalone event.
func (p *Postgres) RecordAudit(ctx context.Context, e model.AuditEvent) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error { return insertAudit(ctx, tx, e) })
}

// AuditEvents lists events newest first.
func (p *Postgres) AuditEvents(ctx context.Context, limit int, before int64) ([]model.AuditEvent, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, at, actor, action, target, detail FROM audit_events
		WHERE $2 <= 0 OR id < $2
		ORDER BY id DESC LIMIT $1`, limit, before)
	if err != nil {
		return nil, fmt.Errorf("store: listing audit events: %w", err)
	}
	defer rows.Close()
	out := make([]model.AuditEvent, 0)
	for rows.Next() {
		var (
			e      model.AuditEvent
			detail []byte
		)
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Target, &detail); err != nil {
			return nil, fmt.Errorf("store: reading an audit event: %w", err)
		}
		if err := json.Unmarshal(detail, &e.Detail); err != nil {
			return nil, fmt.Errorf("store: decoding audit detail %d: %w", e.ID, err)
		}
		if len(e.Detail) == 0 {
			e.Detail = nil
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading audit events: %w", err)
	}
	return out, nil
}
```

In `postgres.go`, wrap each of the four writes in `pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error { ...; return insertAudit(ctx, tx, audit) })`, using `tx.Exec` for the existing statement and keeping its existing error mapping. `DeleteMachine` returns ErrNotFound **before** `insertAudit` when `RowsAffected() == 0`. Example:

```go
func (p *Postgres) DeleteMachine(ctx context.Context, id string, audit model.AuditEvent) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM machines WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("store: deleting machine %q: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
		}
		return insertAudit(ctx, tx, audit)
	})
}
```

Add `console_sessions, audit_events` to the `TRUNCATE` list in `Truncate`.

- [ ] **Step 8: Actor helpers and handler call sites**

`internal/handler/actor.go`:

```go
package handler

import (
	"context"
	"net/http"
)

type actorKey struct{}

// withActor records who is making an admin request: the console user, or
// "token:<X-Applied-By>" for the admin token. Handlers read it with
// actorOf and put it on revisions, snapshots and audit events.
func withActor(r *http.Request, actor string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorKey{}, actor))
}

// actorOf returns the request's actor. Every admin route sets one, so an
// empty result means a handler was mounted without requireAdmin.
func actorOf(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}
```

In `auth.go` `requireAdmin`, replace `next(w, r)` with `next(w, withActor(r, "token:"+r.Header.Get("X-Applied-By")))`.

Handlers:
- `postRevision`: `CreatedBy: actorOf(r.Context())`. The store call becomes `h.store.PutRuleSet(r.Context(), revision, model.AuditEvent{At: h.Now(), Actor: actorOf(r.Context()), Action: model.AuditRevisionCreate, Target: revision.Version})`.
- `postEnrollmentToken`: `model.AuditEvent{At: h.Now(), Actor: actorOf(r.Context()), Action: model.AuditTokenCreate, Target: req.User, Detail: map[string]any{"expiresAt": expires}}`.
- `deleteMachine`: look up the machine first so the event can name it, then delete:

```go
func (h *Handler) deleteMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	machines, err := h.store.ListMachines(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	detail := map[string]any{}
	for _, m := range machines {
		if m.ID == id {
			detail = map[string]any{"user": m.User, "name": m.Name, "os": m.OS}
		}
	}
	event := model.AuditEvent{At: h.Now(), Actor: actorOf(r.Context()), Action: model.AuditMachineRevoke, Target: id, Detail: detail}
	if err := h.store.DeleteMachine(r.Context(), id, event); err != nil {
		h.fail(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "machine revoked", "machine", id, "actor", event.Actor)
	w.WriteHeader(http.StatusNoContent)
}
```

- `putGroups`: `AppliedBy: actorOf(r.Context())`. The audit event is `Action: model.AuditGroupsApply`, with `Detail: map[string]any{"source": sum.Source, "users": sum.Users, "groups": sum.Groups}`, where `sum := summarise(snapshot)`.

Then run `grep -rn "AppliedBy\|CreatedBy" internal/handler/*_test.go cmd/awd/*_test.go` and update every assertion that expected the raw `X-Applied-By` value to expect `token:<value>`.

- [ ] **Step 9: Run tests**

Run: `go test ./internal/store/... ./internal/handler/... ./cmd/...`
Expected: PASS.

Then run the Postgres suite against Docker (see memory for the container):
Run: `AWD_TEST_DATABASE_URL='postgres://postgres:pw@localhost:5432/postgres?sslmode=disable' go test ./internal/store/ -run TestPostgres -count=1`
Expected: PASS, including `a bad audit event leaves the paired write undone`.

- [ ] **Step 10: Commit**

```bash
git add internal/model/console.go internal/store internal/handler
git commit -m "feat(store): console sessions, audit log, and audited admin writes"
```

---

### Task 2: `GET /v1/audit` and per-action audit tests

**Files:**
- Create: `internal/handler/audit.go`, `internal/handler/audit_test.go`
- Modify: `internal/handler/handler.go` (route)

**Interfaces:**
- Consumes: `store.AuditEvents(ctx, limit int, before int64)`, `actorOf`
- Produces: `GET /v1/audit?limit=&before=` → `[]model.AuditEvent` JSON

- [ ] **Step 1: Failing tests**

`internal/handler/audit_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
)

func auditEvents(t *testing.T, resp *http.Response) []model.AuditEvent {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var events []model.AuditEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestEachAdminWriteIsAuditedWithTheTokenActor(t *testing.T) {
	srv := newServer(t)
	req := func(method, path, body string) {
		t.Helper()
		r, _ := http.NewRequest(method, srv.URL+path, stringsReader(body))
		r.Header.Set("Authorization", "Bearer "+adminToken)
		r.Header.Set("X-Applied-By", "alice")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s = %d", method, path, resp.StatusCode)
		}
	}
	req("POST", "/v1/policy/revisions", `{"version":"v1"}`)
	req("POST", "/v1/enrollment-tokens", `{"user":"bob@example.com"}`)
	req("PUT", "/v1/groups", `{"source":"okta","members":{"bob@example.com":["devs"]}}`)
	id := enrollMachine(t, srv, "bob@example.com", "laptop")
	req("DELETE", "/v1/machines/"+id, "")

	events := auditEvents(t, getAs(t, srv, "/v1/audit", adminToken))
	want := []struct{ action, target string }{
		{model.AuditMachineRevoke, id},
		{model.AuditTokenCreate, "bob@example.com"}, // enrollMachine's own token
		{model.AuditGroupsApply, ""},
		{model.AuditTokenCreate, "bob@example.com"},
		{model.AuditRevisionCreate, "v1"},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	for i, w := range want {
		if events[i].Action != w.action || events[i].Target != w.target {
			t.Errorf("event %d = %s %q, want %s %q", i, events[i].Action, events[i].Target, w.action, w.target)
		}
	}
	if events[0].Detail["name"] != "laptop" || events[0].Detail["user"] != "bob@example.com" {
		t.Errorf("revoke detail = %#v", events[0].Detail)
	}
	for _, e := range events[1:] {
		if e.Actor != "token:alice" && e.Actor != "token:" {
			t.Errorf("actor = %q", e.Actor)
		}
	}
	if events[4].Actor != "token:alice" {
		t.Errorf("revision actor = %q, want token:alice", events[4].Actor)
	}
	for _, e := range events {
		if _, leaked := e.Detail["token"]; leaked {
			t.Fatalf("token recorded in audit: %+v", e)
		}
	}
}

func TestAuditPagingAndLimits(t *testing.T) {
	srv := newServer(t)
	for _, v := range []string{"v1", "v2", "v3"} {
		post(t, srv, "/v1/policy/revisions", `{"version":"`+v+`"}`)
	}
	all := auditEvents(t, getAs(t, srv, "/v1/audit?limit=2", adminToken))
	if len(all) != 2 || all[0].Target != "v3" {
		t.Fatalf("limit=2 → %+v", all)
	}
	next := auditEvents(t, getAs(t, srv, "/v1/audit?before="+itoa(all[1].ID), adminToken))
	if len(next) != 1 || next[0].Target != "v1" {
		t.Fatalf("before → %+v", next)
	}
	for _, bad := range []string{"limit=0", "limit=201", "limit=x", "before=-1", "before=x"} {
		if resp := getAs(t, srv, "/v1/audit?"+bad, adminToken); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", bad, resp.StatusCode)
		}
	}
	if resp := getAs(t, srv, "/v1/audit", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token → %d", resp.StatusCode)
	}
}

// An audit write that fails fails the change with it: a 500, and nothing
// stored.
func TestAuditFailureFailsTheWrite(t *testing.T) {
	srv := newServerWithStore(t, failingAudit{Store: store.NewMemory()})
	resp := post(t, srv, "/v1/policy/revisions", `{"version":"v1"}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", resp.StatusCode)
	}
	if got := getAs(t, srv, "/v1/policy/revisions", adminToken); !bodyIs(t, got, "[]") {
		t.Fatal("revision stored despite audit failure")
	}
}
```

Put these helpers in the same file:
- `stringsReader(s string) io.Reader`, which returns nil for `""` and `strings.NewReader(s)` otherwise.
- `itoa(n int64) string`, a wrapper over `strconv.FormatInt`.
- `bodyIs(t, resp, want string) bool`, which reads the body and compares after `strings.TrimSpace`.
- `enrollMachine(t, srv, user, name string) string`, which POSTs `/v1/enrollment-tokens` as admin, POSTs `/v1/machines/enroll` with `{"token":…,"name":name,"os":"linux"}`, and returns the `id` from the response. If `enroll_test.go` already has an equivalent helper, reuse it rather than duplicating it.
- `failingAudit`, which embeds `store.Store` and overrides `PutRuleSet` to return `errors.New("disk full")` without calling through.
- `newServerWithStore(t, s store.Store)`, the same as `newServer` but taking the store. Refactor `newServer` to call it.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run 'Audit' -v`
Expected: FAIL. `/v1/audit` answers 404.

- [ ] **Step 3: Implement**

`internal/handler/audit.go`:

```go
package handler

import (
	"net/http"
	"strconv"
)

const (
	defaultAuditLimit = 50
	maxAuditLimit     = 200
)

// getAudit lists audit events, newest first. before pages backwards by
// event id, which stays stable while new events arrive.
func (h *Handler) getAudit(w http.ResponseWriter, r *http.Request) {
	limit := defaultAuditLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxAuditLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = n
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "before must be a positive event id")
			return
		}
		before = n
	}
	events, err := h.store.AuditEvents(r.Context(), limit, before)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
```

In `Routes`: `mux.HandleFunc("GET /v1/audit", h.requireAdmin(h.getAudit))`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handler/ -v -run 'Audit'`, then `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler
git commit -m "feat(awd): GET /v1/audit and audit coverage for every admin write"
```

---

### Task 3: `sso` package and the `oidctest` fake IdP

**Files:**
- Create: `internal/console/sso/sso.go`, `internal/console/sso/sso_test.go`, `internal/console/oidctest/oidctest.go`, `internal/console/oidctest/cmd/fakeidp/main.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces (`sso`):
  - `type Config struct{ Issuer, ClientID, ClientSecret, RedirectURL, UserClaim string; HTTPClient *http.Client }`
  - `type Attempt struct{ State, Nonce, Verifier string }`, `(Attempt).Encode() string`, `DecodeAttempt(string) (Attempt, bool)`
  - `New(Config) *Client`, `(*Client).Discover(ctx) error`, `(*Client).Start(ctx) (Attempt, string, error)`, `(*Client).Finish(ctx, Attempt, code string) (string, error)`
  - errors `ErrUnavailable`, `ErrExchange`, `ErrInvalidToken`, `ErrUnverified`, `ErrNoUser`
- Produces (`oidctest`):
  - `type Tamper int` with `None, WrongNonce, WrongAudience, Expired, BadSignature`
  - `type Provider struct{ Issuer, ClientID, ClientSecret string; User string; Claims map[string]any; Tamper Tamper; ... }`
  - `New(issuer, clientID, clientSecret string) (*Provider, error)`, `(*Provider).Handler() http.Handler`
  - `Serve(t testing.TB) *Provider`, which starts httptest, sets Issuer, and registers cleanup
  - `(*Provider).Login(t testing.TB, authURL string) (code, state string)`, which follows `/authorize` for `p.User` and returns the redirect's query values

- [ ] **Step 1: Dependencies**

Run: `go get github.com/coreos/go-oidc/v3@v3.12.0 golang.org/x/oauth2@v0.24.0 github.com/go-jose/go-jose/v4@v4.0.5 && go mod tidy`
Then run `grep '^go ' go.mod`. Expected: still `go 1.22`. If tidy raised it, the pins above are wrong; stop and report.

- [ ] **Step 2: Fake IdP**

`internal/console/oidctest/oidctest.go`:

```go
// Package oidctest is a fake OpenID Connect provider for tests: discovery,
// JWKS, an authorize endpoint and a token endpoint with PKCE, signing RS256
// ID tokens. Tamper makes the next tokens wrong in one specific way.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Tamper selects a defect in issued ID tokens.
type Tamper int

const (
	None Tamper = iota
	WrongNonce
	WrongAudience
	Expired
	BadSignature
)

type grant struct {
	user, nonce, challenge, redirect string
}

// Provider is the fake IdP. Set User to auto-approve /authorize as that
// user; leave it empty to get a sign-in form (the Playwright suite fills
// it). Claims are merged into every ID token; a nil value removes a claim.
type Provider struct {
	Issuer, ClientID, ClientSecret string

	mu     sync.Mutex
	User   string
	Claims map[string]any
	Tamper Tamper
	// FailToken makes the token endpoint answer 500.
	FailToken bool

	key, wrongKey *rsa.PrivateKey
	codes         map[string]grant
}

// New builds a provider for the given issuer URL.
func New(issuer, clientID, clientSecret string) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	wrong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Provider{Issuer: issuer, ClientID: clientID, ClientSecret: clientSecret,
		key: key, wrongKey: wrong, codes: map[string]grant{}}, nil
}

// Serve starts a provider on httptest for one test.
func Serve(t testing.TB) *Provider {
	t.Helper()
	p, err := New("", "console", "secret")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	p.Issuer = srv.URL
	return p
}

// Set changes provider behavior under its lock.
func (p *Provider) Set(fn func(p *Provider)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p)
}

// Handler serves the provider's endpoints.
func (p *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	return mux
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.Issuer,
		"authorization_endpoint":                p.Issuer + "/authorize",
		"token_endpoint":                        p.Issuer + "/token",
		"jwks_uri":                              p.Issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig",
	}}})
}

var form = template.Must(template.New("f").Parse(`<!doctype html><title>Fake IdP</title>
<form method="post"><label>Email <input name="user" type="email" required></label>
{{range $k, $v := .}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<button>Sign in</button></form>`))

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	q := r.Form
	p.mu.Lock()
	user := p.User
	p.mu.Unlock()
	if r.Method == http.MethodPost {
		user = q.Get("user")
	}
	if user == "" {
		params := url.Values{}
		for _, k := range []string{"client_id", "redirect_uri", "state", "nonce", "code_challenge", "code_challenge_method"} {
			params.Set(k, q.Get(k))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = form.Execute(w, params)
		return
	}
	if q.Get("client_id") != p.ClientID || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	code := randomString()
	p.mu.Lock()
	p.codes[code] = grant{user: user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	p.mu.Unlock()
	back, _ := url.Parse(q.Get("redirect_uri"))
	v := back.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	p.mu.Lock()
	g, found := p.codes[r.Form.Get("code")]
	delete(p.codes, r.Form.Get("code"))
	fail, tamper, extra := p.FailToken, p.Tamper, p.Claims
	p.mu.Unlock()
	if fail {
		http.Error(w, "token endpoint down", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if id != p.ClientID || secret != p.ClientSecret || !found ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge ||
		r.Form.Get("redirect_uri") != g.redirect {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.Issuer, "sub": g.user, "aud": p.ClientID, "iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(), "nonce": g.nonce,
		"email": g.user, "email_verified": true,
	}
	for k, v := range extra {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	signWith := p.key
	switch tamper {
	case WrongNonce:
		claims["nonce"] = "not-the-nonce"
	case WrongAudience:
		claims["aud"] = "someone-else"
	case Expired:
		claims["exp"] = now.Add(-time.Hour).Unix()
	case BadSignature:
		signWith = p.wrongKey
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signWith},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": randomString(), "token_type": "Bearer", "expires_in": 300, "id_token": raw})
}

// Login drives /authorize for p.User without a browser and returns the
// code and state the IdP would send back to the client.
func (p *Provider) Login(t testing.TB, authURL string) (code, state string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: %d %v", resp.StatusCode, err)
	}
	return loc.Query().Get("code"), loc.Query().Get("state")
}

func randomString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(fmt.Sprintf("oidctest: encoding: %v", err))
	}
}
```

`internal/console/oidctest/cmd/fakeidp/main.go`:

```go
// Command fakeidp runs the oidctest provider for the console's end-to-end
// suite. It is a test fixture: never deploy it.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/acme/agent-wrapper/internal/console/oidctest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9400", "listen address")
	clientID := flag.String("client-id", "console", "OIDC client id")
	secret := flag.String("client-secret", "secret", "OIDC client secret")
	flag.Parse()
	p, err := oidctest.New("http://"+*addr, *clientID, *secret)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fake IdP at http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, p.Handler()))
}
```

- [ ] **Step 3: Failing sso tests**

`internal/console/sso/sso_test.go`:

```go
package sso_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/acme/agent-wrapper/internal/console/oidctest"
	"github.com/acme/agent-wrapper/internal/console/sso"
)

func client(p *oidctest.Provider, claim string) *sso.Client {
	return sso.New(sso.Config{Issuer: p.Issuer, ClientID: p.ClientID, ClientSecret: p.ClientSecret,
		RedirectURL: "http://127.0.0.1:1/console/auth/callback", UserClaim: claim})
}

func login(t *testing.T, p *oidctest.Provider, c *sso.Client) (string, error) {
	t.Helper()
	attempt, authURL, err := c.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	u, _ := url.Parse(authURL)
	if u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") != attempt.Nonce {
		t.Fatalf("auth URL lacks PKCE or nonce: %s", authURL)
	}
	code, state := p.Login(t, authURL)
	if state != attempt.State {
		t.Fatalf("state %q, want %q", state, attempt.State)
	}
	return c.Finish(context.Background(), attempt, code)
}

func TestLoginReturnsTheConfiguredClaim(t *testing.T) {
	p := oidctest.Serve(t)
	p.Set(func(p *oidctest.Provider) {
		p.User = "alice@example.com"
		p.Claims = map[string]any{"preferred_username": "alice@corp"}
	})
	if user, err := login(t, p, client(p, "")); err != nil || user != "alice@example.com" {
		t.Fatalf("default claim → %q, %v", user, err)
	}
	if user, err := login(t, p, client(p, "preferred_username")); err != nil || user != "alice@corp" {
		t.Fatalf("preferred_username → %q, %v", user, err)
	}
}

func TestLoginRejectsBadTokens(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(p *oidctest.Provider)
		want   error
	}{
		{"wrong nonce", func(p *oidctest.Provider) { p.Tamper = oidctest.WrongNonce }, sso.ErrInvalidToken},
		{"wrong audience", func(p *oidctest.Provider) { p.Tamper = oidctest.WrongAudience }, sso.ErrInvalidToken},
		{"expired", func(p *oidctest.Provider) { p.Tamper = oidctest.Expired }, sso.ErrInvalidToken},
		{"bad signature", func(p *oidctest.Provider) { p.Tamper = oidctest.BadSignature }, sso.ErrInvalidToken},
		{"email not verified", func(p *oidctest.Provider) { p.Claims = map[string]any{"email_verified": false} }, sso.ErrUnverified},
		{"email_verified as string false", func(p *oidctest.Provider) { p.Claims = map[string]any{"email_verified": "false"} }, sso.ErrUnverified},
		{"no user claim", func(p *oidctest.Provider) { p.Claims = map[string]any{"email": nil} }, sso.ErrNoUser},
		{"blank user claim", func(p *oidctest.Provider) { p.Claims = map[string]any{"email": "  "} }, sso.ErrNoUser},
		{"token endpoint fails", func(p *oidctest.Provider) { p.FailToken = true }, sso.ErrExchange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := oidctest.Serve(t)
			p.Set(func(p *oidctest.Provider) { p.User = "alice@example.com"; tc.setup(p) })
			if _, err := login(t, p, client(p, "")); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMissingEmailVerifiedIsAccepted(t *testing.T) {
	p := oidctest.Serve(t)
	p.Set(func(p *oidctest.Provider) { p.User = "a@x"; p.Claims = map[string]any{"email_verified": nil} })
	if _, err := login(t, p, client(p, "")); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoveryFailureIsUnavailableAndRetried(t *testing.T) {
	p := oidctest.Serve(t)
	c := sso.New(sso.Config{Issuer: "http://127.0.0.1:1", ClientID: "x", ClientSecret: "y", RedirectURL: "http://127.0.0.1:2/cb"})
	if _, _, err := c.Start(context.Background()); !errors.Is(err, sso.ErrUnavailable) {
		t.Fatalf("Start = %v, want ErrUnavailable", err)
	}
	// Same client, working issuer: discovery is attempted again, not cached as failed.
	c = client(p, "")
	if err := c.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptRoundTrip(t *testing.T) {
	a := sso.Attempt{State: "s", Nonce: "n", Verifier: "v"}
	got, ok := sso.DecodeAttempt(a.Encode())
	if !ok || got != a {
		t.Fatalf("round trip = %+v, %v", got, ok)
	}
	for _, bad := range []string{"", "a.b", "a..c", "a.b.c.d"} {
		if _, ok := sso.DecodeAttempt(bad); ok {
			t.Errorf("DecodeAttempt(%q) ok", bad)
		}
	}
}
```

To check that discovery is retried, add a variant of `TestDiscoveryFailureIsUnavailableAndRetried` to `sso_test.go`. It starts an `httptest` server that answers 503 on the first discovery request and proxies to `p.Handler()` afterwards, then asserts that `Start` fails once and succeeds on the second call with the **same** `*sso.Client`.

- [ ] **Step 4: Run to verify failure**

Run: `go test ./internal/console/...`
Expected: FAIL. Package `sso` does not exist.

- [ ] **Step 5: Implement `sso`**

`internal/console/sso/sso.go`:

```go
// Package sso signs console users in with OpenID Connect: authorization
// code flow with PKCE, awd as a confidential client. It knows nothing of
// cookies or sessions; the handler stores the Attempt between Start and
// Finish.
package sso

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrUnavailable  = errors.New("sso: identity provider unavailable")
	ErrExchange     = errors.New("sso: code exchange failed")
	ErrInvalidToken = errors.New("sso: invalid ID token")
	ErrUnverified   = errors.New("sso: email not verified")
	ErrNoUser       = errors.New("sso: user claim missing")
)

// Config configures a Client.
type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
	// UserClaim names the claim that identifies the user; default "email".
	UserClaim string
	// HTTPClient reaches the IdP; nil means a client with a 10 s timeout.
	HTTPClient *http.Client
}

// Attempt is one login in flight: what Finish needs to check the IdP's
// answer. All three values are random base64url, so "." cannot occur in
// them.
type Attempt struct{ State, Nonce, Verifier string }

// Encode packs an Attempt into a cookie value.
func (a Attempt) Encode() string { return a.State + "." + a.Nonce + "." + a.Verifier }

// DecodeAttempt unpacks Encode's output.
func DecodeAttempt(s string) (Attempt, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Attempt{}, false
	}
	return Attempt{State: parts[0], Nonce: parts[1], Verifier: parts[2]}, true
}

// Client talks to one IdP. Discovery is lazy and retried until it succeeds,
// so an IdP that is down when awd starts does not need a restart.
type Client struct {
	cfg  Config
	http *http.Client

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// New builds a Client without contacting the IdP.
func New(cfg Config) *Client {
	if cfg.UserClaim == "" {
		cfg.UserClaim = "email"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{cfg: cfg, http: hc}
}

// Discover fetches the IdP's configuration if it has not been fetched.
func (c *Client) Discover(ctx context.Context) error {
	_, _, err := c.discovered(ctx)
	return err
}

func (c *Client) discovered(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.oauth != nil {
		return c.oauth, c.verifier, nil
	}
	// The provider keeps this context for later JWKS fetches, so it must
	// not be the request's: WithoutCancel keeps the client, drops the
	// deadline.
	pctx := oidc.ClientContext(context.WithoutCancel(ctx), c.http)
	provider, err := oidc.NewProvider(pctx, c.cfg.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	c.oauth = &oauth2.Config{
		ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret,
		RedirectURL: c.cfg.RedirectURL, Endpoint: provider.Endpoint(),
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	c.verifier = provider.Verifier(&oidc.Config{ClientID: c.cfg.ClientID})
	return c.oauth, c.verifier, nil
}

// Start begins a login and returns the IdP URL to redirect to.
func (c *Client) Start(ctx context.Context) (Attempt, string, error) {
	oc, _, err := c.discovered(ctx)
	if err != nil {
		return Attempt{}, "", err
	}
	a := Attempt{State: random(), Nonce: random(), Verifier: oauth2.GenerateVerifier()}
	url := oc.AuthCodeURL(a.State, oidc.Nonce(a.Nonce), oauth2.S256ChallengeOption(a.Verifier))
	return a, url, nil
}

// Finish exchanges the code, verifies the ID token against the attempt and
// returns the user named by the configured claim.
func (c *Client) Finish(ctx context.Context, a Attempt, code string) (string, error) {
	oc, verifier, err := c.discovered(ctx)
	if err != nil {
		return "", err
	}
	ctx = oidc.ClientContext(ctx, c.http)
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(a.Verifier))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExchange, err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", fmt.Errorf("%w: no id_token in the token response", ErrInvalidToken)
	}
	idt, err := verifier.Verify(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(a.Nonce)) != 1 {
		return "", fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if v, present := claims["email_verified"]; present && !truthy(v) {
		return "", ErrUnverified
	}
	user, _ := claims[c.cfg.UserClaim].(string)
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("%w: %q", ErrNoUser, c.cfg.UserClaim)
	}
	return user, nil
}

// truthy accepts the boolean true and the string "true": some IdPs send
// email_verified as a string.
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("sso: reading random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/console/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/console
git commit -m "feat(console): OIDC client with PKCE and a fake IdP for tests"
```

---

### Task 4: `session` and `authz` packages

**Files:**
- Create: `internal/console/session/session.go`, `session_test.go`, `internal/console/authz/authz.go`, `authz_test.go`

**Interfaces:**
- Consumes: `store.ConsoleStore`; `store.Store` methods `SCIMGroupsFor`, `CurrentGroupSnapshot`, `SCIMCounts`
- Produces (`session`):
  - `type Manager struct{ Store store.ConsoleStore; Now func() time.Time; Absolute, Idle time.Duration }`
  - `NewManager(s store.ConsoleStore) *Manager` (8 h, 1 h, `time.Now`)
  - `(*Manager).Create(ctx, user) (token string, s model.ConsoleSession, err error)`
  - `(*Manager).Lookup(ctx, token) (model.ConsoleSession, error)`, returning `ErrNoSession` or `ErrExpired`
  - `(*Manager).Delete(ctx, token) error`, `(*Manager).DeleteUser(ctx, user) error`
  - `(*Manager).ExpiresAt(model.ConsoleSession) time.Time`, `(*Manager).Sweep(ctx) (int, error)`
  - `session.Hash(token string) string`
- Produces (`authz`):
  - `type Groups interface{ SCIMGroupsFor(ctx, string) ([]string, error); CurrentGroupSnapshot(ctx) (model.GroupSnapshot, error); SCIMCounts(ctx) (model.SCIMCounts, error) }`
  - `type Checker struct{ Store Groups; Group string }`, `(*Checker).Admin(ctx, user) (bool, error)`, `ErrNoGroupData`

- [ ] **Step 1: Failing tests**

`internal/console/session/session_test.go`:

```go
package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/store"
)

func manager(now *time.Time) (*session.Manager, *store.Memory) {
	s := store.NewMemory()
	m := session.NewManager(s)
	m.Now = func() time.Time { return *now }
	return m, s
}

var t0 = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

func TestCreateStoresOnlyTheHash(t *testing.T) {
	now := t0
	m, s := manager(&now)
	token, sess, err := m.Create(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 { // 32 bytes, base64url, no padding
		t.Fatalf("token length %d", len(token))
	}
	if sess.TokenHash == token || sess.TokenHash != session.Hash(token) {
		t.Fatal("stored hash is the token or not its hash")
	}
	if _, err := s.SessionByHash(context.Background(), token); err == nil {
		t.Fatal("session findable by the raw token")
	}
}

func TestLookupBoundaries(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	ctx := context.Background()
	token, _, _ := m.Create(ctx, "alice")

	// Active every 59 minutes: idle never trips; absolute does at 8 h.
	for i := 1; i <= 8; i++ {
		now = t0.Add(time.Duration(i) * 59 * time.Minute)
		if _, err := m.Lookup(ctx, token); err != nil {
			t.Fatalf("at %v: %v", now.Sub(t0), err)
		}
	}
	now = t0.Add(8*time.Hour + time.Second)
	if _, err := m.Lookup(ctx, token); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("past absolute = %v, want ErrExpired", err)
	}
	if _, err := m.Lookup(ctx, token); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("expired session not deleted: %v", err)
	}
}

func TestIdleExpiry(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	ctx := context.Background()
	token, _, _ := m.Create(ctx, "alice")
	now = t0.Add(time.Hour)
	if _, err := m.Lookup(ctx, token); err != nil {
		t.Fatalf("exactly 1 h idle is still valid: %v", err)
	}
	now = now.Add(time.Hour + time.Second)
	if _, err := m.Lookup(ctx, token); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("idle = %v, want ErrExpired", err)
	}
}

func TestTouchIsThrottledToOncePerMinute(t *testing.T) {
	now := t0
	m, s := manager(&now)
	ctx := context.Background()
	token, sess, _ := m.Create(ctx, "alice")
	now = t0.Add(30 * time.Second)
	_, _ = m.Lookup(ctx, token)
	got, _ := s.SessionByHash(ctx, sess.TokenHash)
	if !got.LastSeenAt.Equal(t0) {
		t.Fatalf("touched after 30 s: %v", got.LastSeenAt)
	}
	now = t0.Add(61 * time.Second)
	_, _ = m.Lookup(ctx, token)
	got, _ = s.SessionByHash(ctx, sess.TokenHash)
	if !got.LastSeenAt.Equal(now) {
		t.Fatalf("not touched after 61 s: %v", got.LastSeenAt)
	}
}

func TestExpiresAtIsTheEarlierBound(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	_, sess, _ := m.Create(context.Background(), "alice")
	if got := m.ExpiresAt(sess); !got.Equal(t0.Add(time.Hour)) {
		t.Fatalf("fresh session expires %v, want idle bound", got)
	}
	sess.LastSeenAt = t0.Add(7*time.Hour + 30*time.Minute)
	if got := m.ExpiresAt(sess); !got.Equal(t0.Add(8 * time.Hour)) {
		t.Fatalf("late session expires %v, want absolute bound", got)
	}
}

func TestUnknownTokenIsNoSession(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	if _, err := m.Lookup(context.Background(), "garbage"); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("err = %v", err)
	}
}
```

`internal/console/authz/authz_test.go`:

```go
package authz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

var t0 = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

func audit() model.AuditEvent { return model.AuditEvent{Actor: "token:t", Action: "x"} }

func TestNoGroupData(t *testing.T) {
	c := &authz.Checker{Store: store.NewMemory(), Group: "console-admins"}
	if _, err := c.Admin(context.Background(), "alice"); !errors.Is(err, authz.ErrNoGroupData) {
		t.Fatalf("err = %v, want ErrNoGroupData", err)
	}
}

func TestSnapshotMembership(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	_ = s.PutGroupSnapshot(ctx, model.GroupSnapshot{Source: "t", SyncedAt: t0,
		Members: map[string][]string{"alice": {"console-admins"}, "bob": {"devs"}}}, audit())
	c := &authz.Checker{Store: s, Group: "console-admins"}
	for user, want := range map[string]bool{"alice": true, "bob": false, "Alice": false, "carol": false} {
		got, err := c.Admin(ctx, user)
		if err != nil || got != want {
			t.Errorf("Admin(%q) = %v, %v; want %v", user, got, err, want)
		}
	}
}

func TestSCIMMembership(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	_ = s.CreateSCIMUser(ctx, model.SCIMUser{ID: "u1", UserName: "alice", Active: true, Created: t0, Modified: t0})
	_ = s.CreateSCIMUser(ctx, model.SCIMUser{ID: "u2", UserName: "gone", Active: false, Created: t0, Modified: t0})
	_ = s.CreateSCIMGroup(ctx, model.SCIMGroup{ID: "g1", DisplayName: "console-admins", Members: []string{"u1", "u2"}, Created: t0, Modified: t0})
	c := &authz.Checker{Store: s, Group: "console-admins"}
	if ok, err := c.Admin(ctx, "alice"); !ok || err != nil {
		t.Fatalf("alice = %v, %v", ok, err)
	}
	if ok, err := c.Admin(ctx, "gone"); ok || err != nil {
		t.Fatalf("deactivated user admitted: %v, %v", ok, err)
	}
}

// Authored groups live in the policy an admin edits; counting them would let
// a revision grant its own author console access.
func TestAuthoredGroupsNeverCount(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	_ = s.PutGroupSnapshot(ctx, model.GroupSnapshot{Source: "t", SyncedAt: t0, Members: map[string][]string{"bob": {"devs"}}}, audit())
	_ = s.PutRuleSet(ctx, model.Revision{Version: "v1", RuleSet: policy.RuleSet{Version: "v1",
		Groups: map[string][]string{"console-admins": {"mallory"}}}}, audit())
	c := &authz.Checker{Store: s, Group: "console-admins"}
	if ok, err := c.Admin(ctx, "mallory"); ok || err != nil {
		t.Fatalf("authored membership admitted: %v, %v", ok, err)
	}
}
```

Before writing `TestAuthoredGroupsNeverCount`, check how `policy.RuleSet.Groups` is shaped (`group → members` or `user → groups`) and write the literal to match: `grep -n "Groups" internal/policy/compile.go`. Check the `model.SCIMGroup.Members` element type the same way; it may be a struct rather than a string.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/console/session/ ./internal/console/authz/`
Expected: FAIL. The packages do not exist.

- [ ] **Step 3: Implement**

`internal/console/session/session.go`:

```go
// Package session keeps console sessions on the server. The cookie holds a
// random token; the store holds its hash, so a database read yields nothing
// a browser could present.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

var (
	ErrNoSession = errors.New("session: no such session")
	ErrExpired   = errors.New("session: expired")
)

// touchEvery bounds how often a busy session writes last_seen_at.
const touchEvery = time.Minute

// Manager creates and checks sessions.
type Manager struct {
	Store    store.ConsoleStore
	Now      func() time.Time
	Absolute time.Duration
	Idle     time.Duration
}

// NewManager uses the spec's lifetimes: 8 h absolute, 1 h idle.
func NewManager(s store.ConsoleStore) *Manager {
	return &Manager{Store: s, Now: time.Now, Absolute: 8 * time.Hour, Idle: time.Hour}
}

// Hash is what the store keys a session by.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create starts a session for user and returns the cookie token.
func (m *Manager) Create(ctx context.Context, user string) (string, model.ConsoleSession, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", model.ConsoleSession{}, fmt.Errorf("session: random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	now := m.Now()
	s := model.ConsoleSession{TokenHash: Hash(token), User: user, CreatedAt: now, LastSeenAt: now}
	if err := m.Store.CreateSession(ctx, s); err != nil {
		return "", model.ConsoleSession{}, err
	}
	return token, s, nil
}

// ExpiresAt is the earlier of the absolute and idle bounds.
func (m *Manager) ExpiresAt(s model.ConsoleSession) time.Time {
	abs := s.CreatedAt.Add(m.Absolute)
	idle := s.LastSeenAt.Add(m.Idle)
	if idle.Before(abs) {
		return idle
	}
	return abs
}

// Lookup returns the live session for token, deleting it when expired.
func (m *Manager) Lookup(ctx context.Context, token string) (model.ConsoleSession, error) {
	hash := Hash(token)
	s, err := m.Store.SessionByHash(ctx, hash)
	if errors.Is(err, model.ErrNotFound) {
		return model.ConsoleSession{}, ErrNoSession
	}
	if err != nil {
		return model.ConsoleSession{}, err
	}
	now := m.Now()
	if now.After(m.ExpiresAt(s)) {
		if err := m.Store.DeleteSession(ctx, hash); err != nil {
			return model.ConsoleSession{}, err
		}
		return model.ConsoleSession{}, ErrExpired
	}
	if now.Sub(s.LastSeenAt) >= touchEvery {
		if err := m.Store.TouchSession(ctx, hash, now); err != nil && !errors.Is(err, model.ErrNotFound) {
			return model.ConsoleSession{}, err
		}
		s.LastSeenAt = now
	}
	return s, nil
}

// Delete ends one session.
func (m *Manager) Delete(ctx context.Context, token string) error {
	return m.Store.DeleteSession(ctx, Hash(token))
}

// DeleteUser ends every session of user.
func (m *Manager) DeleteUser(ctx context.Context, user string) error {
	return m.Store.DeleteSessionsFor(ctx, user)
}

// Sweep deletes every expired session.
func (m *Manager) Sweep(ctx context.Context) (int, error) {
	now := m.Now()
	return m.Store.DeleteExpiredSessions(ctx, now.Add(-m.Absolute), now.Add(-m.Idle))
}
```

`internal/console/authz/authz.go`:

```go
// Package authz decides who may use the console: members of one group, as
// the identity provider says. It reads SCIM and the applied snapshot, never
// the policy's own authored groups, which an admin could edit to promote
// themselves.
package authz

import (
	"context"
	"errors"

	"github.com/acme/agent-wrapper/internal/model"
)

// ErrNoGroupData means awd has no IdP group data at all, so no one can be
// shown to be an admin.
var ErrNoGroupData = errors.New("console needs group data (SCIM or awd groups apply)")

// Groups is the part of the store authz reads.
type Groups interface {
	SCIMGroupsFor(ctx context.Context, userName string) ([]string, error)
	CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error)
	SCIMCounts(ctx context.Context) (model.SCIMCounts, error)
}

// Checker answers "is this user a console admin".
type Checker struct {
	Store Groups
	Group string
}

// Admin reports whether user is in the admin group. Matching is exact, as
// bundle resolution is.
func (c *Checker) Admin(ctx context.Context, user string) (bool, error) {
	snapshot, err := c.Store.CurrentGroupSnapshot(ctx)
	hasSnapshot := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return false, err
	}
	counts, err := c.Store.SCIMCounts(ctx)
	if err != nil {
		return false, err
	}
	if !hasSnapshot && counts.Groups == 0 {
		return false, ErrNoGroupData
	}
	if hasSnapshot {
		for _, g := range snapshot.Members[user] {
			if g == c.Group {
				return true, nil
			}
		}
	}
	scim, err := c.Store.SCIMGroupsFor(ctx, user)
	if err != nil {
		return false, err
	}
	for _, g := range scim {
		if g == c.Group {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/console/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/console/session internal/console/authz
git commit -m "feat(console): server-side sessions and IdP-group admin check"
```

---

### Task 5: Console routes, middleware, and static files

**Files:**
- Create: `internal/console/assets.go`, `internal/console/dist/index.html` (placeholder), `internal/handler/console.go`, `internal/handler/console_auth.go`, `internal/handler/console_test.go`
- Modify: `internal/handler/auth.go`, `internal/handler/handler.go`

**Interfaces:**
- Consumes: `sso.Client`, `session.Manager`, `authz.Checker`, `store.RecordAudit`, `withActor`
- Produces:
  - `handler.Console{PublicURL *url.URL; SSO *sso.Client; Sessions *session.Manager; Authz *authz.Checker; Assets fs.FS}`
  - `Handler.Console *Console`
  - `console.Assets() fs.FS`
  - routes `GET /console/`, `GET /console/auth/login`, `GET /console/auth/callback`, `POST /console/auth/logout`, `GET /console/api/me` (→ `{"user","expiresAt","signingKeyId"}`), `GET /console/api/` (→ 404 JSON)

- [ ] **Step 1: Embedded assets with a placeholder**

`internal/console/assets.go`:

```go
// Package console holds the admin console's built single-page app. The
// sources are in /console at the repository root; `pnpm build` there writes
// dist/, which is committed so `go build` needs no Node.
package console

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets is the built SPA, rooted at dist/.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("console: dist missing from the build: " + err.Error())
	}
	return sub
}
```

`internal/console/dist/index.html`:

```html
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Agentic Warden console</title></head>
<body><div id="root">Console assets not built. Run pnpm build in console/.</div></body></html>
```

- [ ] **Step 2: Failing tests: one per failure-mode row**

`internal/handler/console_test.go` contains a harness and the tests.

The harness:

```go
package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/oidctest"
	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

type consoleEnv struct {
	srv      *httptest.Server
	idp      *oidctest.Provider
	store    store.Store
	sessions *session.Manager
	now      *time.Time
	browser  *http.Client // cookie jar, no automatic redirects
}

// newConsole starts awd with the console on, against a fake IdP. members
// seeds the group snapshot; nil leaves awd with no group data.
func newConsole(t *testing.T, members map[string][]string) *consoleEnv {
	t.Helper()
	return newConsoleWithStore(t, store.NewMemory(), members)
}

func newConsoleWithStore(t *testing.T, s store.Store, members map[string][]string) *consoleEnv {
	t.Helper()
	idp := oidctest.Serve(t)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	env := &consoleEnv{idp: idp, store: s, now: &now}
	if members != nil {
		if err := s.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: now, Members: members},
			model.AuditEvent{Actor: "token:seed", Action: model.AuditGroupsApply}); err != nil {
			t.Fatal(err)
		}
	}
	h := handler.New(s, nil)
	h.AdminToken = adminToken
	h.Now = func() time.Time { return *env.now }
	env.sessions = session.NewManager(s)
	env.sessions.Now = h.Now
	srv := httptest.NewUnstartedServer(nil)
	public, _ := url.Parse("http://" + srv.Listener.Addr().String())
	h.Console = &handler.Console{
		PublicURL: public,
		SSO: sso.New(sso.Config{Issuer: idp.Issuer, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret,
			RedirectURL: public.String() + "/console/auth/callback"}),
		Sessions: env.sessions,
		Authz:    &authz.Checker{Store: s, Group: "console-admins"},
		Assets: fstest.MapFS{
			"index.html":         {Data: []byte("<!doctype html><div id=root></div>")},
			"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		},
	}
	srv.Config.Handler = h.Routes()
	srv.Start()
	t.Cleanup(srv.Close)
	env.srv = srv
	jar, _ := cookiejar.New(nil)
	env.browser = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return env
}

// login runs the whole browser flow for user and returns the callback response.
func (e *consoleEnv) login(t *testing.T, user string) *http.Response {
	t.Helper()
	e.idp.Set(func(p *oidctest.Provider) { p.User = user })
	resp := e.do(t, "GET", "/console/auth/login", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	code, state := e.idp.Login(t, resp.Header.Get("Location"))
	return e.do(t, "GET", "/console/auth/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
}

// do sends a request through the browser; header adds or overrides headers.
func (e *consoleEnv) do(t *testing.T, method, path string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := e.browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *consoleEnv) origin() http.Header { return http.Header{"Origin": {e.srv.URL}} }

func (e *consoleEnv) auditActions(t *testing.T) []model.AuditEvent {
	t.Helper()
	events, err := e.store.AuditEvents(context.Background(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

var admins = map[string][]string{"alice@example.com": {"console-admins"}, "bob@example.com": {"devs"}}
```

The tests, each named for its failure-mode row (write every one in full in `console_test.go`):

```go
func TestConsoleOffAnswers404(t *testing.T) {
	srv := newServer(t) // no Console
	for _, p := range []string{"/console/", "/console/auth/login", "/console/api/me"} {
		if resp := get(t, srv, p, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, resp.StatusCode)
		}
	}
	if resp := getAs(t, srv, "/v1/machines", adminToken); resp.StatusCode != http.StatusOK {
		t.Errorf("token path broken: %d", resp.StatusCode)
	}
}

func TestConsoleLoginHappyPath(t *testing.T) {
	e := newConsole(t, admins)
	resp := e.login(t, "alice@example.com")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/console/" {
		t.Fatalf("callback = %d → %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "aw_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode ||
		sessionCookie.Path != "/" || sessionCookie.MaxAge != 0 || sessionCookie.Secure {
		t.Fatalf("session cookie = %+v (loopback http: not Secure)", sessionCookie)
	}
	me := e.do(t, "GET", "/console/api/me", nil)
	body, _ := io.ReadAll(me.Body)
	if me.StatusCode != http.StatusOK || !strings.Contains(string(body), `"user":"alice@example.com"`) {
		t.Fatalf("me = %d %s", me.StatusCode, body)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLogin || ev[0].Actor != "alice@example.com" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

func TestConsoleSessionCanUseAdminAPIAndIsTheActor(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/policy/revisions", strings.NewReader(`{"version":"v1"}`))
	req.Header.Set("Origin", e.srv.URL)
	resp, err := e.browser.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST as console = %v %v", resp.StatusCode, err)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditRevisionCreate || ev[0].Actor != "alice@example.com" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

func TestConsoleIdPDownAtLoginIs503ThenRecovers(t *testing.T) { /* build Console with an SSO client pointed at a
	stub server that answers 503 to discovery once, then proxies to the fake IdP; GET /console/auth/login → 503;
	again → 302. */ }

func TestConsoleCallbackStateMismatchIs400(t *testing.T) {
	e := newConsole(t, admins)
	e.idp.Set(func(p *oidctest.Provider) { p.User = "alice@example.com" })
	resp := e.do(t, "GET", "/console/auth/login", nil)
	code, _ := e.idp.Login(t, resp.Header.Get("Location"))
	cb := e.do(t, "GET", "/console/auth/callback?code="+code+"&state=forged", nil)
	if cb.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session created anyway: %d", me.StatusCode)
	}
}

func TestConsoleCallbackWithoutLoginCookieIs400(t *testing.T) {
	e := newConsole(t, admins)
	if cb := e.do(t, "GET", "/console/auth/callback?code=x&state=y", nil); cb.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", cb.StatusCode)
	}
}

// Review Focus 2.
func TestConsoleCallbackReplayWithSessionRedirects(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	cb := e.do(t, "GET", "/console/auth/callback?code=used&state=stale", nil)
	if cb.StatusCode != http.StatusFound || cb.Header.Get("Location") != "/console/" {
		t.Fatalf("replay = %d → %q", cb.StatusCode, cb.Header.Get("Location"))
	}
}

func TestConsoleCodeExchangeFailureIs502(t *testing.T) {
	e := newConsole(t, admins)
	e.idp.Set(func(p *oidctest.Provider) { p.FailToken = true })
	if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d", cb.StatusCode)
	}
}

func TestConsoleInvalidTokenIs401AndAudited(t *testing.T) {
	for _, tamper := range []oidctest.Tamper{oidctest.WrongNonce, oidctest.WrongAudience, oidctest.Expired, oidctest.BadSignature} {
		e := newConsole(t, admins)
		e.idp.Set(func(p *oidctest.Provider) { p.Tamper = tamper })
		if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusUnauthorized {
			t.Fatalf("tamper %d: status %d", tamper, cb.StatusCode)
		}
		if ev := e.auditActions(t); ev[0].Action != model.AuditLoginDenied || ev[0].Actor != "anonymous" {
			t.Fatalf("tamper %d: audit %+v", tamper, ev[0])
		}
	}
}

func TestConsoleUnverifiedEmailIs403(t *testing.T)   { /* Claims{"email_verified": false} → 403, login_denied */ }
func TestConsoleMissingUserClaimIs403(t *testing.T) { /* Claims{"email": nil} → 403, login_denied */ }

func TestConsoleNoGroupDataIs503(t *testing.T) {
	e := newConsole(t, nil)
	cb := e.login(t, "alice@example.com")
	body, _ := io.ReadAll(cb.Body)
	if cb.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "console needs group data") {
		t.Fatalf("login = %d %s", cb.StatusCode, body)
	}
}

func TestConsoleNonAdminIs403AtLogin(t *testing.T) { /* bob → 403, audit login_denied actor bob, detail reason
	"not in console-admins" */ }

// Review Focus 1.
func TestConsoleLoginCaseMismatchIsDenied(t *testing.T) {
	e := newConsole(t, admins)
	if cb := e.login(t, "Alice@example.com"); cb.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Actor != "Alice@example.com" {
		t.Fatalf("audit actor = %q", ev[0].Actor)
	}
}

func TestConsoleAdminRemovedMidSessionIs403AndSessionsDeleted(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	_ = e.store.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: *e.now,
		Members: map[string][]string{"bob@example.com": {"console-admins"}}}, model.AuditEvent{Actor: "token:x", Action: model.AuditGroupsApply})
	if me := e.do(t, "GET", "/v1/machines", nil); me.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", me.StatusCode)
	}
	// Restoring membership does not revive the session: it was deleted.
	_ = e.store.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: *e.now,
		Members: admins}, model.AuditEvent{Actor: "token:x", Action: model.AuditGroupsApply})
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session survived: %d", me.StatusCode)
	}
}

func TestConsoleExpiredSessionIs401(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	*e.now = e.now.Add(time.Hour + time.Second)
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("idle: %d", me.StatusCode)
	}
}

func TestConsoleWriteNeedsOrigin(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	for _, origin := range []string{"", "https://evil.example"} {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		if resp := e.do(t, "POST", "/v1/enrollment-tokens", h); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: %d", origin, resp.StatusCode)
		}
	}
	if resp := e.do(t, "POST", "/console/auth/logout", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without origin: %d", resp.StatusCode)
	}
	for _, ev := range e.auditActions(t) {
		if ev.Action == model.AuditTokenCreate {
			t.Fatal("token created despite bad origin")
		}
	}
}

func TestConsoleWrongBearerWithValidCookieIs401(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	if resp := e.do(t, "GET", "/v1/machines", http.Header{"Authorization": {"Bearer wrong"}}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestConsoleStoreDownIs503(t *testing.T) {
	/* newConsoleWithStore with a wrapper whose SessionByHash returns errors.New("db down");
	   GET /console/api/me with any aw_session cookie → 503. */
}

func TestConsoleLoginAuditFailureIs500AndNoSession(t *testing.T) {
	/* wrapper whose RecordAudit fails → callback 500; me → 401 (session was removed). */
}

func TestConsoleLogout(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	if resp := e.do(t, "POST", "/console/auth/logout", e.origin()); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout %d", resp.StatusCode)
	}
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout %d", me.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLogout {
		t.Fatalf("audit %+v", ev[0])
	}
}

func TestConsoleStaticFiles(t *testing.T) {
	e := newConsole(t, admins)
	idx := e.do(t, "GET", "/console/policy/revisions/3", nil)
	body, _ := io.ReadAll(idx.Body)
	if idx.StatusCode != 200 || !strings.Contains(string(body), "id=root") || idx.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("deep link = %d %q %q", idx.StatusCode, idx.Header.Get("Cache-Control"), body)
	}
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	if idx.Header.Get("Content-Security-Policy") != csp || idx.Header.Get("X-Content-Type-Options") != "nosniff" ||
		idx.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers = %v", idx.Header)
	}
	asset := e.do(t, "GET", "/console/assets/app-abc.js", nil)
	if asset.StatusCode != 200 || asset.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset = %d %q", asset.StatusCode, asset.Header.Get("Cache-Control"))
	}
	if missing := e.do(t, "GET", "/console/assets/nope.js", nil); missing.StatusCode != 404 {
		t.Fatalf("missing asset = %d", missing.StatusCode)
	}
	if api := e.do(t, "GET", "/console/api/nope", nil); api.StatusCode != 404 {
		t.Fatalf("unknown api = %d", api.StatusCode)
	}
	if bare := e.do(t, "GET", "/console", nil); bare.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("/console = %d", bare.StatusCode)
	}
}
```

The tests whose bodies are shown as `/* ... */` comments must be written out in full, following the pattern of their neighbors. The comment in each states exactly what the test sets up and asserts. Where a test needs a failing store, build a `failingStore` type that embeds `store.Store` and has func fields (`sessionByHash`, `recordAudit`) that override when non-nil.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/handler/ -run Console`
Expected: FAIL. `handler.Console` is undefined.

- [ ] **Step 4: Implement `console.go`**

`internal/handler/console.go`:

```go
package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/model"
)

const (
	sessionCookie = "aw_session"
	loginCookie   = "aw_login"
	consoleCSP    = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
)

// Console is the web console's wiring. A nil Handler.Console means the
// console is off and every /console path is a 404.
type Console struct {
	// PublicURL is awd's external origin: the redirect URI's base and the
	// only Origin accepted on console writes.
	PublicURL *url.URL
	SSO       *sso.Client
	Sessions  *session.Manager
	Authz     *authz.Checker
	// Assets is the built SPA (console.Assets() in production).
	Assets fs.FS
}

func (c *Console) origin() string { return c.PublicURL.Scheme + "://" + c.PublicURL.Host }

// secure reports whether cookies carry Secure: always, except plain http
// on loopback for development.
func (c *Console) secure() bool { return c.PublicURL.Scheme == "https" }

func (h *Handler) consoleRoutes(mux *http.ServeMux) {
	if h.Console == nil {
		return
	}
	mux.Handle("GET /console/", h.consoleStatic())
	mux.HandleFunc("GET /console/auth/login", h.consoleLogin)
	mux.HandleFunc("GET /console/auth/callback", h.consoleCallback)
	mux.HandleFunc("POST /console/auth/logout", h.consoleLogout)
	mux.HandleFunc("GET /console/api/me", h.consoleMe)
	mux.HandleFunc("GET /console/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
}

func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", consoleCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// consoleStatic serves hashed assets as immutable and every other path as
// index.html, so the SPA's client-side routes survive a reload.
func (h *Handler) consoleStatic() http.Handler {
	files := http.StripPrefix("/console/", http.FileServerFS(h.Console.Assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		name := strings.TrimPrefix(r.URL.Path, "/console/")
		if strings.HasPrefix(name, "assets/") {
			if _, err := fs.Stat(h.Console.Assets, name); err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(h.Console.Assets, "index.html")
		if err != nil {
			h.fail(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

// consoleUser authenticates a console request and re-checks admin rights.
// On failure it has written the response and returns ok=false.
func (h *Handler) consoleUser(w http.ResponseWriter, r *http.Request) (model.ConsoleSession, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		writeError(w, http.StatusUnauthorized, "sign in")
		return model.ConsoleSession{}, false
	}
	s, err := h.Console.Sessions.Lookup(r.Context(), c.Value)
	switch {
	case errors.Is(err, session.ErrNoSession), errors.Is(err, session.ErrExpired):
		h.clearCookie(w, sessionCookie, "/")
		writeError(w, http.StatusUnauthorized, "sign in")
		return model.ConsoleSession{}, false
	case err != nil:
		h.log.ErrorContext(r.Context(), "console session lookup failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "session store unavailable")
		return model.ConsoleSession{}, false
	}
	admin, err := h.Console.Authz.Admin(r.Context(), s.User)
	switch {
	case errors.Is(err, authz.ErrNoGroupData):
		writeError(w, http.StatusServiceUnavailable, authz.ErrNoGroupData.Error())
		return model.ConsoleSession{}, false
	case err != nil:
		h.log.ErrorContext(r.Context(), "console admin check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "group data unavailable")
		return model.ConsoleSession{}, false
	case !admin:
		if err := h.Console.Sessions.DeleteUser(r.Context(), s.User); err != nil {
			h.log.ErrorContext(r.Context(), "deleting sessions of a removed admin", "err", err)
		}
		h.clearCookie(w, sessionCookie, "/")
		writeError(w, http.StatusForbidden, "not a console admin")
		return model.ConsoleSession{}, false
	}
	return s, true
}

// sameOrigin guards console writes. Browsers send Origin on every
// non-GET fetch; a missing one means the request did not come from the SPA.
func (h *Handler) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if r.Header.Get("Origin") != h.Console.origin() {
		writeError(w, http.StatusForbidden, "cross-origin request refused")
		return false
	}
	return true
}

// consoleAdmin is requireAdmin's console path.
func (h *Handler) consoleAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := h.consoleUser(w, r)
		if !ok || !h.sameOrigin(w, r) {
			return
		}
		next(w, withActor(r, s.User))
	}
}

func (h *Handler) consoleMe(w http.ResponseWriter, r *http.Request) {
	s, ok := h.consoleUser(w, r)
	if !ok {
		return
	}
	body := map[string]any{"user": s.User, "expiresAt": h.Console.Sessions.ExpiresAt(s).UTC().Format(time.RFC3339)}
	if h.Signer != nil {
		body["signingKeyId"] = h.Signer.KeyID()
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *Handler) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1,
		HttpOnly: true, Secure: h.Console.secure(), SameSite: http.SameSiteStrictMode})
}
```

In `auth.go`, split the existing body into `tokenAdmin(next)` (the current logic plus `withActor`), and have `requireAdmin` dispatch:

```go
// requireAdmin admits the admin token or, when the console is on, a
// console session. A request carrying Authorization is judged by the token
// alone, so a wrong token is never rescued by a cookie.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	token, console := h.tokenAdmin(next), http.HandlerFunc(nil)
	if h.Console != nil {
		console = h.consoleAdmin(next)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || console == nil {
			token(w, r)
			return
		}
		console(w, r)
	}
}
```

Because `requireAdmin` now reads `h.Console` when routes are built, `Routes()` must be called after `Console` is set. That already holds in `main`, and the test harness sets `Console` before calling `Routes()`. Say so in `Routes`' doc comment. In `Routes`, call `h.consoleRoutes(mux)` after `h.scimRoutes(mux)`.

- [ ] **Step 5: Implement `console_auth.go`**

```go
package handler

import (
	"crypto/subtle"
	"errors"
	"html/template"
	"net/http"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/model"
)

var loginErrorPage = template.Must(template.New("e").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<title>Sign-in failed</title></head><body><main><h1>Sign-in failed</h1><p>{{.}}</p>
<p><a href="/console/">Back to the console</a></p></main></body></html>`))

func (h *Handler) loginError(w http.ResponseWriter, status int, message string) {
	securityHeaders(w)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = loginErrorPage.Execute(w, message)
}

func (h *Handler) consoleLogin(w http.ResponseWriter, r *http.Request) {
	attempt, authURL, err := h.Console.SSO.Start(r.Context())
	if err != nil {
		h.log.WarnContext(r.Context(), "console login: identity provider unavailable", "err", err)
		h.loginError(w, http.StatusServiceUnavailable, "The identity provider is unavailable. Try again shortly.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: attempt.Encode(), Path: "/console/auth",
		MaxAge: 600, HttpOnly: true, Secure: h.Console.secure(), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// denied records a refused login. The audit write's own failure is logged,
// not surfaced: the user is already being refused.
func (h *Handler) denied(r *http.Request, actor, reason string) {
	event := model.AuditEvent{At: h.Now(), Actor: actor, Action: model.AuditLoginDenied, Detail: map[string]any{"reason": reason}}
	if err := h.store.RecordAudit(r.Context(), event); err != nil {
		h.log.ErrorContext(r.Context(), "recording a denied console login", "err", err)
	}
}

func (h *Handler) consoleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cookie, cookieErr := r.Cookie(loginCookie)
	h.clearCookie(w, loginCookie, "/console/auth")
	var attempt sso.Attempt
	ok := cookieErr == nil
	if ok {
		attempt, ok = sso.DecodeAttempt(cookie.Value)
	}
	if !ok || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(attempt.State)) != 1 {
		// Back button onto a used callback: a signed-in user just goes home.
		if c, err := r.Cookie(sessionCookie); err == nil {
			if _, err := h.Console.Sessions.Lookup(r.Context(), c.Value); err == nil {
				http.Redirect(w, r, "/console/", http.StatusFound)
				return
			}
		}
		h.loginError(w, http.StatusBadRequest, "This sign-in link is stale. Start again from the console.")
		return
	}
	if idpErr := q.Get("error"); idpErr != "" {
		h.denied(r, "anonymous", "idp: "+idpErr)
		h.loginError(w, http.StatusUnauthorized, "The identity provider refused the sign-in.")
		return
	}

	user, err := h.Console.SSO.Finish(r.Context(), attempt, q.Get("code"))
	switch {
	case errors.Is(err, sso.ErrUnavailable):
		h.loginError(w, http.StatusServiceUnavailable, "The identity provider is unavailable. Try again shortly.")
		return
	case errors.Is(err, sso.ErrExchange):
		h.log.WarnContext(r.Context(), "console login: code exchange failed", "err", err)
		h.loginError(w, http.StatusBadGateway, "The identity provider did not complete the sign-in.")
		return
	case errors.Is(err, sso.ErrInvalidToken):
		h.log.WarnContext(r.Context(), "console login: invalid ID token", "err", err)
		h.denied(r, "anonymous", err.Error())
		h.loginError(w, http.StatusUnauthorized, "The identity provider's answer could not be verified.")
		return
	case errors.Is(err, sso.ErrUnverified), errors.Is(err, sso.ErrNoUser):
		h.denied(r, "anonymous", err.Error())
		h.loginError(w, http.StatusForbidden, "Your account has no verified identity awd can use.")
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}

	admin, err := h.Console.Authz.Admin(r.Context(), user)
	switch {
	case errors.Is(err, authz.ErrNoGroupData):
		h.loginError(w, http.StatusServiceUnavailable, "The console needs group data (SCIM or awd groups apply).")
		return
	case err != nil:
		h.log.ErrorContext(r.Context(), "console login: admin check failed", "err", err)
		h.loginError(w, http.StatusServiceUnavailable, "Group data is unavailable. Try again shortly.")
		return
	case !admin:
		h.denied(r, user, "not in "+h.Console.Authz.Group)
		h.loginError(w, http.StatusForbidden, "You are not a console admin.")
		return
	}

	token, _, err := h.Console.Sessions.Create(r.Context(), user)
	if err != nil {
		h.log.ErrorContext(r.Context(), "console login: creating session", "err", err)
		h.loginError(w, http.StatusServiceUnavailable, "Sign-in is unavailable. Try again shortly.")
		return
	}
	if err := h.store.RecordAudit(r.Context(), model.AuditEvent{At: h.Now(), Actor: user, Action: model.AuditLogin}); err != nil {
		// No unaudited access: take the session back.
		_ = h.Console.Sessions.Delete(r.Context(), token)
		h.log.ErrorContext(r.Context(), "console login: recording audit", "err", err)
		h.loginError(w, http.StatusInternalServerError, "Sign-in could not be recorded.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: h.Console.secure(), SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/console/", http.StatusFound)
}

func (h *Handler) consoleLogout(w http.ResponseWriter, r *http.Request) {
	if !h.sameOrigin(w, r) {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if s, err := h.Console.Sessions.Lookup(r.Context(), c.Value); err == nil {
			if err := h.store.RecordAudit(r.Context(), model.AuditEvent{At: h.Now(), Actor: s.User, Action: model.AuditLogout}); err != nil {
				h.log.ErrorContext(r.Context(), "recording console logout", "err", err)
			}
		}
		if err := h.Console.Sessions.Delete(r.Context(), c.Value); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.clearCookie(w, sessionCookie, "/")
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/handler/ -v -run Console`, then `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/console/assets.go internal/console/dist internal/handler
git commit -m "feat(awd): console SSO routes, session middleware and static SPA serving"
```

---

### Task 6: Configuration, `awd` wiring, sweeper, README

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `cmd/awd/main.go`, `README.md`

**Interfaces:**
- Produces: `config.Config` fields `PublicURL *url.URL`, `Console ConsoleConfig`; `type ConsoleConfig struct{ Issuer, ClientID, ClientSecretFile, AdminGroup, UserClaim string }`; `(Config).ConsoleEnabled() bool`

- [ ] **Step 1: Failing config tests**

Add to `internal/config/config_test.go`, using `t.Setenv`:

```go
func setConsoleEnv(t *testing.T) {
	t.Setenv("AWD_PUBLIC_URL", "https://awd.example.com")
	t.Setenv("AWD_CONSOLE_ISSUER", "https://idp.example.com")
	t.Setenv("AWD_CONSOLE_CLIENT_ID", "awd")
	t.Setenv("AWD_CONSOLE_CLIENT_SECRET_FILE", "/etc/awd/secret")
	t.Setenv("AWD_CONSOLE_ADMIN_GROUP", "console-admins")
}

func TestConsoleAllUnsetIsOff(t *testing.T) {
	cfg, err := FromEnv()
	if err != nil || cfg.ConsoleEnabled() {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestConsoleAllSet(t *testing.T) {
	setConsoleEnv(t)
	cfg, err := FromEnv()
	if err != nil || !cfg.ConsoleEnabled() || cfg.Console.UserClaim != "email" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestConsolePartlySetNamesTheMissing(t *testing.T) {
	setConsoleEnv(t)
	t.Setenv("AWD_CONSOLE_CLIENT_ID", "")
	t.Setenv("AWD_CONSOLE_ADMIN_GROUP", "")
	_, err := FromEnv()
	if err == nil || !strings.Contains(err.Error(), "AWD_CONSOLE_CLIENT_ID") || !strings.Contains(err.Error(), "AWD_CONSOLE_ADMIN_GROUP") {
		t.Fatalf("err = %v", err)
	}
}

// Review Focus 3.
func TestConsolePublicURLShape(t *testing.T) {
	cases := map[string]string{ // value → "" for OK, else a fragment of the error
		"https://awd.example.com/":      "",
		"http://localhost:8080":         "",
		"http://127.0.0.1:9401":         "",
		"http://awd.example.com":        "https",
		"https://awd.example.com/admin": "path",
		"https://awd.example.com?x=1":   "query",
		"https://awd.example.com#f":     "fragment",
		"awd.example.com":               "https",
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			setConsoleEnv(t)
			t.Setenv("AWD_PUBLIC_URL", value)
			cfg, err := FromEnv()
			if want == "" {
				if err != nil || strings.HasSuffix(cfg.PublicURL.String(), "/") {
					t.Fatalf("cfg=%v err=%v", cfg.PublicURL, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want mention of %q", err, want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/`
Expected: FAIL. `ConsoleEnabled` is undefined.

- [ ] **Step 3: Implement config**

Add to `Config`: `PublicURL *url.URL` and `Console ConsoleConfig`. Add to `FromEnv`, before returning:

```go
	console := []struct {
		key    string
		target *string
	}{
		{"AWD_CONSOLE_ISSUER", &cfg.Console.Issuer},
		{"AWD_CONSOLE_CLIENT_ID", &cfg.Console.ClientID},
		{"AWD_CONSOLE_CLIENT_SECRET_FILE", &cfg.Console.ClientSecretFile},
		{"AWD_CONSOLE_ADMIN_GROUP", &cfg.Console.AdminGroup},
	}
	publicURL := os.Getenv("AWD_PUBLIC_URL")
	var set, missing []string
	if publicURL != "" {
		set = append(set, "AWD_PUBLIC_URL")
	} else {
		missing = append(missing, "AWD_PUBLIC_URL")
	}
	for _, c := range console {
		*c.target = os.Getenv(c.key)
		if *c.target != "" {
			set = append(set, c.key)
		} else {
			missing = append(missing, c.key)
		}
	}
	cfg.Console.UserClaim = env("AWD_CONSOLE_USER_CLAIM", "email")
	if len(set) > 0 && len(missing) > 0 {
		return Config{}, fmt.Errorf("config: the console needs all of its settings; missing %s", strings.Join(missing, ", "))
	}
	if len(set) > 0 {
		u, err := parsePublicURL(publicURL)
		if err != nil {
			return Config{}, err
		}
		cfg.PublicURL = u
	}
```

```go
// ConsoleEnabled reports whether every console setting is present.
func (c Config) ConsoleEnabled() bool { return c.PublicURL != nil }

// parsePublicURL accepts a bare origin: https anywhere, http only on
// loopback. The redirect URI and the Origin check are both built from it,
// so a path, query or fragment would break one of them silently.
func parsePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must be an absolute https URL", raw)
	}
	switch {
	case u.Path != "":
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must not have a path", raw)
	case u.RawQuery != "" || u.ForceQuery:
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must not have a query", raw)
	case u.Fragment != "":
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must not have a fragment", raw)
	}
	host := u.Hostname()
	loopback := host == "localhost" || host == "127.0.0.1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must use https (plain http only on localhost)", raw)
	}
	return u, nil
}
```

The switch order (path, query, fragment) matches the test table: every case there has at most one defect.

- [ ] **Step 4: Wire `awd`**

In `serve()`, after the signer block:

```go
	if cfg.ConsoleEnabled() {
		secret, err := os.ReadFile(cfg.Console.ClientSecretFile)
		if err != nil {
			return fmt.Errorf("awd: AWD_CONSOLE_CLIENT_SECRET_FILE: %w", err)
		}
		clientSecret := strings.TrimSpace(string(secret))
		if clientSecret == "" {
			return fmt.Errorf("awd: AWD_CONSOLE_CLIENT_SECRET_FILE %s is empty", cfg.Console.ClientSecretFile)
		}
		client := sso.New(sso.Config{
			Issuer: cfg.Console.Issuer, ClientID: cfg.Console.ClientID, ClientSecret: clientSecret,
			RedirectURL: cfg.PublicURL.String() + "/console/auth/callback", UserClaim: cfg.Console.UserClaim,
		})
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := client.Discover(dctx); err != nil {
			log.Warn("console: identity provider discovery failed; retrying on the next sign-in", "err", err)
		}
		cancel()
		sessions := session.NewManager(backing)
		h.Console = &handler.Console{
			PublicURL: cfg.PublicURL, SSO: client, Sessions: sessions,
			Authz:  &authz.Checker{Store: backing, Group: cfg.Console.AdminGroup},
			Assets: console.Assets(),
		}
		go sweepSessions(sweepCtx, sessions, log)
		log.Info("admin console enabled", "url", cfg.PublicURL.String()+"/console/")
	} else {
		log.Info("admin console disabled; set AWD_PUBLIC_URL and AWD_CONSOLE_* to enable it")
	}
```

Create `sweepCtx, stopSweep := context.WithCancel(context.Background())` near the top of `serve`, with `defer stopSweep()`, and add:

```go
// sweepSessions deletes expired console sessions hourly. Lookup already
// refuses them; this only keeps the table from growing.
func sweepSessions(ctx context.Context, m *session.Manager, log *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := m.Sweep(ctx); err != nil {
				log.Error("sweeping console sessions", "err", err)
			} else if n > 0 {
				log.Debug("swept console sessions", "count", n)
			}
		}
	}
}
```

Add these lines to the usage text:

```
  AWD_PUBLIC_URL                   external https origin of awd; enables the console with AWD_CONSOLE_*
  AWD_CONSOLE_ISSUER               OIDC issuer URL
  AWD_CONSOLE_CLIENT_ID            OIDC client id
  AWD_CONSOLE_CLIENT_SECRET_FILE   file holding the OIDC client secret
  AWD_CONSOLE_ADMIN_GROUP          IdP group whose members are console admins
  AWD_CONSOLE_USER_CLAIM           ID token claim naming the user (default email)
```

- [ ] **Step 5: Startup e2e check**

In `cmd/awd/e2e_test.go`, follow the file's existing pattern for starting the binary and add `TestConsolePartialConfigRefusesToStart`: set only `AWD_PUBLIC_URL=https://x` and assert that the process exits non-zero with `AWD_CONSOLE_ISSUER` in stderr.

- [ ] **Step 6: README**

Add an "Admin console" section with these parts:
- IdP app registration. For Okta: OIDC web app, sign-in redirect `https://<awd>/console/auth/callback`, scopes `openid email profile`. For Entra ID: web platform with the same redirect, `AWD_CONSOLE_USER_CLAIM=preferred_username`, and the ID token optional claim `email` if it is used instead.
- The env table.
- The prerequisite of group data (SCIM or `awd groups apply`), and that the admin group must come from the IdP.
- That `AWD_ADMIN_TOKEN` stays the recovery path.
- That actors from the token path appear as `token:<AWD_APPLIED_BY>`.

- [ ] **Step 7: Run tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/config cmd/awd README.md
git commit -m "feat(awd): console configuration, wiring and session sweeper"
```

---

### Task 7: Console SPA scaffold, auth gate, build into `dist`, CI

**Files:**
- Create: everything under `console/` listed in File Structure except screens other than sign-in; `.github/workflows/console.yml`
- Modify: `internal/console/dist/*` (the real build output replaces the placeholder), `.gitignore` (add `console/node_modules`)

**Interfaces:**
- Produces:
  - `api.ts`: `ApiError(status, message)` and `api.get<T>(path)`, `api.post<T>(path, body)`, `api.put<T>(path, body)`, `api.del(path)`
  - `types.ts`: `Me`, `Revision`, `Machine`, `GroupsView`, `GroupSources`, `AuditEvent`
  - `app.tsx`: `<App/>`, which gates on `GET /console/api/me` (401 → SignIn, 403 → NotAdmin, 503 → Unavailable message)
  - `router.tsx`: routes `/console/`, `/console/policy`, `/console/machines`, `/console/enrollment`, `/console/groups`, `/console/audit`
  - `test/render.tsx`: `renderWithProviders(ui, {route})`; `test/server.ts`: MSW `server` with default handlers

- [ ] **Step 1: Scaffold**

```bash
mkdir console && cd console
pnpm init
pnpm add react@19.2.8 react-dom@19.2.8 @tanstack/react-query@5 @tanstack/react-router@1 @base-ui/react lucide-react class-variance-authority clsx tailwind-merge
pnpm add -D vite@7 @vitejs/plugin-react typescript@5 @types/react @types/react-dom tailwindcss@4 @tailwindcss/vite vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom msw@2 axe-core @playwright/test @axe-core/playwright
```

`console/vite.config.ts`:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  base: "/console/",
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  build: {
    outDir: "../internal/console/dist",
    emptyOutDir: true,
    // The CSP forbids inline scripts; the polyfill injects one.
    modulePreload: { polyfill: false },
  },
  server: { proxy: { "/v1": "http://127.0.0.1:8080", "/console/api": "http://127.0.0.1:8080", "/console/auth": "http://127.0.0.1:8080" } },
});
```

`package.json` scripts: `"dev": "vite"`, `"build": "tsc -b && vite build"`, `"typecheck": "tsc -b --noEmit"`, `"test": "vitest run"`, `"test:e2e": "playwright test"`.

`console/components.json`: copy `web/components.json` with `"rsc": false`, `"css": "src/index.css"`, and aliases rooted at `@/`. Then run `pnpm dlx shadcn@latest add button card table dialog input label badge`.

`console/src/index.css`: copy the `@import "tailwindcss";` header, the `@theme` block, and the light/dark CSS variable blocks from `web/app/globals.css` verbatim, so the console and the site share tokens. Dark mode is class-based (`.dark` on `<html>`), as in `web/`.

`console/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Agentic Warden console</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: API client and types**

`console/src/api.ts`:

```ts
export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? res.statusText);
  return data as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body: unknown) => request<T>("POST", path, body),
  put: <T>(path: string, body: unknown) => request<T>("PUT", path, body),
  del: (path: string) => request<void>("DELETE", path),
};
```

`console/src/types.ts`:

```ts
export type Me = { user: string; expiresAt: string; signingKeyId?: string };
export type Revision = { seq: number; version: string; ruleSet: Record<string, unknown>; createdAt: string; createdBy?: string };
export type Machine = {
  id: string; user: string; name?: string; os?: string; enrolledAt: string; lastSeenAt: string;
  lastBundleVersion?: string; lastKeyId?: string;
};
export type ScimCounts = { users: number; activeUsers: number; groups: number };
export type GroupsView = {
  hasSnapshot: boolean; source?: string; appliedBy?: string; syncedAt?: string; users?: number; groups?: number;
  members?: Record<string, string[]>; scim?: ScimCounts;
};
export type GroupSources = {
  user: string; authored: string[]; snapshot: string[]; scim: string[]; effective: string[]; scimNearMatch: string | null;
};
export type AuditEvent = { id: number; at: string; actor: string; action: string; target?: string; detail?: Record<string, unknown> };
export type EnrollmentToken = { token: string; user: string; expiresAt: string };
```

Before writing `types.ts`, check `groupsDetail` in `internal/handler/groups.go` for the exact JSON field names, and make `GroupsView` match them.

- [ ] **Step 3: Test harness and failing auth-gate tests**

`console/vitest.config.ts`: `environment: "jsdom"`, `setupFiles: ["src/test/setup.ts"]`, with the same `@` alias.

`console/src/test/server.ts`:

```ts
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";

export const me = { user: "alice@example.com", expiresAt: "2026-09-25T10:00:00Z", signingKeyId: "k-new" };

export const defaultHandlers = [
  http.get("/console/api/me", () => HttpResponse.json(me)),
  http.get("/v1/policy/revisions", () => HttpResponse.json([])),
  http.get("/v1/machines", () => HttpResponse.json([])),
  http.get("/v1/groups", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
  http.get("/v1/audit", () => HttpResponse.json([])),
];

export const server = setupServer(...defaultHandlers);
```

`console/src/test/setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
import { afterAll, afterEach, beforeAll } from "vitest";
import { cleanup } from "@testing-library/react";
import { server } from "./server";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => { server.resetHandlers(); cleanup(); });
afterAll(() => server.close());
```

`console/src/test/render.tsx` exports `renderWithProviders(ui?: ReactNode, { route = "/console/" } = {})`. It sets `window.history.pushState({}, "", route)`, creates a fresh `QueryClient` with `retry: false`, and renders `<App/>`. The router is part of App, so tests pass only a route. It also exports `expectNoAxeViolations(container)`:

```ts
import axe from "axe-core";
export async function expectNoAxeViolations(container: HTMLElement) {
  const result = await axe.run(container, { rules: { "color-contrast": { enabled: false } } });
  expect(result.violations.map((v) => v.id)).toEqual([]);
}
```

`console/src/app.test.tsx`:

```tsx
import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";
import { server } from "./test/server";
import { renderWithProviders, expectNoAxeViolations } from "./test/render";

test("signed out shows the sign-in link", async () => {
  server.use(http.get("/console/api/me", () => HttpResponse.json({ error: "sign in" }, { status: 401 })));
  const { container } = renderWithProviders();
  const link = await screen.findByRole("link", { name: /sign in/i });
  expect(link).toHaveAttribute("href", "/console/auth/login");
  await expectNoAxeViolations(container);
});

test("403 says not a console admin", async () => {
  server.use(http.get("/console/api/me", () => HttpResponse.json({ error: "not a console admin" }, { status: 403 })));
  renderWithProviders();
  expect(await screen.findByText(/not a console admin/i)).toBeInTheDocument();
});

test("503 shows the server's reason", async () => {
  server.use(http.get("/console/api/me", () =>
    HttpResponse.json({ error: "console needs group data (SCIM or awd groups apply)" }, { status: 503 })));
  renderWithProviders();
  expect(await screen.findByText(/console needs group data/i)).toBeInTheDocument();
});

test("signed in shows the user and navigation", async () => {
  renderWithProviders();
  expect(await screen.findByText("alice@example.com")).toBeInTheDocument();
  for (const name of ["Overview", "Policy", "Machines", "Enrollment", "Groups", "Audit"]) {
    expect(screen.getByRole("link", { name })).toBeInTheDocument();
  }
});

test("a 401 from any request returns to sign-in", async () => {
  let signedIn = true;
  server.use(
    http.get("/console/api/me", () => signedIn ? HttpResponse.json({ user: "alice@example.com", expiresAt: "" })
      : HttpResponse.json({ error: "sign in" }, { status: 401 })),
    http.get("/v1/machines", () => { signedIn = false; return HttpResponse.json({ error: "sign in" }, { status: 401 }); }),
  );
  renderWithProviders(undefined, { route: "/console/machines" });
  expect(await screen.findByRole("link", { name: /sign in/i })).toBeInTheDocument();
});

test("logout posts and returns to sign-in", async () => {
  /* server.use POST /console/auth/logout → 204 and flips me to 401; click "Sign out"; expect the sign-in link. */
});
```

Write the logout test in full; the comment states what it sets up and asserts.

- [ ] **Step 4: Run to verify failure**

Run: `cd console && pnpm test`
Expected: FAIL. `./app` is not found.

- [ ] **Step 5: Implement App, router, theme**

`console/src/app.tsx`:

```tsx
import { QueryCache, QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import { api, ApiError } from "@/api";
import type { Me } from "@/types";
import { buildRouter } from "@/router";
import { SignIn } from "@/screens/sign-in";

export function makeQueryClient() {
  const client: QueryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: true } },
    // Any 401 means the session is gone: re-ask /me, which flips the gate.
    queryCache: new QueryCache({
      onError: (err) => {
        if (err instanceof ApiError && err.status === 401) client.invalidateQueries({ queryKey: ["me"] });
      },
    }),
  });
  return client;
}

function Message({ title, children }: { title: string; children: ReactNode }) {
  return (
    <main className="mx-auto max-w-md p-8 text-center">
      <h1 className="text-xl font-semibold">{title}</h1>
      <div className="mt-4 text-muted-foreground">{children}</div>
    </main>
  );
}

function Gate() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<Me>("/console/api/me") });
  const [router] = useState(() => buildRouter());
  if (me.isPending) return <Message title="Loading">Checking your session…</Message>;
  if (me.error instanceof ApiError) {
    if (me.error.status === 401) return <SignIn />;
    if (me.error.status === 403) return <Message title="No access">You are not a console admin.</Message>;
    return <Message title="Console unavailable">{me.error.message}</Message>;
  }
  if (me.isError) return <Message title="Console unavailable">Could not reach awd.</Message>;
  return <RouterProvider router={router} context={{ me: me.data }} />;
}

export function App({ client }: { client?: QueryClient }) {
  const [qc] = useState(() => client ?? makeQueryClient());
  return (
    <QueryClientProvider client={qc}>
      <Gate />
    </QueryClientProvider>
  );
}
```

`console/src/screens/sign-in.tsx`:

```tsx
export function SignIn() {
  return (
    <main className="mx-auto max-w-sm p-8 text-center">
      <h1 className="text-2xl font-semibold">Agentic Warden console</h1>
      <p className="mt-2 text-muted-foreground">Sign in with your company account.</p>
      <a className="mt-6 inline-block rounded-md bg-primary px-4 py-2 text-primary-foreground" href="/console/auth/login">
        Sign in
      </a>
    </main>
  );
}
```

`console/src/router.tsx` builds a TanStack code-based router with `basepath: "/console"`. The root route renders the layout: a header with the product name, `me.user`, a theme toggle, and a "Sign out" button that POSTs `/console/auth/logout` then invalidates `["me"]`; a `<nav aria-label="Console">` with `<Link>`s labeled Overview, Policy, Machines, Enrollment, Groups, Audit; and `<Outlet/>` in `<main>`. Child routes: `/` → `Overview`, `/policy` → `Policy`, `/machines` → `Machines`, `/enrollment` → `Enrollment`, `/groups` → `Groups`, `/audit` → `Audit`. Router context type `{ me: Me }` (use `createRootRouteWithContext<{ me: Me }>()`); screens read it with `useRouteContext({ from: "__root__" })`. Until Tasks 8 and 9 land, each screen file exports a component that renders only its `<h1>`.

`console/src/theme.ts`: reads the `localStorage` key `aw-theme` inside `try/catch` and falls back to `prefers-color-scheme`. It toggles `document.documentElement.classList` `dark`, and runs from `main.tsx` before render. It is not inline, because of the CSP.

`console/src/main.tsx`: `import "./index.css"; applyStoredTheme(); createRoot(document.getElementById("root")!).render(<StrictMode><App/></StrictMode>)`.

- [ ] **Step 6: Run tests and build**

Run: `pnpm test && pnpm typecheck && pnpm build`
Expected: tests PASS. `internal/console/dist/index.html` references `/console/assets/index-*.js`. `grep -c "<script>" ../internal/console/dist/index.html` prints `0`, meaning no inline scripts.

Run: `cd .. && go test ./internal/handler/ -run Console`
Expected: PASS. The static tests use their own MapFS, so the real dist does not affect them.

- [ ] **Step 7: CI workflow**

`.github/workflows/console.yml`:

```yaml
name: console

on:
  push:
    branches: [main]
    paths: ["console/**", "internal/**", "cmd/awd/**", "go.mod", "go.sum", ".github/workflows/console.yml"]
  pull_request:
    paths: ["console/**", "internal/**", "cmd/awd/**", "go.mod", "go.sum", ".github/workflows/console.yml"]

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: pnpm/action-setup@v4
        with:
          package_json_file: console/package.json
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: pnpm
          cache-dependency-path: console/pnpm-lock.yaml
      - run: go test ./...
      - working-directory: console
        run: pnpm install --frozen-lockfile
      - working-directory: console
        run: pnpm typecheck
      - working-directory: console
        run: pnpm test
      - working-directory: console
        run: pnpm build
      - name: Committed console build is current
        run: git diff --exit-code internal/console/dist
      - working-directory: console
        run: pnpm exec playwright install --with-deps chromium
      - working-directory: console
        run: pnpm test:e2e
        env:
          CI: "true"
```

- [ ] **Step 8: Commit**

```bash
git add console .github/workflows/console.yml internal/console/dist .gitignore
git commit -m "feat(console): SPA scaffold with session gate, built into awd"
```

---

### Task 8: Overview, Machines, Enrollment, Groups, Audit screens

**Files:**
- Create/replace: `console/src/screens/{overview,machines,enrollment,groups,audit}.tsx` and a `*.test.tsx` next to each; `console/src/lib/format.ts`
- Modify: `internal/console/dist/*` (rebuild)

**Interfaces:**
- Consumes: `api`, the types, router context `me`
- Produces: `format.ts`: `relative(iso: string, now?: Date): string`, `isStale(m: Machine, now?: Date): boolean` (never seen, or last seen more than 7 days ago), `isOldKey(m: Machine, current?: string): boolean` (both IDs set and different)

- [ ] **Step 1: Failing tests**

`console/src/screens/machines.test.tsx`:

```tsx
import { http, HttpResponse } from "msw";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

const machines = [
  { id: "m1", user: "bob@example.com", name: "bob-laptop", os: "darwin", enrolledAt: "2026-09-01T00:00:00Z",
    lastSeenAt: new Date().toISOString(), lastBundleVersion: "v3", lastKeyId: "k-old" },
  { id: "m2", user: "carol@example.com", name: "", os: "linux", enrolledAt: "2026-09-01T00:00:00Z",
    lastSeenAt: "0001-01-01T00:00:00Z" },
];

test("lists machines and flags old keys and stale machines", async () => {
  server.use(http.get("/v1/machines", () => HttpResponse.json(machines)));
  const { container } = renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("bob-laptop")).closest("tr")!;
  expect(within(row).getByText(/old key/i)).toBeInTheDocument();
  const carol = screen.getByText("carol@example.com").closest("tr")!;
  expect(within(carol).getByText(/never seen/i)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("revoke requires typing the machine name", async () => {
  let deleted = "";
  server.use(
    http.get("/v1/machines", () => HttpResponse.json(deleted ? machines.slice(1) : machines)),
    http.delete("/v1/machines/:id", ({ params }) => { deleted = String(params.id); return new HttpResponse(null, { status: 204 }); }),
  );
  renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("bob-laptop")).closest("tr")!;
  await userEvent.click(within(row).getByRole("button", { name: /revoke/i }));
  const dialog = await screen.findByRole("dialog");
  const confirm = within(dialog).getByRole("button", { name: /revoke machine/i });
  expect(confirm).toBeDisabled();
  await userEvent.type(within(dialog).getByLabelText(/type bob-laptop/i), "bob-laptop");
  await userEvent.click(confirm);
  expect(deleted).toBe("m1");
  expect(await screen.findByText(/revoked bob-laptop/i)).toBeInTheDocument();
  expect(screen.queryByText("bob-laptop")).not.toBeInTheDocument();
});

// Review Focus 5.
test("treats a 404 on revoke as already revoked", async () => {
  server.use(
    http.get("/v1/machines", () => HttpResponse.json(machines)),
    http.delete("/v1/machines/:id", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
  );
  renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("bob-laptop")).closest("tr")!;
  await userEvent.click(within(row).getByRole("button", { name: /revoke/i }));
  const dialog = await screen.findByRole("dialog");
  await userEvent.type(within(dialog).getByLabelText(/type bob-laptop/i), "bob-laptop");
  await userEvent.click(within(dialog).getByRole("button", { name: /revoke machine/i }));
  expect(await screen.findByText(/already revoked/i)).toBeInTheDocument();
});

test("a machine without a name is confirmed by its id", async () => {
  /* m2 row → Revoke → dialog label "Type m2 to confirm". */
});
```

`console/src/screens/enrollment.test.tsx`:

```tsx
test("creates a token and shows it once with the enroll command", async () => {
  let body: unknown;
  server.use(http.post("/v1/enrollment-tokens", async ({ request }) => {
    body = await request.json();
    return HttpResponse.json({ token: "tok-123", user: "dan@example.com", expiresAt: "2026-09-26T09:00:00Z" }, { status: 201 });
  }));
  const { container } = renderWithProviders(undefined, { route: "/console/enrollment" });
  await userEvent.type(await screen.findByLabelText(/user/i), "dan@example.com");
  await userEvent.clear(screen.getByLabelText(/valid for/i));
  await userEvent.type(screen.getByLabelText(/valid for/i), "48h");
  await userEvent.click(screen.getByRole("button", { name: /create token/i }));
  expect(body).toEqual({ user: "dan@example.com", ttl: "48h" });
  expect(await screen.findByText("tok-123")).toBeInTheDocument();
  expect(screen.getByText(/aw-sync enroll/)).toHaveTextContent("tok-123");
  expect(screen.getByText(/shown once/i)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("server validation errors are shown", async () => {
  /* POST → 422 {"error":"ttl must be at most 168h"}; expect that text in role=alert. */
});

test("the token is gone after navigating away and back", async () => {
  /* create → click nav "Machines" → click nav "Enrollment" → queryByText("tok-123") is null. */
});
```

`console/src/screens/groups.test.tsx`:
- A snapshot with members `{a:["devs","ops"], b:["devs"]}` renders a table of `devs 2` and `ops 1`, plus the source and last sync.
- SCIM counts render as "SCIM: 3 users (2 active), 1 group".
- A 404 from `/v1/groups` shows "No group data yet" with a hint about SCIM and `awd groups apply`.
- The resolve form GETs `/v1/groups/resolve?user=<encoded>` and renders each source list plus effective.
- `scimNearMatch` shows a warning naming the near match.
- Run axe on each case.

`console/src/screens/audit.test.tsx`:
- Rows render time, actor, action, target.
- "Older" requests `?limit=50&before=<last id>` and appends the result.
- The actor filter and the action select filter the fetched rows client-side.
- An empty list shows "No audit events".
- Run axe.

`console/src/screens/overview.test.tsx`:
- Shows the current revision version and author (the first element of revisions).
- Shows the machine count.
- Shows "1 machine on an old key" (compared with `me.signingKeyId`).
- Shows the group source and sync time.
- Shows the five latest audit events from `/v1/audit?limit=5`.
- With no revisions it shows "No policy yet".

Write every test above in full, in the style of `machines.test.tsx`.

- [ ] **Step 2: Run to verify failure**

Run: `pnpm test`
Expected: FAIL. The screens render only headings.

- [ ] **Step 3: Implement**

`console/src/lib/format.ts`:

```ts
import type { Machine } from "@/types";

const ZERO = "0001-01-01T00:00:00Z";
const WEEK = 7 * 24 * 60 * 60 * 1000;

export function neverSeen(m: Machine) {
  return !m.lastSeenAt || m.lastSeenAt === ZERO;
}

export function isStale(m: Machine, now = new Date()) {
  return neverSeen(m) || now.getTime() - new Date(m.lastSeenAt).getTime() > WEEK;
}

export function isOldKey(m: Machine, current?: string) {
  return Boolean(m.lastKeyId && current && m.lastKeyId !== current);
}

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
export function relative(iso: string, now = new Date()) {
  const diff = (new Date(iso).getTime() - now.getTime()) / 1000;
  const units: [Intl.RelativeTimeFormatUnit, number][] = [["day", 86400], ["hour", 3600], ["minute", 60]];
  for (const [unit, secs] of units) if (Math.abs(diff) >= secs) return rtf.format(Math.round(diff / secs), unit);
  return "just now";
}
```

`console/src/screens/machines.tsx`:
- `useQuery(["machines"], api.get<Machine[]>("/v1/machines"))`.
- A shadcn `Table` with columns User, Name, OS, Last seen, Bundle, Key, and an actions column holding a `Button variant="destructive" size="sm"` labeled "Revoke" with `aria-label={"Revoke " + label}`.
- Badges: "Never seen" when `neverSeen`, "Stale" when `isStale`, and "Old key" when `isOldKey(m, me.signingKeyId)`.
- The Revoke `Dialog` reads "Type <label> to confirm", where `label = m.name || m.id`. Its `Input` has that text as its `<Label>`, and its confirm button "Revoke machine" is disabled until the input equals `label`.
- The mutation calls `api.del("/v1/machines/" + encodeURIComponent(m.id))`. On success it sets a status line "Revoked <label>" in a `role="status"` region. On an `ApiError` with status 404 it sets "<label> was already revoked". Either way it invalidates `["machines"]` and `["audit"]`. Any other error shows `error.message` in `role="alert"`.

`console/src/screens/enrollment.tsx`:
- A form with a User `Input` (type email, required) and a "Valid for" `Input` (default `24h`, with the hint "at most 168h").
- The mutation calls `api.post<EnrollmentToken>("/v1/enrollment-tokens", { user, ttl })`.
- The result is held in component state only, so remounting on navigation clears it. It renders in a `Card` titled "Token for <user> — shown once": the token in `<code>`, a "Copy" button using `navigator.clipboard.writeText` inside try/catch, and `<pre>` with `aw-sync enroll --url ${location.origin} --token ${token}`.
- Errors show in `role="alert"`.
- Before writing the enroll command, check the real `aw-sync enroll` flags: `grep -n "flag\.\|Usage" cmd/aw-sync/*.go | grep -i enroll`.

`console/src/screens/groups.tsx`:
- `useQuery(["groups"])` over `/v1/groups`, where a 404 `ApiError` maps to `null`.
- Summary: source, applied by, relative sync time, and the SCIM line.
- Group table: invert `members` to group → count, sort by name, and show a `Table`.
- Resolve form: an `Input` labeled "User" and a "Resolve" button that runs `useQuery(["resolve", user], { enabled: Boolean(user) })` against `/v1/groups/resolve?user=`. It renders definition-list rows Authored, Snapshot, SCIM, and Effective, plus a warning `role="status"` when `scimNearMatch` is set: "SCIM has <near> — differs only in case; fix the IdP attribute mapping."

`console/src/screens/audit.tsx`:
- `useInfiniteQuery(["audit"])` with a page size of 50. `getNextPageParam` returns the last page's last `id` when the page is full.
- Filters: an actor text `Input` and an action `<select>` whose options are the distinct actions of the loaded rows plus "All".
- Table columns: Time (absolute plus `relative`), Actor, Action, Target, Detail (compact JSON in `<code>`).
- Button "Older" when `hasNextPage`.

`console/src/screens/overview.tsx`: four `Card`s ("Policy", "Machines", "Groups", "Recent activity") built from the queries above. Reuse the query keys `["revisions"]`, `["machines"]`, `["groups"]`, and `["audit", "recent"]` (limit 5).

- [ ] **Step 4: Run tests and rebuild**

Run: `pnpm test && pnpm typecheck && pnpm build`
Expected: PASS, with dist updated.

- [ ] **Step 5: Commit**

```bash
git add console internal/console/dist
git commit -m "feat(console): overview, machines, enrollment, groups and audit screens"
```

---

### Task 9: Policy screen with YAML editor and diff

**Files:**
- Create: `console/src/policy/yaml.ts`, `console/src/policy/yaml.test.ts`, `console/src/policy/editor.tsx`, `console/src/policy/diff.tsx`, `console/src/screens/policy.tsx`, `console/src/screens/policy.test.tsx`
- Modify: `internal/console/dist/*`

**Interfaces:**
- Produces:
  - `yaml.ts`: `toYaml(ruleSet: unknown): string`, and `parsePolicy(text: string): { ok: true; value: unknown } | { ok: false; message: string; line: number }`
  - `diff.tsx`: `<LineDiff before={string} after={string}/>`
  - `editor.tsx`: default export `<YamlEditor value onChange ariaLabel errorLine?/>`, lazy-loaded

- [ ] **Step 1: Dependencies**

Run: `pnpm add yaml@2 diff@8 codemirror @codemirror/lang-yaml @codemirror/state @codemirror/view`

- [ ] **Step 2: Failing tests**

`console/src/policy/yaml.test.ts`:

```ts
import { parsePolicy, toYaml } from "./yaml";

test("round trips a rule set", () => {
  const rs = { version: "v2", rules: [{ name: "baseline", agents: { claude: { managed: { model: "sonnet" } } } }] };
  const parsed = parsePolicy(toYaml(rs));
  expect(parsed).toEqual({ ok: true, value: rs });
});

test("reports a syntax error with its line", () => {
  const r = parsePolicy("version: v1\nrules:\n  - name: [unclosed\n");
  expect(r.ok).toBe(false);
  if (!r.ok) expect(r.line).toBe(3);
});

test("rejects duplicate keys like the CLI's strict parse", () => {
  const r = parsePolicy("version: v1\nversion: v2\n");
  expect(r.ok).toBe(false);
  if (!r.ok) expect(r.line).toBe(2);
});

test("an empty document is an error, not null", () => {
  expect(parsePolicy("   \n").ok).toBe(false);
});
```

`console/src/screens/policy.test.tsx`:

```tsx
const revisions = [
  { seq: 2, version: "v2", ruleSet: { version: "v2", rules: [{ name: "baseline" }] }, createdAt: "2026-09-24T10:00:00Z", createdBy: "alice@example.com" },
  { seq: 1, version: "v1", ruleSet: { version: "v1" }, createdAt: "2026-09-20T10:00:00Z", createdBy: "token:ops" },
];

test("lists revisions and diffs two of them", async () => {
  server.use(http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)));
  const { container } = renderWithProviders(undefined, { route: "/console/policy" });
  expect(await screen.findByText("v2")).toBeInTheDocument();
  expect(screen.getByText("token:ops")).toBeInTheDocument();
  await userEvent.selectOptions(screen.getByLabelText(/compare from/i), "v1");
  await userEvent.selectOptions(screen.getByLabelText(/compare to/i), "v2");
  expect(await screen.findByText(/\+ .*name: baseline/)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("a YAML error is shown at its line and nothing is sent", async () => {
  let posted = false;
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.post("/v1/policy/revisions", () => { posted = true; return HttpResponse.json({}, { status: 201 }); }),
  );
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  await userEvent.clear(editor);
  await userEvent.type(editor, "version: v3\nrules: [{{unclosed");
  await userEvent.click(screen.getByRole("button", { name: /review changes/i }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/line 2/i);
  expect(posted).toBe(false);
});

test("review shows the diff, confirm posts JSON", async () => {
  let body: unknown;
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.post("/v1/policy/revisions", async ({ request }) => { body = await request.json(); return HttpResponse.json({}, { status: 201 }); }),
  );
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  await userEvent.clear(editor);
  await userEvent.type(editor, "version: v3\nrules:\n  - name: baseline\n");
  await userEvent.click(screen.getByRole("button", { name: /review changes/i }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByText(/- version: v2/)).toBeInTheDocument();
  expect(within(dialog).getByText(/\+ version: v3/)).toBeInTheDocument();
  await userEvent.click(within(dialog).getByRole("button", { name: /apply revision/i }));
  expect(body).toEqual({ version: "v3", rules: [{ name: "baseline" }] });
  expect(await screen.findByText(/applied v3/i)).toBeInTheDocument();
});

test("a 422 from the server is shown above the editor and the draft is kept", async () => {
  /* POST → 422 {"error":"rule baseline: agent claude: managed.model must be a string"}; after Apply:
     role=alert has that text; the editor textbox still contains "version: v3". */
});

// Review Focus 4.
test("shows the server message when the version is not a string", async () => {
  /* type "version: 2026.10\n"; Review → dialog → Apply; POST → 400 {"error":"the request body is not a valid rule
     set: json: cannot unmarshal number into Go struct field RuleSet.version of type string"}; role=alert contains
     "cannot unmarshal number"; editor still contains "2026.10". */
});

test("the editor says comments are not kept", async () => {
  /* open New revision; expect text /comments are not kept/i. */
});
```

Write every stubbed test in full.

In jsdom, CodeMirror's contenteditable does not accept `userEvent.type` reliably. `YamlEditor` therefore reads `import.meta.env.MODE === "test"` and renders a plain `<textarea aria-label={ariaLabel}>` under Vitest. The CodeMirror path is exercised by the Playwright suite in Task 10. State this in a comment at the top of `editor.tsx`.

- [ ] **Step 3: Run to verify failure**

Run: `pnpm test src/policy src/screens/policy.test.tsx`
Expected: FAIL.

- [ ] **Step 4: Implement**

`console/src/policy/yaml.ts`:

```ts
import { parseDocument, stringify } from "yaml";

export function toYaml(ruleSet: unknown): string {
  return stringify(ruleSet, { indent: 2, lineWidth: 0 });
}

export type Parsed = { ok: true; value: unknown } | { ok: false; message: string; line: number };

// parsePolicy mirrors the CLI's sigs.k8s.io/yaml UnmarshalStrict closely
// enough for authoring: duplicate keys are errors, and the result is plain
// JSON data. Type checks stay with the server.
export function parsePolicy(text: string): Parsed {
  if (text.trim() === "") return { ok: false, message: "The document is empty.", line: 1 };
  const doc = parseDocument(text, { uniqueKeys: true, prettyErrors: true });
  const err = doc.errors[0];
  if (err) return { ok: false, message: err.message, line: err.linePos?.[0]?.line ?? 1 };
  return { ok: true, value: doc.toJS() };
}
```

`console/src/policy/diff.tsx`:

```tsx
import { diffLines } from "diff";

export function LineDiff({ before, after }: { before: string; after: string }) {
  const parts = diffLines(before, after);
  return (
    <pre className="overflow-x-auto rounded-md border p-3 text-sm" aria-label="Changes">
      {parts.flatMap((p, i) =>
        p.value.replace(/\n$/, "").split("\n").map((line, j) => (
          <div key={`${i}-${j}`}
            className={p.added ? "bg-green-500/10" : p.removed ? "bg-red-500/10" : "text-muted-foreground"}>
            {(p.added ? "+ " : p.removed ? "- " : "  ") + line}
          </div>
        )),
      )}
    </pre>
  );
}
```

`console/src/policy/editor.tsx`: a default export. Under test it renders the textarea. Otherwise it mounts an `EditorView` with `basicSetup`, `yaml()`, and `EditorView.contentAttributes.of({ "aria-label": ariaLabel, role: "textbox", "aria-multiline": "true" })`, plus an update listener calling `onChange`. When `errorLine` is set, it adds a line decoration class `cm-error-line` on that line.

`console/src/screens/policy.tsx`:
- The revisions query renders a `Table` with columns Version, Applied by, Applied.
- Clicking a row expands it to show `toYaml(ruleSet)` in `<pre>`.
- "Compare from" and "Compare to" `<select>`s render a `LineDiff` of the two revisions' YAML.
- "New revision" opens an editor section with the note "Comments are not kept: revisions are stored as JSON." Its draft is prefilled with `toYaml(revisions[0]?.ruleSet ?? { version: "", rules: [] })`, and the `YamlEditor` is loaded with `React.lazy(() => import("@/policy/editor"))` inside `<Suspense>`.
- "Review changes" runs `parsePolicy`. On error it shows `role="alert"` "Line N: message" and sets `errorLine`. On success it opens a `Dialog` "Apply revision <value.version>" with a `LineDiff` between the current YAML and `toYaml(value)`, and an "Apply revision" button.
- The mutation calls `api.post("/v1/policy/revisions", value)`. On success it closes the dialog and the editor, shows `role="status"` "Applied <version>", and invalidates `["revisions"]` and `["audit"]`. On error it closes the dialog, keeps the draft, and shows `role="alert"` with `error.message`.

- [ ] **Step 5: Run tests and rebuild**

Run: `pnpm test && pnpm typecheck && pnpm build`
Expected: PASS. `ls ../internal/console/dist/assets` shows a separate `editor-*.js` chunk, confirming the lazy load.

- [ ] **Step 6: Commit**

```bash
git add console internal/console/dist
git commit -m "feat(console): policy revisions, diff and YAML editor"
```

---

### Task 10: End-to-end suite against real `awd` and the fake IdP

**Files:**
- Create: `console/playwright.config.ts`, `console/e2e/global-setup.ts`, `console/e2e/console.spec.ts`, `console/e2e/secret.txt` (contains `secret`)

**Interfaces:**
- Consumes: the `fakeidp` binary (Task 3), `awd` env (Task 6), the SPA (Tasks 7–9)

- [ ] **Step 1: Config**

`console/playwright.config.ts`:

```ts
import { defineConfig, devices } from "@playwright/test";

const AWD = "http://127.0.0.1:9401";

export default defineConfig({
  testDir: "e2e",
  globalSetup: "./e2e/global-setup.ts",
  use: { baseURL: AWD, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "go run ../internal/console/oidctest/cmd/fakeidp -addr 127.0.0.1:9400",
      url: "http://127.0.0.1:9400/.well-known/openid-configuration",
      reuseExistingServer: !process.env.CI,
    },
    {
      command: "go run ../cmd/awd serve",
      url: `${AWD}/healthz`,
      reuseExistingServer: !process.env.CI,
      env: {
        AWD_ADDR: "127.0.0.1:9401",
        AWD_ADMIN_TOKEN: "e2e-admin",
        AWD_PUBLIC_URL: AWD,
        AWD_CONSOLE_ISSUER: "http://127.0.0.1:9400",
        AWD_CONSOLE_CLIENT_ID: "console",
        AWD_CONSOLE_CLIENT_SECRET_FILE: "e2e/secret.txt",
        AWD_CONSOLE_ADMIN_GROUP: "console-admins",
      },
    },
  ],
});
```

Check how `awd` is started in `cmd/awd/main.go` `run()` (subcommand name, or none for serve) and match the command line to it.

`console/e2e/global-setup.ts` seeds through the admin token, using `fetch` with `Authorization: Bearer e2e-admin` and `X-Applied-By: e2e`:
- `PUT /v1/groups` with `{"source":"e2e","members":{"admin@example.com":["console-admins"],"dev@example.com":["devs"]}}`
- `POST /v1/policy/revisions` with `{"version":"v1","rules":[{"name":"baseline"}]}`
- `POST /v1/enrollment-tokens` with `{"user":"dev@example.com"}`, then `POST /v1/machines/enroll` with `{"token":…,"name":"e2e-laptop","os":"linux"}`

Each step asserts a 2xx status. awd uses the memory store, so a fresh server starts empty. With `reuseExistingServer` locally, the setup tolerates 409 on the revision.

- [ ] **Step 2: Spec**

`console/e2e/console.spec.ts`:

```ts
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

async function signIn(page: Page, email: string) {
  await page.goto("/console/");
  await page.getByRole("link", { name: "Sign in" }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("admin signs in, applies a revision, revokes a machine, and sees both in audit", async ({ page }) => {
  await signIn(page, "admin@example.com");
  await expect(page.getByText("admin@example.com")).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

  await page.getByRole("link", { name: "Policy" }).click();
  await page.getByRole("button", { name: /new revision/i }).click();
  const editor = page.getByRole("textbox", { name: /policy yaml/i });
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("version: v2\nrules:\n  - name: baseline\n");
  await page.getByRole("button", { name: /review changes/i }).click();
  await page.getByRole("dialog").getByRole("button", { name: /apply revision/i }).click();
  await expect(page.getByText(/applied v2/i)).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

  await page.getByRole("link", { name: "Machines" }).click();
  await page.getByRole("button", { name: /revoke e2e-laptop/i }).click();
  await page.getByLabel(/type e2e-laptop/i).fill("e2e-laptop");
  await page.getByRole("button", { name: /revoke machine/i }).click();
  await expect(page.getByText(/revoked e2e-laptop/i)).toBeVisible();

  await page.getByRole("link", { name: "Audit" }).click();
  const rows = page.getByRole("row");
  await expect(rows.filter({ hasText: "machine.revoke" }).filter({ hasText: "admin@example.com" })).toHaveCount(1);
  await expect(rows.filter({ hasText: "policy.revision.create" }).filter({ hasText: "admin@example.com" })).toHaveCount(1);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test("a non-admin is refused", async ({ page }) => {
  await signIn(page, "dev@example.com");
  await expect(page.getByText(/not a console admin/i)).toBeVisible();
});

test("sign out ends the session", async ({ page }) => {
  await signIn(page, "admin@example.com");
  await page.getByRole("button", { name: /sign out/i }).click();
  await expect(page.getByRole("link", { name: "Sign in" })).toBeVisible();
  await page.goto("/console/machines");
  await expect(page.getByRole("link", { name: "Sign in" })).toBeVisible();
});

test("every screen passes axe in dark mode", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await signIn(page, "admin@example.com");
  for (const name of ["Overview", "Policy", "Machines", "Enrollment", "Groups", "Audit"]) {
    await page.getByRole("link", { name }).click();
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    expect((await new AxeBuilder({ page }).analyze()).violations, name).toEqual([]);
  }
});
```

- [ ] **Step 3: Run**

Run: `cd console && pnpm build && pnpm exec playwright install chromium && pnpm test:e2e`
Expected: 4 passed.

On this WSL box, Playwright's system libraries are missing. The known workaround is to extract the debs to `/tmp/pwlibs` and set `LD_LIBRARY_PATH`; see memory. If that fails too, report it and rely on the CI job; do not skip the suite silently.

- [ ] **Step 4: Commit**

```bash
git add console/playwright.config.ts console/e2e
git commit -m "test(console): end-to-end sign-in, revision, revoke and audit against real awd"
```

---

## Final checks (after Task 10)

- [ ] `go test ./...` passes. The Postgres suite passes with `AWD_TEST_DATABASE_URL` set (Docker).
- [ ] `cd console && pnpm typecheck && pnpm test && pnpm build && git diff --exit-code ../internal/console/dist`
- [ ] `grep '^go ' go.mod` prints `go 1.22`.
- [ ] Start `awd` with no console env: `curl -i localhost:8080/console/` returns 404, and the CLI (`awd machines`) still works.
- [ ] Every failure-mode row in Global Constraints maps to a named test. List the mapping in the final review.
