# IdP group sync — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An operator can push an IdP membership snapshot to `awd` (`PUT /v1/groups`, `awd groups apply`), and every machine's bundle resolves its user's groups as the union of the authored `groups` map and the snapshot — with no client or bundle-format change.

**Architecture:** `model.GroupSnapshot` is stored append-only (newest is current) by both stores through two new `Store` methods, one Postgres migration, and conformance cases in `storetest`. `getBundle` unions `ruleSet.GroupsFor(user)` with the current snapshot's entry via `policy.UnionGroups`. Two admin routes (`PUT`/`GET /v1/groups`) and two `awd` subcommands (`groups apply <file>`, `groups`) complete it.

**Tech Stack:** Go 1.22; `encoding/json`; `sigs.k8s.io/yaml` (already required, used by `LoadRuleSet`); pgx (already required). No dependency changes.

**Spec:** `docs/superpowers/specs/2026-09-22-idp-group-sync-design.md` — every section.

## Spec refinements

1. **`SyncedAt` is set by the handler** (`h.Now()`), stored verbatim by both stores; a zero `SyncedAt` handed to a store is replaced with `time.Now()`, as `PutRuleSet` does for `CreatedAt`. The conformance case passes a fixed instant and expects it back.
2. **`adminRequest` gains the `X-Applied-By` header** (from the existing `appliedBy()`), so `groups apply` records who posted, as `apply` does. The other admin routes ignore the header; no behaviour change for them.
3. **Store-level validation mirrors the handler's**: `PutGroupSnapshot` returns `ErrBadInput` for an empty user key or an empty group name, so a snapshot can never be stored malformed whatever the caller. The handler validates first and answers 422 with a message naming the key; the store check is the backstop.
4. **Snapshot summary shape** (`PUT` 200 and `GET` 200): `{"source", "appliedBy", "syncedAt", "users", "groups"}`; `GET` adds `"members"`. `groups` counts distinct group names across all users.

## Global Constraints

- Go 1.22; `go.mod` says `go 1.22`. Do not run `go mod tidy`; no dependency changes.
- No `gcc`: never `go test -race`. Before every commit: `go test ./...`, `go vet ./...`, `gofmt -l .` (must print nothing). Build only with `./...`.
- Postgres conformance runs only with `AWD_TEST_DATABASE_URL` set (the existing `postgres_test.go` skip); the migration must apply cleanly there. Executors without a database run the memory conformance and note that the Postgres path was not exercised.
- Return `make([]T, 0)`, never a nil slice, from anything JSON-encoded as a list.
- Admin routes: 503 without `AWD_ADMIN_TOKEN`, 401 with a wrong bearer — via the existing `requireAdmin`.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S`.
- Comment style: a comment says *why*, in full sentences, on every exported identifier and every struct field.
- Model tiering for executors: Task 1 is transcription across several files (cheapest tier is fine; mid tier if the Postgres path must be run); Tasks 2 and 3 are integration (mid tier); the final whole-branch review is the most capable tier.

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/model/model.go` | `GroupSnapshot` |
| `internal/policy/groups.go`, `groups_test.go` | `UnionGroups` |
| `internal/store/store.go` | two new interface methods |
| `internal/store/memory.go` | in-memory snapshots |
| `internal/store/migrations/0003_group_snapshots.sql` | table |
| `internal/store/postgres.go` | Postgres snapshots |
| `internal/store/storetest/conformance.go` | five conformance cases |
| `internal/handler/groups.go`, `groups_test.go` | `PUT`/`GET /v1/groups` |
| `internal/handler/handler.go` | routes |
| `internal/handler/bundle.go`, `bundle_test.go` | union in `getBundle` |
| `cmd/awd/main.go`, `cmd/awd/e2e_test.go` | `groups apply`, `groups`, `X-Applied-By` in `adminRequest` |
| `examples/groups.yaml`, `README.md` | docs |

---

### Task 1: model, `UnionGroups`, store interface, both stores, conformance

**Files:**
- Modify: `internal/model/model.go`
- Create: `internal/policy/groups.go`, `internal/policy/groups_test.go`
- Modify: `internal/store/store.go`, `internal/store/memory.go`, `internal/store/postgres.go`
- Create: `internal/store/migrations/0003_group_snapshots.sql`
- Modify: `internal/store/storetest/conformance.go`

