# SCIM Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `awd` serves a SCIM 2.0 endpoint that Okta and Entra ID provision users and groups into, and bundles resolve a user's groups as authored ∪ snapshot ∪ SCIM.

**Architecture:** A protocol-only package `internal/scim` (wire types, filter parser, PATCH applier, SCIM errors, discovery documents) sits between a new handler file `internal/handler/scim.go` and new `store.Store` methods backed by three normalized tables. Bundle resolution gains one store lookup, `SCIMGroupsFor`. Admin visibility adds SCIM counts to `GET /v1/groups` and a new `GET /v1/groups/resolve`.

**Tech Stack:** Go 1.22 (`net/http` ServeMux patterns, `min` builtin), pgx v5, `encoding/json`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-24-scim-provisioning-design.md`

## Global Constraints

- Module path `github.com/acme/agent-wrapper`; Go 1.22; no new modules in `go.mod`.
- SCIM auth: `Authorization: Bearer $AWD_SCIM_TOKEN`; unset → 503 on every `/scim/v2` route; wrong → 401; `subtle.ConstantTimeCompare`.
- SCIM responses use `Content-Type: application/scim+json` and the SCIM error body `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"<code as string>","scimType":"…","detail":"…"}`.
- SCIM request bodies capped at 1 MiB (`1 << 20`); 413 beyond.
- List paging: `startIndex` 1-based default 1; `count` default 100, max 1000; order `Created` then `ID`.
- `userName` and `displayName` unique case-insensitively (`strings.ToLower` in memory, `lower()` in Postgres).
- Resolution matches `SCIMUser.UserName` to the enrolled user **exactly**. Only active users contribute SCIM groups.
- Deprovisioning never revokes a machine.
- With no SCIM data, bundles are byte-identical to today (same ETag).
- Resource IDs come from `credential.NewID()` (32 hex chars). The spec says "UUID"; any unique string satisfies SCIM and both IdPs — Task 10 amends the spec line.
- Supported filters: `userName eq "…"`, `externalId eq "…"` on Users; `displayName eq "…"`, `externalId eq "…"` on Groups. Anything else is 400 `invalidFilter`.
- `go test -race` cannot run here (no gcc). Run `go test ./...` and `go vet ./...`.
- Postgres conformance: `docker run -d --name awd-pg -e POSTGRES_PASSWORD=pw -p 5432:5432 postgres:16`, then `AWD_TEST_DATABASE_URL='postgres://postgres:pw@localhost:5432/postgres?sslmode=disable' go test ./internal/store/`.
- Commit messages: conventional prefix (`feat(scim):`, `feat(store):`, …), ending with the line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Comments match the repo: full sentences that say *why*, doc comment on every exported identifier.

## File Map

| File | Responsibility |
| --- | --- |
| `internal/policy/groups.go` (modify) | variadic `UnionGroups` |
| `internal/model/scim.go` (create) | `SCIMUser`, `SCIMGroup`, filter/change/count types, validation |
| `internal/store/store.go` (modify) | interface additions |
| `internal/store/memory_scim.go` (create) | memory implementation |
| `internal/store/storetest/scim.go` (create) | SCIM conformance cases, registered from `conformance.go` |
| `internal/store/migrations/0005_scim.sql` (create) | tables and indexes |
| `internal/store/postgres_scim.go` (create) | Postgres implementation |
| `internal/scim/scim.go` (create) | URNs, `Error`, wire types, encode/decode |
| `internal/scim/filter.go` (create) | `ParseFilter` |
| `internal/scim/patch.go` (create) | `UserPatch`, `GroupPatch` |
| `internal/scim/discovery.go` (create) | ServiceProviderConfig, ResourceTypes, Schemas |
| `internal/handler/scim.go` (create) | `/scim/v2` routes |
| `internal/handler/resolve.go` (create) | shared group resolution, `GET /v1/groups/resolve` |
| `internal/handler/bundle.go`, `groups.go`, `handler.go` (modify) | use resolution, SCIM counts, routes, `SCIMToken` |
| `internal/config/config.go`, `cmd/awd/main.go` (modify) | `AWD_SCIM_TOKEN`, CLI output, `groups resolve` |
| `README.md`, specs (modify) | docs |

---

### Task 1: Variadic `UnionGroups`

**Files:**
- Modify: `internal/policy/groups.go`
- Test: `internal/policy/groups_test.go`

**Interfaces:**
- Produces: `func UnionGroups(lists ...[]string) []string` — sorted, de-duplicated, never nil. Existing two-argument calls compile unchanged.

- [ ] **Step 1: Write the failing tests** (append to `internal/policy/groups_test.go`)

```go
func TestUnionGroupsOfThreeSources(t *testing.T) {
	got := policy.UnionGroups([]string{"platform"}, nil, []string{"oncall", "platform", "mobile"})
	if want := []string{"mobile", "oncall", "platform"}; !equal(got, want) {
		t.Errorf("UnionGroups = %v, want %v", got, want)
	}
}

