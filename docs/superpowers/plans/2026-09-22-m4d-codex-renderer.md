# M4d: Codex renderer and requirements validator — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aw-sync` can render Codex's `requirements.toml` from the bundle: matching rules merge with Codex's own semantics, repo-scoped rules are dropped with a note, the document is validated against a dated key allowlist plus a TOML round trip, and `awd apply` rejects a Codex rule with an unknown key the same way it rejects an invalid Claude rule.

**Architecture:** A new `internal/agent/codex` adapter (minimal `Adapter` — launch-time governance is a later milestone — plus `Renderer`) and a sibling `internal/agent/codex/requirements` package holding the vendored key allowlist and `Validate`/`ForAgent`, mirroring `internal/agent/claude/schema`. `policy.AgentMergeRules` gains the Codex rule (`rules.prefix_rules` unions; everything else, `mcp_servers` tables included, follows the existing deep-merge, which is "merge by name" for tables and "replace" for scalars and lists). `cmd/awd` composes the two validators; `cmd/aw-sync` registers the adapter; the e2e renders both agents.

**Tech Stack:** Go 1.22; one new dependency, `github.com/pelletier/go-toml/v2 v2.2.3` (spiked: builds under Go 1.22 via `go get`, no tidy needed; marshals JSON-shaped `map[string]any`/`[]any`/`float64` deterministically with sorted keys, scalars before tables; an empty map marshals to an empty document).

**Spec:** `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` — sections "Renderers" (intro and "Codex"), "Failure modes" (aw-sync rows), "Testing" (renderers: golden files, merge rules, the repo-scoped drop note, TOML round trip), "Sequencing (M4d)".

## Spec refinements

1. **Key allowlist source and date.** The vendored allowlist is the requirements.toml key list from OpenAI's "Managed configuration" page as read on 2026-09-22 (`learn.chatgpt.com/docs/enterprise/managed-configuration`). It is a list of top-level keys only; nested shapes are not validated beyond the TOML round trip, exactly as the spec says. The list and its date live in one Go file so the next refresh is a one-file diff.
2. **Merge rules.** The spec's "scalars and `allowed_*` lists replace; `mcp_servers` merges by name; `rules.prefix_rules` appends" is implemented by `policy.AgentMergeRules("codex") = merge.Rules{UnionArrays: []string{"rules.prefix_rules"}}`: the existing deep merge already replaces scalars and non-union lists and merges tables key by key. "Appends" is the existing union (concatenate, de-duplicate structurally); a rule repeated verbatim in two policy rules appears once, which is the right outcome for a prefix rule.
3. **Header comment.** The first line of `requirements.toml` is `# Managed by aw-sync from policy revision <version>. Do not edit: the next sync overwrites this file.` followed by a blank line. `<version>` is `unversioned` when the bundle has none. The sync loop already skips `.aw-revision` siblings for non-`.json` files.
4. **Drop note.** Rules that carry a `codex` entry and a `repos` matcher are dropped by `Compile("")`; the renderer notes `codex: N repo-scoped rule(s) not enforceable in requirements.toml: <names>` (names in authored order). No note when N is 0. Rules for other agents do not count.
5. **Empty document.** A bundle with no Codex rules renders the header and nothing else. That is a valid, deliberately empty requirements file: the machine then carries no aw-managed requirements, which is what the policy said.
6. **Minimal adapter.** `codex.Adapter.Build` locates the binary and passes arguments and the merged environment through, with a note that Codex's managed settings are enforced by `requirements.toml` rather than per launch. It is registered in `cmd/aw-sync` only; `cmd/aw` keeps Claude alone until the launch-time Codex milestone. `Locate` resolves `codex` on PATH (`Binary` overrides, as in the Claude adapter).
7. **Validator composition.** `cmd/awd` gets `managedValidator(agentName, managed) error` that runs `schema.ForAgent` then `requirements.ForAgent`; both `serve` and `apply` use it.
8. **`go.mod` hygiene.** `go get github.com/pelletier/go-toml/v2@v2.2.3` records the requirement with `// indirect` (every existing direct dependency in this `go.mod` is marked the same way, since bare `go mod tidy` is off-limits here). Leave the marker; do not run `go mod tidy`.

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run bare `go mod tidy`. The only dependency change is `go get github.com/pelletier/go-toml/v2@v2.2.3` in Task 1.
- No `gcc`: `go test -race` cannot run. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit; `GOOS=darwin go build ./... && GOOS=windows go build ./...` for tasks touching `cmd/` or an adapter.
- **Rendered files are never deleted on failure**; one failed renderer means nothing is written this cycle, for any agent (already enforced by `internal/sync`; do not weaken it).
- Renderer file paths are relative to a root the caller supplies; renderers never touch the filesystem.
- Rendered Codex file is mode `0644`; path `requirements.toml` relative to the Codex root (`/etc/codex` on Linux and macOS, `%ProgramData%\OpenAI\Codex` on Windows — already in `internal/sync/paths.go`).
- Return `make([]T, 0)`, never a nil slice, from anything JSON-encoded as a list.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv`.
- Comment style: a comment says *why*, in full sentences, on every exported identifier and every struct field.
- Model tiering for executors: Tasks 1 and 2 are transcription (cheapest tier); Tasks 3 and 4 are multi-file integration (mid tier); the final whole-branch review is the most capable tier.

---

## File map

| File | Responsibility |
| --- | --- |
| `go.mod`, `go.sum` | add `github.com/pelletier/go-toml/v2 v2.2.3` |
| `internal/agent/codex/requirements/keys.go` | dated allowlist of top-level requirements.toml keys |
| `internal/agent/codex/requirements/requirements.go` | `Validate`, `ForAgent` |
| `internal/agent/codex/requirements/requirements_test.go` | allowlist, round trip, ForAgent tests |
| `internal/policy/compile.go` | `AgentMergeRules("codex")` |
| `internal/policy/compile_test.go` | Codex merge semantics test |
| `internal/agent/codex/codex.go` | `Adapter`: `Name`, `Locate`, `Build` |
| `internal/agent/codex/render.go` | `Render`, header, drop note |
| `internal/agent/codex/codex_test.go`, `render_test.go`, `testdata/requirements.toml` | adapter and golden tests |
| `cmd/awd/main.go` | composite managed validator |
| `cmd/aw-sync/main.go` | register the Codex adapter |
| `cmd/aw-sync/e2e_test.go` | render both agents; note in status |
| `examples/org-policy.yaml` | a Codex entry in the baseline rule |
| `README.md`, `deploy/aw-sync/README.md` | Codex mentioned where agents are listed |

---

### Task 1: `requirements` package — key allowlist and validator

**Files:**
- Modify: `go.mod`, `go.sum` (via `go get`)
- Create: `internal/agent/codex/requirements/keys.go`
- Create: `internal/agent/codex/requirements/requirements.go`
- Create: `internal/agent/codex/requirements/requirements_test.go`

**Interfaces:**
- Produces: `requirements.Validate(managed map[string]any) error`, `requirements.ForAgent(agentName string, managed map[string]any) error`, `requirements.Keys []string`, `requirements.KeysAsOf = "2026-09-22"`. Tasks 3 and 4 consume `Validate`/`ForAgent`.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/pelletier/go-toml/v2@v2.2.3`
Expected: `go: added github.com/pelletier/go-toml/v2 v2.2.3`; `go.mod` gains one `require` line (marked `// indirect`, see refinement 8) and `go.sum` gains its hashes. Do not run `go mod tidy`.