**Interfaces:**
- Produces: `model.GroupSnapshot{Source, AppliedBy string; SyncedAt time.Time; Members map[string][]string}`; `policy.UnionGroups(a, b []string) []string`; `Store.PutGroupSnapshot(ctx, model.GroupSnapshot) error`; `Store.CurrentGroupSnapshot(ctx) (model.GroupSnapshot, error)`. Tasks 2 and 3 consume all of them.

- [ ] **Step 1: Write the failing tests**

Create `internal/policy/groups_test.go`:

```go
package policy_test

import (
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

func TestUnionGroupsSortsAndDeduplicates(t *testing.T) {
	got := policy.UnionGroups([]string{"platform", "oncall"}, []string{"oncall", "mobile"})
	if want := []string{"mobile", "oncall", "platform"}; !equal(got, want) {
		t.Errorf("UnionGroups = %v, want %v", got, want)
	}
}

func TestUnionGroupsWithOneSideNil(t *testing.T) {
	if got := policy.UnionGroups(nil, []string{"b", "a"}); !equal(got, []string{"a", "b"}) {
		t.Errorf("nil left: %v", got)
	}
	if got := policy.UnionGroups([]string{"a"}, nil); !equal(got, []string{"a"}) {
		t.Errorf("nil right: %v", got)
	}
}

func TestUnionGroupsOfNothingIsAnEmptySliceNotNil(t *testing.T) {
	got := policy.UnionGroups(nil, nil)
	if got == nil || len(got) != 0 {
		t.Errorf("UnionGroups(nil, nil) = %#v, want an empty, non-nil slice for JSON", got)
	}
}
```

If `internal/policy` tests have no `equal(a, b []string) bool` helper, add one at the bottom of this file:

```go
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

In `internal/store/storetest/conformance.go`, add five entries to the `tests` table after `{"a deleted machine is gone", machineDelete},`:

```go
		{"current group snapshot on an empty store reports not found", snapshotOnEmpty},
		{"a stored group snapshot can be read back", snapshotPutThenCurrent},
		{"the newest group snapshot is the current one", snapshotNewestWins},
		{"a group snapshot with an empty user key is rejected", snapshotUserKeyRequired},
		{"a group snapshot with an empty group name is rejected", snapshotGroupNameRequired},
```

and append the functions:

```go
func snapshot(source string, at time.Time, members map[string][]string) model.GroupSnapshot {
	return model.GroupSnapshot{Source: source, AppliedBy: "ops", SyncedAt: at, Members: members}
}

func snapshotOnEmpty(t *testing.T, s store.Store) {
	_, err := s.CurrentGroupSnapshot(context.Background())
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound before any snapshot is posted", err)
	}
}

func snapshotPutThenCurrent(t *testing.T, s store.Store) {
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	want := snapshot("okta", at, map[string][]string{"alice@acme.com": {"platform", "oncall"}})
	if err := s.PutGroupSnapshot(context.Background(), want); err != nil {
		t.Fatal(err)
	}

	got, err := s.CurrentGroupSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "okta" || got.AppliedBy != "ops" || !got.SyncedAt.Equal(at) {
		t.Errorf("snapshot header = %+v", got)
	}
	if !equalStrings(got.Members["alice@acme.com"], []string{"platform", "oncall"}) {
		t.Errorf("members = %v", got.Members)
	}
}

