package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/acme/agent-wrapper/internal/store"
	"github.com/acme/agent-wrapper/internal/store/storetest"
)

// TestPostgres runs the same conformance suite as the in-memory store against
// a real database. Set AWD_TEST_DATABASE_URL to a throwaway Postgres to run
// it; without one there is nothing to test against and the suite skips.
//
// The suite expects a fresh store per case, so each case truncates the table.
// Point this at a disposable database, never a real one.
func TestPostgres(t *testing.T) {
	url := os.Getenv("AWD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AWD_TEST_DATABASE_URL is not set; skipping the Postgres conformance suite")
	}

	ctx := context.Background()
	pg, err := store.OpenPostgres(ctx, url)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(pg.Close)

	storetest.Run(t, func(t *testing.T) store.Store {
		if err := pg.Truncate(ctx); err != nil {
			t.Fatalf("resetting the database: %v", err)
		}
		return pg
	})
}