func TestUnionGroupsOfNoListsIsAnEmptySliceNotNil(t *testing.T) {
	got := policy.UnionGroups()
	if got == nil || len(got) != 0 {
		t.Errorf("UnionGroups() = %#v, want an empty, non-nil slice", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/policy/ -run UnionGroups`
Expected: compile error, `too many arguments in call to policy.UnionGroups`.

- [ ] **Step 3: Implement** — replace the function in `internal/policy/groups.go`:

```go
// UnionGroups merges a user's memberships from every source — the authored
// map, the IdP snapshot, SCIM — sorted and without duplicates. Union, not
// replacement: the authored map is the manual override and nothing an
// author wrote disappears when an IdP starts feeding the server. The result
// is never nil, since it is JSON-encoded as a list.
func UnionGroups(lists ...[]string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, list := range lists {
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

- [ ] **Step 4: Run tests**

Run: `go test ./internal/policy/ ./internal/handler/`
Expected: PASS (handler still calls it with two arguments).

- [ ] **Step 5: Commit**

```bash
git add internal/policy/groups.go internal/policy/groups_test.go
git commit -m "feat(policy): union any number of membership sources

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: SCIM model, store interface, memory store, conformance suite

**Files:**
- Create: `internal/model/scim.go`
- Modify: `internal/store/store.go` (new `SCIMStore` interface; `Store` embeds it in Task 3)
- Create: `internal/store/memory_scim.go`
- Modify: `internal/store/memory.go` (three map fields + `NewMemory` init)
- Create: `internal/store/storetest/scim.go`
- Modify: `internal/store/memory_test.go` (`TestMemorySCIM`)

**Interfaces:**
- Consumes: `model.ErrBadInput`, `model.ErrConflict`, `model.ErrNotFound`.
- Produces (used by Tasks 3, 6, 7, 8):

```go
// model
type SCIMUser struct{ ID, UserName, ExternalID string; Active bool; Created, Modified time.Time }
type SCIMGroup struct{ ID, DisplayName, ExternalID string; Members []string; Created, Modified time.Time }
type SCIMFilter struct{ Attribute, Value string }          // zero value matches all
const SCIMAttrUserName, SCIMAttrDisplayName, SCIMAttrExternalID = "userName", "displayName", "externalId"
type SCIMUserChange struct{ UserName, ExternalID *string; Active *bool }
const SCIMMembersAdd, SCIMMembersRemove, SCIMMembersReplace = "add", "remove", "replace"
type SCIMMemberOp struct{ Kind string; Users []string }
type SCIMGroupChange struct{ DisplayName, ExternalID *string; Members []SCIMMemberOp }
type SCIMCounts struct{ Users, ActiveUsers, Groups int } // json users, activeUsers, groups
func (u SCIMUser) Validate() error
func (g SCIMGroup) Validate() error

// store.SCIMStore (embedded into store.Store in Task 3)
CreateSCIMUser(ctx, u model.SCIMUser) error
SCIMUser(ctx, id string) (model.SCIMUser, error)
ReplaceSCIMUser(ctx, u model.SCIMUser) error
PatchSCIMUser(ctx, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error)
DeleteSCIMUser(ctx, id string) error
ListSCIMUsers(ctx, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error)
CreateSCIMGroup(ctx, g model.SCIMGroup) error
SCIMGroup(ctx, id string) (model.SCIMGroup, error)
ReplaceSCIMGroup(ctx, g model.SCIMGroup) error
PatchSCIMGroup(ctx, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error)
DeleteSCIMGroup(ctx, id string) error
ListSCIMGroups(ctx, f model.SCIMFilter, startIndex, count int) ([]model.SCIMGroup, int, error)
SCIMGroupsFor(ctx, userName string) ([]string, error)
SCIMCounts(ctx) (model.SCIMCounts, error)

// storetest
func RunSCIM(t *testing.T, newStore func(t *testing.T) store.SCIMStore)
```

- [ ] **Step 1: Write the model** — `internal/model/scim.go`:

```go
package model

import (
	"fmt"
	"time"
)

// SCIMUser is a user as the identity provider provisioned it over SCIM.
// Resolution matches UserName exactly against the enrolled user; an
// inactive user keeps its memberships but contributes no groups, so a
// reactivation restores them without the IdP re-pushing anything.
type SCIMUser struct {
	ID         string    `json:"id"`
	UserName   string    `json:"userName"`
	ExternalID string    `json:"externalId,omitempty"`
	Active     bool      `json:"active"`
	Created    time.Time `json:"created"`
	Modified   time.Time `json:"modified"`
}

// Validate reports whether the user can be stored.
func (u SCIMUser) Validate() error {
	if u.ID == "" {
		return fmt.Errorf("scim user has no id: %w", ErrBadInput)
	}
	if u.UserName == "" {
		return fmt.Errorf("scim user %q has no userName: %w", u.ID, ErrBadInput)
	}
	return nil
}

// SCIMGroup is a provisioned group. DisplayName is the group name policies
// target; Members are SCIMUser IDs, sorted when read from a store.
type SCIMGroup struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	ExternalID  string    `json:"externalId,omitempty"`
	Members     []string  `json:"members"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
}

// Validate reports whether the group can be stored. Whether each member
// exists is the store's check, since only it can see the users.
func (g SCIMGroup) Validate() error {
	if g.ID == "" {
		return fmt.Errorf("scim group has no id: %w", ErrBadInput)
	}
	if g.DisplayName == "" {
		return fmt.Errorf("scim group %q has no displayName: %w", g.ID, ErrBadInput)
	}
	for _, m := range g.Members {
		if m == "" {
			return fmt.Errorf("scim group %q has an empty member id: %w", g.ID, ErrBadInput)
		}
	}
	return nil
}

// The attributes a SCIM list may be filtered on.
const (
	SCIMAttrUserName    = "userName"
	SCIMAttrDisplayName = "displayName"
	SCIMAttrExternalID  = "externalId"
)

// SCIMFilter selects list results by one attribute equal to one value. The
// zero value matches everything. Names compare case-insensitively, as the
// core schema declares them; externalId compares exactly.
type SCIMFilter struct {
	Attribute string
	Value     string
}

// SCIMUserChange is a PATCH on a user folded into the fields it sets; a nil
// field is left as it is.
type SCIMUserChange struct {
	UserName   *string
	ExternalID *string
	Active     *bool
}

// The kinds of member operation a group PATCH carries.
const (
	SCIMMembersAdd     = "add"
	SCIMMembersRemove  = "remove"
	SCIMMembersReplace = "replace"
)

// SCIMMemberOp is one member operation, applied in the order the IdP sent
// it: a replace followed by an add is not the same as the reverse.
type SCIMMemberOp struct {
	Kind  string
	Users []string
}

// SCIMGroupChange is a PATCH on a group. The store applies it in one
// transaction, so two concurrent member changes cannot lose a write.
type SCIMGroupChange struct {
	DisplayName *string
	ExternalID  *string
	Members     []SCIMMemberOp
}

// SCIMCounts summarises what SCIM has provisioned, for an operator.
type SCIMCounts struct {
	Users       int `json:"users"`
	ActiveUsers int `json:"activeUsers"`
	Groups      int `json:"groups"`
}
```

- [ ] **Step 2: Declare the interface** — append to `internal/store/store.go`. It is its own interface for now so the tree keeps building while only the memory store implements it; Task 3 embeds it into `Store` once Postgres does too.

```go
// SCIMStore holds what the identity provider provisioned over SCIM.
type SCIMStore interface {
	// CreateSCIMUser stores a provisioned user. model.ErrBadInput without an
	// ID or userName; model.ErrConflict when the ID exists or another user
	// has the same userName ignoring case.
	CreateSCIMUser(ctx context.Context, u model.SCIMUser) error
	// SCIMUser returns one user, or model.ErrNotFound.
	SCIMUser(ctx context.Context, id string) (model.SCIMUser, error)
	// ReplaceSCIMUser overwrites userName, externalId, active and Modified;
	// Created is kept. Errors as CreateSCIMUser, plus model.ErrNotFound.
	ReplaceSCIMUser(ctx context.Context, u model.SCIMUser) error
	// PatchSCIMUser applies c atomically, sets Modified to at and returns the
	// result. Errors as ReplaceSCIMUser.
	PatchSCIMUser(ctx context.Context, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error)
	// DeleteSCIMUser removes a user and every membership it had, or
	// model.ErrNotFound.
	DeleteSCIMUser(ctx context.Context, id string) error
	// ListSCIMUsers returns one page of the users f matches, ordered by
	// Created then ID, and the total number matched. startIndex is 1-based;
	// a page past the end is empty, never nil. model.ErrBadInput for a
	// filter attribute users do not have.
	ListSCIMUsers(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error)

	// CreateSCIMGroup stores a group. model.ErrBadInput without an ID or
	// displayName, or when a member is not a stored user; model.ErrConflict
	// when the ID exists or another group has the displayName ignoring case.
	CreateSCIMGroup(ctx context.Context, g model.SCIMGroup) error
	// SCIMGroup returns one group with its members sorted, or
	// model.ErrNotFound.
	SCIMGroup(ctx context.Context, id string) (model.SCIMGroup, error)
	// ReplaceSCIMGroup overwrites displayName, externalId, members and
	// Modified. Errors as CreateSCIMGroup, plus model.ErrNotFound.
	ReplaceSCIMGroup(ctx context.Context, g model.SCIMGroup) error
	// PatchSCIMGroup applies c in one transaction, member operations in
	// order, sets Modified to at and returns the result. Errors as
	// ReplaceSCIMGroup; on any error nothing changes.
	PatchSCIMGroup(ctx context.Context, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error)
	// DeleteSCIMGroup removes a group and its memberships, or
	// model.ErrNotFound.
	DeleteSCIMGroup(ctx context.Context, id string) error
	// ListSCIMGroups is ListSCIMUsers for groups, members included.
	ListSCIMGroups(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMGroup, int, error)

	// SCIMGroupsFor returns the sorted display names of the groups holding
	// the active user whose userName equals userName exactly; an empty,
	// non-nil slice for an unknown or inactive user.
	SCIMGroupsFor(ctx context.Context, userName string) ([]string, error)
	// SCIMCounts counts provisioned users, active users and groups.
	SCIMCounts(ctx context.Context) (model.SCIMCounts, error)
}
```

- [ ] **Step 3: Write the conformance cases** — create `internal/store/storetest/scim.go`:

```go
package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

// RunSCIM executes the SCIM conformance cases against the store the
// factory builds. It is separate from Run while SCIMStore is its own
// interface; once Store embeds it, Run calls it too.
func RunSCIM(t *testing.T, newStore func(t *testing.T) store.SCIMStore) {
	t.Helper()
	for _, tc := range scimCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
}

// scimCases is the SCIM part of the conformance suite.
var scimCases = []struct {
	name string
	fn   func(t *testing.T, s store.SCIMStore)
}{
	{"a scim user can be created and read back", scimUserRoundTrip},
	{"a scim user needs an id and a userName", scimUserRequired},
	{"a scim userName is unique ignoring case", scimUserNameUnique},
	{"replacing a scim user keeps its created time", scimUserReplace},
	{"patching a scim user changes only what it names", scimUserPatch},
	{"a patch cannot take another user's userName", scimUserPatchConflict},
	{"an unknown scim user is not found", scimUserUnknown},
	{"deleting a scim user removes its memberships", scimUserDeleteCascades},
	{"scim users list by filter and page stably", scimUserList},
	{"a scim group can be created with members and read back", scimGroupRoundTrip},
	{"a scim group member must be a stored user", scimGroupMemberMustExist},
	{"a scim displayName is unique ignoring case", scimGroupNameUnique},
	{"a group patch applies member operations in order", scimGroupPatchOrder},
	{"a failed group patch changes nothing", scimGroupPatchAtomic},
	{"concurrent member patches lose nothing", scimGroupPatchConcurrent},
	{"deleting a scim group removes its memberships", scimGroupDelete},
	{"scim groups list by filter", scimGroupList},
	{"scim groups for a user are those of the active user only", scimGroupsForActiveOnly},
	{"scim groups for match the userName exactly", scimGroupsForExact},
	{"scim counts", scimCountsCase},
}

var scimAt = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

func scimUser(id, name string, created time.Time) model.SCIMUser {
	return model.SCIMUser{ID: id, UserName: name, ExternalID: "ext-" + id, Active: true, Created: created, Modified: created}
}

func scimGroup(id, name string, members ...string) model.SCIMGroup {
	return model.SCIMGroup{ID: id, DisplayName: name, Members: members, Created: scimAt, Modified: scimAt}
}

func mustCreateUsers(t *testing.T, s store.Store, users ...model.SCIMUser) {
	t.Helper()
	for _, u := range users {
		if err := s.CreateSCIMUser(context.Background(), u); err != nil {
			t.Fatalf("creating %s: %v", u.ID, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func scimUserRoundTrip(t *testing.T, s store.SCIMStore) {
	want := scimUser("u1", "alice@acme.com", scimAt)
	mustCreateUsers(t, s, want)
	got, err := s.SCIMUser(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.UserName != want.UserName || got.ExternalID != want.ExternalID || !got.Active ||
		!got.Created.Equal(scimAt) || !got.Modified.Equal(scimAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func scimUserRequired(t *testing.T, s store.SCIMStore) {
	for _, u := range []model.SCIMUser{scimUser("", "a@acme.com", scimAt), scimUser("u1", "", scimAt)} {
		if err := s.CreateSCIMUser(context.Background(), u); !errors.Is(err, model.ErrBadInput) {
			t.Errorf("create %+v: err = %v, want ErrBadInput", u, err)
		}
	}
}

func scimUserNameUnique(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	if err := s.CreateSCIMUser(context.Background(), scimUser("u2", "Alice@ACME.com", scimAt)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("same name in another case: err = %v, want ErrConflict", err)
	}
	if err := s.CreateSCIMUser(context.Background(), scimUser("u1", "bob@acme.com", scimAt)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("same id: err = %v, want ErrConflict", err)
	}
}

func scimUserReplace(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	later := scimAt.Add(time.Hour)
	replacement := model.SCIMUser{ID: "u1", UserName: "alice@acme.io", Active: false, Created: later, Modified: later}
	if err := s.ReplaceSCIMUser(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	got, _ := s.SCIMUser(context.Background(), "u1")
	if got.UserName != "alice@acme.io" || got.Active || got.ExternalID != "" ||
		!got.Created.Equal(scimAt) || !got.Modified.Equal(later) {
		t.Errorf("got %+v", got)
	}
	if err := s.ReplaceSCIMUser(context.Background(), scimUser("nope", "x@acme.com", scimAt)); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("replace unknown: err = %v, want ErrNotFound", err)
	}
}

func scimUserPatch(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	later := scimAt.Add(time.Minute)
	got, err := s.PatchSCIMUser(context.Background(), "u1", model.SCIMUserChange{Active: ptr(false)}, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Active || got.UserName != "alice@acme.com" || got.ExternalID != "ext-u1" || !got.Modified.Equal(later) {
		t.Errorf("got %+v", got)
	}
	if _, err := s.PatchSCIMUser(context.Background(), "nope", model.SCIMUserChange{}, later); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("patch unknown: err = %v, want ErrNotFound", err)
	}
}

func scimUserPatchConflict(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt), scimUser("u2", "bob@acme.com", scimAt))
	_, err := s.PatchSCIMUser(context.Background(), "u2", model.SCIMUserChange{UserName: ptr("ALICE@acme.com")}, scimAt)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
	// Renaming a user to its own name in another case is not a clash.
	if _, err := s.PatchSCIMUser(context.Background(), "u1", model.SCIMUserChange{UserName: ptr("Alice@acme.com")}, scimAt); err != nil {
		t.Errorf("recasing one's own name: %v", err)
	}
}

func scimUserUnknown(t *testing.T, s store.SCIMStore) {
	if _, err := s.SCIMUser(context.Background(), "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("get: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSCIMUser(context.Background(), "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("delete: err = %v, want ErrNotFound", err)
	}
}

func scimUserDeleteCascades(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt), scimUser("u2", "bob@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1", "u2")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSCIMUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	g, _ := s.SCIMGroup(ctx, "g1")
	if !equalStrings(g.Members, []string{"u2"}) {
		t.Errorf("members = %v, want [u2]", g.Members)
	}
	if _, err := s.SCIMUser(ctx, "u1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("deleted user still readable: %v", err)
	}
}

func scimUserList(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	// Created out of ID order, and two sharing an instant, to pin the order.
	mustCreateUsers(t, s,
		scimUser("u3", "carol@acme.com", scimAt),
		scimUser("u1", "alice@acme.com", scimAt),
		scimUser("u2", "bob@acme.com", scimAt.Add(-time.Hour)),
	)
	all, total, err := s.ListSCIMUsers(ctx, model.SCIMFilter{}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || !equalStrings(userIDs(all), []string{"u2", "u1", "u3"}) {
		t.Errorf("all = %v (total %d), want u2,u1,u3 by created then id", userIDs(all), total)
	}
	second, total, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{}, 2, 1)
	if total != 3 || !equalStrings(userIDs(second), []string{"u1"}) {
		t.Errorf("page 2 of 1 = %v (total %d)", userIDs(second), total)
	}
	past, _, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{}, 10, 5)
	if past == nil || len(past) != 0 {
		t.Errorf("past the end = %#v, want empty non-nil", past)
	}
	byName, total, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrUserName, Value: "BOB@acme.com"}, 1, 100)
	if total != 1 || !equalStrings(userIDs(byName), []string{"u2"}) {
		t.Errorf("userName filter = %v", userIDs(byName))
	}
	byExt, _, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrExternalID, Value: "ext-u3"}, 1, 100)
	if !equalStrings(userIDs(byExt), []string{"u3"}) {
		t.Errorf("externalId filter = %v", userIDs(byExt))
	}
	caseExt, _, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrExternalID, Value: "EXT-U3"}, 1, 100)
	if len(caseExt) != 0 {
		t.Errorf("externalId compares exactly; got %v", userIDs(caseExt))
	}
	if _, _, err := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrDisplayName, Value: "x"}, 1, 100); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("displayName filter on users: err = %v, want ErrBadInput", err)
	}
}

func userIDs(users []model.SCIMUser) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.ID)
	}
	return out
}

func scimGroupRoundTrip(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u2", "bob@acme.com", scimAt), scimUser("u1", "alice@acme.com", scimAt))
	want := scimGroup("g1", "platform", "u2", "u1")
	want.ExternalID = "okta-g1"
	if err := s.CreateSCIMGroup(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.SCIMGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "platform" || got.ExternalID != "okta-g1" || !equalStrings(got.Members, []string{"u1", "u2"}) ||
		!got.Created.Equal(scimAt) {
		t.Errorf("got %+v; members must come back sorted", got)
	}
	empty := scimGroup("g2", "empty")
	if err := s.CreateSCIMGroup(ctx, empty); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroup(ctx, "g2"); got.Members == nil {
		t.Error("an empty group's members must be an empty slice, not nil")
	}
}

func scimGroupMemberMustExist(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "ghost")); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("create with ghost: err = %v, want ErrBadInput", err)
	}
	if _, err := s.SCIMGroup(ctx, "g1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("a rejected group must not be stored: %v", err)
	}
}

func scimGroupNameUnique(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSCIMGroup(ctx, scimGroup("g2", "Platform")); !errors.Is(err, model.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

func scimGroupPatchOrder(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "a@acme.com", scimAt), scimUser("u2", "b@acme.com", scimAt), scimUser("u3", "c@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	later := scimAt.Add(time.Minute)
	got, err := s.PatchSCIMGroup(ctx, "g1", model.SCIMGroupChange{
		DisplayName: ptr("platform-eng"),
		Members: []model.SCIMMemberOp{
			{Kind: model.SCIMMembersReplace, Users: []string{"u2"}},
			{Kind: model.SCIMMembersAdd, Users: []string{"u3", "u2"}},
			{Kind: model.SCIMMembersRemove, Users: []string{"u2", "u1"}},
		},
	}, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "platform-eng" || !equalStrings(got.Members, []string{"u3"}) || !got.Modified.Equal(later) {
		t.Errorf("got %+v, want platform-eng with [u3]", got)
	}
}

func scimGroupPatchAtomic(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "a@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	_, err := s.PatchSCIMGroup(ctx, "g1", model.SCIMGroupChange{
		DisplayName: ptr("renamed"),
		Members:     []model.SCIMMemberOp{{Kind: model.SCIMMembersAdd, Users: []string{"u1", "ghost"}}},
	}, scimAt)
	if !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("err = %v, want ErrBadInput", err)
	}
	got, _ := s.SCIMGroup(ctx, "g1")
	if got.DisplayName != "platform" || len(got.Members) != 0 {
		t.Errorf("got %+v; a failed patch must change nothing", got)
	}
}

func scimGroupPatchConcurrent(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	const n = 20
	for i := 0; i < n; i++ {
		mustCreateUsers(t, s, scimUser(fmt.Sprintf("u%02d", i), fmt.Sprintf("user%02d@acme.com", i), scimAt))
	}
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := s.PatchSCIMGroup(ctx, "g1", model.SCIMGroupChange{
				Members: []model.SCIMMemberOp{{Kind: model.SCIMMembersAdd, Users: []string{id}}},
			}, scimAt)
			errs <- err
		}(fmt.Sprintf("u%02d", i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.SCIMGroup(ctx, "g1")
	if len(got.Members) != n {
		t.Errorf("members = %d, want %d: a concurrent patch lost a write", len(got.Members), n)
	}
}

func scimGroupDelete(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSCIMGroup(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if groups, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); len(groups) != 0 {
		t.Errorf("groups after delete = %v", groups)
	}
	if err := s.DeleteSCIMGroup(ctx, "g1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
	// The name is free again.
	if err := s.CreateSCIMGroup(ctx, scimGroup("g2", "platform")); err != nil {
		t.Errorf("recreating a deleted name: %v", err)
	}
}

func scimGroupList(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	for _, g := range []model.SCIMGroup{scimGroup("g2", "mobile"), scimGroup("g1", "platform", "u1")} {
		if err := s.CreateSCIMGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	all, total, err := s.ListSCIMGroups(ctx, model.SCIMFilter{}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(all) != 2 || all[0].ID != "g1" || !equalStrings(all[0].Members, []string{"u1"}) {
		t.Errorf("all = %+v (total %d)", all, total)
	}
	byName, _, _ := s.ListSCIMGroups(ctx, model.SCIMFilter{Attribute: model.SCIMAttrDisplayName, Value: "MOBILE"}, 1, 100)
	if len(byName) != 1 || byName[0].ID != "g2" {
		t.Errorf("displayName filter = %+v", byName)
	}
	if _, _, err := s.ListSCIMGroups(ctx, model.SCIMFilter{Attribute: model.SCIMAttrUserName, Value: "x"}, 1, 100); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("userName filter on groups: err = %v, want ErrBadInput", err)
	}
}

func scimGroupsForActiveOnly(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	for _, g := range []model.SCIMGroup{scimGroup("g1", "platform", "u1"), scimGroup("g2", "oncall", "u1"), scimGroup("g3", "mobile")} {
		if err := s.CreateSCIMGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SCIMGroupsFor(ctx, "alice@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(got, []string{"oncall", "platform"}) {
		t.Errorf("groups = %v, want sorted [oncall platform]", got)
	}
	if _, err := s.PatchSCIMUser(ctx, "u1", model.SCIMUserChange{Active: ptr(false)}, scimAt); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); got == nil || len(got) != 0 {
		t.Errorf("inactive user's groups = %#v, want empty non-nil", got)
	}
	if _, err := s.PatchSCIMUser(ctx, "u1", model.SCIMUserChange{Active: ptr(true)}, scimAt); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); !equalStrings(got, []string{"oncall", "platform"}) {
		t.Errorf("reactivated user's groups = %v; memberships must survive deactivation", got)
	}
	if got, err := s.SCIMGroupsFor(ctx, "nobody@acme.com"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("unknown user: %#v, %v", got, err)
	}
}

func scimGroupsForExact(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "Alice@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); len(got) != 0 {
		t.Errorf("groups = %v; resolution must not fold case", got)
	}
}

func scimCountsCase(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	if got, err := s.SCIMCounts(ctx); err != nil || got != (model.SCIMCounts{}) {
		t.Errorf("empty counts = %+v, %v", got, err)
	}
	inactive := scimUser("u2", "bob@acme.com", scimAt)
	inactive.Active = false
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt), inactive)
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMCounts(ctx); got != (model.SCIMCounts{Users: 2, ActiveUsers: 1, Groups: 1}) {
		t.Errorf("counts = %+v", got)
	}
}
```

Append to `internal/store/memory_test.go`:

```go
// TestMemorySCIM runs the SCIM conformance cases against the memory store.
func TestMemorySCIM(t *testing.T) {
	storetest.RunSCIM(t, func(t *testing.T) store.SCIMStore {
		return store.NewMemory()
	})
}
```

- [ ] **Step 4: Run to verify failure**

Run: `go test ./internal/store/...`
Expected: compile error, `*store.Memory does not implement store.SCIMStore (missing method CreateSCIMGroup)`.

- [ ] **Step 5: Implement the memory store**

In `internal/store/memory.go`, add fields to `Memory`:

```go
	scimUsers   map[string]model.SCIMUser  // by id
	scimGroups  map[string]model.SCIMGroup // by id, Members unused
	scimMembers map[string]map[string]bool // group id -> user ids
```

and in `NewMemory`:

```go
		scimUsers:   make(map[string]model.SCIMUser),
		scimGroups:  make(map[string]model.SCIMGroup),
		scimMembers: make(map[string]map[string]bool),
```

Create `internal/store/memory_scim.go`:

```go
package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// CreateSCIMUser stores a provisioned user.
func (m *Memory) CreateSCIMUser(_ context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimUsers[u.ID]; ok {
		return fmt.Errorf("store: scim user %q already exists: %w", u.ID, model.ErrConflict)
	}
	if err := m.userNameFreeLocked(u.UserName, ""); err != nil {
		return err
	}
	m.scimUsers[u.ID] = u
	return nil
}

// userNameFreeLocked reports a conflict when a user other than except
// holds name ignoring case.
func (m *Memory) userNameFreeLocked(name, except string) error {
	for id, u := range m.scimUsers {
		if id != except && strings.ToLower(u.UserName) == strings.ToLower(name) {
			return fmt.Errorf("store: scim userName %q is taken: %w", name, model.ErrConflict)
		}
	}
	return nil
}

// SCIMUser returns one user.
func (m *Memory) SCIMUser(_ context.Context, id string) (model.SCIMUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.scimUsers[id]
	if !ok {
		return model.SCIMUser{}, fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	return u, nil
}

// ReplaceSCIMUser overwrites a user, keeping its Created time.
func (m *Memory) ReplaceSCIMUser(_ context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.scimUsers[u.ID]
	if !ok {
		return fmt.Errorf("store: scim user %q not found: %w", u.ID, model.ErrNotFound)
	}
	if err := m.userNameFreeLocked(u.UserName, u.ID); err != nil {
		return err
	}
	u.Created = old.Created
	m.scimUsers[u.ID] = u
	return nil
}

// PatchSCIMUser applies a change under the lock.
func (m *Memory) PatchSCIMUser(_ context.Context, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.scimUsers[id]
	if !ok {
		return model.SCIMUser{}, fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	u = applyUserChange(u, c, at)
	if err := u.Validate(); err != nil {
		return model.SCIMUser{}, fmt.Errorf("store: %w", err)
	}
	if err := m.userNameFreeLocked(u.UserName, id); err != nil {
		return model.SCIMUser{}, err
	}
	m.scimUsers[id] = u
	return u, nil
}

// applyUserChange sets the fields c names. Both stores use it, so a patch
// means the same thing whichever one runs it.
func applyUserChange(u model.SCIMUser, c model.SCIMUserChange, at time.Time) model.SCIMUser {
	if c.UserName != nil {
		u.UserName = *c.UserName
	}
	if c.ExternalID != nil {
		u.ExternalID = *c.ExternalID
	}
	if c.Active != nil {
		u.Active = *c.Active
	}
	u.Modified = at
	return u
}

// DeleteSCIMUser removes a user and its memberships.
func (m *Memory) DeleteSCIMUser(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimUsers[id]; !ok {
		return fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	delete(m.scimUsers, id)
	for _, members := range m.scimMembers {
		delete(members, id)
	}
	return nil
}

// ListSCIMUsers returns one page of matching users.
func (m *Memory) ListSCIMUsers(_ context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error) {
	var match func(model.SCIMUser) bool
	switch f.Attribute {
	case "":
		match = func(model.SCIMUser) bool { return true }
	case model.SCIMAttrUserName:
		match = func(u model.SCIMUser) bool { return strings.ToLower(u.UserName) == strings.ToLower(f.Value) }
	case model.SCIMAttrExternalID:
		match = func(u model.SCIMUser) bool { return u.ExternalID == f.Value }
	default:
		return nil, 0, fmt.Errorf("store: users cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]model.SCIMUser, 0)
	for _, u := range m.scimUsers {
		if match(u) {
			all = append(all, u)
		}
	}
	sort.Slice(all, func(i, j int) bool { return createdThenID(all[i].Created, all[i].ID, all[j].Created, all[j].ID) })
	return page(all, startIndex, count), len(all), nil
}

// createdThenID is the list order both stores use, so paging is stable.
func createdThenID(ci time.Time, idi string, cj time.Time, idj string) bool {
	if !ci.Equal(cj) {
		return ci.Before(cj)
	}
	return idi < idj
}

// page cuts one 1-based page out of all; past the end is empty, never nil.
func page[T any](all []T, startIndex, count int) []T {
	if startIndex < 1 {
		startIndex = 1
	}
	from := startIndex - 1
	if count <= 0 || from >= len(all) {
		return []T{}
	}
	return append([]T{}, all[from:min(from+count, len(all))]...)
}

// CreateSCIMGroup stores a group.
func (m *Memory) CreateSCIMGroup(_ context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimGroups[g.ID]; ok {
		return fmt.Errorf("store: scim group %q already exists: %w", g.ID, model.ErrConflict)
	}
	if err := m.displayNameFreeLocked(g.DisplayName, ""); err != nil {
		return err
	}
	members, err := m.memberSetLocked(g.Members)
	if err != nil {
		return err
	}
	g.Members = nil
	m.scimGroups[g.ID] = g
	m.scimMembers[g.ID] = members
	return nil
}

func (m *Memory) displayNameFreeLocked(name, except string) error {
	for id, g := range m.scimGroups {
		if id != except && strings.ToLower(g.DisplayName) == strings.ToLower(name) {
			return fmt.Errorf("store: scim displayName %q is taken: %w", name, model.ErrConflict)
		}
	}
	return nil
}

// memberSetLocked builds a member set, refusing an id that is not a user.
func (m *Memory) memberSetLocked(ids []string) (map[string]bool, error) {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, ok := m.scimUsers[id]; !ok {
			return nil, fmt.Errorf("store: scim member %q is not a user: %w", id, model.ErrBadInput)
		}
		set[id] = true
	}
	return set, nil
}

// groupLocked assembles a stored group with its sorted members.
func (m *Memory) groupLocked(id string) (model.SCIMGroup, bool) {
	g, ok := m.scimGroups[id]
	if !ok {
		return model.SCIMGroup{}, false
	}
	g.Members = make([]string, 0, len(m.scimMembers[id]))
	for member := range m.scimMembers[id] {
		g.Members = append(g.Members, member)
	}
	sort.Strings(g.Members)
	return g, true
}

// SCIMGroup returns one group.
func (m *Memory) SCIMGroup(_ context.Context, id string) (model.SCIMGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groupLocked(id)
	if !ok {
		return model.SCIMGroup{}, fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	return g, nil
}

// ReplaceSCIMGroup overwrites a group, keeping its Created time.
func (m *Memory) ReplaceSCIMGroup(_ context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.scimGroups[g.ID]
	if !ok {
		return fmt.Errorf("store: scim group %q not found: %w", g.ID, model.ErrNotFound)
	}
	if err := m.displayNameFreeLocked(g.DisplayName, g.ID); err != nil {
		return err
	}
	members, err := m.memberSetLocked(g.Members)
	if err != nil {
		return err
	}
	g.Created = old.Created
	g.Members = nil
	m.scimGroups[g.ID] = g
	m.scimMembers[g.ID] = members
	return nil
}

// PatchSCIMGroup applies a change to a copy and commits it only if every
// step succeeds, which is the memory store's transaction.
func (m *Memory) PatchSCIMGroup(_ context.Context, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.scimGroups[id]
	if !ok {
		return model.SCIMGroup{}, fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	if c.DisplayName != nil {
		g.DisplayName = *c.DisplayName
	}
	if c.ExternalID != nil {
		g.ExternalID = *c.ExternalID
	}
	g.Modified = at
	if err := g.Validate(); err != nil {
		return model.SCIMGroup{}, fmt.Errorf("store: %w", err)
	}
	if err := m.displayNameFreeLocked(g.DisplayName, id); err != nil {
		return model.SCIMGroup{}, err
	}
	members := make(map[string]bool, len(m.scimMembers[id]))
	for member := range m.scimMembers[id] {
		members[member] = true
	}
	for _, op := range c.Members {
		switch op.Kind {
		case model.SCIMMembersReplace:
			members = make(map[string]bool, len(op.Users))
			fallthrough
		case model.SCIMMembersAdd:
			for _, u := range op.Users {
				if _, ok := m.scimUsers[u]; !ok {
					return model.SCIMGroup{}, fmt.Errorf("store: scim member %q is not a user: %w", u, model.ErrBadInput)
				}
				members[u] = true
			}
		case model.SCIMMembersRemove:
			for _, u := range op.Users {
				delete(members, u)
			}
		default:
			return model.SCIMGroup{}, fmt.Errorf("store: unknown member operation %q: %w", op.Kind, model.ErrBadInput)
		}
	}
	m.scimGroups[id] = g
	m.scimMembers[id] = members
	out, _ := m.groupLocked(id)
	return out, nil
}

// DeleteSCIMGroup removes a group and its memberships.
func (m *Memory) DeleteSCIMGroup(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimGroups[id]; !ok {
		return fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	delete(m.scimGroups, id)
	delete(m.scimMembers, id)
	return nil
}

// ListSCIMGroups returns one page of matching groups.
func (m *Memory) ListSCIMGroups(_ context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMGroup, int, error) {
	var match func(model.SCIMGroup) bool
	switch f.Attribute {
	case "":
		match = func(model.SCIMGroup) bool { return true }
	case model.SCIMAttrDisplayName:
		match = func(g model.SCIMGroup) bool { return strings.ToLower(g.DisplayName) == strings.ToLower(f.Value) }
	case model.SCIMAttrExternalID:
		match = func(g model.SCIMGroup) bool { return g.ExternalID == f.Value }
	default:
		return nil, 0, fmt.Errorf("store: groups cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]model.SCIMGroup, 0)
	for id := range m.scimGroups {
		g, _ := m.groupLocked(id)
		if match(g) {
			all = append(all, g)
		}
	}
	sort.Slice(all, func(i, j int) bool { return createdThenID(all[i].Created, all[i].ID, all[j].Created, all[j].ID) })
	return page(all, startIndex, count), len(all), nil
}

// SCIMGroupsFor resolves an enrolled user's SCIM groups.
func (m *Memory) SCIMGroupsFor(_ context.Context, userName string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0)
	for id, u := range m.scimUsers {
		if u.UserName != userName || !u.Active {
			continue
		}
		for gid, members := range m.scimMembers {
			if members[id] {
				out = append(out, m.scimGroups[gid].DisplayName)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// SCIMCounts counts what SCIM has provisioned.
func (m *Memory) SCIMCounts(_ context.Context) (model.SCIMCounts, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := model.SCIMCounts{Users: len(m.scimUsers), Groups: len(m.scimGroups)}
	for _, u := range m.scimUsers {
		if u.Active {
			c.ActiveUsers++
		}
	}
	return c, nil
}
```

- [ ] **Step 6: Run the suites**

Run: `go build ./... && go vet ./... && go test ./internal/store/ ./internal/model/`
Expected: PASS; `TestMemorySCIM` runs all 20 cases.

- [ ] **Step 7: Commit**

```bash
git add internal/model/scim.go internal/store/store.go internal/store/memory.go internal/store/memory_scim.go internal/store/storetest/
git commit -m "feat(store): SCIM users and groups in the memory store

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Postgres SCIM store, and the deferred Postgres conformance run

**Files:**
- Create: `internal/store/migrations/0005_scim.sql`
- Create: `internal/store/postgres_scim.go`
- Modify: `internal/store/postgres.go` (`Truncate` table list)
- Modify: `internal/store/store.go` (embed `SCIMStore` in `Store`)
- Modify: `internal/store/storetest/conformance.go` (`Run` also runs the SCIM cases)
- Modify: `internal/store/memory_test.go` (drop `TestMemorySCIM`, now covered by `Run`)

**Interfaces:**
- Consumes: everything Task 2 produced; `applyUserChange`, `createdThenID` are memory-file helpers in the same package — reuse `applyUserChange`.
- Produces: `store.Store` embeds `store.SCIMStore`; `*Postgres` satisfies it. Every handler task relies on `h.store` having the SCIM methods.

- [ ] **Step 1: Migration** — `internal/store/migrations/0005_scim.sql`:

```sql
-- SCIM provisioning. The identity provider owns these rows: users and
-- groups it created, and which users are in which group. Names are unique
-- ignoring case because the core schema says userName is not case-exact
-- and because a policy targets a group by name.
CREATE TABLE IF NOT EXISTS scim_users (
    id          TEXT        PRIMARY KEY,
    user_name   TEXT        NOT NULL,
    external_id TEXT        NOT NULL DEFAULT '',
    active      BOOLEAN     NOT NULL,
    created     TIMESTAMPTZ NOT NULL,
    modified    TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS scim_users_user_name ON scim_users (lower(user_name));

CREATE TABLE IF NOT EXISTS scim_groups (
    id           TEXT        PRIMARY KEY,
    display_name TEXT        NOT NULL,
    external_id  TEXT        NOT NULL DEFAULT '',
    created      TIMESTAMPTZ NOT NULL,
    modified     TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS scim_groups_display_name ON scim_groups (lower(display_name));

CREATE TABLE IF NOT EXISTS scim_members (
    group_id TEXT NOT NULL REFERENCES scim_groups (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES scim_users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS scim_members_user ON scim_members (user_id);
```

- [ ] **Step 2: Truncate** — in `internal/store/postgres.go`, change the `TRUNCATE` statement to:

```go
	if _, err := p.pool.Exec(ctx, `TRUNCATE policy_revisions, enrollment_tokens, machines, group_snapshots, scim_members, scim_users, scim_groups RESTART IDENTITY`); err != nil {
```

- [ ] **Step 3: Implement** — `internal/store/postgres_scim.go`:

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/acme/agent-wrapper/internal/model"
)

// foreignKeyViolation is the Postgres code for a reference to a missing
// row, which here means a group member that is not a user.
const foreignKeyViolation = "23503"

// scimWriteError turns a constraint violation into the store's vocabulary.
func scimWriteError(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return fmt.Errorf("store: %s: name or id is taken: %w", what, model.ErrConflict)
		case foreignKeyViolation:
			return fmt.Errorf("store: %s: a member is not a user: %w", what, model.ErrBadInput)
		}
	}
	return fmt.Errorf("store: %s: %w", what, err)
}

const scimUserColumns = `id, user_name, external_id, active, created, modified`

func scanSCIMUser(row pgx.Row) (model.SCIMUser, error) {
	var u model.SCIMUser
	err := row.Scan(&u.ID, &u.UserName, &u.ExternalID, &u.Active, &u.Created, &u.Modified)
	return u, err
}

// CreateSCIMUser stores a provisioned user.
func (p *Postgres) CreateSCIMUser(ctx context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	const query = `INSERT INTO scim_users (` + scimUserColumns + `) VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := p.pool.Exec(ctx, query, u.ID, u.UserName, u.ExternalID, u.Active, u.Created, u.Modified); err != nil {
		return scimWriteError(err, "creating scim user")
	}
	return nil
}

// SCIMUser returns one user.
func (p *Postgres) SCIMUser(ctx context.Context, id string) (model.SCIMUser, error) {
	u, err := scanSCIMUser(p.pool.QueryRow(ctx, `SELECT `+scimUserColumns+` FROM scim_users WHERE id = $1`, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return model.SCIMUser{}, fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	case err != nil:
		return model.SCIMUser{}, fmt.Errorf("store: reading scim user: %w", err)
	}
	return u, nil
}

// ReplaceSCIMUser overwrites a user, keeping its Created time.
func (p *Postgres) ReplaceSCIMUser(ctx context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	const query = `UPDATE scim_users SET user_name = $2, external_id = $3, active = $4, modified = $5 WHERE id = $1`
	tag, err := p.pool.Exec(ctx, query, u.ID, u.UserName, u.ExternalID, u.Active, u.Modified)
	if err != nil {
		return scimWriteError(err, "replacing scim user")
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: scim user %q not found: %w", u.ID, model.ErrNotFound)
	}
	return nil
}

// PatchSCIMUser locks the row, applies the change and writes it back.
func (p *Postgres) PatchSCIMUser(ctx context.Context, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error) {
	var out model.SCIMUser
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		u, err := scanSCIMUser(tx.QueryRow(ctx, `SELECT `+scimUserColumns+` FROM scim_users WHERE id = $1 FOR UPDATE`, id))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
		case err != nil:
			return fmt.Errorf("store: reading scim user: %w", err)
		}
		u = applyUserChange(u, c, at)
		if err := u.Validate(); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		const query = `UPDATE scim_users SET user_name = $2, external_id = $3, active = $4, modified = $5 WHERE id = $1`
		if _, err := tx.Exec(ctx, query, u.ID, u.UserName, u.ExternalID, u.Active, u.Modified); err != nil {
			return scimWriteError(err, "patching scim user")
		}
		out = u
		return nil
	})
	return out, err
}

// DeleteSCIMUser removes a user; memberships go with it by cascade.
func (p *Postgres) DeleteSCIMUser(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scim_users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: deleting scim user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

// limitOffset turns a 1-based page into SQL LIMIT and OFFSET.
func limitOffset(startIndex, count int) (int, int) {
	if startIndex < 1 {
		startIndex = 1
	}
	if count < 0 {
		count = 0
	}
	return count, startIndex - 1
}

// ListSCIMUsers returns one page of matching users.
func (p *Postgres) ListSCIMUsers(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error) {
	var where string
	switch f.Attribute {
	case "":
		where = `$1::text = $1::text` // always true; keeps one parameter for every branch
	case model.SCIMAttrUserName:
		where = `lower(user_name) = lower($1)`
	case model.SCIMAttrExternalID:
		where = `external_id = $1`
	default:
		return nil, 0, fmt.Errorf("store: users cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	var total int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM scim_users WHERE `+where, f.Value).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: counting scim users: %w", err)
	}
	limit, offset := limitOffset(startIndex, count)
	rows, err := p.pool.Query(ctx,
		`SELECT `+scimUserColumns+` FROM scim_users WHERE `+where+` ORDER BY created, id COLLATE "C" LIMIT $2 OFFSET $3`,
		f.Value, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("store: listing scim users: %w", err)
	}
	defer rows.Close()
	users := make([]model.SCIMUser, 0)
	for rows.Next() {
		u, err := scanSCIMUser(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("store: listing scim users: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: listing scim users: %w", err)
	}
	return users, total, nil
}

// querier is what both a pool and a transaction offer, so a group can be
// read inside or outside one.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const scimGroupColumns = `id, display_name, external_id, created, modified`

// readSCIMGroup reads one group and its sorted members.
func readSCIMGroup(ctx context.Context, q querier, id string, lock bool) (model.SCIMGroup, error) {
	query := `SELECT ` + scimGroupColumns + ` FROM scim_groups WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var g model.SCIMGroup
	err := q.QueryRow(ctx, query, id).Scan(&g.ID, &g.DisplayName, &g.ExternalID, &g.Created, &g.Modified)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return model.SCIMGroup{}, fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	case err != nil:
		return model.SCIMGroup{}, fmt.Errorf("store: reading scim group: %w", err)
	}
	members, err := readMembers(ctx, q, []string{id})
	if err != nil {
		return model.SCIMGroup{}, err
	}
	g.Members = members[id]
	if g.Members == nil {
		g.Members = []string{}
	}
	return g, nil
}

// readMembers returns each group's sorted member ids.
func readMembers(ctx context.Context, q querier, groupIDs []string) (map[string][]string, error) {
	rows, err := q.Query(ctx,
		`SELECT group_id, user_id FROM scim_members WHERE group_id = ANY($1) ORDER BY group_id, user_id COLLATE "C"`, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("store: reading scim members: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var gid, uid string
		if err := rows.Scan(&gid, &uid); err != nil {
			return nil, fmt.Errorf("store: reading scim members: %w", err)
		}
		out[gid] = append(out[gid], uid)
	}
	return out, rows.Err()
}

// insertMembers adds members, ignoring ones already present.
func insertMembers(ctx context.Context, tx pgx.Tx, groupID string, users []string) error {
	for _, u := range users {
		const query = `INSERT INTO scim_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		if _, err := tx.Exec(ctx, query, groupID, u); err != nil {
			return scimWriteError(err, "adding scim member")
		}
	}
	return nil
}

// CreateSCIMGroup stores a group and its members in one transaction.
func (p *Postgres) CreateSCIMGroup(ctx context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		const query = `INSERT INTO scim_groups (` + scimGroupColumns + `) VALUES ($1, $2, $3, $4, $5)`
		if _, err := tx.Exec(ctx, query, g.ID, g.DisplayName, g.ExternalID, g.Created, g.Modified); err != nil {
			return scimWriteError(err, "creating scim group")
		}
		return insertMembers(ctx, tx, g.ID, g.Members)
	})
}

// SCIMGroup returns one group.
func (p *Postgres) SCIMGroup(ctx context.Context, id string) (model.SCIMGroup, error) {
	return readSCIMGroup(ctx, p.pool, id, false)
}

// ReplaceSCIMGroup overwrites a group and its member set.
func (p *Postgres) ReplaceSCIMGroup(ctx context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		const query = `UPDATE scim_groups SET display_name = $2, external_id = $3, modified = $4 WHERE id = $1`
		tag, err := tx.Exec(ctx, query, g.ID, g.DisplayName, g.ExternalID, g.Modified)
		if err != nil {
			return scimWriteError(err, "replacing scim group")
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("store: scim group %q not found: %w", g.ID, model.ErrNotFound)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM scim_members WHERE group_id = $1`, g.ID); err != nil {
			return fmt.Errorf("store: replacing scim members: %w", err)
		}
		return insertMembers(ctx, tx, g.ID, g.Members)
	})
}

// PatchSCIMGroup locks the group row, so concurrent patches on one group
// queue behind each other instead of losing writes.
func (p *Postgres) PatchSCIMGroup(ctx context.Context, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error) {
	var out model.SCIMGroup
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		g, err := readSCIMGroup(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if c.DisplayName != nil {
			g.DisplayName = *c.DisplayName
		}
		if c.ExternalID != nil {
			g.ExternalID = *c.ExternalID
		}
		g.Modified = at
		if err := g.Validate(); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		const update = `UPDATE scim_groups SET display_name = $2, external_id = $3, modified = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, update, g.ID, g.DisplayName, g.ExternalID, g.Modified); err != nil {
			return scimWriteError(err, "patching scim group")
		}
		for _, op := range c.Members {
			switch op.Kind {
			case model.SCIMMembersReplace:
				if _, err := tx.Exec(ctx, `DELETE FROM scim_members WHERE group_id = $1`, id); err != nil {
					return fmt.Errorf("store: replacing scim members: %w", err)
				}
				fallthrough
			case model.SCIMMembersAdd:
				if err := insertMembers(ctx, tx, id, op.Users); err != nil {
					return err
				}
			case model.SCIMMembersRemove:
				if _, err := tx.Exec(ctx, `DELETE FROM scim_members WHERE group_id = $1 AND user_id = ANY($2)`, id, op.Users); err != nil {
					return fmt.Errorf("store: removing scim members: %w", err)
				}
			default:
				return fmt.Errorf("store: unknown member operation %q: %w", op.Kind, model.ErrBadInput)
			}
		}
		out, err = readSCIMGroup(ctx, tx, id, false)
		return err
	})
	return out, err
}

// DeleteSCIMGroup removes a group; memberships go with it by cascade.
func (p *Postgres) DeleteSCIMGroup(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scim_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: deleting scim group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

// ListSCIMGroups returns one page of matching groups with their members.
func (p *Postgres) ListSCIMGroups(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMGroup, int, error) {
	var where string
	switch f.Attribute {
	case "":
		where = `$1::text = $1::text` // always true; keeps one parameter for every branch
	case model.SCIMAttrDisplayName:
		where = `lower(display_name) = lower($1)`
	case model.SCIMAttrExternalID:
		where = `external_id = $1`
	default:
		return nil, 0, fmt.Errorf("store: groups cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	var total int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM scim_groups WHERE `+where, f.Value).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: counting scim groups: %w", err)
	}
	limit, offset := limitOffset(startIndex, count)
	rows, err := p.pool.Query(ctx,
		`SELECT `+scimGroupColumns+` FROM scim_groups WHERE `+where+` ORDER BY created, id COLLATE "C" LIMIT $2 OFFSET $3`,
		f.Value, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("store: listing scim groups: %w", err)
	}
	groups := make([]model.SCIMGroup, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var g model.SCIMGroup
		if err := rows.Scan(&g.ID, &g.DisplayName, &g.ExternalID, &g.Created, &g.Modified); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("store: listing scim groups: %w", err)
		}
		groups = append(groups, g)
		ids = append(ids, g.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: listing scim groups: %w", err)
	}
	members, err := readMembers(ctx, p.pool, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range groups {
		groups[i].Members = members[groups[i].ID]
		if groups[i].Members == nil {
			groups[i].Members = []string{}
		}
	}
	return groups, total, nil
}

// SCIMGroupsFor resolves an enrolled user's SCIM groups.
func (p *Postgres) SCIMGroupsFor(ctx context.Context, userName string) ([]string, error) {
	const query = `
		SELECT g.display_name
		FROM scim_users u
		JOIN scim_members m ON m.user_id = u.id
		JOIN scim_groups g ON g.id = m.group_id
		WHERE u.user_name = $1 AND u.active
		ORDER BY g.display_name COLLATE "C"`
	rows, err := p.pool.Query(ctx, query, userName)
	if err != nil {
		return nil, fmt.Errorf("store: resolving scim groups: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: resolving scim groups: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// SCIMCounts counts what SCIM has provisioned.
func (p *Postgres) SCIMCounts(ctx context.Context) (model.SCIMCounts, error) {
	const query = `
		SELECT (SELECT count(*) FROM scim_users),
		       (SELECT count(*) FROM scim_users WHERE active),
		       (SELECT count(*) FROM scim_groups)`
	var c model.SCIMCounts
	if err := p.pool.QueryRow(ctx, query).Scan(&c.Users, &c.ActiveUsers, &c.Groups); err != nil {
		return model.SCIMCounts{}, fmt.Errorf("store: counting scim rows: %w", err)
	}
	return c, nil
}
```

The `COLLATE "C"` clauses make Postgres order strings byte-wise, as Go's `sort.Strings` and the memory store do; the database's default collation would not.

- [ ] **Step 3b: Fold SCIM into Store and the main suite**

In `internal/store/store.go`, add `SCIMStore` as the first line inside `type Store interface {`:

```go
type Store interface {
	SCIMStore

```

In `internal/store/storetest/conformance.go`, at the end of `Run` (after the loop):

```go
	RunSCIM(t, func(t *testing.T) store.SCIMStore { return newStore(t) })
```

Delete `TestMemorySCIM` from `memory_test.go`: `TestMemory` now runs the SCIM cases, and `TestPostgres` runs them too, truncating per case through its factory.

- [ ] **Step 4: Build and run the memory suite**

Run: `go build ./... && go vet ./... && go test ./internal/store/`
Expected: build OK; `TestMemory` PASS including its SCIM subtests; `TestPostgres` SKIP.

- [ ] **Step 5: Run the Postgres suite in Docker** (also closes the deferred migration 0003 run)

```bash
docker rm -f awd-pg 2>/dev/null; docker run -d --name awd-pg -e POSTGRES_PASSWORD=pw -p 5432:5432 postgres:16
until docker exec awd-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
AWD_TEST_DATABASE_URL='postgres://postgres:pw@localhost:5432/postgres?sslmode=disable' go test ./internal/store/ -run TestPostgres -v 2>&1 | tail -40
docker rm -f awd-pg
```

Expected: every case PASS, including the group-snapshot cases (migration 0003) and all SCIM cases. If Docker is unavailable, report that explicitly in the task summary — do not claim the Postgres suite passed.

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/0005_scim.sql internal/store/postgres_scim.go internal/store/postgres.go
git commit -m "feat(store): SCIM users and groups in Postgres

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `internal/scim` — wire types, errors, filter, discovery

**Files:**
- Create: `internal/scim/scim.go`, `internal/scim/filter.go`, `internal/scim/discovery.go`
- Test: `internal/scim/scim_test.go`, `internal/scim/filter_test.go`

**Interfaces:**
- Consumes: `model.SCIMUser`, `model.SCIMGroup`, `model.SCIMFilter`, `model.SCIMAttr*`.
- Produces (Tasks 5–7):

```go
const SchemaUser, SchemaGroup, SchemaEnterpriseUser, SchemaListResponse, SchemaPatchOp, SchemaError string
type Error struct{ Status int; ScimType, Detail string }   // implements error
func (e *Error) Body() ErrorBody
func InvalidSyntax(detail string) *Error   // 400 invalidSyntax
func InvalidFilter(detail string) *Error   // 400 invalidFilter
func InvalidValue(detail string) *Error    // 400 invalidValue
func Uniqueness(detail string) *Error      // 409 uniqueness
func NotFound(detail string) *Error        // 404
type Meta struct{ ResourceType string; Created, LastModified time.Time; Location string }
type Member struct{ Value string `json:"value"` }
type User struct{ Schemas []string; ID, ExternalID, UserName string; Active bool; Meta Meta }
type Group struct{ Schemas []string; ID, ExternalID, DisplayName string; Members []Member (omitempty); Meta Meta }
type ListResponse struct{ Schemas []string; TotalResults, StartIndex, ItemsPerPage int; Resources any }
func DecodeUser(r io.Reader) (model.SCIMUser, error)     // *Error on failure; ID/timestamps unset
func DecodeGroup(r io.Reader) (model.SCIMGroup, error)
func EncodeUser(u model.SCIMUser, location string) User
func EncodeGroup(g model.SCIMGroup, location string, withMembers bool) Group
func NewListResponse(resources any, total, startIndex, count int) ListResponse
func ParseFilter(expr string, allowed ...string) (model.SCIMFilter, error)
func ServiceProviderConfig() any
func ResourceTypes(base string) ListResponse
func Schemas() ListResponse
```

- [ ] **Step 1: Failing tests** — `internal/scim/filter_test.go`:

```go
package scim_test

import (
	"errors"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

func TestParseFilterAcceptsTheFormsTheIdPsSend(t *testing.T) {
	users := []string{model.SCIMAttrUserName, model.SCIMAttrExternalID}
	cases := []struct {
		expr string
		want model.SCIMFilter
	}{
		{``, model.SCIMFilter{}},
		{`userName eq "alice@acme.com"`, model.SCIMFilter{Attribute: "userName", Value: "alice@acme.com"}},
		{`username EQ "alice@acme.com"`, model.SCIMFilter{Attribute: "userName", Value: "alice@acme.com"}},
		{`  userName   eq   "a b"  `, model.SCIMFilter{Attribute: "userName", Value: "a b"}},
		{`externalId eq "00u1"`, model.SCIMFilter{Attribute: "externalId", Value: "00u1"}},
		{`userName eq "quote\"d"`, model.SCIMFilter{Attribute: "userName", Value: `quote"d`}},
		{`urn:ietf:params:scim:schemas:core:2.0:User:userName eq "x"`, model.SCIMFilter{Attribute: "userName", Value: "x"}},
	}
	for _, tc := range cases {
		got, err := scim.ParseFilter(tc.expr, users...)
		if err != nil || got != tc.want {
			t.Errorf("ParseFilter(%q) = %+v, %v; want %+v", tc.expr, got, err, tc.want)
		}
	}
}

func TestParseFilterRejectsEverythingElse(t *testing.T) {
	for _, expr := range []string{
		`userName co "alice"`,
		`userName eq alice`,
		`userName eq "a" and active eq true`,
		`displayName eq "platform"`, // not allowed for users
		`userName eq "unterminated`,
		`userName`,
	} {
		_, err := scim.ParseFilter(expr, model.SCIMAttrUserName, model.SCIMAttrExternalID)
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 || scimErr.ScimType != "invalidFilter" {
			t.Errorf("ParseFilter(%q) err = %v, want 400 invalidFilter", expr, err)
		}
	}
}
```

`internal/scim/scim_test.go`:

```go
package scim_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

func TestDecodeUserKeepsWhatResolutionNeedsAndDropsTheRest(t *testing.T) {
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User","urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"],
	  "userName":"alice@acme.com","externalId":"00u1","active":false,
	  "name":{"givenName":"Alice"},"emails":[{"value":"alice@acme.com","primary":true}],"password":"x"}`
	u, err := scim.DecodeUser(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if u.UserName != "alice@acme.com" || u.ExternalID != "00u1" || u.Active {
		t.Errorf("user = %+v", u)
	}
}

func TestDecodeUserDefaultsActiveToTrue(t *testing.T) {
	u, err := scim.DecodeUser(strings.NewReader(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"a@acme.com"}`))
	if err != nil || !u.Active {
		t.Errorf("user = %+v, err = %v; an omitted active means active", u, err)
	}
}

func TestDecodeUserRejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":    `{`,
		"no schema":   `{"userName":"a@acme.com"}`,
		"no userName": `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"]}`,
		"group schema": `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"userName":"a"}`,
	} {
		_, err := scim.DecodeUser(strings.NewReader(body))
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 {
			t.Errorf("%s: err = %v, want a 400 scim error", name, err)
		}
	}
}

func TestDecodeGroupReadsMemberValues(t *testing.T) {
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"platform","externalId":"g-ext",
	  "members":[{"value":"u1","display":"alice@acme.com"},{"value":"u2"}]}`
	g, err := scim.DecodeGroup(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if g.DisplayName != "platform" || g.ExternalID != "g-ext" || strings.Join(g.Members, ",") != "u1,u2" {
		t.Errorf("group = %+v", g)
	}
}

func TestEncodeGroupCanOmitMembers(t *testing.T) {
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	g := model.SCIMGroup{ID: "g1", DisplayName: "platform", Members: []string{"u1"}, Created: at, Modified: at}
	raw, _ := json.Marshal(scim.EncodeGroup(g, "https://awd/scim/v2/Groups/g1", false))
	if strings.Contains(string(raw), "members") {
		t.Errorf("excluded members still encoded: %s", raw)
	}
	raw, _ = json.Marshal(scim.EncodeGroup(g, "https://awd/scim/v2/Groups/g1", true))
	if !strings.Contains(string(raw), `"members":[{"value":"u1"}]`) || !strings.Contains(string(raw), `"resourceType":"Group"`) {
		t.Errorf("encoded = %s", raw)
	}
}

func TestErrorBodyCarriesTheStatusAsAString(t *testing.T) {
	raw, _ := json.Marshal(scim.Uniqueness("userName taken").Body())
	want := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"409","scimType":"uniqueness","detail":"userName taken"}`
	if string(raw) != want {
		t.Errorf("body = %s\nwant   %s", raw, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/scim/`
Expected: FAIL, package `scim` has no non-test Go files / undefined symbols.

- [ ] **Step 3: Implement** — `internal/scim/scim.go`:

```go
// Package scim is the SCIM 2.0 protocol (RFC 7643, RFC 7644) as the control
// plane speaks it: the wire shapes of users, groups, lists and errors, the
// filter and PATCH forms Okta and Entra ID send, and nothing about HTTP
// routing or storage. Vendor quirks are normalized here and nowhere else.
package scim

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// Schema URNs.
const (
	SchemaUser           = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup          = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaEnterpriseUser = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	SchemaListResponse   = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaPatchOp        = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SchemaError          = "urn:ietf:params:scim:api:messages:2.0:Error"
)

// Error is a SCIM error: an HTTP status, the RFC's scimType keyword when
// one applies, and a detail for the IdP's provisioning log.
type Error struct {
	Status   int
	ScimType string
	Detail   string
}

func (e *Error) Error() string { return fmt.Sprintf("scim %d %s: %s", e.Status, e.ScimType, e.Detail) }

// ErrorBody is the wire form. The RFC makes status a string.
type ErrorBody struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// Body renders the error for the wire.
func (e *Error) Body() ErrorBody {
	return ErrorBody{Schemas: []string{SchemaError}, Status: strconv.Itoa(e.Status), ScimType: e.ScimType, Detail: e.Detail}
}

// InvalidSyntax is a body the server cannot parse.
func InvalidSyntax(detail string) *Error { return &Error{Status: 400, ScimType: "invalidSyntax", Detail: detail} }

// InvalidFilter is a filter outside the supported forms.
func InvalidFilter(detail string) *Error { return &Error{Status: 400, ScimType: "invalidFilter", Detail: detail} }

// InvalidValue is a well-formed request with a value the server refuses.
func InvalidValue(detail string) *Error { return &Error{Status: 400, ScimType: "invalidValue", Detail: detail} }

// Uniqueness is a name or id already taken.
func Uniqueness(detail string) *Error { return &Error{Status: 409, ScimType: "uniqueness", Detail: detail} }

// NotFound is an unknown resource.
func NotFound(detail string) *Error { return &Error{Status: 404, Detail: detail} }

// Meta is a resource's metadata.
type Meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location,omitempty"`
}

// Member is one group member reference.
type Member struct {
	Value string `json:"value"`
}

// User is a user on the wire.
type User struct {
	Schemas    []string `json:"schemas"`
	ID         string   `json:"id"`
	ExternalID string   `json:"externalId,omitempty"`
	UserName   string   `json:"userName"`
	Active     bool     `json:"active"`
	Meta       Meta     `json:"meta"`
}

// Group is a group on the wire. Members is omitted when the IdP asked for
// excludedAttributes=members, which it does for large groups.
type Group struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []Member `json:"members,omitempty"`
	Meta        Meta     `json:"meta"`
}

// ListResponse is a page of resources.
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    any      `json:"Resources"`
}

// NewListResponse wraps one page.
func NewListResponse(resources any, total, startIndex, count int) ListResponse {
	return ListResponse{Schemas: []string{SchemaListResponse}, TotalResults: total, StartIndex: startIndex, ItemsPerPage: count, Resources: resources}
}

// incomingUser is what the server reads from a user body. Every other
// attribute an IdP sends is accepted and dropped.
type incomingUser struct {
	Schemas    []string `json:"schemas"`
	UserName   string   `json:"userName"`
	ExternalID string   `json:"externalId"`
	Active     *bool    `json:"active"`
}

// DecodeUser reads a POST or PUT user body. It leaves ID and timestamps
// for the caller. An omitted active means active, as both IdPs assume.
func DecodeUser(r io.Reader) (model.SCIMUser, error) {
	var in incomingUser
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return model.SCIMUser{}, InvalidSyntax("the body is not a SCIM user: " + err.Error())
	}
	if !slices.Contains(in.Schemas, SchemaUser) {
		return model.SCIMUser{}, InvalidSyntax("the body does not declare the " + SchemaUser + " schema")
	}
	if in.UserName == "" {
		return model.SCIMUser{}, InvalidValue("userName is required")
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	return model.SCIMUser{UserName: in.UserName, ExternalID: in.ExternalID, Active: active}, nil
}

type incomingGroup struct {
	Schemas     []string `json:"schemas"`
	DisplayName string   `json:"displayName"`
	ExternalID  string   `json:"externalId"`
	Members     []Member `json:"members"`
}

// DecodeGroup reads a POST or PUT group body.
func DecodeGroup(r io.Reader) (model.SCIMGroup, error) {
	var in incomingGroup
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return model.SCIMGroup{}, InvalidSyntax("the body is not a SCIM group: " + err.Error())
	}
	if !slices.Contains(in.Schemas, SchemaGroup) {
		return model.SCIMGroup{}, InvalidSyntax("the body does not declare the " + SchemaGroup + " schema")
	}
	if in.DisplayName == "" {
		return model.SCIMGroup{}, InvalidValue("displayName is required")
	}
	members := make([]string, 0, len(in.Members))
	for _, m := range in.Members {
		if m.Value == "" {
			return model.SCIMGroup{}, InvalidValue("a member has no value")
		}
		members = append(members, m.Value)
	}
	return model.SCIMGroup{DisplayName: in.DisplayName, ExternalID: in.ExternalID, Members: members}, nil
}

// EncodeUser renders a stored user.
func EncodeUser(u model.SCIMUser, location string) User {
	return User{
		Schemas: []string{SchemaUser}, ID: u.ID, ExternalID: u.ExternalID, UserName: u.UserName, Active: u.Active,
		Meta: Meta{ResourceType: "User", Created: u.Created, LastModified: u.Modified, Location: location},
	}
}

// EncodeGroup renders a stored group, with or without its members.
func EncodeGroup(g model.SCIMGroup, location string, withMembers bool) Group {
	out := Group{
		Schemas: []string{SchemaGroup}, ID: g.ID, ExternalID: g.ExternalID, DisplayName: g.DisplayName,
		Meta: Meta{ResourceType: "Group", Created: g.Created, LastModified: g.Modified, Location: location},
	}
	if withMembers {
		out.Members = make([]Member, 0, len(g.Members))
		for _, m := range g.Members {
			out.Members = append(out.Members, Member{Value: m})
		}
	}
	return out
}
```

Note: `slices` is in the standard library since Go 1.21.

`internal/scim/filter.go`:

```go
package scim

import (
	"encoding/json"
	"strings"

	"github.com/acme/agent-wrapper/internal/model"
)

// ParseFilter reads the one filter form the target IdPs send,
// `<attribute> eq "<value>"`, for an attribute in allowed. The attribute
// name and the operator are case-insensitive, as RFC 7644 §3.4.2.2 says;
// a core-schema URN prefix on the attribute is accepted. Anything else is
// an invalidFilter error rather than an empty list, so an IdP that sends
// a filter the server does not understand finds out.
func ParseFilter(expr string, allowed ...string) (model.SCIMFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return model.SCIMFilter{}, nil
	}
	attr, rest, ok := strings.Cut(expr, " ")
	if !ok {
		return model.SCIMFilter{}, InvalidFilter("expected `attribute eq \"value\"`, got " + expr)
	}
	rest = strings.TrimSpace(rest)
	op, value, ok := strings.Cut(rest, " ")
	if !ok || !strings.EqualFold(op, "eq") {
		return model.SCIMFilter{}, InvalidFilter("only the eq operator is supported: " + expr)
	}
	value = strings.TrimSpace(value)
	var unquoted string
	if !strings.HasPrefix(value, `"`) || json.Unmarshal([]byte(value), &unquoted) != nil {
		return model.SCIMFilter{}, InvalidFilter("the value must be one quoted string: " + expr)
	}
	attr = stripCoreURN(attr)
	for _, a := range allowed {
		if strings.EqualFold(attr, a) {
			return model.SCIMFilter{Attribute: a, Value: unquoted}, nil
		}
	}
	return model.SCIMFilter{}, InvalidFilter("cannot filter on " + attr)
}

// stripCoreURN removes a core user or group schema prefix from an
// attribute path: "urn:…:core:2.0:User:userName" is "userName".
func stripCoreURN(path string) string {
	for _, urn := range []string{SchemaUser, SchemaGroup} {
		if len(path) > len(urn) && strings.EqualFold(path[:len(urn)+1], urn+":") {
			return path[len(urn)+1:]
		}
	}
	return path
}
```

Note: a value like `"a" and active eq true` fails `json.Unmarshal` because of trailing data — that is what rejects compound filters.

`internal/scim/discovery.go`:

```go
package scim

// ServiceProviderConfig tells the IdP what this server supports. Both
// Okta and Entra read it; neither depends on anything beyond patch and
// filter being true.
func ServiceProviderConfig() any {
	return map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":            map[string]bool{"supported": true},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": true, "maxResults": 1000},
		"changePassword":   map[string]bool{"supported": false},
		"sort":             map[string]bool{"supported": false},
		"etag":             map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token",
			"description": "The AWD_SCIM_TOKEN shared secret in an Authorization: Bearer header.",
		}},
	}
}

