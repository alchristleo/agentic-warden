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

// PutRuleSet stores a revision and its audit event.
func (p *Postgres) PutRuleSet(ctx context.Context, r model.Revision, audit model.AuditEvent) error {
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
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, query, r.Version, encoded, createdAt, r.CreatedBy)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
				return fmt.Errorf("store: revision %q already exists: %w", r.Version, model.ErrConflict)
			}
			return fmt.Errorf("store: storing revision %q: %w", r.Version, err)
		}
		return insertAudit(ctx, tx, audit)
	})
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

// PutEnrollmentToken stores a token and its audit event.
func (p *Postgres) PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken, audit model.AuditEvent) error {
	if t.Hash == "" || t.User == "" {
		return fmt.Errorf("store: enrollment token needs a hash and a user: %w", model.ErrBadInput)
	}
	const query = `
		INSERT INTO enrollment_tokens (hash, "user", expires_at)
		VALUES ($1, $2, $3)`
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, query, t.Hash, t.User, t.ExpiresAt); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
				return fmt.Errorf("store: enrollment token already exists: %w", model.ErrConflict)
			}
			return fmt.Errorf("store: storing enrollment token: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

// ConsumeEnrollmentToken marks a token used. The UPDATE's WHERE clause is the
// atomic check: it matches only an unused, unexpired token, so of two
// concurrent consumers exactly one sees a row.
func (p *Postgres) ConsumeEnrollmentToken(ctx context.Context, hash string, now time.Time) (string, error) {
	const consume = `
		UPDATE enrollment_tokens
		SET used_at = $2
		WHERE hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING "user"`
	var user string
	err := p.pool.QueryRow(ctx, consume, hash, now).Scan(&user)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("store: consuming enrollment token: %w", err)
	}
	// Distinguish "unknown" from "used or expired" for the caller's status.
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM enrollment_tokens WHERE hash = $1)`, hash).Scan(&exists); err != nil {
		return "", fmt.Errorf("store: checking enrollment token: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("store: unknown enrollment token: %w", model.ErrNotFound)
	}
	return "", fmt.Errorf("store: enrollment token already used or expired: %w", model.ErrConflict)
}

// PutMachine stores an enrolled machine.
func (p *Postgres) PutMachine(ctx context.Context, m model.Machine) error {
	if m.ID == "" || m.CredentialHash == "" {
		return fmt.Errorf("store: machine needs an id and a credential hash: %w", model.ErrBadInput)
	}
	const query = `
		INSERT INTO machines (id, "user", name, os, credential_hash, enrolled_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := p.pool.Exec(ctx, query, m.ID, m.User, m.Name, m.OS, m.CredentialHash, m.EnrolledAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return fmt.Errorf("store: machine %q or its credential already exists: %w", m.ID, model.ErrConflict)
		}
		return fmt.Errorf("store: storing machine %q: %w", m.ID, err)
	}
	return nil
}

const selectMachine = `
	SELECT id, "user", name, os, credential_hash, enrolled_at, last_seen_at, last_bundle_version, last_key_id
	FROM machines`

// MachineByCredential finds a machine by credential hash.
func (p *Postgres) MachineByCredential(ctx context.Context, hash string) (model.Machine, error) {
	rows, _ := p.pool.Query(ctx, selectMachine+` WHERE credential_hash = $1`, hash)
	machines, err := collectMachines(rows)
	if err != nil {
		return model.Machine{}, err
	}
	if len(machines) == 0 {
		return model.Machine{}, fmt.Errorf("store: no machine holds this credential: %w", model.ErrNotFound)
	}
	return machines[0], nil
}

// TouchMachine records a fetch. keyID is stored exactly as given, empty
// included, so a machine that stops presenting a key stops reporting one.
func (p *Postgres) TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion, keyID string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE machines SET last_seen_at = $2, last_bundle_version = $3, last_key_id = $4 WHERE id = $1`,
		id, seenAt, bundleVersion, keyID)
	if err != nil {
		return fmt.Errorf("store: touching machine %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

// ListMachines returns machines in enrollment order, hashes cleared.
func (p *Postgres) ListMachines(ctx context.Context) ([]model.Machine, error) {
	rows, _ := p.pool.Query(ctx, selectMachine+` ORDER BY seq`)
	machines, err := collectMachines(rows)
	if err != nil {
		return nil, err
	}
	for i := range machines {
		machines[i].CredentialHash = ""
	}
	return machines, nil
}

// DeleteMachine revokes a machine and records its audit event.
func (p *Postgres) DeleteMachine(ctx context.Context, id string, audit model.AuditEvent) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM machines WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("store: deleting machine %q: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("store: machine %q not found: %w", id, model.ErrNotFound)
		}
		return insertAudit(ctx, tx, audit)
	})
}

func collectMachines(rows pgx.Rows) ([]model.Machine, error) {
	defer rows.Close()
	out := make([]model.Machine, 0)
	for rows.Next() {
		var (
			m        model.Machine
			lastSeen *time.Time
		)
		if err := rows.Scan(&m.ID, &m.User, &m.Name, &m.OS, &m.CredentialHash, &m.EnrolledAt, &lastSeen, &m.LastBundleVersion, &m.LastKeyID); err != nil {
			return nil, fmt.Errorf("store: reading a machine: %w", err)
		}
		if lastSeen != nil {
			m.LastSeenAt = *lastSeen
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading machines: %w", err)
	}
	return out, nil
}

// PutGroupSnapshot stores a snapshot and its audit event; the newest
// snapshot is current.
func (p *Postgres) PutGroupSnapshot(ctx context.Context, s model.GroupSnapshot, audit model.AuditEvent) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	encoded, err := json.Marshal(s.Members)
	if err != nil {
		return fmt.Errorf("store: encoding group snapshot members: %w", err)
	}
	syncedAt := s.SyncedAt
	if syncedAt.IsZero() {
		syncedAt = time.Now()
	}
	const query = `
		INSERT INTO group_snapshots (source, applied_by, synced_at, members)
		VALUES ($1, $2, $3, $4)`
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, query, s.Source, s.AppliedBy, syncedAt, encoded); err != nil {
			return fmt.Errorf("store: storing group snapshot: %w", err)
		}
		return insertAudit(ctx, tx, audit)
	})
}

// CurrentGroupSnapshot returns the newest snapshot.
func (p *Postgres) CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error) {
	const query = `
		SELECT seq, source, applied_by, synced_at, members
		FROM group_snapshots
		ORDER BY seq DESC
		LIMIT 1`
	var (
		s       model.GroupSnapshot
		members []byte
	)
	err := p.pool.QueryRow(ctx, query).Scan(&s.Seq, &s.Source, &s.AppliedBy, &s.SyncedAt, &members)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return model.GroupSnapshot{}, fmt.Errorf("store: no group snapshot has been posted: %w", model.ErrNotFound)
	case err != nil:
		return model.GroupSnapshot{}, fmt.Errorf("store: reading the group snapshot: %w", err)
	}
	if err := json.Unmarshal(members, &s.Members); err != nil {
		return model.GroupSnapshot{}, fmt.Errorf("store: decoding group snapshot members: %w", err)
	}
	return s, nil
}

// Truncate removes every row and restarts the sequences. It exists for the
// conformance suite, which needs a fresh store per case; never call it
// against a database that holds a real policy history.
func (p *Postgres) Truncate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `TRUNCATE policy_revisions, enrollment_tokens, machines, group_snapshots, scim_members, scim_users, scim_groups, console_sessions, audit_events RESTART IDENTITY`); err != nil {
		return fmt.Errorf("store: truncating: %w", err)
	}
	return nil
}
