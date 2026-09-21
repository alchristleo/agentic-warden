# M4b: aw-sync and the Claude renderer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A machine can enroll with `aw-sync enroll`, and `aw-sync once` fetches its bundle from `awd` and atomically renders Claude Code's `aw-bundle.json` plus the static `policyHelper` drop-in into the system directory, never deleting files on failure; `aw-sync status` reports the last sync, drift and errors; timer units ship for systemd, launchd and Task Scheduler.

**Architecture:** `agent.Renderer` is a new optional adapter interface (like `Inspector`) returning relative files that validate themselves. `internal/sync` owns the cycle: load `machine.json`, conditional `GET /v1/bundle`, render every enrolled agent, write all-or-nothing with `cache.ReplaceMode`, record `state.json` and an audit line. `cmd/aw-sync` is a thin CLI over it. The milestone-3 `aw-policy` keeps fetching from the network until M4c, so nothing existing breaks.

**Tech Stack:** Go 1.22, stdlib only (`net/http`, `encoding/json`, `flag`, `crypto/sha256`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` — sections "aw-sync", "Renderers" (Claude only), "Failure modes", "Testing" (renderers, sync, end to end), "Sequencing (M4b)".

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run bare `go mod tidy` (it pulls test deps needing Go 1.25); no new dependencies are needed for this plan.
- No `gcc` on this machine: `go test -race` cannot run here. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit. Also `GOOS=darwin go build ./... && GOOS=windows go build ./...` for any task touching `cmd/` or `internal/sync`.
- File-permission assertions skip on Windows: `if runtime.GOOS == "windows" { t.Skip("POSIX modes") }`.
- **Rendered files are never deleted on failure.** No code path in `internal/sync` calls `os.Remove` on a rendered file.
- One failed renderer means nothing is written this cycle, for any agent.
- Renderer file paths are relative to a root the caller supplies; renderers never touch the filesystem.
- `machine.json` is mode `0600`; `state.json`, rendered Claude files and their `.aw-revision` siblings are `0644`.
- Sentinel errors: `model.ErrUnauthorized` for a 401 from the control plane; `sync.ErrNotEnrolled` for a missing `machine.json`.
- Return `make([]T, 0)`, never a nil slice, from anything that is JSON-encoded as a list.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv`.
- Match the codebase's comment style: a comment says *why*, in full sentences, and is present on every exported identifier.
- Model tiering for executors: Tasks 1, 3, 8 are transcription (cheapest tier); Tasks 2, 4, 6 are small integration (mid tier); Tasks 5 and 7 are multi-file integration (mid tier); the final whole-branch review is the most capable tier.

---

## Spec refinements made by this plan

These are decisions the spec left open or stated as shorthand. They bind every task.

1. **Renderer signature.** The spec writes `Render(bundle) ([]agent.File, error)` and separately requires renderers to return notes. This plan defines `Render(bundle *policy.Bundle) (agent.Rendering, error)` with `Rendering{Files []File; Notes []string}`, so notes travel with the files without a third return value.
2. **Validation lives in `Render`.** The spec's step 4 ("validate every file before writing any") is met by the contract that a renderer validates its own output and returns an error instead of files. The sync loop renders every agent first and writes only when every renderer succeeded.
3. **`cache.Replace` stays private (0600/0700).** A new `cache.ReplaceMode(path, data, mode)` writes world-readable system files; `Replace` becomes `ReplaceMode(path, data, 0o600)`.
4. **The Claude bundle is narrowed, not just filtered.** Rules that mention `claude` are kept with their `match` intact, and each kept rule carries only its `claude` agent entry. This is not compilation: nothing is merged and no matcher is evaluated.
5. **`.aw-revision` siblings** are written by the sync loop, not by renderers, for every rendered file whose path ends in `.json`: `<path>.aw-revision` containing `<version>\n` (or `none\n` for an empty version), mode 0644. They are not tracked in `state.json`.
6. **304 updates `syncedAt`.** A 304 is a successful cycle: `state.json` gets a fresh `syncedAt` and an empty `error`, and keeps its `etag`, `version` and `files`.
7. **A fetch failure keeps state.** On network error, 401 or a non-200 status, `state.json` is rewritten with the previous `etag`, `version` and `files` and the new `error`; the audit log gets a line; `once` exits 1.
8. **Test roots.** `sync.Config.Roots map[string]string` overrides the per-agent system directory; the CLI exposes it as a repeatable `--root agent=dir` flag alongside `--state-dir`, so the end-to-end test syncs into temporary directories without root.
9. **Enrollment token from the environment.** `aw-sync enroll` reads `--token`, falling back to `AW_SYNC_TOKEN`, so the token need not land in shell history.

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/cache/cache.go` (modify) | `ReplaceMode`; `Replace` delegates to it |
| `internal/cache/cache_test.go` (modify) | mode and directory-mode tests |
| `internal/agent/agent.go` (modify) | `File`, `Rendering`, `Renderer` |
| `internal/agent/claude/claude.go` (modify) | `GOOS` field, `goos()` helper |
| `internal/agent/claude/inspect.go` (modify) | `systemDir()` uses `a.goos()` |
| `internal/agent/claude/render.go` (new) | `Render`: `aw-bundle.json` + `managed-settings.d/50-agent-wrapper.json` |
| `internal/agent/claude/render_test.go` (new) | narrowing, drop-in equals the install template, validation, modes |
| `internal/sync/paths.go` (new) | `StateDir(goos)`, `AgentRoot(goos, agent)` |
| `internal/sync/files.go` (new) | `Machine`, `State`, load/save, `ErrNotEnrolled` |
| `internal/sync/client.go` (new) | `Client.Enroll`, `Client.Fetch` |
| `internal/sync/sync.go` (new) | `Config`, `Result`, `Run`, audit line |
| `internal/sync/status.go` (new) | `Report`, `FileStatus`, `Status` |
| `internal/sync/*_test.go` (new) | per file |
| `cmd/aw-sync/main.go` (new) | `enroll`, `once`, `status`, `help` |
| `cmd/aw-sync/e2e_test.go` (new) | builds `awd` and `aw-sync`; the spec's end-to-end flow minus `aw-policy` (M4c) |
| `deploy/aw-sync/systemd/aw-sync.service`, `aw-sync.timer` (new) | Linux timer |
| `deploy/aw-sync/launchd/com.agent-wrapper.aw-sync.plist` (new) | macOS timer |
| `deploy/aw-sync/windows/register-task.ps1` (new) | Task Scheduler registration |
| `deploy/aw-sync/README.md` (new) | install steps |
| `cmd/aw-sync/deploy_test.go` (new) | every unit invokes `aw-sync once` |
| `.gitignore` (modify) | `/aw-sync` |

---

### Task 1: `cache.ReplaceMode`

**Files:**
- Modify: `internal/cache/cache.go` (the `Replace` function and its comment)
- Test: `internal/cache/cache_test.go`

**Interfaces:**
- Produces: `func ReplaceMode(path string, data []byte, mode fs.FileMode) error` — atomic write with an explicit mode; parent directory created `0755` when `mode&0o044 != 0`, else `0700`. `Replace(path, data)` keeps its behaviour (0600 file, 0700 directory).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cache/cache_test.go`:

```go
func TestReplaceModeWritesAWorldReadableFileInAWorldReadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), "etc", "claude-code")
	path := filepath.Join(dir, "aw-bundle.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %o, want 0644: a developer's helper reads what root wrote", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %o, want 0755: a readable file in an unlistable directory is unreachable", dirInfo.Mode().Perm())
	}
}

func TestReplaceModeKeepsAPrivateFileInAPrivateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "machine.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestReplaceModeReplacesAnExistingFileWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json")
	if err := cache.ReplaceMode(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceMode(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want 1: no temporary file may be left behind", len(entries))
	}
}
```

Add `"runtime"` to the test file's imports if it is not there.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cache/ -run ReplaceMode`
Expected: FAIL, `undefined: cache.ReplaceMode`.

- [ ] **Step 3: Implement `ReplaceMode`**

In `internal/cache/cache.go`, add `"io/fs"` to the imports and replace the existing `Replace` function (from its comment through its closing brace) with:

```go
// Replace writes data to path atomically, creating the parent directory when
// needed: the bytes land in a temporary file beside path and are renamed over
// it, so a concurrent reader sees the old file or the new one, never a
// partial write. The file is private to the user.
func Replace(path string, data []byte) error {
	return ReplaceMode(path, data, 0o600)
}

// ReplaceMode is Replace with an explicit file mode, for files that other
// users must read: a policy bundle that root writes and a developer's helper
// reads. The parent directory is created 0755 when the file is readable
// beyond its owner and 0700 otherwise, so a private file never lands in a
// directory that lists it and a shared file never lands in one that hides it.
func ReplaceMode(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	dirMode := fs.FileMode(0o700)
	if mode&0o044 != 0 {
		dirMode = 0o755
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("cache: creating %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cache: creating a temporary file in %s: %w", dir, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath) // no-op once the rename succeeds

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("cache: writing %s: %w", tempPath, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("cache: closing %s: %w", tempPath, err)
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		return fmt.Errorf("cache: setting permissions on %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("cache: publishing %s: %w", path, err)
	}
	return nil
}
```

Note: `MkdirAll` applies the umask, so an existing `0755` umask of `022` still yields `0755`; the test's `t.TempDir()` parent is fine.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cache/ -count=1`
Expected: PASS, including the existing `TestReplaceSwapsTheFileWhole` (mode 0600 is preserved).

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/cache/
git add internal/cache/cache.go internal/cache/cache_test.go
git commit -m "feat(cache): ReplaceMode writes system files other users can read" -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 2: `agent.Renderer` and the Claude renderer

**Files:**
- Modify: `internal/agent/agent.go` (append after `Inspector`)
- Modify: `internal/agent/claude/claude.go` (`Adapter` struct), `internal/agent/claude/inspect.go` (`systemDir()`)
- Create: `internal/agent/claude/render.go`
- Test: `internal/agent/claude/render_test.go`

**Interfaces:**
- Consumes: `policy.Bundle{Version, User, Groups, Rules}`, `policy.Rule{Name, Match, Agents map[string]policy.AgentConfig}`, `schema.Validate(any) error`.
- Produces:
  - `agent.File{Path string; Content []byte; Mode fs.FileMode}`
  - `agent.Rendering{Files []File; Notes []string}`
  - `agent.Renderer` interface: `Render(bundle *policy.Bundle) (Rendering, error)`
  - `claude.BundleFile = "aw-bundle.json"`, `claude.DropInFile = "managed-settings.d/50-agent-wrapper.json"`
  - `(*claude.Adapter).Render` — files are relative to the Claude system directory; both mode `0o644`.
  - `claude.Adapter.GOOS string` field (empty means `runtime.GOOS`).

- [ ] **Step 1: Add the agent types**

Append to `internal/agent/agent.go` (add `"io/fs"` and `"github.com/acme/agent-wrapper/internal/policy"` to its imports; `policy` does not import `agent`, so there is no cycle):

```go
// File is one file a Renderer produces. Path is relative to the agent's
// system directory, which the caller supplies, so a renderer can be tested
// into a temporary directory and deployed into /etc without knowing which.
type File struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
}

// Rendering is what a Renderer produced for one bundle: the files to write,
// and a note for anything the bundle asked for that the agent's static files
// cannot express, such as repository-scoped rules for an agent with no
// per-launch hook. Silence about a dropped rule is not an option.
type Rendering struct {
	Files []File
	Notes []string
}

// Renderer is implemented by adapters whose agent is governed by files on
// disk that something on the machine has to write. Render validates what it
// returns: an error means none of its files may be written, and the caller
// then writes nothing for any agent that cycle, so the machine never carries
// a half-applied policy.
type Renderer interface {
	Render(bundle *policy.Bundle) (Rendering, error)
}
```

- [ ] **Step 2: Write the failing renderer tests**

Create `internal/agent/claude/render_test.go`:

```go
package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/policy"
)

func sampleBundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "2026-09-21.1",
		User:    "alice@acme.com",
		Groups:  []string{"platform"},
		Rules: []policy.Rule{
			{Name: "baseline", Agents: map[string]policy.AgentConfig{
				"claude": {Managed: map[string]any{"model": "opus"}},
			}},
			{Name: "codex-only", Agents: map[string]policy.AgentConfig{
				"codex": {Managed: map[string]any{"allowed_models": []any{"gpt-5"}}},
			}},
			{Name: "payments", Match: policy.Match{Repos: []string{"github.com/acme/payments*"}},
				Agents: map[string]policy.AgentConfig{
					"claude": {Managed: map[string]any{"permissions": map[string]any{"deny": []any{"Bash(curl *)"}}}},
					"codex":  {Managed: map[string]any{"allowed_models": []any{"gpt-5"}}},
				}},
		},
	}
}

func fileNamed(t *testing.T, r agent.Rendering, path string) agent.File {
	t.Helper()
	for _, f := range r.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no rendered file %q; got %d files", path, len(r.Files))
	return agent.File{}
}

func TestRenderKeepsOnlyRulesThatMentionClaudeWithTheirMatchers(t *testing.T) {
	r, err := (&claude.Adapter{GOOS: "linux"}).Render(sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notes) != 0 {
		t.Errorf("notes = %v, want none: Claude enforces every rule per launch", r.Notes)
	}
	var out policy.Bundle
	if err := json.Unmarshal(fileNamed(t, r, claude.BundleFile).Content, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != "2026-09-21.1" || out.User != "alice@acme.com" || !reflect.DeepEqual(out.Groups, []string{"platform"}) {
		t.Errorf("header not carried through: %+v", out)
	}
	if len(out.Rules) != 2 || out.Rules[0].Name != "baseline" || out.Rules[1].Name != "payments" {
		t.Fatalf("rules = %+v, want baseline and payments in order", out.Rules)
	}
	if !reflect.DeepEqual(out.Rules[1].Match.Repos, []string{"github.com/acme/payments*"}) {
		t.Errorf("repo matcher lost: %+v", out.Rules[1].Match)
	}
	if _, leaked := out.Rules[1].Agents["codex"]; leaked {
		t.Error("the Claude bundle carries another agent's configuration")
	}
	if _, kept := out.Rules[1].Agents["claude"]; !kept {
		t.Error("the Claude configuration was dropped")
	}
}

func TestRenderOfANilOrEmptyBundleIsAnEmptyList(t *testing.T) {
	for _, b := range []*policy.Bundle{nil, {}} {
		r, err := (&claude.Adapter{GOOS: "linux"}).Render(b)
		if err != nil {
			t.Fatal(err)
		}
		content := string(fileNamed(t, r, claude.BundleFile).Content)
		var out map[string]any
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			t.Fatal(err)
		}
		if rules, ok := out["rules"].([]any); !ok || len(rules) != 0 {
			t.Errorf("rules = %v, want []", out["rules"])
		}
		if groups, ok := out["groups"].([]any); !ok || len(groups) != 0 {
			t.Errorf("groups = %v, want []", out["groups"])
		}
	}
}

func TestRenderedDropInEqualsTheInstallTemplate(t *testing.T) {
	cases := []struct{ goos, template string }{
		{"linux", "unix"},
		{"darwin", "unix"},
		{"windows", "windows"},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			r, err := (&claude.Adapter{GOOS: tc.goos}).Render(sampleBundle())
			if err != nil {
				t.Fatal(err)
			}
			got := fileNamed(t, r, claude.DropInFile).Content
			want, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "managed-settings", tc.template, "managed-settings.d", "50-agent-wrapper.json"))
			if err != nil {
				t.Fatal(err)
			}
			var gotDoc, wantDoc map[string]any
			if err := json.Unmarshal(got, &gotDoc); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &wantDoc); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("rendered drop-in differs from deploy/managed-settings/%s:\n%s\nwant:\n%s", tc.template, got, want)
			}
			if err := schema.Validate(gotDoc); err != nil {
				t.Errorf("drop-in fails the schema: %v", err)
			}
		})
	}
}

func TestRenderRejectsManagedSettingsTheSchemaRejects(t *testing.T) {
	b := &policy.Bundle{Version: "v", Rules: []policy.Rule{
		{Name: "bad", Agents: map[string]policy.AgentConfig{
			"claude": {Managed: map[string]any{"permissions": "nope"}},
		}},
	}}
	r, err := (&claude.Adapter{GOOS: "linux"}).Render(b)
	if err == nil {
		t.Fatal("want an error: a bundle aw-policy would refuse must not be written")
	}
	if len(r.Files) != 0 {
		t.Errorf("files returned alongside an error: %d", len(r.Files))
	}
}

func TestRenderedFilesAreReadableByEveryone(t *testing.T) {
	r, err := (&claude.Adapter{GOOS: "linux"}).Render(sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 2 {
		t.Fatalf("got %d files, want the bundle and the drop-in", len(r.Files))
	}
	for _, f := range r.Files {
		if f.Mode != 0o644 {
			t.Errorf("%s mode = %o, want 0644: the developer's helper reads it", f.Path, f.Mode)
		}
		if filepath.IsAbs(f.Path) {
			t.Errorf("%s is absolute; paths are relative to the system directory", f.Path)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/agent/claude/ -run Render`
Expected: FAIL, `unknown field GOOS` / `undefined: claude.BundleFile`.

- [ ] **Step 4: Add `GOOS` to the adapter**

In `internal/agent/claude/claude.go`, add to the `Adapter` struct after `ConfigDir`:

```go
	// GOOS overrides the operating system the adapter renders and inspects
	// for; empty means the one this binary runs on. Tests render the Windows
	// drop-in on Linux with it.
	GOOS string
```

and add the helper after `binaryName()`:

```go
// goos is the operating system whose paths the adapter uses.
func (a *Adapter) goos() string {
	if a.GOOS != "" {
		return a.GOOS
	}
	return runtime.GOOS
}
```

Add `"runtime"` to `claude.go`'s imports. In `internal/agent/claude/inspect.go`, change `systemDir()` to switch on `a.goos()` instead of `runtime.GOOS`. Leave the other `runtime.GOOS` use in that file (the executable-bit check) alone: it is about the machine the inspection runs on, not the one rendered for, so the `"runtime"` import stays.

- [ ] **Step 5: Write the renderer**

Create `internal/agent/claude/render.go`:

```go
package claude

import (
	"encoding/json"
	"fmt"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/policy"
)

const (
	// BundleFile is where aw-sync leaves the user's bundle for aw-policy,
	// relative to the system directory. It is the organization's policy, not
	// a secret, so it is readable by every developer on the machine.
	BundleFile = "aw-bundle.json"
	// DropInFile is the managed-settings drop-in that names the helper. It
	// never changes, but aw-sync owning it means installing governance is
	// "install the binaries, enroll" and nothing else.
	DropInFile = "managed-settings.d/50-agent-wrapper.json"
)

// helperPath is where the install procedure puts aw-policy on this OS. The
// drop-in must name the same path deploy/managed-settings documents, and a
// test holds the two together.
func helperPath(goos string) string {
	if goos == "windows" {
		return `C:\Program Files\AgentWrapper\aw-policy.exe`
	}
	return "/usr/local/bin/aw-policy"
}

// Render produces the two files Claude Code's governance rests on: the
// bundle narrowed to this agent, with repository matchers intact for
// aw-policy to resolve per launch, and the static drop-in that points
// Claude Code at aw-policy. Nothing is compiled here: matching a rule to a
// repository is the helper's job, because only it knows where a session
// runs.
//
// Every rule's managed settings are validated against the schema this
// binary carries, so a bundle aw-policy would refuse is never written.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	narrowed := narrow(bundle)
	for _, rule := range narrowed.Rules {
		if err := schema.ForAgent(Name, rule.Agents[Name].Managed); err != nil {
			return agent.Rendering{}, fmt.Errorf("claude: rule %q: %w", rule.Name, err)
		}
	}
	bundleJSON, err := json.MarshalIndent(narrowed, "", "  ")
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("claude: encoding the bundle: %w", err)
	}

	dropIn := map[string]any{"policyHelper": map[string]any{
		"path":              helperPath(a.goos()),
		"timeoutMs":         5000,
		"refreshIntervalMs": 300000,
	}}
	if err := schema.Validate(dropIn); err != nil {
		return agent.Rendering{}, fmt.Errorf("claude: the policyHelper drop-in: %w", err)
	}
	dropInJSON, err := json.MarshalIndent(dropIn, "", "  ")
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("claude: encoding the drop-in: %w", err)
	}

	return agent.Rendering{Files: []agent.File{
		{Path: BundleFile, Content: append(bundleJSON, '\n'), Mode: 0o644},
		{Path: DropInFile, Content: append(dropInJSON, '\n'), Mode: 0o644},
	}}, nil
}

// narrow keeps the rules that configure Claude, each reduced to its Claude
// entry, with matchers untouched. Other agents' settings are written to
// their own system files by their own renderers; carrying them here would
// only widen what a reader of this file learns.
func narrow(bundle *policy.Bundle) *policy.Bundle {
	out := &policy.Bundle{Groups: make([]string, 0), Rules: make([]policy.Rule, 0)}
	if bundle == nil {
		return out
	}
	out.Version = bundle.Version
	out.User = bundle.User
	out.Groups = append(out.Groups, bundle.Groups...)
	for _, rule := range bundle.Rules {
		config, ok := rule.Agents[Name]
		if !ok {
			continue
		}
		out.Rules = append(out.Rules, policy.Rule{
			Name:   rule.Name,
			Match:  rule.Match,
			Agents: map[string]policy.AgentConfig{Name: config},
		})
	}
	return out
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/agent/... -count=1`
Expected: PASS. Then `go vet ./internal/agent/...` and `gofmt -l .` are clean.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent.go internal/agent/claude/
git commit -m "feat(claude): render the bundle and policyHelper drop-in for aw-sync" -m "agent.Renderer is the optional adapter interface aw-sync drives. The Claude renderer narrows the bundle to rules that configure Claude, keeps repo matchers for aw-policy, and emits the same drop-in deploy/managed-settings ships; a test holds them equal." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 3: `internal/sync` paths and state files

**Files:**
- Create: `internal/sync/paths.go`, `internal/sync/files.go`
- Test: `internal/sync/paths_test.go`, `internal/sync/files_test.go`

**Interfaces:**
- Consumes: `cache.ReplaceMode`.
- Produces:
  - `func StateDir(goos string) string`
  - `func AgentRoot(goos, agentName string) (string, error)`
  - `type Machine struct{ Server, MachineID, Credential string; Agents []string }` (JSON: `server`, `machineId`, `credential`, `agents`)
  - `type State struct{ ETag, Version string; SyncedAt time.Time; Files map[string]string; Error string; Notes []string }` (JSON: `etag`, `version`, `syncedAt`, `files`, `error`, `notes`)
  - `var ErrNotEnrolled = errors.New("not enrolled")`
  - `func LoadMachine(dir string) (Machine, error)`, `func SaveMachine(dir string, m Machine) error` (0600)
  - `func LoadState(dir string) (State, error)` (missing → zero State, nil), `func SaveState(dir string, s State) error` (0644)
  - constants `MachineFile = "machine.json"`, `StateFile = "state.json"`, `AuditFile = "aw-sync.log"`

- [ ] **Step 1: Write the failing path tests**

Create `internal/sync/paths_test.go`:

```go
package sync_test

import (
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestStateDirPerOS(t *testing.T) {
	cases := map[string]string{
		"linux":   "/var/lib/agent-wrapper",
		"darwin":  "/Library/Application Support/agent-wrapper",
		"windows": `C:\ProgramData\agent-wrapper`,
	}
	t.Setenv("ProgramData", "")
	for goos, want := range cases {
		if got := sync.StateDir(goos); got != want {
			t.Errorf("StateDir(%s) = %q, want %q", goos, got, want)
		}
	}
}

func TestAgentRootTable(t *testing.T) {
	t.Setenv("ProgramData", "")
	cases := []struct{ goos, agent, want string }{
		{"linux", "claude", "/etc/claude-code"},
		{"darwin", "claude", "/Library/Application Support/ClaudeCode"},
		{"windows", "claude", `C:\Program Files\ClaudeCode`},
		{"linux", "codex", "/etc/codex"},
		{"darwin", "codex", "/etc/codex"},
		{"windows", "codex", `C:\ProgramData\OpenAI\Codex`},
		{"linux", "gemini", "/etc/gemini-cli"},
		{"darwin", "gemini", "/Library/Application Support/GeminiCli"},
		{"windows", "gemini", `C:\ProgramData\gemini-cli`},
	}
	for _, tc := range cases {
		got, err := sync.AgentRoot(tc.goos, tc.agent)
		if err != nil {
			t.Errorf("AgentRoot(%s, %s): %v", tc.goos, tc.agent, err)
			continue
		}
		if got != tc.want {
			t.Errorf("AgentRoot(%s, %s) = %q, want %q", tc.goos, tc.agent, got, tc.want)
		}
	}
}

func TestAgentRootHonoursProgramData(t *testing.T) {
	t.Setenv("ProgramData", `D:\PD`)
	got, err := sync.AgentRoot("windows", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if got != `D:\PD\OpenAI\Codex` {
		t.Errorf("got %q", got)
	}
}

func TestAgentRootRejectsAnUnknownAgent(t *testing.T) {
	_, err := sync.AgentRoot("linux", "copilot")
	if err == nil || !strings.Contains(err.Error(), "copilot") {
		t.Errorf("err = %v, want one naming the agent", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/sync/`
Expected: FAIL to build, `no non-test Go files` or undefined identifiers.

- [ ] **Step 3: Write `paths.go`**

Create `internal/sync/paths.go`:

```go
// Package sync keeps a machine's agent configuration files in step with the
// control plane. It is the one network client on a machine: it fetches the
// enrolled user's bundle with the machine credential, renders every agent's
// files, and writes them all or none, so the machine never carries a policy
// that is half applied.
package sync

import (
	"fmt"
	"os"
)

// StateDir is where aw-sync keeps machine.json and state.json on goos. It is
// root-owned and readable by everyone, because `aw doctor` runs as the
// developer and reports from it; only machine.json is private.
func StateDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/agent-wrapper"
	case "windows":
		return programData() + `\agent-wrapper`
	default:
		return "/var/lib/agent-wrapper"
	}
}

// agentRoots is the one table of where each agent reads its enforced
// configuration, in the order linux, darwin, windows. A Windows entry that
// starts with %ProgramData% is resolved at call time.
var agentRoots = map[string][3]string{
	"claude": {"/etc/claude-code", "/Library/Application Support/ClaudeCode", `C:\Program Files\ClaudeCode`},
	"codex":  {"/etc/codex", "/etc/codex", `%ProgramData%\OpenAI\Codex`},
	"gemini": {"/etc/gemini-cli", "/Library/Application Support/GeminiCli", `%ProgramData%\gemini-cli`},
}

// AgentRoot is the directory agentName's rendered files live under on goos.
// An agent without a row cannot be governed by files, and saying so is
// better than writing them somewhere nothing reads.
func AgentRoot(goos, agentName string) (string, error) {
	roots, ok := agentRoots[agentName]
	if !ok {
		return "", fmt.Errorf("sync: no system directory is known for agent %q", agentName)
	}
	var root string
	switch goos {
	case "darwin":
		root = roots[1]
	case "windows":
		root = roots[2]
	default:
		root = roots[0]
	}
	if len(root) > 13 && root[:13] == "%ProgramData%" {
		root = programData() + root[13:]
	}
	return root, nil
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

- [ ] **Step 4: Run the path tests**

Run: `go test ./internal/sync/ -run 'StateDir|AgentRoot' -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing file tests**

Create `internal/sync/files_test.go`:

```go
package sync_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestMachineRoundTripsAndIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	want := sync.Machine{Server: "http://awd", MachineID: "m1", Credential: "secret", Agents: []string{"claude"}}
	if err := sync.SaveMachine(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := sync.LoadMachine(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, sync.MachineFile))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("machine.json mode = %o, want 0600: it carries the credential", info.Mode().Perm())
		}
	}
}

func TestLoadMachineWithoutEnrollmentIsErrNotEnrolled(t *testing.T) {
	_, err := sync.LoadMachine(t.TempDir())
	if !errors.Is(err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want ErrNotEnrolled", err)
	}
}

func TestLoadMachineRejectsAnIncompleteFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sync.MachineFile), []byte(`{"server":"http://awd"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := sync.LoadMachine(dir)
	if err == nil || errors.Is(err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want a validation error that is not ErrNotEnrolled", err)
	}
}

func TestStateRoundTripsAndIsWorldReadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	want := sync.State{
		ETag:     `"abc"`,
		Version:  "v1",
		SyncedAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
		Files:    map[string]string{"/etc/claude-code/aw-bundle.json": "deadbeef"},
		Error:    "",
		Notes:    []string{"a note"},
	}
	if err := sync.SaveState(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := sync.LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, sync.StateFile))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("state.json mode = %o, want 0644: aw doctor reads it as the developer", info.Mode().Perm())
		}
	}
}

func TestLoadStateWithoutAFileIsTheZeroState(t *testing.T) {
	got, err := sync.LoadState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sync.State{}) {
		t.Errorf("got %+v, want the zero State", got)
	}
}

func TestLoadStateReportsACorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sync.StateFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.LoadState(dir); err == nil {
		t.Error("want an error for a corrupt state file")
	}
}

func TestSaveStateWritesEmptyCollectionsAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := sync.SaveState(dir, sync.State{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, sync.StateFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"files": {}`, `"notes": []`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("state.json lacks %s:\n%s", want, raw)
		}
	}
}
```

Add `"strings"` to the file's imports.

- [ ] **Step 6: Run to verify they fail**

Run: `go test ./internal/sync/ -run 'Machine|State' -count=1`
Expected: FAIL, undefined `sync.Machine` etc.

- [ ] **Step 7: Write `files.go`**

Create `internal/sync/files.go`:

```go
package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/acme/agent-wrapper/internal/cache"
)

const (
	// MachineFile holds the enrollment. It carries the credential, so it is
	// private to root.
	MachineFile = "machine.json"
	// StateFile holds what the last cycle left behind, readable by everyone
	// because `aw doctor` reports from it as the developer.
	StateFile = "state.json"
	// AuditFile is the append-only log of every cycle.
	AuditFile = "aw-sync.log"
)

// ErrNotEnrolled means there is no machine.json: `aw-sync enroll` has not
// been run here.
var ErrNotEnrolled = errors.New("not enrolled")

// Machine is this machine's enrollment, written once by `aw-sync enroll` and
// read by every cycle.
type Machine struct {
	// Server is the control plane's base URL.
	Server string `json:"server"`
	// MachineID is what the control plane calls this machine.
	MachineID string `json:"machineId"`
	// Credential is the bearer credential shown once at enrollment.
	Credential string `json:"credential"`
	// Agents names the adapters whose files this machine renders. A machine
	// without Codex installed lists only what it runs, so no directory is
	// created for an agent that is not there.
	Agents []string `json:"agents"`
}

// State is what the last cycle left behind.
type State struct {
	// ETag is the bundle's entity tag, sent back as If-None-Match.
	ETag string `json:"etag,omitempty"`
	// Version is the policy revision last rendered.
	Version string `json:"version,omitempty"`
	// SyncedAt is when the last successful cycle finished, including one the
	// server answered with 304.
	SyncedAt time.Time `json:"syncedAt"`
	// Files maps every rendered file's absolute path to the SHA-256 of what
	// was written, so `status` can tell drift from a fresh render.
	Files map[string]string `json:"files"`
	// Error is why the last cycle failed, or empty.
	Error string `json:"error,omitempty"`
	// Notes are what the renderers reported, such as rules an agent cannot
	// enforce.
	Notes []string `json:"notes"`
}

// LoadMachine reads the enrollment from dir. A missing file is
// ErrNotEnrolled; an incomplete one is a plain error, because a machine that
// half-enrolled must not be mistaken for one that never did.
func LoadMachine(dir string) (Machine, error) {
	raw, err := os.ReadFile(filepath.Join(dir, MachineFile))
	if errors.Is(err, os.ErrNotExist) {
		return Machine{}, fmt.Errorf("sync: %w: no %s in %s; run `aw-sync enroll`", ErrNotEnrolled, MachineFile, dir)
	}
	if err != nil {
		return Machine{}, fmt.Errorf("sync: reading the enrollment: %w", err)
	}
	var m Machine
	if err := json.Unmarshal(raw, &m); err != nil {
		return Machine{}, fmt.Errorf("sync: parsing %s: %w", MachineFile, err)
	}
	switch {
	case m.Server == "":
		return Machine{}, fmt.Errorf("sync: %s has no server", MachineFile)
	case m.MachineID == "":
		return Machine{}, fmt.Errorf("sync: %s has no machineId", MachineFile)
	case m.Credential == "":
		return Machine{}, fmt.Errorf("sync: %s has no credential", MachineFile)
	}
	if m.Agents == nil {
		m.Agents = make([]string, 0)
	}
	return m, nil
}

// SaveMachine writes the enrollment, private to the owner.
func SaveMachine(dir string, m Machine) error {
	if m.Agents == nil {
		m.Agents = make([]string, 0)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("sync: encoding the enrollment: %w", err)
	}
	if err := cache.ReplaceMode(filepath.Join(dir, MachineFile), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// LoadState reads the last cycle's record. No file means no cycle has run,
// which is the zero State and not an error; a file that cannot be parsed is
// an error, so a caller can decide whether to start over.
func LoadState(dir string) (State, error) {
	raw, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("sync: reading %s: %w", StateFile, err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("sync: parsing %s: %w", StateFile, err)
	}
	return s, nil
}

// SaveState writes the cycle's record, readable by everyone. Empty
// collections are written as empty, not null, so a reader in any language
// sees a map and a list.
func SaveState(dir string, s State) error {
	if s.Files == nil {
		s.Files = make(map[string]string)
	}
	if s.Notes == nil {
		s.Notes = make([]string, 0)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("sync: encoding the state: %w", err)
	}
	if err := cache.ReplaceMode(filepath.Join(dir, StateFile), append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}
```

Note for `TestStateRoundTripsAndIsWorldReadable`: `reflect.DeepEqual` on the round-tripped `State` requires `SyncedAt` in UTC (it is) and `Notes` non-nil (it is). `Files` with one entry round-trips as an equal map.

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/sync/ -count=1 && go vet ./internal/sync/ && gofmt -l .`
Expected: PASS, clean.

- [ ] **Step 9: Commit**

```bash
git add internal/sync/
git commit -m "feat(sync): system paths per OS and the machine and state files" -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 4: `sync.Client` — enroll and fetch

**Files:**
- Create: `internal/sync/client.go`
- Test: `internal/sync/client_test.go`

**Interfaces:**
- Consumes: `awd` routes `POST /v1/machines/enroll` `{token,name,os}` → 201 `{machineId, credential, user}`; `GET /v1/bundle` with `Authorization: Bearer <credential>` and `If-None-Match` → 200 bundle JSON with `ETag`, 304, 401. `model.ErrUnauthorized`.
- Produces:
  - `type Client struct{ Server string; HTTP *http.Client }` (nil HTTP → 15 s timeout client)
  - `type Enrollment struct{ MachineID, Credential, User string }`
  - `func (c *Client) Enroll(ctx context.Context, token, name, goos string) (Enrollment, error)`
  - `type Fetched struct{ Bundle *policy.Bundle; ETag string; Unchanged bool }`
  - `func (c *Client) Fetch(ctx context.Context, credential, etag string) (Fetched, error)` — 304 → `Unchanged: true`, 401 → wraps `model.ErrUnauthorized`.
  - `const maxBundleBytes = 4 << 20`

- [ ] **Step 1: Write the failing tests**

Create `internal/sync/client_test.go`:

```go
package sync_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/sync"
)

func TestEnrollPostsTheTokenAndReturnsTheCredential(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/machines/enroll" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"machineId":"m1","credential":"c1","user":"alice@acme.com"}`))
	}))
	defer srv.Close()

	c := &sync.Client{Server: srv.URL + "/"}
	e, err := c.Enroll(context.Background(), "tok", "host1", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got["token"] != "tok" || got["name"] != "host1" || got["os"] != "linux" {
		t.Errorf("request body = %v", got)
	}
	if e.MachineID != "m1" || e.Credential != "c1" || e.User != "alice@acme.com" {
		t.Errorf("enrollment = %+v", e)
	}
}

func TestEnrollReportsARejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"conflict"}`, http.StatusConflict)
	}))
	defer srv.Close()
	_, err := (&sync.Client{Server: srv.URL}).Enroll(context.Background(), "tok", "h", "linux")
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Errorf("err = %v, want one naming the 409", err)
	}
}

func bundleServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		if status == http.StatusOK {
			w.Header().Set("ETag", `"e2"`)
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestFetchSendsTheCredentialAndETag(t *testing.T) {
	srv, seen := bundleServer(t, http.StatusOK, `{"version":"v2","groups":[],"rules":[]}`)
	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`)
	if err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/v1/bundle" || seen.Header.Get("Authorization") != "Bearer cred" || seen.Header.Get("If-None-Match") != `"e1"` {
		t.Errorf("request: %s %s %v", seen.Method, seen.URL.Path, seen.Header)
	}
	if got.Unchanged || got.ETag != `"e2"` || got.Bundle == nil || got.Bundle.Version != "v2" {
		t.Errorf("fetched = %+v", got)
	}
}

func TestFetchOn304IsUnchanged(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusNotModified, "")
	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Unchanged || got.Bundle != nil {
		t.Errorf("fetched = %+v, want Unchanged with no bundle", got)
	}
}

func TestFetchOn401IsErrUnauthorized(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "")
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestFetchOnServerErrorFails(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusInternalServerError, "boom")
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want one naming the status", err)
	}
}

func TestFetchRejectsAMalformedBundle(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusOK, `{"version":`)
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "")
	if err == nil {
		t.Error("want an error for a malformed bundle")
	}
}

func TestFetchWhenTheServerIsDownFails(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusOK, "{}")
	url := srv.URL
	srv.Close()
	_, err := (&sync.Client{Server: url}).Fetch(context.Background(), "cred", "")
	if err == nil {
		t.Error("want an error when the control plane is unreachable")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/sync/ -run 'Enroll|Fetch' -count=1`
Expected: FAIL, undefined `sync.Client`.

- [ ] **Step 3: Write `client.go`**

Create `internal/sync/client.go`:

```go
package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

// maxBundleBytes bounds what is read from the control plane. A bundle is a
// few kilobytes; anything near this is a broken server, not a policy.
const maxBundleBytes = 4 << 20

// defaultTimeout bounds one exchange when the caller supplies no client. A
// timer-driven cycle can afford to wait, but not forever.
const defaultTimeout = 15 * time.Second

// Client talks to the control plane on behalf of one machine.
type Client struct {
	// Server is the control plane's base URL.
	Server string
	// HTTP overrides the client used; nil means one with defaultTimeout.
	HTTP *http.Client
}

// Enrollment is what the control plane returns for a consumed token. The
// credential is shown once; the control plane keeps only its hash.
type Enrollment struct {
	MachineID  string `json:"machineId"`
	Credential string `json:"credential"`
	User       string `json:"user"`
}

// Fetched is one answer to a conditional bundle request. Unchanged means
// the server answered 304 and Bundle is nil.
type Fetched struct {
	Bundle    *policy.Bundle
	ETag      string
	Unchanged bool
}

// Enroll exchanges a single-use token for this machine's credential.
func (c *Client) Enroll(ctx context.Context, token, name, goos string) (Enrollment, error) {
	body, err := json.Marshal(map[string]string{"token": token, "name": name, "os": goos})
	if err != nil {
		return Enrollment{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/machines/enroll"), bytes.NewReader(body))
	if err != nil {
		return Enrollment{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return Enrollment{}, fmt.Errorf("sync: reaching the control plane at %s: %w", c.Server, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes))
	if resp.StatusCode != http.StatusCreated {
		return Enrollment{}, fmt.Errorf("sync: enrollment refused: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	var e Enrollment
	if err := json.Unmarshal(payload, &e); err != nil {
		return Enrollment{}, fmt.Errorf("sync: decoding the enrollment: %w", err)
	}
	if e.MachineID == "" || e.Credential == "" {
		return Enrollment{}, errors.New("sync: the control plane returned an incomplete enrollment")
	}
	return e, nil
}

// Fetch asks for the machine's bundle, revalidating etag when it is not
// empty. A 401 is reported as model.ErrUnauthorized so the caller can say
// "revoked" rather than "failed".
func (c *Client) Fetch(ctx context.Context, credential, etag string) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/v1/bundle"), nil)
	if err != nil {
		return Fetched{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("User-Agent", "aw-sync")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("sync: reaching the control plane at %s: %w", c.Server, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return Fetched{ETag: etag, Unchanged: true}, nil
	case http.StatusUnauthorized:
		return Fetched{}, fmt.Errorf("sync: %w: the control plane rejected this machine's credential; re-enroll", model.ErrUnauthorized)
	case http.StatusOK:
	default:
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Fetched{}, fmt.Errorf("sync: the control plane answered %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return Fetched{}, fmt.Errorf("sync: reading the bundle: %w", err)
	}
	if len(payload) > maxBundleBytes {
		return Fetched{}, fmt.Errorf("sync: the bundle exceeds %d bytes", maxBundleBytes)
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return Fetched{}, fmt.Errorf("sync: parsing the bundle: %w", err)
	}
	return Fetched{Bundle: &bundle, ETag: resp.Header.Get("ETag")}, nil
}

func (c *Client) endpoint(path string) string {
	return strings.TrimSuffix(c.Server, "/") + path
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/sync/ -count=1 && go vet ./internal/sync/ && gofmt -l .`
Expected: PASS, clean.

- [ ] **Step 5: Commit**

```bash
git add internal/sync/client.go internal/sync/client_test.go
git commit -m "feat(sync): control plane client for enrollment and conditional bundle fetches" -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 5: `sync.Run` — the cycle

**Files:**
- Create: `internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Interfaces:**
- Consumes: Tasks 2–4: `agent.Registry.Lookup`, `agent.Renderer`, `agent.Rendering`, `agent.File`; `Client.Fetch`; `LoadMachine`, `LoadState`, `SaveState`; `AgentRoot`; `cache.ReplaceMode`.
- Produces:
  - `type Config struct{ StateDir string; GOOS string; Roots map[string]string; Registry *agent.Registry; HTTP *http.Client; Now func() time.Time }`
  - `type Result struct{ Unchanged bool; Version string; Written []string; Notes []string; Err error }`
  - `func Run(ctx context.Context, cfg Config) Result`
  - `func (cfg Config) root(agentName string) (string, error)` (unexported helper Task 6 reuses)
  - `func hashOf(data []byte) string` (SHA-256 hex; Task 6 reuses)

- [ ] **Step 1: Write the failing tests**

Create `internal/sync/sync_test.go`:

```go
package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/sync"
)

// fakeAwd serves one bundle with an ETag and honours If-None-Match. Its
// status can be forced to simulate an outage or a revocation.
type fakeAwd struct {
	srv    *httptest.Server
	bundle atomic.Value // *policy.Bundle
	status atomic.Int32 // 0 means behave normally
	hits   atomic.Int32
}

func newFakeAwd(t *testing.T, b *policy.Bundle) *fakeAwd {
	t.Helper()
	f := &fakeAwd{}
	f.bundle.Store(b)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer cred" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if s := f.status.Load(); s != 0 {
			http.Error(w, `{"error":"forced"}`, int(s))
			return
		}
		body, _ := json.Marshal(f.bundle.Load().(*policy.Bundle))
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func testBundle() *policy.Bundle {
	return &policy.Bundle{Version: "v1", User: "alice@acme.com", Groups: []string{"platform"}, Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}}
}

// enrolled sets up a state directory enrolled against f for the given
// agents and returns a Config pointing rendered files at a temp root.
func enrolled(t *testing.T, f *fakeAwd, reg *agent.Registry, agents ...string) (sync.Config, string) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := sync.SaveMachine(stateDir, sync.Machine{Server: f.srv.URL, MachineID: "m1", Credential: "cred", Agents: agents}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "claude-root")
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	return sync.Config{
		StateDir: stateDir,
		GOOS:     "linux",
		Roots:    map[string]string{"claude": root, "broken": filepath.Join(t.TempDir(), "broken-root")},
		Registry: reg,
		Now:      func() time.Time { return now },
	}, root
}

func claudeRegistry(t *testing.T, extra ...agent.Adapter) *agent.Registry {
	t.Helper()
	reg := &agent.Registry{}
	if err := reg.Register(&claude.Adapter{GOOS: "linux"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range extra {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// brokenRenderer is an adapter whose renderer always fails, to prove that
// one agent's failure keeps every agent's files unwritten.
type brokenRenderer struct{}

func (brokenRenderer) Name() string                        { return "broken" }
func (brokenRenderer) Locate([]string) (string, error)     { return "", errors.New("not installed") }
func (brokenRenderer) Build(context.Context, agent.BuildOptions) (*agent.Launch, error) {
	return nil, errors.New("not buildable")
}
func (brokenRenderer) Render(*policy.Bundle) (agent.Rendering, error) {
	return agent.Rendering{}, errors.New("broken: cannot render")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunWritesEveryRenderedFileAndRecordsState(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Unchanged || res.Version != "v1" || len(res.Written) != 2 {
		t.Errorf("result = %+v", res)
	}

	bundlePath := filepath.Join(root, claude.BundleFile)
	dropInPath := filepath.Join(root, claude.DropInFile)
	var written policy.Bundle
	if err := json.Unmarshal(mustRead(t, bundlePath), &written); err != nil {
		t.Fatal(err)
	}
	if written.Version != "v1" || len(written.Rules) != 1 {
		t.Errorf("bundle on disk = %+v", written)
	}
	if !strings.Contains(string(mustRead(t, dropInPath)), "policyHelper") {
		t.Error("drop-in not written")
	}
	for _, p := range []string{bundlePath, dropInPath} {
		if got := string(mustRead(t, p+".aw-revision")); got != "v1\n" {
			t.Errorf("%s.aw-revision = %q, want \"v1\\n\"", p, got)
		}
	}

	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != "v1" || state.ETag == "" || state.Error != "" || !state.SyncedAt.Equal(cfg.Now()) {
		t.Errorf("state = %+v", state)
	}
	sum := sha256.Sum256(mustRead(t, bundlePath))
	if state.Files[bundlePath] != hex.EncodeToString(sum[:]) {
		t.Errorf("state.Files[%s] = %q, want the file's sha256", bundlePath, state.Files[bundlePath])
	}
	if _, ok := state.Files[dropInPath]; !ok {
		t.Error("the drop-in is not in state.Files")
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, sync.AuditFile)); err != nil {
		t.Errorf("no audit log: %v", err)
	}
}

func TestRunIsANoOpOn304(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	before := mustRead(t, filepath.Join(root, claude.BundleFile))
	first, _ := sync.LoadState(cfg.StateDir)

	later := time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return later }
	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !res.Unchanged || len(res.Written) != 0 || res.Version != "v1" {
		t.Errorf("result = %+v, want Unchanged with nothing written", res)
	}
	if string(mustRead(t, filepath.Join(root, claude.BundleFile))) != string(before) {
		t.Error("the bundle was rewritten on a 304")
	}
	second, _ := sync.LoadState(cfg.StateDir)
	if second.ETag != first.ETag || second.Version != first.Version || len(second.Files) != len(first.Files) {
		t.Errorf("state changed on 304: before %+v after %+v", first, second)
	}
	if !second.SyncedAt.Equal(later) {
		t.Errorf("syncedAt = %v, want %v: a 304 is a successful cycle", second.SyncedAt, later)
	}
}

func TestRunKeepsFilesAndStateWhenTheServerFails(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	before := mustRead(t, filepath.Join(root, claude.BundleFile))
	good, _ := sync.LoadState(cfg.StateDir)

	f.status.Store(http.StatusInternalServerError)
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error when the control plane fails")
	}
	if string(mustRead(t, filepath.Join(root, claude.BundleFile))) != string(before) {
		t.Error("a failed fetch changed the bundle on disk")
	}
	if _, err := os.Stat(filepath.Join(root, claude.DropInFile)); err != nil {
		t.Error("a failed fetch removed the drop-in")
	}
	after, _ := sync.LoadState(cfg.StateDir)
	if after.Error == "" || !strings.Contains(after.Error, "500") {
		t.Errorf("state.Error = %q, want the failure recorded", after.Error)
	}
	if after.ETag != good.ETag || after.Version != good.Version || len(after.Files) != len(good.Files) {
		t.Errorf("state lost its last good record: %+v", after)
	}
	if !after.SyncedAt.Equal(good.SyncedAt) {
		t.Error("syncedAt moved on a failed cycle")
	}
}

func TestRunKeepsFilesWhenTheServerIsUnreachable(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	f.srv.Close()
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error when the control plane is down")
	}
	if _, err := os.Stat(filepath.Join(root, claude.BundleFile)); err != nil {
		t.Error("an outage removed the bundle")
	}
}

func TestRunReportsARevokedCredential(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")
	f.status.Store(http.StatusUnauthorized)
	res := sync.Run(context.Background(), cfg)
	if !errors.Is(res.Err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", res.Err)
	}
}

func TestRunWritesNothingForAnyAgentWhenOneRendererFails(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t, brokenRenderer{}), "claude", "broken")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "broken") {
		t.Fatalf("err = %v, want the broken renderer's error", res.Err)
	}
	if _, err := os.Stat(filepath.Join(root, claude.BundleFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("Claude's bundle was written although another agent failed to render")
	}
	state, _ := sync.LoadState(cfg.StateDir)
	if state.Error == "" || state.Version != "" {
		t.Errorf("state = %+v, want the error recorded and no version", state)
	}
}

func TestRunWritesNothingWhenTheBundleFailsValidation(t *testing.T) {
	bad := testBundle()
	bad.Rules[0].Agents["claude"] = policy.AgentConfig{Managed: map[string]any{"permissions": "nope"}}
	f := newFakeAwd(t, bad)
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error for a bundle the schema rejects")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("root has %d entries, want none", len(entries))
	}
}

func TestRunRequiresEnrollment(t *testing.T) {
	cfg := sync.Config{StateDir: t.TempDir(), GOOS: "linux", Registry: claudeRegistry(t)}
	res := sync.Run(context.Background(), cfg)
	if !errors.Is(res.Err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want ErrNotEnrolled", res.Err)
	}
}

func TestRunRejectsAnAgentWithoutARenderer(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "codex")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "codex") {
		t.Errorf("err = %v, want one naming codex", res.Err)
	}
}

func TestRunCarriesRendererNotesIntoState(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t, notingRenderer{}), "claude", "noting")
	cfg.Roots["noting"] = filepath.Join(t.TempDir(), "noting-root")
	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	state, _ := sync.LoadState(cfg.StateDir)
	if len(state.Notes) != 1 || !strings.Contains(state.Notes[0], "not enforceable") {
		t.Errorf("notes = %v", state.Notes)
	}
}

// notingRenderer renders one file and reports a note, standing in for the
// Codex and Gemini renderers that drop repo-scoped rules.
type notingRenderer struct{}

func (notingRenderer) Name() string                    { return "noting" }
func (notingRenderer) Locate([]string) (string, error) { return "", errors.New("not installed") }
func (notingRenderer) Build(context.Context, agent.BuildOptions) (*agent.Launch, error) {
	return nil, errors.New("not buildable")
}
func (notingRenderer) Render(*policy.Bundle) (agent.Rendering, error) {
	return agent.Rendering{
		Files: []agent.File{{Path: "requirements.toml", Content: []byte("# ok\n"), Mode: 0o644}},
		Notes: []string{"1 repo-scoped rule is not enforceable for noting"},
	}, nil
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/sync/ -run Run -count=1`
Expected: FAIL, undefined `sync.Config`, `sync.Run`.

- [ ] **Step 3: Write `sync.go`**

Create `internal/sync/sync.go`:

```go
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/cache"
)

// revisionSuffix names the sibling written beside every rendered JSON file,
// so an operator looking at /etc can see which revision produced it without
// parsing the file. TOML files carry the same fact as a header comment.
const revisionSuffix = ".aw-revision"

// auditRotateAt is the log size past which the current log is moved aside.
const auditRotateAt = 1 << 20

// Config is everything one cycle needs. It is plain data so the executable
// and the tests build it the same way.
type Config struct {
	// StateDir holds machine.json, state.json and the audit log.
	StateDir string
	// GOOS selects the system directories; empty means this binary's.
	GOOS string
	// Roots overrides the system directory per agent, so tests render into
	// temporary directories. An agent not listed uses AgentRoot.
	Roots map[string]string
	// Registry holds the adapters this binary was compiled with. Only those
	// implementing agent.Renderer can be synced.
	Registry *agent.Registry
	// HTTP overrides the client used to reach the control plane.
	HTTP *http.Client
	// Now supplies the time for state.json and the audit log.
	Now func() time.Time
}

// Result is what one cycle did.
type Result struct {
	// Unchanged means the server answered 304 and nothing was rendered.
	Unchanged bool
	// Version is the policy revision now on disk.
	Version string
	// Written lists the absolute paths of the files this cycle wrote.
	Written []string
	// Notes are what the renderers reported.
	Notes []string
	// Err is why the cycle failed. Files on disk are untouched when it is
	// set: a failure keeps the last good policy in force.
	Err error
}

// plannedFile is a rendered file resolved to its absolute path, held until
// every renderer has succeeded.
type plannedFile struct {
	path    string
	content []byte
	mode    fs.FileMode
}

// Run performs one cycle: fetch the bundle, render every enrolled agent,
// write everything or nothing, record what happened. It never deletes a
// rendered file; a machine that cannot reach the control plane keeps the
// policy it last received.
func Run(ctx context.Context, cfg Config) Result {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}

	machine, err := LoadMachine(cfg.StateDir)
	if err != nil {
		return Result{Err: err}
	}
	state, err := LoadState(cfg.StateDir)
	var notes []string
	if err != nil {
		// A corrupt record must not stop the cycle; it is rebuilt below.
		notes = append(notes, "previous state discarded: "+err.Error())
		state = State{}
	}

	client := &Client{Server: machine.Server, HTTP: cfg.HTTP}
	fetched, err := client.Fetch(ctx, machine.Credential, state.ETag)
	if err != nil {
		return cfg.fail(state, notes, err)
	}
	if fetched.Unchanged {
		state.Error = ""
		state.SyncedAt = cfg.Now().UTC()
		res := Result{Unchanged: true, Version: state.Version, Written: make([]string, 0), Notes: state.Notes}
		if err := SaveState(cfg.StateDir, state); err != nil {
			res.Err = err
		}
		cfg.audit(state, res)
		return res
	}

	// Render every agent before writing any file, so one agent's bad
	// bundle cannot leave another's half applied.
	var planned []plannedFile
	for _, name := range machine.Agents {
		adapter, err := cfg.Registry.Lookup(name)
		if err != nil {
			return cfg.fail(state, notes, err)
		}
		renderer, ok := adapter.(agent.Renderer)
		if !ok {
			return cfg.fail(state, notes, fmt.Errorf("sync: agent %q has no renderer", name))
		}
		root, err := cfg.root(name)
		if err != nil {
			return cfg.fail(state, notes, err)
		}
		rendering, err := renderer.Render(fetched.Bundle)
		if err != nil {
			return cfg.fail(state, notes, fmt.Errorf("sync: rendering %s: %w", name, err))
		}
		notes = append(notes, rendering.Notes...)
		for _, f := range rendering.Files {
			planned = append(planned, plannedFile{path: filepath.Join(root, f.Path), content: f.Content, mode: f.Mode})
		}
	}

	version := fetched.Bundle.Version
	written := make([]string, 0, len(planned))
	files := make(map[string]string, len(planned))
	for _, p := range planned {
		if err := cache.ReplaceMode(p.path, p.content, p.mode); err != nil {
			// Each write is atomic, so what landed is whole; the next cycle
			// rewrites the rest. Report which file stopped this one.
			return cfg.fail(state, notes, fmt.Errorf("sync: writing %s: %w", p.path, err))
		}
		written = append(written, p.path)
		files[p.path] = hashOf(p.content)
		if strings.HasSuffix(p.path, ".json") {
			if err := cache.ReplaceMode(p.path+revisionSuffix, []byte(revisionLine(version)), 0o644); err != nil {
				return cfg.fail(state, notes, fmt.Errorf("sync: writing %s: %w", p.path+revisionSuffix, err))
			}
		}
	}

	if notes == nil {
		notes = make([]string, 0)
	}
	state = State{
		ETag:     fetched.ETag,
		Version:  version,
		SyncedAt: cfg.Now().UTC(),
		Files:    files,
		Notes:    notes,
	}
	res := Result{Version: version, Written: written, Notes: notes}
	if err := SaveState(cfg.StateDir, state); err != nil {
		res.Err = err
	}
	cfg.audit(state, res)
	return res
}

// fail records err in the state without touching the last good etag,
// version or file list, so `status` still describes what is on disk.
func (cfg Config) fail(state State, notes []string, err error) Result {
	state.Error = err.Error()
	if len(notes) > 0 {
		state.Notes = append(append([]string{}, state.Notes...), notes...)
	}
	res := Result{Version: state.Version, Written: make([]string, 0), Notes: state.Notes, Err: err}
	// A state write that fails is secondary to the failure being recorded;
	// the audit line still says what happened.
	_ = SaveState(cfg.StateDir, state)
	cfg.audit(state, res)
	return res
}

// root is the directory agentName's files go under: the configured override
// or the OS table.
func (cfg Config) root(agentName string) (string, error) {
	if dir, ok := cfg.Roots[agentName]; ok && dir != "" {
		return dir, nil
	}
	return AgentRoot(cfg.GOOS, agentName)
}

// revisionLine is the content of an .aw-revision sibling.
func revisionLine(version string) string {
	if version == "" {
		version = "none"
	}
	return version + "\n"
}

// hashOf is the hex SHA-256 of data, the form state.json records.
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// auditEntry is one line of the audit log: what a cycle did and why.
type auditEntry struct {
	Time      time.Time `json:"time"`
	Version   string    `json:"version,omitempty"`
	ETag      string    `json:"etag,omitempty"`
	Unchanged bool      `json:"unchanged,omitempty"`
	Written   []string  `json:"written,omitempty"`
	Error     string    `json:"error,omitempty"`
	Notes     []string  `json:"notes,omitempty"`
}

// audit appends one line to the log. It is best effort: a log that cannot
// be written must not fail a cycle that otherwise succeeded.
func (cfg Config) audit(state State, res Result) {
	path := filepath.Join(cfg.StateDir, AuditFile)
	if info, err := os.Stat(path); err == nil && info.Size() > auditRotateAt {
		_ = os.Rename(path, path+".1")
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	errText := ""
	if res.Err != nil {
		errText = res.Err.Error()
	}
	line, err := json.Marshal(auditEntry{
		Time:      cfg.Now().UTC(),
		Version:   state.Version,
		ETag:      state.ETag,
		Unchanged: res.Unchanged,
		Written:   res.Written,
		Error:     errText,
		Notes:     res.Notes,
	})
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/sync/ -count=1 && go vet ./internal/sync/ && gofmt -l .`
Expected: PASS, clean. If `TestRunWritesNothingWhenTheBundleFailsValidation` finds a directory entry, check that `cache.ReplaceMode` was not called before validation — nothing may be created under the root.

- [ ] **Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat(sync): the cycle: conditional fetch, render every agent, write all or nothing" -m "A failed fetch or a failed renderer records the error, keeps every file and the last good state, and exits non-zero for the timer to retry. Rendered files are never deleted." -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 6: `sync.Status` — last sync, drift, errors

**Files:**
- Create: `internal/sync/status.go`
- Test: `internal/sync/status_test.go`

**Interfaces:**
- Consumes: `LoadMachine`, `LoadState`, `hashOf`, `ErrNotEnrolled`.
- Produces:
  - `type FileStatus struct{ Path, State, Expected, Actual string }` (JSON `path`, `state`, `expected`, `actual,omitempty`); `State` is one of `"ok"`, `"drift"`, `"missing"`.
  - `type Report struct{ Enrolled bool; Server, MachineID string; Agents []string; SyncedAt time.Time; Version string; Error string; Notes []string; Files []FileStatus; Drift bool }` (JSON `enrolled`, `server,omitempty`, `machineId,omitempty`, `agents`, `syncedAt`, `version,omitempty`, `error,omitempty`, `notes`, `files`, `drift`)
  - `func Status(cfg Config) (Report, error)` — error only when a present `state.json` cannot be parsed; a missing enrollment yields `Enrolled: false` and no error.

- [ ] **Step 1: Write the failing tests**

Create `internal/sync/status_test.go`:

```go
package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/sync"
)

func synced(t *testing.T) (sync.Config, string) {
	t.Helper()
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	return cfg, root
}

func fileStates(r sync.Report) map[string]string {
	out := make(map[string]string, len(r.Files))
	for _, f := range r.Files {
		out[filepath.Base(f.Path)] = f.State
	}
	return out
}

func TestStatusAfterACleanSyncReportsEverythingOK(t *testing.T) {
	cfg, _ := synced(t)
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Enrolled || r.MachineID != "m1" || r.Version != "v1" || r.Error != "" || r.Drift {
		t.Errorf("report = %+v", r)
	}
	if !r.SyncedAt.Equal(cfg.Now()) {
		t.Errorf("syncedAt = %v", r.SyncedAt)
	}
	states := fileStates(r)
	if states["aw-bundle.json"] != "ok" || states["50-agent-wrapper.json"] != "ok" {
		t.Errorf("file states = %v", states)
	}
	paths := make([]string, 0, len(r.Files))
	for _, f := range r.Files {
		paths = append(paths, f.Path)
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("files are not sorted by path: %v", paths)
	}
}

func TestStatusReportsDrift(t *testing.T) {
	cfg, root := synced(t)
	if err := os.WriteFile(filepath.Join(root, claude.BundleFile), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Drift || fileStates(r)["aw-bundle.json"] != "drift" {
		t.Errorf("report = %+v", r)
	}
	for _, f := range r.Files {
		if f.State == "drift" && (f.Actual == "" || f.Actual == f.Expected) {
			t.Errorf("drifted file lacks a distinct actual hash: %+v", f)
		}
	}
}

func TestStatusReportsAMissingFile(t *testing.T) {
	cfg, root := synced(t)
	if err := os.Remove(filepath.Join(root, claude.DropInFile)); err != nil {
		t.Fatal(err)
	}
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Drift || fileStates(r)["50-agent-wrapper.json"] != "missing" {
		t.Errorf("report = %+v", r)
	}
}

func TestStatusWithoutEnrollmentIsNotAnError(t *testing.T) {
	r, err := sync.Status(sync.Config{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if r.Enrolled || r.Agents == nil || r.Files == nil || r.Notes == nil {
		t.Errorf("report = %+v, want Enrolled false and empty, non-nil lists", r)
	}
}

func TestStatusCarriesTheLastError(t *testing.T) {
	cfg, _ := synced(t)
	state, _ := sync.LoadState(cfg.StateDir)
	state.Error = "the control plane answered 500"
	if err := sync.SaveState(cfg.StateDir, state); err != nil {
		t.Fatal(err)
	}
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != "the control plane answered 500" {
		t.Errorf("error = %q", r.Error)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/sync/ -run Status -count=1`
Expected: FAIL, undefined `sync.Status`.

- [ ] **Step 3: Write `status.go`**

Create `internal/sync/status.go`:

```go
package sync

import (
	"errors"
	"os"
	"sort"
	"time"
)

// FileStatus is one rendered file compared with what the last cycle wrote.
type FileStatus struct {
	Path string `json:"path"`
	// State is "ok", "drift" (content differs) or "missing".
	State string `json:"state"`
	// Expected is the hash recorded at the last sync.
	Expected string `json:"expected"`
	// Actual is the hash of what is on disk now, when the file exists.
	Actual string `json:"actual,omitempty"`
}

// Report is what `aw-sync status` shows: enrollment, the last cycle, and
// every rendered file's drift.
type Report struct {
	Enrolled  bool         `json:"enrolled"`
	Server    string       `json:"server,omitempty"`
	MachineID string       `json:"machineId,omitempty"`
	Agents    []string     `json:"agents"`
	SyncedAt  time.Time    `json:"syncedAt"`
	Version   string       `json:"version,omitempty"`
	Error     string       `json:"error,omitempty"`
	Notes     []string     `json:"notes"`
	Files     []FileStatus `json:"files"`
	// Drift is true when any file is not as the last cycle left it. `once`
	// overwrites drift unconditionally; the files are root-owned, and a
	// user who can edit them already has root.
	Drift bool `json:"drift"`
}

// Status reads the state directory and checks every recorded file. It runs
// nothing and touches the network for nothing, so the developer's `aw
// doctor` can call it.
func Status(cfg Config) (Report, error) {
	r := Report{Agents: make([]string, 0), Notes: make([]string, 0), Files: make([]FileStatus, 0)}
	machine, err := LoadMachine(cfg.StateDir)
	switch {
	case err == nil:
		r.Enrolled = true
		r.Server = machine.Server
		r.MachineID = machine.MachineID
		r.Agents = machine.Agents
	case errors.Is(err, ErrNotEnrolled):
	default:
		return r, err
	}

	state, err := LoadState(cfg.StateDir)
	if err != nil {
		return r, err
	}
	r.SyncedAt = state.SyncedAt
	r.Version = state.Version
	r.Error = state.Error
	if state.Notes != nil {
		r.Notes = state.Notes
	}

	paths := make([]string, 0, len(state.Files))
	for path := range state.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fs := FileStatus{Path: path, Expected: state.Files[path]}
		raw, err := os.ReadFile(path)
		switch {
		case err != nil:
			fs.State = "missing"
		case hashOf(raw) == fs.Expected:
			fs.State = "ok"
			fs.Actual = fs.Expected
		default:
			fs.State = "drift"
			fs.Actual = hashOf(raw)
		}
		if fs.State != "ok" {
			r.Drift = true
		}
		r.Files = append(r.Files, fs)
	}
	return r, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/sync/ -count=1 && go vet ./internal/sync/ && gofmt -l .`
Expected: PASS, clean.

- [ ] **Step 5: Commit**

```bash
git add internal/sync/status.go internal/sync/status_test.go
git commit -m "feat(sync): status report with per-file drift" -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 7: `cmd/aw-sync` and the end-to-end test

**Files:**
- Create: `cmd/aw-sync/main.go`
- Test: `cmd/aw-sync/e2e_test.go`
- Modify: `.gitignore` (add `/aw-sync` under "Built binaries")

**Interfaces:**
- Consumes: `sync.Client.Enroll`, `sync.SaveMachine`, `sync.LoadMachine`, `sync.Run`, `sync.Status`, `sync.StateDir`, `claude.Adapter`, `agent.Registry`.
- Produces the CLI:
  ```
  aw-sync enroll --server URL --token T [--name HOST] [--agents claude] [--force] [--state-dir DIR]
  aw-sync once [--state-dir DIR] [--root agent=DIR ...]
  aw-sync status [--json] [--state-dir DIR]
  aw-sync help
  ```
  `--token` falls back to `AW_SYNC_TOKEN`. `--state-dir` defaults to `sync.StateDir(runtime.GOOS)`. `once` exits 1 when `Result.Err != nil`. `enroll` refuses an existing enrollment without `--force`.

- [ ] **Step 1: Write the failing end-to-end test**

Create `cmd/aw-sync/e2e_test.go`:

```go
package main_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

var (
	builtSync string
	builtAwd  string
)

const adminToken = "e2e-admin-token"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-sync-e2e-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	builtSync = filepath.Join(dir, "aw-sync")
	builtAwd = filepath.Join(dir, "awd")
	if out, err := exec.Command("go", "build", "-o", builtSync, ".").CombinedOutput(); err != nil {
		panic("building aw-sync: " + err.Error() + "\n" + string(out))
	}
	if out, err := exec.Command("go", "build", "-o", builtAwd, "../awd").CombinedOutput(); err != nil {
		panic("building awd: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

type server struct {
	url  string
	cmd  *exec.Cmd
	done chan error
	stop func()
}

// startAwd runs the control plane on an ephemeral port with an admin token
// and the in-memory store. stop() ends it early, for the outage test.
func startAwd(t *testing.T) *server {
	t.Helper()
	cmd := exec.Command(builtAwd, "serve")
	cmd.Env = append(os.Environ(), "AWD_ADDR=127.0.0.1:0", "AWD_LOG_LEVEL=error", "AWD_ADMIN_TOKEN="+adminToken)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the listen address: %v", err)
	}
	address := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "listening on "))
	s := &server{url: "http://" + address, cmd: cmd, done: done}
	var once bool
	s.stop = func() {
		if once {
			return
		}
		once = true
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		io.Copy(io.Discard, stdout)
	}
	t.Cleanup(s.stop)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.url + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("awd did not become healthy")
	return nil
}

// admin sends one authenticated request to awd and decodes the JSON reply.
func admin(t *testing.T, s *server, method, path string, body any, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, s.url+path, reader)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s: %s: %s", method, path, resp.Status, payload)
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			t.Fatalf("decoding %s: %v\n%s", path, err, payload)
		}
	}
}

func runSync(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(builtSync, args...)
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errOut.String(), 0
	case errors.As(err, &exitErr):
		return out.String(), errOut.String(), exitErr.ExitCode()
	default:
		t.Fatalf("running aw-sync: %v", err)
		return "", "", 0
	}
}

const e2ePolicy = `{
  "version": "2026-09-21.e2e",
  "groups": {"alice@acme.com": ["platform"]},
  "rules": [
    {"name": "baseline", "agents": {"claude": {"managed": {"model": "sonnet"}}}},
    {"name": "platform", "match": {"groups": ["platform"]}, "agents": {"claude": {"managed": {"model": "opus"}}}},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {"claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}}}}
  ]
}`

func applyPolicy(t *testing.T, s *server) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(e2ePolicy), &doc); err != nil {
		t.Fatal(err)
	}
	// The body is the bare rule set; awd rejects unknown fields.
	admin(t, s, http.MethodPost, "/v1/policy/revisions", doc, nil)
}

func mintToken(t *testing.T, s *server, user string) string {
	t.Helper()
	var out struct{ Token string }
	admin(t, s, http.MethodPost, "/v1/enrollment-tokens", map[string]string{"user": user}, &out)
	if out.Token == "" {
		t.Fatal("no token minted")
	}
	return out.Token
}

func TestEnrollOnceStatusAndOutage(t *testing.T) {
	s := startAwd(t)
	applyPolicy(t, s)
	stateDir := filepath.Join(t.TempDir(), "state")
	claudeRoot := filepath.Join(t.TempDir(), "claude-root")

	// Enroll, with the token in the environment so it never hits argv.
	token := mintToken(t, s, "alice@acme.com")
	stdout, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=" + token},
		"enroll", "--server", s.url, "--name", "e2e-host", "--agents", "claude", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("enroll exited %d: %s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "alice@acme.com") {
		t.Errorf("enroll output does not name the user: %q", stdout)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(stateDir, "machine.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("machine.json mode = %o, want 0600", info.Mode().Perm())
		}
	}

	// A second enrollment is refused without --force, and the token is
	// single-use anyway.
	_, stderr, code = runSync(t, []string{"AW_SYNC_TOKEN=" + token},
		"enroll", "--server", s.url, "--agents", "claude", "--state-dir", stateDir)
	if code == 0 || !strings.Contains(stderr, "--force") {
		t.Errorf("re-enroll: exit %d, stderr %q; want a refusal naming --force", code, stderr)
	}

	// The first cycle renders both Claude files.
	stdout, stderr, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot)
	if code != 0 {
		t.Fatalf("once exited %d: %s%s", code, stdout, stderr)
	}
	bundlePath := filepath.Join(claudeRoot, "aw-bundle.json")
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Version string `json:"version"`
		Groups  []string
		Rules   []struct{ Name string }
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Version != "2026-09-21.e2e" || len(bundle.Rules) != 3 || len(bundle.Groups) != 1 {
		t.Errorf("bundle = %+v; want the platform group resolved and all three Claude rules with repo matchers intact", bundle)
	}
	if _, err := os.Stat(filepath.Join(claudeRoot, "managed-settings.d", "50-agent-wrapper.json")); err != nil {
		t.Errorf("drop-in not written: %v", err)
	}
	if got, _ := os.ReadFile(bundlePath + ".aw-revision"); string(got) != "2026-09-21.e2e\n" {
		t.Errorf("aw-revision = %q", got)
	}

	// A second cycle is a 304 no-op.
	stdout, _, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot)
	if code != 0 || !strings.Contains(stdout, "unchanged") {
		t.Errorf("second once: exit %d, stdout %q; want an unchanged report", code, stdout)
	}

	// Status is clean and machine-readable.
	stdout, _, code = runSync(t, nil, "status", "--json", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("status exited %d", code)
	}
	var report struct {
		Enrolled bool
		Version  string
		Drift    bool
		Error    string
		Files    []struct{ Path, State string }
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, stdout)
	}
	if !report.Enrolled || report.Version != "2026-09-21.e2e" || report.Drift || report.Error != "" || len(report.Files) != 2 {
		t.Errorf("report = %+v", report)
	}

	// Drift is reported, then overwritten by the next cycle.
	if err := os.WriteFile(bundlePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ = runSync(t, nil, "status", "--state-dir", stateDir)
	if !strings.Contains(stdout, "drift") {
		t.Errorf("status does not report drift:\n%s", stdout)
	}

	// The server goes away: the cycle fails, the files stay.
	s.stop()
	stdout, stderr, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot)
	if code != 1 {
		t.Errorf("once during an outage exited %d, want 1: %s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(claudeRoot, "managed-settings.d", "50-agent-wrapper.json")); err != nil {
		t.Error("an outage removed the drop-in")
	}
	if got, _ := os.ReadFile(bundlePath); string(got) != "{}\n" {
		t.Error("an outage rewrote the bundle; a failed fetch must leave files as they are")
	}
	stdout, _, _ = runSync(t, nil, "status", "--json", "--state-dir", stateDir)
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Error == "" || report.Version != "2026-09-21.e2e" {
		t.Errorf("after the outage report = %+v; want the error recorded and the version kept", report)
	}
}

func TestOnceWithoutEnrollmentExits1(t *testing.T) {
	_, stderr, code := runSync(t, nil, "once", "--state-dir", t.TempDir())
	if code != 1 || !strings.Contains(stderr, "enroll") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestEnrollRejectsAnAgentWithoutARenderer(t *testing.T) {
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude,copilot", "--state-dir", t.TempDir())
	if code != 1 || !strings.Contains(stderr, "copilot") {
		t.Errorf("exit %d, stderr %q; want a refusal naming copilot before any network call", code, stderr)
	}
}

func TestHelpListsTheCommands(t *testing.T) {
	stdout, _, code := runSync(t, nil, "help")
	if code != 0 {
		t.Fatalf("help exited %d", code)
	}
	for _, want := range []string{"enroll", "once", "status", "AW_SYNC_TOKEN"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/aw-sync/ -count=1`
Expected: FAIL (the package has no `main.go` to build).

- [ ] **Step 3: Write `main.go`**

Create `cmd/aw-sync/main.go`:

```go
// Command aw-sync keeps a machine's agent configuration files in step with
// the control plane. It runs as root from a timer, not as a daemon: `once`
// is one cycle, restart-safe, which is what MDM tooling expects.
//
//	aw-sync enroll --server URL --token T    exchange an enrollment token for this machine's credential
//	aw-sync once                             fetch the bundle and render every enrolled agent's files
//	aw-sync status                           last sync, bundle version, per-file drift, last error
//
// The rendered files are never deleted on failure: a machine that cannot
// reach the control plane keeps the policy it last received.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/sync"
)

const usage = `aw-sync keeps this machine's agent configuration in step with the control plane.

Usage:
  aw-sync enroll --server URL [--token T] [--name HOST] [--agents claude] [--force] [--state-dir DIR]
  aw-sync once [--state-dir DIR] [--root agent=DIR ...]
  aw-sync status [--json] [--state-dir DIR]
  aw-sync help

Environment:
  AW_SYNC_TOKEN   the enrollment token, so it need not appear on the command line

--state-dir defaults to the OS state directory (/var/lib/agent-wrapper on Linux).
--root overrides where one agent's files are written and exists for testing.
once exits 1 when the cycle fails; the files on disk are left as they were.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "aw-sync: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string, stdout io.Writer) error {
	if len(argv) == 0 {
		fmt.Fprint(stdout, usage)
		return nil
	}
	switch argv[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	case "enroll":
		return enroll(argv[1:], stdout)
	case "once":
		return once(argv[1:], stdout)
	case "status":
		return status(argv[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q; run `aw-sync help`", argv[0])
	}
}

// newRegistry holds the adapters this binary can sync. Only those that
// implement agent.Renderer are offered to enroll.
func newRegistry() (*agent.Registry, error) {
	reg := &agent.Registry{}
	if err := reg.Register(claude.New()); err != nil {
		return nil, err
	}
	return reg, nil
}

// renderable lists the registered adapters that render files.
func renderable(reg *agent.Registry) []string {
	out := make([]string, 0)
	for _, name := range reg.Names() {
		if a, err := reg.Lookup(name); err == nil {
			if _, ok := a.(agent.Renderer); ok {
				out = append(out, name)
			}
		}
	}
	return out
}

// rootFlags collects repeated --root agent=DIR flags.
type rootFlags map[string]string

func (r rootFlags) String() string { return fmt.Sprint(map[string]string(r)) }

func (r rootFlags) Set(value string) error {
	name, dir, ok := strings.Cut(value, "=")
	if !ok || name == "" || dir == "" {
		return fmt.Errorf("--root wants agent=DIR, got %q", value)
	}
	r[name] = dir
	return nil
}

func newFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stateDir := fs.String("state-dir", sync.StateDir(runtime.GOOS), "where machine.json and state.json live")
	return fs, stateDir
}

func enroll(argv []string, stdout io.Writer) error {
	fs, stateDir := newFlagSet("enroll")
	server := fs.String("server", "", "control plane URL")
	token := fs.String("token", os.Getenv("AW_SYNC_TOKEN"), "enrollment token (or AW_SYNC_TOKEN)")
	name := fs.String("name", "", "this machine's name (default: hostname)")
	agents := fs.String("agents", "", "comma-separated agents to sync (default: every renderable adapter)")
	force := fs.Bool("force", false, "replace an existing enrollment")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("--server is required")
	}
	if *token == "" {
		return errors.New("--token or AW_SYNC_TOKEN is required")
	}
	reg, err := newRegistry()
	if err != nil {
		return err
	}
	selected := renderable(reg)
	if *agents != "" {
		selected = strings.Split(*agents, ",")
		for i, a := range selected {
			selected[i] = strings.TrimSpace(a)
			adapter, err := reg.Lookup(selected[i])
			if err != nil {
				return fmt.Errorf("agent %q: %w", selected[i], err)
			}
			if _, ok := adapter.(agent.Renderer); !ok {
				return fmt.Errorf("agent %q cannot be synced: no renderer", selected[i])
			}
		}
	}
	if existing, err := sync.LoadMachine(*stateDir); err == nil && !*force {
		return fmt.Errorf("already enrolled as machine %s against %s; pass --force to re-enroll", existing.MachineID, existing.Server)
	}
	if *name == "" {
		if host, err := os.Hostname(); err == nil {
			*name = host
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &sync.Client{Server: *server}
	e, err := client.Enroll(ctx, *token, *name, runtime.GOOS)
	if err != nil {
		return err
	}
	machine := sync.Machine{Server: *server, MachineID: e.MachineID, Credential: e.Credential, Agents: selected}
	if err := sync.SaveMachine(*stateDir, machine); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "enrolled machine %s for %s; syncing %s into %s\n", e.MachineID, e.User, strings.Join(selected, ", "), *stateDir)
	return nil
}

func once(argv []string, stdout io.Writer) error {
	fs, stateDir := newFlagSet("once")
	roots := rootFlags{}
	fs.Var(roots, "root", "agent=DIR override for one agent's system directory")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	reg, err := newRegistry()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res := sync.Run(ctx, sync.Config{StateDir: *stateDir, Roots: roots, Registry: reg})
	for _, note := range res.Notes {
		fmt.Fprintln(stdout, "note: "+note)
	}
	if res.Err != nil {
		return res.Err
	}
	switch {
	case res.Unchanged:
		fmt.Fprintf(stdout, "unchanged: bundle %s is current\n", orNone(res.Version))
	default:
		fmt.Fprintf(stdout, "synced bundle %s: wrote %d files\n", orNone(res.Version), len(res.Written))
		for _, path := range res.Written {
			fmt.Fprintln(stdout, "  "+path)
		}
	}
	return nil
}

func status(argv []string, stdout io.Writer) error {
	fs, stateDir := newFlagSet("status")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	report, err := sync.Status(sync.Config{StateDir: *stateDir})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if !report.Enrolled {
		fmt.Fprintf(stdout, "not enrolled: no machine.json in %s; run `aw-sync enroll`\n", *stateDir)
		return nil
	}
	fmt.Fprintf(stdout, "machine %s against %s, agents %s\n", report.MachineID, report.Server, strings.Join(report.Agents, ", "))
	synced := "never"
	if !report.SyncedAt.IsZero() {
		synced = report.SyncedAt.Format(time.RFC3339)
	}
	fmt.Fprintf(stdout, "last sync %s, bundle %s\n", synced, orNone(report.Version))
	if report.Error != "" {
		fmt.Fprintf(stdout, "last error: %s\n", report.Error)
	}
	for _, note := range report.Notes {
		fmt.Fprintln(stdout, "note: "+note)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, f := range report.Files {
		fmt.Fprintf(tw, "%s\t%s\n", f.State, f.Path)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if report.Drift {
		fmt.Fprintln(stdout, "drift: the next `aw-sync once` rewrites every file")
	}
	return nil
}

func orNone(version string) string {
	if version == "" {
		return "(none)"
	}
	return version
}
```

Add `/aw-sync` to `.gitignore` under `# Built binaries`.

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/aw-sync/ -count=1 -v`
Expected: PASS for all four tests. Then `go test ./... && go vet ./... && gofmt -l .` and `GOOS=darwin go build ./... && GOOS=windows go build ./...`.

If `TestEnrollOnceStatusAndOutage` fails at the bundle assertion with fewer than three rules, check that the fake policy's `groups` resolved `alice@acme.com` to `platform`: the bundle must contain `baseline`, `platform` (group-matched) and `payments` (repo-scoped, kept verbatim).

- [ ] **Step 5: Commit**

```bash
git add cmd/aw-sync/ .gitignore
git commit -m "feat(aw-sync): enroll, once and status commands with an end-to-end test against awd" -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 8: Timer units and install notes

**Files:**
- Create: `deploy/aw-sync/systemd/aw-sync.service`, `deploy/aw-sync/systemd/aw-sync.timer`, `deploy/aw-sync/launchd/com.agent-wrapper.aw-sync.plist`, `deploy/aw-sync/windows/register-task.ps1`, `deploy/aw-sync/README.md`
- Test: `cmd/aw-sync/deploy_test.go`

**Interfaces:**
- Consumes: the `aw-sync once` command from Task 7.
- Produces: nothing code-level; a test pins that every unit invokes `aw-sync once`.

- [ ] **Step 1: Write the failing test**

Create `cmd/aw-sync/deploy_test.go`:

```go
package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The timer units are what an organization installs on every machine. One
// that does not run `aw-sync once` syncs nothing, silently, fleet-wide.
func TestEveryTimerUnitRunsOnce(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "aw-sync")
	units := map[string]string{
		"systemd/aw-sync.service":                 "aw-sync once",
		"systemd/aw-sync.timer":                   "OnUnitActiveSec",
		"launchd/com.agent-wrapper.aw-sync.plist": "<string>once</string>",
		"windows/register-task.ps1":               "once",
	}
	for rel, want := range units {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s does not contain %q", rel, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Errorf("deploy/aw-sync/README.md: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/aw-sync/ -run TimerUnit -count=1`
Expected: FAIL, files do not exist.

- [ ] **Step 3: Write the units**

`deploy/aw-sync/systemd/aw-sync.service`:

```ini
[Unit]
Description=Sync agent governance files from the agent-wrapper control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aw-sync once
# The state directory holds the machine credential; nothing else needs it.
StateDirectory=agent-wrapper
StateDirectoryMode=0755
```

`deploy/aw-sync/systemd/aw-sync.timer`:

```ini
[Unit]
Description=Run aw-sync every five minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min
RandomizedDelaySec=60
Persistent=true

[Install]
WantedBy=timers.target
```

`deploy/aw-sync/launchd/com.agent-wrapper.aw-sync.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.agent-wrapper.aw-sync</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/aw-sync</string>
        <string>once</string>
    </array>
    <key>StartInterval</key>
    <integer>300</integer>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Library/Logs/agent-wrapper/aw-sync.log</string>
    <key>StandardErrorPath</key>
    <string>/Library/Logs/agent-wrapper/aw-sync.log</string>
</dict>
</plist>
```

`deploy/aw-sync/windows/register-task.ps1`:

```powershell
# Registers aw-sync as a scheduled task that runs `aw-sync once` as SYSTEM
# every five minutes and at startup. Run from an elevated PowerShell.
$exe = 'C:\Program Files\AgentWrapper\aw-sync.exe'
$action = New-ScheduledTaskAction -Execute $exe -Argument 'once'
$interval = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 5)
$startup = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName 'agent-wrapper\aw-sync' -Action $action -Trigger @($interval, $startup) -Principal $principal -Settings $settings -Force
```

`deploy/aw-sync/README.md`:

````markdown
# Installing aw-sync

aw-sync runs as root from a timer. Each run fetches this machine's policy
bundle and rewrites every enrolled agent's governance files atomically; a
failed run leaves the files as they were and exits 1 for the timer to retry.

## 1. Mint an enrollment token

On a machine with `AWD_ADMIN_TOKEN`:

    awd enroll-token alice@acme.com --url https://awd.example.com

The token is single-use and expires in 24 hours by default.

## 2. Install the binary and enroll

Linux and macOS:

    install -m 0755 aw-sync /usr/local/bin/aw-sync
    AW_SYNC_TOKEN=<token> sudo -E aw-sync enroll --server https://awd.example.com

Windows (elevated): copy `aw-sync.exe` to `C:\Program Files\AgentWrapper\`
and run `aw-sync.exe enroll --server https://awd.example.com --token <token>`.

`--agents claude,codex` limits the sync to the agents installed here; the
default is every agent this build can render. Re-enrolling needs `--force`.

## 3. Install the timer

Linux (systemd):

    install -m 0644 systemd/aw-sync.service systemd/aw-sync.timer /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable --now aw-sync.timer

macOS (launchd):

    install -d -m 0755 /Library/Logs/agent-wrapper
    install -m 0644 launchd/com.agent-wrapper.aw-sync.plist /Library/LaunchDaemons/
    launchctl bootstrap system /Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist

Windows (Task Scheduler), from an elevated PowerShell:

    .\windows\register-task.ps1

## 4. Verify

    sudo aw-sync once
    aw-sync status

`status` shows the last sync, the bundle version, every rendered file with
`ok`, `drift` or `missing`, and the last error. It runs as any user, so
developers can check it too.

## Where things live

| | Linux | macOS | Windows |
| --- | --- | --- | --- |
| State (`machine.json` 0600, `state.json` 0644, `aw-sync.log`) | `/var/lib/agent-wrapper/` | `/Library/Application Support/agent-wrapper/` | `C:\ProgramData\agent-wrapper\` |
| Claude Code | `/etc/claude-code/` | `/Library/Application Support/ClaudeCode/` | `C:\Program Files\ClaudeCode\` |

Rendered files are root-owned and world-readable. A user who can edit them
already has root; `once` rewrites drift unconditionally.

## Revoking a machine

    awd revoke <machine-id> --url https://awd.example.com

The machine's next `once` gets 401, records the error, exits 1, and keeps
its files. Re-enroll it with a new token and `--force`.
````

- [ ] **Step 4: Run the test**

Run: `go test ./cmd/aw-sync/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add deploy/aw-sync/ cmd/aw-sync/deploy_test.go
git commit -m "build(aw-sync): timer units for systemd, launchd and Task Scheduler, with install notes" -m "Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

## Self-review

**Spec coverage (M4b sections):**
- Claude renderer: `aw-bundle.json` narrowed with repo matchers intact, static drop-in with the OS helper path, `refreshIntervalMs` 300000 — Task 2. Drop-in equality with `deploy/managed-settings/` — Task 2 test.
- `agent.Renderer` optional interface, relative paths, renderers never touch disk — Task 2 (validated by `TestRenderedFilesAreReadableByEveryone`).
- `aw-sync enroll` (`--server`, `--token`, `--name`, `--agents`, `--force`, `machine.json` 0600, agents default to every renderable adapter) — Tasks 3, 4, 7.
- `aw-sync once` cycle steps 1–6, exit codes, never delete on failure, all-or-nothing, `.aw-revision` siblings, `state.json` shape, audit line — Task 5.
- `aw-sync status [--json]`: last sync, version, per-file hashes, drift, last error, notes — Tasks 6, 7.
- Paths table keyed by GOOS in one place — Task 3 (`paths.go`, all three agents so M4d/M4e only add renderers).
- Timer units under `deploy/aw-sync/`; `install-timer` deferred — Task 8.
- Failure modes table rows for aw-sync — Tasks 5, 7 (tests: 500, unreachable, 401, renderer failure, not enrolled).
- Testing section: renderer golden equality, sync tests (fake awd, all-or-nothing, 304 no-op, failed fetch, drift, enrollment 0600), end to end minus `aw-policy` (which stays networked until M4c) — Tasks 2, 5, 6, 7.
- TOML header comments: no TOML file exists in M4b; M4d adds the header with the Codex renderer.
- `aw doctor` bundle finding and README: M4c, as the spec sequences it.

**Placeholder scan:** no TBD/TODO; every code step carries its code; no "similar to Task N".

**Type consistency:** `agent.Rendering{Files, Notes}` and `agent.File{Path, Content, Mode}` (Task 2) are what Task 5 consumes; `sync.Machine`/`State`/`LoadMachine`/`SaveMachine`/`LoadState`/`SaveState`/`ErrNotEnrolled`/`MachineFile`/`StateFile`/`AuditFile` (Task 3) match Tasks 5–7; `Client.Enroll(ctx, token, name, goos)` and `Client.Fetch(ctx, credential, etag) (Fetched, error)` (Task 4) match Tasks 5 and 7; `Config{StateDir, GOOS, Roots, Registry, HTTP, Now}` and `Result{Unchanged, Version, Written, Notes, Err}` (Task 5) match Tasks 6 and 7; `hashOf` and `cfg.root` are unexported helpers in the same package; `claude.BundleFile`/`DropInFile` (Task 2) are used by Tasks 5–7; `claude.Adapter.GOOS` (Task 2) is set in Task 5's tests.
