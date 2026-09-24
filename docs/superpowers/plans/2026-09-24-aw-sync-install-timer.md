# aw-sync install-timer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aw-sync install-timer` / `uninstall-timer` install and remove the periodic sync unit on Linux, macOS and Windows, and `once`/`enroll` hold an exclusive lock so two cycles never overlap.

**Architecture:** A new package `internal/sync/timer` renders embedded unit templates (`Render`) and installs them through an injectable command runner (`Installer`). A lock in `internal/sync` (`flock` on Unix, an unshared `CreateFile` on Windows) guards `once` and `enroll` in `cmd/aw-sync`. The files under `deploy/aw-sync/` are regenerated from the templates and pinned by a golden test.

**Tech Stack:** Go 1.22 standard library only (`embed`, `text/template`, `encoding/xml`, `syscall`, `unicode/utf16`, `os/exec`).

**Spec:** `docs/superpowers/specs/2026-09-24-aw-sync-install-timer-design.md` (read the "Refinements from planning" section too).

## Global Constraints

- Module `github.com/acme/agent-wrapper`; Go 1.22; **no new modules** in `go.mod` (no `golang.org/x/sys`).
- No gcc: never `-race`. Every task also runs `GOOS=windows go build ./... && GOOS=darwin go build ./...` so the per-OS files compile.
- Supported OSes: `linux` (systemd), `darwin` (launchd), `windows` (Task Scheduler). Anything else: `install-timer supports linux, darwin and windows`.
- Names and paths, exact: `/etc/systemd/system/aw-sync.service`, `/etc/systemd/system/aw-sync.timer`, timer unit name `aw-sync.timer`; `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist`, label `com.agent-wrapper.aw-sync`, log dir `/Library/Logs/agent-wrapper` (0755); task name `agent-wrapper\aw-sync`; lock file `<stateDir>/aw-sync.lock` (0644, never deleted).
- Defaults: interval 5m; binary `/usr/local/bin/aw-sync` (`C:\Program Files\AgentWrapper\aw-sync.exe` on Windows) for the deploy files; `install-timer` uses the running binary (`os.Executable` + `filepath.EvalSymlinks`), never a guessed path.
- `--interval`: Go duration, 1m ≤ d ≤ 24h, whole minutes. Validated before anything touches disk.
- The unit's command is `<binary> once`, plus `--state-dir <dir>` only when the dir differs from `sync.StateDir(goos)`.
- Unit files are written 0644. Install is idempotent (overwrite + reload).
- Commands, exact order — install: linux `systemctl daemon-reload`, `systemctl enable --now aw-sync.timer`; darwin `launchctl print system/com.agent-wrapper.aw-sync` (loaded?), `launchctl bootout system/com.agent-wrapper.aw-sync` only if loaded, `launchctl enable system/com.agent-wrapper.aw-sync`, `launchctl bootstrap system <plist>`; windows `schtasks /Create /TN agent-wrapper\aw-sync /XML <temp file> /F`, temp file removed afterwards. Uninstall: linux `systemctl disable --now aw-sync.timer`, remove both files, `systemctl daemon-reload`; darwin bootout only if loaded, remove plist; windows `schtasks /Query /TN agent-wrapper\aw-sync` then `schtasks /Delete /TN agent-wrapper\aw-sync /F`.
- **Failure modes (every row is a requirement):**
  - install-timer, not enrolled (no `machine.json` in the state dir) → exit 1 `not enrolled; run aw-sync enroll first`; nothing written.
  - install-timer, `--interval` unparseable, outside 1m–24h or not whole minutes → exit 1 before touching disk.
  - install-timer, not root/admin → exit 1 naming the path or command, with a `sudo` (Unix) / elevated-prompt (Windows) hint.
  - install-timer, units written then the OS tool fails → exit 1 naming the command and its output; unit files stay; re-running is safe.
  - install-timer, unsupported GOOS → exit 1 `install-timer supports linux, darwin and windows`.
  - install-timer, running binary path cannot be resolved → exit 1; never a guessed path.
  - uninstall-timer, nothing installed → exit 0 `no timer installed`.
  - uninstall-timer, OS tool fails part-way → exit 1 naming the command; files it could remove are removed; re-running is safe.
  - uninstall-timer never touches enrollment, state or rendered files.
  - once, lock held → exit 0, stdout `note: another aw-sync cycle is running; skipped`.
  - enroll, lock held → exit 1 `another aw-sync is running; retry`, before any network call.
  - once, state dir missing → no lock taken; reports not enrolled as today (exit 1).
  - enroll, state dir missing → creates it (as today), then locks.
  - once/enroll, lock file cannot be opened for another reason → exit 1 with the error.