// ResourceTypes lists User and Group.
func ResourceTypes(base string) ListResponse {
	types := []map[string]any{
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User",
			"endpoint": "/Users", "schema": SchemaUser, "meta": map[string]string{"resourceType": "ResourceType", "location": base + "/ResourceTypes/User"}},
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "Group", "name": "Group",
			"endpoint": "/Groups", "schema": SchemaGroup, "meta": map[string]string{"resourceType": "ResourceType", "location": base + "/ResourceTypes/Group"}},
	}
	return NewListResponse(types, len(types), 1, len(types))
}

func attribute(name, typ string, required, caseExact bool, uniqueness string) map[string]any {
	return map[string]any{"name": name, "type": typ, "multiValued": false, "required": required,
		"caseExact": caseExact, "mutability": "readWrite", "returned": "default", "uniqueness": uniqueness}
}

// Schemas describes the attributes the server keeps; everything else an
// IdP sends is accepted and dropped.
func Schemas() ListResponse {
	members := map[string]any{"name": "members", "type": "complex", "multiValued": true, "required": false,
		"mutability": "readWrite", "returned": "default",
		"subAttributes": []map[string]any{attribute("value", "string", true, true, "none")}}
	schemas := []map[string]any{
		{"id": SchemaUser, "name": "User", "attributes": []map[string]any{
			attribute("userName", "string", true, false, "server"),
			attribute("externalId", "string", false, true, "none"),
			attribute("active", "boolean", false, false, "none"),
		}},
		{"id": SchemaGroup, "name": "Group", "attributes": []map[string]any{
			attribute("displayName", "string", true, false, "server"),
			attribute("externalId", "string", false, true, "none"),
			members,
		}},
	}
	return NewListResponse(schemas, len(schemas), 1, len(schemas))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/scim/ && go vet ./internal/scim/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scim/
git commit -m "feat(scim): wire types, errors, filters and discovery documents

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `internal/scim` — PATCH applier with Okta and Entra quirks

**Files:**
- Create: `internal/scim/patch.go`
- Test: `internal/scim/patch_test.go`

**Interfaces:**
- Consumes: `InvalidSyntax`, `InvalidValue`, `SchemaPatchOp`, `stripCoreURN` (Task 4), `model.SCIMUserChange`, `model.SCIMGroupChange`, `model.SCIMMemberOp`, `model.SCIMMembers*`.
- Produces: `func UserPatch(r io.Reader) (model.SCIMUserChange, error)`, `func GroupPatch(r io.Reader) (model.SCIMGroupChange, error)` — errors are `*Error`.

- [ ] **Step 1: Failing tests** — `internal/scim/patch_test.go`. The bodies are the shapes Okta's and Entra's provisioning docs show.

```go
package scim_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

const patchSchema = `"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"]`

func userPatch(t *testing.T, ops string) model.SCIMUserChange {
	t.Helper()
	c, err := scim.UserPatch(strings.NewReader(`{` + patchSchema + `,"Operations":` + ops + `}`))
	if err != nil {
		t.Fatalf("UserPatch(%s): %v", ops, err)
	}
	return c
}

func TestUserPatchOktaDeactivate(t *testing.T) {
	c := userPatch(t, `[{"op":"replace","value":{"active":false}}]`)
	if c.Active == nil || *c.Active || c.UserName != nil || c.ExternalID != nil {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchEntraStringBooleanAndCapitalOp(t *testing.T) {
	c := userPatch(t, `[{"op":"Replace","path":"active","value":"False"}]`)
	if c.Active == nil || *c.Active {
		t.Errorf("change = %+v", c)
	}
	c = userPatch(t, `[{"op":"Replace","path":"active","value":"True"}]`)
	if c.Active == nil || !*c.Active {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchEntraAttributeUpdates(t *testing.T) {
	c := userPatch(t, `[
	  {"op":"Replace","path":"userName","value":"alice@acme.io"},
	  {"op":"Add","path":"externalId","value":"alice"},
	  {"op":"Add","path":"emails[type eq \"work\"].value","value":"alice@acme.io"},
	  {"op":"Replace","path":"name.givenName","value":"Alice"},
	  {"op":"Add","path":"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department","value":"Eng"}
	]`)
	if c.UserName == nil || *c.UserName != "alice@acme.io" || c.ExternalID == nil || *c.ExternalID != "alice" || c.Active != nil {
		t.Errorf("change = %+v; unheld paths must be no-ops", c)
	}
}

func TestUserPatchCoreURNPath(t *testing.T) {
	c := userPatch(t, `[{"op":"replace","path":"urn:ietf:params:scim:schemas:core:2.0:User:active","value":false}]`)
	if c.Active == nil || *c.Active {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchNoPathIgnoresUnheldKeys(t *testing.T) {
	c := userPatch(t, `[{"op":"replace","value":{"id":"u1","active":true,"name":{"givenName":"A"},"userName":"b@acme.com"}}]`)
	if c.Active == nil || !*c.Active || c.UserName == nil || *c.UserName != "b@acme.com" {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchRemoveExternalID(t *testing.T) {
	c := userPatch(t, `[{"op":"remove","path":"externalId"}]`)
	if c.ExternalID == nil || *c.ExternalID != "" {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchRejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":         `{`,
		"no schema":        `{"Operations":[{"op":"replace","path":"active","value":false}]}`,
		"no operations":    `{` + patchSchema + `,"Operations":[]}`,
		"unknown op":       `{` + patchSchema + `,"Operations":[{"op":"move","path":"active","value":false}]}`,
		"bad boolean":      `{` + patchSchema + `,"Operations":[{"op":"replace","path":"active","value":"maybe"}]}`,
		"remove userName":  `{` + patchSchema + `,"Operations":[{"op":"remove","path":"userName"}]}`,
		"empty userName":   `{` + patchSchema + `,"Operations":[{"op":"replace","path":"userName","value":""}]}`,
		"non-string name":  `{` + patchSchema + `,"Operations":[{"op":"replace","path":"userName","value":7}]}`,
	} {
		_, err := scim.UserPatch(strings.NewReader(body))
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 {
			t.Errorf("%s: err = %v, want a 400 scim error", name, err)
		}
	}
}

func groupPatch(t *testing.T, ops string) model.SCIMGroupChange {
	t.Helper()
	c, err := scim.GroupPatch(strings.NewReader(`{` + patchSchema + `,"Operations":` + ops + `}`))
	if err != nil {
		t.Fatalf("GroupPatch(%s): %v", ops, err)
	}
	return c
}

func TestGroupPatchOktaAddAndFilteredRemove(t *testing.T) {
	c := groupPatch(t, `[
	  {"op":"add","path":"members","value":[{"value":"u1","display":"alice@acme.com"},{"value":"u2"}]},
	  {"op":"remove","path":"members[value eq \"u3\"]"}
	]`)
	want := []model.SCIMMemberOp{{Kind: "add", Users: []string{"u1", "u2"}}, {Kind: "remove", Users: []string{"u3"}}}
	if !equalOps(c.Members, want) || c.DisplayName != nil {
		t.Errorf("change = %+v, want %+v", c, want)
	}
}

func TestGroupPatchOktaRenameWithIDInValue(t *testing.T) {
	c := groupPatch(t, `[{"op":"replace","value":{"id":"g1","displayName":"platform-eng"}}]`)
	if c.DisplayName == nil || *c.DisplayName != "platform-eng" || len(c.Members) != 0 {
		t.Errorf("change = %+v", c)
	}
}

func TestGroupPatchEntraCapitalOpsAndValueListRemove(t *testing.T) {
	c := groupPatch(t, `[
	  {"op":"Add","path":"members","value":[{"value":"u1"}]},
	  {"op":"Remove","path":"members","value":[{"value":"u2"}]},
	  {"op":"Replace","path":"displayName","value":"oncall"},
	  {"op":"Replace","path":"externalId","value":"8aa1"}
	]`)
	want := []model.SCIMMemberOp{{Kind: "add", Users: []string{"u1"}}, {Kind: "remove", Users: []string{"u2"}}}
	if !equalOps(c.Members, want) || c.DisplayName == nil || *c.DisplayName != "oncall" || c.ExternalID == nil || *c.ExternalID != "8aa1" {
		t.Errorf("change = %+v", c)
	}
}

func TestGroupPatchReplaceMembersAndRemoveAll(t *testing.T) {
	c := groupPatch(t, `[{"op":"replace","path":"members","value":[{"value":"u9"}]},{"op":"remove","path":"members"}]`)
	want := []model.SCIMMemberOp{{Kind: "replace", Users: []string{"u9"}}, {Kind: "replace", Users: []string{}}}
	if !equalOps(c.Members, want) {
		t.Errorf("members = %+v, want %+v", c.Members, want)
	}
}

func TestGroupPatchNoPathMembers(t *testing.T) {
	c := groupPatch(t, `[{"op":"replace","value":{"members":[{"value":"u1"}]}}]`)
	if !equalOps(c.Members, []model.SCIMMemberOp{{Kind: "replace", Users: []string{"u1"}}}) {
		t.Errorf("members = %+v", c.Members)
	}
}

func TestGroupPatchRejects(t *testing.T) {
	for name, ops := range map[string]string{
		"member without value": `[{"op":"add","path":"members","value":[{"display":"x"}]}]`,
		"members not a list":   `[{"op":"add","path":"members","value":"u1"}]`,
		"bad member filter":    `[{"op":"remove","path":"members[display eq \"x\"]"}]`,
		"empty displayName":    `[{"op":"replace","path":"displayName","value":""}]`,
		"remove displayName":   `[{"op":"remove","path":"displayName"}]`,
		"unknown op":           `[{"op":"copy","path":"members","value":[]}]`,
	} {
		_, err := scim.GroupPatch(strings.NewReader(`{` + patchSchema + `,"Operations":` + ops + `}`))
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 {
			t.Errorf("%s: err = %v, want a 400 scim error", name, err)
		}
	}
}

func equalOps(a, b []model.SCIMMemberOp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || strings.Join(a[i].Users, ",") != strings.Join(b[i].Users, ",") {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/scim/ -run Patch`
Expected: FAIL, `undefined: scim.UserPatch`.

- [ ] **Step 3: Implement** — `internal/scim/patch.go`:

```go
package scim

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/acme/agent-wrapper/internal/model"
)

// patchRequest is a PatchOp body. Go's decoder matches "Operations" and
// "operations" alike, which covers both IdPs.
type patchRequest struct {
	Schemas    []string    `json:"schemas"`
	Operations []operation `json:"Operations"`
}

type operation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// readPatch decodes the envelope and normalizes each op to lower case,
// since Entra sends "Replace" and "Add".
func readPatch(r io.Reader) ([]operation, error) {
	var req patchRequest
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return nil, InvalidSyntax("the body is not a SCIM PatchOp: " + err.Error())
	}
	if !slices.Contains(req.Schemas, SchemaPatchOp) {
		return nil, InvalidSyntax("the body does not declare the " + SchemaPatchOp + " schema")
	}
	if len(req.Operations) == 0 {
		return nil, InvalidSyntax("the PatchOp has no Operations")
	}
	for i := range req.Operations {
		req.Operations[i].Op = strings.ToLower(req.Operations[i].Op)
		switch req.Operations[i].Op {
		case "add", "replace", "remove":
		default:
			return nil, InvalidSyntax(fmt.Sprintf("unsupported op %q", req.Operations[i].Op))
		}
		req.Operations[i].Path = stripCoreURN(strings.TrimSpace(req.Operations[i].Path))
	}
	return req.Operations, nil
}

// noPathValue splits the object a path-less add or replace carries into
// its attributes, keyed by lower-cased name.
func noPathValue(op operation) (map[string]json.RawMessage, error) {
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(op.Value, &attrs); err != nil {
		return nil, InvalidValue("an operation without a path needs an object value")
	}
	out := make(map[string]json.RawMessage, len(attrs))
	for k, v := range attrs {
		out[strings.ToLower(stripCoreURN(k))] = v
	}
	return out, nil
}

func stringValue(raw json.RawMessage, attr string, allowEmpty bool) (*string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, InvalidValue(attr + " must be a string")
	}
	if s == "" && !allowEmpty {
		return nil, InvalidValue(attr + " must not be empty")
	}
	return &s, nil
}

// boolValue accepts a JSON boolean or, as Entra sends, the strings "True"
// and "False" in any case.
func boolValue(raw json.RawMessage, attr string) (*bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return &b, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(s) {
		case "true":
			b = true
			return &b, nil
		case "false":
			return &b, nil
		}
	}
	return nil, InvalidValue(attr + " must be a boolean")
}

// UserPatch folds a user PatchOp into the fields it sets. Paths the server
// does not hold (name.*, emails[…], enterprise extension attributes) are
// accepted as no-ops, so an IdP does not retry them forever.
func UserPatch(r io.Reader) (model.SCIMUserChange, error) {
	ops, err := readPatch(r)
	if err != nil {
		return model.SCIMUserChange{}, err
	}
	var c model.SCIMUserChange
	set := func(attr string, raw json.RawMessage, remove bool) error {
		switch attr {
		case "username":
			if remove {
				return InvalidValue("userName cannot be removed")
			}
			v, err := stringValue(raw, "userName", false)
			c.UserName = v
			return err
		case "externalid":
			if remove {
				c.ExternalID = new(string)
				return nil
			}
			v, err := stringValue(raw, "externalId", true)
			c.ExternalID = v
			return err
		case "active":
			if remove {
				return InvalidValue("active cannot be removed")
			}
			v, err := boolValue(raw, "active")
			c.Active = v
			return err
		}
		return nil
	}
	for _, op := range ops {
		remove := op.Op == "remove"
		if op.Path == "" {
			if remove {
				return model.SCIMUserChange{}, InvalidValue("remove needs a path")
			}
			attrs, err := noPathValue(op)
			if err != nil {
				return model.SCIMUserChange{}, err
			}
			for attr, raw := range attrs {
				if err := set(attr, raw, false); err != nil {
					return model.SCIMUserChange{}, err
				}
			}
			continue
		}
		if err := set(strings.ToLower(op.Path), op.Value, remove); err != nil {
			return model.SCIMUserChange{}, err
		}
	}
	return c, nil
}

// memberValues reads a members value list.
func memberValues(raw json.RawMessage) ([]string, error) {
	var members []Member
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, InvalidValue("members must be a list of {\"value\": id}")
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if m.Value == "" {
			return nil, InvalidValue("a member has no value")
		}
		out = append(out, m.Value)
	}
	return out, nil
}

// memberFilterID reads the id out of Okta's `members[value eq "<id>"]`.
func memberFilterID(path string) (string, bool, error) {
	lower := strings.ToLower(path)
	if !strings.HasPrefix(lower, "members[") {
		return "", false, nil
	}
	if !strings.HasSuffix(path, "]") {
		return "", true, InvalidValue("unsupported member path " + path)
	}
	f, err := ParseFilter(path[len("members["):len(path)-1], "value")
	if err != nil {
		return "", true, InvalidValue("unsupported member path " + path)
	}
	return f.Value, true, nil
}

// GroupPatch folds a group PatchOp into a change set. Member operations
// keep their order; the store applies them in that order in one
// transaction.
func GroupPatch(r io.Reader) (model.SCIMGroupChange, error) {
	ops, err := readPatch(r)
	if err != nil {
		return model.SCIMGroupChange{}, err
	}
	var c model.SCIMGroupChange
	set := func(op, attr string, raw json.RawMessage) error {
		switch attr {
		case "displayname":
			if op == "remove" {
				return InvalidValue("displayName cannot be removed")
			}
			v, err := stringValue(raw, "displayName", false)
			c.DisplayName = v
			return err
		case "externalid":
			if op == "remove" {
				c.ExternalID = new(string)
				return nil
			}
			v, err := stringValue(raw, "externalId", true)
			c.ExternalID = v
			return err
		case "members":
			if op == "remove" && len(raw) == 0 {
				c.Members = append(c.Members, model.SCIMMemberOp{Kind: model.SCIMMembersReplace, Users: []string{}})
				return nil
			}
			ids, err := memberValues(raw)
			if err != nil {
				return err
			}
			kind := map[string]string{"add": model.SCIMMembersAdd, "remove": model.SCIMMembersRemove, "replace": model.SCIMMembersReplace}[op]
			c.Members = append(c.Members, model.SCIMMemberOp{Kind: kind, Users: ids})
		}
		return nil
	}
	for _, op := range ops {
		if op.Path == "" {
			if op.Op == "remove" {
				return model.SCIMGroupChange{}, InvalidValue("remove needs a path")
			}
			attrs, err := noPathValue(op)
			if err != nil {
				return model.SCIMGroupChange{}, err
			}
			// Map iteration order is random; apply attributes in a fixed
			// order so a body with both a rename and members is deterministic.
			for _, attr := range []string{"displayname", "externalid", "members"} {
				if raw, ok := attrs[attr]; ok {
					if err := set(op.Op, attr, raw); err != nil {
						return model.SCIMGroupChange{}, err
					}
				}
			}
			continue
		}
		id, isFilter, err := memberFilterID(op.Path)
		if err != nil {
			return model.SCIMGroupChange{}, err
		}
		if isFilter {
			if op.Op != "remove" {
				return model.SCIMGroupChange{}, InvalidValue("a member filter path is supported only for remove")
			}
			c.Members = append(c.Members, model.SCIMMemberOp{Kind: model.SCIMMembersRemove, Users: []string{id}})
			continue
		}
		if err := set(op.Op, strings.ToLower(op.Path), op.Value); err != nil {
			return model.SCIMGroupChange{}, err
		}
	}
	return c, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/scim/ && go vet ./internal/scim/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scim/patch.go internal/scim/patch_test.go
git commit -m "feat(scim): fold Okta and Entra PATCH bodies into change sets

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: SCIM handler — auth, discovery, Users

**Files:**
- Create: `internal/handler/scim.go`
- Modify: `internal/handler/handler.go` (`SCIMToken` field; one line in `Routes`)
- Test: `internal/handler/scim_test.go`

**Interfaces:**
- Consumes: `internal/scim` (Tasks 4–5), store SCIM methods (Task 2), `credential.NewID()`, `bearer(r)` from `auth.go`, `h.Now`.
- Produces (Task 7 uses): `h.requireSCIM(http.HandlerFunc) http.HandlerFunc`, `writeSCIM(w, status, v)`, `h.scimFail(w, r, err)`, `scimBase(r) string`, `decodeSCIM(w, r) io.Reader` (size-limited body), `listParams(r) (startIndex, count int, err error)`, const `maxSCIMBody = 1 << 20`. Test helpers `newSCIMServer`, `scimDo`, const `scimToken`.

- [ ] **Step 1: Failing tests** — `internal/handler/scim_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/store"
)

