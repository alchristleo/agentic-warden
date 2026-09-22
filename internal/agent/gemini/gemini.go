// Package gemini integrates Google's Gemini CLI. Its enforced configuration
// is two root-owned files Gemini reads on its own: the system settings.json,
// which overrides every user and project setting, and an admin policy TOML
// file, which outranks every user policy; this package renders both for
// aw-sync. Launching Gemini through aw takes this further: Gemini has no
// settings flag on its command line, so Build writes the session's compiled
// settings (and any policy rules) to the aw cache and pins
// GEMINI_CLI_SYSTEM_SETTINGS_PATH at it, which is what Gemini's own
// enterprise guidance recommends for scoping the system tier per launch.
package gemini

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

	toml "github.com/pelletier/go-toml/v2"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
	"github.com/acme/agent-wrapper/internal/cache"
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

// SystemSettingsEnv is the variable Gemini reads its system settings path
// from. A developer can export it to move the system tier; the wrapper
// exports it to point the system tier at the settings compiled for this
// session, which is what Gemini's own enterprise guidance recommends.
const SystemSettingsEnv = "GEMINI_CLI_SYSTEM_SETTINGS_PATH"

// launchHeader is the first line of a policy file written for one session,
// distinct from the machine-wide file's header so a reader of the cache
// knows which is which.
const launchHeader = "# Written by aw for one session from policy revision %s. Do not edit.\n\n"

// pruneAfter is how long a generated file survives unused.
const pruneAfter = 30 * 24 * time.Hour

// Adapter launches Gemini and renders its system files. The exported fields
// exist so tests can control the binary and the directories.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// SystemDir overrides where Inspect looks for settings.json and the
	// policies directory; empty means SystemDir(runtime.GOOS).
	SystemDir string
	// CacheDir overrides where Build writes the session's settings; empty
	// means the user cache directory for this agent.
	CacheDir string
}

func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	return SystemDir(runtime.GOOS)
}

func (a *Adapter) cacheDir() (string, error) {
	if a.CacheDir != "" {
		return a.CacheDir, nil
	}
	return cache.Dir(Name)
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

// Build computes the launch. Gemini takes no settings flag, so the compiled
// managed document is written to the cache as a system settings file, with
// any policy rules beside it and named in policyPaths, and the system
// settings variable is pinned to it. The pin replaces whatever the
// developer exported: the system tier is the organization's, and pinning
// it is the point of the wrapper. A policy that sets the same variable in
// env loses to the pin too, with a note.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}
	launch := &agent.Launch{Agent: Name, Binary: binary}

	if err := managed.Validate(o.Settings.Managed); err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	settings, policies, err := managed.Parts(o.Settings.Managed)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	dir, err := a.cacheDir()
	if err != nil {
		return nil, err
	}
	if err := cache.Prune(dir, pruneAfter); err != nil {
		launch.Notes = append(launch.Notes, "gemini: could not prune old session files: "+err.Error())
	}

	if len(policies) > 0 {
		policyBytes, err := policiesTOML(fmt.Sprintf(launchHeader, "unversioned"), policies)
		if err != nil {
			return nil, err
		}
		policyPath, err := cache.Write(dir, "policies", ".toml", policyBytes)
		if err != nil {
			return nil, err
		}
		launch.Files = append(launch.Files, policyPath)
		settings, err = withPolicyPath(settings, policyPath)
		if err != nil {
			return nil, err
		}
	}
	settingsBytes, err := settingsJSON(settings)
	if err != nil {
		return nil, err
	}
	settingsPath, err := cache.Write(dir, "settings", ".json", settingsBytes)
	if err != nil {
		return nil, err
	}
	launch.Files = append(launch.Files, settingsPath)

	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch.Notes = append(launch.Notes, notes...)
	env, note := pin(env, SystemSettingsEnv, settingsPath)
	if note != "" {
		launch.Notes = append(launch.Notes, note)
	}
	launch.Env = env
	launch.Notes = append(launch.Notes, "gemini: system settings for this session written to "+settingsPath)
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}

