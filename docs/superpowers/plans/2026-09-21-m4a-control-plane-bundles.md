# M4a: Control plane bundles, machines, enrollment, admin auth — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `awd` can enroll a machine for a user, resolve that user's groups from the authored policy, and serve the machine a group-resolved bundle with repo matchers intact; apply and admin routes are authenticated.

**Architecture:** The policy compiler is split into a server half (`RuleSet.Slice(groups) → Bundle`) and a client half (`Bundle.Compile(repo) → Document`), with the old `RuleSet.Compile(Subject)` redefined as their composition. The store gains machines and single-use enrollment tokens behind the existing conformance suite. The handler gains two auth middlewares: admin (bearer `AWD_ADMIN_TOKEN`) and machine (bearer credential, hash lookup). Nothing on the client side changes in this milestone; `aw-policy` keeps fetching `/v1/policy` until M4c.

**Tech Stack:** Go 1.22, stdlib `net/http` ServeMux, `log/slog`, `pgx/v5` (Postgres), `sigs.k8s.io/yaml`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` — sections "Control plane", "Failure modes", "Testing", "Sequencing (M4a)".

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run bare `go mod tidy` (it pulls test deps needing Go 1.25); no new dependencies are needed for this plan.
- No `gcc` on this machine: `go test -race` cannot run here. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit.
- Postgres suite skips without `AWD_TEST_DATABASE_URL`. Docker is available; the suite **must** be run against a throwaway Postgres whenever store code changes:
  ```bash
  docker start awd-pg 2>/dev/null || docker run -d --name awd-pg -e POSTGRES_PASSWORD=pw -p 5432:5432 postgres:16
  export AWD_TEST_DATABASE_URL='postgres://postgres:pw@localhost:5432/postgres?sslmode=disable'
  go test ./internal/store/ -count=1
  ```
  The suite truncates every table per case: never point it at a real database. The milestone-3 baseline (10 cases) was verified on 2026-09-21 this way.
- Sentinel errors live in `internal/model` and are mapped to HTTP status only in `internal/handler`.
- Return `make([]T, 0)`, never a nil slice, from list operations.
- Every new store method is exercised by `internal/store/storetest` so Memory and Postgres cannot drift.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv`.
- Match the codebase's comment style: a comment says *why*, in full sentences, and is present on every exported identifier.

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/policy/bundle.go` (new) | `Bundle` type, `RuleSet.Slice`, `RuleSet.GroupsFor`, `Bundle.Compile` |
| `internal/policy/compile.go` (modify) | `RuleSet.Compile` becomes `Slice().Compile()` |
| `internal/policy/policy.go` (modify) | `RuleSet.Groups` field |
| `internal/policy/ruleset.go` (modify) | `Validate` rejects an empty user key in `groups` |
| `internal/policy/bundle_test.go` (new) | slice/compile behaviour, equivalence with the old compiler |
| `internal/model/model.go` (modify) | `Machine`, `EnrollmentToken`, `ErrUnauthorized` |
| `internal/credential/credential.go` (new) | random credentials and IDs, SHA-256 hashing |
| `internal/credential/credential_test.go` (new) | |
| `internal/store/store.go` (modify) | interface: enrollment tokens, machines |
| `internal/store/memory.go` (modify) | in-memory implementation |
| `internal/store/postgres.go` (modify) | Postgres implementation, `Truncate` covers the new tables |
| `internal/store/migrations/0002_machines.sql` (new) | tables |
| `internal/store/storetest/conformance.go` (modify) | new cases |
| `internal/handler/auth.go` (new) | `requireAdmin`, `requireMachine`, bearer parsing |
| `internal/handler/handler.go` (modify) | `AdminToken` field, new routes, apply protected |
| `internal/handler/enroll.go` (new) | enrollment-token and enroll handlers, machines list/delete |
| `internal/handler/bundle.go` (new) | `GET /v1/bundle` |
| `internal/handler/handler_test.go` (modify) | helpers gain auth; existing apply tests send the admin token |
| `internal/handler/auth_test.go`, `enroll_test.go`, `bundle_test.go` (new) | |
| `internal/config/config.go` (modify) | `AdminToken` |
| `cmd/awd/main.go` (modify) | `enroll-token`, `machines`, `revoke`; apply sends bearer |
| `cmd/awd/e2e_test.go` (modify) | admin token in env; new commands |
| `examples/org-policy.yaml` (modify) | `groups:` block |
| `README.md` (modify) | enrollment flow |

---

### Task 1: Bundle type and the compiler split

**Files:**
- Create: `internal/policy/bundle.go`
- Create: `internal/policy/bundle_test.go`
- Modify: `internal/policy/policy.go` (RuleSet struct, ~line 60)
- Modify: `internal/policy/compile.go:96-113` (`RuleSet.Compile`)
- Modify: `internal/policy/ruleset.go:36-60` (`Validate`)

**Interfaces:**
- Consumes: `policy.RuleSet`, `policy.Rule`, `policy.Match.Matches(Subject)`, `policy.Document`, `Document.mergeAgent` (all existing).
- Produces:
  ```go
  type Bundle struct {
      Version string   `json:"version,omitempty"`
      User    string   `json:"user,omitempty"`
      Groups  []string `json:"groups,omitempty"`
      Rules   []Rule   `json:"rules,omitempty"`
  }
  func (rs *RuleSet) GroupsFor(user string) []string
  func (rs *RuleSet) Slice(groups []string) *Bundle
  func (b *Bundle) Compile(repo string) *Document
  // RuleSet gains:
  Groups map[string][]string `json:"groups,omitempty"`
  ```

- [ ] **Step 1: Write the failing tests**

`internal/policy/bundle_test.go`:

```go
package policy_test

import (
	"reflect"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

// slicingRuleSet has one rule of each kind: ungrouped, grouped, repo-scoped,
// and both grouped and repo-scoped.
func slicingRuleSet() *policy.RuleSet {
	claude := func(model string) map[string]policy.AgentConfig {
		return map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": model}}}
	}
	return &policy.RuleSet{
		Version: "v1",
		Groups: map[string][]string{
			"alice@acme.com": {"platform", "security"},
			"bob@acme.com":   {"mobile"},
		},
		Rules: []policy.Rule{
			{Name: "baseline", Agents: claude("sonnet")},
			{Name: "platform", Match: policy.Match{Groups: []string{"platform"}}, Agents: claude("opus")},
			{Name: "mobile", Match: policy.Match{Groups: []string{"mobile"}}, Agents: claude("haiku")},
			{Name: "payments", Match: policy.Match{Repos: []string{"github.com/acme/payments*"}}, Agents: claude("opus-payments")},
			{Name: "platform-payments",
				Match:  policy.Match{Groups: []string{"platform"}, Repos: []string{"github.com/acme/payments*"}},
				Agents: claude("opus-platform-payments")},
		},
	}
}

func ruleNames(rules []policy.Rule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Name)
	}
	return out
}

func TestGroupsForReturnsTheAuthoredMembership(t *testing.T) {
	rs := slicingRuleSet()

	if got := rs.GroupsFor("alice@acme.com"); !reflect.DeepEqual(got, []string{"platform", "security"}) {
		t.Errorf("GroupsFor(alice) = %v", got)
	}
	if got := rs.GroupsFor("nobody@acme.com"); len(got) != 0 {
		t.Errorf("GroupsFor(unknown) = %v, want empty: an unknown user gets the baseline only", got)
	}
	var nilSet *policy.RuleSet
	if got := nilSet.GroupsFor("alice@acme.com"); got == nil || len(got) != 0 {
		t.Errorf("GroupsFor on a nil rule set = %v, want an empty non-nil slice", got)
	}
}

func TestSliceKeepsUngroupedGroupMatchingAndRepoScopedRules(t *testing.T) {
	rs := slicingRuleSet()

	bundle := rs.Slice([]string{"platform"})

	want := []string{"baseline", "platform", "payments", "platform-payments"}
	if got := ruleNames(bundle.Rules); !reflect.DeepEqual(got, want) {
		t.Errorf("Slice(platform) rules = %v, want %v (repo-scoped rules stay in; the client resolves them)", got, want)
	}
	if bundle.Version != "v1" || !reflect.DeepEqual(bundle.Groups, []string{"platform"}) {
		t.Errorf("bundle = %+v, want the version and groups carried", bundle)
	}
}

func TestSliceDropsRulesForOtherGroups(t *testing.T) {
	rs := slicingRuleSet()

	bundle := rs.Slice(nil)

	want := []string{"baseline", "payments"}
	if got := ruleNames(bundle.Rules); !reflect.DeepEqual(got, want) {
		t.Errorf("Slice(no groups) rules = %v, want %v", got, want)
	}
}

func TestSliceKeepsRepoMatchersVerbatim(t *testing.T) {
	rs := slicingRuleSet()

	bundle := rs.Slice([]string{"platform"})

	for _, rule := range bundle.Rules {
		if rule.Name == "platform-payments" && !reflect.DeepEqual(rule.Match.Repos, []string{"github.com/acme/payments*"}) {
			t.Errorf("repo matcher was altered: %+v", rule.Match)
		}
	}
}

func TestBundleCompileResolvesRepoScopedRulesOnTheClient(t *testing.T) {
	bundle := slicingRuleSet().Slice([]string{"platform"})

	inPayments := bundle.Compile("github.com/acme/payments-api")
	elsewhere := bundle.Compile("github.com/acme/website")
	noRepo := bundle.Compile("")

	if got := inPayments.Agent("claude").Managed["model"]; got != "opus-platform-payments" {
		t.Errorf("in payments: model = %v, want the last matching rule's", got)
	}
	if got := elsewhere.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("elsewhere: model = %v, want the group rule's", got)
	}
	if got := noRepo.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("outside a repo: model = %v, want the group rule's", got)
	}
	if !reflect.DeepEqual(inPayments.AppliedRules, []string{"baseline", "platform", "payments", "platform-payments"}) {
		t.Errorf("applied rules = %v", inPayments.AppliedRules)
	}
}