const scimToken = "test-scim-token"

// newSCIMServer is newServer with SCIM enabled.
func newSCIMServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.AdminToken = adminToken
	h.SCIMToken = scimToken
	// The same clock as newServer, so a bundle from either server is
	// byte-comparable.
	h.Now = func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

// scimDo sends a SCIM request with the SCIM token and decodes a JSON
// answer into a generic map (nil for an empty body).
func scimDo(t *testing.T, srv *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	return scimDoAs(t, srv, method, path, body, scimToken)
}

func scimDoAs(t *testing.T, srv *httptest.Server, method, path, body, token string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/scim+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) == 0 {
		return resp.StatusCode, nil
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/scim+json") {
		t.Errorf("%s %s: Content-Type = %q", method, path, ct)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s %s: decoding %s: %v", method, path, raw, err)
	}
	return resp.StatusCode, out
}

const aliceUser = `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@acme.com","externalId":"00u1","active":true}`

func createUser(t *testing.T, srv *httptest.Server, body string) string {
	t.Helper()
	status, out := scimDo(t, srv, http.MethodPost, "/scim/v2/Users", body)
	if status != http.StatusCreated {
		t.Fatalf("creating user: %d %v", status, out)
	}
	return out["id"].(string)
}

func TestSCIMIsDisabledWithoutAToken(t *testing.T) {
	srv := newServer(t) // no SCIMToken
	status, out := scimDoAs(t, srv, http.MethodGet, "/scim/v2/Users", "", "anything")
	if status != http.StatusServiceUnavailable || out["status"] != "503" {
		t.Errorf("status = %d, body = %v; want a 503 SCIM error", status, out)
	}
}

