package sync

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FileStatus is one rendered file compared with what the last cycle wrote.
type FileStatus struct {
	// Path is the file's absolute path, as recorded in state.json.
	Path string `json:"path"`
	// State is "ok", "drift" (content differs) or "missing".
	State string `json:"state"`
	// Expected is the hash recorded at the last sync.
	Expected string `json:"expected"`
	// Actual is the hash of what is on disk now, when the file exists.
	Actual string `json:"actual,omitempty"`
}

// Report is what `aw-sync status` shows: enrollment, the last cycle, and
// every rendered file's drift.
type Report struct {
	// Enrolled is false when there is no machine.json: every other
	// enrollment field is then empty and this is not an error.
	Enrolled bool `json:"enrolled"`
	// Server is the control plane's base URL, from state.json: machine.json
	// carries the same fact but is 0600, unreadable to the developer
	// running `status`.
	Server string `json:"server,omitempty"`
	// MachineID is what the control plane calls this machine, from
	// state.json.
	MachineID string `json:"machineId,omitempty"`
	// Agents names the adapters this machine renders, from state.json.
	Agents []string `json:"agents"`
	// SyncedAt is when the last successful cycle finished, from state.json.
	SyncedAt time.Time `json:"syncedAt"`
	// Version is the policy revision the last cycle rendered.
	Version string `json:"version,omitempty"`
	// Error is why the last cycle failed, carried over from state.json.
	Error string `json:"error,omitempty"`
	// Notes are what the renderers reported on the last cycle, such as
	// rules an agent cannot enforce.
	Notes []string `json:"notes"`
	// Files is every file the last cycle rendered, each compared against
	// what is on disk now.
	Files []FileStatus `json:"files"`
	// Drift is true when any file is not as the last cycle left it. `once`
	// overwrites drift unconditionally; the files are root-owned, and a
	// user who can edit them already has root.
	Drift bool `json:"drift"`
}

// Status reads the state directory and checks every recorded file. It runs
// nothing and touches the network for nothing, so the developer's `aw
// doctor` can call it.
//
// machine.json is 0600, so a developer running `status` as themselves
// cannot read it. Enrolled is therefore decided by whether machine.json
// exists at all (a Stat, not a read), and the enrollment facts reported
// (Server, MachineID, Agents) come from state.json instead, which every
// cycle keeps in step with the enrollment. LoadMachine is still attempted:
// a malformed-but-readable machine.json is still reported as an error (a
// half-enrolled machine is worth flagging), but a permission error from it
// is not, since the facts this reports do not depend on reading the file.
func Status(cfg Config) (Report, error) {
	r := Report{Agents: make([]string, 0), Notes: make([]string, 0), Files: make([]FileStatus, 0)}

	switch _, err := os.Stat(filepath.Join(cfg.StateDir, MachineFile)); {
	case err == nil:
		r.Enrolled = true
	case errors.Is(err, os.ErrNotExist):
	default:
		return r, err
	}

	switch _, err := LoadMachine(cfg.StateDir); {
	case err == nil, errors.Is(err, ErrNotEnrolled), errors.Is(err, os.ErrPermission):
	default:
		return r, err
	}

	state, err := LoadState(cfg.StateDir)
	if err != nil {
		return r, err
	}
	r.Server = state.Server
	r.MachineID = state.MachineID
	if state.Agents != nil {
		r.Agents = state.Agents
	}
	r.SyncedAt = state.SyncedAt
	r.Version = state.Version
	r.Error = state.Error
	if state.Notes != nil {
		r.Notes = state.Notes
	}

	paths := make([]string, 0, len(state.Files))
	for path := range state.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fs := FileStatus{Path: path, Expected: state.Files[path]}
		fs.State, fs.Actual = fileState(path, fs.Expected)
		if fs.State != "ok" {
			r.Drift = true
		}
		r.Files = append(r.Files, fs)
	}
	return r, nil
}

// fileState compares the file at path against expected, the hash recorded
// at the last sync. It reports "ok", "drift" (content differs) or
// "missing", and the hash actually on disk when the file could be read.
// Status uses it to report per-file drift; Run uses it before every fetch
// to detect drift that a conditional request would otherwise miss, since a
// 304 answer means "the bundle is unchanged", not "the files are".
func fileState(path, expected string) (state, actual string) {
	raw, err := os.ReadFile(path)
	switch {
	case err != nil:
		// Any read error, not only os.ErrNotExist, is reported as
		// "missing": a file this process cannot read is, for enforcement
		// purposes, not there, and the next `once` rewrites it regardless
		// of why the read failed.
		return "missing", ""
	case hashOf(raw) == expected:
		return "ok", expected
	default:
		return "drift", hashOf(raw)
	}
}
