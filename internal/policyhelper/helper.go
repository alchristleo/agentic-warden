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
// The order of preference is: a fresh policy from the control plane; failing
// that, the last policy the control plane served this subject; failing that,
// an envelope that carries no managedSettings, which tells Claude Code to
// apply the static managed-settings file the organization deployed. That
// last case is a first launch during an outage, and it is governed by
// whatever the file says, which is more than nothing.
package policyhelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/cache"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/repo"
)

// Source says where the emitted policy came from.
type Source string

const (
	// SourceServer means the control plane served a fresh policy, or
	// confirmed that the cached one is current.
	SourceServer Source = "server"
	// SourceCache means the control plane could not be used and the last
	// good policy for this subject was emitted instead.
	SourceCache Source = "cache"
	// SourceNone means there was nothing to emit: no fresh policy and no
	// cache. The envelope omits managedSettings.
	SourceNone Source = "none"
)

// maxOutput is what Claude Code will read from the helper's stdout. An
// envelope at or past it fails the run, so one is never emitted.
const maxOutput = 1 << 20

// maxResponse bounds what is read from the control plane. It is above
// maxOutput so that an oversized policy is diagnosed as such rather than as
// truncated JSON.
const maxResponse = 2 * maxOutput

// DefaultTimeout is the whole HTTP budget when Config.Timeout is zero. It
// sits well under Claude Code's default policyHelper.timeoutMs of 10000, and
// under its 1000 minimum with room to print, so a slow control plane costs a
// developer a pause and never a refused launch.
const DefaultTimeout = 3 * time.Second

// Config is everything a run needs. It is plain data so that the executable
// and the tests build it the same way.
type Config struct {
	// ServerURL is the control plane's base URL.
	ServerURL string
	// Groups are the subject's group memberships. Until identity lands they
	// come from the helper's deployed configuration.
	Groups []string
	// WorkDir is where the session runs; the repository it is in, if any,
	// is the subject's repo. Empty means the current directory.
	WorkDir string
	// RepoOverride names the repository directly and skips detection.
	RepoOverride string
	// CacheDir holds the last-good policy per subject and the audit log.
	CacheDir string
	// Timeout bounds the whole control plane exchange; zero means
	// DefaultTimeout.
	Timeout time.Duration
	// RequireFresh makes a run without a fresh policy exit non-zero, so
	// Claude Code refuses to start rather than run on cached or absent
	// policy. It is the organization's opt-in; the default is to fail safe.
	RequireFresh bool
	// ClaudeCodeVersion is reported to the control plane so a policy can
	// one day depend on it. Claude Code sets CLAUDE_CODE_VERSION.
	ClaudeCodeVersion string
	// HTTPClient overrides the client used; nil means a default one.
	HTTPClient *http.Client
	// Now supplies the time for the cache and audit log.
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

// cacheEntry is the on-disk form of a last-good policy.
type cacheEntry struct {
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
	Version   string    `json:"version,omitempty"`
	// Managed is the validated managedSettings object, ready to emit.
	Managed map[string]any `json:"managed"`
}

// Run computes the envelope for one launch. It never panics out and never
// returns without an Output.
func Run(ctx context.Context, cfg Config) (result Result) {
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
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	subject, notes := resolveSubject(cfg)
	result.Notes = notes
	cachePath := filepath.Join(cfg.CacheDir, "policy-"+subjectKey(cfg.ServerURL, subject)+".json")
	cached := loadCache(cachePath, &result)

	fresh, err := fetch(ctx, cfg, subject, cached)
	switch {
	case err == nil && fresh != nil:
		result.Source = SourceServer
		result.Version = fresh.Version
		if err := cache.Replace(cachePath, mustJSON(fresh)); err != nil {
			result.note("cache not updated: %v", err)
		}
		result.Output = envelope(fresh.Managed)
	case err == nil && cached != nil:
		// 304: the cache is what the server would have sent.
		result.Source = SourceServer
		result.Version = cached.Version
		result.Output = envelope(cached.Managed)
	case cached != nil:
		result.note("using the cached policy from %s: %v", cached.FetchedAt.Format(time.RFC3339), err)
		result.Source = SourceCache
		result.Version = cached.Version
		result.Output = envelope(cached.Managed)
	default:
		if err != nil {
			result.note("no policy available: %v", err)
		}
		result.Source = SourceNone
		result.Output = envelope(nil)
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

// exitCode is zero unless the organization requires a fresh policy and this
// run did not get one.
func exitCode(cfg Config, source Source) int {
	if cfg.RequireFresh && source != SourceServer {
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

func resolveSubject(cfg Config) (policy.Subject, []string) {
	subject := policy.Subject{Groups: cfg.Groups, Repo: cfg.RepoOverride}
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

// subjectKey names the cache file for one subject against one server. Any
// difference in what is sent to the server yields a different file, so a
// cache can never be served for a subject it was not compiled for.
func subjectKey(server string, s policy.Subject) string {
	sum := sha256.Sum256([]byte(server + "\x00" + strings.Join(s.Groups, "\x00") + "\x00" + s.Repo))
	return hex.EncodeToString(sum[:])[:16]
}

// loadCache reads the last-good entry, revalidating it against the schema
// this binary carries: a cache written by an older build is not trusted just
// because it was valid then. Any problem means no cache.
func loadCache(path string, result *Result) *cacheEntry {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			result.note("cache unreadable: %v", err)
		}
		return nil
	}
	var entry cacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		result.note("cache discarded: %v", err)
		return nil
	}
	if entry.Managed == nil {
		return nil
	}
	if err := schema.Validate(entry.Managed); err != nil {
		result.note("cache discarded: %v", err)
		return nil
	}
	return &entry
}

// fetch asks the control plane for the subject's policy. It returns a new
// entry on 200, nil and no error on 304, and an error otherwise. The error
// cases are deliberately broad: anything that is not a valid fresh policy is
// a reason to fall back, never a reason to emit something doubtful.
func fetch(ctx context.Context, cfg Config, subject policy.Subject, cached *cacheEntry) (*cacheEntry, error) {
	if cfg.ServerURL == "" {
		return nil, errors.New("no control plane URL configured")
	}
	query := url.Values{}
	for _, g := range subject.Groups {
		query.Add("group", g)
	}
	if subject.Repo != "" {
		query.Set("repo", subject.Repo)
	}
	endpoint := strings.TrimSuffix(cfg.ServerURL, "/") + "/v1/policy"
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", "aw-policy")
	if cfg.ClaudeCodeVersion != "" {
		req.Header.Set("X-Claude-Code-Version", cfg.ClaudeCodeVersion)
	}
	if cached != nil && cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("control plane unreachable: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		return nil, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("control plane answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("reading the policy: %w", err)
	}
	if len(body) > maxResponse {
		return nil, fmt.Errorf("the policy exceeds %d bytes", maxResponse)
	}
	doc, err := policy.Parse(body, endpoint)
	if err != nil {
		return nil, err
	}
	managed := managedSettings(doc.Agent("claude"))
	if err := schema.Validate(managed); err != nil {
		return nil, fmt.Errorf("the served policy is not valid Claude Code settings: %w", err)
	}
	return &cacheEntry{
		ETag:      resp.Header.Get("ETag"),
		FetchedAt: cfg.Now().UTC(),
		Version:   doc.Version,
		Managed:   managed,
	}, nil
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
