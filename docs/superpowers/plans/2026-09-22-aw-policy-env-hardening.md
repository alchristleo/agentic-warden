# aw-policy env hardening — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A release build of `aw-policy` ignores `AW_POLICY_BUNDLE` and `AW_POLICY_CONFIG` entirely, tests opt back in with `-tags awtest`, `aw-policy build-info` says which build it is, and `aw doctor` warns when a tagged helper is installed.

**Architecture:** Two tag-guarded files in `cmd/aw-policy` set `const envOverrides`; `loadConfig` takes `allowEnv bool` and, when false, uses the system paths and notes any set variable on stderr. `main` answers the single argument `build-info` before doing anything else. `aw doctor`'s Claude inspection runs `<helper> build-info` after it has confirmed the helper binary and warns only on a positive `env-overrides=on`. Both e2e harnesses that build `aw-policy` pass the tag; the `cmd/aw-policy` harness also builds a release binary to prove the gate.

**Tech Stack:** Go 1.22 build tags; `os/exec` with `context.WithTimeout` in the doctor check. No dependency changes.

**Spec:** `docs/superpowers/specs/2026-09-22-aw-policy-env-hardening-design.md` — every section; the threat model there bounds what tests need to prove.

## Global Constraints

- Go 1.22 toolchain; `go.mod` says `go 1.22`. Do not run `go mod tidy`.
- No `gcc`: `go test -race` cannot run. Run `go test ./...`, `go vet ./...`, `gofmt -l .` before every commit; `GOOS=darwin go build ./... && GOOS=windows go build ./...` and the same two with `-tags awtest` for tasks touching `cmd/aw-policy` or `internal/agent/claude`.
- The helper must always exit 0 with a valid envelope unless `requireBundle` says otherwise; nothing in this plan changes that (spec, "Failure modes").
- The doctor check never produces a finding on a negative or failed probe (spec, "Detection"); only `env-overrides=on` warns.
- Commit messages: Conventional Commits subject; end with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S`.
- Comment style: a comment says *why*, in full sentences, on every exported identifier and every struct field.
- Model tiering for executors: Task 1 and Task 2 are mid tier (they touch a security boundary and an exec path); Task 3 is transcription (cheapest tier); the final whole-branch review is the most capable tier.

---

## File map

| File | Responsibility |
| --- | --- |
| `cmd/aw-policy/env_release.go` | `//go:build !awtest`: `envOverrides = false` |
| `cmd/aw-policy/env_awtest.go` | `//go:build awtest`: `envOverrides = true` |
| `cmd/aw-policy/main.go` | `build-info` argument; `run`/`loadConfig` take `allowEnv`; ignore-notes |
| `cmd/aw-policy/main_test.go` | unit tests for `loadConfig` under both settings and `buildInfo` |
| `cmd/aw-policy/e2e_test.go` | tagged and release binaries; gate, `build-info`, unknown-argument tests |
| `cmd/aw-sync/e2e_test.go` | build `aw-policy` with the tag |
| `internal/agent/claude/inspect.go` | `checkHelperBuild`: run `build-info`, warn on `on` |
| `internal/agent/claude/inspect_test.go` | three fake helpers: on, off, failing |
| `README.md`, `deploy/managed-settings/README.md`, `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md` | docs; the last one already carries its pointer |

---

### Task 1: the gate, `build-info`, and both e2e harnesses

**Files:**
- Create: `cmd/aw-policy/env_release.go`, `cmd/aw-policy/env_awtest.go`
- Modify: `cmd/aw-policy/main.go` (`main`, `run`, `loadConfig`, new `buildInfo`)
- Create: `cmd/aw-policy/main_test.go`
- Modify: `cmd/aw-policy/e2e_test.go` (`TestMain`, `run` → `runBinary`, three new tests)
- Modify: `cmd/aw-sync/e2e_test.go` (`TestMain`, one line)

