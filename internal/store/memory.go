package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// Memory is an in-memory Store. It backs tests and a single-node evaluation
// deployment; state does not survive a restart.
type Memory struct {
	mu        sync.RWMutex
	revisions []model.Revision
	versions  map[string]bool
	nextSeq   int64
	tokens    map[string]model.EnrollmentToken // by hash
	machines  map[string]model.Machine         // by id
	byHash    map[string]string                // credential hash -> machine id
	order     []string                         // machine ids in enrollment order
	snapshots []model.GroupSnapshot
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		versions: make(map[string]bool),
		tokens:   make(map[string]model.EnrollmentToken),
		machines: make(map[string]model.Machine),
		byHash:   make(map[string]string),
	}
}

// PutRuleSet stores a revision.
func (m *Memory) PutRuleSet(_ context.Context, r model.Revision) error {
	if r.Version == "" {
		return fmt.Errorf("store: revision has no version: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.versions[r.Version] {
		return fmt.Errorf("store: revision %q already exists: %w", r.Version, model.ErrConflict)
	}
	m.nextSeq++
	r.Seq = m.nextSeq
	m.versions[r.Version] = true
	m.revisions = append(m.revisions, r)
	return nil
}

// CurrentRuleSet returns the newest revision.
func (m *Memory) CurrentRuleSet(ctx context.Context) (model.Revision, error) {
	newest, err := m.Revisions(ctx, 1)
	if err != nil {
		return model.Revision{}, err
	}
	if len(newest) == 0 {
		return model.Revision{}, fmt.Errorf("store: no policy has been applied: %w", model.ErrNotFound)
	}
	return newest[0], nil
}

// Revisions returns up to limit revisions, newest first.
func (m *Memory) Revisions(_ context.Context, limit int) ([]model.Revision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ordered := make([]model.Revision, len(m.revisions))
	copy(ordered, m.revisions)
	// Sequence is the ordering, not the timestamp: two revisions applied in
	// the same clock tick still have a defined newest.
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Seq > ordered[j].Seq
	})

	if limit >= 0 && limit < len(ordered) {
		ordered = ordered[:limit]
	}
	return ordered, nil
}

// PutEnrollmentToken stores a token.
func (m *Memory) PutEnrollmentToken(_ context.Context, t model.EnrollmentToken) error {
	if t.Hash == "" || t.User == "" {
		return fmt.Errorf("store: enrollment token needs a hash and a user: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tokens[t.Hash]; exists {
		return fmt.Errorf("store: enrollment token already exists: %w", model.ErrConflict)
	}
	m.tokens[t.Hash] = t
	return nil
}

// ConsumeEnrollmentToken marks a token used, atomically.
func (m *Memory) ConsumeEnrollmentToken(_ context.Context, hash string, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[hash]
	if !ok {
		return "", fmt.Errorf("store: unknown enrollment token: %w", model.ErrNotFound)
	}
	if !t.UsedAt.IsZero() {
		return "", fmt.Errorf("store: enrollment token already used: %w", model.ErrConflict)
	}
	if !now.Before(t.ExpiresAt) {
		return "", fmt.Errorf("store: enrollment token expired: %w", model.ErrConflict)
	}
	t.UsedAt = now
	m.tokens[hash] = t
	return t.User, nil
}

// PutMachine stores an enrolled machine.
func (m *Memory) PutMachine(_ context.Context, mc model.Machine) error {
	if mc.ID == "" || mc.CredentialHash == "" {
		return fmt.Errorf("store: machine needs an id and a credential hash: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.machines[mc.ID]; exists {
		return fmt.Errorf("store: machine %q already exists: %w", mc.ID, model.ErrConflict)
	}
	if _, exists := m.byHash[mc.CredentialHash]; exists {
		return fmt.Errorf("store: credential already issued: %w", model.ErrConflict)
	}
	m.machines[mc.ID] = mc
	m.byHash[mc.CredentialHash] = mc.ID
	m.order = append(m.order, mc.ID)
	return nil
}

// MachineByCredential finds a machine by credential hash.
func (m *Memory) MachineByCredential(_ context.Context, hash string) (model.Machine, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byHash[hash]
	if !ok {
		return model.Machine{}, fmt.Errorf("store: no machine holds this credential: %w", model.ErrNotFound)
	}
	return m.machines[id], nil
}

// TouchMachine records a fetch. keyID is stored exactly as given, empty
// included, so a machine that stops presenting a key stops reporting one.
func (m *Memory) TouchMachine(_ context.Context, id string, seenAt time.Time, bundleVersion, keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mc, ok := m.machines[id]
	if !ok {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	mc.LastSeenAt = seenAt
	mc.LastBundleVersion = bundleVersion
	mc.LastKeyID = keyID
	m.machines[id] = mc
	return nil
}

// ListMachines returns machines in enrollment order, hashes cleared.
func (m *Memory) ListMachines(_ context.Context) ([]model.Machine, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Machine, 0, len(m.order))
	for _, id := range m.order {
		mc := m.machines[id]
		mc.CredentialHash = ""
		out = append(out, mc)
	}
	return out, nil
}

// DeleteMachine revokes a machine.
func (m *Memory) DeleteMachine(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mc, ok := m.machines[id]
	if !ok {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	delete(m.machines, id)
	delete(m.byHash, mc.CredentialHash)
	for i, existing := range m.order {
		if existing == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}

// PutGroupSnapshot appends a snapshot; the newest is current.
func (m *Memory) PutGroupSnapshot(_ context.Context, s model.GroupSnapshot) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if s.SyncedAt.IsZero() {
		s.SyncedAt = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSeq++
	s.Seq = m.nextSeq
	m.snapshots = append(m.snapshots, s)
	return nil
}

// CurrentGroupSnapshot returns the newest snapshot.
func (m *Memory) CurrentGroupSnapshot(_ context.Context) (model.GroupSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.snapshots) == 0 {
		return model.GroupSnapshot{}, fmt.Errorf("store: no group snapshot has been posted: %w", model.ErrNotFound)
	}
	return m.snapshots[len(m.snapshots)-1], nil
}
