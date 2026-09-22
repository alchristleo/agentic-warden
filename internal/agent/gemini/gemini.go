// Package gemini integrates Google's Gemini CLI. Its enforced configuration
// is two root-owned files Gemini reads on its own: the system settings.json,
// which overrides every user and project setting, and an admin policy TOML
// file, which outranks every user policy; this package renders both for
// aw-sync. Launching Gemini through aw is pass-through for now: the system
// settings path can be moved with GEMINI_CLI_SYSTEM_SETTINGS_PATH, and
// pinning it per launch is the later launch-time milestone. Until then the
// admin policy directory, which has no such override, is where rules that
// must hold belong.
package gemini

import (
	"context"
	"os"
	"runtime"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/merge"
)

// Name is the subcommand a developer types and the agent key in a policy.
const Name = "gemini"

// SystemDir is where Gemini reads its system settings.json and, under
// policies/, its admin policies on goos. Windows keeps it under
// ProgramData, which an installation can relocate.
func SystemDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/GeminiCli"
	case "windows":
		return programData() + `\gemini-cli`
	default:
		return "/etc/gemini-cli"
	}
}

// programData is Windows' machine-wide data directory.
func programData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}

// Adapter launches Gemini and renders its system files. The exported field
// exists so tests can control the binary.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// SystemDir overrides where Inspect looks for settings.json and the policies
	// directory; empty means SystemDir(runtime.GOOS). Tests aim it at a
	// temporary directory.
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

// Locate resolves the real Gemini binary, never the wrapper.
func (a *Adapter) Locate(env []string) (string, error) {
	name := a.Binary
	if name == "" {
		name = Name
	}
	return agent.ResolveBinary(name, env)
}

// Build computes the launch. Gemini takes no settings file on the command
// line, so the policy's managed document is not applied here: aw-sync has
// already written it to the system settings and admin policy files, which
// Gemini reads on its own. Only the environment is merged, so an
// organization's variables still reach the process.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}
	launch := &agent.Launch{Agent: Name, Binary: binary}

	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch.Env = env
	launch.Notes = append(launch.Notes, notes...)
	launch.Notes = append(launch.Notes, "gemini: managed settings are enforced by the system settings.json and admin policies that aw-sync writes, not per launch")
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}