**Interfaces:**
- Produces: `aw-policy build-info` printing `env-overrides=off` or `env-overrides=on` (one line, exit 0). Task 2 consumes the exact strings `build-info` and `env-overrides=on`.
- Internal: `run(stdout, stderr io.Writer, getenv func(string) string, allowEnv bool) int`, `loadConfig(getenv func(string) string, allowEnv bool) (policyhelper.Config, []string)`, `buildInfo(overrides bool) string`, `const buildInfoArg = "build-info"`.

- [ ] **Step 1: Write the failing unit tests**

Create `cmd/aw-policy/main_test.go`:

```go
package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/claude"
)

// env is a getenv over a fixed map, so the tests control exactly what the
// helper would see in a developer's shell.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestAReleaseBuildReadsTheSystemPathsWhateverTheEnvironmentSays(t *testing.T) {
	cfg, notes := loadConfig(env(map[string]string{
		"AW_POLICY_BUNDLE": "/home/dev/mine.json",
		"AW_POLICY_CONFIG": "/home/dev/mine-config.json",
	}), false)

	want := filepath.Join(claude.SystemDir(runtime.GOOS), claude.BundleFile)
	if cfg.BundlePath != want {
		t.Errorf("BundlePath = %q, want the system path %q; a developer's variable must not move it", cfg.BundlePath, want)
	}
	joined := strings.Join(notes, "\n")
	for _, name := range []string{"AW_POLICY_BUNDLE", "AW_POLICY_CONFIG"} {
		if !strings.Contains(joined, name) || !strings.Contains(joined, "ignores it") {
			t.Errorf("notes %q should tell the developer %s is set and ignored", notes, name)
		}
	}
}

func TestAReleaseBuildIsQuietWhenNothingIsSet(t *testing.T) {
	_, notes := loadConfig(env(nil), false)

	for _, n := range notes {
		if strings.Contains(n, "ignores it") {
			t.Errorf("note %q with no variable set; the note is for a developer who set one", n)
		}
	}
}

func TestATestBuildHonoursTheOverrides(t *testing.T) {
	cfg, _ := loadConfig(env(map[string]string{"AW_POLICY_BUNDLE": "/tmp/fixture.json"}), true)

	if cfg.BundlePath != "/tmp/fixture.json" {
		t.Errorf("BundlePath = %q; a tagged build exists so tests can point at fixtures", cfg.BundlePath)
	}
}

func TestBuildInfoNamesTheSetting(t *testing.T) {
	if got := buildInfo(false); got != "env-overrides=off" {
		t.Errorf("buildInfo(false) = %q", got)
	}
	if got := buildInfo(true); got != "env-overrides=on" {
		t.Errorf("buildInfo(true) = %q", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/aw-policy/ -run 'TestARelease|TestATest|TestBuildInfo'`
Expected: build failure — `too many arguments in call to loadConfig`, `undefined: buildInfo`.

- [ ] **Step 3: Add the tag-guarded constants**

Create `cmd/aw-policy/env_release.go`:

```go
//go:build !awtest

package main

// envOverrides is false in every build made without -tags awtest, which is
// every deployed binary: Claude Code runs the helper with the developer's
// environment, so honouring AW_POLICY_BUNDLE there would let a developer
// choose their own policy. The safe value is the default so that a
// packager who forgets the tag still ships the secure binary.
const envOverrides = false
```

Create `cmd/aw-policy/env_awtest.go`:

```go
//go:build awtest

package main

// envOverrides is true only under -tags awtest, so the e2e tests can point
// the helper at fixtures instead of the root-owned system directory. A
// binary built this way must never be installed; `aw doctor` warns if one
// is, and `aw-policy build-info` says which kind it is.
const envOverrides = true
```

- [ ] **Step 4: Thread `allowEnv` through and add `build-info`**

In `cmd/aw-policy/main.go`:

Replace the package comment's last paragraph (the one starting `The contract with Claude Code is strict`) with:

```go
// The contract with Claude Code is strict: a non-zero exit, a timeout, or a
// schema violation in the output refuses the launch. This program therefore
// always exits 0 with a valid envelope, printing an empty one when there is
// no usable bundle, unless the organization opts into failing closed with
// "requireBundle". See internal/policyhelper for the rules.
//
// The one argument it recognises is "build-info", which prints whether the
// AW_POLICY_BUNDLE and AW_POLICY_CONFIG overrides are compiled in; they are
// only under -tags awtest, for tests. Any other argument runs the helper as
// usual, so a future Claude Code that passes one cannot break a launch.
```

