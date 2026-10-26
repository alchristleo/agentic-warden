package sync

import (
	"context"
	"crypto/ed25519"
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

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/cache"
	"github.com/alchristleo/agentic-warden/internal/policy"
	"github.com/alchristleo/agentic-warden/internal/signing"
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

// plannedFile is one change to make to the filesystem, resolved to an
// absolute path and held until every renderer has succeeded: a file to write,
// or — with remove set, and no content — a file that must no longer be there.
// A removal is planned alongside the writes rather than done after them so
// that it obeys the same rule as everything else in a cycle, namely that it
// happens only once every renderer agreed and that failing it fails the
// cycle.
type plannedFile struct {
	path    string
	content []byte
	mode    fs.FileMode
	remove  bool
	// agent names the agent the file belongs to; empty for state-dir files.
	// A failing agent skips its own files and no one else's.
	agent string
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
	// An enrollment made over plain http to a remote host stops syncing
	// rather than send its credential in the clear.
	if err := CheckTransport(machine.Server); err != nil {
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
	var pinned ed25519.PublicKey
	if machine.PublicKey != "" {
		key, err := signing.ParsePublic(machine.PublicKey)
		if err != nil {
			return cfg.fail(state, notes, nil, nil, fmt.Errorf("sync: the pinned key in machine.json: %w", err))
		}
		pinned = key
	}

	// Signing turned off on the control plane, or a machine re-enrolled
	// against one that never signed, leaves a signature and a trust key
	// behind that nothing would ever remove — and aw-policy goes on checking
	// every bundle against them, failing every session forever. They are
	// planned for removal below; the etag goes with them because a cycle the
	// server answers 304 changes nothing on disk, and on a stable fleet that
	// is every cycle.
	var stale []string
	if pinned == nil {
		for _, name := range []string{SignatureFile, TrustFile} {
			path := filepath.Join(cfg.StateDir, name)
			if _, err := os.Stat(path); err == nil {
				stale = append(stale, path)
				notes = append(notes, "no signing key is pinned; removing "+path)
			}
		}
		if len(stale) > 0 {
			etag = ""
		}
	}

	fetched, err := client.Fetch(ctx, machine.Credential, etag, pinned)
	if err != nil {
		return cfg.fail(state, notes, nil, nil, err)
	}
	if fetched.Unchanged {
		// A rotation has to be able to finish on a fleet whose policy is
		// stable, and there every cycle ends here. The signature and the
		// trust file on disk are deliberately left alone: they describe the
		// bundle this machine already holds, which the outgoing key signed,
		// and rewriting either now would break the very check they exist for.
		// The first changed bundle rewrites both under the new key.
		var repinned []string
		if fetched.Rollover != nil {
			repinned = cfg.repin(machine, *fetched.Rollover)
		}
		// Files recorded for an agent that is no longer synced would
		// otherwise stay forever on a fleet whose policy never changes.
		keepRoots := []string{cfg.StateDir}
		for _, name := range machine.Agents {
			if _, removed := policy.RemovedAgents[name]; removed {
				continue
			}
			if root, err := cfg.root(name); err == nil {
				keepRoots = append(keepRoots, root)
			}
		}
		keep := func(path string) bool { return underAny(keepRoots, path) }
		removed, pruneNotes := prune(state.Files, keep)
		state.Files = keptFiles(state.Files, keep)
		state.Error = ""
		state.SyncedAt = cfg.Now().UTC()
		res := Result{Unchanged: true, Version: state.Version, Written: make([]string, 0), Notes: state.Notes}
		if len(repinned)+len(removed)+len(pruneNotes) > 0 {
			res.Notes = append(append(append([]string{}, repinned...), removedNotes(removed)...), pruneNotes...)
			res.Notes = append(res.Notes, state.Notes...)
		}
		if err := SaveState(cfg.StateDir, state); err != nil {
			res.Err = err
		}
		cfg.audit(state, res)
		return res
	}

	// A pinned machine never goes back to an older policy: the revision time
	// is signed with the bundle, so a replay of an old signed bundle is
	// caught here, before anything is rendered or written.
	if pinned != nil && fetched.Bundle.RevisedAt < state.RevisedAt {
		return cfg.fail(state, notes, nil, nil, fmt.Errorf("policy revision time %s is older than last rendered %s; apply the policy again on the control plane",
			revisionTime(fetched.Bundle.RevisedAt), revisionTime(state.RevisedAt)))
	}

	// Every agent is rendered before any file is written. An agent that
	// cannot be rendered is recorded and skipped, so it never blocks the
	// others, and the cycle still reports it as failed below.
	var planned []plannedFile
	failed := map[string]error{}
	var failedOrder []string
	markFailed := func(name string, err error) {
		if _, dup := failed[name]; !dup {
			failedOrder = append(failedOrder, name)
		}
		failed[name] = err
	}
	for _, name := range machine.Agents {
		if hint, removed := policy.RemovedAgents[name]; removed {
			notes = append(notes, name+": "+hint)
			continue
		}
		agentFiles, agentNotes, err := cfg.renderAgent(name, fetched.Bundle)
		if err != nil {
			markFailed(name, err)
			continue
		}
		notes = append(notes, agentNotes...)
		planned = append(planned, agentFiles...)
	}

	// The signature covers the bytes the server sent, so those bytes go to
	// disk unchanged. Re-encoding here would produce a file no verifier
	// could check.
	planned = append(planned, plannedFile{path: filepath.Join(cfg.StateDir, BundleFile), content: fetched.Raw, mode: 0o644})
	for _, path := range stale {
		planned = append(planned, plannedFile{path: path, remove: true})
	}
	if fetched.Signature != "" && pinned != nil {
		verified := pinned
		if fetched.Rollover != nil {
			verified = fetched.Rollover.PublicKey
		}
		planned = append(planned,
			plannedFile{path: filepath.Join(cfg.StateDir, SignatureFile), content: []byte(signing.SignatureLine(fetched.Format, signing.KeyID(verified), fetched.Signature)), mode: 0o644},
			plannedFile{path: filepath.Join(cfg.StateDir, TrustFile), content: []byte(signing.FormatPublic(verified) + "\n"), mode: 0o644},
		)
	}

	version := fetched.Bundle.Version
	written := make([]string, 0, len(planned))
	files := make(map[string]string, len(planned))
	for _, p := range planned {
		if p.remove {
			// A file that is already gone is the state this asks for, so
			// only a real failure — a directory that cannot be written, a
			// file held open — stops the cycle, exactly as a write would.
			// Nothing is recorded in written or files: what the cycle leaves
			// behind is what state.json describes, and this is not there.
			if err := os.Remove(p.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return cfg.fail(state, notes, files, written, fmt.Errorf("sync: removing %s: %w", p.path, err))
			}
			continue
		}
		if _, bad := failed[p.agent]; p.agent != "" && bad {
			continue
		}
		if err := cache.ReplaceMode(p.path, p.content, p.mode); err != nil {
			if p.agent != "" {
				markFailed(p.agent, fmt.Errorf("sync: writing %s: %w", p.path, err))
				continue
			}
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
				if p.agent != "" {
					markFailed(p.agent, fmt.Errorf("sync: writing %s: %w", p.path+revisionSuffix, err))
					continue
				}
				return cfg.fail(state, notes, files, written, fmt.Errorf("sync: writing %s: %w", p.path+revisionSuffix, err))
			}
		}
	}

	if fetched.Rollover != nil {
		// The pin moves only after the cycle's files are on disk: a machine
		// that crashed mid-cycle repins on the next one, from a statement
		// the old key still signs.
		notes = append(notes, cfg.repin(machine, *fetched.Rollover)...)
	}

	// A failed agent keeps the files it already had: they are still its policy
	// in force, so they are recorded and never pruned this cycle.
	var failedRoots, failedMsgs []string
	for _, name := range failedOrder {
		failedMsgs = append(failedMsgs, fmt.Sprintf("agent %s: %v", name, failed[name]))
		if root, err := cfg.root(name); err == nil {
			failedRoots = append(failedRoots, root)
		}
	}
	plannedPaths := make(map[string]bool, len(planned))
	for _, p := range planned {
		plannedPaths[p.path] = true
	}
	keepRoots := append(failedRoots, cfg.StateDir)
	keep := func(path string) bool { return plannedPaths[path] || underAny(keepRoots, path) }
	removed, pruneNotes := prune(state.Files, keep)
	notes = append(append(notes, removedNotes(removed)...), pruneNotes...)
	for path, sum := range state.Files {
		if _, ok := files[path]; !ok && underAny(failedRoots, path) {
			files[path] = sum
		}
	}

	if notes == nil {
		notes = make([]string, 0)
	}
	cycleErr, etagToSave := "", fetched.ETag
	if len(failedMsgs) > 0 {
		// No etag: the next cycle must refetch to retry the failed agents
		// instead of being answered 304.
		cycleErr, etagToSave = strings.Join(failedMsgs, "; "), ""
	}
	state = State{
		Server:    machine.Server,
		MachineID: machine.MachineID,
		Agents:    machine.Agents,
		ETag:      etagToSave,
		Version:   version,
		RevisedAt: max(state.RevisedAt, fetched.Bundle.RevisedAt),
		SyncedAt:  cfg.Now().UTC(),
		Files:     files,
		Notes:     notes,
		Error:     cycleErr,
	}
	res := Result{Version: version, Written: written, Notes: notes}
	if cycleErr != "" {
		res.Err = errors.New("sync: " + cycleErr)
	}
	if err := SaveState(cfg.StateDir, state); err != nil {
		res.Err = err
	}
	cfg.audit(state, res)
	return res
}

// renderAgent renders one agent and resolves its files under its root. Any
// error is that agent's alone.
func (cfg Config) renderAgent(name string, bundle *policy.Bundle) ([]plannedFile, []string, error) {
	adapter, err := cfg.Registry.Lookup(name)
	if err != nil {
		return nil, nil, err
	}
	renderer, ok := adapter.(agent.Renderer)
	if !ok {
		return nil, nil, fmt.Errorf("sync: agent %q has no renderer", name)
	}
	root, err := cfg.root(name)
	if err != nil {
		return nil, nil, err
	}
	rendering, err := renderer.Render(bundle)
	if err != nil {
		return nil, nil, fmt.Errorf("sync: rendering %s: %w", name, err)
	}
	var files []plannedFile
	for _, f := range rendering.Files {
		if filepath.IsAbs(f.Path) || !filepath.IsLocal(f.Path) {
			return nil, nil, fmt.Errorf("sync: renderer %s returned an unsafe path %q", name, f.Path)
		}
		files = append(files, plannedFile{path: filepath.Join(root, f.Path), content: f.Content, mode: f.Mode, agent: name})
	}
	return files, rendering.Notes, nil
}

// underAny reports whether path is inside one of roots, by filepath.Rel and
// IsLocal rather than a string prefix.
func underAny(roots []string, path string) bool {
	for _, root := range roots {
		if rel, err := filepath.Rel(root, path); err == nil && filepath.IsLocal(rel) {
			return true
		}
	}
	return false
}

// keptFiles is files restricted to the paths keep accepts.
func keptFiles(files map[string]string, keep func(string) bool) map[string]string {
	out := make(map[string]string, len(files))
	for path, sum := range files {
		if keep(path) {
			out[path] = sum
		}
	}
	return out
}

func removedNotes(removed []string) []string {
	var notes []string
	for _, path := range removed {
		notes = append(notes, "removed "+path+": agent no longer synced")
	}
	return notes
}

// prune removes the files aw-sync recorded in prev that keep does not claim.
// It is the only place aw-sync deletes a rendered file, so it is strict: only
// an absolute, clean path, only a regular file (Lstat, so a symlink is never
// followed), only content that still hashes to what was written. Anything
// else stays on disk with a note. The .aw-revision sidecar goes only with its
// main file. A candidate that is gone is dropped silently. The caller drops
// every candidate from state.Files whatever happened to it.
func prune(prev map[string]string, keep func(path string) bool) (removed []string, notes []string) {
	paths := make([]string, 0, len(prev))
	for path := range prev {
		if !keep(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			notes = append(notes, fmt.Sprintf("not removing %q: not an absolute clean path", path))
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("not removing %s: %v", path, err))
			continue
		}
		if !info.Mode().IsRegular() {
			notes = append(notes, fmt.Sprintf("not removing %s: not a regular file", path))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			notes = append(notes, fmt.Sprintf("not removing %s: %v", path, err))
			continue
		}
		if hashOf(data) != prev[path] {
			notes = append(notes, fmt.Sprintf("not removing %s: changed since aw-sync wrote it", path))
			continue
		}
		if err := os.Remove(path); err != nil {
			notes = append(notes, fmt.Sprintf("not removing %s: %v", path, err))
			continue
		}
		removed = append(removed, path)
		if side, err := os.Lstat(path + revisionSuffix); err == nil && side.Mode().IsRegular() {
			if err := os.Remove(path + revisionSuffix); err != nil {
				notes = append(notes, fmt.Sprintf("not removing %s: %v", path+revisionSuffix, err))
			}
		}
	}
	return removed, notes
}

