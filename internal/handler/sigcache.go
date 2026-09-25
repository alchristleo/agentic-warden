package handler

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/acme/agent-wrapper/internal/signing"
)

// sigCache remembers signatures by key and message digest. Machines on the
// same policy receive the same bytes, so a steady fleet asks a remote
// signer (KMS) for a handful of signatures per policy change instead of
// one per request. Signatures are public, so there is nothing to expire;
// failures are never stored, so a throttled call is simply retried.
type sigCache struct {
	mu    sync.Mutex
	size  int
	order *list.List // front = most recent; values are cache keys
	items map[string]*list.Element
	sigs  map[string][]byte
}

func newSigCache(size int) *sigCache {
	return &sigCache{size: size, order: list.New(), items: map[string]*list.Element{}, sigs: map[string][]byte{}}
}

func (c *sigCache) sign(ctx context.Context, s signing.Signer, msg []byte) ([]byte, error) {
	sum := sha256.Sum256(msg)
	key := signing.KeyID(s.Public()) + ":" + hex.EncodeToString(sum[:])

	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		sig := c.sigs[key]
		c.mu.Unlock()
		return sig, nil
	}
	c.mu.Unlock()

	sig, err := s.Sign(ctx, msg)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; !ok {
		c.items[key] = c.order.PushFront(key)
		c.sigs[key] = sig
		if c.order.Len() > c.size {
			oldest := c.order.Back()
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(string))
			delete(c.sigs, oldest.Value.(string))
		}
	}
	return sig, nil
}