Replace the `configEnv` and `bundleEnv` comments and declarations with:

```go
// configEnv names the configuration file explicitly. It is honoured only
// when envOverrides is true, for tests; a deployment reads the system path.
const configEnv = "AW_POLICY_CONFIG"

// bundleEnv names the bundle file explicitly, under the same rule.
const bundleEnv = "AW_POLICY_BUNDLE"

// buildInfoArg is the one argument the helper answers instead of running.
const buildInfoArg = "build-info"
```

Replace `main` and `run`'s signature with:

```go
func main() {
	if len(os.Args) == 2 && os.Args[1] == buildInfoArg {
		fmt.Fprintln(os.Stdout, buildInfo(envOverrides))
		os.Exit(0)
	}
	os.Exit(run(os.Stdout, os.Stderr, os.Getenv, envOverrides))
}

// buildInfo is what `aw-policy build-info` prints: one line `aw doctor`
// parses to tell a test build from a release build.
func buildInfo(overrides bool) string {
	if overrides {
		return "env-overrides=on"
	}
	return "env-overrides=off"
}

// run does everything main would, with the process boundary as parameters.
// It never panics out: a defect here must still let Claude Code start.
func run(stdout, stderr io.Writer, getenv func(string) string, allowEnv bool) (code int) {
```

and inside `run` change `cfg, notes := loadConfig(getenv)` to `cfg, notes := loadConfig(getenv, allowEnv)`.

Replace `loadConfig`'s comment, signature and the path-resolution block (everything from `func loadConfig` down to and including the line `path = filepath.Join(systemDir, "aw-policy.json")` and its closing brace) with:

```go
// loadConfig locates the bundle and reads the optional configuration file.
// Problems are notes, not errors: the helper runs on with the defaults.
// allowEnv is false in a release build: the two environment variables are
// then not consulted at all, and a set one earns a note so the developer
// who set it learns it did nothing rather than wondering why.
func loadConfig(getenv func(string) string, allowEnv bool) (policyhelper.Config, []string) {
	var notes []string
	systemDir := claude.SystemDir(runtime.GOOS)
	cfg := policyhelper.Config{BundlePath: filepath.Join(systemDir, claude.BundleFile)}
	path := filepath.Join(systemDir, "aw-policy.json")
	if allowEnv {
		if p := getenv(bundleEnv); p != "" {
			cfg.BundlePath = p
		}
		if p := getenv(configEnv); p != "" {
			path = p
		}
	} else {
		for _, name := range []string{bundleEnv, configEnv} {
			if getenv(name) != "" {
				notes = append(notes, fmt.Sprintf("%s is set but this build ignores it; the bundle and configuration are read from %s only", name, systemDir))
			}
		}
	}
```

The rest of `loadConfig` (reading `path`, the audit directory) is unchanged.

- [ ] **Step 5: Run the unit tests**

Run: `go test ./cmd/aw-policy/ -run 'TestARelease|TestATest|TestBuildInfo' -v`
Expected: four PASS. (The e2e tests in the same package will fail at this point because the harness still builds without the tag; that is Step 6.)

- [ ] **Step 6: Both harnesses build with the tag; the aw-policy harness also builds a release binary**

In `cmd/aw-sync/e2e_test.go`, `TestMain`, change the `aw-policy` build line to:

```go
	if out, err := exec.Command("go", "build", "-tags", "awtest", "-o", builtPolicy, "../aw-policy").CombinedOutput(); err != nil {
```

In `cmd/aw-policy/e2e_test.go`:

Replace `var built string` and `TestMain` with:

```go
// built is the helper as the tests need it: with -tags awtest, so fixtures
// can be named in the environment. release is the helper as it ships.
var built, release string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-policy-e2e-*")
	if err != nil {
		panic(err)
	}
	built = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-tags", "awtest", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building aw-policy (awtest): " + err.Error() + "\n" + string(out))
	}
	release = filepath.Join(dir, "aw-policy-release")
	if out, err := exec.Command("go", "build", "-o", release, ".").CombinedOutput(); err != nil {
		panic("building aw-policy (release): " + err.Error() + "\n" + string(out))
	}
	// os.Exit does not run deferred calls, so the cleanup has to happen
	// after m.Run returns and before Exit is called.
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
```

Replace the `run` helper with a two-layer version, so existing tests keep calling `run(t, env...)`:

```go
// run executes the tagged helper the way Claude Code does: no arguments,
// stdout and stderr captured separately. env is added to a minimal
// environment that points every cache and home directory at temporary ones.
func run(t *testing.T, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runBinary(t, built, nil, env...)
}

// runBinary is run for a chosen binary and arguments.
func runBinary(t *testing.T, binary string, args []string, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
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
		t.Fatalf("running %s: %v", binary, err)
		return "", "", 0
	}
}
```

Append these tests at the end of the file:

```go
func TestAReleaseBuildIgnoresTheEnvironmentOverrides(t *testing.T) {
	// Claude Code hands the helper the developer's environment. A release
	// build must read the system directory whatever that environment says,
	// and tell the developer so, or the override would look like it worked.
	// The fixture's model is one no real bundle would set, so the check
	// holds even on a machine that has a real bundle in /etc/claude-code.
	bundlePath := writeFile(t, "aw-bundle.json", `{"version":"v1","rules":[{"name":"mine","agents":{"claude":{"managed":{"model":"fixture-only-model"}}}}]}`)

	stdout, stderr, code := runBinary(t, release, nil,
		"AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"))

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if managed, _ := decode(t, stdout)["managedSettings"].(map[string]any); managed["model"] == "fixture-only-model" {
		t.Errorf("stdout = %s; the release build read the developer's file", stdout)
	}
	for _, name := range []string{"AW_POLICY_BUNDLE", "AW_POLICY_CONFIG"} {
		if !strings.Contains(stderr, name) || !strings.Contains(stderr, "ignores it") {
			t.Errorf("stderr %q should say %s is set and ignored", stderr, name)
		}
	}
}

func TestBuildInfoTellsTheTwoBuildsApart(t *testing.T) {
	stdout, _, code := runBinary(t, release, []string{"build-info"})
	if code != 0 || stdout != "env-overrides=off\n" {
		t.Errorf("release build-info: exit %d, stdout %q", code, stdout)
	}
	stdout, _, code = runBinary(t, built, []string{"build-info"})
	if code != 0 || stdout != "env-overrides=on\n" {
		t.Errorf("awtest build-info: exit %d, stdout %q", code, stdout)
	}
}

func TestAnUnrecognisedArgumentStillRunsTheHelper(t *testing.T) {
	// If a future Claude Code passes an argument, the helper must emit an
	// envelope rather than refuse the launch over something it did not
	// understand.
	stdout, _, code := runBinary(t, release, []string{"--something-new"})

	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	decode(t, stdout)
}
```

- [ ] **Step 7: Run both packages' tests**

Run: `go test ./cmd/aw-policy/ ./cmd/aw-sync/ -v -count=1 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`
Expected: `ok` for both, no `--- FAIL`. If `TestAReleaseBuildIgnoresTheEnvironmentOverrides` reports the note missing for `AW_POLICY_CONFIG`, check that the ignore loop in `loadConfig` covers both names.

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags awtest ./cmd/aw-policy/ && \
GOOS=darwin go build ./... && GOOS=windows go build ./... && \
GOOS=darwin go build -tags awtest ./cmd/aw-policy/ && GOOS=windows go build -tags awtest ./cmd/aw-policy/ && \
git add cmd/aw-policy cmd/aw-sync/e2e_test.go
git commit -m "feat(aw-policy): compile the environment overrides out of release builds; build-info reports which build this is

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 2: `aw doctor` warns about a tagged helper