- `sync.Run` stays lock-free.
- Commit messages: conventional prefix, ending with exactly these two lines:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01UENwpnQH7LP5N1SFDRbxUg`
- Comments match the repo: full sentences that say *why*; doc comment on every exported identifier.

## File Map

| File | Responsibility |
| --- | --- |
| `internal/sync/lock.go` (create) | `Lock`, `ErrLocked`, `LockFile` |
| `internal/sync/lock_unix.go`, `lock_windows.go` (create) | per-OS `lockFile` |
| `internal/sync/timer/timer.go` (create) | `Params`, `Default`, `ValidateInterval`, `Supported`, `Render`, `Unit`, `DeployDir`, constants |
| `internal/sync/timer/units/*` (create) | the four embedded templates |
| `internal/sync/timer/install.go` (create) | `Installer`, `Runner`, `ExecRunner`, `CommandError`, `ErrNotInstalled` |
| `cmd/aw-sync/main.go` (modify) | lock in `once`/`enroll`; `install-timer`, `uninstall-timer`; usage |
| `cmd/aw-sync/deploy_test.go` (rewrite) | golden test with `-update` |
| `deploy/aw-sync/**` (regenerate), `deploy/aw-sync/README.md` | deploy units and docs |

---

### Task 1: The cycle lock

**Files:**
- Create: `internal/sync/lock.go`, `internal/sync/lock_unix.go`, `internal/sync/lock_windows.go`
- Test: `internal/sync/lock_test.go`

**Interfaces:**
- Produces: `const LockFile = "aw-sync.lock"`; `var ErrLocked error`; `func Lock(stateDir string) (release func(), err error)` — `ErrLocked` when held; an error satisfying `errors.Is(err, fs.ErrNotExist)` when the state dir does not exist.

- [ ] **Step 1: Failing tests** — `internal/sync/lock_test.go`:

```go
package sync_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestLockIsExclusiveUntilReleased(t *testing.T) {
	dir := t.TempDir()
	release, err := sync.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Lock(dir); !errors.Is(err, sync.ErrLocked) {
		t.Fatalf("second Lock: err = %v, want ErrLocked", err)
	}
	release()
	again, err := sync.Lock(dir)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	again()
}

func TestLockFileIsLeftInPlace(t *testing.T) {
	dir := t.TempDir()
	release, err := sync.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()
	info, err := os.Stat(filepath.Join(dir, sync.LockFile))
	if err != nil {
		t.Fatalf("lock file after release: %v; deleting it would race a concurrent opener", err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("lock file mode = %v, want world-readable 0644", info.Mode().Perm())
	}
}

func TestLockOnAMissingStateDirIsNotExist(t *testing.T) {
	_, err := sync.Lock(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}
```

(The permission assertion is skipped implicitly on Windows by being lenient; if it fails on Windows CI later, guard it with `runtime.GOOS != "windows"`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/sync/ -run Lock`
Expected: compile error, `undefined: sync.Lock`.

- [ ] **Step 3: Implement**

`internal/sync/lock.go`:

```go
package sync

import (
	"errors"
	"path/filepath"
)

// LockFile is the file in the state directory that one aw-sync cycle holds
// at a time. It is never deleted: removing a lock file races a process that
// has just opened it, and the OS releases the lock itself when a holder
// exits, so a crashed cycle never leaves it stuck.
const LockFile = "aw-sync.lock"

// ErrLocked means another aw-sync cycle holds the lock right now.
var ErrLocked = errors.New("sync: another aw-sync cycle holds the lock")

// Lock takes the state directory's cycle lock without waiting. It returns
// ErrLocked when another process holds it, and an error satisfying
// errors.Is(err, fs.ErrNotExist) when stateDir does not exist. release
// gives the lock back; it is safe to defer.
func Lock(stateDir string) (release func(), err error) {
	return lockFile(filepath.Join(stateDir, LockFile))
}
```

`internal/sync/lock_unix.go`:

```go
//go:build unix

package sync

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile holds an flock on path. flock locks belong to the open file, so
// a second open of the same file conflicts even inside one process, and the
// kernel drops the lock when the process exits.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("sync: opening the lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("sync: locking %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
```

`internal/sync/lock_windows.go`:

```go
//go:build windows

package sync

import (
	"errors"
	"fmt"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION, which the syscall package
// does not name.
const errSharingViolation syscall.Errno = 32

// lockFile opens path with no sharing at all, so any other open fails until
// this handle is closed; Windows closes it when the process exits.
func lockFile(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("sync: lock file path: %w", err)
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errSharingViolation) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("sync: opening the lock file: %w", err)
	}
	return func() { _ = syscall.CloseHandle(h) }, nil
}
```

- [ ] **Step 4: Run tests and cross-compile**

Run: `go test ./internal/sync/ -run Lock -v && GOOS=windows go build ./... && GOOS=darwin go build ./... && go vet ./internal/sync/`
Expected: PASS; builds clean. (`syscall.Errno.Is` maps ERROR_PATH_NOT_FOUND to `fs.ErrNotExist`, so the missing-dir case holds on Windows too.)

- [ ] **Step 5: Commit**

```bash
git add internal/sync/lock.go internal/sync/lock_unix.go internal/sync/lock_windows.go internal/sync/lock_test.go
git commit -m "feat(sync): an exclusive, crash-safe lock for one aw-sync cycle at a time"
```
(with the two trailer lines from Global Constraints)

---

### Task 2: `once` and `enroll` take the lock

**Files:**
- Modify: `cmd/aw-sync/main.go` (`once`, `enroll`)
- Test: `cmd/aw-sync/e2e_test.go` (append)

**Interfaces:**
- Consumes: `sync.Lock`, `sync.ErrLocked` (Task 1).

- [ ] **Step 1: Failing e2e tests** — append to `cmd/aw-sync/e2e_test.go` (add `"github.com/acme/agent-wrapper/internal/sync"` to its imports):

```go
func TestOnceSkipsWhileAnotherCycleHoldsTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the lock test holds the lock from this process; covered by the sync package test on Windows")
	}
	stateDir := t.TempDir()
	release, err := sync.Lock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	stdout, stderr, code := runSync(t, nil, "once", "--state-dir", stateDir)
	if code != 0 || !strings.Contains(stdout, "another aw-sync cycle is running; skipped") {
		t.Errorf("exit %d, stdout %q, stderr %q; want a clean skip", code, stdout, stderr)
	}
}

func TestEnrollRefusesWhileAnotherCycleHoldsTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("see TestOnceSkipsWhileAnotherCycleHoldsTheLock")
	}
	stateDir := t.TempDir()
	release, err := sync.Lock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// A closed port: the refusal must come before any network call.
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "another aw-sync is running; retry") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "machine.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("machine.json written while locked: %v", err)
	}
}
```

The existing `TestOnceWithoutEnrollmentExits1` (state dir exists, empty) must keep passing, and add one more case for a missing state dir:

```go
func TestOnceWithAMissingStateDirSaysNotEnrolled(t *testing.T) {
	_, stderr, code := runSync(t, nil, "once", "--state-dir", filepath.Join(t.TempDir(), "absent"))
	if code != 1 || !strings.Contains(stderr, "enroll") {
		t.Errorf("exit %d, stderr %q; a missing state dir is 'not enrolled', not a lock error", code, stderr)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/aw-sync/ -run 'HoldsTheLock|MissingStateDir' -v`
Expected: FAIL — `once` runs the cycle (exit 1, not enrolled) instead of skipping; `enroll` reaches the network (connection refused) instead of the lock message.

- [ ] **Step 3: Implement** — in `cmd/aw-sync/main.go` add `"io/fs"` to imports.

In `once`, after `fs.Parse` succeeds and before `newRegistry()`:

```go
	// One cycle at a time: a timer tick that lands on a manual run is not a
	// failure worth retrying, so it is skipped with exit 0. A state
	// directory that does not exist yet means the machine is not enrolled,
	// which sync.Run reports exactly as it did before the lock existed.
	release, err := sync.Lock(*stateDir)
	switch {
	case errors.Is(err, sync.ErrLocked):
		fmt.Fprintln(stdout, "note: another aw-sync cycle is running; skipped")
		return nil
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		defer release()
	}
```

Note: `once` already declares `fs` as the flag set variable (`fs, stateDir := newFlagSet("once")`), which shadows the `io/fs` package inside `once`. Rename the flag set variable in `once` to `flags` (every use in that function) so `fs.ErrNotExist` resolves to the package.

In `enroll`, immediately after the existing `os.MkdirAll(*stateDir, 0o755)` block and before `context.WithTimeout`:

```go
	// Enrollment rewrites machine.json, which a running cycle may be about
	// to rewrite too during a key rollover; it waits for nobody, so a held
	// lock is an error to retry, and it comes before the token is spent.
	release, err := sync.Lock(*stateDir)
	if errors.Is(err, sync.ErrLocked) {
		return errors.New("another aw-sync is running; retry")
	}
	if err != nil {
		return err
	}
	defer release()
```

(`enroll` also names its flag set `fs`; it does not use the `io/fs` package, so it needs no rename. If the compiler complains, rename it to `flags` too.)

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/aw-sync/ -v 2>&1 | tail -30 && go vet ./... && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: PASS, including the existing `TestEnrollOnceStatusAndOutage`.

- [ ] **Step 5: Commit**

```bash
git add cmd/aw-sync/main.go cmd/aw-sync/e2e_test.go
git commit -m "feat(aw-sync): one cycle at a time; a colliding once skips cleanly"
```

---

### Task 3: `timer.Render` and the templates

**Files:**
- Create: `internal/sync/timer/timer.go`
- Create: `internal/sync/timer/units/aw-sync.service`, `units/aw-sync.timer`, `units/com.agent-wrapper.aw-sync.plist`, `units/aw-sync-task.xml`
- Test: `internal/sync/timer/timer_test.go`

**Interfaces:**
- Consumes: `sync.StateDir(goos string) string`.
- Produces:

```go
const DefaultInterval = 5 * time.Minute
const ServicePath = "/etc/systemd/system/aw-sync.service"
const TimerPath = "/etc/systemd/system/aw-sync.timer"
const TimerUnit = "aw-sync.timer"
const PlistPath = "/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist"
const Label = "com.agent-wrapper.aw-sync"
const LogDir = "/Library/Logs/agent-wrapper"
const TaskName = `agent-wrapper\aw-sync`
type Params struct { Binary, StateDir string; Interval time.Duration }
type Unit struct { Name, Path string; Content []byte }
func Default(goos string) Params
func ValidateInterval(d time.Duration) error
func Supported(goos string) error
func DeployDir(goos string) string // "systemd", "launchd", "windows"
func Render(goos string, p Params) ([]Unit, error)
```

- [ ] **Step 1: Failing tests** — `internal/sync/timer/timer_test.go`:

```go
package timer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
	"github.com/acme/agent-wrapper/internal/sync/timer"
)

func render(t *testing.T, goos string, p timer.Params) map[string]string {
	t.Helper()
	units, err := timer.Render(goos, p)
	if err != nil {
		t.Fatalf("Render(%s): %v", goos, err)
	}
	out := map[string]string{}
	for _, u := range units {
		out[u.Name] = string(u.Content)
	}
	return out
}

func TestRenderLinuxDefault(t *testing.T) {
	got := render(t, "linux", timer.Default("linux"))
	if !strings.Contains(got["aw-sync.service"], "\nExecStart=/usr/local/bin/aw-sync once\n") {
		t.Errorf("service:\n%s", got["aw-sync.service"])
	}
	if !strings.Contains(got["aw-sync.timer"], "\nOnUnitActiveSec=300s\n") {
		t.Errorf("timer:\n%s", got["aw-sync.timer"])
	}
}

func TestRenderLinuxQuotesAndPassesACustomStateDir(t *testing.T) {
	p := timer.Params{Binary: "/opt/agent wrapper/aw-sync", StateDir: "/srv/aw state", Interval: 15 * time.Minute}
	got := render(t, "linux", p)
	want := "\nExecStart=\"/opt/agent wrapper/aw-sync\" once --state-dir \"/srv/aw state\"\n"
	if !strings.Contains(got["aw-sync.service"], want) {
		t.Errorf("service:\n%s\nwant line %q", got["aw-sync.service"], want)
	}
	if !strings.Contains(got["aw-sync.timer"], "OnUnitActiveSec=900s") {
		t.Errorf("timer:\n%s", got["aw-sync.timer"])
	}
}

func TestRenderLinuxEscapesSystemdSpecifiers(t *testing.T) {
	got := render(t, "linux", timer.Params{Binary: "/opt/100%/aw-sync", Interval: time.Minute})
	if !strings.Contains(got["aw-sync.service"], "ExecStart=/opt/100%%/aw-sync once") {
		t.Errorf("a %% must be doubled for systemd:\n%s", got["aw-sync.service"])
	}
}

func TestRenderDarwinDefault(t *testing.T) {
	got := render(t, "darwin", timer.Default("darwin"))
	plist := got["com.agent-wrapper.aw-sync.plist"]
	for _, want := range []string{
		"<string>com.agent-wrapper.aw-sync</string>",
		"        <string>/usr/local/bin/aw-sync</string>\n        <string>once</string>\n    </array>",
		"<integer>300</integer>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "--state-dir") {
		t.Errorf("the default state dir must not be passed:\n%s", plist)
	}
}

func TestRenderDarwinEscapesXMLAndKeepsSpaces(t *testing.T) {
	p := timer.Params{Binary: "/Applications/A&B/aw-sync", StateDir: "/Library/Application Support/other", Interval: time.Hour}
	plist := render(t, "darwin", p)["com.agent-wrapper.aw-sync.plist"]
	for _, want := range []string{
		"<string>/Applications/A&amp;B/aw-sync</string>",
		"<string>--state-dir</string>",
		"<string>/Library/Application Support/other</string>",
		"<integer>3600</integer>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
}

func TestRenderWindowsDefault(t *testing.T) {
	xml := render(t, "windows", timer.Default("windows"))["aw-sync-task.xml"]
	for _, want := range []string{
		`<Command>C:\Program Files\AgentWrapper\aw-sync.exe</Command>`,
		"<Arguments>once</Arguments>",
		"<Interval>PT5M</Interval>",
		"<UserId>S-1-5-18</UserId>",
		"<BootTrigger>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("task xml lacks %q:\n%s", want, xml)
		}
	}
}

func TestRenderWindowsQuotesAStateDirWithSpaces(t *testing.T) {
	p := timer.Params{Binary: `C:\aw\aw-sync.exe`, StateDir: `D:\Agent Wrapper`, Interval: 10 * time.Minute}
	xml := render(t, "windows", p)["aw-sync-task.xml"]
	if !strings.Contains(xml, `<Arguments>once --state-dir &#34;D:\Agent Wrapper&#34;</Arguments>`) {
		t.Errorf("task xml:\n%s", xml)
	}
	if !strings.Contains(xml, "<Interval>PT10M</Interval>") {
		t.Errorf("task xml:\n%s", xml)
	}
}

func TestUnitsCarryTheirInstallPaths(t *testing.T) {
	for goos, want := range map[string][]string{
		"linux":   {timer.ServicePath, timer.TimerPath},
		"darwin":  {timer.PlistPath},
		"windows": {""},
	} {
		units, err := timer.Render(goos, timer.Default(goos))
		if err != nil {
			t.Fatal(err)
		}
		if len(units) != len(want) {
			t.Fatalf("%s: %d units, want %d", goos, len(units), len(want))
		}
		for i, u := range units {
			if u.Path != want[i] {
				t.Errorf("%s unit %s: path %q, want %q", goos, u.Name, u.Path, want[i])
			}
		}
	}
}

func TestIntervalLimits(t *testing.T) {
	for _, bad := range []time.Duration{0, 30 * time.Second, 90 * time.Second, 25 * time.Hour} {
		if err := timer.ValidateInterval(bad); err == nil {
			t.Errorf("ValidateInterval(%s) = nil, want an error", bad)
		}
		if _, err := timer.Render("linux", timer.Params{Binary: "/b", Interval: bad}); err == nil {
			t.Errorf("Render accepted interval %s", bad)
		}
	}
	for _, good := range []time.Duration{time.Minute, 5 * time.Minute, 24 * time.Hour} {
		if err := timer.ValidateInterval(good); err != nil {
			t.Errorf("ValidateInterval(%s) = %v", good, err)
		}
	}
}

func TestUnsupportedOS(t *testing.T) {
	if err := timer.Supported("freebsd"); err == nil || !strings.Contains(err.Error(), "install-timer supports linux, darwin and windows") {
		t.Errorf("Supported(freebsd) = %v", err)
	}
	if _, err := timer.Render("freebsd", timer.Params{Binary: "/b", Interval: time.Minute}); err == nil {
		t.Error("Render(freebsd) succeeded")
	}
}

func TestDefaultUsesTheOSStateDir(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if got := timer.Default(goos).StateDir; got != sync.StateDir(goos) {
			t.Errorf("%s: %q", goos, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/sync/timer/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Templates** — create exactly these files.

`internal/sync/timer/units/aw-sync.service`:

```
[Unit]
Description=Sync agent governance files from the agent-wrapper control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart={{.ExecStart}}
# The state directory holds the machine credential; nothing else needs it.
StateDirectory=agent-wrapper
StateDirectoryMode=0755
```

`internal/sync/timer/units/aw-sync.timer`:

```
[Unit]
Description=Run aw-sync on a timer

[Timer]
OnBootSec=2min
OnUnitActiveSec={{.Seconds}}s
RandomizedDelaySec=60
Persistent=true

[Install]
WantedBy=timers.target
```

`internal/sync/timer/units/com.agent-wrapper.aw-sync.plist`:

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{xml .Label}}</string>
    <key>ProgramArguments</key>
    <array>
{{- range .Args}}
        <string>{{xml .}}</string>
{{- end}}
    </array>
    <key>StartInterval</key>
    <integer>{{.Seconds}}</integer>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Library/Logs/agent-wrapper/aw-sync.log</string>
    <key>StandardErrorPath</key>
    <string>/Library/Logs/agent-wrapper/aw-sync.log</string>
</dict>
</plist>
```

`internal/sync/timer/units/aw-sync-task.xml`:

```
<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Sync agent governance files from the agent-wrapper control plane</Description>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
    </BootTrigger>
    <TimeTrigger>
      <Repetition>
        <Interval>PT{{.Minutes}}M</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
      <StartBoundary>2026-01-01T00:00:00</StartBoundary>
      <Enabled>true</Enabled>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>S-1-5-18</UserId>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT10M</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{{xml .Command}}</Command>
      <Arguments>{{xml .Arguments}}</Arguments>
    </Exec>
  </Actions>
</Task>
```

(Every template ends with a single trailing newline.)

- [ ] **Step 4: Implement** — `internal/sync/timer/timer.go`:

```go
// Package timer renders the units that run `aw-sync once` periodically on
// each supported OS, and installs or removes them. The templates embedded
// here are the one source for both `aw-sync install-timer` and the files
// shipped under deploy/aw-sync/, which a golden test keeps identical.
package timer

import (
	"bytes"
	"embed"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

//go:embed units/*
var unitFS embed.FS

// Where the units live and what they are called.
const (
	ServicePath = "/etc/systemd/system/aw-sync.service"
	TimerPath   = "/etc/systemd/system/aw-sync.timer"
	TimerUnit   = "aw-sync.timer"
	PlistPath   = "/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist"
	Label       = "com.agent-wrapper.aw-sync"
	LogDir      = "/Library/Logs/agent-wrapper"
	TaskName    = `agent-wrapper\aw-sync`
)

// Interval bounds. Task Scheduler repeats in whole minutes, so every OS
// takes the same rule rather than Windows alone rejecting 90s.
const (
	DefaultInterval = 5 * time.Minute
	MinInterval     = time.Minute
	MaxInterval     = 24 * time.Hour
)

// Params is what a unit needs: which binary to run, which state directory
// to pass it, and how often.
type Params struct {
	Binary   string
	StateDir string
	Interval time.Duration
}

// Unit is one rendered file. Name is its file name under
// deploy/aw-sync/<DeployDir>/; Path is where install-timer writes it, empty
// on Windows, where the task is registered from a temporary file.
type Unit struct {
	Name    string
	Path    string
	Content []byte
}

// Default is what the deploy files are rendered with: the conventional
// binary location, the OS state directory and a five-minute interval.
func Default(goos string) Params {
	binary := "/usr/local/bin/aw-sync"
	if goos == "windows" {
		binary = `C:\Program Files\AgentWrapper\aw-sync.exe`
	}
	return Params{Binary: binary, StateDir: sync.StateDir(goos), Interval: DefaultInterval}
}

// ValidateInterval reports whether d can be scheduled on every OS.
func ValidateInterval(d time.Duration) error {
	if d < MinInterval || d > MaxInterval {
		return fmt.Errorf("--interval must be between 1m and 24h, got %s", d)
	}
	if d%time.Minute != 0 {
		return fmt.Errorf("--interval must be a whole number of minutes, got %s", d)
	}
	return nil
}

// Supported reports whether install-timer knows goos's scheduler.
func Supported(goos string) error {
	switch goos {
	case "linux", "darwin", "windows":
		return nil
	}
	return fmt.Errorf("install-timer supports linux, darwin and windows, not %s", goos)
}

// DeployDir is the directory under deploy/aw-sync/ holding goos's units.
func DeployDir(goos string) string {
	switch goos {
	case "darwin":
		return "launchd"
	case "windows":
		return "windows"
	default:
		return "systemd"
	}
}

var templates = template.Must(template.New("units").Funcs(template.FuncMap{"xml": xmlEscape}).ParseFS(unitFS, "units/*"))

// Render fills goos's templates with p. It never touches the filesystem.
func Render(goos string, p Params) ([]Unit, error) {
	if err := Supported(goos); err != nil {
		return nil, err
	}
	if err := ValidateInterval(p.Interval); err != nil {
		return nil, err
	}
	if p.Binary == "" {
		return nil, errors.New("timer: no binary to run")
	}
	// The default state directory is left implicit, so the deploy files
	// read exactly as an operator would write them by hand.
	args := []string{"once"}
	if p.StateDir != "" && p.StateDir != sync.StateDir(goos) {
		args = append(args, "--state-dir", p.StateDir)
	}
	seconds := int(p.Interval / time.Second)
	switch goos {
	case "linux":
		data := map[string]any{"ExecStart": systemdCommand(append([]string{p.Binary}, args...)), "Seconds": seconds}
		service, err := execute("aw-sync.service", data)
		if err != nil {
			return nil, err
		}
		timerUnit, err := execute("aw-sync.timer", data)
		if err != nil {
			return nil, err
		}
		return []Unit{
			{Name: "aw-sync.service", Path: ServicePath, Content: service},
			{Name: "aw-sync.timer", Path: TimerPath, Content: timerUnit},
		}, nil
	case "darwin":
		data := map[string]any{"Label": Label, "Args": append([]string{p.Binary}, args...), "Seconds": seconds}
		plist, err := execute("com.agent-wrapper.aw-sync.plist", data)
		if err != nil {
			return nil, err
		}
		return []Unit{{Name: "com.agent-wrapper.aw-sync.plist", Path: PlistPath, Content: plist}}, nil
	default: // windows
		data := map[string]any{"Command": p.Binary, "Arguments": windowsArgs(args), "Minutes": seconds / 60}
		task, err := execute("aw-sync-task.xml", data)
		if err != nil {
			return nil, err
		}
		return []Unit{{Name: "aw-sync-task.xml", Content: task}}, nil
	}
}

func execute(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("timer: rendering %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// systemdCommand joins words into an ExecStart value. systemd expands %
// specifiers and $ variables even inside quotes, so both are doubled;
// words with spaces, quotes or backslashes are double-quoted.
func systemdCommand(words []string) string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, "%", "%%")
		w = strings.ReplaceAll(w, "$", "$$")
		if strings.ContainsAny(w, " \t\"'\\") {
			w = `"` + strings.ReplaceAll(strings.ReplaceAll(w, `\`, `\\`), `"`, `\"`) + `"`
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// windowsArgs joins arguments the way Windows' command-line parser splits
// them back: an argument with a space or quote is quoted, inner quotes
// escaped.
func windowsArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}
```

Check against the tests: `xml.EscapeText` escapes `"` as `&#34;` (the Windows quote test expects that) and `&` as `&amp;`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/sync/timer/ -v && go vet ./internal/sync/... && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/sync/timer/
git commit -m "feat(sync): render aw-sync's timer units for systemd, launchd and Task Scheduler"
```

---

### Task 4: `timer.Installer`

**Files:**
- Create: `internal/sync/timer/install.go`
- Test: `internal/sync/timer/install_test.go`

**Interfaces:**
- Consumes: Task 3 (`Render`, constants, `Supported`), `cache.ReplaceMode(path string, data []byte, mode fs.FileMode) error` from `internal/cache`.
- Produces:

```go
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error)
var ErrNotInstalled error
type CommandError struct{ Command, Output string; Err error } // Error(), Unwrap()
type Installer struct{ GOOS string; Run Runner; Root, TempDir string }
func NewInstaller(goos string) Installer
func (in Installer) Install(ctx context.Context, p Params) error
func (in Installer) Uninstall(ctx context.Context) error
```

- [ ] **Step 1: Failing tests** — `internal/sync/timer/install_test.go`:

```go
package timer_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/acme/agent-wrapper/internal/sync/timer"
)

// recorder stands in for the OS tools: it records every command and fails
// the ones listed in fail.
type recorder struct {
	calls []string
	fail  map[string]bool
	seen  map[string][]byte // a copy of any file named by schtasks /XML
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, cmd)
	if name == "schtasks" && len(args) > 4 && args[0] == "/Create" {
		raw, _ := os.ReadFile(args[4])
		if r.seen == nil {
			r.seen = map[string][]byte{}
		}
		r.seen["xml"] = raw
	}
	if r.fail[cmd] {
		return []byte("tool said no"), errors.New("exit status 1")
	}
	return nil, nil
}

func installer(t *testing.T, goos string, r *recorder) timer.Installer {
	t.Helper()
	return timer.Installer{GOOS: goos, Run: r.run, Root: t.TempDir(), TempDir: t.TempDir()}
}

var params = timer.Params{Binary: "/usr/local/bin/aw-sync", Interval: 5 * time.Minute}

func equalCalls(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestInstallLinux(t *testing.T) {
	r := &recorder{}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"systemctl daemon-reload", "systemctl enable --now aw-sync.timer"})
	for _, p := range []string{timer.ServicePath, timer.TimerPath} {
		info, err := os.Stat(filepath.Join(in.Root, p))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode %v, want 0644", p, info.Mode().Perm())
		}
	}
	// Re-installing is how the interval changes: same commands, new file.
	r.calls = nil
	if err := in.Install(context.Background(), timer.Params{Binary: params.Binary, Interval: 10 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"systemctl daemon-reload", "systemctl enable --now aw-sync.timer"})
	raw, _ := os.ReadFile(filepath.Join(in.Root, timer.TimerPath))
	if !strings.Contains(string(raw), "OnUnitActiveSec=600s") {
		t.Errorf("re-install did not rewrite the timer:\n%s", raw)
	}
}

func TestInstallLinuxReportsAFailingToolAndKeepsTheUnits(t *testing.T) {
	r := &recorder{fail: map[string]bool{"systemctl enable --now aw-sync.timer": true}}
	in := installer(t, "linux", r)
	err := in.Install(context.Background(), params)
	var cmdErr *timer.CommandError
	if !errors.As(err, &cmdErr) || !strings.Contains(err.Error(), "systemctl enable --now aw-sync.timer") || !strings.Contains(err.Error(), "tool said no") {
		t.Fatalf("err = %v; want the command and its output", err)
	}
	if _, err := os.Stat(filepath.Join(in.Root, timer.ServicePath)); err != nil {
		t.Errorf("unit files must stay after a tool failure so a re-run is safe: %v", err)
	}
}

func TestInstallDarwinNotLoaded(t *testing.T) {
	r := &recorder{fail: map[string]bool{"launchctl print system/com.agent-wrapper.aw-sync": true}}
	in := installer(t, "darwin", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{
		"launchctl print system/com.agent-wrapper.aw-sync",
		"launchctl bootstrap system " + filepath.Join(in.Root, timer.PlistPath),
	})
	if info, err := os.Stat(filepath.Join(in.Root, timer.LogDir)); err != nil || !info.IsDir() {
		t.Errorf("log dir: %v", err)
	}
}

func TestInstallDarwinLoadedBootsOutFirst(t *testing.T) {
	r := &recorder{}
	in := installer(t, "darwin", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{
		"launchctl print system/com.agent-wrapper.aw-sync",
		"launchctl bootout system/com.agent-wrapper.aw-sync",
		"launchctl bootstrap system " + filepath.Join(in.Root, timer.PlistPath),
	})
}

func TestInstallWindowsStagesUTF16AndCleansUp(t *testing.T) {
	r := &recorder{}
	in := installer(t, "windows", r)
	if err := in.Install(context.Background(), timer.Params{Binary: `C:\aw\aw-sync.exe`, Interval: 5 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0], `schtasks /Create /TN agent-wrapper\aw-sync /XML `) || !strings.HasSuffix(r.calls[0], " /F") {
		t.Fatalf("commands: %v", r.calls)
	}
	raw := r.seen["xml"]
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
		t.Fatalf("staged task XML must start with a UTF-16LE BOM, got % x", raw[:min(4, len(raw))])
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 2; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	text := string(utf16.Decode(units))
	if !strings.HasPrefix(text, `<?xml version="1.0" encoding="UTF-16"?>`) || !strings.Contains(text, `<Command>C:\aw\aw-sync.exe</Command>`) {
		t.Errorf("decoded task XML:\n%s", text)
	}
	left, _ := os.ReadDir(in.TempDir)
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestUninstallLinux(t *testing.T) {
	r := &recorder{}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	if err := in.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"systemctl disable --now aw-sync.timer", "systemctl daemon-reload"})
	for _, p := range []string{timer.ServicePath, timer.TimerPath} {
		if _, err := os.Stat(filepath.Join(in.Root, p)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still there: %v", p, err)
		}
	}
}

func TestUninstallLinuxRemovesFilesEvenWhenDisableFails(t *testing.T) {
	r := &recorder{fail: map[string]bool{"systemctl disable --now aw-sync.timer": true}}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	err := in.Uninstall(context.Background())
	if err == nil || !strings.Contains(err.Error(), "systemctl disable --now aw-sync.timer") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(in.Root, timer.ServicePath)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("service file kept after a partial failure: %v", err)
	}
}

func TestUninstallWithNothingInstalled(t *testing.T) {
	for goos, r := range map[string]*recorder{
		"linux":   {},
		"darwin":  {fail: map[string]bool{"launchctl print system/com.agent-wrapper.aw-sync": true}},
		"windows": {fail: map[string]bool{`schtasks /Query /TN agent-wrapper\aw-sync`: true}},
	} {
		in := installer(t, goos, r)
		if err := in.Uninstall(context.Background()); !errors.Is(err, timer.ErrNotInstalled) {
			t.Errorf("%s: err = %v, want ErrNotInstalled", goos, err)
		}
		for _, c := range r.calls {
			if !strings.Contains(c, "print") && !strings.Contains(c, "/Query") {
				t.Errorf("%s: ran %q with nothing installed", goos, c)
			}
		}
	}
}

func TestUninstallDarwinAndWindows(t *testing.T) {
	r := &recorder{}
	in := installer(t, "darwin", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	if err := in.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"launchctl print system/com.agent-wrapper.aw-sync", "launchctl bootout system/com.agent-wrapper.aw-sync"})
	if _, err := os.Stat(filepath.Join(in.Root, timer.PlistPath)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plist kept: %v", err)
	}

	w := &recorder{}
	if err := installer(t, "windows", w).Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, w.calls, []string{`schtasks /Query /TN agent-wrapper\aw-sync`, `schtasks /Delete /TN agent-wrapper\aw-sync /F`})
}

func TestInstallRejectsBadParamsBeforeWriting(t *testing.T) {
	r := &recorder{}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), timer.Params{Binary: "/b", Interval: 30 * time.Second}); err == nil {
		t.Fatal("30s accepted")
	}
	if len(r.calls) != 0 {
		t.Errorf("ran %v", r.calls)
	}
	if entries, _ := os.ReadDir(in.Root); len(entries) != 0 {
		t.Errorf("wrote %v", entries)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/sync/timer/ -run 'Install|Uninstall'`
Expected: compile error, `undefined: timer.Installer`.

- [ ] **Step 3: Implement** — `internal/sync/timer/install.go`:

```go
package timer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/acme/agent-wrapper/internal/cache"
)

// Runner runs one OS tool and returns its combined output. Tests replace
// it so no test ever calls systemctl, launchctl or schtasks.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the real tool.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ErrNotInstalled means there is no timer to remove.
var ErrNotInstalled = errors.New("no timer installed")

// CommandError is an OS tool that failed. It carries the command line and
// what the tool printed, which is what an MDM log needs to diagnose it.
type CommandError struct {
	Command string
	Output  string
	Err     error
}

func (e *CommandError) Error() string {
	msg := fmt.Sprintf("running `%s`: %v", e.Command, e.Err)
	if out := strings.TrimSpace(e.Output); out != "" {
		msg += ": " + out
	}
	return msg
}

func (e *CommandError) Unwrap() error { return e.Err }

// Installer installs and removes the timer on one OS.
type Installer struct {
	GOOS string
	Run  Runner
	// Root prefixes every file path written or removed; empty means the
	// real filesystem root. Tests point it at a temporary directory.
	Root string
	// TempDir is where Windows stages the task XML; empty means os.TempDir().
	TempDir string
}

// NewInstaller is the Installer for goos that runs the real tools.
func NewInstaller(goos string) Installer {
	return Installer{GOOS: goos, Run: ExecRunner}
}

func (in Installer) path(p string) string {
	if in.Root == "" {
		return p
	}
	return filepath.Join(in.Root, filepath.FromSlash(p))
}

func (in Installer) run(ctx context.Context, name string, args ...string) error {
	out, err := in.Run(ctx, name, args...)
	if err != nil {
		return &CommandError{Command: strings.Join(append([]string{name}, args...), " "), Output: string(out), Err: err}
	}
	return nil
}

// loaded asks launchd whether the job is loaded: `launchctl print` exits 0
// only for a job it knows.
func (in Installer) loaded(ctx context.Context) bool {
	_, err := in.Run(ctx, "launchctl", "print", "system/"+Label)
	return err == nil
}

// Install writes goos's units and loads them. Re-running it overwrites and
// reloads, which is how the interval or binary path changes. When a tool
// fails after the units are written, the units stay: running Install again
// is safe.
func (in Installer) Install(ctx context.Context, p Params) error {
	units, err := Render(in.GOOS, p)
	if err != nil {
		return err
	}
	switch in.GOOS {
	case "linux":
		if err := in.write(units); err != nil {
			return err
		}
		if err := in.run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		return in.run(ctx, "systemctl", "enable", "--now", TimerUnit)
	case "darwin":
		if err := os.MkdirAll(in.path(LogDir), 0o755); err != nil {
			return fmt.Errorf("timer: creating %s: %w", in.path(LogDir), err)
		}
		if err := in.write(units); err != nil {
			return err
		}
		if in.loaded(ctx) {
			if err := in.run(ctx, "launchctl", "bootout", "system/"+Label); err != nil {
				return err
			}
		}
		return in.run(ctx, "launchctl", "bootstrap", "system", in.path(PlistPath))
	default: // windows; Render has already refused anything else
		return in.register(ctx, units[0].Content)
	}
}

func (in Installer) write(units []Unit) error {
	for _, u := range units {
		path := in.path(u.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("timer: creating %s: %w", filepath.Dir(path), err)
		}
		if err := cache.ReplaceMode(path, u.Content, 0o644); err != nil {
			return fmt.Errorf("timer: writing %s: %w", path, err)
		}
	}
	return nil
}

// register stages the task XML as UTF-16LE with a BOM — the encoding Task
// Scheduler itself exports and schtasks reliably reads — then registers it
// and removes the staged file.
func (in Installer) register(ctx context.Context, content []byte) error {
	f, err := os.CreateTemp(in.TempDir, "aw-sync-task-*.xml")
	if err != nil {
		return fmt.Errorf("timer: staging the task XML: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	_, werr := f.Write(utf16LE(content))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("timer: staging the task XML: %w", werr)
	}
	return in.run(ctx, "schtasks", "/Create", "/TN", TaskName, "/XML", name, "/F")
}

func utf16LE(content []byte) []byte {
	text := strings.Replace(string(content), `encoding="UTF-8"`, `encoding="UTF-16"`, 1)
	units := utf16.Encode([]rune(text))
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFE})
	for _, u := range units {
		buf.WriteByte(byte(u))
		buf.WriteByte(byte(u >> 8))
	}
	return buf.Bytes()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Uninstall stops and removes the timer. It never touches enrollment,
// state or rendered files. With nothing installed it returns
// ErrNotInstalled; when a tool fails part-way it still removes every file
// it can, so running it again is safe.
func (in Installer) Uninstall(ctx context.Context) error {
	if err := Supported(in.GOOS); err != nil {
		return err
	}
	switch in.GOOS {
	case "linux":
		service, timerFile := in.path(ServicePath), in.path(TimerPath)
		if !exists(service) && !exists(timerFile) {
			return ErrNotInstalled
		}
		var errs []error
		if err := in.run(ctx, "systemctl", "disable", "--now", TimerUnit); err != nil {
			errs = append(errs, err)
		}
		errs = append(errs, remove(timerFile), remove(service))
		if err := in.run(ctx, "systemctl", "daemon-reload"); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	case "darwin":
		plist := in.path(PlistPath)
		loaded := in.loaded(ctx)
		if !loaded && !exists(plist) {
			return ErrNotInstalled
		}
		var errs []error
		if loaded {
			if err := in.run(ctx, "launchctl", "bootout", "system/"+Label); err != nil {
				errs = append(errs, err)
			}
		}
		errs = append(errs, remove(plist))
		return errors.Join(errs...)
	default: // windows
		if _, err := in.Run(ctx, "schtasks", "/Query", "/TN", TaskName); err != nil {
			return ErrNotInstalled
		}
		return in.run(ctx, "schtasks", "/Delete", "/TN", TaskName, "/F")
	}
}

func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("timer: removing %s: %w", path, err)
	}
	return nil
}
```

(`errors.Join` drops nil entries, so appending `remove(...)` results unconditionally is fine.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/sync/timer/ -v && go vet ./internal/sync/... && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sync/timer/install.go internal/sync/timer/install_test.go
git commit -m "feat(sync): install and remove the aw-sync timer through systemctl, launchctl and schtasks"
```

---

### Task 5: `aw-sync install-timer` and `uninstall-timer`

**Files:**
- Modify: `cmd/aw-sync/main.go`
- Test: `cmd/aw-sync/e2e_test.go` (append; extend `TestHelpListsTheCommands`)

**Interfaces:**
- Consumes: `timer.Supported`, `timer.ValidateInterval`, `timer.DefaultInterval`, `timer.NewInstaller`, `timer.Params`, `timer.Render`, `timer.TaskName`, `timer.ErrNotInstalled` (Tasks 3–4); `sync.MachineFile`.

- [ ] **Step 1: Failing e2e tests** — append to `cmd/aw-sync/e2e_test.go`:

```go
func TestInstallTimerRefusesAnUnenrolledMachine(t *testing.T) {
	stateDir := t.TempDir()
	_, stderr, code := runSync(t, nil, "install-timer", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "not enrolled; run aw-sync enroll first") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if entries, _ := os.ReadDir(stateDir); len(entries) != 0 {
		t.Errorf("wrote into the state dir: %v", entries)
	}
}

func TestInstallTimerRejectsBadIntervalsFirst(t *testing.T) {
	for _, interval := range []string{"30s", "90s", "25h", "soon"} {
		// Even on an unenrolled dir the interval is the reported problem:
		// it is checked before anything else.
		_, stderr, code := runSync(t, nil, "install-timer", "--interval", interval, "--state-dir", t.TempDir())
		if code != 1 || !strings.Contains(stderr, "interval") {
			t.Errorf("--interval %s: exit %d, stderr %q", interval, code, stderr)
		}
	}
}

func TestInstallTimerRejectsStrayArguments(t *testing.T) {
	_, stderr, code := runSync(t, nil, "install-timer", "now")
	if code != 1 || !strings.Contains(stderr, "no arguments") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	_, stderr, code = runSync(t, nil, "uninstall-timer", "now")
	if code != 1 || !strings.Contains(stderr, "no arguments") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
```

In `TestHelpListsTheCommands`, extend the list to `[]string{"enroll", "once", "status", "install-timer", "uninstall-timer", "AW_SYNC_TOKEN"}`.

(A real install and uninstall are not e2e-tested: they need root and a live init system, and would change the machine running the tests. Task 4 covers the command sequences.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/aw-sync/ -run 'InstallTimer|Help' -v`
Expected: FAIL — `unknown command "install-timer"`.

- [ ] **Step 3: Implement** in `cmd/aw-sync/main.go` (imports: add `"github.com/acme/agent-wrapper/internal/sync/timer"`; `"io/fs"` is already imported from Task 2).

Package doc comment — add after the `status` line:

```
//	aw-sync install-timer [--interval 5m]      run `once` periodically (systemd, launchd or Task Scheduler)
//	aw-sync uninstall-timer                    stop and remove that timer; enrollment and files stay
```

`usage` — add after the `status` line:

```
  aw-sync install-timer [--interval 5m] [--state-dir DIR]
  aw-sync uninstall-timer
```

and after the `once exits 1…` line:

```
install-timer and uninstall-timer need root (an elevated prompt on Windows).
```

`run` switch — add:

```go
	case "install-timer":
		return helpOr(installTimer(argv[1:], stdout), stdout)
	case "uninstall-timer":
		return helpOr(uninstallTimer(argv[1:], stdout), stdout)
```

New functions:

```go
// installTimer schedules `aw-sync once` for this machine. The unit runs the
// binary that is running now, so a binary installed somewhere other than
// /usr/local/bin still syncs; re-running it changes the interval.
func installTimer(argv []string, stdout io.Writer) error {
	flags, stateDir := newFlagSet("install-timer")
	interval := flags.Duration("interval", timer.DefaultInterval, "how often to run once (1m to 24h, whole minutes)")
	if err := flags.Parse(argv); err != nil {
		if strings.Contains(err.Error(), "interval") {
			return fmt.Errorf("--interval: %w", err)
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("install-timer takes no arguments")
	}
	if err := timer.ValidateInterval(*interval); err != nil {
		return err
	}
	if err := timer.Supported(runtime.GOOS); err != nil {
		return err
	}
	dir, err := filepath.Abs(*stateDir)
	if err != nil {
		return err
	}
	// A timer on an unenrolled machine would only log "not enrolled" every
	// interval. machine.json is root-only, so its existence is the test.
	if _, err := os.Stat(filepath.Join(dir, sync.MachineFile)); err != nil {
		return errors.New("not enrolled; run aw-sync enroll first")
	}
	binary, err := runningBinary()
	if err != nil {
		return err
	}
	p := timer.Params{Binary: binary, StateDir: dir, Interval: *interval}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := timer.NewInstaller(runtime.GOOS).Install(ctx, p); err != nil {
		return privilegeHint(err, "install-timer")
	}
	fmt.Fprintf(stdout, "installed the aw-sync timer: %s once, every %s\n", binary, *interval)
	units, _ := timer.Render(runtime.GOOS, p)
	for _, u := range units {
		if u.Path != "" {
			fmt.Fprintln(stdout, "  "+u.Path)
		}
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(stdout, "  scheduled task "+timer.TaskName)
	}
	fmt.Fprintln(stdout, "check it with: aw-sync status")
	return nil
}

// uninstallTimer stops future syncs. Enrollment, state and the rendered
// files are left exactly as they are.
func uninstallTimer(argv []string, stdout io.Writer) error {
	flags, _ := newFlagSet("uninstall-timer")
	if err := flags.Parse(argv); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("uninstall-timer takes no arguments")
	}
	if err := timer.Supported(runtime.GOOS); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := timer.NewInstaller(runtime.GOOS).Uninstall(ctx)
	switch {
	case errors.Is(err, timer.ErrNotInstalled):
		fmt.Fprintln(stdout, "no timer installed")
		return nil
	case err != nil:
		return privilegeHint(err, "uninstall-timer")
	}
	fmt.Fprintln(stdout, "removed the aw-sync timer; this machine stays enrolled and keeps its files")
	return nil
}

// runningBinary is the path of this executable with symlinks resolved, so
// the unit keeps working if a convenience symlink is later removed. It
// never guesses: an unresolvable path is an error.
func runningBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot resolve the running aw-sync binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the running aw-sync binary: %w", err)
	}
	return resolved, nil
}

// privilegeHint adds how to get the privilege the timer commands need,
// since "permission denied" from systemctl does not say what to do.
func privilegeHint(err error, command string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("%w (run aw-sync %s from an elevated prompt)", err, command)
	}
	if errors.Is(err, fs.ErrPermission) || os.Geteuid() != 0 {
		return fmt.Errorf("%w (run as root: sudo aw-sync %s)", err, command)
	}
	return err
}
```

Note on `flags.Duration`: `flag` reports an unparseable value as `invalid value "soon" for flag -interval: …`, which already contains "interval"; the `strings.Contains` branch just keeps that message. Keep `newFlagSet`'s `SetOutput(io.Discard)` behaviour.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/aw-sync/ -v 2>&1 | tail -40 && go vet ./... && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/aw-sync/main.go cmd/aw-sync/e2e_test.go
git commit -m "feat(aw-sync): install-timer and uninstall-timer"
```

---

### Task 6: Deploy files from the templates, golden test, docs

**Files:**
- Rewrite: `cmd/aw-sync/deploy_test.go`
- Regenerate: `deploy/aw-sync/systemd/aw-sync.service`, `deploy/aw-sync/systemd/aw-sync.timer`, `deploy/aw-sync/launchd/com.agent-wrapper.aw-sync.plist`
- Create: `deploy/aw-sync/windows/aw-sync-task.xml`; Delete: `deploy/aw-sync/windows/register-task.ps1`
- Modify: `deploy/aw-sync/README.md`, `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md`, `docs/superpowers/specs/2026-09-24-aw-sync-install-timer-design.md` (status line)

**Interfaces:**
- Consumes: `timer.Render`, `timer.Default`, `timer.DeployDir` (Task 3).

- [ ] **Step 1: Golden test** — replace `cmd/aw-sync/deploy_test.go` entirely:

```go
package main_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync/timer"
)

var update = flag.Bool("update", false, "rewrite deploy/aw-sync's units from the embedded templates")

// The deploy units are what an organization ships when it pushes files
// instead of running install-timer. They must be exactly what
// install-timer would install with the default binary path and interval,
// so the two ways of installing cannot drift apart.
func TestDeployUnitsMatchTheTemplates(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "aw-sync")
	for _, goos := range []string{"linux", "darwin", "windows"} {
		units, err := timer.Render(goos, timer.Default(goos))
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		for _, u := range units {
			path := filepath.Join(root, timer.DeployDir(goos), u.Name)
			if !bytes.Contains(u.Content, []byte("once")) {
				t.Errorf("%s does not run `aw-sync once`; it would sync nothing, fleet-wide", path)
			}
			if *update {
				if err := os.WriteFile(path, u.Content, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				continue
			}
			if !bytes.Equal(got, u.Content) {
				t.Errorf("%s differs from timer.Render(%q, Default); regenerate with: go test ./cmd/aw-sync -run TestDeployUnitsMatchTheTemplates -update", path, goos)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Errorf("deploy/aw-sync/README.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "windows", "register-task.ps1")); err == nil {
		t.Error("windows/register-task.ps1 still ships; aw-sync-task.xml replaced it")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/aw-sync/ -run TestDeployUnitsMatchTheTemplates`
Expected: FAIL — `aw-sync.timer` differs (template says `OnUnitActiveSec=300s`), `aw-sync-task.xml` missing, `register-task.ps1` still present.

- [ ] **Step 3: Regenerate and remove**

```bash
git rm deploy/aw-sync/windows/register-task.ps1
go test ./cmd/aw-sync/ -run TestDeployUnitsMatchTheTemplates -update
go test ./cmd/aw-sync/ -run TestDeployUnitsMatchTheTemplates
git diff --stat deploy/
```
Expected: the second run PASSES. Read `git diff deploy/` and confirm the only changes are: the timer's `Description` and `OnUnitActiveSec=300s`, the plist's `ProgramArguments` layout if any whitespace changed, and the new task XML. Nothing else in the service or plist should change meaning.

- [ ] **Step 4: README** — in `deploy/aw-sync/README.md`, replace the whole `## 3. Install the timer` section with:

```markdown
## 3. Install the timer

    sudo aw-sync install-timer

(On Windows, `aw-sync.exe install-timer` from an elevated prompt.) It
refuses a machine that is not enrolled, schedules `aw-sync once` every five
minutes — `--interval 15m` changes that, from 1m to 24h in whole minutes —
and runs the binary you invoked, wherever it is installed. Running it again
replaces the timer. `sudo aw-sync uninstall-timer` removes it and leaves the
enrollment and the rendered files alone.

| OS | What it installs |
| --- | --- |
| Linux | `/etc/systemd/system/aw-sync.service` and `aw-sync.timer`, enabled and started |
| macOS | `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist`, bootstrapped into the system domain; logs to `/Library/Logs/agent-wrapper/aw-sync.log` |
| Windows | the scheduled task `agent-wrapper\aw-sync`, as SYSTEM, every interval and at startup |

### Shipping the units yourself

MDM pipelines that push files rather than run commands can ship the units
in this directory; they are exactly what `install-timer` writes for
`/usr/local/bin/aw-sync` (`C:\Program Files\AgentWrapper\aw-sync.exe`) and a
five-minute interval, and a test keeps them that way.

Linux (systemd):

    install -m 0644 systemd/aw-sync.service systemd/aw-sync.timer /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable --now aw-sync.timer

Linux without systemd has no unit here; run `aw-sync once` from cron every
five minutes instead.

macOS (launchd):

    install -d -m 0755 /Library/Logs/agent-wrapper
    install -m 0644 launchd/com.agent-wrapper.aw-sync.plist /Library/LaunchDaemons/
    launchctl bootstrap system /Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist

Windows (Task Scheduler), elevated:

    schtasks /Create /TN "agent-wrapper\aw-sync" /XML windows\aw-sync-task.xml /F
```

In `## 4. Verify`, after the paragraph about `status`, add:

```markdown
Only one cycle runs at a time: a `once` that starts while another is
running (a timer tick during a manual run) prints
`note: another aw-sync cycle is running; skipped` and exits 0.
```

- [ ] **Step 5: Specs** — in `docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md`, replace the text `` `install-timer` is deferred. `` with `` `install-timer` shipped later: see `2026-09-24-aw-sync-install-timer-design.md`. ``. In `docs/superpowers/specs/2026-09-24-aw-sync-install-timer-design.md`, change `Status: approved, not yet implemented.` to `Status: implemented 2026-09-24.`

- [ ] **Step 6: Verify**

Run: `gofmt -l cmd internal && go build ./... && go vet ./... && go test ./... && GOOS=windows go build ./... && GOOS=darwin go build ./...`
Expected: no gofmt output; all PASS.

- [ ] **Step 7: Commit**

```bash
git add deploy/aw-sync cmd/aw-sync/deploy_test.go docs/superpowers/specs/2026-09-21-multi-agent-bundle-sync-design.md docs/superpowers/specs/2026-09-24-aw-sync-install-timer-design.md
git commit -m "docs(aw-sync): install-timer in the install guide; deploy units rendered from the templates"
```

---

## Self-review notes

- Spec coverage: commands (T5), per-OS files/commands (T3, T4), idempotence (T4 re-install test), lock semantics incl. exit codes and missing state dir (T1, T2), architecture/package layout (T3, T4), deploy files + golden (T6), every failure-mode row (Global Constraints; tests in T1, T2, T4, T5), docs (T6), refinements (interval minutes T3; launchctl print T4; UTF-16 T4; nothing-installed definitions T4; Unit shape T3).
- Known unverified: the Windows lock and task registration have never run on Windows here; cross-compilation is the only check. The spec says so.
- Names used across tasks: `sync.Lock`, `sync.ErrLocked`, `sync.LockFile`, `timer.Params`, `timer.Unit{Name, Path, Content}`, `timer.Default`, `timer.ValidateInterval`, `timer.Supported`, `timer.DeployDir`, `timer.Render`, `timer.Installer{GOOS, Run, Root, TempDir}`, `timer.NewInstaller`, `timer.ErrNotInstalled`, `timer.CommandError`, `timer.TaskName`, `timer.DefaultInterval`.
