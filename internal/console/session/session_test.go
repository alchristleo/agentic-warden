package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/store"
)

func manager(now *time.Time) (*session.Manager, *store.Memory) {
	s := store.NewMemory()
	m := session.NewManager(s)
	m.Now = func() time.Time { return *now }
	return m, s
}

var t0 = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

func TestCreateStoresOnlyTheHash(t *testing.T) {
	now := t0
	m, s := manager(&now)
	token, sess, err := m.Create(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 { // 32 bytes, base64url, no padding
		t.Fatalf("token length %d", len(token))
	}
	if sess.TokenHash == token || sess.TokenHash != session.Hash(token) {
		t.Fatal("stored hash is the token or not its hash")
	}
	if _, err := s.SessionByHash(context.Background(), token); err == nil {
		t.Fatal("session findable by the raw token")
	}
}

func TestLookupBoundaries(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	ctx := context.Background()
	token, _, _ := m.Create(ctx, "alice")

	// Active every 59 minutes: idle never trips; absolute does at 8 h.
	for i := 1; i <= 8; i++ {
		now = t0.Add(time.Duration(i) * 59 * time.Minute)
		if _, err := m.Lookup(ctx, token); err != nil {
			t.Fatalf("at %v: %v", now.Sub(t0), err)
		}
	}
	now = t0.Add(8*time.Hour + time.Second)
	if _, err := m.Lookup(ctx, token); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("past absolute = %v, want ErrExpired", err)
	}
	if _, err := m.Lookup(ctx, token); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("expired session not deleted: %v", err)
	}
}

func TestIdleExpiry(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	ctx := context.Background()
	token, _, _ := m.Create(ctx, "alice")
	now = t0.Add(time.Hour)
	if _, err := m.Lookup(ctx, token); err != nil {
		t.Fatalf("exactly 1 h idle is still valid: %v", err)
	}
	now = now.Add(time.Hour + time.Second)
	if _, err := m.Lookup(ctx, token); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("idle = %v, want ErrExpired", err)
	}
}

func TestTouchIsThrottledToOncePerMinute(t *testing.T) {
	now := t0
	m, s := manager(&now)
	ctx := context.Background()
	token, sess, _ := m.Create(ctx, "alice")
	now = t0.Add(30 * time.Second)
	_, _ = m.Lookup(ctx, token)
	got, _ := s.SessionByHash(ctx, sess.TokenHash)
	if !got.LastSeenAt.Equal(t0) {
		t.Fatalf("touched after 30 s: %v", got.LastSeenAt)
	}
	now = t0.Add(61 * time.Second)
	_, _ = m.Lookup(ctx, token)
	got, _ = s.SessionByHash(ctx, sess.TokenHash)
	if !got.LastSeenAt.Equal(now) {
		t.Fatalf("not touched after 61 s: %v", got.LastSeenAt)
	}
}

func TestExpiresAtIsTheEarlierBound(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	_, sess, _ := m.Create(context.Background(), "alice")
	if got := m.ExpiresAt(sess); !got.Equal(t0.Add(time.Hour)) {
		t.Fatalf("fresh session expires %v, want idle bound", got)
	}
	sess.LastSeenAt = t0.Add(7*time.Hour + 30*time.Minute)
	if got := m.ExpiresAt(sess); !got.Equal(t0.Add(8 * time.Hour)) {
		t.Fatalf("late session expires %v, want absolute bound", got)
	}
}

func TestUnknownTokenIsNoSession(t *testing.T) {
	now := t0
	m, _ := manager(&now)
	if _, err := m.Lookup(context.Background(), "garbage"); !errors.Is(err, session.ErrNoSession) {
		t.Fatalf("err = %v", err)
	}
}