**Files:**
- Modify: `internal/agent/claude/inspect.go` (`Inspect` default branch; new `checkHelperBuild`)
- Modify: `internal/agent/claude/inspect_test.go` (new helper constructor; three tests)

**Interfaces:**
- Consumes: the strings `build-info` and `env-overrides=on` from Task 1.
- Produces: `checkHelperBuild(helper, source string) (agent.Finding, bool)` — unexported; `Inspect` appends the finding only when the bool is true.

- [ ] **Step 1: Write the failing tests**

In `internal/agent/claude/inspect_test.go`, after `helperBinary`, add:

```go
// helperSaying is a fake helper whose `build-info` answer is script; the
// no-argument path still prints an envelope so the other checks hold.
func (in *inspection) helperSaying(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "aw-policy")
	body := "#!/bin/sh\nif [ \"$1\" = build-info ]; then " + script + "; fi\necho '{}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
```

and add `"runtime"` to the test file's imports. Then append these tests:

```go
func TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides(t *testing.T) {
	in := newInspection(t)
	helper := in.helperSaying(t, "echo env-overrides=on; exit 0")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "-tags awtest") {
		t.Errorf("findings %v should warn that the helper honours a developer's AW_POLICY_BUNDLE", findings)
	}
}

func TestInspectSaysNothingAboutAReleaseHelper(t *testing.T) {
	in := newInspection(t)
	helper := in.helperSaying(t, "echo env-overrides=off; exit 0")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "awtest") {
		t.Errorf("findings %v warn about a release build", findings)
	}
}

func TestInspectSaysNothingWhenTheHelperCannotAnswerBuildInfo(t *testing.T) {
	// The drop-in may name a helper that is not ours; failing on an unknown
	// argument is not evidence of anything.
	in := newInspection(t)
	helper := in.helperSaying(t, "echo unknown argument >&2; exit 1")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "awtest") {
		t.Errorf("findings %v warn on a probe that failed", findings)
	}
	if !findingsWith(findings, agent.OK, helper) {
		t.Errorf("findings %v should still confirm the helper binary", findings)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agent/claude/ -run 'TestInspectWarnsWhenTheHelperWasBuilt|TestInspectSaysNothing' -v`
Expected: `TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides` FAILS (no warning); the other two pass already (nothing warns yet) — that is fine, they guard the negative path once Step 3 lands.

- [ ] **Step 3: Implement the probe**

In `internal/agent/claude/inspect.go`, add `"context"`, `"os/exec"` and `"time"` to the imports, and change `Inspect`'s `default:` branch to:

```go
	default:
		binary := checkHelperBinary(helper, source)
		findings = append(findings, binary)
		if binary.Level == agent.OK {
			if build, warn := checkHelperBuild(helper, source); warn {
				findings = append(findings, build)
			}
		}
```

After `checkHelperBinary`, add:

```go
// helperProbeTimeout bounds `aw-policy build-info`. The real helper answers
// instantly; the bound is for a helper that is not ours and reads stdin or
// waits on a network.
const helperProbeTimeout = 2 * time.Second

// checkHelperBuild asks the helper which build it is and warns when it is a
// test build, since that build honours AW_POLICY_BUNDLE from the
// developer's environment and so lets a developer choose their own policy.
// Every other outcome, including a helper that fails on the argument or
// prints something else, is reported as nothing: the drop-in may name a
// helper that is not ours, and only a positive answer is evidence.
func checkHelperBuild(helper, source string) (agent.Finding, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), helperProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, helper, "build-info").Output()
	if err != nil || !strings.Contains(string(out), "env-overrides=on") {
		return agent.Finding{}, false
	}
	return agent.Finding{Level: agent.Warn,
		Message: fmt.Sprintf("policyHelper %s (from %s) was built with -tags awtest: a developer can point it at their own bundle; rebuild without the tag", helper, source)}, true
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/agent/claude/ -count=1`
Expected: PASS. The pre-existing `helperBinary` script prints `{}` for every argument, so the older tests see no warning and keep passing.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && GOOS=darwin go build ./... && GOOS=windows go build ./... && \
git add internal/agent/claude/inspect.go internal/agent/claude/inspect_test.go
git commit -m "feat(claude): doctor warns when the installed aw-policy was built with -tags awtest

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

