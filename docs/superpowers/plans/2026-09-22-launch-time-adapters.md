# Launch-time Codex and Gemini adapters — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aw codex` and `aw gemini` compile the machine bundle for the repository a session runs in and apply the result per launch — Codex through `-c` config overrides from a new `launch` policy document, Gemini through a generated system settings file pinned with `GEMINI_CLI_SYSTEM_SETTINGS_PATH` — with `aw-sync` leaving the bundle in its state directory and `aw doctor` reporting all three agents.

**Architecture:** `policy.AgentConfig` and `agent.Settings` gain `Launch`; `RuleSet.Validate` admits it for Codex alone. The two adapters own their system directories (`SystemDir`, as Claude does) and `sync.AgentRoot` delegates to them. `internal/sync` plans one extra file, `<state dir>/aw-bundle.json`. `cmd/aw` resolves policy from `--policy` or that bundle plus `repo.Detect`, registers all three adapters, and appends the compiled-for note. Codex's `Build` flattens `Launch` to sorted `-c key=value` pairs; Gemini's `Build` writes settings and policies to the aw cache and pins the variable. Both adapters implement `Inspector`.

**Tech Stack:** Go 1.22; `encoding/json` for TOML-compatible string escaping and the settings file; `github.com/pelletier/go-toml/v2` (already required) for Gemini's policy file and Inspect parsing; `internal/cache`, `internal/repo`, `internal/merge` as they are.

**Spec:** `docs/superpowers/specs/2026-09-22-launch-time-adapters-design.md` — every section.

## Spec refinements

1. **The state-dir bundle is the full bundle.** The spec says "the same bytes the Claude renderer writes"; Claude's copy is *narrowed* to Claude's rules (`claude/render.go`, `narrow`). The state-dir file is the fetched bundle with every agent's rules, `json.MarshalIndent` two-space, trailing newline. The spec file is corrected in the same commit as this plan.
2. **`-c` pairs have no spaces.** The argument Codex receives is `sandbox_mode="read-only"`; the note reads `codex: -c sandbox_mode="read-only" from launch`, matching the argument rather than the spec's spaced rendering.
3. **`Launch` validation lives in `RuleSet.Validate`,** not in a new validator type: the rule is agent-name based and `policy` already names agents in `AgentMergeRules`. `awd` needs no change beyond its existing call; the e2e proves the path.
4. **Gemini's generated policy header** reads `# Written by aw for one session from policy revision <version>. Do not edit.` so a reader of the cache can tell it from the machine-wide file aw-sync owns.
5. **`aw` keeps `agent.Run`'s shape** by calling `agent.Prepare` and then `launch.Exec` itself, so it can append the compiled-for note between the two.

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run `go mod tidy`; no dependency changes.
- No `gcc`: `go test -race` cannot run. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit; `GOOS=darwin go build ./... && GOOS=windows go build ./...` for tasks touching `cmd/` or an adapter. Build only with `./...` — a single-package `go build` of a `main` package drops a binary in the working directory.
- **Rendered files are never deleted on failure**; one failed write means the cycle fails (`internal/sync`, unchanged).
- Adapters never import `internal/sync`; `internal/sync` may import adapters.
- Return `make([]T, 0)`, never a nil slice, from anything JSON-encoded as a list.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S`.
- Comment style: a comment says *why*, in full sentences, on every exported identifier and every struct field.
- Model tiering for executors: Tasks 1–3 are transcription (cheapest tier); Tasks 4–6 are multi-file integration (mid tier); the final whole-branch review is the most capable tier.

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/policy/policy.go` | `AgentConfig.Launch` |
| `internal/policy/compile.go` | merge `Launch` in `mergeAgent` |
| `internal/policy/ruleset.go` | reject `launch` for agents other than codex |
| `internal/policy/compile_test.go`, `ruleset_test.go` | merge and validation tests |
| `internal/agent/agent.go` | `Settings.Launch` |
| `cmd/aw/main.go` | `settingsFor` passes `Launch`; policy resolution; registry; doctor source; compiled-for note |
| `cmd/awd/e2e_test.go` | apply rejects `launch` on gemini |
| `internal/agent/codex/codex.go` | `SystemDir`, `Adapter.SystemDir`, `-c` overrides in `Build`, `Inspect` |
| `internal/agent/codex/overrides.go` | flatten + TOML value encoding |
| `internal/agent/codex/overrides_test.go`, `codex_test.go`, `inspect_test.go` | tests |
| `internal/agent/gemini/gemini.go` | `SystemDir`, `Adapter.SystemDir`/`CacheDir`, launch-time `Build`, `Inspect` |
| `internal/agent/gemini/render.go` | `settingsJSON`, `policiesTOML` shared by `Render` and `Build` |
| `internal/agent/gemini/gemini_test.go`, `inspect_test.go` | tests |
| `internal/sync/paths.go` | `AgentRoot` delegates to the three adapters |
| `internal/sync/sync.go` | plan `<state dir>/aw-bundle.json` |
| `internal/sync/sync_test.go` | state-dir bundle test |
| `cmd/aw/e2e_test.go` | three agents; bundle-driven doctor; no-bundle note |
| `README.md` | `aw codex`/`aw gemini`, `launch`, the Gemini tier limitation |

---

### Task 1: `Launch` in policy and agent settings

**Files:**
- Modify: `internal/policy/policy.go` (`AgentConfig`)
- Modify: `internal/policy/compile.go` (`mergeAgent`)
- Modify: `internal/policy/ruleset.go` (`Validate`)
- Modify: `internal/policy/compile_test.go`, `internal/policy/ruleset_test.go` (append tests)
- Modify: `internal/agent/agent.go` (`Settings`)
- Modify: `cmd/aw/main.go` (`settingsFor`)
- Modify: `cmd/awd/e2e_test.go` (one test)

**Interfaces:**
- Produces: `policy.AgentConfig.Launch map[string]any` (json `launch`), `agent.Settings.Launch map[string]any`. Tasks 4 and 6 consume them.

- [ ] **Step 1: Write the failing tests**

Append to `internal/policy/compile_test.go`:

```go
func TestLaunchMergesByKeyAcrossRules(t *testing.T) {
	// launch is the per-launch document for agents whose launch channel is
	// not their managed file; tables merge by key and scalars replace, so a
	// repo rule can tighten one setting without restating the baseline.
	rs := &policy.RuleSet{Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"codex": {Launch: map[string]any{
			"sandbox_mode":    "workspace-write",
			"approval_policy": "on-request",
			"mcp_servers":     map[string]any{"docs": map[string]any{"command": "codex-mcp"}},
		}}}},
		{Name: "payments", Match: policy.Match{Repos: []string{"github.com/acme/payments*"}},
			Agents: map[string]policy.AgentConfig{"codex": {Launch: map[string]any{
				"sandbox_mode": "read-only",
				"mcp_servers":  map[string]any{"jira": map[string]any{"url": "https://jira/mcp"}},
			}}}},
	}}

	launch := rs.Compile(policy.Subject{Repo: "github.com/acme/payments-api"}).Agent("codex").Launch

	if launch["sandbox_mode"] != "read-only" || launch["approval_policy"] != "on-request" {
		t.Errorf("launch = %v; want the repo rule's scalar over the baseline's and the untouched one kept", launch)
	}
	servers, _ := launch["mcp_servers"].(map[string]any)
	if _, docs := servers["docs"]; !docs {
		t.Error("mcp_servers lost docs; tables merge by key")
	}
	if _, jira := servers["jira"]; !jira {
		t.Error("mcp_servers lost jira; tables merge by key")
	}
	if outside := rs.Compile(policy.Subject{}).Agent("codex").Launch; outside["sandbox_mode"] != "workspace-write" {
		t.Errorf("outside the repo launch = %v; the repo rule must not apply", outside)
	}
}
```

Append to `internal/policy/ruleset_test.go`:

```go
func TestLaunchIsAcceptedForCodexAlone(t *testing.T) {
	// Claude and Gemini apply their managed document at launch; a launch
	// entry for them would be silently ignored, which an author must hear.
	ok := &policy.RuleSet{Version: "v1", Rules: []policy.Rule{
		{Name: "b", Agents: map[string]policy.AgentConfig{"codex": {Launch: map[string]any{"sandbox_mode": "read-only"}}}},
	}}
	if err := ok.Validate(); err != nil {
		t.Errorf("codex launch: %v; want nil", err)
	}
	for _, agentName := range []string{"claude", "gemini"} {
		bad := &policy.RuleSet{Version: "v1", Rules: []policy.Rule{
			{Name: "b", Agents: map[string]policy.AgentConfig{agentName: {Launch: map[string]any{"x": 1}}}},
		}}
		err := bad.Validate()
		if err == nil || !strings.Contains(err.Error(), `rule "b"`) || !strings.Contains(err.Error(), agentName) || !strings.Contains(err.Error(), "launch") {
			t.Errorf("%s launch: err = %v; want the rule, the agent and launch named", agentName, err)
		}
	}
}
```

If `ruleset_test.go` does not already import `strings`, add it.

In `cmd/awd/e2e_test.go`, immediately before `func TestApplyWithoutTheAdminTokenIsRefused`, insert:

```go
func TestApplyRejectsALaunchDocumentForGemini(t *testing.T) {
	// launch is Codex's per-launch channel; Gemini applies managed at
	// launch, so a launch entry for it would do nothing. The author hears
	// that at apply time, from the client and the server alike.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      gemini:\n        launch:\n          x: 1\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for launch on gemini: %s", out)
	}
	if !strings.Contains(out, "launch") || !strings.Contains(out, "gemini") {
		t.Errorf("output %q does not name launch and the agent", out)
	}

	body := `{"version":"v2","rules":[{"name":"b","agents":{"gemini":{"launch":{"x":1}}}}]}`
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

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/policy/ -run 'TestLaunch' ; go test ./cmd/awd/ -run TestApplyRejectsALaunchDocumentForGemini`
Expected: `policy` fails to compile (`unknown field Launch`); the awd test would fail with `apply exited 0` once it compiles.

- [ ] **Step 3: Add the field, the merge and the check**

In `internal/policy/policy.go`, in `AgentConfig` after `ForceEnv`, add:

```go
	// Launch is the document the agent's wrapper applies per launch, in the
	// agent's launch-time schema, for an agent whose launch channel takes a
	// different shape from its managed file. Codex's requirements.toml and
	// its config.toml are two schemas; Launch is the second. Claude and
	// Gemini apply Managed at launch and have no use for it.
	Launch map[string]any `json:"launch,omitempty"`
```

In `internal/policy/compile.go`, in `mergeAgent`, directly after the `if config.Managed != nil { ... }` block, add:

```go
	if config.Launch != nil {
		// No agent-specific union paths: the launch document is a plain
		// configuration where a later rule's value is the one that holds.
		merged, _ := merge.JSON(cloneMap(current.Launch), config.Launch, merge.Rules{}).(map[string]any)
		current.Launch = merged
	}
```

In `internal/policy/ruleset.go`, in `Validate`, inside the `for agentName, config := range rule.Agents` loop, directly after the `if agentName == "" { ... }` block, add:

```go
			if config.Launch != nil && agentName != "codex" {
				return fmt.Errorf("rule %q, agent %q: launch is not supported; %s applies managed at launch", rule.Name, agentName, agentName)
			}
```

In `internal/agent/agent.go`, in `Settings` after `ForceEnv`, add:

```go
	// Launch is the per-launch document for adapters whose launch channel
	// is not their managed file; Codex turns it into -c overrides. Adapters
	// that have no such channel ignore it.
	Launch map[string]any
```

In `cmd/aw/main.go`, `settingsFor` returns:

```go
	return agent.Settings{
		Managed:  config.Managed,
		Env:      config.Env,
		ForceEnv: config.ForceEnv,
		Launch:   config.Launch,
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/policy/ ./internal/agent/ ./cmd/awd/ ./cmd/aw/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && git add internal/policy internal/agent/agent.go cmd/aw/main.go cmd/awd/e2e_test.go
git commit -m "feat(policy): launch document per agent, merged by key and accepted for codex alone

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 2: adapters own their system directories; `sync.AgentRoot` delegates

**Files:**
- Modify: `internal/agent/codex/codex.go` (`SystemDir` func, `Adapter.SystemDir` field, `systemDir` method)
- Modify: `internal/agent/gemini/gemini.go` (same)
- Modify: `internal/sync/paths.go` (`AgentRoot`, delete `agentRoots`)
- Modify: `internal/sync/paths_test.go` (two pin tests)

**Interfaces:**
- Produces: `codex.SystemDir(goos string) string`, `gemini.SystemDir(goos string) string`, `Adapter.SystemDir string` on both. Tasks 4 and 5 use the field in `Inspect`.

- [ ] **Step 1: Append the pin tests**

Append to `internal/sync/paths_test.go` (add `codex` and `gemini` imports beside `claude`):

```go
// TestCodexAndGeminiRootsMatchTheAdapters pins sync.AgentRoot to the
// adapters' own SystemDir, as the Claude test above does: aw-sync writes
// where AgentRoot says and `aw doctor` reads where the adapter says.
func TestCodexAndGeminiRootsMatchTheAdapters(t *testing.T) {
	t.Setenv("ProgramData", "")
	for _, goos := range []string{"linux", "darwin", "windows"} {
		got, err := sync.AgentRoot(goos, "codex")
		if err != nil {
			t.Fatal(err)
		}
		if want := codex.SystemDir(goos); got != want {
			t.Errorf("AgentRoot(%s, codex) = %q, want %q", goos, got, want)
		}
		got, err = sync.AgentRoot(goos, "gemini")
		if err != nil {
			t.Fatal(err)
		}
		if want := gemini.SystemDir(goos); got != want {
			t.Errorf("AgentRoot(%s, gemini) = %q, want %q", goos, got, want)
		}
	}
}
```

Run: `go test ./internal/sync/ -run TestCodexAndGeminiRootsMatchTheAdapters`
Expected: build failure, `undefined: codex.SystemDir`.

- [ ] **Step 2: Codex and Gemini `SystemDir`**

In `internal/agent/codex/codex.go`, add `"path/filepath"` is not needed; add after `const Name`:

```go
// SystemDir is where Codex reads requirements.toml on goos: the enforced
// tier that outranks every user file. Windows keeps it under ProgramData,
// which an installation can relocate; the environment says where.
func SystemDir(goos string) string {
	switch goos {
	case "windows":
		return programData() + `\OpenAI\Codex`
	default:
		return "/etc/codex"
	}
}

// programData is Windows' machine-wide data directory.
func programData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}
```

Extend `Adapter`:

```go
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// SystemDir overrides where Inspect looks for requirements.toml; empty
	// means SystemDir(runtime.GOOS). Tests aim it at a temporary directory.
	SystemDir string
}

func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	return SystemDir(runtime.GOOS)
}
```

and add `"runtime"` to the imports.

In `internal/agent/gemini/gemini.go`, the same with Gemini's rows:

```go
// SystemDir is where Gemini reads its system settings.json and, under
// policies/, its admin policies on goos. Windows keeps it under
// ProgramData, which an installation can relocate.
func SystemDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/GeminiCli"
	case "windows":
		return programData() + `\gemini-cli`
	default:
		return "/etc/gemini-cli"
	}
}

