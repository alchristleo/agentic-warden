package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/acme/agent-wrapper/internal/cache"
)

const (
	// MachineFile holds the enrollment. It carries the credential, so it is
	// private to root.
	MachineFile = "machine.json"
	// StateFile holds what the last cycle left behind, readable by everyone
	// because `aw doctor` reports from it as the developer.
	StateFile = "state.json"
	// AuditFile is the append-only log of every cycle.
	AuditFile = "aw-sync.log"
)

// ErrNotEnrolled means there is no machine.json: `aw-sync enroll` has not
// been run here.
var ErrNotEnrolled = errors.New("not enrolled")

// Machine is this machine's enrollment, written once by `aw-sync enroll` and
// read by every cycle.
type Machine struct {
	// Server is the control plane's base URL.
	Server string `json:"server"`
	// MachineID is what the control plane calls this machine.
	MachineID string `json:"machineId"`
	// Credential is the bearer credential shown once at enrollment.
	Credential string `json:"credential"`
	// Agents names the adapters whose files this machine renders. A machine
	// without Codex installed lists only what it runs, so no directory is
	// created for an agent that is not there.
	Agents []string `json:"agents"`
}

// State is what the last cycle left behind.
type State struct {
	// ETag is the bundle's entity tag, sent back as If-None-Match.
	ETag string `json:"etag,omitempty"`
	// Version is the policy revision last rendered.
	Version string `json:"version,omitempty"`
	// SyncedAt is when the last successful cycle finished, including one the
	// server answered with 304.
	SyncedAt time.Time `json:"syncedAt"`
	// Files maps every rendered file's absolute path to the SHA-256 of what
	// was written, so `status` can tell drift from a fresh render.
	Files map[string]string `json:"files"`
	// Error is why the last cycle failed, or empty.
	Error string `json:"error,omitempty"`
	// Notes are what the renderers reported, such as rules an agent cannot
	// enforce.
	Notes []string `json:"notes"`
}

// LoadMachine reads the enrollment from dir. A missing file is
// ErrNotEnrolled; an incomplete one is a plain error, because a machine that
// half-enrolled must not be mistaken for one that never did.
func LoadMachine(dir string) (Machine, error) {
	raw, err := os.ReadFile(filepath.Join(dir, MachineFile))
	if errors.Is(err, os.ErrNotExist) {
		return Machine{}, fmt.Errorf("sync: %w: no %s in %s; run `aw-sync enroll`", ErrNotEnrolled, MachineFile, dir)
	}
	if err != nil {
		return Machine{}, fmt.Errorf("sync: reading the enrollment: %w", err)
	}
	var m Machine
	if err := json.Unmarshal(raw, &m); err != nil {
		return Machine{}, fmt.Errorf("sync: parsing %s: %w", MachineFile, err)
	}
	switch {
	case m.Server == "":
		return Machine{}, fmt.Errorf("sync: %s has no server", MachineFile)
	case m.MachineID == "":
		return Machine{}, fmt.Errorf("sync: %s has no machineId", MachineFile)
	case m.Credential == "":
		return Machine{}, fmt.Errorf("sync: %s has no credential", MachineFile)
	}
	if m.Agents == nil {
		m.Agents = make([]string, 0)
	}
	return m, nil
}

// SaveMachine writes the enrollment, private to the owner.
func SaveMachine(dir string, m Machine) error {
	if m.Agents == nil {
		m.Agents = make([]string, 0)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("sync: encoding the enrollment: %w", err)
	}
	if err := cache.ReplaceMode(filepath.Join(dir, MachineFile), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// LoadState reads the last cycle's record. No file means no cycle has run,
// which is the zero State and not an error; a file that cannot be parsed is
// an error, so a caller can decide whether to start over.
func LoadState(dir string) (State, error) {
	raw, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("sync: reading %s: %w", StateFile, err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("sync: parsing %s: %w", StateFile, err)
	}
	return s, nil
}

// SaveState writes the cycle's record, readable by everyone. Empty
// collections are written as empty, not null, so a reader in any language
// sees a map and a list.
func SaveState(dir string, s State) error {
	if s.Files == nil {
		s.Files = make(map[string]string)
	}
	if s.Notes == nil {
		s.Notes = make([]string, 0)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("sync: encoding the state: %w", err)
	}
	if err := cache.ReplaceMode(filepath.Join(dir, StateFile), append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}