func snapshotNewestWins(t *testing.T, s store.Store) {
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	first := snapshot("okta", at, map[string][]string{"alice@acme.com": {"platform"}})
	second := snapshot("okta", at.Add(time.Hour), map[string][]string{"bob@acme.com": {"mobile"}})
	for _, snap := range []model.GroupSnapshot{first, second} {
		if err := s.PutGroupSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.CurrentGroupSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, alice := got.Members["alice@acme.com"]; alice {
		t.Error("a snapshot replaces the previous one whole; alice should be gone")
	}
	if !equalStrings(got.Members["bob@acme.com"], []string{"mobile"}) {
		t.Errorf("members = %v", got.Members)
	}
}

func snapshotUserKeyRequired(t *testing.T, s store.Store) {
	err := s.PutGroupSnapshot(context.Background(), snapshot("okta", time.Now(), map[string][]string{"": {"platform"}}))
	if !errors.Is(err, model.ErrBadInput) {
		t.Errorf("err = %v, want ErrBadInput", err)
	}
}

func snapshotGroupNameRequired(t *testing.T, s store.Store) {
	err := s.PutGroupSnapshot(context.Background(), snapshot("okta", time.Now(), map[string][]string{"alice@acme.com": {"platform", ""}}))
	if !errors.Is(err, model.ErrBadInput) {
		t.Errorf("err = %v, want ErrBadInput", err)
	}
}
```

`equalStrings` already exists in this file. Make sure `errors` and `context` are imported.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/policy/ -run TestUnionGroups; go test ./internal/store/`
Expected: `undefined: policy.UnionGroups`; `s.CurrentGroupSnapshot undefined`.

- [ ] **Step 3: Model and `UnionGroups`**

In `internal/model/model.go`, after `Revision`, add:

```go
// GroupSnapshot is one export of identity-provider memberships. It replaces
// the previous snapshot whole: a user absent from it has no synced groups,
// which is what a cron export means and what makes a snapshot auditable.
type GroupSnapshot struct {
	// Seq orders snapshots, assigned by the store as for revisions.
	Seq int64 `json:"seq"`
	// Source names the exporter, for the operator reading a listing.
	Source string `json:"source"`
	// AppliedBy is who posted it, from the client's environment.
	AppliedBy string `json:"appliedBy,omitempty"`
	// SyncedAt is when the server stored it.
	SyncedAt time.Time `json:"syncedAt"`
	// Members maps a user, exactly as the enrollment names them, to their
	// groups. No case folding: an export whose keys differ from the
	// enrolled emails is an export to fix, not to paper over.
	Members map[string][]string `json:"members"`
}

// Validate reports whether every user key and group name is present. A
// snapshot with an empty key would silently apply to nobody.
func (s GroupSnapshot) Validate() error {
	for user, groups := range s.Members {
		if user == "" {
			return fmt.Errorf("group snapshot has a member with an empty user: %w", ErrBadInput)
		}
		for _, g := range groups {
			if g == "" {
				return fmt.Errorf("group snapshot: user %q has an empty group name: %w", user, ErrBadInput)
			}
		}
	}
	return nil
}
```

Add `"fmt"` to the model imports if missing.

Create `internal/policy/groups.go`:

```go
package policy

import "sort"

// UnionGroups merges the authored memberships with the synced ones, sorted
// and without duplicates. Union, not replacement: the authored map is the
// manual override and nothing an author wrote disappears when the first
// snapshot lands. The result is never nil, since it is JSON-encoded as a
// list.
func UnionGroups(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, g := range list {
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Store interface and memory store**

In `internal/store/store.go`, append to the `Store` interface after `DeleteMachine`:

```go

	// PutGroupSnapshot stores a membership snapshot; the newest is current.
	// It returns model.ErrBadInput for an empty user key or group name.
	PutGroupSnapshot(ctx context.Context, s model.GroupSnapshot) error
	// CurrentGroupSnapshot returns the newest snapshot, or model.ErrNotFound
	// when none has ever been posted.
	CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error)
```

In `internal/store/memory.go`, add a field `snapshots []model.GroupSnapshot` to `Memory` and these methods:

```go
// PutGroupSnapshot appends a snapshot; the newest is current.
func (m *Memory) PutGroupSnapshot(_ context.Context, s model.GroupSnapshot) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if s.SyncedAt.IsZero() {
		s.SyncedAt = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSeq++
	s.Seq = m.nextSeq
	m.snapshots = append(m.snapshots, s)
	return nil
}

// CurrentGroupSnapshot returns the newest snapshot.
func (m *Memory) CurrentGroupSnapshot(_ context.Context) (model.GroupSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.snapshots) == 0 {
		return model.GroupSnapshot{}, fmt.Errorf("store: no group snapshot has been posted: %w", model.ErrNotFound)
	}
	return m.snapshots[len(m.snapshots)-1], nil
}
```

Add `"time"` to memory.go's imports if missing.

- [ ] **Step 5: Postgres**

Create `internal/store/migrations/0003_group_snapshots.sql`:

```sql
-- A group snapshot is one export of identity-provider memberships. The
-- newest is current and replaces the previous one whole; older rows stay as
-- the audit trail. Members is the user -> groups map as JSON.
CREATE TABLE IF NOT EXISTS group_snapshots (
    seq        BIGSERIAL   PRIMARY KEY,
    source     TEXT        NOT NULL DEFAULT '',
    applied_by TEXT        NOT NULL DEFAULT '',
    synced_at  TIMESTAMPTZ NOT NULL,
    members    JSONB       NOT NULL
);
```

In `internal/store/postgres.go`, add:

```go
// PutGroupSnapshot stores a snapshot; the newest is current.
func (p *Postgres) PutGroupSnapshot(ctx context.Context, s model.GroupSnapshot) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	encoded, err := json.Marshal(s.Members)
	if err != nil {
		return fmt.Errorf("store: encoding group snapshot members: %w", err)
	}
	syncedAt := s.SyncedAt
	if syncedAt.IsZero() {
		syncedAt = time.Now()
	}
	const query = `
		INSERT INTO group_snapshots (source, applied_by, synced_at, members)
		VALUES ($1, $2, $3, $4)`
	if _, err := p.pool.Exec(ctx, query, s.Source, s.AppliedBy, syncedAt, encoded); err != nil {
		return fmt.Errorf("store: storing group snapshot: %w", err)
	}
	return nil
}

