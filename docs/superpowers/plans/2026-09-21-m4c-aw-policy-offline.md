# M4c: aw-policy offline, doctor bundle finding — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aw-policy` no longer talks to the network: it reads the `aw-bundle.json` that `aw-sync` left in Claude Code's system directory, resolves the session's repository against it, validates and emits; `aw doctor` reports the bundle and aw-sync's state; the READMEs describe the new shape.

**Architecture:** `policyhelper.Run` loses HTTP, ETag and the per-subject cache and gains a bundle loader plus `bundle.Compile(repo)`. `cmd/aw-policy` keeps `AW_POLICY_CONFIG`, adds `AW_POLICY_BUNDLE`, and its config file shrinks to `{"requireBundle": bool}`. The Claude adapter's `Inspect` adds one finding about the bundle file it owns; `aw doctor` adds a `sync` section built from `sync.Status`, because sync facts (last sync, age, error, drift) are machine-wide, not per agent. The M3 fake-server tests go with the code they tested.

**Tech Stack:** Go 1.22, stdlib only. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` — sections "aw-policy, offline", "Failure modes" (aw-policy rows), "Testing" (end to end; M3 fake-server tests removed), "Sequencing (M4c)".

## Spec refinements

Decisions the spec leaves open, settled here so every task agrees:

1. **Doctor split.** The spec says "aw doctor's Claude inspection adds the bundle: present, version, age from aw-sync's state.json, and aw-sync's last error." The Claude adapter reports what it owns (bundle present / unparseable / version and rule count) as a finding. Age, last error and drift come from `sync.Status` and are printed once, in a new top-level `sync` section of doctor, not per agent: they are facts about the machine, and an adapter importing `internal/sync` would invert the layering (sync consumes adapters). Both appear in the same `aw doctor` output, which is what the spec wants a reader to see.
2. **`Run` signature.** `policyhelper.Run(cfg Config) Result` — no `context.Context`, since nothing in the run blocks any more. `Config.Timeout`, `DefaultTimeout`, `HTTPClient`, `ServerURL`, `Groups`, `ClaudeCodeVersion` are deleted. `CacheDir` becomes `AuditDir`: the only thing left in that directory is the audit log.
3. **Sources.** `SourceServer` and `SourceCache` are replaced by `SourceBundle`; `SourceNone` stays. The audit line's `source` field carries `"bundle"` or `"none"`.
4. **Groups.** The bundle carries the groups it was cut for; the audit line records those. There is no groups configuration in `aw-policy.json` any more.
5. **Config file is optional.** A missing `aw-policy.json` produces no note. An unreadable or invalid one produces a note and the defaults (`requireBundle: false`).
6. **Bundle size guard.** The bundle file is read through a 4 MiB limit (`aw-sync` never writes more); the 1 MiB guard on the emitted envelope stays as it was.
7. **State-dir override for `aw`.** `aw doctor` reads `sync.StateDir(runtime.GOOS)` unless `AW_SYNC_STATE_DIR` is set, which exists for tests and for reading a state directory copied from another machine.
8. **`claude.SystemDir(goos)`** is exported so `cmd/aw-policy` and the adapter agree on the directory from one table.

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run bare `go mod tidy` (it pulls test deps needing Go 1.25); no new dependencies are needed for this plan.
- No `gcc` on this machine: `go test -race` cannot run here. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit. Also `GOOS=darwin go build ./... && GOOS=windows go build ./...` for any task touching `cmd/` or `internal/agent/claude`.
- **`aw-policy` always prints a complete JSON envelope and exits 0** unless `requireBundle` is set and no usable bundle was found; then it still prints `{}` and exits 1. Never emit a `managedSettings` object that fails `schema.Validate`. Never exceed 1 MiB on stdout or 16 KiB on stderr.
- `aw-policy` makes no network request. No `net/http` import anywhere under `internal/policyhelper` or `cmd/aw-policy` after Task 2.
- File-permission assertions skip on Windows: `if runtime.GOOS == "windows" { t.Skip("POSIX modes") }`.
- Return `make([]T, 0)`, never a nil slice, from anything that is JSON-encoded as a list.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv`.
- Match the codebase's comment style: a comment says *why*, in full sentences, and is present on every exported identifier and on every struct field.
- Model tiering for executors: Tasks 1, 2, 3, 6 are transcription (cheapest tier); Tasks 4 and 5 are small multi-file integration (mid tier); the final whole-branch review is the most capable tier.

---

## File map

| File | Responsibility after this plan |
| --- | --- |
| `internal/policyhelper/helper.go` | Offline `Run`: load bundle, compile for the repo, validate, envelope, 1 MiB guard, panic recovery. |
| `internal/policyhelper/audit.go` | Unchanged shape; reads `cfg.AuditDir`. |
| `internal/policyhelper/helper_test.go` | Rewritten: bundle fixtures on disk, no HTTP. |
| `cmd/aw-policy/main.go` | Reads `aw-policy.json` (`requireBundle` only), `AW_POLICY_CONFIG`, `AW_POLICY_BUNDLE`; calls `Run`. |
| `cmd/aw-policy/e2e_test.go` | Rewritten: builds the binary, runs it against a bundle file. |
| `internal/agent/claude/inspect.go` | Exports `SystemDir(goos)`; `Inspect` adds the bundle finding. |
| `internal/agent/claude/inspect_test.go` | Three bundle-finding tests. |
| `cmd/aw/main.go` | Doctor `sync` section from `sync.Status`; `AW_SYNC_STATE_DIR`. |
| `cmd/aw/doctor_test.go` | New: unit tests for `syncSection`. |
| `cmd/aw/e2e_test.go` | One test: doctor `--json` carries the `sync` section. |
| `cmd/aw-sync/e2e_test.go` | Builds `aw-policy` too; runs it against the rendered bundle in and out of a repository, and during the outage. |
| `deploy/managed-settings/aw-policy.json` | `{"requireBundle": false}`. |
| `deploy/managed-settings/README.md` | Rewritten for the aw-sync-owned drop-in and the optional config. |
| `README.md` | Helper paragraph, layout, try-it section updated. |

---

### Task 1: `policyhelper` reads the bundle instead of the network

**Files:**
- Modify: `internal/policyhelper/helper.go` (replace whole file)
- Modify: `internal/policyhelper/audit.go` (one field rename)
- Modify: `internal/policyhelper/helper_test.go` (replace whole file)

**Interfaces:**
- Consumes: `policy.Bundle` and `(*policy.Bundle).Compile(repo string) *policy.Document` from `internal/policy/bundle.go`; `schema.Validate(any) error`; `repo.Detect(dir string) (string, error)`.
- Produces: `policyhelper.Config{BundlePath, WorkDir, RepoOverride, AuditDir string; RequireBundle bool; Now func() time.Time}`, `policyhelper.Run(cfg Config) Result`, `Result{Output []byte; ExitCode int; Source Source; Version string; Notes []string}`, `SourceBundle`, `SourceNone`. Task 2 and Task 5 rely on these names exactly.

- [ ] **Step 1: Replace the test file**

Write `internal/policyhelper/helper_test.go` with exactly this content:

