// Package agent turns an organization's configuration into a concrete launch
// of a coding agent.
//
// The split that matters: Build computes everything and runs nothing. A Launch
// carries the binary, the exact argv, the full environment, the files that
// were generated, and a note for every decision that produced them. Diagnostic
// tooling prints that; Run executes it. Nothing about a launch is discovered
// only by running it.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/acme/agent-wrapper/internal/policy"
)

// ErrNoAdapter is returned by Registry.Lookup for an unregistered name.
var ErrNoAdapter = errors.New("no adapter registered")

// Settings is the organization configuration an adapter applies to a launch.
type Settings struct {
	// Managed is the settings document to enforce, in the agent's own schema.
	Managed map[string]any
	// Env holds environment defaults to inject.
	Env map[string]string
	// ForceEnv makes Env replace values the developer already exports. The
	// zero value leaves the developer's environment winning.
	ForceEnv bool
}

// BuildOptions carries everything an adapter needs to compute a launch.
type BuildOptions struct {
	// Args are the developer's arguments. Adapters append them after any
	// injected flags, so an explicit flag still wins.
	Args []string
	// Env is the base environment; nil means os.Environ.
	Env []string
	// Settings is the organization configuration to apply.
	Settings Settings
}

// Launch is a computed launch plus the decision trail that produced it.
type Launch struct {
	Agent  string   `json:"agent"`
	Binary string   `json:"binary"`
	Args   []string `json:"args"`
	Env    []string `json:"env,omitempty"`
	// Files lists artifacts written for this launch, for debugging.
	Files []string `json:"files,omitempty"`
	// Notes records every decision, in the order it was made.
	Notes []string `json:"notes,omitempty"`
}

// Adapter integrates one coding agent. Implementations must be safe for
// concurrent use.
type Adapter interface {
	// Name is the subcommand a developer types, e.g. "claude".
	Name() string
	// Locate returns the path of the real agent binary, resolved against env
	// (nil means os.Environ) and never the wrapper itself.
	Locate(env []string) (string, error)
	// Build computes the launch. It must not execute the agent.
	Build(ctx context.Context, o BuildOptions) (*Launch, error)
}

// Registry holds the adapters a binary was compiled with. The zero value is
// ready to use.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// Register adds a to the registry, rejecting a duplicate name. Adapters are
// static configuration, so a duplicate is a programming error, but returning
// it as an error keeps the decision with the caller rather than panicking
// inside a library.
func (r *Registry) Register(a Adapter) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adapters == nil {
		r.adapters = make(map[string]Adapter)
	}
	name := a.Name()
	if _, duplicate := r.adapters[name]; duplicate {
		return fmt.Errorf("agent: adapter %q registered twice", name)
	}
	r.adapters[name] = a
	return nil
}

// Lookup returns the adapter registered under name.
func (r *Registry) Lookup(name string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("agent: %w for %q (registered: %s)",
			ErrNoAdapter, name, strings.Join(r.namesLocked(), ", "))
	}
	return a, nil
}

// Names lists the registered adapter names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namesLocked()
}

func (r *Registry) namesLocked() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Level grades a Finding.
type Level string

const (
	// OK records something that is as it should be.
	OK Level = "ok"
	// Warn records something that silently weakens enforcement.
	Warn Level = "warn"
	// Error records something that will break launches.
	Error Level = "error"
)

// Finding is one thing an Inspector noticed about the machine.
type Finding struct {
	Level   Level  `json:"level"`
	Message string `json:"message"`
}

// Inspector is implemented by adapters that can check how their agent is
// governed on this machine, beyond what a computed launch shows: whether the
// enforcement path is wired up, and whether something shadows it.
type Inspector interface {
	// Inspect examines the machine as env (nil means os.Environ) describes
	// it and reports what it found. It runs nothing.
	Inspect(env []string) []Finding
}

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
