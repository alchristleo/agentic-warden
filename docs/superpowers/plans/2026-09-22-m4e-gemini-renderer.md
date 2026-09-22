# M4e: Gemini renderer and managed-document validator — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aw-sync` can render Gemini CLI's two enforced files from the bundle — the system `settings.json` and the admin policy `policies/50-agent-wrapper.toml` — with Gemini's merge semantics, a note for repo-scoped rules it drops, validation of the settings key allowlist and every policy rule's required fields, and `awd apply` rejects a bad Gemini rule on the same path as a bad Claude or Codex one.

**Architecture:** A new `internal/agent/gemini` adapter (minimal `Adapter` plus `Renderer`, mirroring `internal/agent/codex`) and a sibling `internal/agent/gemini/managed` package holding the dated settings key allowlist, the decision list, `Parts`, `Validate` and `ForAgent` (mirroring `internal/agent/codex/requirements`). `policy.AgentMergeRules` gains the Gemini rule (`settings.tools.exclude`, `settings.mcp.allowed` and `policies` union; everything else follows the existing deep merge: tables such as `settings.mcpServers` merge by key, scalars and other lists replace). `cmd/awd` adds the third validator to `managedValidator`; `cmd/aw-sync` registers the adapter; the e2e renders all three agents.

**Tech Stack:** Go 1.22; `encoding/json` for `settings.json`; `github.com/pelletier/go-toml/v2 v2.2.3` (already a dependency since M4d) for the policy file. No dependency changes.

**Spec:** `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` — sections "Renderers" (intro and "Gemini"), "aw-sync" (Ownership, Paths), "Control plane / API" (validators), "Failure modes", "Testing", "Sequencing (M4e)".

## Spec refinements

1. **Settings key allowlist source and date.** The vendored list is every top-level `settings.json` key documented in the Gemini CLI repository's `docs/reference/configuration.md` (the `SETTINGS-AUTOGEN` block) as read on 2026-09-22, cross-checked against the top-level entries of `SETTINGS_SCHEMA` in `packages/cli/src/config/settingsSchema.ts` on the same day. 26 keys: `admin, adminPolicyPaths, advanced, agents, billing, context, contextManagement, experimental, extensions, general, hooks, hooksConfig, ide, mcp, mcpServers, model, modelConfigs, output, policyPaths, privacy, security, skills, telemetry, tools, ui, useWriteTodos`. Top level only, as for Codex; nested shapes are covered by the JSON round trip alone. The list and its date live in one Go file.
2. **Policy rule validation is stricter than the spec's two fields.** Gemini's TOML loader (`packages/core/src/policy/toml-loader.ts`, zod `PolicyRuleSchema`) requires `toolName` (string or array of strings), `decision` in `allow | deny | ask_user`, **and** `priority`, an integer from 0 to 999; a rule failing that check makes Gemini report the file invalid and skip its rules. So the validator checks all three, with `priority` integral and in range. An author who omits `priority` would otherwise ship a policy file Gemini silently ignores, which is exactly the failure the spec's validator exists to prevent.
3. **Priority must render as a TOML integer.** JSON numbers arrive from the bundle as `float64`, and go-toml marshals `float64(100)` as `100.0`, which Gemini rejects ("priority must be an integer"). The renderer converts each rule's `priority` to `int64` before encoding. Spiked on 2026-09-22: `priority = 100.0` for the raw map, `priority = 100` after conversion.
4. **Managed document shape.** `managed` is `{settings: {...}, policies: [{...}]}`. Any other top-level key is an error; both halves are optional. `settings` must be an object; `policies` must be a list of objects.
5. **Merge rules.** `policy.AgentMergeRules("gemini") = merge.Rules{UnionArrays: []string{"settings.tools.exclude", "settings.mcp.allowed", "policies"}}`. The existing deep merge already merges tables key by key (`settings.mcpServers` by server name, `settings.admin` by field) and replaces scalars and other lists. "Appended across rules" for `policies` is the existing union: concatenate in rule order, drop structural duplicates; a rule repeated verbatim appears once, which is the right outcome for a policy rule.
6. **Two files, both always written.** `settings.json` (mode `0644`) holds the merged `settings` object, `{}` when there is none, encoded with `json.MarshalIndent(v, "", "  ")` plus a trailing newline: keys come out sorted, which makes the output stable. JSON has no comments, so the revision rides in the `settings.json.aw-revision` sibling that `internal/sync` already writes for every `.json` file; Gemini reads only `settings.json` and ignores the sibling. `policies/50-agent-wrapper.toml` (mode `0644`) starts with `# Managed by aw-sync from policy revision <version>. Do not edit: the next sync overwrites this file.` and a blank line, then one `[[rule]]` table per policy entry, encoded from `map[string]any{"rule": policies}`; header alone when there are no policies. `<version>` is `unversioned` when the bundle has none. Writing both files even when empty is what "aw-sync owns them whole" means: a stale file from an earlier revision never lingers.
7. **Drop note.** Rules that carry a `gemini` entry and a `repos` matcher are dropped by `Compile("")`; the renderer notes `gemini: N repo-scoped rule(s) not enforceable in settings.json or policies: <names>` (authored order). No note when N is 0. Rules for other agents do not count.
8. **Minimal adapter.** `gemini.Adapter.Build` locates the binary and passes arguments and the merged environment through, with a note that Gemini's managed settings are enforced by the system files aw-sync writes rather than per launch. Pinning `GEMINI_CLI_SYSTEM_SETTINGS_PATH` is the later launch-time milestone's job, per the spec's "Known weakness". Registered in `cmd/aw-sync` only; `cmd/aw` keeps Claude alone. `Locate` resolves `gemini` on PATH (`Binary` overrides, as in the Codex adapter).
9. **Validator composition.** `cmd/awd`'s existing `managedValidator` gains a third line: `managed.ForAgent`.
10. **Directory ownership is documented, not enforced.** Gemini ignores the standard admin `policies/` directory unless it is root-owned and not group/other-writable (Linux/macOS) or denies write to standard users (Windows). aw-sync runs as root and `cache.ReplaceMode` creates directories `0755` for a `0644` file, so a fresh install satisfies this; `deploy/aw-sync/README.md` says so and tells an operator what to check when a policy seems ignored.

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run `go mod tidy`; no dependency changes in this milestone.
- No `gcc`: `go test -race` cannot run. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit; `GOOS=darwin go build ./... && GOOS=windows go build ./...` for tasks touching `cmd/` or an adapter.
- **Rendered files are never deleted on failure**; one failed renderer means nothing is written this cycle, for any agent (already enforced by `internal/sync`; do not weaken it).
- Renderer file paths are relative to a root the caller supplies; renderers never touch the filesystem.
- Rendered Gemini files are mode `0644`; paths `settings.json` and `policies/50-agent-wrapper.toml` relative to the Gemini root (`/etc/gemini-cli` on Linux, `/Library/Application Support/GeminiCli` on macOS, `%ProgramData%\gemini-cli` on Windows — already in `internal/sync/paths.go`).
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
| `internal/agent/gemini/managed/keys.go` | dated allowlist of top-level `settings.json` keys; the decision list |
| `internal/agent/gemini/managed/managed.go` | `Parts`, `Validate`, `ForAgent`, `Integer` |
| `internal/agent/gemini/managed/managed_test.go` | allowlist, shape, policy rule, round-trip, ForAgent tests |
| `internal/policy/compile.go` | `AgentMergeRules("gemini")` |
| `internal/policy/compile_test.go` | Gemini merge semantics test |
| `internal/agent/gemini/gemini.go` | `Adapter`: `Name`, `Locate`, `Build` |
| `internal/agent/gemini/render.go` | `Render`, header, priority coercion, drop note |
| `internal/agent/gemini/gemini_test.go`, `render_test.go`, `testdata/settings.json`, `testdata/50-agent-wrapper.toml` | adapter and golden tests |
| `cmd/awd/main.go` | third validator in `managedValidator` |
| `cmd/awd/e2e_test.go` | apply rejects a Gemini rule without priority |
| `cmd/aw-sync/main.go` | register the Gemini adapter |
| `cmd/aw-sync/e2e_test.go` | render all three agents; note in status |
| `examples/org-policy.yaml` | a Gemini entry in the baseline rule |
| `README.md`, `deploy/aw-sync/README.md` | Gemini mentioned where agents are listed; policies directory ownership |