func TestSCIMRefusesAWrongToken(t *testing.T) {
	srv := newSCIMServer(t)
	for _, token := range []string{"", "wrong", adminToken} {
		if status, _ := scimDoAs(t, srv, http.MethodGet, "/scim/v2/Users", "", token); status != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", token, status)
		}
	}
}

func TestSCIMDiscoveryDocuments(t *testing.T) {
	srv := newSCIMServer(t)
	status, spc := scimDo(t, srv, http.MethodGet, "/scim/v2/ServiceProviderConfig", "")
	if status != 200 || spc["patch"].(map[string]any)["supported"] != true {
		t.Errorf("ServiceProviderConfig = %d %v", status, spc)
	}
	for _, path := range []string{"/scim/v2/ResourceTypes", "/scim/v2/Schemas"} {
		status, out := scimDo(t, srv, http.MethodGet, path, "")
		if status != 200 || out["totalResults"] != float64(2) {
			t.Errorf("%s = %d %v", path, status, out)
		}
	}
}

func TestSCIMUserLifecycle(t *testing.T) {
	srv := newSCIMServer(t)
	status, created := scimDo(t, srv, http.MethodPost, "/scim/v2/Users", aliceUser)
	if status != http.StatusCreated {
		t.Fatalf("POST = %d %v", status, created)
	}
	id := created["id"].(string)
	meta := created["meta"].(map[string]any)
	if created["userName"] != "alice@acme.com" || created["active"] != true || meta["resourceType"] != "User" ||
		!strings.HasSuffix(meta["location"].(string), "/scim/v2/Users/"+id) {
		t.Errorf("created = %v", created)
	}

	if status, got := scimDo(t, srv, http.MethodGet, "/scim/v2/Users/"+id, ""); status != 200 || got["externalId"] != "00u1" {
		t.Errorf("GET = %d %v", status, got)
	}

	status, patched := scimDo(t, srv, http.MethodPatch, "/scim/v2/Users/"+id,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}`)
	if status != 200 || patched["active"] != false {
		t.Errorf("PATCH = %d %v", status, patched)
	}

	status, replaced := scimDo(t, srv, http.MethodPut, "/scim/v2/Users/"+id,
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@acme.io","active":true}`)
	if status != 200 || replaced["userName"] != "alice@acme.io" || replaced["active"] != true {
		t.Errorf("PUT = %d %v", status, replaced)
	}

	if status, _ := scimDo(t, srv, http.MethodDelete, "/scim/v2/Users/"+id, ""); status != http.StatusNoContent {
		t.Errorf("DELETE = %d", status)
	}
	if status, out := scimDo(t, srv, http.MethodGet, "/scim/v2/Users/"+id, ""); status != 404 || out["status"] != "404" {
		t.Errorf("GET after delete = %d %v", status, out)
	}
}