func TestBundleCompileOnNilIsEmpty(t *testing.T) {
	var b *policy.Bundle
	if doc := b.Compile("github.com/acme/x"); doc == nil || len(doc.Agents) != 0 {
		t.Errorf("nil bundle compiled to %+v, want an empty document", doc)
	}
}

func TestSliceThenCompileEqualsCompile(t *testing.T) {
	// The split must not change what a subject receives. This runs the
	// shipped example policy plus the synthetic one through both paths for
	// every combination of group and repo the rules mention.
	example, err := policy.LoadRuleSet("../../examples/org-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, rs := range []*policy.RuleSet{example, slicingRuleSet()} {
		for _, groups := range [][]string{nil, {"platform"}, {"mobile"}, {"platform", "mobile"}} {
			for _, repo := range []string{"", "github.com/acme/payments-api", "github.com/acme/website"} {
				subject := policy.Subject{Groups: groups, Repo: repo}
				direct := rs.Compile(subject)
				split := rs.Slice(groups).Compile(repo)
				if !reflect.DeepEqual(direct, split) {
					t.Errorf("groups=%v repo=%q:\n direct = %+v\n split  = %+v", groups, repo, direct, split)
				}
			}
		}
	}
}

func TestValidateRejectsAnEmptyUserInGroups(t *testing.T) {
	rs := &policy.RuleSet{Version: "v1", Groups: map[string][]string{"": {"platform"}}}
	if err := rs.Validate(); err == nil {
		t.Error("Validate() = nil, want an error for a group entry with no user")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/leo/AI_STUFF/agent-wrapper && go test ./internal/policy/ 2>&1 | head`
Expected: build failure mentioning `rs.GroupsFor undefined`, `rs.Slice undefined`, `policy.Bundle`, and `unknown field Groups`.

- [ ] **Step 3: Add the `Groups` field to `RuleSet`**

In `internal/policy/policy.go`, change the `RuleSet` struct to:

```go
// RuleSet is a complete authored policy.
type RuleSet struct {
	// Version identifies this revision. Clients cache on it.
	Version string `json:"version,omitempty"`
	// Groups maps a user to the groups they belong to. It is authored with
	// the rules and versioned with them, so a membership change is a policy
	// revision like any other. An identity-provider sync can replace this
	// map later without changing what a client receives.
	Groups map[string][]string `json:"groups,omitempty"`
	// Rules apply in order. A later rule overrides an earlier one per key, so
	// the author controls precedence by ordering rather than by a priority
	// field that has to be kept consistent.
	Rules []Rule `json:"rules,omitempty"`
}
```

- [ ] **Step 4: Write `bundle.go`**

`internal/policy/bundle.go`:

```go
package policy

// Bundle is the part of a policy that applies to one user: every rule that
// is not scoped to a group the user is outside of, with repository matchers
// left in place. The control plane resolves groups, because only it knows
// them; the client resolves the repository, because only it knows where the
// session runs. The same JSON shape is served over HTTP and written to disk.
type Bundle struct {
	// Version is the policy revision the bundle was cut from.
	Version string `json:"version,omitempty"`
	// User is who the bundle was resolved for, for diagnostics.
	User string `json:"user,omitempty"`
	// Groups are the memberships the bundle was resolved with.
	Groups []string `json:"groups,omitempty"`
	// Rules are the surviving rules, in their authored order.
	Rules []Rule `json:"rules,omitempty"`
}

// GroupsFor returns the authored group memberships of user. An unknown user
// has none, and receives only the rules that target everyone. The result is
// never nil, so a caller can hand it to JSON and get a list, not null.
func (rs *RuleSet) GroupsFor(user string) []string {
	if rs == nil {
		return make([]string, 0)
	}
	groups := rs.Groups[user]
	out := make([]string, len(groups))
	copy(out, groups)
	return out
}

// Slice keeps the rules that could apply to a subject with these groups:
// those with no group constraint and those whose groups intersect. Rules
// scoped to a repository are kept whatever the repository, since that is
// resolved later. The rules are shared with the rule set, not copied; nothing
// downstream mutates a rule.
func (rs *RuleSet) Slice(groups []string) *Bundle {
	if rs == nil {
		return &Bundle{Rules: make([]Rule, 0)}
	}
	bundle := &Bundle{Version: rs.Version, Groups: groups, Rules: make([]Rule, 0, len(rs.Rules))}
	for _, rule := range rs.Rules {
		if len(rule.Match.Groups) > 0 && !anyIn(rule.Match.Groups, groups) {
			continue
		}
		bundle.Rules = append(bundle.Rules, rule)
	}
	return bundle
}

// Compile resolves the bundle for a session in repo (empty outside a
// repository) into the document a client applies. Repository matching is the
// only decision left; group matching was made when the bundle was cut, and
// re-checking it here is harmless because the bundle carries its groups.
func (b *Bundle) Compile(repo string) *Document {
	if b == nil {
		return &Document{}
	}
	subject := Subject{Groups: b.Groups, Repo: repo}
	doc := &Document{Version: b.Version}
	for _, rule := range b.Rules {
		if !rule.Match.Matches(subject) {
			continue
		}
		doc.AppliedRules = append(doc.AppliedRules, rule.Name)
		for agentName, config := range rule.Agents {
			doc.mergeAgent(agentName, config)
		}
	}
	return doc
}
```

- [ ] **Step 5: Redefine `RuleSet.Compile` as the composition**

In `internal/policy/compile.go`, replace the body of `func (rs *RuleSet) Compile(s Subject) *Document` (the whole function, lines ~96-113) with:

```go
// Compile resolves the rule set for one subject into the document a client
// applies. It is the server-side slice followed by the client-side compile,
// in one call, for callers that know the whole subject at once. It never
// mutates the rule set, so the same RuleSet can serve concurrent requests.
func (rs *RuleSet) Compile(s Subject) *Document {
	return rs.Slice(s.Groups).Compile(s.Repo)
}
```

- [ ] **Step 6: Reject an empty user key in `Validate`**

In `internal/policy/ruleset.go`, inside `Validate`, after the version check and before the `seen := ...` line, add:

```go
	for user := range rs.Groups {
		if user == "" {
			return fmt.Errorf("groups has an entry with no user")
		}
	}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/policy/ ./internal/handler/ ./cmd/awd/`
Expected: all PASS. If `TestSliceThenCompileEqualsCompile` fails on `AppliedRules` being `nil` versus empty, the two paths differ only in a nil-vs-empty slice; make `Bundle.Compile` and the old behaviour agree by leaving `AppliedRules` nil when nothing applied (the old code did), which is what the code above does.

- [ ] **Step 8: Format, vet, commit**

```bash
gofmt -l . && go vet ./... && git add internal/policy && git commit -m "feat(policy): split compilation into a server-side slice and a client-side compile

A Bundle is every rule that could apply to a user, with repository matchers
kept for the client to resolve. RuleSet.Compile is now Slice then Compile,
and a test proves the split changes nothing for any subject. RuleSet gains an
authored groups map, the seam an identity-provider sync will later fill.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 2: Credentials package and model types

**Files:**
- Create: `internal/credential/credential.go`
- Create: `internal/credential/credential_test.go`
- Modify: `internal/model/model.go`

**Interfaces:**
- Produces:
  ```go
  // internal/credential
  func New() (plain, hash string, err error)   // 32 random bytes, base64url plain, hex sha256 hash
  func Hash(plain string) string               // hex sha256
  func NewID() (string, error)                 // 16 random bytes, hex
  // internal/model
  type Machine struct { ID, User, Name, OS, CredentialHash string; EnrolledAt, LastSeenAt time.Time; LastBundleVersion string }
  type EnrollmentToken struct { Hash, User string; ExpiresAt, UsedAt time.Time }
  var ErrUnauthorized = errors.New("unauthorized")
  ```

- [ ] **Step 1: Write the failing tests**

`internal/credential/credential_test.go`:

```go
package credential_test

import (
	"encoding/hex"
	"testing"

	"github.com/acme/agent-wrapper/internal/credential"
)

func TestNewReturnsAPlainCredentialAndItsHash(t *testing.T) {
	plain, hash, err := credential.New()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) < 40 {
		t.Errorf("plain credential %q is too short for 32 random bytes", plain)
	}
	if hash != credential.Hash(plain) {
		t.Errorf("hash %q does not match Hash(plain) %q", hash, credential.Hash(plain))
	}
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 {
		t.Errorf("hash %q is not hex SHA-256", hash)
	}
}

func TestNewIsUnpredictable(t *testing.T) {
	a, _, _ := credential.New()
	b, _, _ := credential.New()
	if a == b {
		t.Error("two credentials are identical")
	}
}

func TestNewIDIsHexAndUnique(t *testing.T) {
	a, err := credential.NewID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := credential.NewID()
	if _, err := hex.DecodeString(a); err != nil || len(a) != 32 {
		t.Errorf("id %q is not 16 hex bytes", a)
	}
	if a == b {
		t.Error("two ids are identical")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/credential/`
Expected: `no non-test Go files`.

- [ ] **Step 3: Implement**

`internal/credential/credential.go`:

```go
// Package credential mints the secrets the control plane hands out and the
// hashes it stores.
//
// A credential is shown once, at enrollment, and only its hash is kept, so a
// copy of the database does not yield a working credential. Comparing hashes
// rather than plaintext also means the lookup key is safe to log.
package credential

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// New returns a fresh credential and the hash to store for it.
func New() (plain, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("credential: reading random bytes: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, Hash(plain), nil
}

// Hash returns the hex SHA-256 of a plaintext credential, which is what the
// store keeps and what a lookup uses.
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// NewID returns a random identifier for a stored object.
func NewID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("credential: reading random bytes: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
```

Append to `internal/model/model.go` (after `Revision`):

```go
// Machine is a device enrolled for one user. It fetches that user's bundle
// with a credential shown once at enrollment; only the credential's hash is
// stored.
type Machine struct {
	ID   string `json:"id"`
	User string `json:"user"`
	// Name is what the machine called itself at enrollment, for listings.
	Name string `json:"name,omitempty"`
	// OS is the machine's GOOS, so an operator can see which files aw-sync
	// writes there.
	OS             string `json:"os,omitempty"`
	CredentialHash string `json:"-"`
	EnrolledAt     time.Time `json:"enrolledAt"`
	// LastSeenAt is the last successful bundle fetch; zero until the first.
	LastSeenAt time.Time `json:"lastSeenAt,omitempty"`
	// LastBundleVersion is the policy version served at that fetch.
	LastBundleVersion string `json:"lastBundleVersion,omitempty"`
}

// EnrollmentToken lets one machine enroll for one user, once, before it
// expires. Only the token's hash is stored.
type EnrollmentToken struct {
	Hash      string
	User      string
	ExpiresAt time.Time
	// UsedAt is zero until the token is consumed.
	UsedAt time.Time
}
```

And add to the `var (...)` block of sentinel errors:

```go
	// ErrUnauthorized means the caller presented no valid credential.
	ErrUnauthorized = errors.New("unauthorized")
```

- [ ] **Step 4: Run tests, vet, commit**

```bash
go test ./internal/credential/ ./internal/model/... && go vet ./... && gofmt -l . && git add internal/credential internal/model && git commit -m "feat: credential minting and the machine and enrollment-token model types

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 3: Store interface, Memory implementation, conformance cases

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/memory.go`
- Modify: `internal/store/storetest/conformance.go`

**Interfaces:**
- Produces (added to `store.Store`):
  ```go
  PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken) error
  ConsumeEnrollmentToken(ctx context.Context, hash string, now time.Time) (user string, err error)
  PutMachine(ctx context.Context, m model.Machine) error
  MachineByCredential(ctx context.Context, hash string) (model.Machine, error)
  TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion string) error
  ListMachines(ctx context.Context) ([]model.Machine, error)
  DeleteMachine(ctx context.Context, id string) error
  ```
  Error contract: `ConsumeEnrollmentToken` → `ErrNotFound` unknown hash, `ErrConflict` used or expired (`now` at or after `ExpiresAt`). `PutMachine` → `ErrBadInput` empty ID or CredentialHash, `ErrConflict` duplicate ID or duplicate CredentialHash. `MachineByCredential`, `TouchMachine`, `DeleteMachine` → `ErrNotFound`. `ListMachines` sorted by `EnrolledAt` then `ID`, never nil.

- [ ] **Step 1: Add the conformance cases (failing)**

In `internal/store/storetest/conformance.go`, extend the `tests` table in `Run` with:

```go
		{"an enrollment token is consumed once", tokenConsumedOnce},
		{"an unknown enrollment token is not found", tokenUnknown},
		{"an expired enrollment token is a conflict", tokenExpired},
		{"a machine can be found by its credential hash", machineByCredential},
		{"a machine needs an id and a credential hash", machineRequiresIDAndHash},
		{"a machine id is unique", machineIDUnique},
		{"a credential hash is unique", machineHashUnique},
		{"touching a machine records the fetch", machineTouch},
		{"machines list in enrollment order", machinesList},
		{"listing no machines yields an empty slice", machinesListEmpty},
		{"a deleted machine is gone", machineDelete},
```

Append these functions to the same file:

```go
func token(hash, user string, expires time.Time) model.EnrollmentToken {
	return model.EnrollmentToken{Hash: hash, User: user, ExpiresAt: expires}
}

func machine(id, user, hash string, at time.Time) model.Machine {
	return model.Machine{ID: id, User: user, Name: "laptop-" + id, OS: "linux", CredentialHash: hash, EnrolledAt: at}
}

func tokenConsumedOnce(t *testing.T, s store.Store) {
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutEnrollmentToken(ctx, token("h1", "alice@acme.com", now.Add(time.Hour))); err != nil {
		t.Fatalf("PutEnrollmentToken: %v", err)
	}

	user, err := s.ConsumeEnrollmentToken(ctx, "h1", now)
	if err != nil || user != "alice@acme.com" {
		t.Fatalf("first consume = %q, %v; want alice and nil", user, err)
	}
	_, err = s.ConsumeEnrollmentToken(ctx, "h1", now)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("second consume error = %v, want ErrConflict: a token enrolls one machine", err)
	}
}

func tokenUnknown(t *testing.T, s store.Store) {
	_, err := s.ConsumeEnrollmentToken(context.Background(), "nope", time.Now())
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func tokenExpired(t *testing.T, s store.Store) {
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutEnrollmentToken(ctx, token("h1", "alice@acme.com", now)); err != nil {
		t.Fatal(err)
	}
	_, err := s.ConsumeEnrollmentToken(ctx, "h1", now)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("consuming at the expiry instant: error = %v, want ErrConflict", err)
	}
}

func machineByCredential(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutMachine(ctx, machine("m1", "alice@acme.com", "c1", at)); err != nil {
		t.Fatalf("PutMachine: %v", err)
	}

	got, err := s.MachineByCredential(ctx, "c1")
	if err != nil {
		t.Fatalf("MachineByCredential: %v", err)
	}
	if got.ID != "m1" || got.User != "alice@acme.com" || got.Name != "laptop-m1" || got.OS != "linux" || !got.EnrolledAt.Equal(at) {
		t.Errorf("machine = %+v", got)
	}
	if _, err := s.MachineByCredential(ctx, "c2"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("unknown credential: error = %v, want ErrNotFound", err)
	}
}

func machineRequiresIDAndHash(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutMachine(ctx, model.Machine{User: "a", CredentialHash: "c"}); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("no id: error = %v, want ErrBadInput", err)
	}
	if err := s.PutMachine(ctx, model.Machine{ID: "m", User: "a"}); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("no hash: error = %v, want ErrBadInput", err)
	}
}

func machineIDUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Now()
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", at)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMachine(ctx, machine("m1", "b", "c2", at)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("error = %v, want ErrConflict", err)
	}
}

func machineHashUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Now()
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", at)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMachine(ctx, machine("m2", "b", "c1", at)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("error = %v, want ErrConflict: two machines must never share a credential", err)
	}
}

func machineTouch(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", at)); err != nil {
		t.Fatal(err)
	}
	seen := at.Add(time.Hour)
	if err := s.TouchMachine(ctx, "m1", seen, "v7"); err != nil {
		t.Fatalf("TouchMachine: %v", err)
	}
	got, _ := s.MachineByCredential(ctx, "c1")
	if !got.LastSeenAt.Equal(seen) || got.LastBundleVersion != "v7" {
		t.Errorf("after touch: %+v", got)
	}
	if err := s.TouchMachine(ctx, "missing", seen, "v7"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("touching an unknown machine: error = %v, want ErrNotFound", err)
	}
}

func machinesList(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"m3", "m1", "m2"} {
		if err := s.PutMachine(ctx, machine(id, "a", "c"+id, at.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListMachines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.ID)
		if m.CredentialHash != "" {
			t.Errorf("listing leaks a credential hash for %s", m.ID)
		}
	}
	if want := []string{"m3", "m1", "m2"}; !equalStrings(ids, want) {
		t.Errorf("ids = %v, want enrollment order %v", ids, want)
	}
}

func machinesListEmpty(t *testing.T, s store.Store) {
	got, err := s.ListMachines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("ListMachines on an empty store = %v, want an empty non-nil slice", got)
	}
}

func machineDelete(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, "m1"); err != nil {
		t.Fatalf("DeleteMachine: %v", err)
	}
	if _, err := s.MachineByCredential(ctx, "c1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("after delete: error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteMachine(ctx, "m1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("deleting twice: error = %v, want ErrNotFound", err)
	}
}

func equalStrings(a, b []string) bool {
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

Note: `ListMachines` must clear `CredentialHash` in its result; the conformance case checks it.

- [ ] **Step 2: Extend the interface**

In `internal/store/store.go`, add `"time"` to the imports and extend the interface:

```go
// Store holds policy revisions, enrolled machines and enrollment tokens.
type Store interface {
	// PutRuleSet stores a new revision. It returns model.ErrBadInput for a
	// revision without a version and model.ErrConflict when that version is
	// already stored, because a revision is immutable once written.
	PutRuleSet(ctx context.Context, r model.Revision) error
	// CurrentRuleSet returns the newest revision, or model.ErrNotFound when
	// no policy has been applied yet.
	CurrentRuleSet(ctx context.Context) (model.Revision, error)
	// Revisions returns up to limit revisions, newest first.
	Revisions(ctx context.Context, limit int) ([]model.Revision, error)

	// PutEnrollmentToken stores a token for later consumption.
	PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken) error
	// ConsumeEnrollmentToken marks the token used and returns the user it
	// enrolls. It returns model.ErrNotFound for an unknown hash and
	// model.ErrConflict for a token already used or expired at now. The
	// check and the mark are one atomic step, so two machines racing on one
	// token cannot both enroll.
	ConsumeEnrollmentToken(ctx context.Context, hash string, now time.Time) (user string, err error)

	// PutMachine stores an enrolled machine. It returns model.ErrBadInput
	// without an ID or credential hash and model.ErrConflict when either is
	// already stored.
	PutMachine(ctx context.Context, m model.Machine) error
	// MachineByCredential finds the machine holding this credential hash,
	// or model.ErrNotFound.
	MachineByCredential(ctx context.Context, hash string) (model.Machine, error)
	// TouchMachine records a successful bundle fetch, or model.ErrNotFound.
	TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion string) error
	// ListMachines returns every machine in enrollment order with its
	// credential hash cleared: a listing is for operators, and the hash is
	// the lookup key, not information.
	ListMachines(ctx context.Context) ([]model.Machine, error)
	// DeleteMachine revokes a machine, or model.ErrNotFound.
	DeleteMachine(ctx context.Context, id string) error
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/store/ 2>&1 | head -5`
Expected: `*Memory does not implement store.Store (missing method ...)`.

- [ ] **Step 4: Implement in Memory**

In `internal/store/memory.go`, add `"time"` to the imports, extend the struct and constructor, and append the methods:

```go
// Memory is an in-memory Store. It backs tests and a single-node evaluation
// deployment; state does not survive a restart.
type Memory struct {
	mu        sync.RWMutex
	revisions []model.Revision
	versions  map[string]bool
	nextSeq   int64
	tokens    map[string]model.EnrollmentToken // by hash
	machines  map[string]model.Machine         // by id
	byHash    map[string]string                // credential hash -> machine id
	order     []string                         // machine ids in enrollment order
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		versions: make(map[string]bool),
		tokens:   make(map[string]model.EnrollmentToken),
		machines: make(map[string]model.Machine),
		byHash:   make(map[string]string),
	}
}
```

(Replace the existing struct and `NewMemory`.) Then append:

```go
// PutEnrollmentToken stores a token.
func (m *Memory) PutEnrollmentToken(_ context.Context, t model.EnrollmentToken) error {
	if t.Hash == "" || t.User == "" {
		return fmt.Errorf("store: enrollment token needs a hash and a user: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[t.Hash] = t
	return nil
}

// ConsumeEnrollmentToken marks a token used, atomically.
func (m *Memory) ConsumeEnrollmentToken(_ context.Context, hash string, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[hash]
	if !ok {
		return "", fmt.Errorf("store: unknown enrollment token: %w", model.ErrNotFound)
	}
	if !t.UsedAt.IsZero() {
		return "", fmt.Errorf("store: enrollment token already used: %w", model.ErrConflict)
	}
	if !now.Before(t.ExpiresAt) {
		return "", fmt.Errorf("store: enrollment token expired: %w", model.ErrConflict)
	}
	t.UsedAt = now
	m.tokens[hash] = t
	return t.User, nil
}

// PutMachine stores an enrolled machine.
func (m *Memory) PutMachine(_ context.Context, mc model.Machine) error {
	if mc.ID == "" || mc.CredentialHash == "" {
		return fmt.Errorf("store: machine needs an id and a credential hash: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.machines[mc.ID]; exists {
		return fmt.Errorf("store: machine %q already exists: %w", mc.ID, model.ErrConflict)
	}
	if _, exists := m.byHash[mc.CredentialHash]; exists {
		return fmt.Errorf("store: credential already issued: %w", model.ErrConflict)
	}
	m.machines[mc.ID] = mc
	m.byHash[mc.CredentialHash] = mc.ID
	m.order = append(m.order, mc.ID)
	return nil
}

// MachineByCredential finds a machine by credential hash.
func (m *Memory) MachineByCredential(_ context.Context, hash string) (model.Machine, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byHash[hash]
	if !ok {
		return model.Machine{}, fmt.Errorf("store: no machine holds this credential: %w", model.ErrNotFound)
	}
	return m.machines[id], nil
}

// TouchMachine records a fetch.
func (m *Memory) TouchMachine(_ context.Context, id string, seenAt time.Time, bundleVersion string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mc, ok := m.machines[id]
	if !ok {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	mc.LastSeenAt = seenAt
	mc.LastBundleVersion = bundleVersion
	m.machines[id] = mc
	return nil
}

// ListMachines returns machines in enrollment order, hashes cleared.
func (m *Memory) ListMachines(_ context.Context) ([]model.Machine, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Machine, 0, len(m.order))
	for _, id := range m.order {
		mc := m.machines[id]
		mc.CredentialHash = ""
		out = append(out, mc)
	}
	return out, nil
}

// DeleteMachine revokes a machine.
func (m *Memory) DeleteMachine(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mc, ok := m.machines[id]
	if !ok {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	delete(m.machines, id)
	delete(m.byHash, mc.CredentialHash)
	for i, existing := range m.order {
		if existing == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the Memory suite**

Run: `go test ./internal/store/ -run TestMemory -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: every case PASS. `TestPostgres` still fails to compile until Task 4 — run only `-run TestMemory` here; the whole package compiles again after Task 4.

Do **not** commit yet: the package does not build for Postgres. Continue to Task 4 and commit both together.

---

### Task 4: Postgres implementation and migration

**Files:**
- Create: `internal/store/migrations/0002_machines.sql`
- Modify: `internal/store/postgres.go`

**Interfaces:** same as Task 3.

- [ ] **Step 1: Write the migration**

`internal/store/migrations/0002_machines.sql`:

```sql
-- An enrollment token lets one machine enroll for one user, once. Only the
-- token's hash is stored; the plaintext is shown to the administrator once.
CREATE TABLE IF NOT EXISTS enrollment_tokens (
    hash       TEXT        PRIMARY KEY,
    "user"     TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

-- A machine holds one credential, stored as its hash, and fetches one user's
-- bundle with it. The credential hash is the lookup key on every fetch.
CREATE TABLE IF NOT EXISTS machines (
    id                  TEXT        PRIMARY KEY,
    "user"              TEXT        NOT NULL,
    name                TEXT        NOT NULL DEFAULT '',
    os                  TEXT        NOT NULL DEFAULT '',
    credential_hash     TEXT        NOT NULL UNIQUE,
    enrolled_at         TIMESTAMPTZ NOT NULL,
    last_seen_at        TIMESTAMPTZ,
    last_bundle_version TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS machines_enrolled_at ON machines (enrolled_at, id);
```

- [ ] **Step 2: Implement the methods**

Append to `internal/store/postgres.go`:

```go
// PutEnrollmentToken stores a token.
func (p *Postgres) PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken) error {
	if t.Hash == "" || t.User == "" {
		return fmt.Errorf("store: enrollment token needs a hash and a user: %w", model.ErrBadInput)
	}
	const query = `
		INSERT INTO enrollment_tokens (hash, "user", expires_at)
		VALUES ($1, $2, $3)`
	if _, err := p.pool.Exec(ctx, query, t.Hash, t.User, t.ExpiresAt); err != nil {
		return fmt.Errorf("store: storing enrollment token: %w", err)
	}
	return nil
}

// ConsumeEnrollmentToken marks a token used. The UPDATE's WHERE clause is the
// atomic check: it matches only an unused, unexpired token, so of two
// concurrent consumers exactly one sees a row.
func (p *Postgres) ConsumeEnrollmentToken(ctx context.Context, hash string, now time.Time) (string, error) {
	const consume = `
		UPDATE enrollment_tokens
		SET used_at = $2
		WHERE hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING "user"`
	var user string
	err := p.pool.QueryRow(ctx, consume, hash, now).Scan(&user)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("store: consuming enrollment token: %w", err)
	}
	// Distinguish "unknown" from "used or expired" for the caller's status.
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM enrollment_tokens WHERE hash = $1)`, hash).Scan(&exists); err != nil {
		return "", fmt.Errorf("store: checking enrollment token: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("store: unknown enrollment token: %w", model.ErrNotFound)
	}
	return "", fmt.Errorf("store: enrollment token already used or expired: %w", model.ErrConflict)
}

// PutMachine stores an enrolled machine.
func (p *Postgres) PutMachine(ctx context.Context, m model.Machine) error {
	if m.ID == "" || m.CredentialHash == "" {
		return fmt.Errorf("store: machine needs an id and a credential hash: %w", model.ErrBadInput)
	}
	const query = `
		INSERT INTO machines (id, "user", name, os, credential_hash, enrolled_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := p.pool.Exec(ctx, query, m.ID, m.User, m.Name, m.OS, m.CredentialHash, m.EnrolledAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return fmt.Errorf("store: machine %q or its credential already exists: %w", m.ID, model.ErrConflict)
		}
		return fmt.Errorf("store: storing machine %q: %w", m.ID, err)
	}
	return nil
}

const selectMachine = `
	SELECT id, "user", name, os, credential_hash, enrolled_at, last_seen_at, last_bundle_version
	FROM machines`

// MachineByCredential finds a machine by credential hash.
func (p *Postgres) MachineByCredential(ctx context.Context, hash string) (model.Machine, error) {
	rows, _ := p.pool.Query(ctx, selectMachine+` WHERE credential_hash = $1`, hash)
	machines, err := collectMachines(rows)
	if err != nil {
		return model.Machine{}, err
	}
	if len(machines) == 0 {
		return model.Machine{}, fmt.Errorf("store: no machine holds this credential: %w", model.ErrNotFound)
	}
	return machines[0], nil
}

// TouchMachine records a fetch.
func (p *Postgres) TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE machines SET last_seen_at = $2, last_bundle_version = $3 WHERE id = $1`,
		id, seenAt, bundleVersion)
	if err != nil {
		return fmt.Errorf("store: touching machine %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

// ListMachines returns machines in enrollment order, hashes cleared.
func (p *Postgres) ListMachines(ctx context.Context) ([]model.Machine, error) {
	rows, _ := p.pool.Query(ctx, selectMachine+` ORDER BY enrolled_at, id`)
	machines, err := collectMachines(rows)
	if err != nil {
		return nil, err
	}
	for i := range machines {
		machines[i].CredentialHash = ""
	}
	return machines, nil
}

// DeleteMachine revokes a machine.
func (p *Postgres) DeleteMachine(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM machines WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: deleting machine %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

func collectMachines(rows pgx.Rows) ([]model.Machine, error) {
	defer rows.Close()
	out := make([]model.Machine, 0)
	for rows.Next() {
		var (
			m        model.Machine
			lastSeen *time.Time
		)
		if err := rows.Scan(&m.ID, &m.User, &m.Name, &m.OS, &m.CredentialHash, &m.EnrolledAt, &lastSeen, &m.LastBundleVersion); err != nil {
			return nil, fmt.Errorf("store: reading a machine: %w", err)
		}
		if lastSeen != nil {
			m.LastSeenAt = *lastSeen
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading machines: %w", err)
	}
	return out, nil
}
```

Replace `Truncate` with:

```go
// Truncate removes every row and restarts the sequences. It exists for the
// conformance suite, which needs a fresh store per case; never call it
// against a database that holds a real policy history.
func (p *Postgres) Truncate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `TRUNCATE policy_revisions, enrollment_tokens, machines RESTART IDENTITY`); err != nil {
		return fmt.Errorf("store: truncating: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Build, vet, run the whole store package against both stores**

Run:
```bash
go vet ./internal/store/... && docker start awd-pg 2>/dev/null || docker run -d --name awd-pg -e POSTGRES_PASSWORD=pw -p 5432:5432 postgres:16
sleep 2
AWD_TEST_DATABASE_URL='postgres://postgres:pw@localhost:5432/postgres?sslmode=disable' go test ./internal/store/ -count=1 -v 2>&1 | grep -E -- '--- (PASS|FAIL)|^ok|^FAIL'
```
Expected: `TestMemory` and `TestPostgres` both PASS with 21 subcases each (10 existing + 11 new); no `--- FAIL` line. A failure only under Postgres is a real divergence: fix the SQL, not the test. Migration `0002` applies on the existing container because every statement is `IF NOT EXISTS`.

- [ ] **Step 4: Commit Tasks 3 and 4 together** (only after both stores pass)

```bash
gofmt -l . && git add internal/store && git commit -m "feat(store): enrollment tokens and machines, in memory and in Postgres

Consuming a token is one atomic step in both stores, so two machines racing
on one token cannot both enroll. Listings clear the credential hash. The
Postgres store is committed unverified, as before: no database here.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 5: Admin authentication on the handler and the apply route

**Files:**
- Create: `internal/handler/auth.go`
- Create: `internal/handler/auth_test.go`
- Modify: `internal/handler/handler.go` (struct, `Routes`, `fail`)
- Modify: `internal/handler/handler_test.go` (`newServer`, `post`, `applyBaseline`)
- Modify: `internal/config/config.go`
- Modify: `cmd/awd/main.go` (`serve`, `apply`, usage)
- Modify: `cmd/awd/e2e_test.go` (`startServer`, `runAwd`)

**Interfaces:**
- Produces:
  ```go
  // handler.Handler gains:
  AdminToken string
  func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc
  func bearer(r *http.Request) string   // token after "Bearer ", or ""
  // config.Config gains:
  AdminToken string   // AWD_ADMIN_TOKEN
  ```
  Behaviour: `AdminToken == ""` → 503 `{"error":"admin token not configured"}`; missing/wrong bearer → 401; the compare is constant-time. `fail` maps `model.ErrUnauthorized` → 401.

- [ ] **Step 1: Update the handler test helpers so existing tests keep passing**

In `internal/handler/handler_test.go`:

Replace `newServer` with:

```go
// adminToken is what the test server expects on admin routes.
const adminToken = "test-admin-token"

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.AdminToken = adminToken
	h.Now = func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}
```

Replace `post` with a version that sends the admin bearer, and add `postAs`:

```go
// post sends JSON as an administrator.
func post(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	return postAs(t, srv, path, body, adminToken)
}

// postAs sends JSON with the given bearer token; empty sends none.
func postAs(t *testing.T, srv *httptest.Server, path, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
```

(Read the existing `post` first and keep whatever it did beyond this — the cleanup and the content-type — so nothing else changes.) In the test added in M3, `TestApplyingARuleSetThatFailsManagedValidationIsRejected`, add `h.AdminToken = adminToken` after `h := handler.New(...)`.

- [ ] **Step 2: Write the failing auth tests**

`internal/handler/auth_test.go`:

```go
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/store"
)

func TestApplyWithoutTheAdminTokenIs401(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/policy/revisions", baselineRuleSet, "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestApplyWithTheWrongAdminTokenIs401(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/policy/revisions", baselineRuleSet, "not-it")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminRoutesAre503WhenNoTokenIsConfigured(t *testing.T) {
	// An unset token must never mean "open": it means the route is off.
	h := handler.New(store.NewMemory(), nil)
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)

	resp := postAs(t, srv, "/v1/policy/revisions", baselineRuleSet, "anything")

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestReadingPolicyNeedsNoToken(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	resp := get(t, srv, "/v1/policy", nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the preview route stays open", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/handler/ 2>&1 | head -5`
Expected: `h.AdminToken undefined`.

- [ ] **Step 4: Implement auth**

`internal/handler/auth.go`:

```go
package handler

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearer returns the token in an Authorization: Bearer header, or "".
func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

// requireAdmin admits a request only with the configured admin token. With
// no token configured the route answers 503 rather than opening: an
// operator who forgot to set AWD_ADMIN_TOKEN must find out from the error,
// not from an audit.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.AdminToken == "" {
			writeError(w, http.StatusServiceUnavailable, "admin token not configured")
			return
		}
		presented := bearer(r)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(h.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}
```

In `internal/handler/handler.go`:

Add to the `Handler` struct, after `ManagedValidator`:

```go
	// AdminToken is the bearer token administrative routes require. Empty
	// disables them with a 503; it never leaves them open.
	AdminToken string
```

In `Routes`, change the revisions line to:

```go
	mux.HandleFunc("POST /v1/policy/revisions", h.requireAdmin(h.postRevision))
```

In `fail`, add before the `default:` case:

```go
	case errors.Is(err, model.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
```

- [ ] **Step 5: Run the handler tests**

Run: `go test ./internal/handler/`
Expected: PASS.

- [ ] **Step 6: Config and awd**

In `internal/config/config.go`, add to `Config`:

```go
	// AdminToken is the bearer token for administrative routes. Unset means
	// those routes are disabled.
	AdminToken string
```

and in `FromEnv`, in the `cfg := Config{...}` literal, add `AdminToken: os.Getenv("AWD_ADMIN_TOKEN"),`.

In `cmd/awd/main.go`:

In `serve`, after `h.ManagedValidator = schema.ForAgent`, add:

```go
	h.AdminToken = cfg.AdminToken
	if cfg.AdminToken == "" {
		log.Warn("AWD_ADMIN_TOKEN is unset; apply and machine administration are disabled")
	}
```

In `apply`, after `req.Header.Set("Content-Type", "application/json")`, add:

```go
	if token := os.Getenv("AWD_ADMIN_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
```

In `usage`, add under Environment:

```
  AWD_ADMIN_TOKEN       bearer token for apply and machine administration; unset disables them
```

- [ ] **Step 7: Update the e2e harness**

In `cmd/awd/e2e_test.go`:

Add near the top: `const e2eAdminToken = "e2e-admin-token"`.

In `startServer`, change the env line to:

```go
	cmd.Env = append(os.Environ(), "AWD_ADDR=127.0.0.1:0", "AWD_LOG_LEVEL=error", "AWD_ADMIN_TOKEN="+e2eAdminToken)
```

Replace `runAwd` with:

```go
func runAwd(t *testing.T, args ...string) (string, int) {
	t.Helper()
	return runAwdEnv(t, []string{"AWD_ADMIN_TOKEN=" + e2eAdminToken}, args...)
}

// runAwdEnv runs awd with extra environment on top of the process's own.
func runAwdEnv(t *testing.T, extra []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(built, args...)
	cmd.Env = append(append(os.Environ(), "AWD_LOG_LEVEL=error"), extra...)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running awd %v: %v", args, err)
		return "", 0
	}
}
```

Add a test:

```go
func TestApplyWithoutTheAdminTokenIsRefused(t *testing.T) {
	s := startServer(t)
	path := writePolicy(t, policyYAML)

	out, code := runAwdEnv(t, []string{"AWD_ADMIN_TOKEN="}, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0 without a token: %s", out)
	}
	if !strings.Contains(out, "401") {
		t.Errorf("output %q should show the 401", out)
	}
}
```

Note: `AWD_ADMIN_TOKEN=` (empty) appended after `os.Environ()` overrides any value inherited from the developer's shell, since later entries win in `exec.Cmd.Env`.

The e2e test in M3 that POSTs directly with `http.Post` (`TestApplyRejectsClaudeSettingsThatBreakTheSchema`) must now send the bearer: replace its `http.Post(...)` with a `http.NewRequest` + `req.Header.Set("Authorization", "Bearer "+e2eAdminToken)` + `http.DefaultClient.Do(req)`.

- [ ] **Step 8: Run everything, commit**

```bash
gofmt -l . && go vet ./... && go test ./... 2>&1 | grep -v '^ok' ; git add -A && git commit -m "feat(awd): admin bearer token on apply

AWD_ADMIN_TOKEN gates POST /v1/policy/revisions. Unset does not mean open:
the route answers 503 until an operator configures a token. awd apply sends
the same variable as a bearer.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 6: Enrollment tokens, enrollment, machine listing and revocation

**Files:**
- Create: `internal/handler/enroll.go`
- Create: `internal/handler/enroll_test.go`
- Modify: `internal/handler/handler.go` (`Routes`)

**Interfaces:**
- Consumes: `credential.New`, `credential.Hash`, `credential.NewID`, store methods from Task 3, `h.requireAdmin`, `h.Now`.
- Produces routes:
  - `POST /v1/enrollment-tokens` admin, body `{"user":"alice@acme.com","ttl":"24h"}` (ttl optional) → 201 `{"token":"...","user":"...","expiresAt":"..."}`
  - `POST /v1/machines/enroll` open, body `{"token":"...","name":"host","os":"linux"}` → 201 `{"machineId":"...","credential":"...","user":"..."}`; 404 unknown token; 409 used/expired; 422 missing token.
  - `GET /v1/machines` admin → 200 `[Machine]`
  - `DELETE /v1/machines/{id}` admin → 204; 404.

- [ ] **Step 1: Write the failing tests**

`internal/handler/enroll_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mintToken asks the server for an enrollment token as an administrator.
func mintToken(t *testing.T, srv *httptest.Server, user string) string {
	t.Helper()
	resp := post(t, srv, "/v1/enrollment-tokens", `{"user":"`+user+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("minting a token: status = %d", resp.StatusCode)
	}
	var body struct{ Token string }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}

// enroll enrolls a machine with a token and returns its credential.
func enroll(t *testing.T, srv *httptest.Server, token string) (machineID, credential string) {
	t.Helper()
	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"`+token+`","name":"laptop","os":"linux"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enrolling: status = %d", resp.StatusCode)
	}
	var body struct {
		MachineID  string `json:"machineId"`
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.MachineID, body.Credential
}

func TestMintingATokenNeedsTheAdminToken(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/enrollment-tokens", `{"user":"alice@acme.com"}`, "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMintingATokenNeedsAUser(t *testing.T) {
	srv := newServer(t)

	resp := post(t, srv, "/v1/enrollment-tokens", `{}`)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestATokenEnrollsOneMachine(t *testing.T) {
	srv := newServer(t)
	token := mintToken(t, srv, "alice@acme.com")

	id, credential := enroll(t, srv, token)
	if id == "" || credential == "" {
		t.Fatal("enrollment returned no machine id or credential")
	}

	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"`+token+`","name":"other"}`, "")
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("second enrollment status = %d, want 409", resp.StatusCode)
	}
}

func TestAnUnknownTokenIs404(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"made-up","name":"x"}`, "")

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestEnrollingWithoutATokenIs422(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/machines/enroll", `{"name":"x"}`, "")

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMachinesListShowsEnrollmentsWithoutCredentials(t *testing.T) {
	srv := newServer(t)
	enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	enroll(t, srv, mintToken(t, srv, "bob@acme.com"))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var machines []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		t.Fatal(err)
	}
	if len(machines) != 2 || machines[0]["user"] != "alice@acme.com" || machines[1]["user"] != "bob@acme.com" {
		t.Errorf("machines = %v", machines)
	}
	for _, m := range machines {
		if _, leaked := m["credentialHash"]; leaked {
			t.Error("listing leaks the credential hash")
		}
	}
}

func TestMachinesListNeedsTheAdminToken(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, "/v1/machines", nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRevokingAMachine(t *testing.T) {
	srv := newServer(t)
	id, _ := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/machines/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	again, _ := http.DefaultClient.Do(req)
	again.Body.Close()
	if again.StatusCode != http.StatusNotFound {
		t.Errorf("revoking twice: status = %d, want 404", again.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run 'Token|Enroll|Machines|Revok' 2>&1 | tail -12`
Expected: the routes 404 (unknown path), so status assertions fail.

- [ ] **Step 3: Implement**

`internal/handler/enroll.go`:

```go
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/acme/agent-wrapper/internal/credential"
	"github.com/acme/agent-wrapper/internal/model"
)

// defaultTokenTTL is how long an enrollment token lives unless the request
// says otherwise. A day covers "mint it now, run the installer this
// afternoon" without leaving tokens lying around for weeks.
const defaultTokenTTL = 24 * time.Hour

// postEnrollmentToken mints a single-use token that enrolls one machine for
// one user. The plaintext is returned once and never stored.
func (h *Handler) postEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		TTL  string `json:"ttl"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.User == "" {
		writeError(w, http.StatusUnprocessableEntity, "user is required")
		return
	}
	ttl := defaultTokenTTL
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "ttl must be a positive duration such as 24h")
			return
		}
		ttl = parsed
	}
	plain, hash, err := credential.New()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	expires := h.Now().Add(ttl)
	if err := h.store.PutEnrollmentToken(r.Context(), model.EnrollmentToken{Hash: hash, User: req.User, ExpiresAt: expires}); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "user": req.User, "expiresAt": expires})
}

// postEnroll exchanges an enrollment token for a machine credential. It is
// the one unauthenticated write, because the token is the credential.
func (h *Handler) postEnroll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Name  string `json:"name"`
		OS    string `json:"os"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusUnprocessableEntity, "token is required")
		return
	}
	now := h.Now()
	user, err := h.store.ConsumeEnrollmentToken(r.Context(), credential.Hash(req.Token), now)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := credential.NewID()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	plain, hash, err := credential.New()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	machine := model.Machine{ID: id, User: user, Name: req.Name, OS: req.OS, CredentialHash: hash, EnrolledAt: now}
	if err := h.store.PutMachine(r.Context(), machine); err != nil {
		h.fail(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "machine enrolled", "machine", id, "user", user, "name", req.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"machineId": id, "credential": plain, "user": user})
}

func (h *Handler) getMachines(w http.ResponseWriter, r *http.Request) {
	machines, err := h.store.ListMachines(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, machines)
}

func (h *Handler) deleteMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteMachine(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.log.InfoContext(r.Context(), "machine revoked", "machine", id)
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON reads a small JSON body, rejecting unknown fields so a typo in
// a request is an error rather than a silently ignored option.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return errors.New("the request body is not valid: " + err.Error())
	}
	return nil
}
```

In `Routes` in `handler.go`, add:

```go
	mux.HandleFunc("POST /v1/enrollment-tokens", h.requireAdmin(h.postEnrollmentToken))
	mux.HandleFunc("POST /v1/machines/enroll", h.postEnroll)
	mux.HandleFunc("GET /v1/machines", h.requireAdmin(h.getMachines))
	mux.HandleFunc("DELETE /v1/machines/{id}", h.requireAdmin(h.deleteMachine))
```

- [ ] **Step 4: Run, vet, commit**

```bash
go test ./internal/handler/ && gofmt -l . && go vet ./... && git add internal/handler && git commit -m "feat(awd): enrollment tokens, machine enrollment, listing and revocation

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 7: `GET /v1/bundle` with machine authentication

**Files:**
- Create: `internal/handler/bundle.go`
- Create: `internal/handler/bundle_test.go`
- Modify: `internal/handler/auth.go` (`requireMachine`)
- Modify: `internal/handler/handler.go` (`Routes`)

**Interfaces:**
- Consumes: `store.MachineByCredential`, `store.TouchMachine`, `store.CurrentRuleSet`, `policy.RuleSet.GroupsFor`, `policy.RuleSet.Slice`, `etagOf`, `credential.Hash`.
- Produces: `GET /v1/bundle` — bearer machine credential; 401 unknown; 200 `policy.Bundle` JSON with `ETag`; 304 on `If-None-Match`; 200 empty bundle (`{"user":..., "groups":[], "rules":[]}`) when no policy exists. Touches the machine with the served version on 200 and 304.
  ```go
  func (h *Handler) requireMachine(next func(http.ResponseWriter, *http.Request, model.Machine)) http.HandlerFunc
  ```

- [ ] **Step 1: Write the failing tests**

`internal/handler/bundle_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

const groupedRuleSet = `{
  "version": "g1",
  "groups": {"alice@acme.com": ["platform"], "bob@acme.com": ["mobile"]},
  "rules": [
    {"name": "baseline", "agents": {"claude": {"managed": {"model": "sonnet"}}}},
    {"name": "platform", "match": {"groups": ["platform"]}, "agents": {"claude": {"managed": {"model": "opus"}}}},
    {"name": "mobile", "match": {"groups": ["mobile"]}, "agents": {"claude": {"managed": {"model": "haiku"}}}},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {"claude": {"managed": {"model": "opus-payments"}}}}
  ]
}`

func fetchBundle(t *testing.T, srv *httptest.Server, credential string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/bundle", nil)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBundle(t *testing.T, resp *http.Response) policy.Bundle {
	t.Helper()
	var b policy.Bundle
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBundleWithoutACredentialIs401(t *testing.T) {
	srv := newServer(t)

	if resp := fetchBundle(t, srv, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if resp := fetchBundle(t, srv, "not-a-credential", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong credential: status = %d, want 401", resp.StatusCode)
	}
}

func TestBundleIsResolvedForTheMachinesUser(t *testing.T) {
	srv := newServer(t)
	if resp := post(t, srv, "/v1/policy/revisions", groupedRuleSet); resp.StatusCode != http.StatusCreated {
		t.Fatalf("apply: %d", resp.StatusCode)
	}
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	b := decodeBundle(t, resp)
	if b.User != "alice@acme.com" || len(b.Groups) != 1 || b.Groups[0] != "platform" || b.Version != "g1" {
		t.Errorf("bundle header = %+v", b)
	}
	names := make([]string, 0)
	for _, r := range b.Rules {
		names = append(names, r.Name)
	}
	if want := []string{"baseline", "platform", "payments"}; !equal(names, want) {
		t.Errorf("rules = %v, want %v: mobile is dropped, payments kept for the client", names, want)
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("no ETag")
	}
}

func TestBundleIs304WhenUnchanged(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	first := fetchBundle(t, srv, alice, nil)

	second := fetchBundle(t, srv, alice, http.Header{"If-None-Match": {first.Header.Get("ETag")}})

	if second.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", second.StatusCode)
	}
}

func TestBundleForAnUnknownUserIsTheBaseline(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, carol := enroll(t, srv, mintToken(t, srv, "carol@acme.com"))

	b := decodeBundle(t, fetchBundle(t, srv, carol, nil))

	if len(b.Groups) != 0 {
		t.Errorf("groups = %v, want none", b.Groups)
	}
	if len(b.Rules) != 2 || b.Rules[0].Name != "baseline" || b.Rules[1].Name != "payments" {
		t.Errorf("rules = %+v, want baseline and the repo-scoped rule only", b.Rules)
	}
}

func TestBundleBeforeAnyPolicyIsEmptyNotAnError(t *testing.T) {
	srv := newServer(t)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: no policy yet is the ordinary case, not a failure", resp.StatusCode)
	}
	b := decodeBundle(t, resp)
	if b.User != "alice@acme.com" || b.Rules == nil || len(b.Rules) != 0 {
		t.Errorf("bundle = %+v, want the user and an empty rules list", b)
	}
}

func TestBundleFetchTouchesTheMachine(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	fetchBundle(t, srv, alice, nil)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	var machines []struct {
		LastBundleVersion string `json:"lastBundleVersion"`
		LastSeenAt        string `json:"lastSeenAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		t.Fatal(err)
	}
	if len(machines) != 2 || machines[1].LastBundleVersion != "g1" || machines[1].LastSeenAt == "" {
		t.Errorf("machines = %+v, want the second one touched with g1", machines)
	}
	if machines[0].LastBundleVersion != "" {
		t.Error("the machine that never fetched was touched")
	}
}

func TestARevokedMachineIs401(t *testing.T) {
	srv := newServer(t)
	id, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/machines/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}

	if resp := fetchBundle(t, srv, alice, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 after revocation", resp.StatusCode)
	}
}

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

Check `handler_test.go` for an existing helper named `equal`; if one exists, delete the copy above.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/handler/ -run Bundle 2>&1 | tail -8`
Expected: 404s where 200/401 are wanted.

- [ ] **Step 3: Implement**

Append to `internal/handler/auth.go`:

```go
// requireMachine admits a request carrying an enrolled machine's credential
// and hands the machine to the handler. Lookup is by the credential's hash,
// so the store never sees the secret.
func (h *Handler) requireMachine(next func(http.ResponseWriter, *http.Request, model.Machine)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented := bearer(r)
		if presented == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		machine, err := h.store.MachineByCredential(r.Context(), credential.Hash(presented))
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			h.fail(w, r, err)
			return
		}
		next(w, r, machine)
	}
}
```

Add `"errors"`, `"github.com/acme/agent-wrapper/internal/credential"` and `"github.com/acme/agent-wrapper/internal/model"` to that file's imports.

`internal/handler/bundle.go`:

```go
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