- [ ] **Step 2: Write the failing tests**

Create `internal/agent/codex/requirements/requirements_test.go`:

```go
package requirements_test

import (
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/codex/requirements"
)

func TestKnownKeysValidate(t *testing.T) {
	managed := map[string]any{
		"allowed_approval_policies": []any{"on-request", "untrusted"},
		"allowed_sandbox_modes":     []any{"read-only", "workspace-write"},
		"default_permissions":       ":workspace",
		"allow_appshots":            false,
		"mcp_servers":               map[string]any{"docs": map[string]any{"identity": map[string]any{"command": "codex-mcp"}}},
		"rules": map[string]any{"prefix_rules": []any{
			map[string]any{"pattern": []any{map[string]any{"token": "rm"}}, "decision": "forbidden"},
		}},
	}

	if err := requirements.Validate(managed); err != nil {
		t.Fatalf("Validate = %v, want nil for documented keys", err)
	}
}

func TestAnUnknownTopLevelKeyIsRejected(t *testing.T) {
	err := requirements.Validate(map[string]any{"allowed_sandbox_modes": []any{"read-only"}, "sandbox_mode": "read-only"})

	if err == nil || !strings.Contains(err.Error(), `"sandbox_mode"`) || !strings.Contains(err.Error(), requirements.KeysAsOf) {
		t.Errorf("err = %v; want the unknown key named and the allowlist date, so a reader knows which reference to check", err)
	}
}

func TestUnknownKeysAreReportedSorted(t *testing.T) {
	err := requirements.Validate(map[string]any{"zeta": 1, "alpha": 2})

	if err == nil || strings.Index(err.Error(), `"alpha"`) > strings.Index(err.Error(), `"zeta"`) {
		t.Errorf("err = %v; want unknown keys in sorted order so the message is stable", err)
	}
}

func TestAValueTOMLCannotEncodeIsRejected(t *testing.T) {
	// TOML has no null: a JSON null in the policy would be dropped or
	// mis-encoded, and the author should hear about it at apply time.
	err := requirements.Validate(map[string]any{"default_permissions": nil})

	if err == nil {
		t.Error("Validate = nil; want an error for a value that does not survive a TOML round trip")
	}
}

func TestEmptyAndNilDocumentsAreValid(t *testing.T) {
	if err := requirements.Validate(map[string]any{}); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := requirements.Validate(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
}

func TestForAgentIgnoresOtherAgents(t *testing.T) {
	bad := map[string]any{"not_a_key": true}

	if err := requirements.ForAgent("claude", bad); err != nil {
		t.Errorf("claude: %v; the Codex validator must not judge another agent's document", err)
	}
	if err := requirements.ForAgent("codex", bad); err == nil {
		t.Error("codex: nil; want the unknown key rejected")
	}
	if err := requirements.ForAgent("codex", nil); err != nil {
		t.Errorf("codex nil: %v", err)
	}
}

func TestTheAllowlistIsSortedAndDated(t *testing.T) {
	for i := 1; i < len(requirements.Keys); i++ {
		if requirements.Keys[i-1] >= requirements.Keys[i] {
			t.Errorf("Keys not sorted/unique at %d: %q >= %q", i, requirements.Keys[i-1], requirements.Keys[i])
		}
	}
	if len(requirements.KeysAsOf) != len("2026-09-22") {
		t.Errorf("KeysAsOf = %q, want an ISO date", requirements.KeysAsOf)
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `go test ./internal/agent/codex/requirements/`
Expected: build failure — the package does not exist.

- [ ] **Step 4: Write the allowlist**

Create `internal/agent/codex/requirements/keys.go`:

```go
package requirements