// programData is Windows' machine-wide data directory.
func programData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}
```

with the same `SystemDir` field (comment: "overrides where Inspect looks for settings.json and the policies directory") and `systemDir()` method, and `"runtime"` imported.

- [ ] **Step 3: `sync.AgentRoot` delegates**

Replace everything in `internal/sync/paths.go` from the `agentRoots` comment through the end of `programData` with:

```go
// AgentRoot is the directory agentName's rendered files live under on goos.
// Each adapter owns its row, so the place aw-sync writes and the place the
// adapter's Inspect reads cannot drift apart. An agent without a row cannot
// be governed by files, and saying so is better than writing them somewhere
// nothing reads.
func AgentRoot(goos, agentName string) (string, error) {
	switch agentName {
	case claude.Name:
		return claude.SystemDir(goos), nil
	case codex.Name:
		return codex.SystemDir(goos), nil
	case gemini.Name:
		return gemini.SystemDir(goos), nil
	}
	return "", fmt.Errorf("sync: no system directory is known for agent %q", agentName)
}

// programData is Windows' machine-wide data directory, which an installation
// can relocate; the environment says where.
func programData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}
```

Add the three adapter imports to `paths.go`. `programData` stays because `StateDir` uses it.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/sync/ ./internal/agent/...`
Expected: PASS, including the existing `TestAgentRootTable` and `TestAgentRootHonoursProgramData`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && GOOS=darwin go build ./... && GOOS=windows go build ./... && \
git add internal/agent/codex/codex.go internal/agent/gemini/gemini.go internal/sync/paths.go internal/sync/paths_test.go
git commit -m "refactor(sync): each adapter owns its system directory; AgentRoot delegates

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 3: `aw-sync` leaves the bundle in its state directory

**Files:**
- Modify: `internal/sync/files.go` (constant)
- Modify: `internal/sync/sync.go` (plan the file)
- Modify: `internal/sync/sync_test.go` (append a test)

**Interfaces:**
- Produces: `sync.BundleFile = "aw-bundle.json"`; `<StateDir>/aw-bundle.json` written on every successful cycle. Task 6 reads it.

- [ ] **Step 1: Append the failing test**

Append to `internal/sync/sync_test.go`:

```go
func TestRunLeavesTheFullBundleInTheStateDirectory(t *testing.T) {
	// aw compiles per repository from this copy, so a machine that enrols
	// no Claude still has the bundle. It is a planned file like the rest:
	// hashed into state, so drift on it is repaired too.
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	path := filepath.Join(cfg.StateDir, sync.BundleFile)
	var written policy.Bundle
	if err := json.Unmarshal(mustRead(t, path), &written); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if written.Version != "v1" || len(written.Rules) != 1 {
		t.Errorf("bundle on disk = %+v", written)
	}
	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Files[path]; !ok {
		t.Errorf("state.Files lacks %s", path)
	}
	if len(res.Written) != 3 {
		t.Errorf("Written = %v; want the two Claude files and the state-dir bundle", res.Written)
	}
}
```

Run: `go test ./internal/sync/ -run TestRunLeavesTheFullBundleInTheStateDirectory`
Expected: build failure, `undefined: sync.BundleFile`.

- [ ] **Step 2: Plan the file**

In `internal/sync/files.go`, beside `StateFile`, add:

```go
	// BundleFile is the fetched bundle, every agent's rules with matchers
	// intact, left in the state directory for `aw` to compile per launch.
	BundleFile = "aw-bundle.json"
```

In `internal/sync/sync.go`, after the `for _, name := range machine.Agents { ... }` planning loop and before `version := fetched.Bundle.Version`, insert:

```go
	// The full bundle goes beside state.json so `aw` can compile it for the
	// repository a session runs in, whatever agents this machine enrolled.
	bundleJSON, err := json.MarshalIndent(fetched.Bundle, "", "  ")
	if err != nil {
		return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: encoding the bundle: %w", err))
	}
	planned = append(planned, plannedFile{path: filepath.Join(cfg.StateDir, BundleFile), content: append(bundleJSON, '\n'), mode: 0o644})
```

Add `"encoding/json"` to `sync.go`'s imports if it is not there.

- [ ] **Step 3: Run the package and the e2e**

Run: `go test ./internal/sync/ ./cmd/aw-sync/ -count=1`
Expected: PASS. `TestRunWritesEveryRenderedFileAndRecordsState` asserts `len(res.Written) != 2` — change that `2` to `3` and its comment if it has one; in `cmd/aw-sync/e2e_test.go` the two `len(report.Files)` assertions (`5` in the main test, `2` in the relative-root test) become `6` and `3`. Re-run until green.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./... && git add internal/sync cmd/aw-sync/e2e_test.go
git commit -m "feat(sync): leave the full bundle in the state directory for aw to compile per launch

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 4: `aw codex` — `-c` overrides and `Inspect`

**Files:**
- Create: `internal/agent/codex/overrides.go`, `internal/agent/codex/overrides_test.go`
- Modify: `internal/agent/codex/codex.go` (`Build`, `Inspect`)
- Modify: `internal/agent/codex/codex_test.go` (one test)
- Create: `internal/agent/codex/inspect_test.go`

**Interfaces:**
- Consumes: `agent.Settings.Launch` (Task 1), `Adapter.SystemDir` (Task 2).
- Produces: `overrides(launch map[string]any) ([]string, error)` (unexported; sorted `key=value` strings), `Inspect(env []string) []agent.Finding`.

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/codex/overrides_test.go`:

```go
package codex

import (
	"strings"
	"testing"
)