func TestSCIMUserErrors(t *testing.T) {
	srv := newSCIMServer(t)
	createUser(t, srv, aliceUser)
	cases := []struct {
		name, method, path, body string
		status                   int
		scimType                 string
	}{
		{"duplicate userName ignoring case", "POST", "/scim/v2/Users",
			`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"ALICE@acme.com"}`, 409, "uniqueness"},
		{"not json", "POST", "/scim/v2/Users", `{`, 400, "invalidSyntax"},
		{"no userName", "POST", "/scim/v2/Users", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"]}`, 400, "invalidValue"},
		{"unsupported filter", "GET", "/scim/v2/Users?filter=userName%20co%20%22a%22", "", 400, "invalidFilter"},
		{"bad count", "GET", "/scim/v2/Users?count=many", "", 400, "invalidValue"},
		{"unknown id", "PATCH", "/scim/v2/Users/nope",
			`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}`, 404, ""},
		{"unknown id on PUT", "PUT", "/scim/v2/Users/nope", aliceUser, 404, ""},
	}
	for _, tc := range cases {
		status, out := scimDo(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || (tc.scimType != "" && out["scimType"] != tc.scimType) {
			t.Errorf("%s: %d %v, want %d %s", tc.name, status, out, tc.status, tc.scimType)
		}
	}
}

func TestSCIMBodyOverTheCapIs413(t *testing.T) {
	srv := newSCIMServer(t)
	big := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"a@acme.com","x":"` + strings.Repeat("a", 1<<20) + `"}`
	if status, _ := scimDo(t, srv, http.MethodPost, "/scim/v2/Users", big); status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", status)
	}
}

func TestSCIMUserListFiltersAndPages(t *testing.T) {
	srv := newSCIMServer(t)
	createUser(t, srv, aliceUser)
	createUser(t, srv, `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"bob@acme.com"}`)

	status, list := scimDo(t, srv, http.MethodGet, "/scim/v2/Users?filter=userName+eq+%22Alice%40acme.com%22", "")
	resources := list["Resources"].([]any)
	if status != 200 || list["totalResults"] != float64(1) || len(resources) != 1 ||
		resources[0].(map[string]any)["userName"] != "alice@acme.com" {
		t.Errorf("filtered list = %d %v", status, list)
	}
	schemas := list["schemas"].([]any)
	if schemas[0] != "urn:ietf:params:scim:api:messages:2.0:ListResponse" {
		t.Errorf("schemas = %v", schemas)
	}

	_, page := scimDo(t, srv, http.MethodGet, "/scim/v2/Users?startIndex=2&count=1", "")
	if page["totalResults"] != float64(2) || page["startIndex"] != float64(2) || page["itemsPerPage"] != float64(1) {
		t.Errorf("page = %v", page)
	}

	_, none := scimDo(t, srv, http.MethodGet, "/scim/v2/Users?filter=userName+eq+%22nobody%40acme.com%22", "")
	if none["totalResults"] != float64(0) || len(none["Resources"].([]any)) != 0 {
		t.Errorf("empty list = %v; Resources must be [] not null", none)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run SCIM`
Expected: FAIL, `h.SCIMToken undefined`.

- [ ] **Step 3: Implement**

In `internal/handler/handler.go`, add to `Handler` after `AdminToken`:

```go
	// SCIMToken is the bearer token the identity provider presents on
	// /scim/v2. It is separate from AdminToken because it lives in the
	// IdP's configuration, not with the operators. Empty disables SCIM
	// with a 503.
	SCIMToken string
```

In `Routes`, before the `return`:

```go
	h.scimRoutes(mux)
```

Create `internal/handler/scim.go`:

```go
package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/acme/agent-wrapper/internal/credential"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

// maxSCIMBody bounds a SCIM request. SCIM changes are small; a large group
// arrives as member PATCHes, not as one body.
const maxSCIMBody = 1 << 20

// Paging limits for SCIM lists.
const (
	defaultSCIMCount = 100
	maxSCIMCount     = 1000
)

func (h *Handler) scimRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /scim/v2/ServiceProviderConfig", h.requireSCIM(h.scimServiceProviderConfig))
	mux.HandleFunc("GET /scim/v2/ResourceTypes", h.requireSCIM(h.scimResourceTypes))
	mux.HandleFunc("GET /scim/v2/Schemas", h.requireSCIM(h.scimSchemas))
	mux.HandleFunc("GET /scim/v2/Users", h.requireSCIM(h.scimListUsers))
	mux.HandleFunc("POST /scim/v2/Users", h.requireSCIM(h.scimCreateUser))
	mux.HandleFunc("GET /scim/v2/Users/{id}", h.requireSCIM(h.scimGetUser))
	mux.HandleFunc("PUT /scim/v2/Users/{id}", h.requireSCIM(h.scimReplaceUser))
	mux.HandleFunc("PATCH /scim/v2/Users/{id}", h.requireSCIM(h.scimPatchUser))
	mux.HandleFunc("DELETE /scim/v2/Users/{id}", h.requireSCIM(h.scimDeleteUser))
}

// requireSCIM admits the identity provider's token. Unset answers 503 so a
// forgotten AWD_SCIM_TOKEN shows up in the IdP's provisioning log rather
// than leaving the endpoint open.
func (h *Handler) requireSCIM(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.SCIMToken == "" {
			writeSCIMError(w, &scim.Error{Status: http.StatusServiceUnavailable, Detail: "SCIM is not configured on this server"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(h.SCIMToken)) != 1 {
			writeSCIMError(w, &scim.Error{Status: http.StatusUnauthorized, Detail: "unauthorized"})
			return
		}
		next(w, r)
	}
}

func writeSCIM(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encoding a SCIM response", "err", err)
	}
}

func writeSCIMError(w http.ResponseWriter, e *scim.Error) {
	writeSCIM(w, e.Status, e.Body())
}

// scimFail maps an error to a SCIM error. It is the SCIM routes' fail.
func (h *Handler) scimFail(w http.ResponseWriter, r *http.Request, err error) {
	var scimErr *scim.Error
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &scimErr):
		writeSCIMError(w, scimErr)
	case errors.As(err, &tooLarge):
		writeSCIMError(w, &scim.Error{Status: http.StatusRequestEntityTooLarge, Detail: fmt.Sprintf("the body exceeds %d bytes", maxSCIMBody)})
	case errors.Is(err, model.ErrNotFound):
		writeSCIMError(w, scim.NotFound("no such resource"))
	case errors.Is(err, model.ErrConflict):
		writeSCIMError(w, scim.Uniqueness(err.Error()))
	case errors.Is(err, model.ErrBadInput):
		writeSCIMError(w, scim.InvalidValue(err.Error()))
	default:
		h.log.ErrorContext(r.Context(), "unhandled SCIM error", "err", err, "path", r.URL.Path)
		writeSCIMError(w, &scim.Error{Status: http.StatusInternalServerError, Detail: "internal error"})
	}
}

// scimBody reads the whole request body, capped. It is read before any
// decoding so that an oversized body surfaces as the *http.MaxBytesError
// scimFail turns into a 413, not as a decoder's invalidSyntax.
func scimBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(w, r.Body, maxSCIMBody))
}

// scimBase is this server's SCIM root as the IdP reached it, for
// meta.location. X-Forwarded-Proto covers a TLS-terminating proxy.
func scimBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host + "/scim/v2"
}

// listParams reads startIndex and count with the RFC's defaults.
func listParams(r *http.Request) (startIndex, count int, err error) {
	startIndex, count = 1, defaultSCIMCount
	if v := r.URL.Query().Get("startIndex"); v != "" {
		if startIndex, err = strconv.Atoi(v); err != nil {
			return 0, 0, scim.InvalidValue("startIndex must be an integer")
		}
		if startIndex < 1 {
			startIndex = 1
		}
	}
	if v := r.URL.Query().Get("count"); v != "" {
		if count, err = strconv.Atoi(v); err != nil {
			return 0, 0, scim.InvalidValue("count must be an integer")
		}
		count = max(0, min(count, maxSCIMCount))
	}
	return startIndex, count, nil
}

func (h *Handler) scimServiceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	writeSCIM(w, http.StatusOK, scim.ServiceProviderConfig())
}

func (h *Handler) scimResourceTypes(w http.ResponseWriter, r *http.Request) {
	writeSCIM(w, http.StatusOK, scim.ResourceTypes(scimBase(r)))
}

func (h *Handler) scimSchemas(w http.ResponseWriter, _ *http.Request) {
	writeSCIM(w, http.StatusOK, scim.Schemas())
}

func (h *Handler) userLocation(r *http.Request, id string) string {
	return scimBase(r) + "/Users/" + id
}

func (h *Handler) scimListUsers(w http.ResponseWriter, r *http.Request) {
	filter, err := scim.ParseFilter(r.URL.Query().Get("filter"), model.SCIMAttrUserName, model.SCIMAttrExternalID)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	startIndex, count, err := listParams(r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	users, total, err := h.store.ListSCIMUsers(r.Context(), filter, startIndex, count)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	out := make([]scim.User, 0, len(users))
	for _, u := range users {
		out = append(out, scim.EncodeUser(u, h.userLocation(r, u.ID)))
	}
	writeSCIM(w, http.StatusOK, scim.NewListResponse(out, total, startIndex, len(out)))
}

func (h *Handler) scimCreateUser(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u, err := scim.DecodeUser(strings.NewReader(string(body)))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	if u.ID, err = credential.NewID(); err != nil {
		h.scimFail(w, r, err)
		return
	}
	u.Created, u.Modified = h.Now(), h.Now()
	if err := h.store.CreateSCIMUser(r.Context(), u); err != nil {
		h.scimFail(w, r, err)
		return
	}
	location := h.userLocation(r, u.ID)
	w.Header().Set("Location", location)
	writeSCIM(w, http.StatusCreated, scim.EncodeUser(u, location))
}

func (h *Handler) scimGetUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.store.SCIMUser(r.Context(), r.PathValue("id"))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeUser(u, h.userLocation(r, u.ID)))
}

func (h *Handler) scimReplaceUser(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u, err := scim.DecodeUser(strings.NewReader(string(body)))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u.ID, u.Modified = r.PathValue("id"), h.Now()
	if err := h.store.ReplaceSCIMUser(r.Context(), u); err != nil {
		h.scimFail(w, r, err)
		return
	}
	h.scimGetUser(w, r)
}

func (h *Handler) scimPatchUser(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	change, err := scim.UserPatch(strings.NewReader(string(body)))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	u, err := h.store.PatchSCIMUser(r.Context(), r.PathValue("id"), change, h.Now())
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeUser(u, h.userLocation(r, u.ID)))
}

func (h *Handler) scimDeleteUser(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteSCIMUser(r.Context(), r.PathValue("id")); err != nil {
		h.scimFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Use `bytes.NewReader(body)` instead of `strings.NewReader(string(body))` if you prefer — either is fine; be consistent within the file.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handler/ && go vet ./internal/handler/`
Expected: PASS (all existing handler tests too).

- [ ] **Step 5: Commit**

```bash
git add internal/handler/scim.go internal/handler/scim_test.go internal/handler/handler.go
git commit -m "feat(awd): SCIM discovery and user provisioning behind AWD_SCIM_TOKEN

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: SCIM handler — Groups

**Files:**
- Modify: `internal/handler/scim.go` (routes + handlers)
- Test: `internal/handler/scim_groups_test.go`

**Interfaces:**
- Consumes: Task 6 helpers (`requireSCIM`, `writeSCIM`, `scimFail`, `scimBody`, `scimBase`, `listParams`), `scim.DecodeGroup`, `scim.EncodeGroup`, `scim.GroupPatch`; test helpers `newSCIMServer`, `scimDo`, `createUser`, `aliceUser`.
- Produces: test helper `createGroup(t, srv, body) string`.

- [ ] **Step 1: Failing tests** — `internal/handler/scim_groups_test.go`:

```go
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func createGroup(t *testing.T, srv *httptest.Server, body string) string {
	t.Helper()
	status, out := scimDo(t, srv, http.MethodPost, "/scim/v2/Groups", body)
	if status != http.StatusCreated {
		t.Fatalf("creating group: %d %v", status, out)
	}
	return out["id"].(string)
}

func groupBody(name string, members ...string) string {
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"` + name + `","members":[`
	for i, m := range members {
		if i > 0 {
			body += ","
		}
		body += `{"value":"` + m + `"}`
	}
	return body + `]}`
}

func memberIDs(group map[string]any) []string {
	out := []string{}
	members, _ := group["members"].([]any)
	for _, m := range members {
		out = append(out, m.(map[string]any)["value"].(string))
	}
	return out
}

func TestSCIMGroupLifecycle(t *testing.T) {
	srv := newSCIMServer(t)
	alice := createUser(t, srv, aliceUser)

	status, created := scimDo(t, srv, http.MethodPost, "/scim/v2/Groups", groupBody("platform", alice))
	if status != http.StatusCreated || created["displayName"] != "platform" || !equal(memberIDs(created), []string{alice}) {
		t.Fatalf("POST = %d %v", status, created)
	}
	id := created["id"].(string)

	if status, got := scimDo(t, srv, http.MethodGet, "/scim/v2/Groups/"+id+"?excludedAttributes=members", ""); status != 200 || got["members"] != nil {
		t.Errorf("GET excluding members = %d %v", status, got)
	}

	// Entra: PATCH answers 204 when no attributes are requested.
	status, _ = scimDo(t, srv, http.MethodPatch, "/scim/v2/Groups/"+id,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Remove","path":"members","value":[{"value":"`+alice+`"}]}]}`)
	if status != http.StatusNoContent {
		t.Errorf("PATCH = %d, want 204", status)
	}
	// With attributes requested, the resource comes back.
	status, patched := scimDo(t, srv, http.MethodPatch, "/scim/v2/Groups/"+id+"?attributes=members",
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"add","path":"members","value":[{"value":"`+alice+`"}]}]}`)
	if status != 200 || !equal(memberIDs(patched), []string{alice}) {
		t.Errorf("PATCH with attributes = %d %v", status, patched)
	}

	status, replaced := scimDo(t, srv, http.MethodPut, "/scim/v2/Groups/"+id, groupBody("platform-eng"))
	if status != 200 || replaced["displayName"] != "platform-eng" || len(memberIDs(replaced)) != 0 {
		t.Errorf("PUT = %d %v", status, replaced)
	}

	if status, _ := scimDo(t, srv, http.MethodDelete, "/scim/v2/Groups/"+id, ""); status != http.StatusNoContent {
		t.Errorf("DELETE = %d", status)
	}
	if status, _ := scimDo(t, srv, http.MethodGet, "/scim/v2/Groups/"+id, ""); status != 404 {
		t.Errorf("GET after delete = %d", status)
	}
}

func TestSCIMGroupErrors(t *testing.T) {
	srv := newSCIMServer(t)
	createGroup(t, srv, groupBody("platform"))
	cases := []struct {
		name, method, path, body string
		status                   int
		scimType                 string
	}{
		{"member is not a user", "POST", "/scim/v2/Groups", groupBody("mobile", "ghost"), 400, "invalidValue"},
		{"duplicate displayName", "POST", "/scim/v2/Groups", groupBody("PLATFORM"), 409, "uniqueness"},
		{"userName filter on groups", "GET", "/scim/v2/Groups?filter=userName%20eq%20%22a%22", "", 400, "invalidFilter"},
		{"unknown id", "PATCH", "/scim/v2/Groups/nope",
			`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"add","path":"members","value":[]}]}`, 404, ""},
	}
	for _, tc := range cases {
		status, out := scimDo(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || (tc.scimType != "" && out["scimType"] != tc.scimType) {
			t.Errorf("%s: %d %v, want %d %s", tc.name, status, out, tc.status, tc.scimType)
		}
	}
}

func TestSCIMGroupListFilterAndExcludedMembers(t *testing.T) {
	srv := newSCIMServer(t)
	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("platform", alice))
	createGroup(t, srv, groupBody("mobile"))

	_, list := scimDo(t, srv, http.MethodGet, "/scim/v2/Groups?filter=displayName+eq+%22Platform%22&excludedAttributes=members", "")
	resources := list["Resources"].([]any)
	if list["totalResults"] != float64(1) || len(resources) != 1 {
		t.Fatalf("list = %v", list)
	}
	if g := resources[0].(map[string]any); g["displayName"] != "platform" || g["members"] != nil {
		t.Errorf("group = %v", g)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run SCIMGroup`