// getBundle serves the machine's user their slice of the current policy:
// group targeting resolved here, repository targeting left for the client.
//
// An organization with no policy yet gets an empty bundle and a 200, for
// the same reason /v1/policy does: a client that falls back on error must
// not be pushed there by the ordinary case.
func (h *Handler) getBundle(w http.ResponseWriter, r *http.Request, machine model.Machine) {
	var ruleSet *policy.RuleSet
	revision, err := h.store.CurrentRuleSet(r.Context())
	switch {
	case err == nil:
		ruleSet = &revision.RuleSet
	case errors.Is(err, model.ErrNotFound):
		ruleSet = &policy.RuleSet{}
	default:
		h.fail(w, r, err)
		return
	}

	bundle := ruleSet.Slice(ruleSet.GroupsFor(machine.User))
	bundle.User = machine.User
	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		h.fail(w, r, fmt.Errorf("encoding the bundle: %w", err))
		return
	}

	// Touching is bookkeeping; a failure is logged, not surfaced, because
	// the machine still needs its policy.
	if err := h.store.TouchMachine(r.Context(), machine.ID, h.Now(), bundle.Version); err != nil {
		h.log.WarnContext(r.Context(), "recording a bundle fetch", "machine", machine.ID, "err", err)
	}

	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.log.ErrorContext(r.Context(), "writing the bundle response", "err", err)
	}
}
```

In `Routes`, add:

```go
	mux.HandleFunc("GET /v1/bundle", h.requireMachine(h.getBundle))
