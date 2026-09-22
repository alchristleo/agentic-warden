// Command aw launches a coding agent with the organization's configuration
// applied at launch time.
//
// It is the reference consumer of the wrapper library: a binary an
// organization rebuilds under its own name, with its own policy source baked
// in, so developers type `acme claude` and get the org's configuration without
// configuring anything themselves.
//
// Enforcement does not depend on this binary. Managed settings reach the agent
// through its own managed-settings tier, which applies whether or not the
// developer goes through the wrapper. What the wrapper adds is launch-time
// environment injection, and doctor, which shows exactly what would run.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/agent/codex"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/repo"
	"github.com/acme/agent-wrapper/internal/sync"
)

const usage = `aw launches a coding agent with your organization's configuration applied.

Usage:
  aw [flags] <agent> [agent args...]   run an agent
  aw [flags] agents                    list the agents this binary can launch
  aw [flags] doctor [--json]           report what would run, without running it
  aw help

Flags:
  --policy PATH   policy document to apply (default: $AW_POLICY)

doctor also reads aw-sync's state directory ($AW_SYNC_STATE_DIR overrides
the OS default) and reports the last sync, its version, errors and drift.

Everything after the agent name is passed to the agent untouched.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "aw: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	opts, rest, err := parseFlags(argv)
	if err != nil {
		return err
	}

	registry := &agent.Registry{}
	for _, a := range []agent.Adapter{claude.New(), codex.New(), gemini.New()} {
		if err := registry.Register(a); err != nil {
			return err
		}
	}

	if len(rest) == 0 {
		fmt.Print(usage)
		return nil
	}

	command, args := rest[0], rest[1:]
	switch command {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "agents":
		for _, name := range registry.Names() {
			fmt.Println(name)
		}
		return nil
	case "doctor":
		return doctor(registry, opts, args)
	default:
		return launch(registry, opts, command, args)
	}
}

type options struct {
	policyPath string
}

// parseFlags reads the wrapper's own flags, stopping at the first argument
// that is not one. Everything from there on belongs to the agent, so a flag
// the wrapper happens to share a name with still reaches it untouched.
func parseFlags(argv []string) (options, []string, error) {
	opts := options{policyPath: os.Getenv("AW_POLICY")}
	i := 0
	for ; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "--") {
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if name != "policy" {
			break
		}
		if !hasValue {
			if i+1 >= len(argv) {
				return opts, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = argv[i]
		}
		opts.policyPath = value
	}
	return opts, argv[i:], nil
}

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

func settingsFor(doc *policy.Document, agentName string) agent.Settings {
	config := doc.Agent(agentName)
	return agent.Settings{
		Managed:  config.Managed,
		Env:      config.Env,
		ForceEnv: config.ForceEnv,
		Launch:   config.Launch,
	}
}

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

// report is the machine-readable shape of doctor's output.
type report struct {
	Wrapper string `json:"wrapper"`
	Policy  string `json:"policy"`
	// PolicyNote says why there is no policy, when there is none.
	PolicyNote string `json:"policyNote,omitempty"`
	// SyncStateDir is where aw-sync's state was looked for.
	SyncStateDir string `json:"syncStateDir"`
	// Sync is aw-sync's own status, read as the developer from its state
	// directory: enrollment, last cycle, drift. Nil when that directory
	// could not be read, with SyncError saying why.
	Sync *sync.Report `json:"sync,omitempty"`
	// SyncError is why aw-sync's state could not be read; empty when Sync
	// is populated.
	SyncError string        `json:"syncError,omitempty"`
	Agents    []agentStatus `json:"agents"`
}

type agentStatus struct {
	Name   string        `json:"name"`
	Binary string        `json:"binary,omitempty"`
	Error  string        `json:"error,omitempty"`
	Launch *agent.Launch `json:"launch,omitempty"`
	// Env lists only the variables this launch adds or changes, which is the
	// part worth reading.
	Env []string `json:"injected_env,omitempty"`
	// Findings is what the adapter noticed about how the agent is governed
	// on this machine, for adapters that can tell.
	Findings []agent.Finding `json:"findings,omitempty"`
}

func doctor(registry *agent.Registry, opts options, args []string) error {
	asJSON := len(args) > 0 && args[0] == "--json"

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
	out.Sync, out.SyncError = syncSection(stateDir)
	for _, name := range registry.Names() {
		status := agentStatus{Name: name}
		adapter, err := registry.Lookup(name)
		if err != nil {
			status.Error = err.Error()
			out.Agents = append(out.Agents, status)
			continue
		}
		if binary, err := adapter.Locate(nil); err != nil {
			status.Error = err.Error()
		} else {
			status.Binary = binary
		}
		if inspector, ok := adapter.(agent.Inspector); ok {
			status.Findings = inspector.Inspect(nil)
		}
		launch, err := agent.Prepare(context.Background(), registry, agent.Options{
			Agent:    name,
			Settings: settingsFor(doc, name),
		})
		if err != nil {
			if status.Error == "" {
				status.Error = err.Error()
			}
			out.Agents = append(out.Agents, status)
			continue
		}
		status.Launch = launch
		if note := compiledFor(src); note != "" {
			launch.Notes = append(launch.Notes, note)
		}
		status.Env = addedEnv(os.Environ(), launch.Env)
		// The full environment is noise in a report; the diff above is the
		// part a reader needs.
		launch.Env = nil
		out.Agents = append(out.Agents, status)
	}

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(out)
	}
	printReport(out)
	return nil
}

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
	if d < 0 {
		// Clock skew between the machine that wrote at and this one can
		// put at slightly in the future; "-5s ago" is not a useful report.
		d = 0
	}
	if d >= 24*time.Hour {
		days := d / (24 * time.Hour)
		hours := (d % (24 * time.Hour)) / time.Hour
		return fmt.Sprintf("%dd%dh ago", days, hours)
	}
	return d.String() + " ago"
}

func printReport(r report) {
	fmt.Printf("wrapper: %s\n", r.Wrapper)
	fmt.Printf("policy:  %s\n", r.Policy)
	if r.PolicyNote != "" {
		fmt.Printf("  %s\n", r.PolicyNote)
	}
	fmt.Printf("sync:    %s\n", r.SyncStateDir)
	switch {
	case r.SyncError != "":
		fmt.Printf("  error:   %s\n", r.SyncError)
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
	for _, a := range r.Agents {
		fmt.Printf("\nagent %s\n", a.Name)
		if a.Binary != "" {
			fmt.Printf("  binary: %s\n", a.Binary)
		}
		for _, f := range a.Findings {
			fmt.Printf("  %-5s   %s\n", f.Level+":", f.Message)
		}
		if a.Error != "" {
			fmt.Printf("  error:  %s\n", a.Error)
			continue
		}
		if a.Launch != nil {
			fmt.Printf("  args:   %s\n", strings.Join(a.Launch.Args, " "))
			for _, file := range a.Launch.Files {
				fmt.Printf("  file:   %s\n", file)
			}
			for _, entry := range a.Env {
				fmt.Printf("  env:    %s\n", entry)
			}
			for _, note := range a.Launch.Notes {
				fmt.Printf("  note:   %s\n", note)
			}
		}
	}
}

// addedEnv returns the entries in launch that base does not already have
// verbatim: exactly the variables the wrapper injected or overrode.
func addedEnv(base, launch []string) []string {
	existing := make(map[string]bool, len(base))
	for _, entry := range base {
		existing[entry] = true
	}
	out := make([]string, 0)
	for _, entry := range launch {
		if !existing[entry] {
			out = append(out, entry)
		}
	}
	return out
}

// orNone makes an empty field visible in the plain report.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