Expected: FAIL, 404/405 from the mux (routes not registered).

- [ ] **Step 3: Implement** — append the routes in `scimRoutes`:

```go
	mux.HandleFunc("GET /scim/v2/Groups", h.requireSCIM(h.scimListGroups))
	mux.HandleFunc("POST /scim/v2/Groups", h.requireSCIM(h.scimCreateGroup))
	mux.HandleFunc("GET /scim/v2/Groups/{id}", h.requireSCIM(h.scimGetGroup))
	mux.HandleFunc("PUT /scim/v2/Groups/{id}", h.requireSCIM(h.scimReplaceGroup))
	mux.HandleFunc("PATCH /scim/v2/Groups/{id}", h.requireSCIM(h.scimPatchGroup))
	mux.HandleFunc("DELETE /scim/v2/Groups/{id}", h.requireSCIM(h.scimDeleteGroup))
```

and the handlers, appended to `internal/handler/scim.go`:

```go
func (h *Handler) groupLocation(r *http.Request, id string) string {
	return scimBase(r) + "/Groups/" + id
}

// withMembers reports whether the IdP wants members in the answer. Both
// target IdPs ask to exclude them when reading large groups.
func withMembers(r *http.Request) bool {
	for _, attr := range strings.Split(r.URL.Query().Get("excludedAttributes"), ",") {
		if strings.EqualFold(strings.TrimSpace(attr), "members") {
			return false
		}
	}
	return true
}

func (h *Handler) scimListGroups(w http.ResponseWriter, r *http.Request) {
	filter, err := scim.ParseFilter(r.URL.Query().Get("filter"), model.SCIMAttrDisplayName, model.SCIMAttrExternalID)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	startIndex, count, err := listParams(r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	groups, total, err := h.store.ListSCIMGroups(r.Context(), filter, startIndex, count)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	members := withMembers(r)
	out := make([]scim.Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, scim.EncodeGroup(g, h.groupLocation(r, g.ID), members))
	}
	writeSCIM(w, http.StatusOK, scim.NewListResponse(out, total, startIndex, len(out)))
}

func (h *Handler) scimCreateGroup(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g, err := scim.DecodeGroup(strings.NewReader(string(body)))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	if g.ID, err = credential.NewID(); err != nil {
		h.scimFail(w, r, err)
		return
	}
	g.Created, g.Modified = h.Now(), h.Now()
	if err := h.store.CreateSCIMGroup(r.Context(), g); err != nil {
		h.scimFail(w, r, err)
		return
	}
	stored, err := h.store.SCIMGroup(r.Context(), g.ID)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	location := h.groupLocation(r, g.ID)
	w.Header().Set("Location", location)
	writeSCIM(w, http.StatusCreated, scim.EncodeGroup(stored, location, true))
}

func (h *Handler) scimGetGroup(w http.ResponseWriter, r *http.Request) {
	g, err := h.store.SCIMGroup(r.Context(), r.PathValue("id"))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeGroup(g, h.groupLocation(r, g.ID), withMembers(r)))
}

func (h *Handler) scimReplaceGroup(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g, err := scim.DecodeGroup(strings.NewReader(string(body)))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g.ID, g.Modified = r.PathValue("id"), h.Now()
	if err := h.store.ReplaceSCIMGroup(r.Context(), g); err != nil {
		h.scimFail(w, r, err)
		return
	}
	h.scimGetGroup(w, r)
}

// scimPatchGroup answers 204 unless the IdP asked for attributes back;
// Entra accepts either, and 204 spares serializing a large member list.
func (h *Handler) scimPatchGroup(w http.ResponseWriter, r *http.Request) {
	body, err := scimBody(w, r)
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	change, err := scim.GroupPatch(strings.NewReader(string(body)))
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	g, err := h.store.PatchSCIMGroup(r.Context(), r.PathValue("id"), change, h.Now())
	if err != nil {
		h.scimFail(w, r, err)
		return
	}
	if r.URL.Query().Get("attributes") == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeSCIM(w, http.StatusOK, scim.EncodeGroup(g, h.groupLocation(r, g.ID), withMembers(r)))
}

func (h *Handler) scimDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteSCIMGroup(r.Context(), r.PathValue("id")); err != nil {
		h.scimFail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handler/ && go vet ./internal/handler/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/scim.go internal/handler/scim_groups_test.go
git commit -m "feat(awd): SCIM group provisioning with transactional member PATCH

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Resolution — bundles, `GET /v1/groups`, `GET /v1/groups/resolve`, vendor replays

**Files:**
- Create: `internal/handler/resolve.go`
- Modify: `internal/handler/bundle.go` (replace the snapshot block), `internal/handler/groups.go` (`getGroups`), `internal/handler/handler.go` (one route)
- Test: `internal/handler/resolve_test.go`, `internal/handler/scim_vendor_test.go`

**Interfaces:**
- Consumes: `store.SCIMGroupsFor`, `store.SCIMCounts`, `store.ListSCIMUsers`, `policy.UnionGroups` (Task 1); test helpers `enroll`, `mintToken`, `fetchBundle`, `decodeBundle`, `groupedRuleSet`, `putGroups`, `getAs`, `equal`, `newSCIMServer`, `scimDo`, `createUser`, `createGroup`, `groupBody`.
- Produces:

```go
type groupSources struct {
	User          string   `json:"user"`
	Authored      []string `json:"authored"`
	Snapshot      []string `json:"snapshot"`
	SCIM          []string `json:"scim"`
	Effective     []string `json:"effective"`
	SCIMNearMatch *string  `json:"scimNearMatch"`
}
func (h *Handler) resolveGroups(ctx context.Context, ruleSet *policy.RuleSet, user string) (groupSources, error)
// GET /v1/groups body gains: "hasSnapshot": bool, "scim": {users, activeUsers, groups} (present when SCIMToken set)
```

- [ ] **Step 1: Failing tests** — `internal/handler/resolve_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestBundleGroupsIncludeActiveSCIMGroups(t *testing.T) {
	srv := newSCIMServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	before := fetchBundle(t, srv, credential, nil)
	beforeETag := before.Header.Get("ETag")

	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("mobile", alice))

	resp := fetchBundle(t, srv, credential, http.Header{"If-None-Match": {beforeETag}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; a SCIM membership must change the ETag", resp.StatusCode)
	}
	if b := decodeBundle(t, resp); !equal(b.Groups, []string{"mobile", "platform"}) {
		t.Errorf("groups = %v, want authored platform ∪ scim mobile", b.Groups)
	}

	// Deactivation drops the SCIM group; the authored one stays.
	scimDo(t, srv, http.MethodPatch, "/scim/v2/Users/"+alice,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}`)
	if b := decodeBundle(t, fetchBundle(t, srv, credential, nil)); !equal(b.Groups, []string{"platform"}) {
		t.Errorf("groups after deactivation = %v, want [platform]", b.Groups)
	}
}

func TestBundleWithoutSCIMDataIsUnchanged(t *testing.T) {
	plain := newServer(t)
	withSCIM := newSCIMServer(t)
	var etags []string
	for _, srv := range []*httptest.Server{plain, withSCIM} {
		post(t, srv, "/v1/policy/revisions", groupedRuleSet)
		_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
		etags = append(etags, fetchBundle(t, srv, credential, nil).Header.Get("ETag"))
	}
	if etags[0] != etags[1] {
		t.Errorf("ETags differ (%v): enabling SCIM with no data must not change any bundle", etags)
	}
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestResolveShowsEverySource(t *testing.T) {
	srv := newSCIMServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	putGroups(t, srv, `{"members":{"alice@acme.com":["oncall"]}}`, adminToken)
	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("mobile", alice))

	resp := getAs(t, srv, "/v1/groups/resolve?user=alice@acme.com", adminToken)
	var got struct {
		User          string   `json:"user"`
		Authored      []string `json:"authored"`
		Snapshot      []string `json:"snapshot"`
		SCIM          []string `json:"scim"`
		Effective     []string `json:"effective"`
		SCIMNearMatch *string  `json:"scimNearMatch"`
	}
	decodeJSON(t, resp, &got)
	if resp.StatusCode != 200 || !equal(got.Authored, []string{"platform"}) || !equal(got.Snapshot, []string{"oncall"}) ||
		!equal(got.SCIM, []string{"mobile"}) || !equal(got.Effective, []string{"mobile", "oncall", "platform"}) || got.SCIMNearMatch != nil {
		t.Errorf("resolve = %d %+v", resp.StatusCode, got)
	}
}

func TestResolveFlagsACaseMismatch(t *testing.T) {
	srv := newSCIMServer(t)
	createUser(t, srv, `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"Alice@acme.com"}`)
	resp := getAs(t, srv, "/v1/groups/resolve?user=alice@acme.com", adminToken)
	var got struct {
		SCIM          []string `json:"scim"`
		SCIMNearMatch *string  `json:"scimNearMatch"`
	}
	decodeJSON(t, resp, &got)
	if got.SCIMNearMatch == nil || *got.SCIMNearMatch != "Alice@acme.com" || len(got.SCIM) != 0 || got.SCIM == nil {
		t.Errorf("resolve = %+v; want scim [] and the near match named", got)
	}
}

func TestResolveNeedsAdminAndAUser(t *testing.T) {
	srv := newSCIMServer(t)
	if resp := getAs(t, srv, "/v1/groups/resolve?user=a@acme.com", ""); resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	if resp := getAs(t, srv, "/v1/groups/resolve", adminToken); resp.StatusCode != 400 {
		t.Errorf("no user: %d, want 400", resp.StatusCode)
	}
}

func TestGetGroupsReportsSCIMCounts(t *testing.T) {
	srv := newSCIMServer(t)
	if resp := getAs(t, srv, "/v1/groups", adminToken); resp.StatusCode != 404 {
		t.Errorf("nothing at all: %d, want 404", resp.StatusCode)
	}
	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("mobile", alice))

	resp := getAs(t, srv, "/v1/groups", adminToken)
	var got struct {
		HasSnapshot bool `json:"hasSnapshot"`
		SCIM        *struct {
			Users, ActiveUsers, Groups int
		} `json:"scim"`
		Source *string `json:"source"`
	}
	decodeJSON(t, resp, &got)
	if resp.StatusCode != 200 || got.HasSnapshot || got.SCIM == nil || got.SCIM.Users != 1 || got.SCIM.Groups != 1 || got.Source != nil {
		t.Errorf("groups = %d %+v", resp.StatusCode, got)
	}

	putGroups(t, srv, `{"source":"okta-export","members":{}}`, adminToken)
	resp = getAs(t, srv, "/v1/groups", adminToken)
	decodeJSON(t, resp, &got)
	if !got.HasSnapshot || got.Source == nil || *got.Source != "okta-export" || got.SCIM == nil {
		t.Errorf("groups with a snapshot = %+v", got)
	}
}

func TestGetGroupsWithoutSCIMHasNoSCIMField(t *testing.T) {
	srv := newServer(t)
	putGroups(t, srv, `{"members":{}}`, adminToken)
	resp := getAs(t, srv, "/v1/groups", adminToken)
	var got map[string]any
	decodeJSON(t, resp, &got)
	if _, ok := got["scim"]; ok || got["hasSnapshot"] != true {
		t.Errorf("groups = %v", got)
	}
}
```

Add `"net/http/httptest"` to that file's imports.

`internal/handler/scim_vendor_test.go` — full provisioning sequences as each IdP sends them:

```go
package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// provisioningStep is one IdP request and the groups alice's bundle must
// show afterwards. Bodies use {alice} and {group} placeholders filled in
// from earlier responses.
type provisioningStep struct {
	name, method, path, body string
	wantStatus               int
	wantGroups               []string
}

// replay runs an IdP's provisioning sequence against a fresh server with
// alice enrolled, checking her bundle's groups after every step.
func replay(t *testing.T, steps []provisioningStep) {
	t.Helper()
	srv := newSCIMServer(t)
	post(t, srv, "/v1/policy/revisions", `{"version":"v","rules":[{"name":"base","agents":{"claude":{"managed":{"model":"sonnet"}}}}]}`)
	_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	ids := map[string]string{}
	fill := func(s string) string {
		for placeholder, id := range ids {
			s = strings.ReplaceAll(s, placeholder, id)
		}
		return s
	}
	for _, step := range steps {
		status, out := scimDo(t, srv, step.method, fill(step.path), fill(step.body))
		if status != step.wantStatus {
			t.Fatalf("%s: status %d %v, want %d", step.name, status, out, step.wantStatus)
		}
		if id, ok := out["id"].(string); ok && step.method == http.MethodPost {
			if _, isUser := out["userName"]; isUser {
				ids["{alice}"] = id
			} else {
				ids["{group}"] = id
			}
		}
		if b := decodeBundle(t, fetchBundle(t, srv, credential, nil)); !equal(b.Groups, step.wantGroups) {
			t.Errorf("%s: bundle groups = %v, want %v", step.name, b.Groups, step.wantGroups)
		}
	}
}

const (
	patchOp   = `"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"]`
	userURN   = `urn:ietf:params:scim:schemas:core:2.0:User`
	groupURN  = `urn:ietf:params:scim:schemas:core:2.0:Group`
)

func TestOktaProvisioningSequence(t *testing.T) {
	q := url.QueryEscape(`userName eq "alice@acme.com"`)
	replay(t, []provisioningStep{
		{"look up before create", "GET", "/scim/v2/Users?filter=" + q + "&startIndex=1&count=100", "", 200, []string{}},
		{"create user", "POST", "/scim/v2/Users", `{"schemas":["` + userURN + `"],"userName":"alice@acme.com",
		  "name":{"givenName":"Alice","familyName":"Anders"},"emails":[{"primary":true,"value":"alice@acme.com","type":"work"}],
		  "displayName":"Alice Anders","locale":"en-US","externalId":"00u1abcd","groups":[],"password":"xY9!","active":true}`, 201, []string{}},
		{"push group", "POST", "/scim/v2/Groups", `{"schemas":["` + groupURN + `"],"displayName":"platform","members":[]}`, 201, []string{}},
		{"add member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"add","path":"members",
		  "value":[{"value":"{alice}","display":"alice@acme.com"}]}]}`, 204, []string{"platform"}},
		{"rename group", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"replace",
		  "value":{"id":"{group}","displayName":"platform-eng"}}]}`, 204, []string{"platform-eng"}},
		{"deactivate", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"replace","value":{"active":false}}]}`, 200, []string{}},
		{"reactivate", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"replace","value":{"active":true}}]}`, 200, []string{"platform-eng"}},
		{"remove member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"remove",
		  "path":"members[value eq \"{alice}\"]"}]}`, 204, []string{}},
		{"delete group", "DELETE", "/scim/v2/Groups/{group}", "", 204, []string{}},
	})
}

