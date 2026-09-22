package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/cache"
)

// revisionSuffix names the sibling written beside every rendered JSON file,
// so an operator looking at /etc can see which revision produced it without
// parsing the file. TOML files carry the same fact as a header comment.
const revisionSuffix = ".aw-revision"

// auditRotateAt is the log size past which the current log is moved aside.
const auditRotateAt = 1 << 20

// Config is everything one cycle needs. It is plain data so the executable
// and the tests build it the same way.
type Config struct {
	// StateDir holds machine.json, state.json and the audit log.
	StateDir string
	// GOOS selects the system directories; empty means this binary's.
	GOOS string
	// Roots overrides the system directory per agent, so tests render into
	// temporary directories. An agent not listed uses AgentRoot.
	Roots map[string]string
	// Registry holds the adapters this binary was compiled with. Only those
	// implementing agent.Renderer can be synced.
	Registry *agent.Registry
	// HTTP overrides the client used to reach the control plane.
	HTTP *http.Client
	// Now supplies the time for state.json and the audit log.
	Now func() time.Time
}

// Result is what one cycle did.
type Result struct {
	// Unchanged means the server answered 304 and nothing was rendered.
	Unchanged bool
	// Version is the policy revision now on disk.
	Version string
	// Written lists the absolute paths of the files this cycle wrote.
	Written []string
	// Notes are what the renderers reported.
	Notes []string
	// Err is why the cycle failed. Files on disk are untouched when it is
	// set: a failure keeps the last good policy in force.
	Err error
}

// plannedFile is a rendered file resolved to its absolute path, held until
// every renderer has succeeded.
type plannedFile struct {
	path    string
	content []byte
	mode    fs.FileMode
}

// Run performs one cycle: fetch the bundle, render every enrolled agent,
// write everything or nothing, record what happened. It never deletes a
// rendered file; a machine that cannot reach the control plane keeps the
// policy it last received.
func Run(ctx context.Context, cfg Config) Result {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.Registry == nil {
		// Without a registry there is no adapter to look up, and reaching
		// the lookup below would panic on a nil pointer. Audited the same
		// way as ErrNotEnrolled: this is a caller misconfiguration, not a
		// cycle that ran and failed.
		res := Result{Written: make([]string, 0), Err: errors.New("sync: no adapter registry configured")}
		cfg.audit(State{}, res)
		return res
	}

	machine, err := LoadMachine(cfg.StateDir)
	if err != nil {
		// No state was loaded yet, so there is nothing to merge into, but a
		// fresh machine's first failure should still leave a trace.
		res := Result{Written: make([]string, 0), Err: err}
		cfg.audit(State{}, res)
		return res
	}
	state, err := LoadState(cfg.StateDir)
	var notes []string
	if err != nil {
		// A corrupt record must not stop the cycle; it is rebuilt below.
		notes = append(notes, "previous state discarded: "+err.Error())
		state = State{}
	}
	// The enrollment's non-secret facts ride in state.json too, so `status`
	// can report them without reading machine.json (0600). Every state this
	// cycle saves, on any path, carries the current facts.
	state.Server, state.MachineID, state.Agents = machine.Server, machine.MachineID, machine.Agents

	// A file this process rendered may have been edited or deleted since:
	// send an empty etag so the server cannot answer 304 and the cycle
	// re-renders everything, repairing the drift. `once` overwrites drift
	// unconditionally, so this check must run before the conditional fetch,
	// not after a 304 short-circuits it.
	etag := state.ETag
	var driftPaths []string
	for path, expected := range state.Files {
		if st, _ := fileState(path, expected); st != "ok" {
			driftPaths = append(driftPaths, path)
		}
	}
	if len(driftPaths) > 0 {
		sort.Strings(driftPaths)
		etag = ""
		for _, path := range driftPaths {
			notes = append(notes, fmt.Sprintf("drift detected in %s; re-rendering", path))
		}
	}

	client := &Client{Server: machine.Server, HTTP: cfg.HTTP}
	// The pinned key and rollover persistence belong to the task that stores
	// them in machine.json; until then every fetch verifies nothing.
	fetched, err := client.Fetch(ctx, machine.Credential, etag, nil)
	if err != nil {
		return cfg.fail(state, notes, nil, nil, err)
	}
	if fetched.Unchanged {
		state.Error = ""
		state.SyncedAt = cfg.Now().UTC()
		res := Result{Unchanged: true, Version: state.Version, Written: make([]string, 0), Notes: state.Notes}
		if err := SaveState(cfg.StateDir, state); err != nil {
			res.Err = err
		}
		cfg.audit(state, res)
		return res
	}

	// Render every agent before writing any file, so one agent's bad
	// bundle cannot leave another's half applied.
	var planned []plannedFile
	for _, name := range machine.Agents {
		adapter, err := cfg.Registry.Lookup(name)
		if err != nil {
			return cfg.fail(state, notes, nil, nil, err)
		}
		renderer, ok := adapter.(agent.Renderer)
		if !ok {
			return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: agent %q has no renderer", name))
		}
		root, err := cfg.root(name)
		if err != nil {
			return cfg.fail(state, notes, nil, nil, err)
		}
		rendering, err := renderer.Render(fetched.Bundle)
		if err != nil {
			return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: rendering %s: %w", name, err))
		}
		notes = append(notes, rendering.Notes...)
		for _, f := range rendering.Files {
			if filepath.IsAbs(f.Path) || !filepath.IsLocal(f.Path) {
				return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: renderer %s returned an unsafe path %q", name, f.Path))
			}
			planned = append(planned, plannedFile{path: filepath.Join(root, f.Path), content: f.Content, mode: f.Mode})
		}
	}

	// The full bundle goes beside state.json so `aw` can compile it for the
	// repository a session runs in, whatever agents this machine enrolled.
	bundleJSON, err := json.MarshalIndent(fetched.Bundle, "", "  ")
	if err != nil {
		return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: encoding the bundle: %w", err))
	}
	planned = append(planned, plannedFile{path: filepath.Join(cfg.StateDir, BundleFile), content: append(bundleJSON, '\n'), mode: 0o644})

	version := fetched.Bundle.Version
	written := make([]string, 0, len(planned))
	files := make(map[string]string, len(planned))
	for _, p := range planned {
		if err := cache.ReplaceMode(p.path, p.content, p.mode); err != nil {
			// Each write is atomic, so what landed is whole; the next cycle
			// rewrites the rest. written/files describe only what actually
			// reached disk this cycle, so fail can merge them into the last
			// good state instead of reporting stale pre-cycle hashes for
			// files this cycle already overwrote.
			return cfg.fail(state, notes, files, written, fmt.Errorf("sync: writing %s: %w", p.path, err))
		}
		written = append(written, p.path)
		files[p.path] = hashOf(p.content)
		if strings.HasSuffix(p.path, ".json") {
			if err := cache.ReplaceMode(p.path+revisionSuffix, []byte(revisionLine(version)), 0o644); err != nil {
				return cfg.fail(state, notes, files, written, fmt.Errorf("sync: writing %s: %w", p.path+revisionSuffix, err))
			}
		}
	}

	if notes == nil {
		notes = make([]string, 0)
	}
	state = State{
		Server:    machine.Server,
		MachineID: machine.MachineID,
		Agents:    machine.Agents,
		ETag:      fetched.ETag,
		Version:   version,
		SyncedAt:  cfg.Now().UTC(),
		Files:     files,
		Notes:     notes,
	}
	res := Result{Version: version, Written: written, Notes: notes}
	if err := SaveState(cfg.StateDir, state); err != nil {
		res.Err = err
	}
	cfg.audit(state, res)
	return res
}

