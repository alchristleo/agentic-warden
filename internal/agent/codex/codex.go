// Package codex integrates OpenAI Codex. Its enforced configuration is a
// machine-wide requirements.toml that Codex composes under any cloud or MDM
// layer an organization also runs; this package renders that file for
// aw-sync. Launching Codex through aw also turns the policy's per-launch
// document into sorted -c overrides on the command line; requirements.toml
// otherwise applies to bare `codex` as much as to `aw codex`.
package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/merge"
)

// Name is the subcommand a developer types and the agent key in a policy.
const Name = "codex"

// SystemDir is where Codex reads requirements.toml on goos: the enforced
// tier that outranks every user file. Windows keeps it under ProgramData,
// which an installation can relocate; the environment says where.
func SystemDir(goos string) string {
	switch goos {
	case "windows":
		return programData() + `\OpenAI\Codex`
	default:
		return "/etc/codex"
	}
}

// programData is Windows' machine-wide data directory.
func programData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}

// Adapter launches Codex and renders its requirements file. The exported
// field exists so tests can control the binary.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// SystemDir overrides where Inspect looks for requirements.toml; empty
	// means SystemDir(runtime.GOOS). Tests aim it at a temporary directory.
	SystemDir string
}

func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	return SystemDir(runtime.GOOS)
}

// New returns an adapter with the defaults a developer's machine implies.
func New() *Adapter { return &Adapter{} }

// Name reports the subcommand this adapter handles.
func (a *Adapter) Name() string { return Name }

// Locate resolves the real Codex binary, never the wrapper.
func (a *Adapter) Locate(env []string) (string, error) {
	name := a.Binary
	if name == "" {
		name = Name
	}
	return agent.ResolveBinary(name, env)
}

// Build computes the launch. Codex takes no managed settings file on the
// command line, so the policy's managed document is not applied here:
// aw-sync has already written it to requirements.toml, which Codex reads
// on its own. The per-launch document does have a command-line channel,
// -c, so it is turned into sorted overrides; the environment is also
// merged, so an organization's variables still reach the process.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}
	launch := &agent.Launch{Agent: Name, Binary: binary}

	// The launch document is the per-repository layer; requirements.toml,
	// which aw-sync wrote, bounds what any override can do. Overrides go
	// first so the developer's own -c, appended below, wins.
	pairs, err := overrides(o.Settings.Launch)
	if err != nil {
		return nil, err
	}
	for _, pair := range pairs {
		launch.Args = append(launch.Args, "-c", pair)
		launch.Notes = append(launch.Notes, "codex: -c "+pair+" from launch")
	}

	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch.Env = env
	launch.Notes = append(launch.Notes, notes...)
	launch.Notes = append(launch.Notes, "codex: managed settings are enforced by the system requirements.toml that aw-sync writes, not per launch")
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}

// Inspect reports whether Codex is governed on this machine: the
// requirements file aw-sync owns is present and parseable. It runs
// nothing. env is unused; Codex has no environment variable that moves
// the file.
func (a *Adapter) Inspect(env []string) []agent.Finding {
	path := filepath.Join(a.systemDir(), RequirementsFile)
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return []agent.Finding{{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: Codex is not governed on this machine until aw-sync has run", path)}}
	case err != nil:
		return []agent.Finding{{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", path, err)}}
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return []agent.Finding{{Level: agent.Error,
			Message: fmt.Sprintf("%s is not valid TOML, so Codex ignores it: %v", path, err)}}
	}
	if version := agent.RevisionFromHeader(raw); version != "" {
		return []agent.Finding{{Level: agent.OK, Message: fmt.Sprintf("%s (policy revision %s)", path, version)}}
	}
	return []agent.Finding{{Level: agent.OK, Message: fmt.Sprintf("%s (not written by aw-sync)", path)}}
}
