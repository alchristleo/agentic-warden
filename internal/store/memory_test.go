package store_test

import (
	"testing"

	"github.com/acme/agent-wrapper/internal/store"
	"github.com/acme/agent-wrapper/internal/store/storetest"
)

// TestMemory runs the shared store conformance suite. Every Store
// implementation runs this same suite, so a Postgres store and the in-memory
// one cannot drift apart in behavior.
func TestMemory(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		return store.NewMemory()
	})
}

// TestMemorySCIM runs the SCIM conformance cases against the memory store.
func TestMemorySCIM(t *testing.T) {
	storetest.RunSCIM(t, func(t *testing.T) store.SCIMStore {
		return store.NewMemory()
	})
}
