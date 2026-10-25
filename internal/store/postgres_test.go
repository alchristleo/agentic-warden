package store_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alchristleo/agentic-warden/internal/store"
	"github.com/alchristleo/agentic-warden/internal/store/storetest"
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

// TestConcurrentMigrate opens the store from several goroutines against a
// fresh database, as several cells or replicas starting together would. The
// migration lock must let every one of them succeed.
func TestConcurrentMigrate(t *testing.T) {
	base := os.Getenv("AWD_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("AWD_TEST_DATABASE_URL is not set; skipping the concurrent migration test")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { admin.Close(ctx) })

	name := fmt.Sprintf("awd_migrate_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("dropping database: %v", err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing url: %v", err)
	}
	u.Path = "/" + name

	const n = 4
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pg, err := store.OpenPostgres(ctx, u.String())
			if err == nil {
				pg.Close()
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("OpenPostgres: %v", err)
		}
	}
}