```

- [ ] **Step 4: Run, vet, commit**

```bash
go test ./internal/handler/ && gofmt -l . && go vet ./... && git add internal/handler && git commit -m "feat(awd): serve an enrolled machine its group-resolved bundle

GET /v1/bundle authenticates by machine credential, slices the current policy
for the machine's user, keeps repository matchers for the client, and
records the fetch on the machine. No policy yet is an empty bundle, not an
error.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 8: `awd enroll-token`, `machines`, `revoke`

**Files:**
- Modify: `cmd/awd/main.go`
- Modify: `cmd/awd/e2e_test.go`

**Interfaces:**
- Consumes: routes from Tasks 6-7, `AWD_ADMIN_TOKEN`, `AWD_URL`.
- Produces CLI:
  - `awd enroll-token <user> [--url URL] [--ttl 24h]` → prints the token on stdout alone (so it can be piped), expiry on stderr.
  - `awd machines [--url URL]` → one line per machine: `id  user  name  os  enrolled  last-seen  version`.
  - `awd revoke <id> [--url URL]` → prints `revoked <id>`.

- [ ] **Step 1: Write the failing e2e tests**

Append to `cmd/awd/e2e_test.go`:

```go
func TestEnrollTokenThenEnrollThenBundle(t *testing.T) {
	s := startServer(t)
	if out, code := runAwd(t, "apply", writePolicy(t, groupedPolicyYAML), "--url", s.url); code != 0 {
		t.Fatalf("apply: %s", out)
	}

	out, code := runAwd(t, "enroll-token", "alice@acme.com", "--url", s.url)
	if code != 0 {
		t.Fatalf("enroll-token exited %d: %s", code, out)
	}
	token := strings.TrimSpace(out)
	if token == "" || strings.ContainsAny(token, " \n") {
		t.Fatalf("stdout should be the token alone, got %q", out)
	}

	body := `{"token":"` + token + `","name":"e2e-host","os":"linux"}`
	resp, err := http.Post(s.url+"/v1/machines/enroll", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enroll status = %d", resp.StatusCode)
	}
	var enrolled struct {
		MachineID  string `json:"machineId"`
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
		User  string `json:"user"`
		Rules []struct{ Name string }
	}
	if err := json.NewDecoder(bundleResp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.User != "alice@acme.com" || len(bundle.Rules) != 2 {
		t.Errorf("bundle = %+v, want alice's baseline and platform rules", bundle)
	}

	list, code := runAwd(t, "machines", "--url", s.url)
	if code != 0 || !strings.Contains(list, enrolled.MachineID) || !strings.Contains(list, "e2e-host") {
		t.Errorf("machines output %q should list the enrolled machine", list)
	}

	if out, code := runAwd(t, "revoke", enrolled.MachineID, "--url", s.url); code != 0 || !strings.Contains(out, "revoked") {
		t.Errorf("revoke: %d %q", code, out)
	}
	after, _ := http.DefaultClient.Do(req)
	after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("bundle after revoke: %d, want 401", after.StatusCode)
	}
}

func TestEnrollTokenNeedsAUser(t *testing.T) {
	s := startServer(t)

	out, code := runAwd(t, "enroll-token", "--url", s.url)

	if code == 0 || !strings.Contains(out, "user") {
		t.Errorf("exit %d, output %q", code, out)
	}
}

const groupedPolicyYAML = `
version: "e2e-groups"
groups:
  alice@acme.com: [platform]
rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny: [Read(./.env)]
  - name: platform
    match:
      groups: [platform]
    agents:
      claude:
        managed:
          model: opus
  - name: mobile
    match:
      groups: [mobile]
    agents:
      claude:
        managed:
          model: haiku
`
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/awd/ -run 'EnrollToken' 2>&1 | tail -5`
Expected: `unknown command "enroll-token"`.

- [ ] **Step 3: Implement the commands**

In `cmd/awd/main.go`:

Extend `usage`:

```
  awd enroll-token <user> [--url URL] [--ttl 24h]   mint a single-use token that enrolls one machine
  awd machines [--url URL]                          list enrolled machines
  awd revoke <id> [--url URL]                       revoke a machine's credential
```

Extend the `switch` in `run`:

```go
	case "enroll-token":
		return enrollToken(argv[1:])
	case "machines":
		return machines(argv[1:])
	case "revoke":
		return revoke(argv[1:])
```

Add a small shared argument parser and client, and the three commands:

```go
// adminArgs is what every administrative command accepts: positional
// arguments, --url, and for enroll-token, --ttl.
type adminArgs struct {
	positional []string
	url        string
	ttl        string
}

func parseAdminArgs(argv []string) (adminArgs, error) {
	var a adminArgs
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch {
		case !strings.HasPrefix(arg, "--"):
			a.positional = append(a.positional, arg)
			continue
		case name != "url" && name != "ttl":
			return a, fmt.Errorf("unknown flag %q", arg)
		}
		if !hasValue {
			if i+1 >= len(argv) {
				return a, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = argv[i]
		}
		if name == "url" {
			a.url = value
		} else {
			a.ttl = value
		}
	}
	if a.url == "" {
		a.url = envOr("AWD_URL", "http://localhost:8080")
	}
	return a, nil
}

// adminRequest sends one authenticated request and returns the decoded
// JSON body, or an error naming the status and the server's message.
func adminRequest(method, url, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(url, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("AWD_ADMIN_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the control plane at %s: %w", url, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("control plane refused: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decoding the control plane's response: %w", err)
		}
	}
	return nil
}

func enrollToken(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("enroll-token needs exactly one user; run `awd help`")
	}
	body := map[string]string{"user": a.positional[0]}
	if a.ttl != "" {
		body["ttl"] = a.ttl
	}
	var minted struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := adminRequest(http.MethodPost, a.url, "/v1/enrollment-tokens", body, &minted); err != nil {
		return err
	}
	// The token alone on stdout, so `aw-sync enroll --token $(awd enroll-token ...)` works.
	fmt.Println(minted.Token)
	fmt.Fprintf(os.Stderr, "token for %s expires %s\n", a.positional[0], minted.ExpiresAt.Format(time.RFC3339))
	return nil
}

func machines(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 0 {
		return errors.New("machines takes no arguments")
	}
	var list []model.Machine
	if err := adminRequest(http.MethodGet, a.url, "/v1/machines", nil, &list); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSER\tNAME\tOS\tENROLLED\tLAST SEEN\tVERSION")
	for _, m := range list {
		lastSeen := "never"
		if !m.LastSeenAt.IsZero() {
			lastSeen = m.LastSeenAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.User, m.Name, m.OS, m.EnrolledAt.Format(time.RFC3339), lastSeen, m.LastBundleVersion)
	}
	return w.Flush()
}

func revoke(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("revoke needs exactly one machine id; run `awd help`")
	}
	if err := adminRequest(http.MethodDelete, a.url, "/v1/machines/"+a.positional[0], nil, nil); err != nil {
		return err
	}
	fmt.Printf("revoked %s\n", a.positional[0])
	return nil
}
```