---

### Task 1: `managed` package — settings allowlist, document shape, policy rule check

**Files:**
- Create: `internal/agent/gemini/managed/keys.go`
- Create: `internal/agent/gemini/managed/managed.go`
- Create: `internal/agent/gemini/managed/managed_test.go`

**Interfaces:**
- Produces: `managed.Parts(doc map[string]any) (settings map[string]any, policies []any, err error)`, `managed.Validate(doc map[string]any) error`, `managed.ForAgent(agentName string, doc map[string]any) error`, `managed.Integer(v any) (int64, bool)`, `managed.SettingsKeys []string`, `managed.Decisions []string`, `managed.KeysAsOf = "2026-09-22"`. Tasks 3 and 4 consume `Parts`, `Validate`, `Integer`, `ForAgent`.

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/gemini/managed/managed_test.go`:

```go
package managed_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
)

func TestADocumentWithKnownSettingsAndAFullRuleValidates(t *testing.T) {
	doc := map[string]any{
		"settings": map[string]any{
			"tools":      map[string]any{"exclude": []any{"run_shell_command"}},
			"mcp":        map[string]any{"allowed": []any{"docs"}},
			"mcpServers": map[string]any{"docs": map[string]any{"command": "gemini-mcp"}},
			"admin":      map[string]any{"secureModeEnabled": true},
		},
		"policies": []any{
			map[string]any{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": float64(100)},
			map[string]any{"toolName": []any{"write_file", "replace"}, "decision": "ask_user", "priority": 50},
		},
	}

	if err := managed.Validate(doc); err != nil {
		t.Fatalf("Validate = %v, want nil for documented keys and complete rules", err)
	}
}

func TestPartsSplitsTheDocument(t *testing.T) {
	settings, policies, err := managed.Parts(map[string]any{
		"settings": map[string]any{"general": map[string]any{}},
		"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := settings["general"]; !ok || len(policies) != 1 {
		t.Errorf("settings = %v, policies = %v", settings, policies)
	}
}

func TestAnUnknownTopLevelKeyIsRejected(t *testing.T) {
	err := managed.Validate(map[string]any{"settings": map[string]any{}, "rules": []any{}})

	if err == nil || !strings.Contains(err.Error(), `"rules"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestSettingsMustBeAnObjectAndPoliciesAList(t *testing.T) {
	if err := managed.Validate(map[string]any{"settings": []any{}}); err == nil || !strings.Contains(err.Error(), "settings") {
		t.Errorf("settings as a list: err = %v", err)
	}
	if err := managed.Validate(map[string]any{"policies": map[string]any{}}); err == nil || !strings.Contains(err.Error(), "policies") {
		t.Errorf("policies as an object: err = %v", err)
	}
}

func TestAnUnknownSettingsKeyIsRejectedWithTheDate(t *testing.T) {
	err := managed.Validate(map[string]any{"settings": map[string]any{"tools": map[string]any{}, "toolz": map[string]any{}}})

	if err == nil || !strings.Contains(err.Error(), `"toolz"`) || !strings.Contains(err.Error(), managed.KeysAsOf) {
		t.Errorf("err = %v; want the unknown key named and the allowlist date", err)
	}
}

func TestUnknownSettingsKeysAreReportedSorted(t *testing.T) {
	err := managed.Validate(map[string]any{"settings": map[string]any{"zeta": 1, "alpha": 2}})

	if err == nil || strings.Index(err.Error(), `"alpha"`) > strings.Index(err.Error(), `"zeta"`) {
		t.Errorf("err = %v; want unknown keys in sorted order so the message is stable", err)
	}
}

func TestAPolicyRuleMustBeAnObject(t *testing.T) {
	err := managed.Validate(map[string]any{"policies": []any{"deny everything"}})

	if err == nil || !strings.Contains(err.Error(), "policies[0]") {
		t.Errorf("err = %v; want the offending entry named by index", err)
	}
}

func TestAPolicyRuleNeedsAToolName(t *testing.T) {
	for name, rule := range map[string]map[string]any{
		"missing":       {"decision": "deny", "priority": 1},
		"empty string":  {"toolName": "", "decision": "deny", "priority": 1},
		"empty list":    {"toolName": []any{}, "decision": "deny", "priority": 1},
		"list with int": {"toolName": []any{"a", 3}, "decision": "deny", "priority": 1},
	} {
		err := managed.Validate(map[string]any{"policies": []any{rule}})
		if err == nil || !strings.Contains(err.Error(), "toolName") {
			t.Errorf("%s: err = %v; want toolName named", name, err)
		}
	}
}

func TestAPolicyRuleNeedsAKnownDecision(t *testing.T) {
	for name, rule := range map[string]map[string]any{
		"missing":    {"toolName": "*", "priority": 1},
		"unknown":    {"toolName": "*", "decision": "forbid", "priority": 1},
		"wrong type": {"toolName": "*", "decision": true, "priority": 1},
	} {
		err := managed.Validate(map[string]any{"policies": []any{rule}})
		if err == nil || !strings.Contains(err.Error(), "decision") || !strings.Contains(err.Error(), "ask_user") {
			t.Errorf("%s: err = %v; want decision named with the valid values", name, err)
		}
	}
}

func TestAPolicyRuleNeedsAnIntegerPriorityInRange(t *testing.T) {
	// Gemini's loader rejects the whole file when priority is missing, a
	// fraction, or outside 0..999; the validator must catch it at apply time.
	for name, rule := range map[string]map[string]any{
		"missing":   {"toolName": "*", "decision": "deny"},
		"fraction":  {"toolName": "*", "decision": "deny", "priority": 1.5},
		"negative":  {"toolName": "*", "decision": "deny", "priority": -1},
		"too large": {"toolName": "*", "decision": "deny", "priority": 1000},
		"string":    {"toolName": "*", "decision": "deny", "priority": "10"},
	} {
		err := managed.Validate(map[string]any{"policies": []any{rule}})
		if err == nil || !strings.Contains(err.Error(), "priority") {
			t.Errorf("%s: err = %v; want priority named", name, err)
		}
	}
	for _, p := range []any{0, 999, float64(100), int64(5)} {
		if err := managed.Validate(map[string]any{"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": p}}}); err != nil {
			t.Errorf("priority %v (%T): err = %v; want nil", p, p, err)
		}
	}
}

func TestANullInsideAPolicyRuleIsRejectedWithItsPath(t *testing.T) {
	err := managed.Validate(map[string]any{"policies": []any{
		map[string]any{"toolName": "*", "decision": "deny", "priority": 1, "modes": []any{"default", nil}},
	}})

	if err == nil || !strings.Contains(err.Error(), "policies[0].modes[1]") {
		t.Errorf("err = %v; want the null's path, since TOML cannot carry it", err)
	}
}

func TestIntegerAcceptsWholeNumbersOnly(t *testing.T) {
	for v, want := range map[any]int64{5: 5, int64(7): 7, float64(9): 9} {
		if got, ok := managed.Integer(v); !ok || got != want {
			t.Errorf("Integer(%v) = %d, %v; want %d, true", v, got, ok, want)
		}
	}
	for _, v := range []any{1.5, "3", nil, true} {
		if _, ok := managed.Integer(v); ok {
			t.Errorf("Integer(%v) accepted a value that is not a whole number", v)
		}
	}
}

func TestEmptyAndNilDocumentsAreValid(t *testing.T) {
	if err := managed.Validate(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
	if err := managed.Validate(map[string]any{}); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := managed.Validate(map[string]any{"settings": map[string]any{}, "policies": []any{}}); err != nil {
		t.Errorf("empty halves: %v", err)
	}
}

func TestForAgentIgnoresOtherAgents(t *testing.T) {
	bad := map[string]any{"model": "opus"}
	if err := managed.ForAgent("claude", bad); err != nil {
		t.Errorf("claude: %v; want nil, this validator knows Gemini alone", err)
	}
	if err := managed.ForAgent("codex", bad); err != nil {
		t.Errorf("codex: %v; want nil", err)
	}
	if err := managed.ForAgent("gemini", bad); err == nil {
		t.Error("gemini: nil; want the unknown top-level key rejected")
	}
	if err := managed.ForAgent("gemini", nil); err != nil {
		t.Errorf("gemini with no managed settings: %v; want nil", err)
	}
}

func TestTheAllowlistIsSortedAndDated(t *testing.T) {
	if !sort.StringsAreSorted(managed.SettingsKeys) {
		t.Error("SettingsKeys must be sorted; lookup binary-searches it")
	}
	if !sort.StringsAreSorted(managed.Decisions) {
		t.Error("Decisions must be sorted")
	}
	if len(managed.KeysAsOf) != len("2026-09-22") {
		t.Errorf("KeysAsOf = %q; want a YYYY-MM-DD date", managed.KeysAsOf)
	}
	for _, key := range []string{"admin", "mcpServers", "policyPaths", "tools", "useWriteTodos"} {
		i := sort.SearchStrings(managed.SettingsKeys, key)
		if i == len(managed.SettingsKeys) || managed.SettingsKeys[i] != key {
			t.Errorf("SettingsKeys lacks %q", key)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail to compile**

Run: `go test ./internal/agent/gemini/managed/`
Expected: build failure, `no required module provides package .../internal/agent/gemini/managed` or `undefined: managed`.

- [ ] **Step 3: Write the allowlist**

Create `internal/agent/gemini/managed/keys.go`:

```go
package managed

// KeysAsOf is the date the allowlist below was read from the Gemini CLI
// repository's docs/reference/configuration.md and cross-checked against the
// top level of SETTINGS_SCHEMA in packages/cli/src/config/settingsSchema.ts.
// A key that appears after that date is rejected until this file is
// refreshed; the error names the date so the reader knows which reference
// to compare against.
const KeysAsOf = "2026-09-22"

// SettingsKeys is every top-level key settings.json documents, sorted. Only
// the top level is checked: nested shapes are many and change often, and a
// JSON round trip catches what cannot be encoded at all. Gemini reads
// unknown keys without complaint, so a typo here would be a policy that
// silently does nothing; making it an apply-time error is the point.
var SettingsKeys = []string{
	"admin",
	"adminPolicyPaths",
	"advanced",
	"agents",
	"billing",
	"context",
	"contextManagement",
	"experimental",
	"extensions",
	"general",
	"hooks",
	"hooksConfig",
	"ide",
	"mcp",
	"mcpServers",
	"model",
	"modelConfigs",
	"output",
	"policyPaths",
	"privacy",
	"security",
	"skills",
	"telemetry",
	"tools",
	"ui",
	"useWriteTodos",
}

// Decisions is what a policy rule's decision field may say, sorted. It is
// the PolicyDecision enum in packages/core/src/policy/types.ts as of
// KeysAsOf.
var Decisions = []string{"allow", "ask_user", "deny"}
```

- [ ] **Step 4: Write the validator**

Create `internal/agent/gemini/managed/managed.go`:

```go
// Package managed validates the document aw-sync renders for Gemini CLI:
// {settings: {...}, policies: [{...}]}. settings becomes the system
// settings.json and policies becomes an admin policy TOML file, so the
// checks are the ones each format needs: a vendored, dated allowlist of
// top-level settings keys plus a JSON round trip, and for every policy
// rule the three fields Gemini's own loader refuses to do without.
package managed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Parts splits a Gemini managed document into its settings object and its
// policy list, rejecting any other top-level key. Both halves are optional;
// a missing half comes back nil.
func Parts(doc map[string]any) (map[string]any, []any, error) {
	var unknown []string
	for key := range doc {
		if key != "settings" && key != "policies" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, nil, fmt.Errorf("managed: unknown top-level key(s) %s; a Gemini document has settings and policies alone", quoted(unknown))
	}
	var settings map[string]any
	if raw, present := doc["settings"]; present && raw != nil {
		var ok bool
		if settings, ok = raw.(map[string]any); !ok {
			return nil, nil, fmt.Errorf("managed: settings must be an object, not %T", raw)
		}
	}
	var policies []any
	if raw, present := doc["policies"]; present && raw != nil {
		var ok bool
		if policies, ok = raw.([]any); !ok {
			return nil, nil, fmt.Errorf("managed: policies must be a list, not %T", raw)
		}
	}
	return settings, policies, nil
}

// Validate reports whether doc can be rendered for Gemini: the document has
// the right shape, every top-level settings key is documented, settings
// survive a JSON round trip, and every policy rule carries a toolName, a
// known decision and an integer priority from 0 to 999, with no null
// anywhere in it, because TOML cannot represent one. A nil or empty
// document is valid; it renders to an empty settings file and a header-only
// policy file, which is a policy that requires nothing.
func Validate(doc map[string]any) error {
	settings, policies, err := Parts(doc)
	if err != nil {
		return err
	}
	if err := validateSettings(settings); err != nil {
		return err
	}
	for i, entry := range policies {
		if err := validateRule(entry, fmt.Sprintf("policies[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

// ForAgent is a policy.ManagedValidator: it validates the managed document of
// rules aimed at Gemini and ignores every other agent, whose format it does
// not know.
func ForAgent(agentName string, doc map[string]any) error {
	if agentName != "gemini" || doc == nil {
		return nil
	}
	return Validate(doc)
}

// Integer reports v as an int64 when it is a whole number: a Go int from a
// literal, or a float64 with no fraction, which is how every number in a
// bundle arrives after JSON decoding. The renderer uses it to write
// priority as a TOML integer, since Gemini rejects 100.0.
func Integer(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
	}
	return 0, false
}

func validateSettings(settings map[string]any) error {
	var unknown []string
	for key := range settings {
		if !knownKey(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("managed: unknown settings key(s) %s; the allowlist is from the reference as of %s", quoted(unknown), KeysAsOf)
	}
	if len(settings) == 0 {
		return nil
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("managed: settings cannot be written as JSON: %w", err)
	}
	var back map[string]any
	if err := json.Unmarshal(encoded, &back); err != nil {
		return fmt.Errorf("managed: settings do not read back as JSON: %w", err)
	}
	return nil
}

// validateRule checks one policy entry against what Gemini's TOML loader
// requires. path names the entry in errors, as policies[i].
func validateRule(entry any, path string) error {
	rule, ok := entry.(map[string]any)
	if !ok {
		return fmt.Errorf("managed: %s must be an object, not %T", path, entry)
	}
	if p := firstNull(rule, path); p != "" {
		return fmt.Errorf("managed: %s is null, which TOML cannot represent", p)
	}
	switch name := rule["toolName"].(type) {
	case string:
		if name == "" {
			return fmt.Errorf(`managed: %s.toolName is empty; use "*" to match every tool`, path)
		}
	case []any:
		if len(name) == 0 {
			return fmt.Errorf("managed: %s.toolName is an empty list", path)
		}
		for i, n := range name {
			if s, ok := n.(string); !ok || s == "" {
				return fmt.Errorf("managed: %s.toolName[%d] must be a non-empty string", path, i)
			}
		}
	default:
		return fmt.Errorf("managed: %s.toolName is required and must be a string or a list of strings", path)
	}
	decision, _ := rule["decision"].(string)
	if !knownDecision(decision) {
		return fmt.Errorf("managed: %s.decision must be one of %s, not %q", path, strings.Join(Decisions, ", "), decision)
	}
	if priority, ok := Integer(rule["priority"]); !ok || priority < 0 || priority > 999 {
		return fmt.Errorf("managed: %s.priority is required and must be an integer from 0 to 999; Gemini rejects the whole policy file otherwise", path)
	}
	return nil
}

// firstNull returns the path of the first nil value in v, or "" when there
// is none. TOML has no null: the encoder drops a nil map value silently and
// refuses a nil slice element, and neither is what an author meant.
func firstNull(v any, path string) string {
	switch t := v.(type) {
	case nil:
		return path
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if found := firstNull(t[k], path+"."+k); found != "" {
				return found
			}
		}
	case []any:
		for i, e := range t {
			if found := firstNull(e, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

func knownKey(key string) bool {
	i := sort.SearchStrings(SettingsKeys, key)
	return i < len(SettingsKeys) && SettingsKeys[i] == key
}

func knownDecision(d string) bool {
	i := sort.SearchStrings(Decisions, d)
	return i < len(Decisions) && Decisions[i] == d
}

func quoted(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(q, ", ")
}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `go test ./internal/agent/gemini/managed/ -v`
Expected: every test PASS. If `TestANullInsideAPolicyRuleIsRejectedWithItsPath` fails on the path, check that `validateRule` calls `firstNull(rule, path)` with `path` (not `""`) so the report starts with `policies[0]`.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./internal/agent/gemini/... && git add internal/agent/gemini/managed
git commit -m "feat(gemini): managed-document validator with a dated settings allowlist and policy rule checks

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 2: Gemini merge semantics in `policy`

**Files:**
- Modify: `internal/policy/compile.go` (`AgentMergeRules`)
- Modify: `internal/policy/compile_test.go` (append one test)

**Interfaces:**
- Produces: `AgentMergeRules("gemini")` returning `merge.Rules{UnionArrays: []string{"settings.tools.exclude", "settings.mcp.allowed", "policies"}}`. Task 3's renderer relies on `Bundle.Compile` applying it.

- [ ] **Step 1: Append the failing test**

Append to `internal/policy/compile_test.go`:

```go
func TestGeminiRulesMergeWithGeminiSemantics(t *testing.T) {
	// Gemini's two enforced files layer differently: exclusion and MCP
	// allowlists union so a team's additions join the organization's,
	// mcpServers merge by name, other settings replace, and policy rules
	// accumulate since each is its own restriction.
	rs := &policy.RuleSet{Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"gemini": {Managed: map[string]any{
			"settings": map[string]any{
				"tools":      map[string]any{"exclude": []any{"run_shell_command"}},
				"mcp":        map[string]any{"allowed": []any{"docs"}},
				"mcpServers": map[string]any{"docs": map[string]any{"command": "gemini-mcp"}},
				"admin":      map[string]any{"secureModeEnabled": true},
			},
			"policies": []any{map[string]any{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": 100}},
		}}}},
		{Name: "strict", Agents: map[string]policy.AgentConfig{"gemini": {Managed: map[string]any{
			"settings": map[string]any{
				"tools":      map[string]any{"exclude": []any{"web_fetch"}},
				"mcp":        map[string]any{"allowed": []any{"jira"}},
				"mcpServers": map[string]any{"jira": map[string]any{"url": "https://jira/mcp"}},
				"admin":      map[string]any{"secureModeEnabled": false},
			},
			"policies": []any{map[string]any{"toolName": []any{"write_file", "replace"}, "decision": "ask_user", "priority": 50}},
		}}}},
	}}

	managed := rs.Compile(policy.Subject{}).Agent("gemini").Managed
	settings, _ := managed["settings"].(map[string]any)

	tools, _ := settings["tools"].(map[string]any)
	if exclude, _ := tools["exclude"].([]any); len(exclude) != 2 || exclude[0] != "run_shell_command" || exclude[1] != "web_fetch" {
		t.Errorf("tools.exclude = %v; want the union in rule order", exclude)
	}
	mcp, _ := settings["mcp"].(map[string]any)
	if allowed, _ := mcp["allowed"].([]any); len(allowed) != 2 {
		t.Errorf("mcp.allowed = %v; want the union", allowed)
	}
	servers, _ := settings["mcpServers"].(map[string]any)
	if _, docs := servers["docs"]; !docs {
		t.Error("mcpServers lost docs; tables merge by name")
	}
	if _, jira := servers["jira"]; !jira {
		t.Error("mcpServers lost jira; tables merge by name")
	}
	admin, _ := settings["admin"].(map[string]any)
	if admin["secureModeEnabled"] != false {
		t.Errorf("admin.secureModeEnabled = %v; scalars replace", admin["secureModeEnabled"])
	}
	if policies, _ := managed["policies"].([]any); len(policies) != 2 {
		t.Errorf("policies = %v; want both rules appended in order", policies)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/policy/ -run TestGeminiRulesMergeWithGeminiSemantics -v`
Expected: FAIL — `tools.exclude = [web_fetch]` (replace, not union) and `policies` has one entry.

- [ ] **Step 3: Add the rule**

In `internal/policy/compile.go`, replace the `AgentMergeRules` doc comment and function with:

```go
// AgentMergeRules says how one agent's managed settings combine across the
// rules that apply to a subject. Claude's permission lists union so an
// organization allowlist and a team allowlist coexist instead of one
// silently erasing the other. Codex's requirements follow Codex's own
// layering: scalars and allowlists replace (a union would widen an
// allowlist), tables such as mcp_servers merge by key, and only
// rules.prefix_rules accumulate, since each entry is its own restriction.
// Gemini's tool exclusions and MCP allowlist union for the same reason as
// Claude's lists, mcpServers merge by name, and policy rules accumulate.
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
	case "gemini":
		return merge.Rules{UnionArrays: []string{
			"settings.tools.exclude",
			"settings.mcp.allowed",
			"policies",
		}}
	default:
		return merge.Rules{}
	}
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/policy/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/policy/ && git add internal/policy/compile.go internal/policy/compile_test.go
git commit -m "feat(policy): Gemini merge semantics — tool exclusions, MCP allowlist and policies accumulate

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 3: the Gemini adapter and renderer

**Files:**
- Create: `internal/agent/gemini/gemini.go`
- Create: `internal/agent/gemini/render.go`
- Create: `internal/agent/gemini/gemini_test.go`
- Create: `internal/agent/gemini/render_test.go`
- Create: `internal/agent/gemini/testdata/settings.json`
- Create: `internal/agent/gemini/testdata/50-agent-wrapper.toml`

**Interfaces:**
- Consumes: `managed.Validate`, `managed.Parts`, `managed.Integer` (Task 1); `policy.Bundle.Compile(repo).Agent(name).Managed`; `agent.Rendering{Files, Notes}`, `agent.File{Path, Content, Mode}`, `agent.ResolveBinary`, `merge.Env` (existing).
- Produces: `gemini.New() *Adapter`, `gemini.Name = "gemini"`, `gemini.SettingsFile = "settings.json"`, `gemini.PoliciesFile = "policies/50-agent-wrapper.toml"`, `(*Adapter).Render(*policy.Bundle) (agent.Rendering, error)`, `gemini.DecodeTOMLForTest([]byte, *map[string]any) error`. Task 4 consumes `New`, `SettingsFile`, `PoliciesFile`.

- [ ] **Step 1: Write the adapter tests**

Create `internal/agent/gemini/gemini_test.go`:

```go
package gemini_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
)

// fakeBinary puts an executable named gemini on a private PATH.
func fakeBinary(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable bit")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gemini")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir, "HOME=" + t.TempDir()}
}

func TestNameIsGemini(t *testing.T) {
	if got := gemini.New().Name(); got != "gemini" {
		t.Errorf("Name = %q", got)
	}
}

func TestBuildPassesArgumentsThroughAndMergesTheEnvironment(t *testing.T) {
	env := fakeBinary(t)

	launch, err := gemini.New().Build(context.Background(), agent.BuildOptions{
		Env:      env,
		Args:     []string{"--model", "gemini-2.5-pro"},
		Settings: agent.Settings{Env: map[string]string{"GOOGLE_GEMINI_BASE_URL": "https://proxy.acme"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if launch.Agent != "gemini" || !strings.HasSuffix(launch.Binary, "/gemini") {
		t.Errorf("launch = %+v", launch)
	}
	if strings.Join(launch.Args, " ") != "--model gemini-2.5-pro" {
		t.Errorf("args = %q; want the developer's arguments untouched", launch.Args)
	}
	found := false
	for _, e := range launch.Env {
		if e == "GOOGLE_GEMINI_BASE_URL=https://proxy.acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("env %q lacks the policy's variable", launch.Env)
	}
	if len(launch.Notes) == 0 || !strings.Contains(strings.Join(launch.Notes, "\n"), "settings.json") {
		t.Errorf("notes %q should say where Gemini's managed settings are enforced", launch.Notes)
	}
}

func TestBuildFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, err := gemini.New().Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})

	if err == nil {
		t.Error("Build = nil error; want the missing binary reported")
	}
}
```

- [ ] **Step 2: Write the renderer tests and golden files**

Create `internal/agent/gemini/testdata/settings.json` (exactly this content, trailing newline included):

```json
{
  "admin": {
    "secureModeEnabled": false
  },
  "mcp": {
    "allowed": [
      "docs",
      "jira"
    ]
  },
  "mcpServers": {
    "docs": {
      "command": "gemini-mcp"
    },
    "jira": {
      "url": "https://jira/mcp"
    }
  },
  "tools": {
    "exclude": [
      "run_shell_command",
      "web_fetch"
    ]
  }
}
```

Create `internal/agent/gemini/testdata/50-agent-wrapper.toml` (exactly this content; one blank line after the header, single quotes as go-toml writes them, trailing newline after the last line):

```toml
# Managed by aw-sync from policy revision 2026-09-22.1. Do not edit: the next sync overwrites this file.

[[rule]]
commandPrefix = 'rm -rf'
decision = 'deny'
denyMessage = 'Deletion is permanent'
priority = 100
toolName = 'run_shell_command'

[[rule]]
decision = 'ask_user'
modes = ['default']
priority = 50
toolName = ['write_file', 'replace']
```

Create `internal/agent/gemini/render_test.go`:

```go
package gemini_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
	"github.com/acme/agent-wrapper/internal/policy"
)

func geminiRule(name string, repos []string, managed map[string]any) policy.Rule {
	return policy.Rule{
		Name:   name,
		Match:  policy.Match{Repos: repos},
		Agents: map[string]policy.AgentConfig{gemini.Name: {Managed: managed}},
	}
}

// bundle is a platform user's bundle: a baseline for everyone, a stricter
// rule, a repo-scoped rule Gemini cannot honour, and a Claude-only rule.
// Priorities are float64 because that is how JSON numbers reach a renderer.
func bundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "2026-09-22.1",
		Groups:  []string{"platform"},
		Rules: []policy.Rule{
			geminiRule("baseline", nil, map[string]any{
				"settings": map[string]any{
					"tools":      map[string]any{"exclude": []any{"run_shell_command"}},
					"mcp":        map[string]any{"allowed": []any{"docs"}},
					"mcpServers": map[string]any{"docs": map[string]any{"command": "gemini-mcp"}},
					"admin":      map[string]any{"secureModeEnabled": true},
				},
				"policies": []any{
					map[string]any{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": float64(100), "denyMessage": "Deletion is permanent"},
				},
			}),
			geminiRule("strict", nil, map[string]any{
				"settings": map[string]any{
					"tools":      map[string]any{"exclude": []any{"web_fetch"}},
					"mcp":        map[string]any{"allowed": []any{"jira"}},
					"mcpServers": map[string]any{"jira": map[string]any{"url": "https://jira/mcp"}},
					"admin":      map[string]any{"secureModeEnabled": false},
				},
				"policies": []any{
					map[string]any{"toolName": []any{"write_file", "replace"}, "decision": "ask_user", "priority": float64(50), "modes": []any{"default"}},
				},
			}),
			geminiRule("payments", []string{"github.com/acme/payments*"}, map[string]any{
				"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": float64(999)}},
			}),
			{Name: "claude-only", Match: policy.Match{Repos: []string{"github.com/acme/x"}},
				Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
		},
	}
}

// files indexes a rendering by path.
func files(t *testing.T, r agent.Rendering) map[string]agent.File {
	t.Helper()
	out := make(map[string]agent.File, len(r.Files))
	for _, f := range r.Files {
		out[f.Path] = f
	}
	return out
}

func TestRenderMatchesTheGoldenFiles(t *testing.T) {
	rendering, err := gemini.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(rendering.Files) != 2 {
		t.Fatalf("files = %d, want settings.json and the policy file", len(rendering.Files))
	}
	got := files(t, rendering)
	for path, golden := range map[string]string{
		gemini.SettingsFile: "settings.json",
		gemini.PoliciesFile: "50-agent-wrapper.toml",
	} {
		f, ok := got[path]
		if !ok || f.Mode != 0o644 {
			t.Errorf("%s: present %v mode %o", path, ok, f.Mode)
			continue
		}
		want, err := os.ReadFile(filepath.Join("testdata", golden))
		if err != nil {
			t.Fatal(err)
		}
		if string(f.Content) != string(want) {
			t.Errorf("%s differs from testdata/%s:\n%s\nwant:\n%s", path, golden, f.Content, want)
		}
	}
}

func TestRenderNotesTheRepoScopedRulesItDropped(t *testing.T) {
	rendering, err := gemini.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}

	notes := strings.Join(rendering.Notes, "\n")
	if len(rendering.Notes) != 1 || !strings.Contains(notes, "1 repo-scoped rule") || !strings.Contains(notes, "payments") {
		t.Errorf("notes = %q; want one note naming the dropped Gemini rule", rendering.Notes)
	}
	if strings.Contains(notes, "claude-only") {
		t.Error("the note counts a rule that does not configure Gemini")
	}
}

func TestRenderWithNoGeminiRulesWritesEmptyFiles(t *testing.T) {
	rendering, err := gemini.New().Render(&policy.Bundle{Version: "v1", Rules: []policy.Rule{
		{Name: "claude", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	got := files(t, rendering)
	if s := string(got[gemini.SettingsFile].Content); s != "{}\n" {
		t.Errorf("settings.json = %q; want an empty object so a stale file never lingers", s)
	}
	if p := string(got[gemini.PoliciesFile].Content); p != "# Managed by aw-sync from policy revision v1. Do not edit: the next sync overwrites this file.\n\n" {
		t.Errorf("policy file = %q; want the header alone", p)
	}
	if len(rendering.Notes) != 0 {
		t.Errorf("notes = %q; nothing was dropped", rendering.Notes)
	}
}

func TestRenderOfANilBundleIsEmptyAndUnversioned(t *testing.T) {
	rendering, err := gemini.New().Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := files(t, rendering)
	if !strings.HasPrefix(string(got[gemini.PoliciesFile].Content), "# Managed by aw-sync from policy revision unversioned.") {
		t.Errorf("policy file = %q", got[gemini.PoliciesFile].Content)
	}
	if string(got[gemini.SettingsFile].Content) != "{}\n" {
		t.Errorf("settings.json = %q", got[gemini.SettingsFile].Content)
	}
}

func TestRenderRejectsAnUnknownSettingsKey(t *testing.T) {
	_, err := gemini.New().Render(&policy.Bundle{Rules: []policy.Rule{
		geminiRule("bad", nil, map[string]any{"settings": map[string]any{"toolz": map[string]any{}}}),
	}})

	if err == nil || !strings.Contains(err.Error(), `"toolz"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestRenderRejectsAPolicyRuleWithoutAPriority(t *testing.T) {
	_, err := gemini.New().Render(&policy.Bundle{Rules: []policy.Rule{
		geminiRule("bad", nil, map[string]any{"policies": []any{map[string]any{"toolName": "*", "decision": "deny"}}}),
	}})

	if err == nil || !strings.Contains(err.Error(), "priority") {
		t.Errorf("err = %v; want priority named", err)
	}
}

func TestRenderedFilesReadBack(t *testing.T) {
	rendering, err := gemini.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	got := files(t, rendering)

	var settings map[string]any
	if err := json.Unmarshal(got[gemini.SettingsFile].Content, &settings); err != nil {
		t.Fatalf("settings.json round trip: %v", err)
	}
	if admin, _ := settings["admin"].(map[string]any); admin["secureModeEnabled"] != false {
		t.Errorf("admin.secureModeEnabled = %v after the strict rule", admin["secureModeEnabled"])
	}

	var policies map[string]any
	if err := gemini.DecodeTOMLForTest(got[gemini.PoliciesFile].Content, &policies); err != nil {
		t.Fatalf("policy file round trip: %v", err)
	}
	rules, _ := policies["rule"].([]any)
	if len(rules) != 2 {
		t.Fatalf("rule = %v; want two [[rule]] tables", rules)
	}
	first, _ := rules[0].(map[string]any)
	if _, isInt := first["priority"].(int64); !isInt {
		t.Errorf("priority decoded as %T; Gemini requires a TOML integer, not a float", first["priority"])
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/agent/gemini/`
Expected: build failure, `undefined: gemini` / package not found.

- [ ] **Step 4: Write the adapter**

Create `internal/agent/gemini/gemini.go`:

```go
// Package gemini integrates Google's Gemini CLI. Its enforced configuration
// is two root-owned files Gemini reads on its own: the system settings.json,
// which overrides every user and project setting, and an admin policy TOML
// file, which outranks every user policy; this package renders both for
// aw-sync. Launching Gemini through aw is pass-through for now: the system
// settings path can be moved with GEMINI_CLI_SYSTEM_SETTINGS_PATH, and
// pinning it per launch is the later launch-time milestone. Until then the
// admin policy directory, which has no such override, is where rules that
// must hold belong.
package gemini

import (
	"context"
	"os"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/merge"
)

// Name is the subcommand a developer types and the agent key in a policy.
const Name = "gemini"

// Adapter launches Gemini and renders its system files. The exported field
// exists so tests can control the binary.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
}

// New returns an adapter with the defaults a developer's machine implies.
func New() *Adapter { return &Adapter{} }

// Name reports the subcommand this adapter handles.
func (a *Adapter) Name() string { return Name }

// Locate resolves the real Gemini binary, never the wrapper.
func (a *Adapter) Locate(env []string) (string, error) {
	name := a.Binary
	if name == "" {
		name = Name
	}
	return agent.ResolveBinary(name, env)
}

// Build computes the launch. Gemini takes no settings file on the command
// line, so the policy's managed document is not applied here: aw-sync has
// already written it to the system settings and admin policy files, which
// Gemini reads on its own. Only the environment is merged, so an
// organization's variables still reach the process.
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
	launch.Notes = append(launch.Notes, "gemini: managed settings are enforced by the system settings.json and admin policies that aw-sync writes, not per launch")
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}
```

- [ ] **Step 5: Write the renderer**

Create `internal/agent/gemini/render.go`:

```go
package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
	"github.com/acme/agent-wrapper/internal/policy"
)

// SettingsFile is Gemini's system settings, relative to its system
// directory. aw-sync owns the whole file: it has no drop-in directory, so
// there is nothing to share with another author. Its revision rides in the
// .aw-revision sibling that sync writes for every JSON file, since JSON has
// no comments.
const SettingsFile = "settings.json"

// PoliciesFile is aw-sync's one file in Gemini's admin policy directory,
// which is a drop-in directory: other administrators' .toml files beside it
// are left alone. The revision rides in a header comment.
const PoliciesFile = "policies/50-agent-wrapper.toml"

// Render compiles the bundle for a session in no repository and writes the
// result as settings.json and the admin policy file. Both are written even
// when empty, so a file from an earlier revision never outlives the policy
// that produced it. A static file cannot express a repo-scoped rule, so
// those are dropped here and reported in a note; silence would leave an
// author believing a restriction applies when it does not.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	doc := bundle.Compile("").Agent(Name).Managed
	if err := managed.Validate(doc); err != nil {
		return agent.Rendering{}, fmt.Errorf("gemini: %w", err)
	}
	settings, policies, err := managed.Parts(doc)
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("gemini: %w", err)
	}

	if settings == nil {
		settings = map[string]any{}
	}
	settingsJSON, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("gemini: encoding settings.json: %w", err)
	}
	settingsJSON = append(settingsJSON, '\n')

	var rules []byte
	if len(policies) > 0 {
		rules, err = toml.Marshal(map[string]any{"rule": integerPriorities(policies)})
		if err != nil {
			return agent.Rendering{}, fmt.Errorf("gemini: encoding %s: %w", PoliciesFile, err)
		}
	}
	policiesTOML := append([]byte(header(bundle)), rules...)

	return agent.Rendering{
		Files: []agent.File{
			{Path: SettingsFile, Content: settingsJSON, Mode: 0o644},
			{Path: PoliciesFile, Content: policiesTOML, Mode: 0o644},
		},
		Notes: dropped(bundle),
	}, nil
}

// integerPriorities copies each rule with its priority as an int64. Numbers
// reach a renderer as float64 after JSON decoding, and go-toml writes
// float64(100) as 100.0, which Gemini's loader rejects as a non-integer.
// Validate has already established every priority is a whole number.
func integerPriorities(policies []any) []any {
	out := make([]any, 0, len(policies))
	for _, entry := range policies {
		rule, _ := entry.(map[string]any)
		copied := make(map[string]any, len(rule))
		for k, v := range rule {
			copied[k] = v
		}
		if n, ok := managed.Integer(rule["priority"]); ok {
			copied["priority"] = n
		}
		out = append(out, copied)
	}
	return out
}

// header is the first line of the policy file. TOML has no sidecar
// convention, so the revision rides in a comment, where `aw-sync status`
// and a curious operator can both read it.
func header(bundle *policy.Bundle) string {
	version := "unversioned"
	if bundle != nil && bundle.Version != "" {
		version = bundle.Version
	}
	return "# Managed by aw-sync from policy revision " + version + ". Do not edit: the next sync overwrites this file.\n\n"
}

// dropped names the Gemini rules Compile("") left out because they are
// scoped to repositories. Rules for other agents are not this renderer's to
// report.
func dropped(bundle *policy.Bundle) []string {
	if bundle == nil {
		return nil
	}
	var names []string
	for _, rule := range bundle.Rules {
		if _, forGemini := rule.Agents[Name]; forGemini && len(rule.Match.Repos) > 0 {
			names = append(names, rule.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("gemini: %d repo-scoped rule(s) not enforceable in settings.json or policies: %s",
		len(names), strings.Join(names, ", "))}
}

// DecodeTOMLForTest parses a rendered policy file back into a map. It exists
// so the package's tests can assert a round trip without importing the
// encoder themselves.
func DecodeTOMLForTest(content []byte, into *map[string]any) error {
	return toml.Unmarshal(content, into)
}
```

- [ ] **Step 6: Run the package tests**

Run: `go test ./internal/agent/gemini/... -v`
Expected: every test PASS. If `TestRenderMatchesTheGoldenFiles` fails only on whitespace or quoting in the TOML golden, print the rendered content, confirm it satisfies all of: header line exactly as in refinement 6, one blank line after it, `priority = 100` and `priority = 50` as integers (no `.0`), two `[[rule]]` tables in rule order, keys sorted within each table — and then replace `testdata/50-agent-wrapper.toml` with the real output. Do not touch the JSON golden: `json.MarshalIndent` output is fully determined.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./internal/agent/gemini/... && GOOS=darwin go build ./... && GOOS=windows go build ./... && git add internal/agent/gemini
git commit -m "feat(gemini): adapter and renderer for settings.json and the admin policy file, with a repo-scoped drop note

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 4: wire it up — awd validates Gemini rules, aw-sync renders Gemini, e2e, example, docs

**Files:**
- Modify: `cmd/awd/main.go` (`managedValidator`)
- Modify: `cmd/awd/e2e_test.go` (one new test)
- Modify: `cmd/aw-sync/main.go` (`newRegistry`, usage text)
- Modify: `cmd/aw-sync/e2e_test.go` (`e2ePolicy`, `TestEnrollOnceStatusAndOutage`)
- Modify: `examples/org-policy.yaml` (Gemini entry in `baseline`)
- Modify: `README.md`, `deploy/aw-sync/README.md`

**Interfaces:**
- Consumes: `managed.ForAgent` (Task 1), `gemini.New()`, `gemini.SettingsFile`, `gemini.PoliciesFile` (Task 3).

- [ ] **Step 1: awd — the failing e2e test**

In `cmd/awd/e2e_test.go`, immediately before `func TestApplyWithoutTheAdminTokenIsRefused`, insert:

```go
func TestApplyRejectsAGeminiPolicyRuleWithoutAPriority(t *testing.T) {
	// A Gemini rule goes through the same gate as a Claude or Codex one.
	// Gemini's own loader refuses a policy file whose rule lacks a
	// priority, so the whole file would be ignored on every machine; apply
	// and the server both stop it in front of the author.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      gemini:\n        managed:\n          policies:\n            - toolName: run_shell_command\n              decision: deny\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for a Gemini rule without a priority: %s", out)
	}
	if !strings.Contains(out, "policies[0].priority") {
		t.Errorf("output %q does not name the offending field", out)
	}

	body := `{"version":"v2","rules":[{"name":"b","agents":{"gemini":{"managed":{"policies":[{"toolName":"*","decision":"deny"}]}}}}]}`
	req, err := http.NewRequest(http.MethodPost, s.url+"/v1/policy/revisions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("server status = %d, want 422", resp.StatusCode)
	}
}
```

Run: `go test ./cmd/awd/ -run TestApplyRejectsAGeminiPolicyRuleWithoutAPriority`
Expected: FAIL — `apply exited 0`.

- [ ] **Step 2: awd — the third validator**

In `cmd/awd/main.go`, add the import `"github.com/acme/agent-wrapper/internal/agent/gemini/managed"` (keep the import block sorted: it goes after the `codex/requirements` line) and replace `managedValidator` with:

```go
// managedValidator runs every agent's own check over a rule's managed
// settings: Claude's settings schema, Codex's requirements allowlist and
// Gemini's managed-document rules. Each ignores the agents it does not
// know, so adding an agent is adding a line here. The parameter is doc,
// not managed, so the Gemini package name is not shadowed.
func managedValidator(agentName string, doc map[string]any) error {
	if err := schema.ForAgent(agentName, doc); err != nil {
		return err
	}
	if err := requirements.ForAgent(agentName, doc); err != nil {
		return err
	}
	return managed.ForAgent(agentName, doc)
}
```

Run: `go test ./cmd/awd/`
Expected: PASS, including the new test.

- [ ] **Step 3: aw-sync — register Gemini**

In `cmd/aw-sync/main.go`, import `"github.com/acme/agent-wrapper/internal/agent/gemini"` (after the `codex` import) and change the adapter list in `newRegistry` to:

```go
	for _, a := range []agent.Adapter{claude.New(), codex.New(), gemini.New()} {
```

In the usage text, change `[--agents claude,codex]` to `[--agents claude,codex,gemini]`.

Run: `go build ./cmd/aw-sync`
Expected: builds.

- [ ] **Step 4: e2e — render all three agents**

In `cmd/aw-sync/e2e_test.go`:

Replace `e2ePolicy` with:

```go
const e2ePolicy = `{
  "version": "2026-09-21.e2e",
  "groups": {"alice@acme.com": ["platform"]},
  "rules": [
    {"name": "baseline", "agents": {
      "claude": {"managed": {"model": "sonnet"}},
      "codex": {"managed": {"allowed_sandbox_modes": ["read-only", "workspace-write"]}},
      "gemini": {"managed": {
        "settings": {"admin": {"secureModeEnabled": true}},
        "policies": [{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": 100}]
      }}
    }},
    {"name": "platform", "match": {"groups": ["platform"]}, "agents": {"claude": {"managed": {"model": "opus"}}}},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {
      "claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}},
      "codex": {"managed": {"allowed_sandbox_modes": ["read-only"]}},
      "gemini": {"managed": {"policies": [{"toolName": "*", "decision": "deny", "priority": 999}]}}
    }}
  ]
}`
```

In `TestEnrollOnceStatusAndOutage`:
- Add `geminiRoot := filepath.Join(t.TempDir(), "gemini-root")` beside `codexRoot`.
- Enroll with `"--agents", "claude,codex,gemini"` and change the fresh-report assertion to `len(freshReport.Agents) != 3 || freshReport.Agents[0] != "claude" || freshReport.Agents[1] != "codex" || freshReport.Agents[2] != "gemini"`.
- Every `once` call (there are four) gains `"--root", "gemini="+geminiRoot` after the codex root.
- Directly after the Codex `requirements.toml.aw-revision` check (the block ending `t.Error("a TOML file carries its revision in the header; no .aw-revision sibling is written")` and its closing brace), insert:

```go
	// Gemini gets both files: settings.json with its revision sibling, and
	// the admin policy file with the revision in its header. The repo-scoped
	// payments rule is absent from both and named in the note.
	settings, err := os.ReadFile(filepath.Join(geminiRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"secureModeEnabled": true`) {
		t.Errorf("settings.json =\n%s", settings)
	}
	if got, _ := os.ReadFile(filepath.Join(geminiRoot, "settings.json.aw-revision")); string(got) != "2026-09-21.e2e\n" {
		t.Errorf("settings.json.aw-revision = %q; a JSON file carries its revision in a sibling", got)
	}
	policies, err := os.ReadFile(filepath.Join(geminiRoot, "policies", "50-agent-wrapper.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(policies), "# Managed by aw-sync from policy revision 2026-09-21.e2e.") ||
		!strings.Contains(string(policies), "priority = 100\n") ||
		strings.Contains(string(policies), "priority = 999") {
		t.Errorf("50-agent-wrapper.toml =\n%s", policies)
	}
```

- In the status check after the second `once`, change `len(report.Files) != 3` to `len(report.Files) != 5` and extend the notes assertion so it also requires `"gemini:"`:

```go
	if notes := strings.Join(report.Notes, "\n"); !strings.Contains(notes, "repo-scoped rule") || !strings.Contains(notes, "payments") ||
		!strings.Contains(notes, "codex:") || !strings.Contains(notes, "gemini:") {
		t.Errorf("report.Notes = %q; want the dropped payments rule reported by both static-file renderers", report.Notes)
	}
```

- In the outage section, after the `requirements.toml` survival check, add:

```go
	if _, err := os.Stat(filepath.Join(geminiRoot, "policies", "50-agent-wrapper.toml")); err != nil {
		t.Error("an outage removed the Gemini policy file")
	}
```

Run: `go test ./cmd/aw-sync/ -run TestEnrollOnceStatusAndOutage -v`
Expected: PASS. Then `go test ./cmd/aw-sync/` for the rest (the relative-root test enrolls Claude alone and still expects 2 files; leave it).

- [ ] **Step 5: Example policy**

In `examples/org-policy.yaml`, inside the `baseline` rule's `agents:` block, after the `codex:` entry (its last line is `justification: Use git clean -fd instead.`), add:

```yaml
      # Gemini reads a system settings.json (overrides every user setting)
      # and an admin policy directory; aw-sync writes one file in each.
      # tools.exclude and mcp.allowed union across rules, mcpServers merge
      # by name, other settings replace, and policies accumulate. Every
      # policy rule needs toolName, decision and an integer priority 0-999.
      gemini:
        managed:
          settings:
            admin:
              secureModeEnabled: true
            tools:
              exclude: [web_fetch]
          policies:
            - toolName: run_shell_command
              commandPrefix: rm -rf
              decision: deny
              priority: 100
              denyMessage: Use git clean -fd instead.
```

Confirm the file still loads: `go test ./internal/policy/` (its tests load the example), then `go run ./cmd/awd apply examples/org-policy.yaml --url http://127.0.0.1:1` and expect the failure to be the connection (`connection refused`), not validation.

- [ ] **Step 6: Docs**

`README.md`:
- Status line: change to `Status: early. Claude Code is the only agent `aw` launches; `aw-sync` also renders Codex's `requirements.toml` and Gemini's system settings and admin policy.`
- In the Try-it paragraph that now says "For Codex it writes `/etc/codex/requirements.toml` from the bundle's `codex` entries; rules scoped to a repository cannot live in a static file and are reported by `aw-sync status`.", change it to: "For Codex it writes `/etc/codex/requirements.toml` from the bundle's `codex` entries, and for Gemini `/etc/gemini-cli/settings.json` and `/etc/gemini-cli/policies/50-agent-wrapper.toml` from the `gemini` entries; rules scoped to a repository cannot live in a static file and are reported by `aw-sync status`."
- Layout: directly under the `internal/agent/codex/` line add `    internal/agent/gemini/  Gemini adapter: settings.json and admin policy renderer, key allowlist`.

`deploy/aw-sync/README.md`: in the `--agents` paragraph, change "the choices are `claude` and `codex`" to "the choices are `claude`, `codex` and `gemini`", delete the sentence "The Gemini renderer arrives in a later milestone.", and after the Codex sentence add: "Gemini's files are `/etc/gemini-cli/settings.json`, owned whole, and `/etc/gemini-cli/policies/50-agent-wrapper.toml`, one file in a directory other administrators may also use (`/Library/Application Support/GeminiCli/...` on macOS, `%ProgramData%\gemini-cli\...` on Windows). Gemini ignores the policies directory unless it is owned by root and not writable by group or others (`chmod 755`), or on Windows denies write to standard users; aw-sync creates it that way, so if a policy seems ignored, check the directory's ownership and mode first. A user can point `GEMINI_CLI_SYSTEM_SETTINGS_PATH` elsewhere, so put the rules that must hold in `policies` and treat `settings` as defaults until the launch-time `aw gemini` wrapper pins that variable." Keep the Claude and Codex sentences as they are.

- [ ] **Step 7: Verify and commit**

Run: `go test ./... && go vet ./... && gofmt -l . && GOOS=darwin go build ./... && GOOS=windows go build ./...`
Expected: all green, no output from gofmt.

```bash
git add cmd/awd/main.go cmd/awd/e2e_test.go cmd/aw-sync/main.go cmd/aw-sync/e2e_test.go examples/org-policy.yaml README.md deploy/aw-sync/README.md
git commit -m "feat(aw-sync,awd): render Gemini's system files alongside Claude and Codex; awd validates Gemini rules

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

## Self-review

**Spec coverage.** Renderers intro (relative paths, `managed` is the agent's document, repo-scoped rules dropped with a note shown by `aw-sync status`): Task 3 (`Render`, `dropped`) and Task 4 (status note assertion). Gemini section: `{settings, policies}` shape → Task 1 `Parts`; `settings` → `settings.json` with `tools.exclude`/`mcp.allowed` union, `mcpServers` by name, else replace → Task 2; `policies` → `policies/50-agent-wrapper.toml`, one `[[rule]]` each, appended across rules → Tasks 2 and 3; validation (JSON round trip, settings key allowlist, `toolName` and `decision` per rule, plus `priority` per refinement 2) → Task 1; apply-time error on the Claude path → Task 4 (`managedValidator`); the "Known weakness" paragraph → package comment (Task 3) and deploy README (Task 4). Ownership ("aw-sync owns the whole of Gemini's `settings.json`; `policies/` is a drop-in directory and only `50-agent-wrapper.toml` is touched") → refinement 6 and the two file constants. Paths → already in `internal/sync/paths.go`, checked against Gemini's docs on 2026-09-22. Failure modes "any renderer fails validation → write nothing" → existing sync behaviour, exercised by `Render` returning an error. Testing: golden files → Task 3; merge rules → Task 2; drop note → Task 3; JSON and TOML round trips → Tasks 1 and 3; e2e → Task 4.

**Placeholders.** None; every code step is complete. Task 3 Step 6 names the one place where the TOML golden may need to be regenerated from real output, with the acceptance criteria spelled out.

**Type consistency.** `managed.Parts/Validate/ForAgent/Integer/SettingsKeys/Decisions/KeysAsOf` (Task 1) used as such in Tasks 3 and 4. `gemini.New`, `gemini.Name`, `gemini.SettingsFile`, `gemini.PoliciesFile`, `gemini.DecodeTOMLForTest` defined in Task 3 and used in Task 3's tests and Task 4. `AgentMergeRules` keeps its signature. `agent.Rendering{Files, Notes}` and `agent.File{Path, Content, Mode}` match `internal/agent/agent.go`. The `managedValidator` parameter is renamed to `doc` in Task 4 Step 2 so the `managed` package import is not shadowed.
