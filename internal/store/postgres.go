package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

//go:embed migrations/*.sql
var migrations embed.FS

// uniqueViolation is the Postgres error code for a broken unique constraint,
// which here means the version has already been applied.
const uniqueViolation = "23505"

// Postgres stores policy revisions in Postgres. It satisfies the same
// conformance suite as Memory.
type Postgres struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects to url and applies the schema.
func OpenPostgres(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: connecting to Postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: reaching Postgres: %w", err)
	}
	p := &Postgres{pool: pool}
	if err := p.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

// migrate applies every embedded migration in name order. Each one is
// idempotent, so re-running them on an existing database is a no-op and no
// version table is needed yet.
func (p *Postgres) migrate(ctx context.Context) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("store: listing migrations: %w", err)
	}
	for _, name := range names {
		statements, err := migrations.ReadFile(name)
		if err != nil {
			return fmt.Errorf("store: reading migration %s: %w", name, err)
		}
		if _, err := p.pool.Exec(ctx, string(statements)); err != nil {
			return fmt.Errorf("store: applying migration %s: %w", name, err)
		}
	}
	return nil
}

// PutRuleSet stores a revision.
func (p *Postgres) PutRuleSet(ctx context.Context, r model.Revision) error {
	if r.Version == "" {
		return fmt.Errorf("store: revision has no version: %w", model.ErrBadInput)
	}
	encoded, err := json.Marshal(r.RuleSet)
	if err != nil {
		return fmt.Errorf("store: encoding rule set %q: %w", r.Version, err)
	}
	createdAt := r.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	const query = `
		INSERT INTO policy_revisions (version, rule_set, created_at, created_by)
		VALUES ($1, $2, $3, $4)`
	_, err = p.pool.Exec(ctx, query, r.Version, encoded, createdAt, r.CreatedBy)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return fmt.Errorf("store: revision %q already exists: %w", r.Version, model.ErrConflict)
		}
		return fmt.Errorf("store: storing revision %q: %w", r.Version, err)
	}
	return nil
}

// CurrentRuleSet returns the newest revision.
func (p *Postgres) CurrentRuleSet(ctx context.Context) (model.Revision, error) {
	const query = `
		SELECT seq, version, rule_set, created_at, created_by
		FROM policy_revisions
		ORDER BY seq DESC
		LIMIT 1`
	rows, _ := p.pool.Query(ctx, query)
	revisions, err := collectRevisions(rows)
	if err != nil {
		return model.Revision{}, err
	}
	if len(revisions) == 0 {
		return model.Revision{}, fmt.Errorf("store: no policy has been applied: %w", model.ErrNotFound)
	}
	return revisions[0], nil
}

// Revisions returns up to limit revisions, newest first.
func (p *Postgres) Revisions(ctx context.Context, limit int) ([]model.Revision, error) {
	// Ordering is by sequence, not by timestamp: two revisions applied in the
	// same clock tick still have a defined newest.
	const query = `
		SELECT seq, version, rule_set, created_at, created_by
		FROM policy_revisions
		ORDER BY seq DESC
		LIMIT $1`
	if limit < 0 {
		limit = 0
	}
	rows, _ := p.pool.Query(ctx, query, limit)
	return collectRevisions(rows)
}

func collectRevisions(rows pgx.Rows) ([]model.Revision, error) {
	defer rows.Close()

	out := make([]model.Revision, 0)
	for rows.Next() {
		var (
			revision model.Revision
			ruleSet  []byte
		)
		if err := rows.Scan(&revision.Seq, &revision.Version, &ruleSet,
			&revision.CreatedAt, &revision.CreatedBy); err != nil {
			return nil, fmt.Errorf("store: reading a revision: %w", err)
		}
		var parsed policy.RuleSet
		if err := json.Unmarshal(ruleSet, &parsed); err != nil {
			return nil, fmt.Errorf("store: decoding rule set %q: %w", revision.Version, err)
		}
		revision.RuleSet = parsed
		out = append(out, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading revisions: %w", err)
	}
	return out, nil
}

// Truncate removes every revision and restarts the sequence. It exists for
// the conformance suite, which needs a fresh store per case; never call it
// against a database that holds a real policy history.
func (p *Postgres) Truncate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `TRUNCATE policy_revisions RESTART IDENTITY`); err != nil {
		return fmt.Errorf("store: truncating policy_revisions: %w", err)
	}
	return nil
}