// CurrentGroupSnapshot returns the newest snapshot.
func (p *Postgres) CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error) {
	const query = `
		SELECT seq, source, applied_by, synced_at, members
		FROM group_snapshots
		ORDER BY seq DESC
		LIMIT 1`
	var (
		s       model.GroupSnapshot
		members []byte
	)
	err := p.pool.QueryRow(ctx, query).Scan(&s.Seq, &s.Source, &s.AppliedBy, &s.SyncedAt, &members)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return model.GroupSnapshot{}, fmt.Errorf("store: no group snapshot has been posted: %w", model.ErrNotFound)
	case err != nil:
		return model.GroupSnapshot{}, fmt.Errorf("store: reading the group snapshot: %w", err)
	}
	if err := json.Unmarshal(members, &s.Members); err != nil {
		return model.GroupSnapshot{}, fmt.Errorf("store: decoding group snapshot members: %w", err)
	}
	return s, nil
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/policy/ ./internal/store/... ./internal/model/`
Expected: PASS (Postgres cases skip without `AWD_TEST_DATABASE_URL`; if a database is available, run with it set and confirm the five new cases pass there too — say which in the report).

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./... && git add internal/model internal/policy/groups.go internal/policy/groups_test.go internal/store
git commit -m "feat(store): group membership snapshots, newest current, in both stores; policy.UnionGroups

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 2: `PUT`/`GET /v1/groups`; the bundle resolves the union

**Files:**
- Create: `internal/handler/groups.go`, `internal/handler/groups_test.go`
- Modify: `internal/handler/handler.go` (routes)
- Modify: `internal/handler/bundle.go` (`getBundle`), `internal/handler/bundle_test.go` (two tests)

**Interfaces:**
- Consumes: Task 1's store methods, `model.GroupSnapshot.Validate`, `policy.UnionGroups`.
- Produces: the two routes and the summary shape from refinement 4. Task 3 posts to and reads from them.

- [ ] **Step 1: Write the failing tests**

Create `internal/handler/groups_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// putGroups posts a snapshot with the given bearer; empty sends none.
func putGroups(t *testing.T, srv *httptest.Server, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/v1/groups", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func getGroups(t *testing.T, srv *httptest.Server, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/groups", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type groupSummary struct {
	Source    string              `json:"source"`
	AppliedBy string              `json:"appliedBy"`
	SyncedAt  string              `json:"syncedAt"`
	Users     int                 `json:"users"`
	Groups    int                 `json:"groups"`
	Members   map[string][]string `json:"members"`
}

func decodeSummary(t *testing.T, resp *http.Response) groupSummary {
	t.Helper()
	var s groupSummary
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGroupsRoutesRequireTheAdminToken(t *testing.T) {
	srv := newServer(t)
	if resp := putGroups(t, srv, `{"members":{}}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT without a token: %d, want 401", resp.StatusCode)
	}
	if resp := putGroups(t, srv, `{"members":{}}`, "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT with a wrong token: %d, want 401", resp.StatusCode)
	}
	if resp := getGroups(t, srv, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET without a token: %d, want 401", resp.StatusCode)
	}
}

func TestPutGroupsStoresTheSnapshotAndSummarises(t *testing.T) {
	srv := newServer(t)

	resp := putGroups(t, srv, `{"source":"okta-export","members":{"alice@acme.com":["platform","oncall"],"bob@acme.com":["oncall"]}}`, adminToken)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	s := decodeSummary(t, resp)
	if s.Source != "okta-export" || s.Users != 2 || s.Groups != 2 || s.SyncedAt == "" {
		t.Errorf("summary = %+v; want 2 users and 2 distinct groups", s)
	}

	got := getGroups(t, srv, adminToken)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", got.StatusCode)
	}
	if full := decodeSummary(t, got); len(full.Members["alice@acme.com"]) != 2 {
		t.Errorf("GET members = %v", full.Members)
	}
}

func TestGetGroupsIs404BeforeAnySnapshot(t *testing.T) {
	srv := newServer(t)
	if resp := getGroups(t, srv, adminToken); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPutGroupsRejectsMalformedSnapshots(t *testing.T) {
	srv := newServer(t)
	for name, body := range map[string]string{
		"no members":        `{"source":"x"}`,
		"members not object": `{"members":[]}`,
		"empty user":        `{"members":{"":["platform"]}}`,
		"value not list":    `{"members":{"alice@acme.com":"platform"}}`,
		"empty group":       `{"members":{"alice@acme.com":["platform",""]}}`,
		"unknown field":     `{"members":{},"extra":1}`,
	} {
		resp := putGroups(t, srv, body, adminToken)
		if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 422 or 400", name, resp.StatusCode)
		}
	}
	// The previous snapshot survives a bad one.
	putGroups(t, srv, `{"members":{"alice@acme.com":["platform"]}}`, adminToken)
	putGroups(t, srv, `{"members":{"":["x"]}}`, adminToken)
	if s := decodeSummary(t, getGroups(t, srv, adminToken)); s.Users != 1 {
		t.Errorf("a rejected snapshot replaced the good one: %+v", s)
	}
}

func TestPutGroupsRejectsAnOversizedBody(t *testing.T) {
	srv := newServer(t)
	huge := `{"members":{"alice@acme.com":["` + strings.Repeat("g", 9<<20) + `"]}}`
	if resp := putGroups(t, srv, huge, adminToken); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestAnEmptyMembersObjectIsAValidSnapshot(t *testing.T) {
	srv := newServer(t)
	if resp := putGroups(t, srv, `{"members":{}}`, adminToken); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the IdP may say nobody is in anything", resp.StatusCode)
	}
}
```

Append to `internal/handler/bundle_test.go`:

```go
func TestBundleGroupsAreTheUnionOfAuthoredAndSynced(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	before := fetchBundle(t, srv, alice, nil)
	beforeETag := before.Header.Get("ETag")

	// The IdP says alice is also mobile; the authored map keeps platform.
	if resp := putGroups(t, srv, `{"members":{"alice@acme.com":["mobile","platform"]}}`, adminToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("put groups: %d", resp.StatusCode)
	}

	resp := fetchBundle(t, srv, alice, http.Header{"If-None-Match": {beforeETag}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; a changed membership must change the ETag and defeat the 304", resp.StatusCode)
	}
	b := decodeBundle(t, resp)
	if !equal(b.Groups, []string{"mobile", "platform"}) {
		t.Errorf("groups = %v, want the sorted union", b.Groups)
	}
	names := make([]string, 0)
	for _, r := range b.Rules {
		names = append(names, r.Name)
	}
	if want := []string{"baseline", "platform", "mobile", "payments"}; !equal(names, want) {
		t.Errorf("rules = %v, want %v", names, want)
	}
}

func TestBundleWithAnEmptySnapshotIsTheAuthoredBundle(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	authored := decodeBundle(t, fetchBundle(t, srv, alice, nil))

	putGroups(t, srv, `{"members":{}}`, adminToken)

	synced := decodeBundle(t, fetchBundle(t, srv, alice, nil))
	if !equal(synced.Groups, authored.Groups) || len(synced.Rules) != len(authored.Rules) {
		t.Errorf("bundle changed under an empty snapshot: %+v vs %+v", synced, authored)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/handler/ -run 'Groups|Snapshot'`
Expected: the `PUT`/`GET` tests get 404/405 (route absent); the union test fails on groups.

- [ ] **Step 3: Routes and handlers**

In `internal/handler/handler.go`, after the `DELETE /v1/machines/{id}` route, add:

```go
	mux.HandleFunc("PUT /v1/groups", h.requireAdmin(h.putGroups))
	mux.HandleFunc("GET /v1/groups", h.requireAdmin(h.getGroups))
```

Create `internal/handler/groups.go`:

```go
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// maxGroupsBody bounds a membership snapshot. An organization of tens of
// thousands of users with a handful of groups each is a few megabytes; the
// cap is for a runaway exporter, not a real fleet.
const maxGroupsBody = 8 << 20

// groupsRequest is the body of PUT /v1/groups. Unknown fields are rejected
// so a misspelled key fails loudly rather than posting an empty snapshot.
type groupsRequest struct {
	Source  string              `json:"source"`
	Members map[string][]string `json:"members"`
}

// groupsSummary is what both routes answer with: enough for an operator to
// confirm what the server holds without reading the members.
type groupsSummary struct {
	Source    string              `json:"source"`
	AppliedBy string              `json:"appliedBy,omitempty"`
	SyncedAt  time.Time           `json:"syncedAt"`
	Users     int                 `json:"users"`
	Groups    int                 `json:"groups"`
	Members   map[string][]string `json:"members,omitempty"`
}

func summarise(s model.GroupSnapshot, withMembers bool) groupsSummary {
	distinct := make(map[string]bool)
	for _, groups := range s.Members {
		for _, g := range groups {
			distinct[g] = true
		}
	}
	out := groupsSummary{Source: s.Source, AppliedBy: s.AppliedBy, SyncedAt: s.SyncedAt, Users: len(s.Members), Groups: len(distinct)}
	if withMembers {
		out.Members = s.Members
		if out.Members == nil {
			out.Members = map[string][]string{}
		}
	}
	return out
}

// putGroups stores an IdP membership snapshot. It replaces the previous
// snapshot whole; the authored groups map in the policy still unions with
// it, so a snapshot can only add memberships an author did not write.
func (h *Handler) putGroups(w http.ResponseWriter, r *http.Request) {
	var req groupsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGroupsBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the snapshot exceeds %d bytes", maxGroupsBody))
			return
		}
		writeError(w, http.StatusBadRequest, "the request body is not a valid group snapshot: "+err.Error())
		return
	}
	if req.Members == nil {
		writeError(w, http.StatusUnprocessableEntity, "the snapshot has no members object")
		return
	}
	snapshot := model.GroupSnapshot{
		Source:    req.Source,
		AppliedBy: r.Header.Get("X-Applied-By"),
		SyncedAt:  h.Now(),
		Members:   req.Members,
	}
	if err := snapshot.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.store.PutGroupSnapshot(r.Context(), snapshot); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, summarise(snapshot, false))
}

// getGroups shows the current snapshot, members included, for an operator
// checking what the server resolves against.
func (h *Handler) getGroups(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.store.CurrentGroupSnapshot(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, summarise(snapshot, true))
}
```

Note: a JSON value that is not a list of strings for a member (`"platform"` or `[1]`) fails `Decode` into `map[string][]string` and answers 400; the test accepts 400 or 422 for the malformed cases.

- [ ] **Step 4: The union in `getBundle`**

In `internal/handler/bundle.go`, replace

```go
	bundle := ruleSet.Slice(ruleSet.GroupsFor(machine.User))
```

with

```go
	// Group resolution is the union of what the policy authored and what the
	// identity provider last exported; with no snapshot yet, the authored
	// map alone, exactly as before the sync existed.
	var synced []string
	snapshot, err := h.store.CurrentGroupSnapshot(r.Context())
	switch {
	case err == nil:
		synced = snapshot.Members[machine.User]
	case errors.Is(err, model.ErrNotFound):
	default:
		h.fail(w, r, err)
		return
	}
	bundle := ruleSet.Slice(policy.UnionGroups(ruleSet.GroupsFor(machine.User), synced))
```

`errors`, `model` and `policy` are already imported there.

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/handler/ -count=1`
Expected: PASS, including every pre-existing bundle test (no snapshot → unchanged behaviour). The `TestBundleGroupsAreTheUnionOfAuthoredAndSynced` expected rule order is authored order in `groupedRuleSet`: `baseline, platform, mobile, payments`.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./... && git add internal/handler
git commit -m "feat(awd): PUT/GET /v1/groups membership snapshots; bundles resolve the union of authored and synced groups

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 3: `awd groups apply` / `awd groups`; example; README

**Files:**
- Modify: `cmd/awd/main.go` (usage, `run`, `adminRequest`, new `groups`, `groupsApply`)
- Modify: `cmd/awd/e2e_test.go` (two tests)
- Create: `examples/groups.yaml`
- Modify: `README.md`

**Interfaces:**
- Consumes: the routes and summary shape from Task 2; `appliedBy()`, `adminRequest`, `parseAdminArgs` (existing).

- [ ] **Step 1: Write the failing e2e tests**

Append to `cmd/awd/e2e_test.go`:

```go
const groupsYAML = `
source: okta-export
members:
  alice@acme.com: [mobile]
  carol@acme.com: [platform]
`

func TestGroupsApplyThenTheBundleShowsTheUnion(t *testing.T) {
	s := startServer(t)
	if out, code := runAwd(t, "apply", writePolicy(t, groupedPolicyYAML), "--url", s.url); code != 0 {
		t.Fatalf("apply: %s", out)
	}
	path := writePolicy(t, groupsYAML)

	out, code := runAwd(t, "groups", "apply", path, "--url", s.url)
	if code != 0 || !strings.Contains(out, "okta-export") || !strings.Contains(out, "2 users") {
		t.Fatalf("groups apply exited %d: %s", code, out)
	}

	// alice is platform by the policy and mobile by the IdP: both rules apply.
	out, code = runAwd(t, "enroll-token", "alice@acme.com", "--url", s.url)
	if code != 0 {
		t.Fatal(out)
	}
	resp, err := http.Post(s.url+"/v1/machines/enroll", "application/json",
		strings.NewReader(`{"token":"`+strings.TrimSpace(out)+`","name":"h","os":"linux"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var enrolled struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&enrolled); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, s.url+"/v1/bundle", nil)
	req.Header.Set("Authorization", "Bearer "+enrolled.Credential)
	bundleResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer bundleResp.Body.Close()
	var bundle struct {
		Groups []string `json:"groups"`
		Rules  []struct{ Name string }
	}
	if err := json.NewDecoder(bundleResp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	if strings.Join(bundle.Groups, ",") != "mobile,platform" || len(bundle.Rules) != 3 {
		t.Errorf("bundle = %+v, want groups mobile,platform and baseline+platform+mobile rules", bundle)
	}

	summary, code := runAwd(t, "groups", "--url", s.url)
	if code != 0 || !strings.Contains(summary, "okta-export") || !strings.Contains(summary, "users: 2") {
		t.Errorf("groups exited %d: %s", code, summary)
	}
}

func TestGroupsWithoutASnapshotSaysSo(t *testing.T) {
	s := startServer(t)
	out, code := runAwd(t, "groups", "--url", s.url)
	if code != 0 || !strings.Contains(out, "no group snapshot") {
		t.Errorf("exit %d, output %q", code, out)
	}
	if out, code := runAwdEnv(t, []string{"AWD_ADMIN_TOKEN="}, "groups", "apply", writePolicy(t, groupsYAML), "--url", s.url); code == 0 {
		t.Errorf("groups apply without the admin token exited 0: %s", out)
	}
}
```

`groupedPolicyYAML` already defines `alice@acme.com: [platform]` and rules `baseline`, `platform`, `mobile`, so the union yields three rules.

Run: `go test ./cmd/awd/ -run TestGroups`
Expected: FAIL — `unknown command "groups"`.

- [ ] **Step 2: The subcommands**

In `cmd/awd/main.go`:

Usage text: add the lines `  awd groups apply FILE [--url URL]   push an identity-provider membership snapshot` and `  awd groups [--url URL]              show the current membership snapshot` beside the other admin commands (match the file's existing alignment).

In `run`, add before `default:`:

```go
	case "groups":
		if len(argv) > 1 && argv[1] == "apply" {
			return groupsApply(argv[2:])
		}
		return groups(argv[1:])
```

In `adminRequest`, after the `Authorization` header block, add:

```go
	if who := appliedBy(); who != "" {
		req.Header.Set("X-Applied-By", who)
	}
```

Add these functions near `machines`:

```go
// groupSummary is what /v1/groups answers with.
type groupSummary struct {
	Source    string    `json:"source"`
	AppliedBy string    `json:"appliedBy"`
	SyncedAt  time.Time `json:"syncedAt"`
	Users     int       `json:"users"`
	Groups    int       `json:"groups"`
}

// groupsApply posts a membership snapshot from a JSON or YAML file. The
// file is decoded locally first so a malformed export is reported with its
// path rather than as a status code.
func groupsApply(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("groups apply takes one argument: the snapshot file")
	}
	raw, err := os.ReadFile(a.positional[0])
	if err != nil {
		return err
	}
	var body struct {
		Source  string              `json:"source"`
		Members map[string][]string `json:"members"`
	}
	if err := yaml.UnmarshalStrict(raw, &body); err != nil {
		return fmt.Errorf("%s: %w", a.positional[0], err)
	}
	if body.Members == nil {
		return fmt.Errorf("%s: no members map", a.positional[0])
	}
	var summary groupSummary
	if err := adminRequest(http.MethodPut, a.url, "/v1/groups", body, &summary); err != nil {
		return err
	}
	fmt.Printf("applied group snapshot from %s: %d users, %d groups\n", orUnknownSource(summary.Source), summary.Users, summary.Groups)
	return nil
}

// groups prints the current snapshot's summary; none is a fact, not an error.
func groups(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 0 {
		return errors.New("groups takes no arguments; did you mean `groups apply FILE`?")
	}
	var summary groupSummary
	err = adminRequest(http.MethodGet, a.url, "/v1/groups", nil, &summary)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			fmt.Println("no group snapshot has been applied; bundles resolve from the policy's groups map alone")
			return nil
		}
		return err
	}
	fmt.Printf("source:   %s\nsynced:   %s\nby:       %s\nusers:    %d\ngroups:   %d\n",
		orUnknownSource(summary.Source), summary.SyncedAt.Format(time.RFC3339), orNoneString(summary.AppliedBy), summary.Users, summary.Groups)
	return nil
}

func orUnknownSource(s string) string {
	if s == "" {
		return "(unnamed source)"
	}
	return s
}

func orNoneString(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
```

Import `sigs.k8s.io/yaml` (already in `go.mod`; `LoadRuleSet` uses it). `go test` needs the `"users: 2"` and `"2 users"` substrings the e2e asserts — they come from the two `Printf`s above.

- [ ] **Step 3: Run the awd tests**

Run: `go test ./cmd/awd/ -count=1`
Expected: PASS.

- [ ] **Step 4: Example and README**

Create `examples/groups.yaml`:

```yaml
# An identity-provider membership snapshot, as `awd groups apply` posts it.
# Keys are users exactly as their machines enrolled them; values are the
# group names the policy's match.groups refer to. A snapshot replaces the
# previous one whole and unions with the policy's own groups map, so an
# entry authored in the policy survives whatever the export says.
source: okta-export
members:
  alice@acme.com: [platform, oncall]
  bob@acme.com: [mobile]
```

In `README.md`, in the control-plane section (near where `awd enroll-token`/`machines` are described; if that is the Try-it section, put it after the enroll-token line), add:

```markdown
Group membership has two sources. The policy's `groups` map is authored and
reviewed with the rules; it is the manual override. An identity-provider
snapshot is what the IdP says, posted whole by whatever export the operator
already trusts:

    awd groups apply examples/groups.yaml --url http://127.0.0.1:8080
    awd groups --url http://127.0.0.1:8080

A machine's bundle resolves its user's groups as the union of the two, so
a membership that must go away is removed from the source that added it.
User keys match the enrolled email exactly; there is no case folding. The
bundle's ETag covers the resolved rules, so a new snapshot reaches every
affected machine on its next `aw-sync` cycle.
```

Also add `examples/groups.yaml` to the README's Layout under `examples/` if that directory is listed with its files; otherwise leave the Layout alone.

- [ ] **Step 5: Verify and commit**

Run: `go test ./... -count=1 && go vet ./... && gofmt -l .`
Expected: all green, no gofmt output.

```bash
git add cmd/awd/main.go cmd/awd/e2e_test.go examples/groups.yaml README.md
git commit -m "feat(awd): groups apply and groups subcommands push and show the IdP membership snapshot

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

## Self-review

**Spec coverage.** Data model and store → Task 1 (struct, `Validate`, two methods, both stores, migration, five conformance cases). Resolution (`UnionGroups`, no-snapshot unchanged, exact keys, ETag) → Task 1 (`UnionGroups` + tests) and Task 2 (`getBundle`, union test with ETag, empty-snapshot test). API (503/401 via `requireAdmin`; 422 shapes; 413; 200 summary; `GET` 404/200) → Task 2 (handler + tests; the 503 case is covered by the existing `TestAdminRoutesAre503WhenNoTokenIsConfigured` pattern — extend it with `/v1/groups` if it enumerates routes). CLI → Task 3 (`groups apply`, `groups`, `AppliedBy` via `adminRequest`, no-snapshot exit 0). Failure modes: each row maps to a test named above except "store write fails → 500", which is `h.fail`'s existing default branch. Documentation → Task 3; the parent-spec pointer is already committed.

**Placeholders.** None.

**Type consistency.** `model.GroupSnapshot{Seq, Source, AppliedBy, SyncedAt, Members}` and its `Validate` (Task 1) are used by both stores (Task 1) and `putGroups` (Task 2). `Store.PutGroupSnapshot`/`CurrentGroupSnapshot` names match between interface, both implementations, conformance and handlers. `policy.UnionGroups` (Task 1) is called in `bundle.go` (Task 2). The summary JSON keys (`source`, `appliedBy`, `syncedAt`, `users`, `groups`, `members`) match between `handler.groupsSummary` (Task 2), the handler test's `groupSummary`, and `cmd/awd`'s `groupSummary` (Task 3).
