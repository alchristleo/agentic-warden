// Package agy integrates Google's Antigravity CLI. Its enforced
// configuration is one root-owned file agy reads on its own,
// admin_settings.json, which outranks the developer's environment and user
// settings; this package renders it for aw-sync. No environment variable or
// flag moves that file, so launching agy through aw cannot scope it per
// session: Build is a passthrough.
package agy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/agent/agy/managed"
	"github.com/alchristleo/agentic-warden/internal/merge"
)

// Name is the subcommand a developer types and the agent key in a policy.
const Name = "agy"

// AdminFile is agy's admin settings file, relative to its system directory.
// aw-sync owns the whole file. Its revision rides in the .aw-revision
// sibling that sync writes for every JSON file.
const AdminFile = "admin_settings.json"

// gatewayKeyEnv is the developer's own gateway credential. It never belongs
// in policy.
const gatewayKeyEnv = "AGY_LLM_GATEWAY_API_KEY"

// SystemDir is where agy reads AdminFile on goos. Windows keeps it under
// ProgramData, which an installation can relocate.
func SystemDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/Antigravity"
	case "windows":
		return agent.ProgramData() + `\Antigravity`
	default:
		return "/etc/antigravity"
	}
}

// Adapter launches agy and renders its admin file. The exported fields exist
// so tests can control the binary and the directory.
type Adapter struct {
	// Binary is the command to resolve on PATH; empty means Name.
	Binary string
	// SystemDir overrides where Inspect looks for AdminFile; empty means
	// SystemDir(runtime.GOOS).
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

// Locate resolves the real agy binary, never the wrapper.
func (a *Adapter) Locate(env []string) (string, error) {
	name := a.Binary
	if name == "" {
		name = Name
	}
	return agent.ResolveBinary(name, env)
}

// Build computes the launch. Nothing can be pinned per launch, so it merges
// the policy's environment, passes the arguments through and writes no file.
func (a *Adapter) Build(ctx context.Context, o agent.BuildOptions) (*agent.Launch, error) {
	binary, err := a.Locate(o.Env)
	if err != nil {
		return nil, err
	}
	base := o.Env
	if base == nil {
		base = os.Environ()
	}
	env, notes := merge.Env(base, o.Settings.Env, o.Settings.ForceEnv)
	launch := &agent.Launch{Agent: Name, Binary: binary, Env: env, Notes: notes}
	launch.Notes = append(launch.Notes, "agy: enforcement is machine-wide (admin_settings.json via aw-sync); nothing is pinned per launch")
	launch.Args = append(launch.Args, o.Args...)
	return launch, nil
}

// Inspect reports whether agy has an admin file on this machine and whether
// it can work. It runs nothing and never claims enforcement: agy may ignore
// the file.
func (a *Adapter) Inspect(env []string) []agent.Finding {
	if env == nil {
		env = os.Environ()
	}
	path := filepath.Join(a.systemDir(), AdminFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []agent.Finding{{Level: agent.Warn,
			Message: fmt.Sprintf("no %s: agy's admin settings are not governed until aw-sync has run", path)}}
	}
	if err != nil {
		return []agent.Finding{{Level: agent.Error, Message: fmt.Sprintf("%s is unreadable: %v", path, err)}}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []agent.Finding{{Level: agent.Error,
			Message: fmt.Sprintf("%s is not valid JSON, so agy ignores it: %v", path, err)}}
	}
	if err := managed.Validate(doc); err != nil {
		return []agent.Finding{{Level: agent.Error, Message: fmt.Sprintf("%s is invalid: %v", path, err)}}
	}

	var findings []agent.Finding
	revision, revErr := os.ReadFile(path + ".aw-revision")
	if revErr != nil {
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("%s has no .aw-revision sibling: it was not written by aw-sync", path)})
	} else {
		findings = append(findings, agent.Finding{Level: agent.OK,
			Message: fmt.Sprintf("%s: admin file present, revision %s", path, strings.TrimSpace(string(revision)))})
	}

	if g, _ := doc["gateway"].(map[string]any); g != nil {
		if enabled, _ := g["enabled"].(bool); enabled && envValue(env, gatewayKeyEnv) == "" {
			findings = append(findings, agent.Finding{Level: agent.Warn,
				Message: gatewayKeyEnv + " is not set: agy sends gateway requests with no API key"})
		}
	}
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(name, "AGY_LLM_GATEWAY_") && name != gatewayKeyEnv {
			findings = append(findings, agent.Finding{Level: agent.OK,
				Message: name + " is set: the admin file outranks it"})
		}
	}
	return findings
}

func envValue(env []string, name string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			return v
		}
	}
	return ""
}
