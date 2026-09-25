// Package session keeps console sessions on the server. The cookie holds a
// random token; the store holds its hash, so a database read yields nothing
// a browser could present.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

var (
	ErrNoSession = errors.New("session: no such session")
	ErrExpired   = errors.New("session: expired")
)

// touchEvery bounds how often a busy session writes last_seen_at.
const touchEvery = time.Minute

// Manager creates and checks sessions.
type Manager struct {
	Store    store.ConsoleStore
	Now      func() time.Time
	Absolute time.Duration
	Idle     time.Duration
}

// NewManager uses the spec's lifetimes: 8 h absolute, 1 h idle.
func NewManager(s store.ConsoleStore) *Manager {
	return &Manager{Store: s, Now: time.Now, Absolute: 8 * time.Hour, Idle: time.Hour}
}

// Hash is what the store keys a session by.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create starts a session for user and returns the cookie token.
func (m *Manager) Create(ctx context.Context, user string) (string, model.ConsoleSession, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", model.ConsoleSession{}, fmt.Errorf("session: random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	now := m.Now()
	s := model.ConsoleSession{TokenHash: Hash(token), User: user, CreatedAt: now, LastSeenAt: now}
	if err := m.Store.CreateSession(ctx, s); err != nil {
		return "", model.ConsoleSession{}, err
	}
	return token, s, nil
}

// ExpiresAt is the earlier of the absolute and idle bounds.
func (m *Manager) ExpiresAt(s model.ConsoleSession) time.Time {
	abs := s.CreatedAt.Add(m.Absolute)
	idle := s.LastSeenAt.Add(m.Idle)
	if idle.Before(abs) {
		return idle
	}
	return abs
}

// Lookup returns the live session for token, deleting it when expired.
func (m *Manager) Lookup(ctx context.Context, token string) (model.ConsoleSession, error) {
	hash := Hash(token)
	s, err := m.Store.SessionByHash(ctx, hash)
	if errors.Is(err, model.ErrNotFound) {
		return model.ConsoleSession{}, ErrNoSession
	}
	if err != nil {
		return model.ConsoleSession{}, err
	}
	now := m.Now()
	if now.After(m.ExpiresAt(s)) {
		if err := m.Store.DeleteSession(ctx, hash); err != nil {
			return model.ConsoleSession{}, err
		}
		return model.ConsoleSession{}, ErrExpired
	}
	if now.Sub(s.LastSeenAt) >= touchEvery {
		if err := m.Store.TouchSession(ctx, hash, now); err != nil && !errors.Is(err, model.ErrNotFound) {
			return model.ConsoleSession{}, err
		}
		s.LastSeenAt = now
	}
	return s, nil
}

// Delete ends one session.
func (m *Manager) Delete(ctx context.Context, token string) error {
	return m.Store.DeleteSession(ctx, Hash(token))
}

// DeleteUser ends every session of user.
func (m *Manager) DeleteUser(ctx context.Context, user string) error {
	return m.Store.DeleteSessionsFor(ctx, user)
}

// Sweep deletes every expired session.
func (m *Manager) Sweep(ctx context.Context) (int, error) {
	now := m.Now()
	return m.Store.DeleteExpiredSessions(ctx, now.Add(-m.Absolute), now.Add(-m.Idle))
}
