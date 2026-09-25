package handler

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/acme/agent-wrapper/internal/signing"
)

// countingSigner wraps a seed and counts Sign calls; fail makes the next
// call fail once.
type countingSigner struct {
	signing.Signer
	calls atomic.Int32
	fail  atomic.Bool
}

func (c *countingSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	c.calls.Add(1)
	if c.fail.CompareAndSwap(true, false) {
		return nil, errors.New("ThrottlingException: rate exceeded")
	}
	return c.Signer.Sign(ctx, msg)
}

func newCounting(t *testing.T) *countingSigner {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return &countingSigner{Signer: signing.NewSeedSigner(key)}
}

func TestSigCacheHitsOnTheSameMessage(t *testing.T) {
	c := newSigCache(1024)
	s := newCounting(t)
	for i := 0; i < 3; i++ {
		if _, err := c.sign(context.Background(), s, []byte("same")); err != nil {
			t.Fatal(err)
		}
	}
	if s.calls.Load() != 1 {
		t.Fatalf("Sign called %d times, want 1", s.calls.Load())
	}
}

// Review Focus 5.
func TestSigningFailureIsNotCached(t *testing.T) {
	c := newSigCache(1024)
	s := newCounting(t)
	s.fail.Store(true)
	if _, err := c.sign(context.Background(), s, []byte("m")); err == nil {
		t.Fatal("want failure")
	}
	sig, err := c.sign(context.Background(), s, []byte("m"))
	if err != nil || !ed25519.Verify(s.Public(), []byte("m"), sig) {
		t.Fatalf("retry = %v", err)
	}
}

func TestSigCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newSigCache(2)
	s := newCounting(t)
	ctx := context.Background()
	_, _ = c.sign(ctx, s, []byte("a"))
	_, _ = c.sign(ctx, s, []byte("b"))
	_, _ = c.sign(ctx, s, []byte("a")) // a is now most recent
	_, _ = c.sign(ctx, s, []byte("c")) // evicts b
	_, _ = c.sign(ctx, s, []byte("a")) // hit
	_, _ = c.sign(ctx, s, []byte("b")) // miss
	if got := s.calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4 (a, b, c, b)", got)
	}
}

// Two keys signing the same message are two entries.
func TestSigCacheKeysBySigner(t *testing.T) {
	c := newSigCache(1024)
	a, b := newCounting(t), newCounting(t)
	sa, _ := c.sign(context.Background(), a, []byte("m"))
	sb, _ := c.sign(context.Background(), b, []byte("m"))
	if !ed25519.Verify(b.Public(), []byte("m"), sb) || string(sa) == string(sb) {
		t.Fatal("cache returned one signer's signature for another")
	}
}
