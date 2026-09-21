package sync

import (
	"errors"
	"os"
	"sort"
	"time"
)

// FileStatus is one rendered file compared with what the last cycle wrote.
type FileStatus struct {
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
	Enrolled  bool         `json:"enrolled"`
	Server    string       `json:"server,omitempty"`
	MachineID string       `json:"machineId,omitempty"`
	Agents    []string     `json:"agents"`
	SyncedAt  time.Time    `json:"syncedAt"`
	Version   string       `json:"version,omitempty"`
	Error     string       `json:"error,omitempty"`
	Notes     []string     `json:"notes"`
	Files     []FileStatus `json:"files"`
	// Drift is true when any file is not as the last cycle left it. `once`
	// overwrites drift unconditionally; the files are root-owned, and a
	// user who can edit them already has root.
	Drift bool `json:"drift"`
}

// Status reads the state directory and checks every recorded file. It runs
// nothing and touches the network for nothing, so the developer's `aw
// doctor` can call it.
func Status(cfg Config) (Report, error) {
	r := Report{Agents: make([]string, 0), Notes: make([]string, 0), Files: make([]FileStatus, 0)}
	machine, err := LoadMachine(cfg.StateDir)
	switch {
	case err == nil:
		r.Enrolled = true
		r.Server = machine.Server
		r.MachineID = machine.MachineID
		r.Agents = machine.Agents
	case errors.Is(err, ErrNotEnrolled):
	default:
		return r, err
	}

	state, err := LoadState(cfg.StateDir)
	if err != nil {
		return r, err
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
		raw, err := os.ReadFile(path)
		switch {
		case err != nil:
			fs.State = "missing"
		case hashOf(raw) == fs.Expected:
			fs.State = "ok"
			fs.Actual = fs.Expected
		default:
			fs.State = "drift"
			fs.Actual = hashOf(raw)
		}
		if fs.State != "ok" {
			r.Drift = true
		}
		r.Files = append(r.Files, fs)
	}
	return r, nil
}