// fail records err in the state without touching the last good etag,
// version or notes, since the cycle did not complete: `status` must still
// describe the policy revision actually in force. state.Notes is left as it
// was (the last good cycle's notes), not extended with this cycle's notes,
// so a machine stuck failing forever does not grow state.Notes without
// bound; this cycle's own notes are reported in Result.Notes only.
// partialFiles and partialWritten describe files this cycle wrote to disk
// before the failure (nil when the failure happened before any write);
// their hashes are merged into state.Files so a file aw-sync itself just
// overwrote is never reported as drift, and their paths are surfaced in
// Result.Written.
func (cfg Config) fail(state State, notes []string, partialFiles map[string]string, partialWritten []string, err error) Result {
	state.Error = err.Error()
	if len(partialFiles) > 0 {
		merged := make(map[string]string, len(state.Files)+len(partialFiles))
		for path, sum := range state.Files {
			merged[path] = sum
		}
		for path, sum := range partialFiles {
			merged[path] = sum
		}
		state.Files = merged
	}
	written := make([]string, len(partialWritten))
	copy(written, partialWritten)
	res := Result{Version: state.Version, Written: written, Notes: notes, Err: err}
	// A state write that fails is secondary to the failure being recorded;
	// the audit line still says what happened.
	_ = SaveState(cfg.StateDir, state)
	cfg.audit(state, res)
	return res
}

// root is the directory agentName's files go under: the configured override
// or the OS table.
func (cfg Config) root(agentName string) (string, error) {
	if dir, ok := cfg.Roots[agentName]; ok && dir != "" {
		return dir, nil
	}
	return AgentRoot(cfg.GOOS, agentName)
}

// revisionLine is the content of an .aw-revision sibling.
func revisionLine(version string) string {
	if version == "" {
		version = "none"
	}
	return version + "\n"
}

// hashOf is the hex SHA-256 of data, the form state.json records.
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// auditEntry is one line of the audit log: what a cycle did and why.
type auditEntry struct {
	Time      time.Time `json:"time"`
	Version   string    `json:"version,omitempty"`
	ETag      string    `json:"etag,omitempty"`
	Unchanged bool      `json:"unchanged,omitempty"`
	Written   []string  `json:"written,omitempty"`
	Error     string    `json:"error,omitempty"`
	Notes     []string  `json:"notes,omitempty"`
}

// audit appends one line to the log. It is best effort: a log that cannot
// be written must not fail a cycle that otherwise succeeded.
func (cfg Config) audit(state State, res Result) {
	path := filepath.Join(cfg.StateDir, AuditFile)
	if info, err := os.Stat(path); err == nil && info.Size() > auditRotateAt {
		_ = os.Rename(path, path+".1")
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	errText := ""
	if res.Err != nil {
		errText = res.Err.Error()
	}
	line, err := json.Marshal(auditEntry{
		Time:      cfg.Now().UTC(),
		Version:   state.Version,
		ETag:      state.ETag,
		Unchanged: res.Unchanged,
		Written:   res.Written,
		Error:     errText,
		Notes:     res.Notes,
	})
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}