```go
package policyhelper_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/policyhelper"
)

// bundle is what aw-sync leaves for a user in the platform group: a
// baseline rule and one scoped to the payments repositories, with the
// repository matcher intact for the helper to resolve per launch.
const bundle = `{
  "version": "2026-09-21.1",
  "groups": ["platform"],
  "rules": [
    {
      "name": "baseline",
      "agents": {"claude": {
        "managed": {"permissions": {"deny": ["Read(./.env)"]}, "allowManagedPermissionRulesOnly": true},
        "env": {"CLAUDE_CODE_ENABLE_TELEMETRY": "1"}
      }}
    },
    {
      "name": "payments",
      "match": {"repos": ["github.com/acme/payments*"]},
      "agents": {"claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}}}
    }
  ]
}`

func writeBundle(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aw-bundle.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func config(t *testing.T, bundlePath string) policyhelper.Config {
	t.Helper()
	return policyhelper.Config{
		BundlePath: bundlePath,
		AuditDir:   t.TempDir(),
		WorkDir:    t.TempDir(), // not a repository
	}
}

func envelope(t *testing.T, r policyhelper.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Output, &out); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, r.Output)
	}
	return out
}

func managedOf(t *testing.T, r policyhelper.Result) map[string]any {
	t.Helper()
	managed, ok := envelope(t, r)["managedSettings"].(map[string]any)
	if !ok {
		t.Fatalf("output has no managedSettings object:\n%s", r.Output)
	}
	return managed
}

// denies returns permissions.deny as strings, or nothing when it is absent.
func denies(t *testing.T, managed map[string]any) []string {
	t.Helper()
	permissions, _ := managed["permissions"].(map[string]any)
	raw, _ := permissions["deny"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func hasNote(r policyhelper.Result, text string) bool {
	return strings.Contains(strings.Join(r.Notes, "\n"), text)
}

func TestTheBundleIsCompiledAndEmittedAsManagedSettings(t *testing.T) {
	r := policyhelper.Run(config(t, writeBundle(t, bundle)))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; notes %q", r.ExitCode, r.Notes)
	}
	if r.Source != policyhelper.SourceBundle || r.Version != "2026-09-21.1" {
		t.Errorf("source = %q version = %q, want bundle and 2026-09-21.1", r.Source, r.Version)
	}
	managed := managedOf(t, r)
	if got := denies(t, managed); len(got) != 1 || got[0] != "Read(./.env)" {
		t.Errorf("deny = %q; outside a repository only the baseline rule applies", got)
	}
	if managed["allowManagedPermissionRulesOnly"] != true {
		t.Errorf("managed settings lost a key: %v", managed)
	}
	env, _ := managed["env"].(map[string]any)
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("the policy's env was not carried into managed settings: %v", managed)
	}
}

func TestTheRepositorySelectsItsRules(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.RepoOverride = "github.com/acme/payments-api"

	r := policyhelper.Run(cfg)

	if got := strings.Join(denies(t, managedOf(t, r)), ","); got != "Read(./.env),Bash(curl *)" {
		t.Errorf("deny = %q; want the baseline and payments rules merged in order", got)
	}
}

func TestTheRepositoryIsDetectedFromTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = git@github.com:acme/payments-api.git\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config(t, writeBundle(t, bundle))
	cfg.WorkDir = root

	r := policyhelper.Run(cfg)

	if got := denies(t, managedOf(t, r)); len(got) != 2 {
		t.Errorf("deny = %q; the payments rule should apply inside its repository", got)
	}
}

func TestWithoutABundleTheEnvelopeIsEmpty(t *testing.T) {
	// A machine aw-sync has not reached yet is governed by the static
	// managed-settings files alone. An envelope without managedSettings
	// leaves them in force; that is the safest thing to do, and it is
	// said out loud in the notes.
	r := policyhelper.Run(config(t, filepath.Join(t.TempDir(), "absent.json")))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if r.Source != policyhelper.SourceNone || string(r.Output) != "{}\n" {
		t.Errorf("source = %q output = %q, want none and an empty envelope", r.Source, r.Output)
	}
	if !hasNote(r, "no policy available") || !hasNote(r, "absent.json") {
		t.Errorf("notes %q should say there is no policy and name the file", r.Notes)
	}
}

func TestAnUnparseableBundleIsNotedAndNotEmitted(t *testing.T) {
	r := policyhelper.Run(config(t, writeBundle(t, "{not json")))

	if r.ExitCode != 0 || string(r.Output) != "{}\n" {
		t.Errorf("exit = %d output = %q, want 0 and an empty envelope", r.ExitCode, r.Output)
	}
	if !hasNote(r, "not valid JSON") {
		t.Errorf("notes %q should say the bundle does not parse", r.Notes)
	}
}

func TestASchemaViolationIsNeverEmitted(t *testing.T) {
	// aw-sync validated every rule when it wrote the file, so this guards
	// a newer schema in this binary; the answer is still no settings,
	// never settings that make Claude Code refuse to start.
	bad := `{"version":"v2","groups":[],"rules":[{"name":"bad","agents":{"claude":{"managed":{"permissions":{"deny":"Read(./.env)"}}}}}]}`

	r := policyhelper.Run(config(t, writeBundle(t, bad)))

	if r.ExitCode != 0 || r.Source != policyhelper.SourceNone || string(r.Output) != "{}\n" {
		t.Errorf("exit = %d source = %q output = %q, want 0, none and an empty envelope", r.ExitCode, r.Source, r.Output)
	}
	if !hasNote(r, "/permissions/deny") {
		t.Errorf("notes %q should say what was wrong", r.Notes)
	}
}

func TestRequireBundleFailsClosedWithoutABundle(t *testing.T) {
	cfg := config(t, filepath.Join(t.TempDir(), "absent.json"))
	cfg.RequireBundle = true

	r := policyhelper.Run(cfg)

	if r.ExitCode != 1 {
		t.Errorf("exit = %d, want 1: the organization asked to fail closed", r.ExitCode)
	}
	if string(r.Output) != "{}\n" {
		t.Errorf("output = %q; a valid envelope is printed even when refusing", r.Output)
	}
}

func TestRequireBundleIsSatisfiedByAUsableBundle(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.RequireBundle = true

	if r := policyhelper.Run(cfg); r.ExitCode != 0 {
		t.Errorf("exit = %d, want 0; notes %q", r.ExitCode, r.Notes)
	}
}

func TestOutputStaysUnderClaudeCodesLimit(t *testing.T) {
	// Claude Code reads at most 1 MiB from stdout; more fails the run.
	big := strings.Repeat("x", 2<<20)
	oversized := `{"version":"v1","groups":[],"rules":[{"name":"big","agents":{"claude":{"managed":{"model":"` + big + `"}}}}]}`

	r := policyhelper.Run(config(t, writeBundle(t, oversized)))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if len(r.Output) >= 1<<20 {
		t.Errorf("output is %d bytes; an oversized policy must degrade, not brick", len(r.Output))
	}
	if !hasNote(r, "1 MiB") {
		t.Errorf("notes %q should say the policy was too large", r.Notes)
	}
}

func TestEveryRunIsAudited(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.RepoOverride = "github.com/acme/payments-api"
	cfg.Now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

	policyhelper.Run(cfg)

	raw, err := os.ReadFile(filepath.Join(cfg.AuditDir, "aw-policy.log"))
	if err != nil {
		t.Fatalf("no audit log: %v", err)
	}
	var entry struct {
		Time     time.Time `json:"time"`
		Source   string    `json:"source"`
		Version  string    `json:"version"`
		Groups   []string  `json:"groups"`
		Repo     string    `json:"repo"`
		ExitCode int       `json:"exitCode"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &entry); err != nil {
		t.Fatalf("audit line is not JSON: %v\n%s", err, raw)
	}
	if entry.Source != "bundle" || entry.Version != "2026-09-21.1" || entry.Repo != "github.com/acme/payments-api" ||
		len(entry.Groups) != 1 || entry.Groups[0] != "platform" || entry.ExitCode != 0 || !entry.Time.Equal(cfg.Now()) {
		t.Errorf("audit entry = %+v; want the bundle's groups and version, the repository and the time", entry)
	}
}

