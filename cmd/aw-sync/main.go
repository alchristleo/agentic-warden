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
	"path/filepath"
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
		return helpOr(enroll(argv[1:], stdout), stdout)
	case "once":
		return helpOr(once(argv[1:], stdout), stdout)
	case "status":
		return helpOr(status(argv[1:], stdout), stdout)
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
	existing, loadErr := sync.LoadMachine(*stateDir)
	switch {
	case loadErr == nil && !*force:
		return fmt.Errorf("already enrolled as machine %s against %s; pass --force to re-enroll", existing.MachineID, existing.Server)
	case loadErr != nil && !errors.Is(loadErr, sync.ErrNotEnrolled) && !*force:
		return fmt.Errorf("an enrollment exists in %s but cannot be read (%s); pass --force to replace it", *stateDir, loadErr)
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &sync.Client{Server: *server}
	e, err := client.Enroll(ctx, *token, *name, runtime.GOOS)
	if err != nil {
		return err
	}
	machine := sync.Machine{Server: *server, MachineID: e.MachineID, Credential: e.Credential, Agents: selected}
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
