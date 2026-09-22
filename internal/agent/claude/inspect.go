package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/signing"
)

// stateTrustFile, stateBundleFile and stateSignatureFile mirror
// sync.TrustFile, sync.BundleFile and sync.SignatureFile. This package
// cannot import internal/sync, which imports this package for SystemDir,
// nor internal/policyhelper, which imports internal/sync — either would
// close an import cycle. So the file names are kept in step by hand rather
// than a shared constant, and the verification below calls the same
// internal/signing primitives Task 7's policyhelper.VerifyBundle calls
// (ParsePublic, Verify, KeyID) rather than that function itself, which is
// what cmd/aw's resolvePolicy calls directly since it sits outside this
// cycle. stateBundleFile happens to share its value with BundleFile above,
// but the two name different files: BundleFile is the narrowed copy this
// adapter renders into the system directory; stateBundleFile is aw-sync's
// own signed copy in its state directory.
const (
	stateTrustFile     = "aw-trust.pub"
	stateBundleFile    = "aw-bundle.json"
	stateSignatureFile = "aw-bundle.json.sig"
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
		binary := checkHelperBinary(helper, source)
		findings = append(findings, binary)
		if binary.Level == agent.OK {
			if build, warn := checkHelperBuild(helper, source); warn {
				findings = append(findings, build)
			}
		}
	}

	findings = append(findings, inspectBundle(systemDir))
	findings = append(findings, inspectSignature(a.StateDir))
	if warn := inspectPermissions(a.goos(), a.StateDir); warn != nil {
		findings = append(findings, *warn)
	}

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

// helperProbeTimeout bounds `aw-policy build-info`. The real helper answers
// instantly; the bound is for a helper that is not ours and reads stdin or
// waits on a network.
const helperProbeTimeout = 2 * time.Second

// checkHelperBuild asks the helper which build it is and warns when it is a
// test build, since that build honours AW_POLICY_BUNDLE from the
// developer's environment and so lets a developer choose their own policy.
// Every other outcome, including a helper that fails on the argument or
// prints something else, is reported as nothing: the drop-in may name a
// helper that is not ours, and only a positive answer is evidence.
func checkHelperBuild(helper, source string) (agent.Finding, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), helperProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, helper, "build-info").Output()
	if err != nil || !strings.Contains(string(out), "env-overrides=on") {
		return agent.Finding{}, false
	}
	return agent.Finding{Level: agent.Warn,
		Message: fmt.Sprintf("policyHelper %s (from %s) was built with -tags awtest: a developer can point it at their own bundle; rebuild without the tag", helper, source)}, true
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
	if len(bundle.Rules) == 0 {
		return agent.Finding{Level: agent.Warn,
			Message: fmt.Sprintf("bundle %s has no rules (version %s): nothing is enforced; check the policy and aw-sync status", path, version)}
	}
	return agent.Finding{Level: agent.OK,
		Message: fmt.Sprintf("bundle %s: version %s, %d rule(s) for %d group(s)", path, version, len(bundle.Rules), len(bundle.Groups))}
}

// inspectSignature reports the state aw-sync's signature check is in, from
// its own state directory, not from this agent's system directory (which
// holds a narrowed, re-encoded copy of the bundle that a signature over the
// original bytes cannot check). Three states, and only a positive failure
// is an Error: a deployment that does not sign is a choice, not a fault.
//
// This mirrors policyhelper.VerifyBundle's file layout and signature-line
// format rather than calling it, because internal/policyhelper imports
// internal/sync, which imports this package for SystemDir; importing
// policyhelper here would close that cycle. What it does share with
// VerifyBundle is the actual cryptographic check, via the same
// internal/signing primitives (ParsePublic, Verify, KeyID) — only the glue
// that reads three files and picks a message is written twice.
func inspectSignature(stateDir string) agent.Finding {
	if stateDir == "" {
		// No state directory configured reads exactly like a machine with
		// no trust file: signing was never turned on for this Inspect call.
		return agent.Finding{Level: agent.OK, Message: "bundle signature: unsigned deployment"}
	}
	trustPath := filepath.Join(stateDir, stateTrustFile)
	bundlePath := filepath.Join(stateDir, stateBundleFile)

	trust, err := os.ReadFile(trustPath)
	if errors.Is(err, os.ErrNotExist) {
		return agent.Finding{Level: agent.OK, Message: "bundle signature: unsigned deployment"}
	}
	fail := func(keyID string) agent.Finding {
		return agent.Finding{Level: agent.Error, Message: "bundle signature: FAILED — " + bundlePath + " does not match key " + keyID}
	}
	if err != nil {
		// Present but unreadable is a broken deployment, not an unsigned
		// one; there is no key ID to show for it.
		return fail("")
	}
	key, err := signing.ParsePublic(string(trust))
	if err != nil {
		return fail("")
	}
	keyID := signing.KeyID(key)

	body, err := os.ReadFile(bundlePath)
	if err != nil {
		return fail(keyID)
	}
	line, err := os.ReadFile(filepath.Join(stateDir, stateSignatureFile))
	if err != nil {
		return fail(keyID)
	}
	fields := strings.Fields(string(line))
	if len(fields) != 3 || fields[0] != "aw-ed25519" || !signing.Verify(key, body, fields[2]) {
		return fail(keyID)
	}
	return agent.Finding{Level: agent.OK, Message: "bundle signature: verified (key " + keyID + ")"}
}

// worldWritable is the permission-bit mask that, ORed into a file's mode,
// means someone other than its owner can rewrite it: group or other write
// access. Either bit on the bundle or the trust file would let a non-root
// account replace what the signature check above trusts.
const worldWritable = 0o022

// inspectPermissions warns when the bundle or the trust file in stateDir is
// writable by more than its owner. It is skipped on Windows, where the mode
// bits Stat reports carry no ACL information, so a check based on them
// would only produce a false sense of assurance; the real answer lives in
// the file's ACL, which this package has no portable way to read.
func inspectPermissions(goos, stateDir string) *agent.Finding {
	if stateDir == "" || goos == "windows" {
		return nil
	}
	for _, name := range []string{stateBundleFile, stateTrustFile} {
		path := filepath.Join(stateDir, name)
		info, err := os.Stat(path)
		if err != nil {
			// Absent or unreadable is inspectSignature's to report; a
			// permission warning about a file that is not there would only
			// be noise.
			continue
		}
		if info.Mode().Perm()&worldWritable != 0 {
			return &agent.Finding{Level: agent.Warn, Message: fmt.Sprintf(
				"%s is writable by more than its owner (mode %s): anyone who can rewrite it can control what the signature check trusts",
				path, info.Mode().Perm())}
		}
	}
	return nil
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
// the bundle back. sync.AgentRoot delegates to each adapter's SystemDir
// rather than keeping its own copy of this table, so this file and
// internal/sync/paths.go cannot drift; a test in that package checks it.
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