### Task 3: docs

**Files:**
- Modify: `README.md` (caveat paragraph; the try-it build line)
- Modify: `deploy/managed-settings/README.md` (build/install paragraph)

The parent spec's pointer sentence was added with the hardening spec's commit and needs nothing here.

- [ ] **Step 1: README.md — the caveat becomes a statement**

Replace the paragraph beginning `One caveat, deliberate for now: the helper honours` (through `which is also the developer's to edit.`) with:

```markdown
A release build of the helper reads only the system directory. The
`AW_POLICY_BUNDLE` and `AW_POLICY_CONFIG` overrides the tests use exist
only under `-tags awtest`; a release build ignores them and says so on
stderr, and `aw doctor` warns if a tagged helper is installed. What remains
outside the helper's control is which repository a session is in:
repo-scoped rules key on `.git/config`'s origin, which is the developer's
to edit.
```

- [ ] **Step 2: README.md — the try-it builds a tagged helper**

In "Try it", change the line

```
    go build -o aw-sync ./cmd/aw-sync && go build -o aw-policy ./cmd/aw-policy
```

to

```
    go build -o aw-sync ./cmd/aw-sync && go build -tags awtest -o aw-policy ./cmd/aw-policy
```

and directly after the `AW_POLICY_BUNDLE=$PWD/claude-root/aw-bundle.json ./aw-policy` line's paragraph (the one ending `leaves the rendered files as they were.`) add:

```markdown
The `-tags awtest` build is what lets `AW_POLICY_BUNDLE` point at the
working directory; it is for this walkthrough and the tests, never for a
machine you enrol.
```

- [ ] **Step 3: deploy/managed-settings/README.md — build without the tag**

Directly before the `## Installing by hand` heading, add:

```markdown
## Building the helper

    go build -o aw-policy ./cmd/aw-policy

Never install a helper built with `-tags awtest`: that build honours
`AW_POLICY_BUNDLE` and `AW_POLICY_CONFIG` from the developer's environment,
which Claude Code passes through, so a developer could point it at a bundle
they wrote. `aw-policy build-info` prints `env-overrides=off` for a release
build, and `aw doctor` warns when the installed helper answers `on`.
```

- [ ] **Step 4: Verify and commit**

Run: `go test ./... && go vet ./... && gofmt -l .`
Expected: all green (docs only, but the suite is the gate before every commit).

```bash
git add README.md deploy/managed-settings/README.md
git commit -m "docs: release aw-policy ignores its environment overrides; build without -tags awtest

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0123uXuFtq52SXVDr15QeT4S"
```

---

## Self-review

**Spec coverage.** "The gate is a build tag" → Task 1 Steps 3–4 (two files, `allowEnv`, ignore-note naming the variable). "Detection: `build-info` and `aw doctor`" → Task 1 Step 4 (`main`, `buildInfo`, unknown arguments run normally) and Task 2 (probe with 2 s timeout, warn only on `on`). "Tests" → Task 1 Step 6 (both harnesses tagged; release binary; gate, `build-info`, unknown-argument e2e), Task 1 Step 1 (`loadConfig` unit tests), Task 2 Step 1 (three fake helpers), cross-builds with and without the tag in Task 1 Step 8. "Documentation" → Task 3, plus the parent spec pointer already committed. "Failure modes" table: each row is asserted by a test named above.

**Placeholders.** None; every code step is complete.

**Type consistency.** `loadConfig(getenv, allowEnv)` and `run(stdout, stderr, getenv, allowEnv)` match between Task 1 Steps 1, 4 and the existing `run` callers (only `main`). `buildInfoArg = "build-info"` is what Task 2's `exec.CommandContext(ctx, helper, "build-info")` sends and what `TestBuildInfoTellsTheTwoBuildsApart` passes. `env-overrides=on` is the exact string `buildInfo(true)` returns, Task 2 matches on, and the fake helpers echo. `runBinary(t, binary, args, env...)` is used by the three new e2e tests and by `run`.