Add `"text/tabwriter"` and `"github.com/acme/agent-wrapper/internal/model"` to the imports (`bytes`, `io`, `json`, `time` are already imported by `apply`).

- [ ] **Step 4: Run the whole suite, vet, commit**

```bash
gofmt -l . && go vet ./... && go test ./... 2>&1 | grep -v '^ok' ; git add cmd/awd && git commit -m "feat(awd): enroll-token, machines and revoke commands

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 9: Example policy, README, plan progress

**Files:**
- Modify: `examples/org-policy.yaml`
- Modify: `README.md`
- Modify: `/home/leo/.claude/plans/enterprise-grade-coding-agent-wrapper-cozy-liskov.md` (Progress table)

- [ ] **Step 1: Add `groups` to the example policy**

In `examples/org-policy.yaml`, after the `version:` line, add:

```yaml
# Who belongs to which group. Authored here so a membership change is a
# policy revision like any other; an identity-provider sync can replace this
# block later without changing what a machine receives.
groups:
  alice@acme.com: [platform]
  bob@acme.com: [platform, security]
```

Run: `go test ./internal/policy/ ./cmd/awd/` — `TestTheShippedExamplePolicyIsValid` and the e2e apply must still pass.

- [ ] **Step 2: README — enrollment flow**

In `README.md`, in "Try it", after the `awd apply` line, add:

```markdown
Apply needs an administrator token; set `AWD_ADMIN_TOKEN` for both the
server and the CLI. Then enroll a machine and fetch its bundle:

    export AWD_ADMIN_TOKEN=change-me
    TOKEN=$(./awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
    curl -s -X POST http://127.0.0.1:8080/v1/machines/enroll \
      -d "{\"token\":\"$TOKEN\",\"name\":\"$(hostname)\",\"os\":\"linux\"}"
    # {"machineId":"...","credential":"...","user":"alice@acme.com"}
    curl -s -H 'Authorization: Bearer <credential>' http://127.0.0.1:8080/v1/bundle

The bundle is every rule that could apply to that user, with repository
matchers still in it; the machine resolves those per session. `aw-sync`,
which does the enrolling and the rendering on a real machine, is the next
milestone.
```

In the "Layout" block, add `    internal/credential/  minting and hashing machine credentials` in alphabetical position.

- [ ] **Step 3: Update the plan's Progress table**

In the plan document's Progress table, add a row `| 4a. Bundles, machines, enrollment, admin auth | **done** |` and change "Last worked" to the current date and commit. Under "Resume here", replace the first sentence with: "M4a is done. Next: write the M4b plan (Claude renderer, aw-sync enroll/once/status, timer units) from the spec, then execute it."

- [ ] **Step 4: Full verification and commit**

```bash
gofmt -l . && go vet ./... && GOOS=windows go build ./... && GOOS=darwin go build ./... && go test ./... 2>&1 | grep -v '^ok'
git add -A && git commit -m "docs: groups in the example policy; enrollment flow in the README

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 10: Docker Compose for evaluation and end-to-end runs

**Files:**
- Create: `deploy/docker-compose.yml`
- Create: `deploy/Dockerfile`
- Modify: `README.md` ("Try it")

**Interfaces:**
- Produces: `docker compose -f deploy/docker-compose.yml up` brings up Postgres 16 and `awd serve` on `:8080` with `AWD_ADMIN_TOKEN` from the environment (default `change-me`). Pulled forward from the old M7 so the M4b end-to-end (enroll → sync → aw-policy) has a real stack to run against.

- [ ] **Step 1: Write the Dockerfile**

`deploy/Dockerfile`:

```dockerfile
# Build every binary once; the compose file runs awd, and the same image can
# run aw-sync in a sidecar for end-to-end tests.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/awd ./cmd/awd  && CGO_ENABLED=0 go build -o /out/aw-policy ./cmd/aw-policy

FROM alpine:3.20
RUN adduser -D -u 10001 awd
COPY --from=build /out/ /usr/local/bin/
USER awd
EXPOSE 8080
ENTRYPOINT ["awd"]
CMD ["serve"]
```

- [ ] **Step 2: Write the compose file**

`deploy/docker-compose.yml`:

```yaml
# Evaluation stack: the control plane on Postgres. Not a production
# deployment: no TLS, a default admin token, and a password in the file.
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: pw
      POSTGRES_DB: awd
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 2s
      timeout: 2s
      retries: 15
    volumes:
      - awd-pg:/var/lib/postgresql/data

  awd:
    build:
      context: ..
      dockerfile: deploy/Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      AWD_ADDR: ":8080"
      AWD_DATABASE_URL: postgres://postgres:pw@postgres:5432/awd?sslmode=disable
      AWD_ADMIN_TOKEN: ${AWD_ADMIN_TOKEN:-change-me}
      AWD_LOG_LEVEL: ${AWD_LOG_LEVEL:-info}
    ports:
      - "8080:8080"

volumes:
  awd-pg:
```

- [ ] **Step 3: Bring it up and prove the flow**

```bash
cd /home/leo/AI_STUFF/agent-wrapper && docker compose -f deploy/docker-compose.yml up -d --build
for i in $(seq 1 30); do curl -sf http://127.0.0.1:8080/readyz && break; sleep 1; done
export AWD_ADMIN_TOKEN=change-me
go run ./cmd/awd apply examples/org-policy.yaml --url http://127.0.0.1:8080
TOKEN=$(go run ./cmd/awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
curl -s -X POST http://127.0.0.1:8080/v1/machines/enroll -d "{\"token\":\"$TOKEN\",\"name\":\"compose-test\",\"os\":\"linux\"}"
docker compose -f deploy/docker-compose.yml down
```
Expected: `readyz` answers `{"status":"ready"}`; apply prints `applied revision ...`; enroll returns a `machineId` and `credential`. Port 5432 is not published by the compose file, so it does not collide with the `awd-pg` test container.

- [ ] **Step 4: README**

In `README.md` "Try it", before "Run the control plane and apply a policy:", add:

```markdown
With Docker, the whole control plane comes up on Postgres:

    docker compose -f deploy/docker-compose.yml up -d --build

It listens on :8080 with `AWD_ADMIN_TOKEN=change-me` unless you export
another. Without Docker, run it from source:
```

- [ ] **Step 5: Commit**

```bash
git add deploy README.md && git commit -m "build: docker compose for the control plane on Postgres

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

## Self-review

**Spec coverage (Control plane section):** data model → Task 2; groups → Task 1; compile split → Task 1; store → Tasks 3-4; API rows: enrollment-tokens, enroll, bundle, machines, delete, revisions auth, policy preview → Tasks 5-7; admin auth 503/401 → Task 5; machine auth constant-time → Task 7 (hash lookup; the hash comparison happens in the store by exact match, which is not timing-sensitive because the hash is not the secret); `awd` CLI → Task 8; apply-time validators for Codex/Gemini → out of M4a (they land with their renderers in M4d/M4e). Failure-modes rows for awd → Tasks 5, 7.

**Type consistency:** `policy.Bundle` fields used in handler tests match Task 1; `model.Machine` JSON tags (`machineId` is a response field, `id` is the model tag) — Task 6 responses use `machineId`, listing uses the model's `id`; the e2e test reads `machineId` from enroll and looks for the id string in `machines` output, which prints `m.ID`. `store` signatures in Tasks 3, 4, 6, 7 agree. `postAs`/`post`/`get`/`enroll`/`mintToken`/`adminToken` helpers defined once each in `handler_test.go`, `enroll_test.go`.

**Placeholders:** none.