func TestEntraProvisioningSequence(t *testing.T) {
	q := url.QueryEscape(`userName eq "alice@acme.com"`)
	replay(t, []provisioningStep{
		{"look up before create", "GET", "/scim/v2/Users?filter=" + q, "", 200, []string{}},
		{"create user", "POST", "/scim/v2/Users", `{"schemas":["` + userURN + `","urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"],
		  "externalId":"alice","userName":"alice@acme.com","active":true,"displayName":"Alice Anders",
		  "emails":[{"primary":true,"type":"work","value":"alice@acme.com"}],"meta":{"resourceType":"User"},
		  "name":{"formatted":"Alice Anders","familyName":"Anders","givenName":"Alice"},"roles":[],
		  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User":{"department":"Eng"}}`, 201, []string{}},
		{"look up group", "GET", "/scim/v2/Groups?excludedAttributes=members&filter=" + url.QueryEscape(`displayName eq "oncall"`), "", 200, []string{}},
		{"create group", "POST", "/scim/v2/Groups", `{"schemas":["` + groupURN + `"],"externalId":"8aa1a5c0","displayName":"oncall","meta":{"resourceType":"Group"},"members":[]}`, 201, []string{}},
		{"add member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"Add","path":"members","value":[{"value":"{alice}"}]}]}`, 204, []string{"oncall"}},
		{"update attributes", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[
		  {"op":"Replace","path":"displayName","value":"Alice A."},
		  {"op":"Add","path":"emails[type eq \"work\"].value","value":"alice@acme.com"}]}`, 200, []string{"oncall"}},
		{"disable", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"Replace","path":"active","value":"False"}]}`, 200, []string{}},
		{"enable", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"Replace","path":"active","value":"True"}]}`, 200, []string{"oncall"}},
		{"remove member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"Remove","path":"members","value":[{"value":"{alice}"}]}]}`, 204, []string{}},
		{"add back", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"Add","path":"members","value":[{"value":"{alice}"}]}]}`, 204, []string{"oncall"}},
		{"delete user", "DELETE", "/scim/v2/Users/{alice}", "", 204, []string{}},
	})
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run 'Resolve|SCIMGroups|Provisioning|GetGroups|BundleGroupsInclude|WithoutSCIM'`
Expected: FAIL — bundle groups lack SCIM groups; `/v1/groups/resolve` 404; no `hasSnapshot`.

- [ ] **Step 3: Implement** — create `internal/handler/resolve.go`:

```go
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

// groupSources is a user's groups by where each came from. The bundle
// serves Effective; /v1/groups/resolve shows all of it, so an operator can
// answer "why does alice get this policy" without reading three tables.
type groupSources struct {
	User      string   `json:"user"`
	Authored  []string `json:"authored"`
	Snapshot  []string `json:"snapshot"`
	SCIM      []string `json:"scim"`
	Effective []string `json:"effective"`
	// SCIMNearMatch names a SCIM userName equal to User ignoring case but
	// not exactly — an IdP attribute mapping to fix. Resolution does not
	// fold case, so without this the mismatch would only show up as a user
	// quietly missing their groups.
	SCIMNearMatch *string `json:"scimNearMatch"`
}

// resolveGroups gathers a user's groups from every source. With no
// snapshot and no SCIM data, Effective is the authored list alone, exactly
// as before either source existed.
func (h *Handler) resolveGroups(ctx context.Context, ruleSet *policy.RuleSet, user string) (groupSources, error) {
	s := groupSources{User: user, Authored: policy.UnionGroups(ruleSet.GroupsFor(user))}

	snapshot, err := h.store.CurrentGroupSnapshot(ctx)
	switch {
	case err == nil:
		s.Snapshot = policy.UnionGroups(snapshot.Members[user])
	case errors.Is(err, model.ErrNotFound):
		s.Snapshot = []string{}
	default:
		return groupSources{}, err
	}

	if s.SCIM, err = h.store.SCIMGroupsFor(ctx, user); err != nil {
		return groupSources{}, err
	}
	s.Effective = policy.UnionGroups(s.Authored, s.Snapshot, s.SCIM)
	return s, nil
}

// getGroupsResolve shows one user's resolution, per source.
func (h *Handler) getGroupsResolve(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		writeError(w, http.StatusBadRequest, "the user query parameter is required")
		return
	}
	ruleSet := &policy.RuleSet{}
	revision, err := h.store.CurrentRuleSet(r.Context())
	switch {
	case err == nil:
		ruleSet = &revision.RuleSet
	case errors.Is(err, model.ErrNotFound):
	default:
		h.fail(w, r, err)
		return
	}
	s, err := h.resolveGroups(r.Context(), ruleSet, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	near, _, err := h.store.ListSCIMUsers(r.Context(), model.SCIMFilter{Attribute: model.SCIMAttrUserName, Value: user}, 1, 1)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if len(near) == 1 && near[0].UserName != user {
		s.SCIMNearMatch = &near[0].UserName
	}
	writeJSON(w, http.StatusOK, s)
}
```

Check: `ruleSet.GroupsFor(user)` may return nil — `UnionGroups` turns it into `[]string{}`; its order becomes sorted, matching the bundle.

In `internal/handler/bundle.go`, replace the whole block from the comment `// Group resolution is the union of …` through `bundle := ruleSet.Slice(policy.UnionGroups(ruleSet.GroupsFor(machine.User), synced))` with:

```go
	// Group resolution is the union of what the policy authored, what the
	// IdP last exported and what SCIM provisioned; with neither feed, the
	// authored map alone, exactly as before either existed.
	groups, err := h.resolveGroups(r.Context(), ruleSet, machine.User)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	bundle := ruleSet.Slice(groups.Effective)
```

Remove imports that become unused (`goimports`/`go vet` will say which).

In `internal/handler/groups.go`, replace `getGroups`:

```go
// groupsView is GET /v1/groups: the snapshot's detail when there is one,
// and SCIM's counts when SCIM is configured. The embedded pointer drops
// the snapshot fields entirely when there is no snapshot, so a caller
// never mistakes zero values for an empty export.
type groupsView struct {
	*groupsDetail
	HasSnapshot bool              `json:"hasSnapshot"`
	SCIM        *model.SCIMCounts `json:"scim,omitempty"`
}

// getGroups shows what the server resolves against. With no snapshot and
// no SCIM data it is a 404, as before SCIM existed.
func (h *Handler) getGroups(w http.ResponseWriter, r *http.Request) {
	var view groupsView
	snapshot, err := h.store.CurrentGroupSnapshot(r.Context())
	switch {
	case err == nil:
		d := detail(snapshot)
		view.groupsDetail, view.HasSnapshot = &d, true
	case errors.Is(err, model.ErrNotFound):
	default:
		h.fail(w, r, err)
		return
	}
	if h.SCIMToken != "" {
		counts, err := h.store.SCIMCounts(r.Context())
		if err != nil {
			h.fail(w, r, err)
			return
		}
		view.SCIM = &counts
	}
	if !view.HasSnapshot && (view.SCIM == nil || *view.SCIM == (model.SCIMCounts{})) {
		writeError(w, http.StatusNotFound, "no group snapshot has been posted and SCIM holds nothing")
		return
	}
	writeJSON(w, http.StatusOK, view)
}
```

In `Routes` (`handler.go`), after the `GET /v1/groups` line:

```go
	mux.HandleFunc("GET /v1/groups/resolve", h.requireAdmin(h.getGroupsResolve))
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handler/ && go vet ./internal/handler/`
Expected: PASS, including the pre-existing `TestGetGroupsIs404BeforeAnySnapshot` and `TestBundleWithAnEmptySnapshotIsTheAuthoredBundle`.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/
git commit -m "feat(awd): resolve bundle groups from SCIM too, and show why

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: `AWD_SCIM_TOKEN`, CLI output, `awd groups resolve`, e2e

**Files:**
- Modify: `internal/config/config.go`, `cmd/awd/main.go`
- Test: `internal/config/config_test.go` (if it exists; otherwise the e2e covers it), `cmd/awd/e2e_test.go`

**Interfaces:**
- Consumes: `GET /v1/groups` (`hasSnapshot`, `scim`), `GET /v1/groups/resolve` (Task 8); e2e helpers `startServerEnv`, `runAwd`, `writePolicy`, `groupedPolicyYAML`, `e2eAdminToken`.
- Produces: `config.Config.SCIMToken`; CLI `awd groups resolve USER [--url URL]`.

- [ ] **Step 1: Failing e2e test** — append to `cmd/awd/e2e_test.go`:

```go
const e2eSCIMToken = "e2e-scim-token"

// scimRequest sends a SCIM request to the e2e server and returns the
// decoded body.
func scimRequest(t *testing.T, base, method, path, body string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e2eSCIMToken)
	req.Header.Set("Content-Type", "application/scim+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %d %v", method, path, resp.StatusCode, out)
	}
	return out
}

func TestSCIMProvisionedGroupReachesTheBundle(t *testing.T) {
	s := startServerEnv(t, []string{"AWD_SCIM_TOKEN=" + e2eSCIMToken})
	if out, code := runAwd(t, "apply", writePolicy(t, groupedPolicyYAML), "--url", s.url); code != 0 {
		t.Fatalf("apply: %s", out)
	}
	user := scimRequest(t, s.url, http.MethodPost, "/scim/v2/Users",
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@acme.com"}`)
	scimRequest(t, s.url, http.MethodPost, "/scim/v2/Groups",
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"mobile","members":[{"value":"`+user["id"].(string)+`"}]}`)
	scimRequest(t, s.url, http.MethodPost, "/scim/v2/Users",
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"Bob@acme.com"}`)

	out, code := runAwd(t, "enroll-token", "alice@acme.com", "--url", s.url)
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
	}
	if err := json.NewDecoder(bundleResp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	if strings.Join(bundle.Groups, ",") != "mobile,platform" {
		t.Errorf("groups = %v, want authored platform ∪ scim mobile", bundle.Groups)
	}

	summary, code := runAwd(t, "groups", "--url", s.url)
	if code != 0 || !strings.Contains(summary, "no group snapshot") || !strings.Contains(summary, "scim users: 2 (2 active)") {
		t.Errorf("groups exited %d: %s", code, summary)
	}

	resolved, code := runAwd(t, "groups", "resolve", "alice@acme.com", "--url", s.url)
	if code != 0 || !strings.Contains(resolved, "scim: mobile") || !strings.Contains(resolved, "effective: mobile, platform") {
		t.Errorf("groups resolve exited %d: %s", code, resolved)
	}
	warned, _ := runAwd(t, "groups", "resolve", "bob@acme.com", "--url", s.url)
	if !strings.Contains(warned, `"Bob@acme.com"`) {
		t.Errorf("no near-match warning: %s", warned)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/awd/ -run SCIMProvisioned`
Expected: FAIL — SCIM routes answer 503 (token not read from env).

- [ ] **Step 3: Implement**

`internal/config/config.go` — add to `Config` after `AdminToken`:

```go
	// SCIMToken is the bearer token the identity provider presents on
	// /scim/v2. Unset means SCIM is disabled.
	SCIMToken string
```

and in `FromEnv`'s literal: `SCIMToken: os.Getenv("AWD_SCIM_TOKEN"),`.

`cmd/awd/main.go`:

1. Usage — add after the `awd groups [--url URL]` line:

```
  awd groups resolve USER [--url URL] show one user's groups by source
```

and after the `AWD_ADMIN_TOKEN` env line:

```
  AWD_SCIM_TOKEN             bearer token the identity provider presents on /scim/v2; unset disables SCIM
```

2. `serve()` — after `h.AdminToken = cfg.AdminToken` and its warning:

```go
	h.SCIMToken = cfg.SCIMToken
	if cfg.SCIMToken != "" {
		log.Info("SCIM provisioning is enabled at /scim/v2")
	}
```

3. Dispatch — in the `case "groups":` branch, route `resolve` like `apply`:

```go
	case "groups":
		if len(argv) > 1 && argv[1] == "apply" {
			return groupsApply(argv[2:])
		}
		if len(argv) > 1 && argv[1] == "resolve" {
			return groupsResolve(argv[2:])
		}
		return groups(argv[1:])
```

(Match the existing shape of that branch; read it first — the snippet assumes `argv[0] == "groups"`.)

4. `groupSummary` gains:

```go
	// HasSnapshot is false when only SCIM data exists. A server from
	// before SCIM never sends it, and always had a snapshot on a 200.
	HasSnapshot *bool `json:"hasSnapshot"`
	// SCIM is present when the server has SCIM enabled.
	SCIM *struct {
		Users       int `json:"users"`
		ActiveUsers int `json:"activeUsers"`
		Groups      int `json:"groups"`
	} `json:"scim"`
```

5. `groups()` — after the successful request, replace the final `Printf` with:

```go
	if summary.HasSnapshot == nil || *summary.HasSnapshot {
		fmt.Printf("source: %s\nsynced: %s\nby: %s\nusers: %d\ngroups: %d\n",
			orUnknownSource(summary.Source), summary.SyncedAt.Format(time.RFC3339), orNoneString(summary.AppliedBy), summary.Users, summary.Groups)
	} else {
		fmt.Println("no group snapshot has been applied")
	}
	if summary.SCIM != nil {
		fmt.Printf("scim users: %d (%d active)\nscim groups: %d\n", summary.SCIM.Users, summary.SCIM.ActiveUsers, summary.SCIM.Groups)
	}
	return nil
```

6. New command:

```go
// groupsResolve prints one user's groups by source, and warns when SCIM
// holds the user under a different case — the mapping mistake resolution
// deliberately does not paper over.
func groupsResolve(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("groups resolve takes one argument: the enrolled user")
	}
	var s struct {
		Authored      []string `json:"authored"`
		Snapshot      []string `json:"snapshot"`
		SCIM          []string `json:"scim"`
		Effective     []string `json:"effective"`
		SCIMNearMatch *string  `json:"scimNearMatch"`
	}
	if err := adminRequest(http.MethodGet, a.url, "/v1/groups/resolve?user="+url.QueryEscape(a.positional[0]), nil, &s); err != nil {
		return err
	}
	list := func(groups []string) string {
		if len(groups) == 0 {
			return "(none)"
		}
		return strings.Join(groups, ", ")
	}
	fmt.Printf("authored: %s\nsnapshot: %s\nscim: %s\neffective: %s\n", list(s.Authored), list(s.Snapshot), list(s.SCIM), list(s.Effective))
	if s.SCIMNearMatch != nil {
		fmt.Printf("warning: SCIM has %q, which differs from %q only in case; resolution matches exactly, so its SCIM groups do not apply. Fix the IdP's userName mapping.\n",
			*s.SCIMNearMatch, a.positional[0])
	}
	return nil
}
```

Add `"net/url"` to imports; `url` may shadow a local variable named `url` in `apply` — if so, import it as `neturl "net/url"`.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS everywhere (Postgres suite skips without the env var).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go cmd/awd/
git commit -m "feat(awd): AWD_SCIM_TOKEN, SCIM counts in awd groups, and groups resolve

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Documentation and spec amendments

**Files:**
- Modify: `README.md` (after the "Group membership has two sources…" paragraph, ~line 196–209)
- Modify: `docs/superpowers/specs/2026-09-24-scim-provisioning-design.md`
- Modify: `docs/superpowers/specs/2026-09-22-idp-group-sync-design.md` (one pointer line)

- [ ] **Step 1: README** — change "Group membership has two sources." to "Group membership has three sources." and, after that paragraph, add:

```markdown
The third is SCIM 2.0 provisioning. Set `AWD_SCIM_TOKEN` and point the
identity provider at `https://<awd>/scim/v2`:

- **Okta**: add a SCIM 2.0 app integration with *HTTP Header* authentication
  and the token as the bearer value; enable *Create Users*, *Update User
  Attributes*, *Deactivate Users* and *Push Groups*. Map `userName` to the
  email users enroll with.
- **Entra ID**: in an enterprise application, set provisioning to
  *Automatic*, the tenant URL to the SCIM root, and the secret token to
  `AWD_SCIM_TOKEN`. Map `userName` to whichever of `userPrincipalName` or
  `mail` equals the enrolled email.

SCIM groups union with the other two sources. Deactivating or deleting a
user in the IdP drops its SCIM groups on the next `aw-sync` cycle; it does
**not** revoke the user's machines — use `awd revoke` for that. Resolution
matches `userName` to the enrolled email exactly, and
`awd groups resolve <user>` shows each source's groups and warns when SCIM
holds the user under a different case.
```

- [ ] **Step 2: Spec amendments** — in `2026-09-24-scim-provisioning-design.md`:
  - Status line → `Status: implemented 2026-09-24.`
  - In the Model block, `ID string // server-generated UUID` → `ID string // server-generated, 32 hex characters (credential.NewID)`.
  - In "Admin additions", replace "the 404 for "no snapshot" becomes a 200 with `snapshot: null` when SCIM holds data" with: "the snapshot fields are omitted and `"hasSnapshot": false` is set when there is no snapshot but SCIM holds data; `hasSnapshot` is `true` whenever a snapshot exists. With neither, it is still a 404."

  In `2026-09-22-idp-group-sync-design.md`, under "Out of scope, deliberately", after the SCIM bullet add: `  SCIM was later built as a third source: see 2026-09-24-scim-provisioning-design.md.`

- [ ] **Step 3: Verify**

Run: `go test ./... && git diff --stat`
Expected: PASS; only docs changed in this task.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/superpowers/specs/
git commit -m "docs: SCIM provisioning setup for Okta and Entra, and spec amendments

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Self-review notes (resolved)

- Spec coverage: decisions (Task 8 union, no revocation), `internal/scim` (4–5), model/store (2–3), resolution (1, 8), SCIM API table (6–7), admin additions + CLI (8–9), failure-mode table (6–8 tests), testing section (every listed item mapped), docs (10).
- Addition #3 from review (`GET /v1/groups` without snapshot) is realised as `hasSnapshot` rather than `snapshot: null`, because the existing body is flat; Task 10 amends the spec to match.
- Type names checked across tasks: `SCIMUserChange`, `SCIMGroupChange`, `SCIMMemberOp{Kind, Users}`, `SCIMCounts`, `groupSources`, `resolveGroups`, `scimFail`, `writeSCIM`, `scimBody`, `withMembers`.
- Every commit builds: Task 2 adds `SCIMStore` as its own interface; Task 3 embeds it into `Store` once Postgres implements it.
