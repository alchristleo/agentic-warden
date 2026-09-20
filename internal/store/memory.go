package store

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/acme/agent-wrapper/internal/model"
)

// Memory is an in-memory Store. It backs tests and a single-node evaluation
// deployment; state does not survive a restart.
type Memory struct {
	mu        sync.RWMutex
	revisions []model.Revision
	versions  map[string]bool
	nextSeq   int64
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{versions: make(map[string]bool)}
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
