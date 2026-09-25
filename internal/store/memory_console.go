package store

import (
	"context"
	"fmt"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// appendAudit records an event. Callers hold m.mu for writing and have
// validated the event.
func (m *Memory) appendAudit(e model.AuditEvent) {
	m.nextAudit++
	e.ID = m.nextAudit
	if e.At.IsZero() {
		e.At = time.Now()
	}
	m.audit = append(m.audit, e)
}

// CreateSession stores a session.
func (m *Memory) CreateSession(_ context.Context, s model.ConsoleSession) error {
	if s.TokenHash == "" || s.User == "" {
		return fmt.Errorf("store: session needs a token hash and a user: %w", model.ErrBadInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.TokenHash]; ok {
		return fmt.Errorf("store: session exists: %w", model.ErrConflict)
	}
	m.sessions[s.TokenHash] = s
	return nil
}

// SessionByHash returns a session.
func (m *Memory) SessionByHash(_ context.Context, hash string) (model.ConsoleSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[hash]
	if !ok {
		return model.ConsoleSession{}, fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	return s, nil
}

// TouchSession records activity on a session.
func (m *Memory) TouchSession(_ context.Context, hash string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[hash]
	if !ok {
		return fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	s.LastSeenAt = at
	m.sessions[hash] = s
	return nil
}

// DeleteSession removes a session if it exists.
func (m *Memory) DeleteSession(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, hash)
	return nil
}

// DeleteSessionsFor removes every session of a user.
func (m *Memory) DeleteSessionsFor(_ context.Context, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, s := range m.sessions {
		if s.User == user {
			delete(m.sessions, h)
		}
	}
	return nil
}

// DeleteExpiredSessions sweeps sessions past either bound.
func (m *Memory) DeleteExpiredSessions(_ context.Context, createdBefore, seenBefore time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for h, s := range m.sessions {
		if s.CreatedAt.Before(createdBefore) || s.LastSeenAt.Before(seenBefore) {
			delete(m.sessions, h)
			n++
		}
	}
	return n, nil
}

// RecordAudit stores a standalone event.
func (m *Memory) RecordAudit(_ context.Context, e model.AuditEvent) error {
	if err := e.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendAudit(e)
	return nil
}

// AuditEvents lists events newest first.
func (m *Memory) AuditEvents(_ context.Context, limit int, before int64) ([]model.AuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.AuditEvent, 0)
	for i := len(m.audit) - 1; i >= 0 && len(out) < limit; i-- {
		if before > 0 && m.audit[i].ID >= before {
			continue
		}
		out = append(out, m.audit[i])
	}
	return out, nil
}