// KeysAsOf is the date the allowlist below was read from OpenAI's managed
// configuration reference for Codex (learn.chatgpt.com, "Managed
// configuration", requirements.toml). A key that appears after that date is
// rejected until this file is refreshed; the error names the date so the
// reader knows which reference to compare against.
const KeysAsOf = "2026-09-22"

// Keys is every top-level key requirements.toml documents, sorted. Only the
// top level is checked: nested shapes vary by key and change more often, and
// a TOML round trip catches what cannot be encoded at all. A key missing
// here makes Codex ignore the whole file at best, so it is an apply-time
// error, on the same path as a Claude settings schema violation.
var Keys = []string{
	"allow_appshots",
	"allow_locked_computer_use",
	"allow_managed_hooks_only",
	"allow_remote_control",
	"allowed_approval_policies",
	"allowed_approvals_reviewers",
	"allowed_chatgpt_workspaces",
	"allowed_login_methods",
	"allowed_permission_profiles",
	"allowed_sandbox_modes",
	"allowed_web_search_modes",
	"browser_use",
	"chatgpt_base_url",
	"cli_auth_credentials_store",
	"computer_use",
	"default_permissions",
	"experimental_network",
	"features",
	"guardian_policy_config",
	"hooks",
	"marketplaces",
	"mcp_servers",
	"permissions",
	"remote_sandbox_config",
	"rules",
}
```

- [ ] **Step 5: Write the validator**

Create `internal/agent/codex/requirements/requirements.go`:

```go
// Package requirements validates the document aw-sync writes to Codex's
// requirements.toml. Codex has no published schema for the file, so the
// check is the one the spec names: a vendored, dated allowlist of top-level
// keys, and a TOML round trip so nothing the policy carries is silently
// lost or mangled on the way to disk.
package requirements