func TestNoAuditDirMeansNoAuditAndNoFailure(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.AuditDir = ""

	if r := policyhelper.Run(cfg); r.ExitCode != 0 || r.Source != policyhelper.SourceBundle {
		t.Errorf("exit = %d source = %q; auditing is best effort", r.ExitCode, r.Source)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail to compile**

Run: `go test ./internal/policyhelper/`
Expected: compile errors naming `BundlePath`, `AuditDir`, `RequireBundle`, `SourceBundle` and `Run` taking one argument.

- [ ] **Step 3: Replace helper.go**

Write `internal/policyhelper/helper.go` with exactly this content:

```go
// Package policyhelper computes the managed settings Claude Code applies to a
// session. It is the core of aw-policy, the policyHelper executable.
//
// This is the highest-severity code path in the project. Claude Code runs the
// helper before accepting a prompt, and if the helper exits non-zero, times
// out, or emits a managedSettings object with a schema violation, Claude Code
// refuses to start. So the contract here is fail-safe: whatever goes wrong,
// Run returns something valid to print and an exit code of zero. The only
// exception is an organization that opts into failing closed.
//
// The helper is offline. aw-sync, running as root on a timer, fetches the
// enrolled user's bundle from the control plane and leaves it in Claude
// Code's system directory; the helper reads that file, makes the one
// decision left in it (which repository the session is in) and prints the
// result. No bundle means an envelope that carries no managedSettings, which
// tells Claude Code to apply the static managed-settings files the
// organization deployed. That is a machine aw-sync has not reached yet, and
// it is governed by whatever those files say, which is more than nothing.
package policyhelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/repo"
)

// Source says where the emitted policy came from.
type Source string

const (
	// SourceBundle means the bundle aw-sync left on this machine was
	// compiled for the session and emitted.
	SourceBundle Source = "bundle"
	// SourceNone means there was nothing to emit: no bundle, or one this
	// binary could not use. The envelope omits managedSettings.
	SourceNone Source = "none"
)

// maxOutput is what Claude Code will read from the helper's stdout. An
// envelope at or past it fails the run, so one is never emitted.
const maxOutput = 1 << 20

// maxBundle bounds what is read from the bundle file. aw-sync never accepts
// more than 4 MiB from the control plane, so a larger file is not its work
// and is not trusted.
const maxBundle = 4 << 20

// Config is everything a run needs. It is plain data so that the executable
// and the tests build it the same way.
type Config struct {
	// BundlePath is the bundle aw-sync wrote for this machine's user.
	BundlePath string
	// WorkDir is where the session runs; the repository it is in, if any,
	// is the subject's repo. Empty means the current directory.
	WorkDir string
	// RepoOverride names the repository directly and skips detection.
	RepoOverride string
	// AuditDir holds the audit log. Empty disables auditing.
	AuditDir string
	// RequireBundle makes a run without a usable bundle exit non-zero, so
	// Claude Code refuses to start rather than run ungoverned. It is the
	// organization's opt-in; the default is to fail safe.
	RequireBundle bool
	// Now supplies the time for the audit log.
	Now func() time.Time
}

// Result is what a run produced. Output is always a complete JSON envelope,
// even when ExitCode is non-zero, so the executable can print it regardless.
type Result struct {
	// Output is the envelope to write to stdout.
	Output []byte
	// ExitCode is what the executable should exit with.
	ExitCode int
	// Source says where the policy came from.
	Source Source
	// Version is the policy revision emitted, when one was.
	Version string
	// Notes explain what happened, for stderr and the audit log.
	Notes []string
}

// Run computes the envelope for one launch. It never panics out and never
// returns without an Output.
func Run(cfg Config) (result Result) {
	defer func() {
		// A defect in this package must still leave Claude Code able to
		// start. Turn a panic into the no-policy envelope and a note.
		if r := recover(); r != nil {
			result = Result{Source: SourceNone, Notes: []string{fmt.Sprintf("internal error: %v", r)}}
			result.Output = envelope(nil)
			result.ExitCode = exitCode(cfg, result.Source)
		}
	}()

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	subject, notes := resolveSubject(cfg)
	result.Notes = notes

	var managed map[string]any
	bundle, err := loadBundle(cfg.BundlePath)
	if err == nil {
		subject.Groups = bundle.Groups
		managed, err = compile(bundle, subject.Repo)
	}
	if err != nil {
		result.note("no policy available: %v", err)
		result.Source = SourceNone
		result.Output = envelope(nil)
	} else {
		result.Source = SourceBundle
		result.Version = bundle.Version
		result.Output = envelope(managed)
	}

	if len(result.Output) >= maxOutput {
		// Claude Code would fail the run on an oversized envelope, which is
		// worse than an ungoverned session with a loud note.
		result.note("the policy is %d bytes, over Claude Code's 1 MiB limit; emitting no policy", len(result.Output))
		result.Source = SourceNone
		result.Version = ""
		result.Output = envelope(nil)
	}
	result.ExitCode = exitCode(cfg, result.Source)
	audit(cfg, subject, result)
	return result
}

func (r *Result) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// exitCode is zero unless the organization requires a bundle and this run
// did not get a usable one.
func exitCode(cfg Config, source Source) int {
	if cfg.RequireBundle && source != SourceBundle {
		return 1
	}
	return 0
}

// envelope builds the stdout document. A nil managed omits the key, which
// Claude Code reads as "no managed settings from the helper".
func envelope(managed map[string]any) []byte {
	if managed == nil {
		return []byte("{}\n")
	}
	out := mustJSON(map[string]any{"managedSettings": managed})
	return append(out, '\n')
}

// resolveSubject finds the repository the session runs in. Groups are not
// decided here: the bundle carries the ones it was cut for.
func resolveSubject(cfg Config) (policy.Subject, []string) {
	subject := policy.Subject{Repo: cfg.RepoOverride}
	if subject.Repo != "" {
		return subject, nil
	}
	dir := cfg.WorkDir
	if dir == "" {
		dir = "."
	}
	detected, err := repo.Detect(dir)
	if err != nil {
		// Not knowing the repo means the baseline policy applies, which is
		// the conservative outcome; record why.
		return subject, []string{"repository not detected: " + err.Error()}
	}
	subject.Repo = detected
	return subject, nil
}

// loadBundle reads what aw-sync left. Every problem is one error: the
// caller does nothing different for a missing file than for a broken one,
// and the message says which it was.
func loadBundle(path string) (*policy.Bundle, error) {
	if path == "" {
		return nil, errors.New("no bundle path configured")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("bundle %s: %w", path, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxBundle+1))
	if err != nil {
		return nil, fmt.Errorf("reading bundle %s: %w", path, err)
	}
	if len(raw) > maxBundle {
		return nil, fmt.Errorf("bundle %s exceeds %d bytes", path, maxBundle)
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, fmt.Errorf("bundle %s is not valid JSON: %w", path, err)
	}
	return &bundle, nil
}

// compile resolves the bundle for the session's repository and checks the
// result against the schema this binary carries. aw-sync validated every
// rule when it wrote the file, so a failure here means this build carries a
// newer schema than the one that wrote it; the safe answer is still no
// settings rather than settings Claude Code refuses.
func compile(bundle *policy.Bundle, repoName string) (map[string]any, error) {
	managed := managedSettings(bundle.Compile(repoName).Agent("claude"))
	if err := schema.Validate(managed); err != nil {
		return nil, fmt.Errorf("the bundle compiles to settings this build rejects: %w", err)
	}
	return managed, nil
}

// managedSettings turns one agent's policy into the object Claude Code
// applies. The policy's env goes under the settings' own env key; a variable
// set in both keeps the managed value, since that is the more deliberate of
// the two places to put it.
func managedSettings(config policy.AgentConfig) map[string]any {
	out := make(map[string]any, len(config.Managed)+1)
	for k, v := range config.Managed {
		out[k] = v
	}
	if len(config.Env) == 0 {
		return out
	}
	env := make(map[string]any, len(config.Env))
	if existing, ok := out["env"].(map[string]any); ok {
		for k, v := range existing {
			env[k] = v
		}
	}
	for k, v := range config.Env {
		if _, taken := env[k]; !taken {
			env[k] = v
		}
	}
	out["env"] = env
	return out
}

// mustJSON encodes a value this package built itself; a failure would be a
// defect, and the recover in Run turns it into a note.
func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return out
}
```

- [ ] **Step 4: Rename the audit directory field**

In `internal/policyhelper/audit.go`, replace every `cfg.CacheDir` with `cfg.AuditDir` (three occurrences: the early return, `filepath.Join`, `os.MkdirAll`) and change the comment on `audit` to:

```go
// audit appends one line to the log in the audit directory. It is best
// effort: a log that cannot be written must not affect the launch.
```

Nothing else in that file changes.

- [ ] **Step 5: Run the tests to see them pass**

Run: `go test ./internal/policyhelper/ && go vet ./internal/policyhelper/ && gofmt -l internal/policyhelper`
Expected: PASS, no vet output, no gofmt output. `cmd/aw-policy` will not build until Task 2; that is expected and is why Task 2 follows immediately.

- [ ] **Step 6: Commit**

```bash
git add internal/policyhelper
git commit -m "refactor(policyhelper): read the bundle aw-sync wrote instead of fetching policy

The helper is offline: it loads aw-bundle.json, compiles it for the
session's repository, validates and emits. The HTTP fetch, ETag handling
and the per-subject cache are gone with the fake-server tests that
covered them.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 2: `cmd/aw-policy` reads `AW_POLICY_BUNDLE` and a one-key config; `claude.SystemDir` exported

**Files:**
- Modify: `internal/agent/claude/inspect.go` (export `SystemDir`, lines 152-165)
- Modify: `cmd/aw-policy/main.go` (replace whole file)
- Modify: `cmd/aw-policy/e2e_test.go` (replace whole file)
- Modify: `deploy/managed-settings/aw-policy.json` (replace whole file)

**Interfaces:**
- Consumes: `policyhelper.Config`, `policyhelper.Run(cfg) Result` from Task 1; `claude.BundleFile` (`"aw-bundle.json"`).
- Produces: `claude.SystemDir(goos string) string`; environment contract `AW_POLICY_BUNDLE`, `AW_POLICY_CONFIG`; config file `{"requireBundle": bool}`. Tasks 5 and 6 rely on these.

- [ ] **Step 1: Export `SystemDir` in the Claude adapter**

In `internal/agent/claude/inspect.go`, replace the `systemDir` method:

```go
func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	switch a.goos() {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}
```

with:

```go
// SystemDir is where Claude Code reads its managed settings on goos, and so
// where aw-sync writes the bundle and the drop-in and where aw-policy reads
// the bundle back. One table, so the writer and the reader cannot disagree.
func SystemDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}

func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	return SystemDir(a.goos())
}
```

Run: `go test ./internal/agent/claude/` — Expected: PASS (behaviour unchanged).

- [ ] **Step 2: Replace the e2e test**

Write `cmd/aw-policy/e2e_test.go` with exactly this content:

```go
package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var built string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-policy-e2e-*")
	if err != nil {
		panic(err)
	}
	built = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building aw-policy: " + err.Error() + "\n" + string(out))
	}
	// os.Exit does not run deferred calls, so the cleanup has to happen
	// after m.Run returns and before Exit is called.
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// bundle is a rendered aw-bundle.json with one rule everyone gets.
const bundle = `{"version":"v1","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`

