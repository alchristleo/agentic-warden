# aw-sync installs its own timer, and one cycle runs at a time

Date: 2026-09-24. Status: implemented 2026-09-24. Extends
`2026-09-21-multi-agent-bundle-sync-design.md`, which shipped timer units
under `deploy/aw-sync/` and said "`install-timer` is deferred". This is
that command, plus the lock the M4b review parked ("no lock for
overlapping `once`").

## Why this exists

Installing governance on a machine is meant to be "install the binaries,
enroll". Today there is a third step: copy two systemd units, a plist or a
PowerShell script into place and run the right `systemctl`/`launchctl`
incantation, by hand or in an MDM script someone has to write per OS. The
units also hard-code `/usr/local/bin/aw-sync`, so a binary installed
anywhere else syncs nothing, silently.

A timer also makes overlap likely: an operator runs `sudo aw-sync once`
while the timer fires, and two cycles race on `state.json`, the rendered
files and `machine.json` (a key rollover rewrites it).

## Decisions

- **Scope:** `aw-sync install-timer`, `aw-sync uninstall-timer`, on Linux
  (systemd), macOS (launchd) and Windows (Task Scheduler), and an exclusive
  lock around `once` and `enroll`.
- **Separate from enrollment, and refuses an unenrolled machine.** A timer
  on an unenrolled box only logs "not enrolled" every interval. The MDM
  script is: install binaries → `enroll` → `install-timer`.
- **Embedded templates are the source of truth; the deploy files stay.**
  Operators and MDM pipelines that push unit files (Jamf shipping a plist
  is common) keep their artifacts. A golden test pins every
  `deploy/aw-sync/*` file to the rendered default so the two cannot drift.

## Commands

### `aw-sync install-timer [--interval 5m] [--state-dir DIR]`

1. Refuses unless `machine.json` exists in the state dir:
   `not enrolled; run aw-sync enroll first`. Nothing is written.
2. `--interval` is a Go duration between 1m and 24h; default 5m, as the
   deploy units have today. Anything else is an error before disk is
   touched.
3. The unit runs **the binary actually running**: `os.Executable`, symlinks
   resolved with `filepath.EvalSymlinks`. It never falls back to a guessed
   path. `--state-dir` is passed through to the unit's `once` only when it
   differs from `sync.StateDir(goos)`.
4. Writes the units and loads them:

| OS | Files | Commands, in order |
| --- | --- | --- |
| linux | `/etc/systemd/system/aw-sync.service`, `/etc/systemd/system/aw-sync.timer` (0644) | `systemctl daemon-reload`; `systemctl enable --now aw-sync.timer` |
| darwin | `/Library/Logs/agent-wrapper/` (0755); `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist` (0644) | `launchctl bootout system/com.agent-wrapper.aw-sync` (a "not loaded" failure is ignored); `launchctl bootstrap system <plist>` |
| windows | task XML in a temp file, removed afterwards | `schtasks /Create /TN agent-wrapper\aw-sync /XML <file> /F` |

5. Idempotent: re-running overwrites and reloads. That is how an operator
   changes the interval or picks up a moved binary.
6. Prints what it installed, where, and suggests `aw-sync status`.

### `aw-sync uninstall-timer`

| OS | Commands and files |
| --- | --- |
| linux | `systemctl disable --now aw-sync.timer`; delete both unit files; `systemctl daemon-reload` |
| darwin | `launchctl bootout system/com.agent-wrapper.aw-sync`; delete the plist |
| windows | `schtasks /Delete /TN agent-wrapper\aw-sync /F` |

With nothing installed it succeeds and prints `no timer installed`. It
never touches enrollment, state or rendered files: removing the timer
stops future syncs, nothing else.

### The cycle lock

`once` and `enroll` take an exclusive, non-blocking lock on
`<stateDir>/aw-sync.lock` before anything else.

- `once` finding it held exits **0** with
  `note: another aw-sync cycle is running; skipped`. A timer tick that
  collides with a manual run is not a failure worth retrying.
- `enroll` finding it held exits 1:
  `another aw-sync is running; retry`. Enrollment is interactive.
- Unix: `syscall.Flock(LOCK_EX|LOCK_NB)`. Windows: `syscall.CreateFile`
  with share mode 0. Both are standard library, so no new dependency, and
  the OS releases either on process exit, so a crashed cycle never leaves
  a stale lock.
- The lock file is 0644 and never deleted; deleting a lock file races a
  concurrent opener.
- `sync.Run` stays lock-free; the command layer owns the lock, so the sync
  tests are unchanged.

## Architecture

### `internal/sync/timer`

One job: turn `(binary, state dir, interval)` into OS units, and install
or remove them.

- `units/` — embedded `text/template` files: `aw-sync.service`,
  `aw-sync.timer`, `com.agent-wrapper.aw-sync.plist`, `aw-sync-task.xml`.
- `Params{Binary, StateDir, Interval}` and `Default(goos) Params`: the
  deploy defaults (`/usr/local/bin/aw-sync`,
  `C:\Program Files\AgentWrapper\aw-sync.exe`, the OS state dir, 5m).
- `Render(goos, Params) ([]Unit, error)` with `Unit{Path, Content, Mode}` —
  pure. Values are escaped for their format: XML escaping in the plist and
  task XML, systemd quoting for paths with spaces in `ExecStart`.
- `Installer{GOOS string; Run Runner; Root string}` with
  `Install(ctx, Params) error` and `Uninstall(ctx) error`.
  - `Runner` is `func(ctx context.Context, name string, args ...string) ([]byte, error)`,
    defaulting to `exec.CommandContext(...).CombinedOutput()`.
  - `Root` prefixes every file path (default `/`), so tests write into a
    temp dir.
  - A failed command's error names the command and carries its output.

### Lock

`internal/sync/lock.go` (`Lock(stateDir) (release func(), err error)`,
`ErrLocked`), with `lock_unix.go` and `lock_windows.go`.

### `cmd/aw-sync`

`install-timer` and `uninstall-timer` subcommands; usage text; a helper
resolving the running binary; `once` and `enroll` wrapped in the lock.

### Deploy files

`deploy/aw-sync/systemd/*` and `launchd/*.plist` are regenerated from
`Render(goos, Default(goos))`. `windows/register-task.ps1` is replaced by
`windows/aw-sync-task.xml`, registered with one `schtasks /Create /XML`
line in the README.

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| install-timer | not enrolled | exit 1, `not enrolled; run aw-sync enroll first`; nothing written |
| install-timer | `--interval` unparseable or outside 1m–24h | exit 1 before touching disk |
| install-timer | not root/admin (write refused or tool refuses) | exit 1 naming the path or command, with a `sudo` / elevated-prompt hint |
| install-timer | units written, then the OS tool fails (e.g. no `systemctl` on a non-systemd distro) | exit 1 naming the command and its output; unit files stay, re-running is safe; README points non-systemd Linux at the deploy files and cron |
| install-timer | unsupported GOOS | exit 1, `install-timer supports linux, darwin and windows` |
| install-timer | running binary path cannot be resolved | exit 1; never a guessed path |
| uninstall-timer | nothing installed | exit 0, `no timer installed` |
| uninstall-timer | OS tool fails part-way | exit 1 naming the command; files it could remove are removed; re-running is safe |
| once | lock held | exit 0, note, cycle skipped |
| enroll | lock held | exit 1, `another aw-sync is running; retry` |
| once / enroll | state dir does not exist | unchanged from today: `once` reports not enrolled; `enroll` creates the dir, then locks |
| once / enroll | lock file cannot be opened for another reason | exit 1 with the error |

## Testing

- `timer.Render`: per OS, default and custom interval and state dir;
  paths with spaces (`Application Support`, `Program Files`) escaped
  correctly for each format; interval rendered as `OnUnitActiveSec=`,
  `StartInterval` seconds and `PT…` duration respectively.
- `timer.Installer` against a temp `Root` and a recording `Runner`: the
  exact command sequence for install, re-install and uninstall on each OS;
  file modes 0644; a failing command's name and output in the error;
  uninstall with nothing installed succeeds; darwin's ignored "not loaded"
  bootout.
- Golden: every `deploy/aw-sync/*` unit equals `Render(goos,
  Default(goos))`, byte for byte. This replaces
  `TestEveryTimerUnitRunsOnce`.
- Lock: a second `Lock` on the same dir returns `ErrLocked`; after
  `release` it succeeds. On Unix, a helper process holds the lock while
  the built `aw-sync once` reports skipped and exits 0.
- cmd e2e: `install-timer` on an unenrolled state dir refuses and writes
  nothing; `--interval 30s` refuses; unknown flags refuse. A real install
  is not e2e-tested — it needs root and a live init system; the
  `Installer` tests cover the sequence.

## Documentation

- `deploy/aw-sync/README.md` step 3 becomes `sudo aw-sync install-timer`
  (elevated prompt on Windows), with the per-OS file steps kept as
  "shipping the units yourself". Adds `uninstall-timer` and the overlap
  note under Verify.
- The bundle-sync spec's "`install-timer` is deferred" line points here.

## Refinements from planning

Settled while writing the implementation plan; they refine, not change,
the decisions above.

- `--interval` must also be a whole number of minutes: Task Scheduler
  repeats in minutes, and one rule for every OS beats a Windows-only
  surprise.
- The systemd timer renders the interval in seconds
  (`OnUnitActiveSec=300s`) and its description no longer names a period,
  so one template serves every interval.
- launchd: `launchctl print system/com.agent-wrapper.aw-sync` decides
  whether the job is loaded. Install boots it out only when loaded, and a
  bootout failure is then reported, not ignored.
- "Nothing installed" for uninstall means: Linux, neither unit file
  exists; macOS, no plist and the job is not loaded; Windows,
  `schtasks /Query /TN agent-wrapper\aw-sync` fails.
- The task XML is UTF-8 on disk under `deploy/`; install-timer stages it
  for `schtasks` as UTF-16LE with a BOM and a UTF-16 declaration, the
  encoding Task Scheduler itself exports. Nobody has run this on a real
  Windows host yet; the first Windows install is its test.
- Unit files are always 0644, so a rendered unit is `{Name, Path, Content}`
  (`Name` is its file name under `deploy/aw-sync/<dir>/`, `Path` its install
  location, empty on Windows).
- `once` with no state directory skips the lock (there is nothing to
  protect) and reports "not enrolled" exactly as today.

## Out of scope, deliberately

- User-level (non-root) timers.
- cron, OpenRC, runit and other init systems (the deploy files remain the
  path there).
- Jitter tuning beyond the existing `RandomizedDelaySec=60`.
- Uninstall removing enrollment or rendered files.
