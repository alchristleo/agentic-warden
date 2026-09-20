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
	"fmt"
	"os"
	"strings"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/policy"
)

const usage = `aw launches a coding agent with your organization's configuration applied.

Usage:
  aw [flags] <agent> [agent args...]   run an agent
  aw [flags] agents                    list the agents this binary can launch
  aw [flags] doctor [--json]           report what would run, without running it
  aw help

Flags:
  --policy PATH   policy document to apply (default: $AW_POLICY)

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
	if err := registry.Register(claude.New()); err != nil {
		return err
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

// loadPolicy reads the configured policy. A policy that was named but cannot
// be read is an error: silently launching without the organization's
// configuration would be worse than refusing.
func loadPolicy(opts options) (*policy.Document, error) {
	if opts.policyPath == "" {
		return nil, nil
	}
	return policy.Load(opts.policyPath)
}

func settingsFor(doc *policy.Document, agentName string) agent.Settings {
	config := doc.Agent(agentName)
	return agent.Settings{
		Managed:  config.Managed,
		Env:      config.Env,
		ForceEnv: config.ForceEnv,
	}
}

func launch(registry *agent.Registry, opts options, agentName string, args []string) error {
	doc, err := loadPolicy(opts)
	if err != nil {
		return err
	}
	// Lookup first so an unknown agent reports the agents this binary knows,
	// rather than whatever the policy happens to be missing.
	if _, err := registry.Lookup(agentName); err != nil {
		return err
	}
	return agent.Run(context.Background(), registry, agent.Options{
		Agent:    agentName,
		Args:     args,
		Settings: settingsFor(doc, agentName),
	})
}

// report is the machine-readable shape of doctor's output.
type report struct {
	Wrapper string        `json:"wrapper"`
	Policy  string        `json:"policy,omitempty"`
	Agents  []agentStatus `json:"agents"`
}

type agentStatus struct {
	Name   string        `json:"name"`
	Binary string        `json:"binary,omitempty"`
	Error  string        `json:"error,omitempty"`
	Launch *agent.Launch `json:"launch,omitempty"`
	// Env lists only the variables this launch adds or changes, which is the
	// part worth reading.
	Env []string `json:"injected_env,omitempty"`
}

func doctor(registry *agent.Registry, opts options, args []string) error {
	asJSON := len(args) > 0 && args[0] == "--json"

	doc, err := loadPolicy(opts)
	if err != nil {
		return err
	}

	out := report{Wrapper: "aw", Policy: opts.policyPath}
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

func printReport(r report) {
	fmt.Printf("wrapper: %s\n", r.Wrapper)
	if r.Policy != "" {
		fmt.Printf("policy:  %s\n", r.Policy)
	}
	for _, a := range r.Agents {
		fmt.Printf("\nagent %s\n", a.Name)
		if a.Binary != "" {
			fmt.Printf("  binary: %s\n", a.Binary)
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