// withPolicyPath returns settings with path appended to policyPaths, the
// author's own entries first. settings is not modified: it is the compiled
// document, which doctor may print afterwards. An existing policyPaths that
// is not a list is refused rather than silently overwritten: Validate only
// checks settings' top-level key names and JSON round-trippability, so a
// malformed policyPaths would otherwise pass unnoticed until Gemini itself
// rejected the file.
func withPolicyPath(settings map[string]any, path string) (map[string]any, error) {
	out := make(map[string]any, len(settings)+1)
	for k, v := range settings {
		out[k] = v
	}
	var existing []any
	if raw, present := out["policyPaths"]; present {
		var ok bool
		if existing, ok = raw.([]any); !ok {
			return nil, fmt.Errorf("gemini: settings.policyPaths is %T, not a list, so the session's policy file cannot be added", raw)
		}
	}
	paths := make([]any, 0, len(existing)+1)
	paths = append(paths, existing...)
	out["policyPaths"] = append(paths, path)
	return out, nil
}

// pin sets name=value in env, replacing any earlier value and saying so.
func pin(env []string, name, value string) ([]string, string) {
	out := make([]string, 0, len(env)+1)
	var note string
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			note = fmt.Sprintf("gemini: %s was %s; replaced with the organization's settings for this session", name, strings.TrimPrefix(e, name+"="))
			continue
		}
		out = append(out, e)
	}
	return append(out, name+"="+value), note
}

// Inspect reports whether Gemini is governed on this machine: the system
// settings file and aw-sync's admin policy file are present and parseable,
// and the developer has not moved the system settings path, which bare
// `gemini` would honour. It runs nothing.
func (a *Adapter) Inspect(env []string) []agent.Finding {
	if env == nil {
		env = os.Environ()
	}
	findings := make([]agent.Finding, 0, 3)
	dir := a.systemDir()

	settingsPath := filepath.Join(dir, SettingsFile)
	raw, err := os.ReadFile(settingsPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: Gemini's system settings are not governed until aw-sync has run", settingsPath)})
	case err != nil:
		findings = append(findings, agent.Finding{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", settingsPath, err)})
	default:
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			findings = append(findings, agent.Finding{Level: agent.Error,
				Message: fmt.Sprintf("%s is not valid JSON, so Gemini ignores it: %v", settingsPath, err)})
		} else {
			revision, _ := os.ReadFile(settingsPath + ".aw-revision")
			findings = append(findings, agent.Finding{Level: agent.OK,
				Message: fmt.Sprintf("%s (policy revision %s)", settingsPath, orUnknown(strings.TrimSpace(string(revision))))})
		}
	}

	policiesPath := filepath.Join(dir, PoliciesFile)
	raw, err = os.ReadFile(policiesPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: Gemini's admin policies are not governed until aw-sync has run", policiesPath)})
	case err != nil:
		findings = append(findings, agent.Finding{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", policiesPath, err)})
	default:
		var doc map[string]any
		if err := toml.Unmarshal(raw, &doc); err != nil {
			findings = append(findings, agent.Finding{Level: agent.Error,
				Message: fmt.Sprintf("%s is not valid TOML, so Gemini ignores it: %v", policiesPath, err)})
		} else {
			findings = append(findings, agent.Finding{Level: agent.OK,
				Message: fmt.Sprintf("%s (policy revision %s)", policiesPath, orUnknown(agent.RevisionFromHeader(raw)))})
		}
	}

	for _, e := range env {
		if strings.HasPrefix(e, SystemSettingsEnv+"=") {
			findings = append(findings, agent.Finding{Level: agent.Warn,
				Message: fmt.Sprintf("%s is set to %s: bare `gemini` reads that file instead of the system settings (aw gemini pins its own)",
					SystemSettingsEnv, strings.TrimPrefix(e, SystemSettingsEnv+"="))})
		}
	}
	return findings
}

// orUnknown returns s, or "unknown" when s is empty, for a finding message
// that always names a revision.
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
