// Package claude adapts Claude Code to the wrapper.
//
// Everything is applied at launch time and nothing on disk is modified. The
// adapter reads the settings the developer and their project already have,
// rebuilds the merge itself with the organization's managed settings on top,
// writes the result to a cache file, and points the agent at it with
// --settings. Rebuilding the merge here rather than relying on the agent's own
// precedence keeps the documented layer order true even if the agent's
// internal ordering changes.
//
// This is the wrapper path, used for launch-time environment injection and for
// diagnostics. Enforcement rides on the agent's managed settings tier via a
// policy helper, which applies whether or not the developer uses the wrapper.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/cache"
	"github.com/acme/agent-wrapper/internal/merge"
)

// Name is the subcommand a developer types.
const Name = "claude"

// pruneAfter is how long a generated settings file survives unused.
const pruneAfter = 30 * 24 * time.Hour

// unionPaths are the settings whose lists accumulate across layers instead of
// being replaced, so an org allowlist and a personal allowlist coexist.
var unionPaths = []string{
	"permissions.allow",
	"permissions.deny",
	"permissions.ask",
	"permissions.additionalDirectories",
}

// Adapter launches Claude Code. The zero value is not usable; call New. The
// exported fields exist so tests can control every input.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// UserSettingsPath overrides ~/.claude/settings.json.
	UserSettingsPath string
	// ProjectDir overrides the working directory used to find .claude/.
	ProjectDir string
	// CacheDir overrides where generated settings are written.
	CacheDir string
	// SystemDir overrides the directory Claude Code reads managed settings
	// from on this OS.
	SystemDir string
	// ConfigDir overrides ~/.claude, or $CLAUDE_CONFIG_DIR.
	ConfigDir string
}

// New returns an adapter with the defaults a developer's machine implies.
func New() *Adapter { return &Adapter{} }

// Name reports the subcommand this adapter handles.
func (a *Adapter) Name() string { return Name }

// Locate resolves the real agent, never the wrapper.
func (a *Adapter) Locate(env []string) (string, error) {
	return agent.ResolveBinary(a.binaryName(), env)
}

func (a *Adapter) binaryName() string {
	if a.Binary != "" {
		return a.Binary
	}
	return Name
}

// Build computes the launch. It never executes the agent.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}

	launch := &agent.Launch{Agent: Name, Binary: binary}

	settingsPath, err := a.mergeSettings(o.Settings.Managed, launch)
	if err != nil {
		return nil, err
	}
	launch.Args = append(launch.Args, "--settings", settingsPath)
	launch.Files = append(launch.Files, settingsPath)

	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch.Env = env
	launch.Notes = append(launch.Notes, notes...)

	// The developer's arguments go last so an explicit flag outranks anything
	// injected above.
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}

// layer is one settings document on the way into the merge.
type layer struct {
	label string
	value map[string]any
}

// mergeSettings combines every settings layer and writes the result, returning
// the path to pass to the agent.
func (a *Adapter) mergeSettings(managed map[string]any, launch *agent.Launch) (string, error) {
	layers := make([]layer, 0, 4)
	for _, candidate := range a.settingsFiles() {
		value, note, err := readSettings(candidate.label, candidate.path)
		if note != "" {
			launch.Notes = append(launch.Notes, note)
		}
		if err != nil || value == nil {
			continue
		}
		layers = append(layers, layer{label: candidate.label, value: value})
	}
	if managed != nil {
		layers = append(layers, layer{label: "managed (org, locked)", value: managed})
	}

	rules := merge.Rules{UnionArrays: unionPaths}
	var merged any = map[string]any{}
	order := make([]string, 0, len(layers))
	for _, l := range layers {
		merged = merge.JSON(merged, l.value, rules)
		order = append(order, l.label)
	}

	if len(order) > 0 {
		launch.Notes = append(launch.Notes, "settings layers (low to high): "+joinArrow(order))
	} else {
		launch.Notes = append(launch.Notes, "settings layers: none, using the agent's own defaults")
	}

	encoded, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return "", fmt.Errorf("claude: encoding merged settings: %w", err)
	}

	dir, err := a.cacheDir()
	if err != nil {
		return "", err
	}
	// Pruning before the write keeps the directory from growing without bound
	// and never touches the file this launch is about to publish.
	if err := cache.Prune(dir, pruneAfter); err != nil {
		launch.Notes = append(launch.Notes, "could not prune old settings files: "+err.Error())
	}
	path, err := cache.Write(dir, "settings", ".json", encoded)
	if err != nil {
		return "", err
	}
	launch.Notes = append(launch.Notes, "merged settings written to "+path)
	return path, nil
}

type settingsCandidate struct {
	label string
	path  string
}

// settingsFiles lists the layers below the managed one, lowest first, matching
// the agent's own precedence.
func (a *Adapter) settingsFiles() []settingsCandidate {
	var out []settingsCandidate
	if path := a.userSettingsPath(); path != "" {
		out = append(out, settingsCandidate{label: "user " + path, path: path})
	}
	if dir := a.projectDir(); dir != "" {
		out = append(out,
			settingsCandidate{label: "project " + filepath.Join(dir, ".claude", "settings.json"),
				path: filepath.Join(dir, ".claude", "settings.json")},
			settingsCandidate{label: "project local " + filepath.Join(dir, ".claude", "settings.local.json"),
				path: filepath.Join(dir, ".claude", "settings.local.json")},
		)
	}
	return out
}

func (a *Adapter) userSettingsPath() string {
	if a.UserSettingsPath != "" {
		return a.UserSettingsPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func (a *Adapter) projectDir() string {
	if a.ProjectDir != "" {
		return a.ProjectDir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func (a *Adapter) cacheDir() (string, error) {
	if a.CacheDir != "" {
		return a.CacheDir, nil
	}
	return cache.Dir(Name)
}

// readSettings loads one settings file. A missing file is silent. A file that
// exists but cannot be parsed produces a note and is skipped: a developer with
// a half-edited settings file should still get a working agent, and the note
// tells them why their settings are not taking effect.
func readSettings(label, path string) (map[string]any, string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, fmt.Sprintf("%s could not be read (%v); skipped", label, err), err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Sprintf("%s could not be read (%v); skipped", label, err), err
	}
	return value, "", nil
}

func joinArrow(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " < "
		}
		out += part
	}
	return out
}