// run executes the helper the way Claude Code does: no arguments, stdout
// and stderr captured separately. env is added to a minimal environment
// that points every cache and home directory at temporary ones.
func run(t *testing.T, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(built)
	cmd.Dir = t.TempDir() // not a repository
	cmd.Env = append(env, "PATH="+os.Getenv("PATH"), "HOME="+t.TempDir(), "XDG_CACHE_HOME="+t.TempDir())
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
		t.Fatalf("running aw-policy: %v", err)
		return "", "", 0
	}
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func decode(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return out
}

func TestEmitsTheBundleAsManagedSettings(t *testing.T) {
	bundlePath := writeFile(t, "aw-bundle.json", bundle)

	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"))

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	managed, _ := decode(t, stdout)["managedSettings"].(map[string]any)
	if managed["model"] != "opus" {
		t.Errorf("managedSettings = %v, want the bundle's rule", managed)
	}
	if stderr != "" {
		t.Errorf("stderr = %q; a missing configuration file is not worth a note", stderr)
	}
}

func TestAMissingBundleStillExitsZero(t *testing.T) {
	// A machine aw-sync has not reached yet must still let Claude Code
	// start: an empty envelope leaves the static managed-settings files
	// in force.
	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+filepath.Join(t.TempDir(), "absent-bundle.json"))

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, has := decode(t, stdout)["managedSettings"]; has {
		t.Errorf("stdout = %s, want an envelope without managedSettings", stdout)
	}
	if !strings.Contains(stderr, "absent-bundle.json") {
		t.Errorf("stderr %q should name the missing bundle", stderr)
	}
}

func TestAnInvalidConfigIsNotedAndIgnored(t *testing.T) {
	cfg := writeFile(t, "aw-policy.json", `{"serverUrl":"https://old.example.com"}`)
	bundlePath := writeFile(t, "aw-bundle.json", bundle)

	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+cfg)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if managed, _ := decode(t, stdout)["managedSettings"].(map[string]any); managed["model"] != "opus" {
		t.Errorf("managedSettings = %v; a bad configuration must not cost the policy", managed)
	}
	if !strings.Contains(stderr, "aw-policy.json") {
		t.Errorf("stderr %q should name the invalid configuration", stderr)
	}
}

func TestRequireBundleExitsNonZeroWithoutABundle(t *testing.T) {
	cfg := writeFile(t, "aw-policy.json", `{"requireBundle":true}`)

	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+filepath.Join(t.TempDir(), "absent.json"), "AW_POLICY_CONFIG="+cfg)

	if code == 0 {
		t.Fatal("exit = 0, want non-zero: the organization asked to fail closed")
	}
	decode(t, stdout)
	if stderr == "" {
		t.Error("stderr is empty; Claude Code shows it as the reason for refusing to start")
	}
}

