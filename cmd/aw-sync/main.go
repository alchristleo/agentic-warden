// Command aw-sync keeps a machine's agent configuration files in step with
// the control plane. It runs as root from a timer, not as a daemon: `once`
// is one cycle, restart-safe, which is what MDM tooling expects.
//
//	aw-sync enroll --server URL --token T    exchange an enrollment token for this machine's credential
//	aw-sync once                             fetch the bundle and render every enrolled agent's files
//	aw-sync status                           last sync, bundle version, per-file drift, last error
//	aw-sync install-timer [--interval 5m]      run `once` periodically (systemd, launchd or Task Scheduler)
//	aw-sync uninstall-timer                    stop and remove that timer; enrollment and files stay
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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/agent/codex"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
	"github.com/acme/agent-wrapper/internal/sync"
	"github.com/acme/agent-wrapper/internal/sync/timer"
)

const usage = `aw-sync keeps this machine's agent configuration in step with the control plane.

Usage:
  aw-sync enroll --server URL [--token T] [--name HOST] [--agents claude,codex,gemini] [--force] [--state-dir DIR]
  aw-sync once [--state-dir DIR] [--root agent=DIR ...]
  aw-sync status [--json] [--state-dir DIR]
  aw-sync install-timer [--interval 5m] [--state-dir DIR]
  aw-sync uninstall-timer
  aw-sync help

Environment:
  AW_SYNC_TOKEN   the enrollment token, so it need not appear on the command line

--state-dir defaults to the OS state directory (/var/lib/agent-wrapper on Linux).
--root overrides where one agent's files are written and exists for testing.
once exits 1 when the cycle fails; the files on disk are left as they were.
install-timer and uninstall-timer need root (an elevated prompt on Windows).
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
		return helpOr(enroll(argv[1:], stdout), stdout)
	case "once":
		return helpOr(once(argv[1:], stdout), stdout)
	case "status":
		return helpOr(status(argv[1:], stdout), stdout)
	case "install-timer":
		return helpOr(installTimer(argv[1:], stdout), stdout)
	case "uninstall-timer":
		return helpOr(uninstallTimer(argv[1:], stdout), stdout)
	default:
		return fmt.Errorf("unknown command %q; run `aw-sync help`", argv[0])
	}
}

// helpOr turns a subcommand's flag.ErrHelp (from -h/--help) into the top
// level usage printed to stdout and a clean exit, the same as `aw-sync
// help`; any other error, including nil, passes through unchanged.
func helpOr(err error, stdout io.Writer) error {
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return nil
	}
	return err
}

// newRegistry holds the adapters this binary can sync. Only those that
// implement agent.Renderer are offered to enroll.
func newRegistry() (*agent.Registry, error) {
	reg := &agent.Registry{}
	for _, a := range []agent.Adapter{claude.New(), codex.New(), gemini.New()} {
		if err := reg.Register(a); err != nil {
			return nil, err
		}
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
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("--root %s: %w", value, err)
	}
	r[name] = abs
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
			if selected[i] == "" {
				return fmt.Errorf("--agents has an empty entry")
			}
			adapter, err := reg.Lookup(selected[i])
			if err != nil {
				return fmt.Errorf("agent %q: %w", selected[i], err)
			}
			if _, ok := adapter.(agent.Renderer); !ok {
				return fmt.Errorf("agent %q cannot be synced: no renderer", selected[i])
			}
		}
	}
	if *name == "" {
		if host, err := os.Hostname(); err == nil {
			*name = host
		}
	}

	// Created before the token is spent, so a state directory this process
	// cannot write to fails loudly here rather than after the single-use
	// token is already consumed.
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", *stateDir, err)
	}

	// Enrollment rewrites machine.json, which a running cycle may be about
	// to rewrite too during a key rollover; it waits for nobody, so a held
	// lock is an error to retry, and it comes before the token is spent.
	// The lock also covers the already-enrolled check below: without it,
	// two concurrent enrolls without --force could both read no conflict
	// and both write, the second silently overwriting the first's
	// enrollment. Under the lock, a second concurrent enroll sees the
	// first one's machine.json and is refused normally.
	release, err := sync.Lock(*stateDir)
	if errors.Is(err, sync.ErrLocked) {
		return errors.New("another aw-sync is running; retry")
	}
	if err != nil {
		return err
	}
	defer release()

	existing, loadErr := sync.LoadMachine(*stateDir)
	switch {
	case loadErr == nil && !*force:
		return fmt.Errorf("already enrolled as machine %s against %s; pass --force to re-enroll", existing.MachineID, existing.Server)
	case loadErr != nil && !errors.Is(loadErr, sync.ErrNotEnrolled) && !*force:
		return fmt.Errorf("an enrollment exists in %s but cannot be read (%s); pass --force to replace it", *stateDir, loadErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &sync.Client{Server: *server}
	e, err := client.Enroll(ctx, *token, *name, runtime.GOOS)
	if err != nil {
		return err
	}
	machine := sync.Machine{Server: *server, MachineID: e.MachineID, Credential: e.Credential, Agents: selected, PublicKey: e.PublicKey, KeyID: e.KeyID}
	if err := sync.SaveMachine(*stateDir, machine); err != nil {
		return fmt.Errorf("%w; the enrollment token was consumed; mint a new one", err)
	}
	// state.json carries the enrollment's non-secret facts too, so `status`
	// can report them without reading machine.json (0600) even before the
	// first `once` has run.
	initial := sync.State{Server: *server, MachineID: e.MachineID, Agents: selected}
	if err := sync.SaveState(*stateDir, initial); err != nil {
		return fmt.Errorf("%w; the enrollment token was consumed; mint a new one", err)
	}
	fmt.Fprintf(stdout, "enrolled machine %s for %s; syncing %s into %s\n", e.MachineID, e.User, strings.Join(selected, ", "), *stateDir)
	return nil
}

func once(argv []string, stdout io.Writer) error {
	flags, stateDir := newFlagSet("once")
	roots := rootFlags{}
	flags.Var(roots, "root", "agent=DIR override for one agent's system directory")
	if err := flags.Parse(argv); err != nil {
		return err
	}

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