import (
	"fmt"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Validate reports whether managed can be written as requirements.toml: every
// top-level key is documented, and the document survives a TOML encode and
// decode. A nil or empty document is valid; it renders to an empty file,
// which is a policy that requires nothing.
func Validate(managed map[string]any) error {
	var unknown []string
	for key := range managed {
		if !known(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		quoted := make([]string, len(unknown))
		for i, k := range unknown {
			quoted[i] = fmt.Sprintf("%q", k)
		}
		return fmt.Errorf("requirements: unknown top-level key(s) %s; the allowlist is from the reference as of %s",
			strings.Join(quoted, ", "), KeysAsOf)
	}
	if len(managed) == 0 {
		return nil
	}
	encoded, err := toml.Marshal(managed)
	if err != nil {
		return fmt.Errorf("requirements: the document cannot be written as TOML: %w", err)
	}
	var back map[string]any
	if err := toml.Unmarshal(encoded, &back); err != nil {
		return fmt.Errorf("requirements: the document does not read back as TOML: %w", err)
	}
	if len(back) != len(managed) {
		// A value TOML cannot represent (a null, say) is dropped by the
		// encoder rather than refused; the author should hear about it.
		return fmt.Errorf("requirements: %d key(s) did not survive a TOML round trip; a value is probably null", len(managed)-len(back))
	}
	return nil
}

// ForAgent is a policy.ManagedValidator: it validates the managed document of
// rules aimed at Codex and ignores every other agent, whose format it does
// not know.
func ForAgent(agentName string, managed map[string]any) error {
	if agentName != "codex" || managed == nil {
		return nil
	}
	return Validate(managed)
}

func known(key string) bool {
	i := sort.SearchStrings(Keys, key)
	return i < len(Keys) && Keys[i] == key
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/agent/codex/requirements/ && go vet ./internal/agent/codex/... && gofmt -l internal/agent/codex`
Expected: PASS, no output from vet/gofmt. If `TestAValueTOMLCannotEncodeIsRejected` fails because go-toml returns an error for nil rather than dropping it, that is still a rejection — the test only requires `err != nil`.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/agent/codex/requirements
git commit -m "feat(codex): requirements.toml validator with a dated key allowlist and TOML round trip

Adds github.com/pelletier/go-toml/v2 v2.2.3.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 2: Codex merge semantics in `policy`

**Files:**
- Modify: `internal/policy/compile.go` (`AgentMergeRules`)
- Modify: `internal/policy/compile_test.go` (append one test)

**Interfaces:**
- Produces: `AgentMergeRules("codex")` returns `merge.Rules{UnionArrays: []string{"rules.prefix_rules"}}`. Task 3's renderer relies on `Bundle.Compile` using it.

- [ ] **Step 1: Append the failing test**

Append to `internal/policy/compile_test.go`:

```go
func TestCodexRulesMergeWithCodexSemantics(t *testing.T) {
	// Codex's own layering replaces scalars and lists and merges tables by
	// key; a union on an allowlist would widen it. Only prefix_rules
	// accumulate, since each is a separate restriction.
	rs := &policy.RuleSet{Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"codex": {Managed: map[string]any{
			"allowed_sandbox_modes": []any{"read-only", "workspace-write"},
			"default_permissions":   ":workspace",
			"mcp_servers":           map[string]any{"docs": map[string]any{"identity": map[string]any{"command": "codex-mcp"}}},
			"rules":                 map[string]any{"prefix_rules": []any{map[string]any{"pattern": []any{"rm"}, "decision": "forbidden"}}},
		}}}},
		{Name: "strict", Agents: map[string]policy.AgentConfig{"codex": {Managed: map[string]any{
			"allowed_sandbox_modes": []any{"read-only"},
			"default_permissions":   ":read-only",
			"mcp_servers":           map[string]any{"jira": map[string]any{"identity": map[string]any{"url": "https://jira/mcp"}}},
			"rules":                 map[string]any{"prefix_rules": []any{map[string]any{"pattern": []any{"git", "push"}, "decision": "prompt"}}},
		}}}},
	}}

	managed := rs.Compile(policy.Subject{}).Agent("codex").Managed

	if modes, _ := managed["allowed_sandbox_modes"].([]any); len(modes) != 1 || modes[0] != "read-only" {
		t.Errorf("allowed_sandbox_modes = %v; a later rule's allowlist must replace, not widen", modes)
	}
	if managed["default_permissions"] != ":read-only" {
		t.Errorf("default_permissions = %v; scalars replace", managed["default_permissions"])
	}
	servers, _ := managed["mcp_servers"].(map[string]any)
	if _, docs := servers["docs"]; !docs {
		t.Error("mcp_servers lost docs; tables merge by name")
	}
	if _, jira := servers["jira"]; !jira {
		t.Error("mcp_servers lost jira; tables merge by name")
	}
	rules, _ := managed["rules"].(map[string]any)
	if prefix, _ := rules["prefix_rules"].([]any); len(prefix) != 2 {
		t.Errorf("prefix_rules = %v; want both rules appended in order", prefix)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/policy/ -run TestCodexRulesMergeWithCodexSemantics`
Expected: FAIL — `prefix_rules` has 1 entry (the later rule replaced the list).

- [ ] **Step 3: Add the Codex rule**

In `internal/policy/compile.go`, replace `AgentMergeRules` with:

```go
// AgentMergeRules says how one agent's managed settings combine across the
// rules that apply to a subject. Claude's permission lists union so an
// organization allowlist and a team allowlist coexist instead of one
// silently erasing the other. Codex's requirements follow Codex's own
// layering: scalars and allowlists replace (a union would widen an
// allowlist), tables such as mcp_servers merge by key, and only
// rules.prefix_rules accumulate, since each entry is its own restriction.
func AgentMergeRules(agentName string) merge.Rules {
	switch agentName {
	case "claude":
		return merge.Rules{UnionArrays: []string{
			"permissions.allow",
			"permissions.deny",
			"permissions.ask",
			"permissions.additionalDirectories",
		}}
	case "codex":
		return merge.Rules{UnionArrays: []string{"rules.prefix_rules"}}
	default:
		return merge.Rules{}
	}
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/policy/ && go vet ./internal/policy/ && gofmt -l internal/policy`
Expected: PASS, no output.

- [ ] **Step 5: Commit**

```bash
git add internal/policy/compile.go internal/policy/compile_test.go
git commit -m "feat(policy): Codex merge semantics — prefix_rules accumulate, everything else replaces or merges by key

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 3: the Codex adapter and renderer

**Files:**
- Create: `internal/agent/codex/codex.go`
- Create: `internal/agent/codex/render.go`
- Create: `internal/agent/codex/codex_test.go`
- Create: `internal/agent/codex/render_test.go`
- Create: `internal/agent/codex/testdata/requirements.toml`

**Interfaces:**
- Consumes: `requirements.Validate` (Task 1); `policy.Bundle.Compile` with Codex merge rules (Task 2); `agent.Adapter`, `agent.Renderer`, `agent.Rendering`, `agent.File`, `agent.ResolveBinary`, `merge.Env`.
- Produces: `codex.New() *Adapter`, `codex.Name = "codex"`, `codex.RequirementsFile = "requirements.toml"`. Task 4 registers `codex.New()`.

- [ ] **Step 1: Write the failing adapter tests**

Create `internal/agent/codex/codex_test.go`:

```go
package codex_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/codex"
)

// fakeBinary puts an executable named codex on a private PATH.
func fakeBinary(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable bit")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir, "HOME=" + t.TempDir()}
}

func TestNameIsCodex(t *testing.T) {
	if got := codex.New().Name(); got != "codex" {
		t.Errorf("Name = %q", got)
	}
}

func TestBuildPassesArgumentsThroughAndMergesTheEnvironment(t *testing.T) {
	env := fakeBinary(t)

	launch, err := codex.New().Build(context.Background(), agent.BuildOptions{
		Env:      env,
		Args:     []string{"--model", "gpt-5"},
		Settings: agent.Settings{Env: map[string]string{"OPENAI_BASE_URL": "https://proxy.acme"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if launch.Agent != "codex" || !strings.HasSuffix(launch.Binary, "/codex") {
		t.Errorf("launch = %+v", launch)
	}
	if strings.Join(launch.Args, " ") != "--model gpt-5" {
		t.Errorf("args = %q; want the developer's arguments untouched", launch.Args)
	}
	found := false
	for _, e := range launch.Env {
		if e == "OPENAI_BASE_URL=https://proxy.acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("env %q lacks the policy's variable", launch.Env)
	}
	if len(launch.Notes) == 0 || !strings.Contains(strings.Join(launch.Notes, "\n"), "requirements.toml") {
		t.Errorf("notes %q should say where Codex's managed settings are enforced", launch.Notes)
	}
}

func TestBuildFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, err := codex.New().Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})

	if err == nil {
		t.Error("Build = nil error; want the missing binary reported")
	}
}
```

- [ ] **Step 2: Write the failing renderer tests**

Create `internal/agent/codex/render_test.go`:

```go
package codex_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/codex"
	"github.com/acme/agent-wrapper/internal/policy"
)

func codexRule(name string, repos []string, managed map[string]any) policy.Rule {
	return policy.Rule{
		Name:   name,
		Match:  policy.Match{Repos: repos},
		Agents: map[string]policy.AgentConfig{codex.Name: {Managed: managed}},
	}
}

// bundle is a platform user's bundle: a baseline for everyone, a stricter
// rule, a repo-scoped rule Codex cannot honour, and a Claude-only rule.
func bundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "2026-09-22.1",
		Groups:  []string{"platform"},
		Rules: []policy.Rule{
			codexRule("baseline", nil, map[string]any{
				"allowed_sandbox_modes":     []any{"read-only", "workspace-write"},
				"allowed_approval_policies": []any{"on-request", "untrusted"},
				"mcp_servers":               map[string]any{"docs": map[string]any{"identity": map[string]any{"command": "codex-mcp"}}},
				"rules": map[string]any{"prefix_rules": []any{
					map[string]any{"pattern": []any{map[string]any{"token": "rm"}}, "decision": "forbidden", "justification": "Use git clean -fd instead."},
				}},
			}),
			codexRule("strict", nil, map[string]any{
				"allowed_sandbox_modes": []any{"read-only"},
				"rules": map[string]any{"prefix_rules": []any{
					map[string]any{"pattern": []any{map[string]any{"token": "git"}, map[string]any{"token": "push"}}, "decision": "prompt"},
				}},
			}),
			codexRule("payments", []string{"github.com/acme/payments*"}, map[string]any{
				"allowed_sandbox_modes": []any{"read-only"},
			}),
			{Name: "claude-only", Match: policy.Match{Repos: []string{"github.com/acme/x"}},
				Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
		},
	}
}

func TestRenderMatchesTheGoldenFile(t *testing.T) {
	rendering, err := codex.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(rendering.Files) != 1 {
		t.Fatalf("files = %d, want requirements.toml alone", len(rendering.Files))
	}
	f := rendering.Files[0]
	if f.Path != "requirements.toml" || f.Mode != 0o644 {
		t.Errorf("file = %s mode %o", f.Path, f.Mode)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "requirements.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Content) != string(want) {
		t.Errorf("rendered requirements.toml differs from testdata:\n%s\nwant:\n%s", f.Content, want)
	}
}

func TestRenderNotesTheRepoScopedRulesItDropped(t *testing.T) {
	rendering, err := codex.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}

	notes := strings.Join(rendering.Notes, "\n")
	if len(rendering.Notes) != 1 || !strings.Contains(notes, "1 repo-scoped rule") || !strings.Contains(notes, "payments") {
		t.Errorf("notes = %q; want one note naming the dropped Codex rule", rendering.Notes)
	}
	if strings.Contains(notes, "claude-only") {
		t.Error("the note counts a rule that does not configure Codex")
	}
}

func TestRenderWithNoCodexRulesWritesTheHeaderAlone(t *testing.T) {
	rendering, err := codex.New().Render(&policy.Bundle{Version: "v1", Rules: []policy.Rule{
		{Name: "claude", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if got := string(rendering.Files[0].Content); got != "# Managed by aw-sync from policy revision v1. Do not edit: the next sync overwrites this file.\n\n" {
		t.Errorf("content = %q", got)
	}
	if len(rendering.Notes) != 0 {
		t.Errorf("notes = %q; nothing was dropped", rendering.Notes)
	}
}

func TestRenderOfANilBundleIsEmptyAndUnversioned(t *testing.T) {
	rendering, err := codex.New().Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(rendering.Files[0].Content), "# Managed by aw-sync from policy revision unversioned.") {
		t.Errorf("content = %q", rendering.Files[0].Content)
	}
}

func TestRenderRejectsAnUnknownKey(t *testing.T) {
	_, err := codex.New().Render(&policy.Bundle{Rules: []policy.Rule{
		codexRule("bad", nil, map[string]any{"sandbox_mode": "read-only"}),
	}})

	if err == nil || !strings.Contains(err.Error(), `"sandbox_mode"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestRenderedDocumentReadsBackAsTOML(t *testing.T) {
	rendering, err := codex.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := codex.DecodeForTest(rendering.Files[0].Content, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if modes, _ := back["allowed_sandbox_modes"].([]any); len(modes) != 1 || modes[0] != "read-only" {
		t.Errorf("allowed_sandbox_modes = %v after the strict rule", modes)
	}
}
```

- [ ] **Step 3: Write the golden file**

Create `internal/agent/codex/testdata/requirements.toml` with exactly this content (go-toml sorts keys, scalars and arrays before tables, and the header is ours):

```toml
# Managed by aw-sync from policy revision 2026-09-22.1. Do not edit: the next sync overwrites this file.

allowed_approval_policies = ['on-request', 'untrusted']
allowed_sandbox_modes = ['read-only']

[mcp_servers]
[mcp_servers.docs]
[mcp_servers.docs.identity]
command = 'codex-mcp'

[rules]
[[rules.prefix_rules]]
decision = 'forbidden'
justification = 'Use git clean -fd instead.'

[[rules.prefix_rules.pattern]]
token = 'rm'

[[rules.prefix_rules]]
decision = 'prompt'

[[rules.prefix_rules.pattern]]
token = 'git'

[[rules.prefix_rules.pattern]]
token = 'push'
```

If the golden test fails only on whitespace or key order, the encoder's exact layout differs from this transcription: print `f.Content` once, confirm it is a faithful TOML rendering of the merged document (both prefix rules, one sandbox mode, both approval policies, the docs server), and replace the golden file with the actual output. Record that in your report.

- [ ] **Step 4: Run the tests to see them fail**

Run: `go test ./internal/agent/codex/`
Expected: build failure — package `codex` does not exist.

- [ ] **Step 5: Write the adapter**

Create `internal/agent/codex/codex.go`:

```go
// Package codex integrates OpenAI Codex. Its enforced configuration is a
// machine-wide requirements.toml that Codex composes under any cloud or MDM
// layer an organization also runs; this package renders that file for
// aw-sync. Launching Codex through aw is pass-through for now: there is no
// per-launch settings channel to govern, and requirements.toml applies to
// bare `codex` as much as to `aw codex`.
package codex

import (
	"context"
	"os"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/merge"
)

// Name is the subcommand a developer types and the agent key in a policy.
const Name = "codex"

// Adapter launches Codex and renders its requirements file. The exported
// field exists so tests can control the binary.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
}

// New returns an adapter with the defaults a developer's machine implies.
func New() *Adapter { return &Adapter{} }

// Name reports the subcommand this adapter handles.
func (a *Adapter) Name() string { return Name }

// Locate resolves the real Codex binary, never the wrapper.
func (a *Adapter) Locate(env []string) (string, error) {
	name := a.Binary
	if name == "" {
		name = Name
	}
	return agent.ResolveBinary(name, env)
}

// Build computes the launch. Codex takes no settings file on the command
// line, so the policy's managed document is not applied here: aw-sync has
// already written it to requirements.toml, which Codex reads on its own.
// Only the environment is merged, so an organization's variables still
// reach the process.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}
	launch := &agent.Launch{Agent: Name, Binary: binary}

	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch.Env = env
	launch.Notes = append(launch.Notes, notes...)
	launch.Notes = append(launch.Notes, "codex: managed settings are enforced by the system requirements.toml that aw-sync writes, not per launch")
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}
```

Check `agent.BuildOptions`, `agent.Settings` and `agent.Launch` field names against `internal/agent/agent.go` and `internal/agent/claude/claude.go` before relying on them; the Claude adapter's `Build` is the pattern.

- [ ] **Step 6: Write the renderer**

Create `internal/agent/codex/render.go`:

```go
package codex

import (
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/codex/requirements"
	"github.com/acme/agent-wrapper/internal/policy"
)

// RequirementsFile is Codex's enforced configuration, relative to its system
// directory. aw-sync owns the whole file: Codex has no drop-in directory,
// so there is nothing to share with another author.
const RequirementsFile = "requirements.toml"

// Render compiles the bundle for a session in no repository and writes the
// result as requirements.toml. A static file cannot express a repo-scoped
// rule, so those are dropped here and reported in a note; silence would
// leave an author believing a restriction applies when it does not.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	managed := bundle.Compile("").Agent(Name).Managed
	if err := requirements.Validate(managed); err != nil {
		return agent.Rendering{}, fmt.Errorf("codex: %w", err)
	}
	var body []byte
	if len(managed) > 0 {
		var err error
		body, err = toml.Marshal(managed)
		if err != nil {
			return agent.Rendering{}, fmt.Errorf("codex: encoding requirements.toml: %w", err)
		}
	}
	content := append([]byte(header(bundle)), body...)
	return agent.Rendering{
		Files: []agent.File{{Path: RequirementsFile, Content: content, Mode: 0o644}},
		Notes: dropped(bundle),
	}, nil
}

// header is the first line of the file. TOML has no sidecar convention, so
// the revision rides in a comment, where `aw-sync status` and a curious
// operator can both read it.
func header(bundle *policy.Bundle) string {
	version := "unversioned"
	if bundle != nil && bundle.Version != "" {
		version = bundle.Version
	}
	return "# Managed by aw-sync from policy revision " + version + ". Do not edit: the next sync overwrites this file.\n\n"
}

// dropped names the Codex rules Compile("") left out because they are scoped
// to repositories. Rules for other agents are not this renderer's to report.
func dropped(bundle *policy.Bundle) []string {
	if bundle == nil {
		return nil
	}
	var names []string
	for _, rule := range bundle.Rules {
		if _, forCodex := rule.Agents[Name]; forCodex && len(rule.Match.Repos) > 0 {
			names = append(names, rule.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("codex: %d repo-scoped rule(s) not enforceable in requirements.toml: %s",
		len(names), strings.Join(names, ", "))}
}

// DecodeForTest parses a rendered document back into a map. It exists so the
// package's tests can assert a round trip without importing the encoder
// themselves.
func DecodeForTest(content []byte, into *map[string]any) error {
	return toml.Unmarshal(content, into)
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/agent/codex/... && go vet ./internal/agent/codex/... && gofmt -l internal/agent/codex && GOOS=darwin go build ./... && GOOS=windows go build ./...`
Expected: PASS (see Step 3 if only the golden layout differs), no vet/gofmt output.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/codex
git commit -m "feat(codex): adapter and requirements.toml renderer with a repo-scoped drop note

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 4: wire it up — awd validates Codex rules, aw-sync renders Codex, e2e, example, docs

**Files:**
- Modify: `cmd/awd/main.go` (composite validator, both call sites)
- Modify: `cmd/aw-sync/main.go` (`newRegistry`)
- Modify: `cmd/aw-sync/e2e_test.go` (`e2ePolicy`, `TestEnrollOnceStatusAndOutage`)
- Modify: `examples/org-policy.yaml` (Codex entry in `baseline`)
- Modify: `README.md`, `deploy/aw-sync/README.md`

**Interfaces:**
- Consumes: `requirements.ForAgent`, `schema.ForAgent`, `codex.New()`.

- [ ] **Step 1: awd — composite validator**

In `cmd/awd/main.go`, add the import `"github.com/acme/agent-wrapper/internal/agent/codex/requirements"` and this function near the other helpers:

```go
// managedValidator runs every agent's own check over a rule's managed
// settings: Claude's settings schema and Codex's requirements allowlist.
// Each ignores the agents it does not know, so adding an agent is adding a
// line here.
func managedValidator(agentName string, managed map[string]any) error {
	if err := schema.ForAgent(agentName, managed); err != nil {
		return err
	}
	return requirements.ForAgent(agentName, managed)
}
```

Replace `h.ManagedValidator = schema.ForAgent` with `h.ManagedValidator = managedValidator`, and `policy.LoadRuleSet(path, schema.ForAgent)` with `policy.LoadRuleSet(path, managedValidator)`.

Add a handler-level test to `cmd/awd/e2e_test.go` if that file already posts revisions, otherwise skip to the aw-sync e2e below, which posts a Codex rule through the same path. Either way, run `go test ./cmd/awd/` after the change.

- [ ] **Step 2: aw-sync — register Codex**

In `cmd/aw-sync/main.go`, import `"github.com/acme/agent-wrapper/internal/agent/codex"` and make `newRegistry` register both:

```go
func newRegistry() (*agent.Registry, error) {
	reg := &agent.Registry{}
	for _, a := range []agent.Adapter{claude.New(), codex.New()} {
		if err := reg.Register(a); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
```

Update the usage text's `--agents` example from `[--agents claude]` to `[--agents claude,codex]` if it lists agents.

- [ ] **Step 3: e2e — render both agents**

In `cmd/aw-sync/e2e_test.go`:

Change `e2ePolicy` so the `baseline` rule also configures Codex and the `payments` rule does too:

```go
const e2ePolicy = `{
  "version": "2026-09-21.e2e",
  "groups": {"alice@acme.com": ["platform"]},
  "rules": [
    {"name": "baseline", "agents": {
      "claude": {"managed": {"model": "sonnet"}},
      "codex": {"managed": {"allowed_sandbox_modes": ["read-only", "workspace-write"]}}
    }},
    {"name": "platform", "match": {"groups": ["platform"]}, "agents": {"claude": {"managed": {"model": "opus"}}}},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {
      "claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}},
      "codex": {"managed": {"allowed_sandbox_modes": ["read-only"]}}
    }}
  ]
}`
```

In `TestEnrollOnceStatusAndOutage`:
- Add `codexRoot := filepath.Join(t.TempDir(), "codex-root")` beside `claudeRoot`.
- Enroll with `"--agents", "claude,codex"` and expect `len(freshReport.Agents) == 2` (update that assertion: `len(freshReport.Agents) != 2 || freshReport.Agents[0] != "claude" || freshReport.Agents[1] != "codex"`).
- Every `once` call gains `"--root", "codex="+codexRoot`.
- After the first `once`, add:

```go
	// Codex gets a whole requirements.toml: the baseline allowlist, the
	// header naming the revision, and no trace of the repo-scoped payments
	// rule, which a static file cannot express and the note reports.
	reqs, err := os.ReadFile(filepath.Join(codexRoot, "requirements.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(reqs), "# Managed by aw-sync from policy revision 2026-09-21.e2e.") ||
		!strings.Contains(string(reqs), "allowed_sandbox_modes = ['read-only', 'workspace-write']") {
		t.Errorf("requirements.toml =\n%s", reqs)
	}
	if _, err := os.Stat(filepath.Join(codexRoot, "requirements.toml.aw-revision")); err == nil {
		t.Error("a TOML file carries its revision in the header; no .aw-revision sibling is written")
	}
```

- In the status check after the second `once`, extend the decoded `report` struct with `Notes []string` and assert `len(report.Files) == 3` (was 2) and that `strings.Join(report.Notes, "\n")` contains `"repo-scoped rule"` and `"payments"`.
- In the outage section, after asserting the drop-in survived, also assert `requirements.toml` still exists in `codexRoot`.

- [ ] **Step 4: Example policy**

In `examples/org-policy.yaml`, inside the `baseline` rule's `agents:` block, after the `claude:` entry, add:

```yaml
      # Codex reads a machine-wide requirements.toml that aw-sync writes.
      # Scalars and allowlists replace across rules; prefix_rules accumulate.
      codex:
        managed:
          allowed_sandbox_modes: [read-only, workspace-write]
          allowed_approval_policies: [on-request, untrusted]
          rules:
            prefix_rules:
              - pattern: [{token: rm}]
                decision: forbidden
                justification: Use git clean -fd instead.
```

Then `go run ./cmd/awd apply --help` is not needed; instead confirm the file still loads: `go test ./internal/policy/` (the policy tests load `examples/org-policy.yaml` if any do; otherwise run `go run ./cmd/awd apply examples/org-policy.yaml --url http://127.0.0.1:1` and expect the failure to be the connection, not validation).

- [ ] **Step 5: Docs**

`README.md`: in the paragraph that begins "The bundle is every rule that could apply to that user" (Try it), after "`aw-sync` does the enrolling and the rendering on a real machine", add: "For Codex it writes `/etc/codex/requirements.toml` from the bundle's `codex` entries; rules scoped to a repository cannot live in a static file and are reported by `aw-sync status`." In "Layout", add `internal/agent/codex/  Codex adapter: requirements.toml renderer and key allowlist` directly under the Claude schema line.

`deploy/aw-sync/README.md`: where `--agents` is described, say the choices are `claude` and `codex`, that a machine lists only the agents it runs, and that Codex's file is `/etc/codex/requirements.toml` (`%ProgramData%\OpenAI\Codex\requirements.toml` on Windows), owned whole by aw-sync. One short paragraph in the file's existing voice; keep the Claude instructions as they are.

- [ ] **Step 6: Verify and commit**

Run: `go test ./... && go vet ./... && gofmt -l . && GOOS=darwin go build ./... && GOOS=windows go build ./...`
Expected: all green, no output from gofmt.

```bash
git add cmd/awd/main.go cmd/aw-sync/main.go cmd/aw-sync/e2e_test.go examples/org-policy.yaml README.md deploy/aw-sync/README.md
git commit -m "feat(aw-sync,awd): render Codex requirements alongside Claude; awd validates Codex rules

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

## Self-review

**Spec coverage.** Renderers intro (relative paths, `managed` is the agent's document, repo-scoped rules dropped with a note shown by `aw-sync status`): Task 3 (`Render`, `dropped`) and Task 4 (status note assertion). Codex section: merge semantics → Task 2; go-toml output with header → Task 3; dated allowlist + round trip → Task 1; unknown key as apply-time error on the Claude path → Task 4 (`managedValidator`). Coexistence with cloud/MDM layers → documented in the package comment and README (Task 3/4). Failure modes "any renderer fails validation → write nothing" → existing sync behaviour, exercised by `Render` returning an error. Testing: golden file → Task 3; merge rules → Task 2; drop note → Task 3; TOML round trip → Tasks 1 and 3.

**Placeholders.** None; every code step is complete. Task 3 Step 3 names the one place where the golden file may need to be regenerated from real output, with the acceptance criteria spelled out.

**Type consistency.** `requirements.Validate/ForAgent/Keys/KeysAsOf` (Task 1) used as such in Tasks 3 and 4. `codex.New`, `codex.Name`, `codex.RequirementsFile`, `codex.DecodeForTest` defined in Task 3 and used in Task 3's tests and Task 4. `AgentMergeRules` keeps its signature. `agent.Rendering{Files, Notes}` and `agent.File{Path, Content, Mode}` match `internal/agent/agent.go`.