// repin moves this machine's pinned key to the one a rollover the fetch
// already verified announced, and returns the notes the cycle should carry.
// A save that fails is a note rather than a failed cycle: what is on disk is
// consistent either way, and the next cycle repins from a statement the
// outgoing key still signs.
func (cfg Config) repin(machine Machine, rollover signing.Rollover) []string {
	machine.PublicKey = signing.FormatPublic(rollover.PublicKey)
	machine.KeyID = rollover.KeyID
	if err := SaveMachine(cfg.StateDir, machine); err != nil {
		return []string{"the new signing key could not be pinned: " + err.Error()}
	}
	return nil
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

// revisionTime renders a revision time for the refusal message; zero is a
// bundle from a control plane that sent none.
func revisionTime(ms int64) string {
	if ms == 0 {
		return "(none)"
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
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
	if err := EnsureStateDir(cfg.StateDir); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	// OpenFile's mode is umask-filtered on creation like any other mode, so
	// under a strict umask the log can come out 0600 despite the 0644 asked
	// for above. The state directory is aw-sync's own, so forcing the mode
	// here is always safe, and Chmod is unconditional rather than only on
	// creation because a log an older, unfixed binary already narrowed to
	// 0600 needs the same repair EnsureStateDir gives the directory itself.
	_ = os.Chmod(path, 0o644)
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
