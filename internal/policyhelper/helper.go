// Package policyhelper computes the managed settings Claude Code applies to a
// session. It is the core of aw-policy, the policyHelper executable.
//
// This is the highest-severity code path in the project. Claude Code runs the
// helper before accepting a prompt, and if the helper exits non-zero, times
// out, or emits a managedSettings object with a schema violation, Claude Code
// refuses to start. So the contract here is fail-safe: whatever goes wrong,
// Run returns something valid to print and an exit code of zero. The only
// exception is an organization that opts into failing closed.
//
// The helper is offline. aw-sync, running as root on a timer, fetches the
// enrolled user's bundle from the control plane and leaves it in Claude
// Code's system directory; the helper reads that file, makes the one
// decision left in it (which repository the session is in) and prints the
// result. No bundle means an envelope that carries no managedSettings, which
// tells Claude Code to apply the static managed-settings files the
// organization deployed. That is a machine aw-sync has not reached yet, and
// it is governed by whatever those files say, which is more than nothing.
package policyhelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/repo"
	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/sync"
)

// Source says where the emitted policy came from.
type Source string

const (
	// SourceBundle means the bundle aw-sync left on this machine was
	// compiled for the session and emitted.
	SourceBundle Source = "bundle"
	// SourceNone means there was nothing to emit: no bundle, or one this
	// binary could not use. The envelope omits managedSettings.
	SourceNone Source = "none"
)

// maxOutput is what Claude Code will read from the helper's stdout. An
// envelope at or past it fails the run, so one is never emitted.
const maxOutput = 1 << 20

// maxBundle bounds what is read from the bundle file. aw-sync fetches at
// most 4 MiB from the control plane and writes it back out re-indented,
// which can grow it slightly; 8 MiB is comfortably above anything aw-sync
// produces, so a file past it is not its work and is not trusted. The 1
// MiB guard on the output still protects Claude Code.
const maxBundle = 8 << 20

// Config is everything a run needs. It is plain data so that the executable
// and the tests build it the same way.
type Config struct {
	// BundlePath is the bundle aw-sync wrote for this machine's user.
	BundlePath string
	// StateDir is where aw-sync keeps the signed bundle, its signature and
	// the key they are checked against. Empty disables verification, which
	// is what a test aiming at a single bundle file wants.
	StateDir string
	// WorkDir is where the session runs; the repository it is in, if any,
	// is the subject's repo. Empty means the current directory.
	WorkDir string
	// RepoOverride names the repository directly and skips detection.
	RepoOverride string
	// AuditDir holds the audit log. Empty disables auditing.
	AuditDir string
	// RequireBundle makes a run without a usable bundle exit non-zero, so
	// Claude Code refuses to start rather than run ungoverned. It is the
	// organization's opt-in; the default is to fail safe.
	RequireBundle bool
	// Now supplies the time for the audit log.
	Now func() time.Time
}

// Result is what a run produced. Output is always a complete JSON envelope,
// even when ExitCode is non-zero, so the executable can print it regardless.
type Result struct {
	// Output is the envelope to write to stdout.
	Output []byte
	// ExitCode is what the executable should exit with.
	ExitCode int
	// Source says where the policy came from.
	Source Source
	// Version is the policy revision emitted, when one was.
	Version string
	// Notes explain what happened, for stderr and the audit log.
	Notes []string
}

// Run computes the envelope for one launch. It never panics out and never
// returns without an Output.
func Run(cfg Config) (result Result) {
	defer func() {
		// A defect in this package must still leave Claude Code able to
		// start. Turn a panic into the no-policy envelope and a note.
		if r := recover(); r != nil {
			result = Result{Source: SourceNone, Notes: []string{fmt.Sprintf("internal error: %v", r)}}
			result.Output = envelope(nil)
			result.ExitCode = exitCode(cfg, result.Source)
		}
	}()

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	subject, notes := resolveSubject(cfg)
	result.Notes = notes

	var managed map[string]any
	bundlePath, note, ok := VerifyBundle(cfg.StateDir, cfg.BundlePath)
	if !ok {
		result.note("%s", note)
		result.Source = SourceNone
		result.Output = envelope(nil)
	} else {
		bundle, err := loadBundle(bundlePath)
		if err == nil {
			subject.Groups = bundle.Groups
			managed, err = compile(bundle, subject.Repo)
		}
		if err != nil {
			result.note("no policy available: %v", err)
			result.Source = SourceNone
			result.Output = envelope(nil)
		} else {
			result.Source = SourceBundle
			result.Version = bundle.Version
			result.Output = envelope(managed)
		}
	}

	if len(result.Output) >= maxOutput {
		// Claude Code would fail the run on an oversized envelope, which is
		// worse than an ungoverned session with a loud note.
		result.note("the policy is %d bytes, over Claude Code's 1 MiB limit; emitting no policy", len(result.Output))
		result.Source = SourceNone
		result.Version = ""
		result.Output = envelope(nil)
	}
	result.ExitCode = exitCode(cfg, result.Source)
	audit(cfg, subject, result)
	return result
}

func (r *Result) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// exitCode is zero unless the organization requires a bundle and this run
// did not get a usable one.
func exitCode(cfg Config, source Source) int {
	if cfg.RequireBundle && source != SourceBundle {
		return 1
	}
	return 0
}