func TestStderrStaysShort(t *testing.T) {
	// Claude Code fails the run past 1 MiB of stderr, so notes are bounded
	// however much goes wrong.
	_, stderr, _ := run(t, "AW_POLICY_BUNDLE="+writeFile(t, "aw-bundle.json", "{"+strings.Repeat("x", 100<<10)))

	if len(stderr) > 64<<10 {
		t.Errorf("stderr is %d bytes", len(stderr))
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `go test ./cmd/aw-policy/`
Expected: the build in `TestMain` fails (main.go still references `cfg.ServerURL` and `Run(ctx, cfg)`), so the package panics with "building aw-policy".

- [ ] **Step 4: Replace main.go**

Write `cmd/aw-policy/main.go` with exactly this content:

```go
// Command aw-policy is the policyHelper executable: Claude Code runs it at
// startup with no arguments and applies the managed settings it prints.
//
// It is offline. aw-sync enrolls the machine and keeps the user's bundle in
// Claude Code's system directory as aw-bundle.json, beside the drop-in that
// names this helper:
//
//	{"policyHelper": {"path": "/usr/local/bin/aw-policy", "timeoutMs": 5000}}
//
// The helper reads the bundle, resolves the repository the session runs in
// and prints the settings that apply. Its own configuration, aw-policy.json
// in the same directory, is optional and has one key:
//
//	{"requireBundle": false}
//
// The contract with Claude Code is strict: a non-zero exit, a timeout, or a
// schema violation in the output refuses the launch. This program therefore
// always exits 0 with a valid envelope, printing an empty one when there is
// no usable bundle, unless the organization opts into failing closed with
// "requireBundle". See internal/policyhelper for the rules.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/policyhelper"
)

// configEnv names the configuration file explicitly. It exists for tests and
// for trying the helper by hand; a deployment relies on the system path.
const configEnv = "AW_POLICY_CONFIG"

// bundleEnv names the bundle file explicitly, for the same reasons.
const bundleEnv = "AW_POLICY_BUNDLE"

// maxStderr bounds what is written to stderr. Claude Code fails the run past
// 1 MiB, and it shows stderr as the reason when the helper exits non-zero,
// so short is also more useful.
const maxStderr = 16 << 10

// fileConfig is the deployed configuration file.
type fileConfig struct {
	RequireBundle bool `json:"requireBundle"`
}

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Getenv))
}

// run does everything main would, with the process boundary as parameters.
// It never panics out: a defect here must still let Claude Code start.
func run(stdout, stderr io.Writer, getenv func(string) string) (code int) {
	var notes []string
	defer func() {
		if r := recover(); r != nil {
			notes = append(notes, fmt.Sprintf("internal error: %v", r))
			fmt.Fprint(stdout, "{}\n")
			code = 0
		}
		writeNotes(stderr, notes)
	}()

	cfg, notes := loadConfig(getenv)

	result := policyhelper.Run(cfg)
	notes = append(notes, result.Notes...)
	if _, err := stdout.Write(result.Output); err != nil {
		notes = append(notes, "writing stdout: "+err.Error())
	}
	return result.ExitCode
}

// loadConfig locates the bundle and reads the optional configuration file.
// Problems are notes, not errors: the helper runs on with the defaults.
func loadConfig(getenv func(string) string) (policyhelper.Config, []string) {
	var notes []string
	systemDir := claude.SystemDir(runtime.GOOS)
	cfg := policyhelper.Config{BundlePath: getenv(bundleEnv)}
	if cfg.BundlePath == "" {
		cfg.BundlePath = filepath.Join(systemDir, claude.BundleFile)
	}

	path := getenv(configEnv)
	if path == "" {
		path = filepath.Join(systemDir, "aw-policy.json")
	}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		var file fileConfig
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&file); err != nil {
			notes = append(notes, fmt.Sprintf("configuration %s is invalid: %v", path, err))
			break
		}
		cfg.RequireBundle = file.RequireBundle
	case errors.Is(err, os.ErrNotExist):
		// The file is optional: its one key defaults to failing safe.
	default:
		notes = append(notes, fmt.Sprintf("configuration %s: %v", path, err))
	}

	if dir, err := os.UserCacheDir(); err == nil {
		cfg.AuditDir = filepath.Join(dir, "agent-wrapper", "aw-policy")
	} else {
		notes = append(notes, "no audit directory: "+err.Error())
	}
	return cfg, notes
}

func writeNotes(stderr io.Writer, notes []string) {
	if len(notes) == 0 {
		return
	}
	text := "aw-policy: " + strings.Join(notes, "\naw-policy: ") + "\n"
	if len(text) > maxStderr {
		text = text[:maxStderr]
	}
	_, _ = io.WriteString(stderr, text)
}
```

- [ ] **Step 5: Replace the deployed configuration template**

Write `deploy/managed-settings/aw-policy.json` with exactly:

```json
{
  "requireBundle": false
}
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `go test ./cmd/aw-policy/ ./internal/agent/claude/ && go build ./... && go vet ./... && gofmt -l . && GOOS=darwin go build ./... && GOOS=windows go build ./...`
Expected: PASS, everything builds, no vet or gofmt output. Confirm no network import remains: `grep -rn '"net/http"' internal/policyhelper cmd/aw-policy` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/claude/inspect.go cmd/aw-policy deploy/managed-settings/aw-policy.json
git commit -m "feat(aw-policy): offline helper reads aw-bundle.json; config shrinks to requireBundle

AW_POLICY_BUNDLE overrides the bundle path for tests, AW_POLICY_CONFIG
stays. claude.SystemDir is exported so the helper and aw-sync share one
table of system directories.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 3: Claude `Inspect` reports the bundle

**Files:**
- Modify: `internal/agent/claude/inspect.go` (`Inspect` and a new `inspectBundle`)
- Modify: `internal/agent/claude/inspect_test.go` (append three tests)

**Interfaces:**
- Consumes: `BundleFile` from `render.go`; `policy.Bundle`.
- Produces: one extra `agent.Finding` from `(*Adapter).Inspect`, placed right after the policyHelper finding.

- [ ] **Step 1: Append the failing tests**

Append to `internal/agent/claude/inspect_test.go`:

```go
func TestInspectWarnsWhenThereIsNoBundle(t *testing.T) {
	in := newInspection(t)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Warn, "aw-bundle.json") {
		t.Errorf("findings %+v should warn that no bundle has been synced", findings)
	}
}

func TestInspectReportsTheBundleVersion(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"),
		`{"version":"2026-09-21.1","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.OK, "version 2026-09-21.1") || !findingsWith(findings, agent.OK, "1 rule") {
		t.Errorf("findings %+v should report the bundle's version and rule count", findings)
	}
}

func TestInspectFlagsAnUnparseableBundle(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"), "{not json")

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Error, "not valid JSON") {
		t.Errorf("findings %+v should flag the bundle aw-policy cannot read", findings)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agent/claude/ -run 'TestInspect.*Bundle'`
Expected: all three FAIL (no bundle finding is produced yet).

- [ ] **Step 3: Add the finding**

In `internal/agent/claude/inspect.go`:

Add `"errors"` to the imports and `"github.com/acme/agent-wrapper/internal/policy"` after the `agent` import.

In `Inspect`, change `findings := make([]agent.Finding, 0, 3)` to `findings := make([]agent.Finding, 0, 4)`, and insert, immediately after the `switch` that appends the policyHelper finding and before the `fetchSkippers` check:

```go
	findings = append(findings, inspectBundle(systemDir))
```

Add this function after `checkHelperBinary`:

```go
// inspectBundle reports the bundle aw-sync leaves for aw-policy. Without
// it the helper emits no settings and every launch runs on the static
// managed-settings files alone, which is silent; with one that does not
// parse, the same happens for a worse reason. Age and last error are
// aw-sync's to report, from its state directory; `aw doctor` shows both.
func inspectBundle(systemDir string) agent.Finding {
	path := filepath.Join(systemDir, BundleFile)
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no bundle at %s: aw-policy emits no settings until aw-sync has run", path)}
	case err != nil:
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("bundle %s is unreadable, so aw-policy emits no settings: %v", path, err)}
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("bundle %s is not valid JSON, so aw-policy emits no settings: %v", path, err)}
	}
	version := bundle.Version
	if version == "" {
		version = "unversioned"
	}
	return agent.Finding{Level: agent.OK,
		Message: fmt.Sprintf("bundle %s: version %s, %d rule(s) for %d group(s)", path, version, len(bundle.Rules), len(bundle.Groups))}
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/agent/claude/ ./cmd/aw/ && go vet ./internal/agent/claude/ && gofmt -l internal/agent/claude`
Expected: PASS. The existing `TestDoctorReportsHowTheAgentIsGoverned` in `cmd/aw` still passes (it only requires findings with a level and message).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/claude/inspect.go internal/agent/claude/inspect_test.go
git commit -m "feat(claude): doctor finding for the synced bundle

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 4: `aw doctor` shows aw-sync's state

**Files:**
- Modify: `cmd/aw/main.go` (usage, `report`, `doctor`, `printReport`, new `syncSection`, `age`)
- Create: `cmd/aw/doctor_test.go`
- Modify: `cmd/aw/e2e_test.go` (append one test)

**Interfaces:**
- Consumes: `sync.Status(sync.Config{StateDir}) (sync.Report, error)`, `sync.StateDir(goos) string`, `sync.SaveMachine`, `sync.SaveState`, `sync.Machine`, `sync.State` from `internal/sync`.
- Produces: `report.SyncStateDir string`, `report.Sync *sync.Report` (JSON key `sync`), `report.SyncError string`; env `AW_SYNC_STATE_DIR`; `syncSection(stateDir string) (*sync.Report, string)`.

- [ ] **Step 1: Write the failing unit tests**

Create `cmd/aw/doctor_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestSyncSectionReadsAwSyncState(t *testing.T) {
	dir := t.TempDir()
	if err := sync.SaveMachine(dir, sync.Machine{Server: "http://awd", MachineID: "m1", Credential: "secret", Agents: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "aw-bundle.json")
	if err := os.WriteFile(bundle, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := sync.State{
		Server: "http://awd", MachineID: "m1", Agents: []string{"claude"},
		Version:  "v1",
		SyncedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		Files:    map[string]string{bundle: "not-the-hash-on-disk"},
		Error:    "boom",
	}
	if err := sync.SaveState(dir, state); err != nil {
		t.Fatal(err)
	}

	r, errText := syncSection(dir)

	if errText != "" {
		t.Fatalf("syncSection error = %q", errText)
	}
	if !r.Enrolled || r.MachineID != "m1" || r.Version != "v1" || r.Error != "boom" || !r.SyncedAt.Equal(state.SyncedAt) {
		t.Errorf("report = %+v; want the enrollment and last-cycle facts from state.json", r)
	}
	if !r.Drift || len(r.Files) != 1 || r.Files[0].State != "drift" {
		t.Errorf("report = %+v; want the tampered bundle reported as drift", r)
	}
}

func TestSyncSectionWithoutAStateDirectoryIsNotEnrolled(t *testing.T) {
	r, errText := syncSection(filepath.Join(t.TempDir(), "absent"))

	if errText != "" || r == nil || r.Enrolled {
		t.Errorf("report = %+v error = %q; a machine without aw-sync is not enrolled, and that is not an error", r, errText)
	}
}

func TestSyncSectionReportsAnUnreadableState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, errText := syncSection(dir)

	if r != nil || errText == "" {
		t.Errorf("report = %+v error = %q; a broken state.json is worth telling the developer about", r, errText)
	}
}

func TestAgeIsHumanReadable(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cases := map[time.Time]string{
		now.Add(-30 * time.Second): "30s ago",
		now.Add(-5 * time.Minute):  "5m0s ago",
		now.Add(-3 * time.Hour):    "3h0m0s ago",
		now.Add(-49 * time.Hour):   "2d1h ago",
		{}:                         "never",
	}
	for at, want := range cases {
		if got := age(at, now); got != want {
			t.Errorf("age(%v) = %q, want %q", at, got, want)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/aw/ -run 'TestSyncSection|TestAge'`
Expected: compile error, `undefined: syncSection` and `undefined: age`.

- [ ] **Step 3: Implement the section**

In `cmd/aw/main.go`:

Add `"runtime"` and `"time"` to the imports, and `"github.com/acme/agent-wrapper/internal/sync"` after the `policy` import.

Change the usage text's `Flags:` block to:

```
Flags:
  --policy PATH   policy document to apply (default: $AW_POLICY)

doctor also reads aw-sync's state directory ($AW_SYNC_STATE_DIR overrides
the OS default) and reports the last sync, its version, errors and drift.
```

Replace the `report` type with:

```go
// report is the machine-readable shape of doctor's output.
type report struct {
	Wrapper string `json:"wrapper"`
	Policy  string `json:"policy,omitempty"`
	// SyncStateDir is where aw-sync's state was looked for.
	SyncStateDir string `json:"syncStateDir"`
	// Sync is aw-sync's own status, read as the developer from its state
	// directory: enrollment, last cycle, drift. Nil when that directory
	// could not be read, with SyncError saying why.
	Sync      *sync.Report `json:"sync,omitempty"`
	SyncError string       `json:"syncError,omitempty"`
	Agents    []agentStatus `json:"agents"`
}
```

In `doctor`, replace `out := report{Wrapper: "aw", Policy: opts.policyPath}` with:

```go
	stateDir := os.Getenv("AW_SYNC_STATE_DIR")
	if stateDir == "" {
		stateDir = sync.StateDir(runtime.GOOS)
	}
	out := report{Wrapper: "aw", Policy: opts.policyPath, SyncStateDir: stateDir}
	out.Sync, out.SyncError = syncSection(stateDir)
```

Add after `doctor`:

```go
// syncSection reads aw-sync's state directory as the developer. A missing
// directory is "not enrolled", which is a fact and not an error; a state
// file that cannot be read is an error, so a half-installed machine is
// visible rather than reported as clean.
func syncSection(stateDir string) (*sync.Report, string) {
	r, err := sync.Status(sync.Config{StateDir: stateDir})
	if err != nil {
		return nil, err.Error()
	}
	return &r, ""
}

// age says how long ago at was, coarsely: seconds under a minute, Go's
// duration form up to a day, then days and hours. A zero time is "never".
func age(at, now time.Time) string {
	if at.IsZero() {
		return "never"
	}
	d := now.Sub(at).Truncate(time.Second)
	if d >= 24*time.Hour {
		days := d / (24 * time.Hour)
		hours := (d % (24 * time.Hour)) / time.Hour
		return fmt.Sprintf("%dd%dh ago", days, hours)
	}
	return d.String() + " ago"
}
```

In `printReport`, after the `if r.Policy != "" { ... }` block and before the `for _, a := range r.Agents` loop, insert:

```go
	fmt.Printf("sync:    %s\n", r.SyncStateDir)
	switch {
	case r.SyncError != "":
		fmt.Printf("  error:  %s\n", r.SyncError)
	case !r.Sync.Enrolled:
		fmt.Println("  not enrolled: aw-sync has not run on this machine")
	default:
		fmt.Printf("  machine: %s at %s\n", orNone(r.Sync.MachineID), orNone(r.Sync.Server))
		fmt.Printf("  synced:  %s, version %s\n", age(r.Sync.SyncedAt, time.Now()), orNone(r.Sync.Version))
		if r.Sync.Error != "" {
			fmt.Printf("  error:   %s\n", r.Sync.Error)
		}
		for _, f := range r.Sync.Files {
			if f.State != "ok" {
				fmt.Printf("  %-8s %s\n", f.State+":", f.Path)
			}
		}
	}
```

and add the helper at the end of the file:

```go
// orNone makes an empty field visible in the plain report.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
```

- [ ] **Step 4: Run the unit tests**

Run: `go test ./cmd/aw/ -run 'TestSyncSection|TestAge'`
Expected: PASS.

- [ ] **Step 5: Append the e2e test**

Append to `cmd/aw/e2e_test.go`:

```go
func TestDoctorReportsAwSyncState(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	stateDir := t.TempDir()
	state := `{"server":"http://awd.example","machineId":"m1","agents":["claude"],"etag":"","version":"v7","syncedAt":"2026-09-21T12:00:00Z","files":{},"error":"control plane unreachable","notes":[]}`
	if err := os.WriteFile(filepath.Join(stateDir, "machine.json"), []byte(`{"server":"http://awd.example","machineId":"m1","credential":"c","agents":["claude"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir)

	out, code := run(t, env, "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	var report struct {
		SyncStateDir string `json:"syncStateDir"`
		Sync         struct {
			Enrolled bool   `json:"enrolled"`
			Version  string `json:"version"`
			Error    string `json:"error"`
		} `json:"sync"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", err, out)
	}
	if report.SyncStateDir != stateDir || !report.Sync.Enrolled || report.Sync.Version != "v7" || report.Sync.Error != "control plane unreachable" {
		t.Errorf("sync section = %+v; want aw-sync's last cycle from state.json", report)
	}

	text, _ := run(t, env, "doctor")
	if !strings.Contains(text, "version v7") || !strings.Contains(text, "control plane unreachable") {
		t.Errorf("plain doctor output should show the last sync and its error:\n%s", text)
	}
}
```

If `cmd/aw/e2e_test.go` does not already import `os` and `path/filepath`, add them.

- [ ] **Step 6: Run everything for the package**

Run: `go test ./cmd/aw/ && go vet ./cmd/aw/ && gofmt -l cmd/aw && GOOS=darwin go build ./... && GOOS=windows go build ./...`
Expected: PASS, no output from vet/gofmt.

- [ ] **Step 7: Commit**

```bash
git add cmd/aw
git commit -m "feat(aw): doctor reports aw-sync's last cycle, version, error and drift

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 5: end to end — awd, aw-sync and aw-policy together

**Files:**
- Modify: `cmd/aw-sync/e2e_test.go` (`TestMain`, new `runPolicy`, additions inside `TestEnrollOnceStatusAndOutage`)

**Interfaces:**
- Consumes: the built `aw-policy` binary's environment contract from Task 2 (`AW_POLICY_BUNDLE`, `AW_POLICY_CONFIG`); the rendered bundle path from the existing test.

- [ ] **Step 1: Build aw-policy in TestMain and add a runner**

In `cmd/aw-sync/e2e_test.go`, change the `var (...)` block to:

```go
var (
	builtSync   string
	builtAwd    string
	builtPolicy string
)
```

In `TestMain`, after the `awd` build block, add:

```go
	builtPolicy = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-o", builtPolicy, "../aw-policy").CombinedOutput(); err != nil {
		panic("building aw-policy: " + err.Error() + "\n" + string(out))
	}
```

Add after `runSync`:

```go
// runPolicy executes aw-policy the way Claude Code does, from dir, against
// the bundle aw-sync rendered. It returns the managedSettings object (nil
// when the envelope has none), stderr and the exit code.
func runPolicy(t *testing.T, dir, bundlePath string) (managed map[string]any, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(builtPolicy)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"AW_POLICY_BUNDLE="+bundlePath,
		"AW_POLICY_CONFIG="+filepath.Join(dir, "absent-aw-policy.json"),
		"HOME="+t.TempDir(), "XDG_CACHE_HOME="+t.TempDir())
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("running aw-policy: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("aw-policy stdout is not one JSON object: %v\n%s", err, out.String())
	}
	managed, _ = envelope["managedSettings"].(map[string]any)
	return managed, errOut.String(), code
}

// paymentsRepo makes a directory that repo.Detect resolves to
// github.com/acme/payments-api, without needing git.
func paymentsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[remote \"origin\"]\n\turl = git@github.com:acme/payments-api.git\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
```

- [ ] **Step 2: Exercise the helper inside the main test**

In `TestEnrollOnceStatusAndOutage`, immediately after the block that checks `bundlePath + ".aw-revision"` (the line `t.Errorf("aw-revision = %q", got)` and its closing brace) and before the comment `// A second cycle is a 304 no-op.`, insert:

```go
	// aw-policy resolves the rendered bundle per launch and needs no
	// server: outside a repository the platform rule sets the model and
	// the payments deny stays out; inside a payments repository it joins.
	managed, stderr, code := runPolicy(t, t.TempDir(), bundlePath)
	if code != 0 {
		t.Fatalf("aw-policy exited %d: %s", code, stderr)
	}
	if managed["model"] != "opus" {
		t.Errorf("outside a repository managed = %v; want model opus from the platform rule", managed)
	}
	if _, has := managed["permissions"]; has {
		t.Errorf("outside a repository the payments rule leaked in: %v", managed)
	}
	managed, stderr, code = runPolicy(t, paymentsRepo(t), bundlePath)
	if code != 0 {
		t.Fatalf("aw-policy in the payments repository exited %d: %s", code, stderr)
	}
	permissions, _ := managed["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	if managed["model"] != "opus" || len(deny) != 1 || deny[0] != "Bash(curl *)" {
		t.Errorf("in the payments repository managed = %v; want the platform model and the repo-scoped deny", managed)
	}
```

Then, at the very end of the same test, after the outage `report` assertions (the `if report.Error == "" || report.Version != ...` block), append:

```go
	// The helper still answers during the outage, from whatever is on
	// disk: it never needs the server, and a valid envelope with exit 0 is
	// all Claude Code requires to start.
	if _, stderr, code := runPolicy(t, t.TempDir(), bundlePath); code != 0 {
		t.Errorf("aw-policy during the outage exited %d: %s", code, stderr)
	}
```

- [ ] **Step 3: Run the e2e**

Run: `go test ./cmd/aw-sync/ -run TestEnrollOnceStatusAndOutage -v`
Expected: PASS, with the new assertions exercised. Then `go test ./... && go vet ./... && gofmt -l .` — all green, no output.

- [ ] **Step 4: Commit**

```bash
git add cmd/aw-sync/e2e_test.go
git commit -m "test(e2e): aw-policy emits a repo-specific policy from the bundle aw-sync rendered, server or not

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

### Task 6: READMEs describe the offline helper

**Files:**
- Modify: `README.md` (the helper paragraphs, "Layout", "Try it")
- Modify: `deploy/managed-settings/README.md` (replace whole file)

**Interfaces:** none; prose only. Every command shown must match the flags that exist (`aw-sync enroll --server --agents --state-dir`, `aw-sync once --state-dir --root agent=DIR`, `AW_POLICY_BUNDLE`, `AW_SYNC_STATE_DIR`).

- [ ] **Step 1: README.md — the helper paragraphs**

Replace the block that starts `**A helper that exits non-zero` and ends `detects the collision.` (three paragraphs) with:

```markdown
**A helper that exits non-zero, or emits settings that fail schema validation,
makes Claude Code refuse to start.** The helper must therefore always exit 0.
That contract is the highest-severity path in the project.

`aw-policy` is that helper, and it is offline. `aw-sync`, running as root on
a timer, fetches the enrolled user's bundle from the control plane and writes
it to Claude Code's system directory as `aw-bundle.json`, beside a drop-in
that names the helper. On every launch `aw-policy` reads that bundle, makes
the one decision left in it (which repository the session is in), validates
the result against the published Claude Code settings schema, and prints it.
With no bundle, or one it cannot use, it prints an envelope with no settings,
which leaves the static managed-settings files in force, and still exits 0.
It exits non-zero only when the organization sets `requireBundle`. Every run
appends one line to a local audit log. `deploy/aw-sync/README.md` installs
the timer; `deploy/managed-settings/README.md` describes the files it writes.

Server-managed settings from the claude.ai console shadow the helper
entirely, so an organization uses one channel or the other; `aw doctor`
detects the collision, and reports the bundle and aw-sync's last cycle.
```

- [ ] **Step 2: README.md — layout**

Replace these two lines in the "Layout" block:

```
    cmd/aw-policy/    the policyHelper executable
```
```
    internal/policyhelper/  what aw-policy does: fetch, validate, cache, emit
```

with, respectively:

```
    cmd/aw-policy/    the policyHelper executable, offline
    cmd/aw-sync/      root-side sync: enroll, render every agent's files
```
```
    internal/policyhelper/  what aw-policy does: read the bundle, compile, validate, emit
    internal/sync/    the sync cycle: fetch the bundle, render, write all-or-nothing
```

(`cmd/aw-sync/` goes directly under `cmd/aw-policy/`; `internal/sync/` directly under `internal/policyhelper/`.)

- [ ] **Step 3: README.md — try it**

Replace the block from `Run the policy helper the way Claude Code would, against that server:` through `Stop \`awd\` and run it again: the same policy comes back from the cache.` with:

```markdown
Run the sync and the policy helper the way a managed machine would, with the
files kept under the working directory so no root is needed:

    go build -o aw-sync ./cmd/aw-sync && go build -o aw-policy ./cmd/aw-policy
    TOKEN=$(./awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
    AW_SYNC_TOKEN=$TOKEN ./aw-sync enroll --server http://127.0.0.1:8080 --agents claude --state-dir ./state
    ./aw-sync once --state-dir ./state --root claude=./claude-root
    AW_POLICY_BUNDLE=$PWD/claude-root/aw-bundle.json ./aw-policy

Stop `awd` and run `aw-policy` again: it needs no server. Run `aw-sync once`
again: it exits 1 and leaves the rendered files as they were.
```

And replace the paragraph starting `` `doctor` prints the resolved binary`` with:

```markdown
`doctor` prints the resolved binary, the exact arguments, the injected
environment and a note for every decision, plus aw-sync's last cycle
(`AW_SYNC_STATE_DIR=./state` points it at the directory above) and whether
a bundle is in place. It never executes the agent.
```

- [ ] **Step 4: deploy/managed-settings/README.md**

Replace the whole file with:

```markdown
# The policy helper's files

Claude Code runs `aw-policy` at every launch and applies what it prints. The
helper is offline: it reads `aw-bundle.json`, which `aw-sync` keeps up to
date from the control plane. See `deploy/aw-sync/README.md` for installing
`aw-sync`; this page is about what ends up in Claude Code's system
directory and why.

| OS | System directory |
| --- | --- |
| macOS | `/Library/Application Support/ClaudeCode/` |
| Linux and WSL | `/etc/claude-code/` |
| Windows | `C:\Program Files\ClaudeCode\` |

Three files live there:

1. `managed-settings.d/50-agent-wrapper.json` — the drop-in that names the
   helper. `aw-sync` writes it on every cycle; the copies under `unix/` and
   `windows/` here are the same content, for a machine that is provisioned
   by MDM before `aw-sync` first runs, and a test keeps them identical.
2. `aw-bundle.json` — the enrolled user's bundle, written by `aw-sync`,
   mode 0644. It is the organization's policy, not a secret.
3. `aw-policy.json` — the helper's own configuration. Optional. One key:

       {"requireBundle": false}

Plus the `aw-policy` binary at the path the drop-in names:
`/usr/local/bin/aw-policy` on Linux and macOS,
`C:\Program Files\AgentWrapper\aw-policy.exe` on Windows.

Claude Code merges `managed-settings.json` first, then every `*.json` in
`managed-settings.d/` alphabetically. The `50-` prefix leaves room on both
sides for files other teams own. `policyHelper` is honoured only from a file
in that directory, a macOS configuration profile, or the Windows `HKLM`
registry; it is ignored in server-managed settings and in `HKCU`.
`policyHelper.path` must be absolute and normalized: no `.` or `..`
segments, no symlinks. On Windows it must end in `.exe`.

## Installing by hand

    install -m 0755 aw-policy /usr/local/bin/aw-policy
    install -d -m 0755 /etc/claude-code/managed-settings.d
    install -m 0644 unix/managed-settings.d/50-agent-wrapper.json /etc/claude-code/managed-settings.d/
    install -m 0644 aw-policy.json /etc/claude-code/aw-policy.json   # optional

On macOS use `/Library/Application Support/ClaudeCode/` in place of
`/etc/claude-code/`. On Windows copy `aw-policy.exe` to
`C:\Program Files\AgentWrapper\` and `windows\managed-settings.d\50-agent-wrapper.json`
into `C:\Program Files\ClaudeCode\`.

## Timeouts

`timeoutMs` in the drop-in is Claude Code's budget for the whole helper run
(minimum 1000, default 10000). The helper reads one file and does no network
I/O, so the 5000 in the template is generous.

## What happens without a bundle

On a machine `aw-sync` has not reached, or whose bundle does not parse, the
helper emits an envelope with no `managedSettings` and exits 0, which tells
Claude Code to fall back to whatever `managed-settings.json` and the other
drop-ins say. Set `"requireBundle": true` to refuse to start instead; that
is an organization's call, and the default is to keep developers working.
A bundle that compiles to settings this helper's schema rejects is treated
the same way and noted on stderr.

## Verify

On a managed machine, `claude doctor` prints a `Setting sources` line that
must read `(helper)`. `aw doctor` reports the helper, the bundle (present,
version, rule count) and `aw-sync`'s last cycle, and checks for the two
things that silently disable the helper: a server-managed payload from the
claude.ai console, which shadows every file-based source, and a helper path
that does not resolve to an executable.

Break it on purpose once: point `path` at a script that exits 1 and confirm
Claude Code refuses to start. The fail-safe contract is worth seeing rather
than assuming.
```

- [ ] **Step 5: Check and commit**

Run: `grep -rn "serverUrl\|requireFresh\|from the cache" README.md deploy/` — Expected: no output. Then `go test ./cmd/aw-sync/ -run TestDeploy` (the deploy template test still passes; the templates did not change).

```bash
git add README.md deploy/managed-settings/README.md
git commit -m "docs: the offline policy helper, aw-sync's files, and doctor's sync section

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01YRB4bPeCCjyjHQcC58zccv"
```

---

## Self-review

**Spec coverage.** "aw-policy, offline" steps 1–4: Task 1 (read, missing/unparseable → `{}` + note, `requireBundle` → exit 1, `repo.Detect` → `Compile` → `managedSettings` → `schema.Validate` → emit, audit) and Task 2 (`AW_POLICY_BUNDLE`, `AW_POLICY_CONFIG`, one-key config). "Deleted" list: Task 1 removes fetch/ETag/cache, Task 2 removes `serverUrl`/`groups`/`timeoutMs`. "Kept" list: envelope rules, 1 MiB guard, panic recovery, audit — all in Task 1's `helper.go`; `AW_POLICY_CONFIG` in Task 2. Doctor bundle/version/age/error: Tasks 3 and 4 (see refinement 1). Failure-mode rows for aw-policy: Task 1 tests `TestWithoutABundle…`, `TestRequireBundle…`, `TestASchemaViolation…`. Testing "end to end … aw-policy with AW_POLICY_BUNDLE emits a repo-specific policy; stop awd; … aw-policy still emits": Task 5. "Fake-server aw-policy tests removed": Task 1 and Task 2 replace both test files. README: Task 6.

**Placeholders.** None: every code step carries the full content.

**Type consistency.** `policyhelper.Config{BundlePath, WorkDir, RepoOverride, AuditDir, RequireBundle, Now}` is used identically in Tasks 1, 2. `Run(cfg)` single argument in Tasks 1, 2. `claude.SystemDir(goos)` defined in Task 2 Step 1 and used in Task 2 Step 4. `claude.BundleFile` exists already (`render.go`). `syncSection(stateDir) (*sync.Report, string)` and `age(at, now time.Time) string` defined and tested in Task 4. `sync.Report` fields used by `printReport` (`Enrolled`, `MachineID`, `Server`, `SyncedAt`, `Version`, `Error`, `Files[].State/Path`) all exist in `internal/sync/status.go`. `runPolicy` and `paymentsRepo` defined and used only in Task 5.
