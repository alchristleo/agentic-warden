package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

	machine, err := LoadMachine(cfg.StateDir)
	if err != nil {
		return Result{Err: err}
	}
	state, err := LoadState(cfg.StateDir)
	var notes []string
	if err != nil {
		// A corrupt record must not stop the cycle; it is rebuilt below.
		notes = append(notes, "previous state discarded: "+err.Error())
		state = State{}
	}

	client := &Client{Server: machine.Server, HTTP: cfg.HTTP}
	fetched, err := client.Fetch(ctx, machine.Credential, state.ETag)
	if err != nil {
		return cfg.fail(state, notes, err)
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
			return cfg.fail(state, notes, err)
		}
		renderer, ok := adapter.(agent.Renderer)
		if !ok {
			return cfg.fail(state, notes, fmt.Errorf("sync: agent %q has no renderer", name))
		}
		root, err := cfg.root(name)
		if err != nil {
			return cfg.fail(state, notes, err)
		}
		rendering, err := renderer.Render(fetched.Bundle)
		if err != nil {
			return cfg.fail(state, notes, fmt.Errorf("sync: rendering %s: %w", name, err))
		}
		notes = append(notes, rendering.Notes...)
		for _, f := range rendering.Files {
			planned = append(planned, plannedFile{path: filepath.Join(root, f.Path), content: f.Content, mode: f.Mode})
		}
	}

	version := fetched.Bundle.Version
	written := make([]string, 0, len(planned))
	files := make(map[string]string, len(planned))
	for _, p := range planned {
		if err := cache.ReplaceMode(p.path, p.content, p.mode); err != nil {
			// Each write is atomic, so what landed is whole; the next cycle
			// rewrites the rest. Report which file stopped this one.
			return cfg.fail(state, notes, fmt.Errorf("sync: writing %s: %w", p.path, err))
		}
		written = append(written, p.path)
		files[p.path] = hashOf(p.content)
		if strings.HasSuffix(p.path, ".json") {
			if err := cache.ReplaceMode(p.path+revisionSuffix, []byte(revisionLine(version)), 0o644); err != nil {
				return cfg.fail(state, notes, fmt.Errorf("sync: writing %s: %w", p.path+revisionSuffix, err))
			}
		}
	}

	if notes == nil {
		notes = make([]string, 0)
	}
	state = State{
		ETag:     fetched.ETag,
		Version:  version,
		SyncedAt: cfg.Now().UTC(),
		Files:    files,
		Notes:    notes,
	}
	res := Result{Version: version, Written: written, Notes: notes}
	if err := SaveState(cfg.StateDir, state); err != nil {
		res.Err = err
	}
	cfg.audit(state, res)
	return res
}

// fail records err in the state without touching the last good etag,
// version or file list, so `status` still describes what is on disk.
func (cfg Config) fail(state State, notes []string, err error) Result {
	state.Error = err.Error()
	if len(notes) > 0 {
		state.Notes = append(append([]string{}, state.Notes...), notes...)
	}
	res := Result{Version: state.Version, Written: make([]string, 0), Notes: state.Notes, Err: err}
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