// envelope builds the stdout document. A nil managed omits the key, which
// Claude Code reads as "no managed settings from the helper".
func envelope(managed map[string]any) []byte {
	if managed == nil {
		return []byte("{}\n")
	}
	out := mustJSON(map[string]any{"managedSettings": managed})
	return append(out, '\n')
}

// resolveSubject finds the repository the session runs in. Groups are not
// decided here: the bundle carries the ones it was cut for.
func resolveSubject(cfg Config) (policy.Subject, []string) {
	subject := policy.Subject{Repo: cfg.RepoOverride}
	if subject.Repo != "" {
		return subject, nil
	}
	dir := cfg.WorkDir
	if dir == "" {
		dir = "."
	}
	detected, err := repo.Detect(dir)
	if err != nil {
		// Not knowing the repo means the baseline policy applies, which is
		// the conservative outcome; record why.
		return subject, []string{"repository not detected: " + err.Error()}
	}
	subject.Repo = detected
	return subject, nil
}

// VerifyBundle reports which bundle file to compile from. An unsigned
// deployment — no trust file in stateDir — yields fallback and no note, so a
// machine that has never seen a signature behaves exactly as it did before
// signing existed. A note with ok false means signing is configured and the
// proof does not hold; the caller emits the no-policy envelope rather than
// compile from bytes that did not check out. Only a missing trust file reads
// as unsigned: a trust file present but unreadable for some other reason
// (permission denied, for instance) is a broken deployment, and folding it
// into the unsigned case would make this function fail open exactly where
// its job is to fail closed.
func VerifyBundle(stateDir, fallback string) (path string, note string, ok bool) {
	if stateDir == "" {
		return fallback, "", true
	}
	trustPath := filepath.Join(stateDir, sync.TrustFile)
	trust, err := os.ReadFile(trustPath)
	if errors.Is(err, os.ErrNotExist) {
		// No trust file at all is the unsigned case. Anything else reading
		// it — permission denied, a directory in its place — is a broken
		// deployment, not an absent one, and must not be mistaken for
		// "signing was never turned on here".
		return fallback, "", true
	}
	if err != nil {
		return "", "the trusted key " + trustPath + " is unreadable: " + err.Error(), false
	}
	key, err := signing.ParsePublic(string(trust))
	if err != nil {
		return "", "the trusted key " + trustPath + " is unreadable: " + err.Error(), false
	}
	bundlePath := filepath.Join(stateDir, sync.BundleFile)
	body, err := os.ReadFile(bundlePath)
	if err != nil {
		return "", "the signed bundle is unreadable: " + err.Error(), false
	}
	line, err := os.ReadFile(filepath.Join(stateDir, sync.SignatureFile))
	if err != nil {
		return "", "no signature beside " + bundlePath, false
	}
	fields := strings.Fields(string(line))
	if len(fields) != 3 || fields[0] != "aw-ed25519" || !signing.Verify(key, body, fields[2]) {
		return "", bundlePath + " is not signed by key " + signing.KeyID(key), false
	}
	return bundlePath, "", true
}

// loadBundle reads what aw-sync left. Every problem is one error: the
// caller does nothing different for a missing file than for a broken one,
// and the message says which it was.
func loadBundle(path string) (*policy.Bundle, error) {
	if path == "" {
		return nil, errors.New("no bundle path configured")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("bundle %s: %w", path, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxBundle+1))
	if err != nil {
		return nil, fmt.Errorf("reading bundle %s: %w", path, err)
	}
	if len(raw) > maxBundle {
		return nil, fmt.Errorf("bundle %s exceeds %d bytes", path, maxBundle)
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, fmt.Errorf("bundle %s is not valid JSON: %w", path, err)
	}
	return &bundle, nil
}

// compile resolves the bundle for the session's repository and checks the
// result against the schema this binary carries. aw-sync validated every
// rule when it wrote the file, so a failure here means this build carries a
// newer schema than the one that wrote it, or the file was edited by hand:
// aw-sync validates each rule as it writes, not the merged result, and the
// file is world-readable. Either way the safe answer is still no settings
// rather than settings Claude Code refuses.
func compile(bundle *policy.Bundle, repoName string) (map[string]any, error) {
	managed := managedSettings(bundle.Compile(repoName).Agent("claude"))
	if err := schema.Validate(managed); err != nil {
		return nil, fmt.Errorf("the bundle compiles to settings this build rejects: %w", err)
	}
	return managed, nil
}

// managedSettings turns one agent's policy into the object Claude Code
// applies. The policy's env goes under the settings' own env key; a variable
// set in both keeps the managed value, since that is the more deliberate of
// the two places to put it.
func managedSettings(config policy.AgentConfig) map[string]any {
	out := make(map[string]any, len(config.Managed)+1)
	for k, v := range config.Managed {
		out[k] = v
	}
	if len(config.Env) == 0 {
		return out
	}
	env := make(map[string]any, len(config.Env))
	if existing, ok := out["env"].(map[string]any); ok {
		for k, v := range existing {
			env[k] = v
		}
	}
	for k, v := range config.Env {
		if _, taken := env[k]; !taken {
			env[k] = v
		}
	}
	out["env"] = env
	return out
}

// mustJSON encodes a value this package built itself; a failure would be a
// defect, and the recover in Run turns it into a note.
func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return out
}