func TestOverridesFlattenSortAndEncodeAsTOML(t *testing.T) {
	got, err := overrides(map[string]any{
		"sandbox_mode":    "read-only",
		"approval_policy": "untrusted",
		"model_reasoning": map[string]any{"effort": "high"},
		"mcp_servers":     map[string]any{"docs": map[string]any{"command": "codex-mcp", "args": []any{"--port", float64(8080)}}},
		"features":        map[string]any{"web_search": true, "ratio": 0.5},
		"note":            `say "hi"`,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		`approval_policy="untrusted"`,
		`features.ratio=0.5`,
		`features.web_search=true`,
		`mcp_servers.docs.args=["--port", 8080]`,
		`mcp_servers.docs.command="codex-mcp"`,
		`model_reasoning.effort="high"`,
		`note="say \"hi\""`,
		`sandbox_mode="read-only"`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("overrides =\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOverridesKeepTablesInsideArraysInline(t *testing.T) {
	got, err := overrides(map[string]any{"servers": []any{map[string]any{"name": "a", "port": float64(1)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != `servers=[{name = "a", port = 1}]` {
		t.Errorf("overrides = %q", got)
	}
}

func TestOverridesRejectNull(t *testing.T) {
	_, err := overrides(map[string]any{"a": map[string]any{"b": nil}})
	if err == nil || !strings.Contains(err.Error(), "a.b") {
		t.Errorf("err = %v; want the null's path", err)
	}
}

func TestOverridesOfNothingAreNothing(t *testing.T) {
	if got, err := overrides(nil); err != nil || len(got) != 0 {
		t.Errorf("overrides(nil) = %v, %v", got, err)
	}
}
```

Append to `internal/agent/codex/codex_test.go`:

```go
func TestBuildTurnsTheLaunchDocumentIntoConfigOverrides(t *testing.T) {
	env := fakeBinary(t)

	launch, err := codex.New().Build(context.Background(), agent.BuildOptions{
		Env:  env,
		Args: []string{"--model", "gpt-5"},
		Settings: agent.Settings{Launch: map[string]any{
			"sandbox_mode":    "read-only",
			"approval_policy": "untrusted",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sorted overrides first, the developer's arguments last so an explicit
	// -c on the command line wins.
	want := `-c approval_policy="untrusted" -c sandbox_mode="read-only" --model gpt-5`
	if got := strings.Join(launch.Args, " "); got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
	notes := strings.Join(launch.Notes, "\n")
	if !strings.Contains(notes, `-c sandbox_mode="read-only" from launch`) {
		t.Errorf("notes %q should list each override", launch.Notes)
	}
}

func TestBuildWithoutALaunchDocumentInjectsNothing(t *testing.T) {
	env := fakeBinary(t)
	launch, err := codex.New().Build(context.Background(), agent.BuildOptions{Env: env, Args: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(launch.Args, " ") != "x" {
		t.Errorf("args = %q; nothing to inject", launch.Args)
	}
}
```

Create `internal/agent/codex/inspect_test.go`:

```go
package codex_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/codex"
)

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func TestInspectWarnsWhenThereIsNoRequirementsFile(t *testing.T) {
	a := codex.New()
	a.SystemDir = t.TempDir()

	findings := a.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "requirements.toml") {
		t.Errorf("findings %v should warn that Codex is ungoverned until aw-sync runs", findings)
	}
}

func TestInspectReportsTheRequirementsRevision(t *testing.T) {
	a := codex.New()
	a.SystemDir = t.TempDir()
	content := "# Managed by aw-sync from policy revision 2026-09-22.7. Do not edit: the next sync overwrites this file.\n\nallowed_sandbox_modes = ['read-only']\n"
	if err := os.WriteFile(filepath.Join(a.SystemDir, codex.RequirementsFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := a.Inspect(nil)

	if !findingsWith(findings, agent.OK, "2026-09-22.7") {
		t.Errorf("findings %v should report the revision from the header", findings)
	}
}

func TestInspectFlagsARequirementsFileThatIsNotTOML(t *testing.T) {
	a := codex.New()
	a.SystemDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(a.SystemDir, codex.RequirementsFile), []byte("= not toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := a.Inspect(nil)

	if !findingsWith(findings, agent.Error, "requirements.toml") {
		t.Errorf("findings %v should flag the unparseable file", findings)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agent/codex/`
Expected: build failure, `undefined: overrides`, `a.Inspect undefined`.

- [ ] **Step 3: The encoder**

Create `internal/agent/codex/overrides.go`:

```go
package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// overrides turns the launch document into the key=value strings Codex's
// -c flag takes, one per leaf, sorted so two launches from the same policy
// produce the same command line. Nested tables become dotted keys, which
// is how Codex addresses them; a table inside an array cannot be dotted
// and is written inline instead. Values are TOML: Codex parses the value
// as TOML and falls back to a raw string, so strings are quoted to keep
// "1.0" a string and "true" a string.
func overrides(launch map[string]any) ([]string, error) {
	leaves := make(map[string]any)
	flatten("", launch, leaves)
	keys := make([]string, 0, len(leaves))
	for k := range leaves {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		value, err := tomlValue(leaves[k])
		if err != nil {
			return nil, fmt.Errorf("codex: launch %s: %w", k, err)
		}
		out = append(out, k+"="+value)
	}
	return out, nil
}

// flatten walks tables into dotted keys and stops at anything else.
func flatten(prefix string, table map[string]any, leaves map[string]any) {
	for k, v := range table {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			flatten(key, sub, leaves)
			continue
		}
		leaves[key] = v
	}
}

// tomlValue writes one value as a TOML literal. JSON's string escaping is a
// subset of TOML's basic-string escaping, so encoding/json does the quoting.
// Numbers arrive as float64 from JSON; a whole one is written as an integer
// because Codex's integer settings reject 8080.0.
func tomlValue(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", fmt.Errorf("is null, which TOML cannot represent")
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case bool:
		return strconv.FormatBool(t), nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), nil
		}
		return strconv.FormatFloat(t, 'g', -1, 64), nil
	case []any:
		parts := make([]string, 0, len(t))
		for i, e := range t {
			s, err := tomlValue(e)
			if err != nil {
				return "", fmt.Errorf("[%d] %w", i, err)
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, err := tomlValue(t[k])
			if err != nil {
				return "", fmt.Errorf(".%s %w", k, err)
			}
			parts = append(parts, k+" = "+s)
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	default:
		return "", fmt.Errorf("has type %T, which has no TOML form", v)
	}
}
```

The null test expects the path `a.b`: `flatten` puts `a.b` in `leaves` with a nil value (a nil is not a non-empty map), so `overrides` reports `codex: launch a.b: is null…`. Good.

- [ ] **Step 4: `Build` and `Inspect`**

In `internal/agent/codex/codex.go`, replace `Build`'s body from `launch := &agent.Launch{...}` to the end with:

```go
	launch := &agent.Launch{Agent: Name, Binary: binary}

	// The launch document is the per-repository layer; requirements.toml,
	// which aw-sync wrote, bounds what any override can do. Overrides go
	// first so the developer's own -c, appended below, wins.
	pairs, err := overrides(o.Settings.Launch)
	if err != nil {
		return nil, err
	}
	for _, pair := range pairs {
		launch.Args = append(launch.Args, "-c", pair)
		launch.Notes = append(launch.Notes, "codex: -c "+pair+" from launch")
	}

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

// Inspect reports whether Codex is governed on this machine: the
// requirements file aw-sync owns is present and parseable. It runs
// nothing. env is unused; Codex has no environment variable that moves
// the file.
func (a *Adapter) Inspect(env []string) []agent.Finding {
	path := filepath.Join(a.systemDir(), RequirementsFile)
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return []agent.Finding{{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: Codex is not governed on this machine until aw-sync has run", path)}}
	case err != nil:
		return []agent.Finding{{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", path, err)}}
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return []agent.Finding{{Level: agent.Error,
			Message: fmt.Sprintf("%s is not valid TOML, so Codex ignores it: %v", path, err)}}
	}
	if version := revisionFromHeader(raw); version != "" {
		return []agent.Finding{{Level: agent.OK, Message: fmt.Sprintf("%s (policy revision %s)", path, version)}}
	}
	return []agent.Finding{{Level: agent.OK, Message: fmt.Sprintf("%s (not written by aw-sync)", path)}}
}

// revisionFromHeader reads the revision the renderer put in the first line,
// or "" when the file has no such header.
func revisionFromHeader(content []byte) string {
	const prefix = "# Managed by aw-sync from policy revision "
	first, _, _ := strings.Cut(string(content), "\n")
	if !strings.HasPrefix(first, prefix) {
		return ""
	}
	version, _, _ := strings.Cut(strings.TrimPrefix(first, prefix), ". ")
	return version
}
```

Add to the imports: `"errors"`, `"fmt"`, `"path/filepath"`, `"strings"`, and `toml "github.com/pelletier/go-toml/v2"`.

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/agent/codex/... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./... && GOOS=darwin go build ./... && GOOS=windows go build ./... && git add internal/agent/codex
git commit -m "feat(codex): launch document becomes sorted -c overrides; doctor inspects requirements.toml

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 5: `aw gemini` — generated system settings, pinned; `Inspect`

**Files:**
- Modify: `internal/agent/gemini/render.go` (extract `settingsJSON`, `policiesTOML`)
- Modify: `internal/agent/gemini/gemini.go` (`Adapter.CacheDir`, `Build`, `Inspect`)
- Modify: `internal/agent/gemini/gemini_test.go` (replace the Build test; add launch tests)
- Create: `internal/agent/gemini/inspect_test.go`

**Interfaces:**
- Consumes: `managed.Parts`, `managed.Validate` (M4e), `cache.Dir/Write/Prune`, `merge.Env`.
- Produces: `settingsJSON(settings map[string]any) ([]byte, error)`, `policiesTOML(header string, policies []any) ([]byte, error)` (unexported), `const SystemSettingsEnv = "GEMINI_CLI_SYSTEM_SETTINGS_PATH"`, `Adapter.CacheDir string`, `Inspect`.

- [ ] **Step 1: Write the failing tests**

In `internal/agent/gemini/gemini_test.go`, replace `TestBuildPassesArgumentsThroughAndMergesTheEnvironment` with:

```go
// newAdapter is an adapter whose cache is a temporary directory.
func newAdapter(t *testing.T) *gemini.Adapter {
	t.Helper()
	a := gemini.New()
	a.CacheDir = t.TempDir()
	return a
}

func envValue(env []string, name string) string {
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			return strings.TrimPrefix(e, name+"=")
		}
	}
	return ""
}

func TestBuildWritesTheCompiledSettingsAndPinsThePath(t *testing.T) {
	env := fakeBinary(t)

	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:  env,
		Args: []string{"--model", "gemini-2.5-pro"},
		Settings: agent.Settings{
			Managed: map[string]any{
				"settings": map[string]any{"admin": map[string]any{"secureModeEnabled": true}},
				"policies": []any{map[string]any{"toolName": "run_shell_command", "decision": "deny", "priority": float64(100)}},
			},
			Env: map[string]string{"GOOGLE_GEMINI_BASE_URL": "https://proxy.acme"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if launch.Agent != "gemini" || !strings.HasSuffix(launch.Binary, "/gemini") {
		t.Errorf("launch = %+v", launch)
	}
	if strings.Join(launch.Args, " ") != "--model gemini-2.5-pro" {
		t.Errorf("args = %q; Gemini takes no settings flag, so the arguments pass through", launch.Args)
	}
	if envValue(launch.Env, "GOOGLE_GEMINI_BASE_URL") != "https://proxy.acme" {
		t.Errorf("env %q lacks the policy's variable", launch.Env)
	}

	settingsPath := envValue(launch.Env, gemini.SystemSettingsEnv)
	if settingsPath == "" {
		t.Fatalf("env %q does not pin %s", launch.Env, gemini.SystemSettingsEnv)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("%s: %v", settingsPath, err)
	}
	if admin, _ := settings["admin"].(map[string]any); admin["secureModeEnabled"] != true {
		t.Errorf("settings = %v; the compiled settings were not written", settings)
	}
	paths, _ := settings["policyPaths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("policyPaths = %v; want the generated policy file", paths)
	}
	policies, err := os.ReadFile(paths[0].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(policies), "# Written by aw for one session") || !strings.Contains(string(policies), "priority = 100\n") {
		t.Errorf("policy file =\n%s", policies)
	}
	if len(launch.Files) != 2 {
		t.Errorf("Files = %v; want the settings and policy files", launch.Files)
	}
}

func TestBuildWithoutPoliciesWritesSettingsAlone(t *testing.T) {
	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:      fakeBinary(t),
		Settings: agent.Settings{Managed: map[string]any{"settings": map[string]any{"general": map[string]any{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(envValue(launch.Env, gemini.SystemSettingsEnv))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "policyPaths") || len(launch.Files) != 1 {
		t.Errorf("settings = %s, files = %v; no policies means no policy file and no policyPaths", raw, launch.Files)
	}
}

func TestBuildWithNoManagedDocumentStillPinsAnEmptySettingsFile(t *testing.T) {
	// A launch outside any policy must still pin the variable: a developer's
	// own GEMINI_CLI_SYSTEM_SETTINGS_PATH would otherwise stand.
	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{Env: fakeBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(envValue(launch.Env, gemini.SystemSettingsEnv))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{}\n" {
		t.Errorf("settings = %q, want an empty object", raw)
	}
}

func TestBuildReplacesTheDevelopersOwnSettingsPathAndSaysSo(t *testing.T) {
	env := append(fakeBinary(t), gemini.SystemSettingsEnv+"=/home/dev/mine.json")

	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}

	if got := envValue(launch.Env, gemini.SystemSettingsEnv); got == "/home/dev/mine.json" || got == "" {
		t.Errorf("%s = %q; the pin must replace the developer's value", gemini.SystemSettingsEnv, got)
	}
	if !strings.Contains(strings.Join(launch.Notes, "\n"), "/home/dev/mine.json") {
		t.Errorf("notes %q should say what was replaced", launch.Notes)
	}
	count := 0
	for _, e := range launch.Env {
		if strings.HasPrefix(e, gemini.SystemSettingsEnv+"=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("env has %d copies of %s; want one", count, gemini.SystemSettingsEnv)
	}
}

func TestBuildRejectsAManagedDocumentThatFailsValidation(t *testing.T) {
	_, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:      fakeBinary(t),
		Settings: agent.Settings{Managed: map[string]any{"settings": map[string]any{"toolz": 1}}},
	})
	if err == nil || !strings.Contains(err.Error(), `"toolz"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}
```

Add `"encoding/json"` to that file's imports.

Create `internal/agent/gemini/inspect_test.go`:

```go
package gemini_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
)

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func inspected(t *testing.T) *gemini.Adapter {
	t.Helper()
	a := gemini.New()
	a.SystemDir = t.TempDir()
	return a
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectWarnsWhenNeitherFileExists(t *testing.T) {
	findings := inspected(t).Inspect([]string{"PATH=/nowhere"})

	if !findingsWith(findings, agent.Warn, "settings.json") || !findingsWith(findings, agent.Warn, "50-agent-wrapper.toml") {
		t.Errorf("findings %v should warn about both missing files", findings)
	}
}

func TestInspectReportsBothFilesWithTheirRevisions(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, gemini.SettingsFile), "{\n  \"admin\": {}\n}\n")
	write(t, filepath.Join(a.SystemDir, gemini.SettingsFile+".aw-revision"), "2026-09-22.7\n")
	write(t, filepath.Join(a.SystemDir, gemini.PoliciesFile), "# Managed by aw-sync from policy revision 2026-09-22.7. Do not edit: the next sync overwrites this file.\n\n")

	findings := a.Inspect([]string{"PATH=/nowhere"})

	ok := 0
	for _, f := range findings {
		if f.Level == agent.OK && strings.Contains(f.Message, "2026-09-22.7") {
			ok++
		}
	}
	if ok != 2 {
		t.Errorf("findings %v should report both files at revision 2026-09-22.7", findings)
	}
}

func TestInspectFlagsUnparseableFiles(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, gemini.SettingsFile), "{not json")
	write(t, filepath.Join(a.SystemDir, gemini.PoliciesFile), "= not toml")

	findings := a.Inspect([]string{"PATH=/nowhere"})

	if !findingsWith(findings, agent.Error, "settings.json") || !findingsWith(findings, agent.Error, "50-agent-wrapper.toml") {
		t.Errorf("findings %v should flag both files", findings)
	}
}

func TestInspectWarnsWhenTheDeveloperMovedTheSystemSettings(t *testing.T) {
	findings := inspected(t).Inspect([]string{gemini.SystemSettingsEnv + "=/home/dev/mine.json"})

	if !findingsWith(findings, agent.Warn, gemini.SystemSettingsEnv) {
		t.Errorf("findings %v should warn that bare gemini reads the developer's file", findings)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agent/gemini/`
Expected: build failure, `undefined: gemini.SystemSettingsEnv`, `a.CacheDir undefined`.

- [ ] **Step 3: Share the encoders in `render.go`**

In `internal/agent/gemini/render.go`, replace `Render`'s body after `settings, policies, err := managed.Parts(doc)` error check through the `return agent.Rendering{...}` with:

```go
	settingsBytes, err := settingsJSON(settings)
	if err != nil {
		return agent.Rendering{}, err
	}
	policiesBytes, err := policiesTOML(header(bundle), policies)
	if err != nil {
		return agent.Rendering{}, err
	}
	return agent.Rendering{
		Files: []agent.File{
			{Path: SettingsFile, Content: settingsBytes, Mode: 0o644},
			{Path: PoliciesFile, Content: policiesBytes, Mode: 0o644},
		},
		Notes: dropped(bundle),
	}, nil
}

// settingsJSON encodes the settings object as Gemini reads it: indented,
// keys sorted by the encoder, trailing newline, {} for nothing.
func settingsJSON(settings map[string]any) ([]byte, error) {
	if settings == nil {
		settings = map[string]any{}
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("gemini: encoding settings.json: %w", err)
	}
	return append(encoded, '\n'), nil
}

// policiesTOML encodes the policy list as one [[rule]] table per entry
// under the given header; the header alone when there are none.
func policiesTOML(header string, policies []any) ([]byte, error) {
	var rules []byte
	if len(policies) > 0 {
		var err error
		rules, err = toml.Marshal(map[string]any{"rule": integerPriorities(policies)})
		if err != nil {
			return nil, fmt.Errorf("gemini: encoding policies: %w", err)
		}
	}
	return append([]byte(header), rules...), nil
}
```

Run `go test ./internal/agent/gemini/ -run TestRender` — the render tests, including the goldens, must still pass byte for byte.

- [ ] **Step 4: `Build` and `Inspect` in `gemini.go`**

Replace `Adapter` and everything from `Build` to the end of `gemini.go` with:

```go
// SystemSettingsEnv is the variable Gemini reads its system settings path
// from. A developer can export it to move the system tier; the wrapper
// exports it to point the system tier at the settings compiled for this
// session, which is what Gemini's own enterprise guidance recommends.
const SystemSettingsEnv = "GEMINI_CLI_SYSTEM_SETTINGS_PATH"

// launchHeader is the first line of a policy file written for one session,
// distinct from the machine-wide file's header so a reader of the cache
// knows which is which.
const launchHeader = "# Written by aw for one session from policy revision %s. Do not edit.\n\n"

// pruneAfter is how long a generated file survives unused.
const pruneAfter = 30 * 24 * time.Hour

// Adapter launches Gemini and renders its system files. The exported fields
// exist so tests can control the binary and the directories.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// SystemDir overrides where Inspect looks for settings.json and the
	// policies directory; empty means SystemDir(runtime.GOOS).
	SystemDir string
	// CacheDir overrides where Build writes the session's settings; empty
	// means the user cache directory for this agent.
	CacheDir string
}

func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	return SystemDir(runtime.GOOS)
}

func (a *Adapter) cacheDir() (string, error) {
	if a.CacheDir != "" {
		return a.CacheDir, nil
	}
	return cache.Dir(Name)
}

// Build computes the launch. Gemini takes no settings flag, so the compiled
// managed document is written to the cache as a system settings file, with
// any policy rules beside it and named in policyPaths, and the system
// settings variable is pinned to it. The pin replaces whatever the
// developer exported: the system tier is the organization's, and pinning
// it is the point of the wrapper. A policy that sets the same variable in
// env loses to the pin too, with a note.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}
	launch := &agent.Launch{Agent: Name, Binary: binary}

	if err := managed.Validate(o.Settings.Managed); err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	settings, policies, err := managed.Parts(o.Settings.Managed)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	dir, err := a.cacheDir()
	if err != nil {
		return nil, err
	}
	if err := cache.Prune(dir, pruneAfter); err != nil {
		launch.Notes = append(launch.Notes, "gemini: could not prune old session files: "+err.Error())
	}

	if len(policies) > 0 {
		version := "unversioned"
		if v, _ := o.Settings.Managed["version"].(string); v != "" {
			version = v
		}
		policyBytes, err := policiesTOML(fmt.Sprintf(launchHeader, version), policies)
		if err != nil {
			return nil, err
		}
		policyPath, err := cache.Write(dir, "policies", ".toml", policyBytes)
		if err != nil {
			return nil, err
		}
		launch.Files = append(launch.Files, policyPath)
		settings = withPolicyPath(settings, policyPath)
	}
	settingsBytes, err := settingsJSON(settings)
	if err != nil {
		return nil, err
	}
	settingsPath, err := cache.Write(dir, "settings", ".json", settingsBytes)
	if err != nil {
		return nil, err
	}
	launch.Files = append(launch.Files, settingsPath)

	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch.Notes = append(launch.Notes, notes...)
	env, note := pin(env, SystemSettingsEnv, settingsPath)
	if note != "" {
		launch.Notes = append(launch.Notes, note)
	}
	launch.Env = env
	launch.Notes = append(launch.Notes, "gemini: system settings for this session written to "+settingsPath)
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}

// withPolicyPath returns settings with path appended to policyPaths, the
// author's own entries first. settings is not modified: it is the compiled
// document, which doctor may print afterwards.
func withPolicyPath(settings map[string]any, path string) map[string]any {
	out := make(map[string]any, len(settings)+1)
	for k, v := range settings {
		out[k] = v
	}
	existing, _ := out["policyPaths"].([]any)
	paths := make([]any, 0, len(existing)+1)
	paths = append(paths, existing...)
	out["policyPaths"] = append(paths, path)
	return out
}

// pin sets name=value in env, replacing any earlier value and saying so.
func pin(env []string, name, value string) ([]string, string) {
	out := make([]string, 0, len(env)+1)
	var note string
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			note = fmt.Sprintf("gemini: %s was %s; replaced with the organization's settings for this session", name, strings.TrimPrefix(e, name+"="))
			continue
		}
		out = append(out, e)
	}
	return append(out, name+"="+value), note
}

// Inspect reports whether Gemini is governed on this machine: the system
// settings file and aw-sync's admin policy file are present and parseable,
// and the developer has not moved the system settings path, which bare
// `gemini` would honour. It runs nothing.
func (a *Adapter) Inspect(env []string) []agent.Finding {
	if env == nil {
		env = os.Environ()
	}
	findings := make([]agent.Finding, 0, 3)
	dir := a.systemDir()

	settingsPath := filepath.Join(dir, SettingsFile)
	raw, err := os.ReadFile(settingsPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: Gemini's system settings are not governed until aw-sync has run", settingsPath)})
	case err != nil:
		findings = append(findings, agent.Finding{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", settingsPath, err)})
	default:
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			findings = append(findings, agent.Finding{Level: agent.Error,
				Message: fmt.Sprintf("%s is not valid JSON, so Gemini ignores it: %v", settingsPath, err)})
		} else {
			revision, _ := os.ReadFile(settingsPath + ".aw-revision")
			findings = append(findings, agent.Finding{Level: agent.OK,
				Message: fmt.Sprintf("%s (policy revision %s)", settingsPath, orUnknown(strings.TrimSpace(string(revision))))})
		}
	}

	policiesPath := filepath.Join(dir, PoliciesFile)
	raw, err = os.ReadFile(policiesPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: Gemini's admin policies are not governed until aw-sync has run", policiesPath)})
	case err != nil:
		findings = append(findings, agent.Finding{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", policiesPath, err)})
	default:
		var doc map[string]any
		if err := toml.Unmarshal(raw, &doc); err != nil {
			findings = append(findings, agent.Finding{Level: agent.Error,
				Message: fmt.Sprintf("%s is not valid TOML, so Gemini ignores it: %v", policiesPath, err)})
		} else {
			findings = append(findings, agent.Finding{Level: agent.OK,
				Message: fmt.Sprintf("%s (policy revision %s)", policiesPath, orUnknown(revisionFromHeader(raw)))})
		}
	}

	for _, e := range env {
		if strings.HasPrefix(e, SystemSettingsEnv+"=") {
			findings = append(findings, agent.Finding{Level: agent.Warn,
				Message: fmt.Sprintf("%s is set to %s: bare `gemini` reads that file instead of the system settings (aw gemini pins its own)",
					SystemSettingsEnv, strings.TrimPrefix(e, SystemSettingsEnv+"="))})
		}
	}
	return findings
}

// revisionFromHeader reads the revision the renderer put in the first line,
// or "" when the file has no such header.
func revisionFromHeader(content []byte) string {
	const prefix = "# Managed by aw-sync from policy revision "
	first, _, _ := strings.Cut(string(content), "\n")
	if !strings.HasPrefix(first, prefix) {
		return ""
	}
	version, _, _ := strings.Cut(strings.TrimPrefix(first, prefix), ". ")
	return version
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
```

Note on `version` in `Build`: `agent.Settings.Managed` is the agent's own document and carries no version; the lookup of `Managed["version"]` therefore always yields `unversioned`. Remove that lookup and use the literal `"unversioned"` — the session file's header identifies the session, not the revision, and `aw doctor` shows the revision on its policy line (Task 6). So the block reads:

```go
		policyBytes, err := policiesTOML(fmt.Sprintf(launchHeader, "unversioned"), policies)
```

Imports for `gemini.go`: `"context"`, `"encoding/json"`, `"errors"`, `"fmt"`, `"os"`, `"path/filepath"`, `"runtime"`, `"strings"`, `"time"`, `toml "github.com/pelletier/go-toml/v2"`, `internal/agent`, `internal/agent/gemini/managed`, `internal/cache`, `internal/merge`. Keep the `SystemDir`/`programData` functions from Task 2.

`TestBuildWithNoManagedDocumentStillPinsAnEmptySettingsFile` passes because `managed.Validate(nil)` and `managed.Parts(nil)` both accept nil and `settingsJSON(nil)` writes `{}`.

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/agent/gemini/... -count=1`
Expected: PASS, goldens unchanged.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./... && GOOS=darwin go build ./... && GOOS=windows go build ./... && git add internal/agent/gemini
git commit -m "feat(gemini): aw gemini writes the session's system settings and pins GEMINI_CLI_SYSTEM_SETTINGS_PATH; doctor inspects both files

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 6: `aw` compiles from the bundle, registers three agents, reports the source; docs

**Files:**
- Modify: `cmd/aw/main.go` (`run`, `launch`, `doctor`, `printReport`, `report`, new `resolvePolicy`, `stateDirFor`)
- Modify: `cmd/aw/e2e_test.go` (three tests)
- Modify: `README.md`

**Interfaces:**
- Consumes: `sync.BundleFile` (Task 3), `codex.New`, `gemini.New`, `repo.Detect`, `policy.Bundle.Compile`.

- [ ] **Step 1: Write the failing e2e tests**

Append to `cmd/aw/e2e_test.go` (add `"os/exec"` is already imported; add `"encoding/json"` if missing):

```go
// gitRepo makes dir a repository whose origin is url, so repo.Detect finds it.
func gitRepo(t *testing.T, dir, url string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", url}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

const e2eBundle = `{
  "version": "2026-09-22.e2e",
  "groups": ["platform"],
  "rules": [
    {"name": "baseline", "agents": {
      "claude": {"managed": {"model": "sonnet"}},
      "codex": {"launch": {"sandbox_mode": "workspace-write"}},
      "gemini": {"managed": {"settings": {"admin": {"secureModeEnabled": true}}}}
    }},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {
      "codex": {"launch": {"sandbox_mode": "read-only"}},
      "gemini": {"managed": {"policies": [{"toolName": "run_shell_command", "decision": "deny", "priority": 100}]}}
    }}
  ]
}`

func TestDoctorCompilesTheBundleForTheRepositoryItRunsIn(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	fakeAgent(t, binDir, "codex", "exit 0")
	fakeAgent(t, binDir, "gemini", "exit 0")
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "aw-bundle.json"), []byte(e2eBundle), 0o644); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	gitRepo(t, work, "https://github.com/acme/payments-api.git")

	cmd := exec.Command(awBinary(t), "doctor", "--json")
	cmd.Dir = work
	cmd.Env = append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}

	var report struct {
		Policy string `json:"policy"`
		Agents []struct {
			Name   string `json:"name"`
			Launch struct {
				Args  []string `json:"args"`
				Notes []string `json:"notes"`
			} `json:"launch"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	if !strings.Contains(report.Policy, "2026-09-22.e2e") || !strings.Contains(report.Policy, "github.com/acme/payments-api") {
		t.Errorf("policy = %q; want the bundle version and the detected repository", report.Policy)
	}
	names := make([]string, 0, 3)
	for _, a := range report.Agents {
		names = append(names, a.Name)
		switch a.Name {
		case "codex":
			if args := strings.Join(a.Launch.Args, " "); args != `-c sandbox_mode="read-only"` {
				t.Errorf("codex args = %q; want the payments rule's override", args)
			}
		}
		if !strings.Contains(strings.Join(a.Launch.Notes, "\n"), "policy compiled for github.com/acme/payments-api") {
			t.Errorf("%s notes %q lack the compiled-for note", a.Name, a.Launch.Notes)
		}
	}
	if strings.Join(names, ",") != "claude,codex,gemini" {
		t.Errorf("agents = %v", names)
	}
}

func TestDoctorSaysWhenThereIsNoBundle(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")

	out, code := run(t, append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+t.TempDir()), "doctor")

	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
	if !strings.Contains(out, "policy:  none") || !strings.Contains(out, "aw-sync has not written") {
		t.Errorf("output should report no policy and why:\n%s", out)
	}
}

func TestAnUnreadableBundleRefusesToLaunch(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "aw-bundle.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir), "claude")

	if code == 0 || !strings.Contains(out, "aw-bundle.json") {
		t.Errorf("exit %d, output %q; a bundle that was meant to apply and cannot be read must refuse the launch", code, out)
	}
}
```

Also update `TestAgentsListsWhatTheBinaryCanLaunch` to require all three names:

```go
	for _, name := range []string{"claude", "codex", "gemini"} {
		if !strings.Contains(out, name) {
			t.Errorf("output %q does not list %s", out, name)
		}
	}
```

Run: `go test ./cmd/aw/ -run 'TestDoctorCompiles|TestDoctorSays|TestAnUnreadable|TestAgentsLists'`
Expected: FAIL on all four.

- [ ] **Step 2: Resolve the policy from the bundle**

In `cmd/aw/main.go`:

Imports: add `"path/filepath"`, `internal/agent/codex`, `internal/agent/gemini`, `internal/repo`.

In `run`, replace the registry block with:

```go
	registry := &agent.Registry{}
	for _, a := range []agent.Adapter{claude.New(), codex.New(), gemini.New()} {
		if err := registry.Register(a); err != nil {
			return err
		}
	}
```

Replace `loadPolicy` with:

```go
// policySource says where the policy a launch applies came from, for
// doctor and for the compiled-for note on every launch.
type policySource struct {
	// Path is the document or bundle file, empty when there was none.
	Path string
	// Kind is "document", "bundle" or "none".
	Kind string
	// Version is the bundle's revision, bundle only.
	Version string
	// Repo is the repository the bundle was compiled for, bundle only;
	// empty means none was detected.
	Repo string
	// Note explains a missing policy.
	Note string
}

// String is doctor's one-line rendering.
func (s policySource) String() string {
	switch s.Kind {
	case "document":
		return s.Path + " (document)"
	case "bundle":
		return fmt.Sprintf("%s (bundle %s, repo %s)", s.Path, orNone(s.Version), orNone(s.Repo))
	}
	return "none"
}

// resolvePolicy finds the policy for a session run in workDir: an explicit
// document wins; otherwise aw-sync's bundle, compiled for the repository
// workDir is in. A policy that was named or written but cannot be read is
// an error: launching without the organization's configuration when one
// was meant to apply would be worse than refusing. No bundle at all is not
// an error, only a note, so a machine aw-sync has not reached still runs.
func resolvePolicy(opts options, workDir string) (*policy.Document, policySource, error) {
	if opts.policyPath != "" {
		doc, err := policy.Load(opts.policyPath)
		if err != nil {
			return nil, policySource{}, err
		}
		return doc, policySource{Path: opts.policyPath, Kind: "document"}, nil
	}
	path := filepath.Join(stateDirFor(), sync.BundleFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, policySource{Kind: "none", Note: "no policy: aw-sync has not written " + path}, nil
	}
	if err != nil {
		return nil, policySource{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, policySource{}, fmt.Errorf("%s is not a valid bundle: %w", path, err)
	}
	repoURL, err := repo.Detect(workDir)
	if err != nil {
		return nil, policySource{}, fmt.Errorf("detecting the repository of %s: %w", workDir, err)
	}
	return bundle.Compile(repoURL), policySource{Path: path, Kind: "bundle", Version: bundle.Version, Repo: repoURL}, nil
}

// stateDirFor is aw-sync's state directory: the OS default, or
// AW_SYNC_STATE_DIR for tests and unusual installs.
func stateDirFor() string {
	if dir := os.Getenv("AW_SYNC_STATE_DIR"); dir != "" {
		return dir
	}
	return sync.StateDir(runtime.GOOS)
}

// compiledFor is the note every launch carries about its policy.
func compiledFor(src policySource) string {
	if src.Kind != "bundle" {
		return ""
	}
	if src.Repo == "" {
		return "policy compiled for no repository"
	}
	return "policy compiled for " + src.Repo
}
```

Add `"errors"` to the imports.

Replace `launch` with:

```go
func launch(registry *agent.Registry, opts options, agentName string, args []string) error {
	// Lookup first so an unknown agent reports the agents this binary knows,
	// rather than whatever the policy happens to be missing.
	if _, err := registry.Lookup(agentName); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	doc, src, err := resolvePolicy(opts, cwd)
	if err != nil {
		return err
	}
	prepared, err := agent.Prepare(context.Background(), registry, agent.Options{
		Agent:    agentName,
		Args:     args,
		Settings: settingsFor(doc, agentName),
	})
	if err != nil {
		return err
	}
	if note := compiledFor(src); note != "" {
		prepared.Notes = append(prepared.Notes, note)
	}
	if src.Note != "" {
		prepared.Notes = append(prepared.Notes, src.Note)
	}
	return prepared.Exec(agent.ExecOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
}
```

In `doctor`, replace

```go
	doc, err := loadPolicy(opts)
	if err != nil {
		return err
	}

	stateDir := os.Getenv("AW_SYNC_STATE_DIR")
	if stateDir == "" {
		stateDir = sync.StateDir(runtime.GOOS)
	}
	out := report{Wrapper: "aw", Policy: opts.policyPath, SyncStateDir: stateDir}
```

with

```go
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	doc, src, err := resolvePolicy(opts, cwd)
	if err != nil {
		return err
	}

	stateDir := stateDirFor()
	out := report{Wrapper: "aw", Policy: src.String(), PolicyNote: src.Note, SyncStateDir: stateDir}
```

and after `status.Launch = launch` add:

```go
		if note := compiledFor(src); note != "" {
			launch.Notes = append(launch.Notes, note)
		}
```

In `report`, change `Policy string `json:"policy,omitempty"`` to `Policy string `json:"policy"`` and add after it:

```go
	// PolicyNote says why there is no policy, when there is none.
	PolicyNote string `json:"policyNote,omitempty"`
```

In `printReport`, replace the `if r.Policy != "" { ... }` block with:

```go
	fmt.Printf("policy:  %s\n", r.Policy)
	if r.PolicyNote != "" {
		fmt.Printf("  %s\n", r.PolicyNote)
	}
```

Delete the old `loadPolicy`.

- [ ] **Step 3: Run the aw tests**

Run: `go test ./cmd/aw/ -count=1 -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`
Expected: `ok`. `TestDoctorReportsTheComputedLaunchWithoutRunningTheAgent` and `TestDoctorJSONIsMachineReadable` run without a bundle and must still pass; if either asserts on the absence of a `policy` line, adjust it to expect `policy:  none`.

- [ ] **Step 4: README**

In `README.md`:

- Usage/overview where `aw claude` is described (the paragraph under "How policy reaches the agent" or the Try-it "Inspect what the wrapper would run" section — whichever lists `aw claude`), add a paragraph:

```markdown
`aw codex` and `aw gemini` apply the rules scoped to the repository you are
in, which the machine-wide files cannot carry. `aw` reads the bundle
`aw-sync` leaves in its state directory, compiles it for the repository
`.git/config` names, and hands each agent the result through its own
launch-time channel: Codex gets `-c key=value` overrides from the rule's
`launch` document (its `config.toml` schema; `managed` stays
`requirements.toml`), and Gemini gets a settings file generated for the
session with `GEMINI_CLI_SYSTEM_SETTINGS_PATH` pinned to it, repo-scoped
`policies` included via `policyPaths`. This layer is advisory: bare
`codex` or `gemini` sees the machine-wide files alone. `aw doctor` shows
the compiled result per agent and which repository it was compiled for.

One Gemini limitation: rules delivered through `policyPaths` load at
Gemini's user tier, below the admin tier where `aw-sync`'s machine-wide
policy file lives, and Gemini ignores admin-tier supplements once that
directory has any file. A machine-wide rule that names a tool therefore
outranks a repo-scoped rule for the same tool. Keep machine-wide
`policies` to what must hold everywhere and let repo rules tighten the
rest.
```

- In "Layout", the `cmd/aw/` line becomes `cmd/aw/           wrapper CLI: run an agent with the bundle compiled for its repository, doctor, agents`.

- In `examples/org-policy.yaml`, inside the `payments`-style repo-scoped rule if one exists (otherwise the `platform-team` rule), add a `codex:` entry with `launch: {sandbox_mode: read-only}` and a comment: `# launch is Codex's per-launch config.toml layer, applied by aw codex; managed is requirements.toml, applied machine-wide by aw-sync.` Then `go test ./internal/policy/` must still pass (it loads the example).

- [ ] **Step 5: Verify and commit**

Run: `go test ./... -count=1 && go vet ./... && gofmt -l . && GOOS=darwin go build ./... && GOOS=windows go build ./...`
Expected: all green, no output from gofmt.

```bash
git add cmd/aw/main.go cmd/aw/e2e_test.go README.md examples/org-policy.yaml
git commit -m "feat(aw): compile the machine bundle per repository; launch codex and gemini; doctor names the policy source

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

## Self-review

**Spec coverage.** "What `aw` compiles from": state-dir bundle → Task 3; resolution order, errors, note, doctor line → Task 6 (`resolvePolicy`, `policySource`, tests). "Policy: the `launch` document": field, merge, validation → Task 1 (with the awd e2e). "`aw codex`": flatten/encode/sort/notes/args order → Task 4. "`aw gemini`": Parts/Validate, cache files, `policyPaths`, pin with note, `Launch.Files`; compiled-for note owned by `aw` → Tasks 5 and 6. Tier limitation → Task 6 README. "Registry and doctor": three adapters, both `Inspect`s, adapters own `SystemDir` and `AgentRoot` delegates → Tasks 2, 4, 5, 6. "Failure modes": each row has a test — sync write failure is existing behaviour; unreadable bundle → `TestAnUnreadableBundleRefusesToLaunch`; no bundle → `TestDoctorSaysWhenThereIsNoBundle`; null in launch → `TestOverridesRejectNull`; gemini validation → `TestBuildRejectsAManagedDocumentThatFailsValidation`; awd 422 → `TestApplyRejectsALaunchDocumentForGemini`. Cache-unwritable is the `cache.Write` error path, returned as-is.

**Placeholders.** None. Task 5 Step 4 corrects its own `version` lookup inline rather than leaving two versions — executors use the literal `"unversioned"` line.

**Type consistency.** `agent.Settings.Launch` (Task 1) is read by `codex.Build` (Task 4) and set by `settingsFor` (Task 1). `codex.SystemDir`/`gemini.SystemDir` (Task 2) are what `sync.AgentRoot` (Task 2) and both `Inspect`s (Tasks 4, 5) use. `sync.BundleFile` (Task 3) is what `resolvePolicy` (Task 6) joins. `settingsJSON`/`policiesTOML` (Task 5) serve both `Render` and `Build`. `gemini.SystemSettingsEnv` is used in Task 5's `Build`, `Inspect` and tests. `policySource.String()` is what the e2e's `report.Policy` assertion parses.
