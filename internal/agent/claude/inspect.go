package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/policy"
)

// remoteSettingsFile is where Claude Code caches server-managed settings.
// Its presence with any key means the console delivers a policy, and a
// server-managed policy shadows every file-based source, helper included.
const remoteSettingsFile = "remote-settings.json"

// fetchSkippers are environment variables that make Claude Code skip the
// server-managed settings fetch entirely, so a cached payload cannot shadow
// the helper on that machine.
var fetchSkippers = []string{
	"ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_MANTLE",
}

// Inspect reports whether the policy helper is wired up on this machine and
// whether anything shadows it. Two things silently disable enforcement: no
// helper configured in any file-based source, and a server-managed payload
// from the claude.ai console. One thing breaks every launch: a helper path
// that does not resolve to an executable.
func (a *Adapter) Inspect(env []string) []agent.Finding {
	if env == nil {
		env = os.Environ()
	}
	findings := make([]agent.Finding, 0, 4)

	systemDir := a.systemDir()
	helper, source, err := findPolicyHelper(systemDir)
	switch {
	case err != nil:
		findings = append(findings, agent.Finding{Level: agent.Error, Message: err.Error()})
	case helper == "":
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no policyHelper in %s: organization policy is not enforced on bare `claude`", systemDir)})
	default:
		findings = append(findings, checkHelperBinary(helper, source))
	}

	findings = append(findings, inspectBundle(systemDir))

	if skipper := firstSet(env, fetchSkippers); skipper != "" {
		findings = append(findings, agent.Finding{Level: agent.OK,
			Message: skipper + " is set, so Claude Code skips the server-managed settings fetch; nothing shadows the helper"})
		return findings
	}
	remote := filepath.Join(a.configDir(env), remoteSettingsFile)
	if keys := topLevelKeys(remote); len(keys) > 0 {
		findings = append(findings, agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("%s holds a server-managed policy (%s); it shadows policyHelper until removed in the claude.ai console",
				remote, strings.Join(keys, ", "))})
	}
	return findings
}

// findPolicyHelper scans the managed settings file and the drop-ins in the
// order Claude Code merges them, so the last file to set policyHelper wins,
// and returns the helper path and the file it came from.
func findPolicyHelper(systemDir string) (helper, source string, err error) {
	files := []string{filepath.Join(systemDir, "managed-settings.json")}
	dropIns, _ := filepath.Glob(filepath.Join(systemDir, "managed-settings.d", "*.json"))
	sort.Strings(dropIns)
	for _, path := range dropIns {
		if !strings.HasPrefix(filepath.Base(path), ".") {
			files = append(files, path)
		}
	}
	for _, path := range files {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue // absent, or unreadable; either way not a source
		}
		var settings struct {
			PolicyHelper *struct {
				Path string `json:"path"`
			} `json:"policyHelper"`
		}
		if jsonErr := json.Unmarshal(raw, &settings); jsonErr != nil {
			return "", "", fmt.Errorf("%s is not valid JSON, which makes Claude Code refuse to start: %v", path, jsonErr)
		}
		if settings.PolicyHelper != nil {
			helper, source = settings.PolicyHelper.Path, path
		}
	}
	return helper, source, nil
}

// checkHelperBinary reports whether Claude Code will be able to run the
// helper. A path that fails here fails every launch.
func checkHelperBinary(helper, source string) agent.Finding {
	info, err := os.Stat(helper)
	switch {
	case err != nil:
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("policyHelper %s (from %s) is missing: Claude Code refuses to start until it exists", helper, source)}
	case !info.Mode().IsRegular():
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("policyHelper %s (from %s) is not a regular file", helper, source)}
	case runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0:
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("policyHelper %s (from %s) is not executable", helper, source)}
	}
	return agent.Finding{Level: agent.OK, Message: fmt.Sprintf("policyHelper %s (from %s)", helper, source)}
}

// inspectBundle reports the bundle aw-sync leaves for aw-policy. Without
// it the helper emits no settings and every launch runs on the static
// managed-settings files alone, which is silent; with one that does not
// parse, the same happens for a worse reason. Age and last error are
// aw-sync's to report, from its state directory; `aw doctor` shows both.
func inspectBundle(systemDir string) agent.Finding {
	path := filepath.Join(systemDir, BundleFile)
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("no bundle at %s: aw-policy emits no settings until aw-sync has run", path)}
	case err != nil:
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("bundle %s is unreadable, so aw-policy emits no settings: %v", path, err)}
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return agent.Finding{Level: agent.Error,
			Message: fmt.Sprintf("bundle %s is not valid JSON, so aw-policy emits no settings: %v", path, err)}
	}
	version := bundle.Version
	if version == "" {
		version = "unversioned"
	}
	return agent.Finding{Level: agent.OK,
		Message: fmt.Sprintf("bundle %s: version %s, %d rule(s) for %d group(s)", path, version, len(bundle.Rules), len(bundle.Groups))}
}

// topLevelKeys lists the keys of the JSON object in path, sorted, or nothing
// when the file is absent, unreadable or not an object.
func topLevelKeys(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func firstSet(env, names []string) string {
	for _, name := range names {
		for _, entry := range env {
			if value, ok := strings.CutPrefix(entry, name+"="); ok && value != "" {
				return name
			}
		}
	}
	return ""
}

// SystemDir is where Claude Code reads its managed settings on goos, and so
// where aw-sync writes the bundle and the drop-in and where aw-policy reads
// the bundle back. One table, so the writer and the reader cannot disagree.
func SystemDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}

func (a *Adapter) systemDir() string {
	if a.SystemDir != "" {
		return a.SystemDir
	}
	return SystemDir(a.goos())
}

func (a *Adapter) configDir(env []string) string {
	if a.ConfigDir != "" {
		return a.ConfigDir
	}
	for _, entry := range env {
		if dir, ok := strings.CutPrefix(entry, "CLAUDE_CONFIG_DIR="); ok && dir != "" {
			return dir
		}
	}
	for _, entry := range env {
		if home, ok := strings.CutPrefix(entry, "HOME="); ok && home != "" {
			return filepath.Join(home, ".claude")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}
