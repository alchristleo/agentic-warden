package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/acme/agent-wrapper/internal/model"
)

// checkViolation is the Postgres error code for a failed CHECK constraint:
// here, an audit event without an actor or action.
const checkViolation = "23514"

// insertAudit writes an event inside tx. It deliberately does not call
// Validate: the table's CHECKs reject a bad event inside the transaction,
// so the paired write rolls back with it. The conformance suite relies on
// that.
func insertAudit(ctx context.Context, tx pgx.Tx, e model.AuditEvent) error {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	detail := e.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("store: encoding audit detail: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO audit_events (at, actor, action, target, detail) VALUES ($1, $2, $3, $4, $5)`,
		at, e.Actor, e.Action, e.Target, encoded)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == checkViolation {
		return fmt.Errorf("store: audit event needs an actor and an action: %w", model.ErrBadInput)
	}
	if err != nil {
		return fmt.Errorf("store: recording audit event: %w", err)
	}
	return nil
}

// CreateSession stores a session.
func (p *Postgres) CreateSession(ctx context.Context, s model.ConsoleSession) error {
	if s.TokenHash == "" || s.User == "" {
		return fmt.Errorf("store: session needs a token hash and a user: %w", model.ErrBadInput)
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO console_sessions (token_hash, user_name, created_at, last_seen_at) VALUES ($1, $2, $3, $4)`,
		s.TokenHash, s.User, s.CreatedAt, s.LastSeenAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return fmt.Errorf("store: session exists: %w", model.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("store: storing session: %w", err)
	}
	return nil
}

// SessionByHash returns a session.
func (p *Postgres) SessionByHash(ctx context.Context, hash string) (model.ConsoleSession, error) {
	s := model.ConsoleSession{TokenHash: hash}
	err := p.pool.QueryRow(ctx,
		`SELECT user_name, created_at, last_seen_at FROM console_sessions WHERE token_hash = $1`, hash).
		Scan(&s.User, &s.CreatedAt, &s.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ConsoleSession{}, fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	if err != nil {
		return model.ConsoleSession{}, fmt.Errorf("store: reading session: %w", err)
	}
	return s, nil
}

// TouchSession records activity on a session.
func (p *Postgres) TouchSession(ctx context.Context, hash string, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE console_sessions SET last_seen_at = $2 WHERE token_hash = $1`, hash, at)
	if err != nil {
		return fmt.Errorf("store: touching session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: session not found: %w", model.ErrNotFound)
	}
	return nil
}

// DeleteSession removes a session if it exists.
func (p *Postgres) DeleteSession(ctx context.Context, hash string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM console_sessions WHERE token_hash = $1`, hash); err != nil {
		return fmt.Errorf("store: deleting session: %w", err)
	}
	return nil
}

// DeleteSessionsFor removes every session of a user.
func (p *Postgres) DeleteSessionsFor(ctx context.Context, user string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM console_sessions WHERE user_name = $1`, user); err != nil {
		return fmt.Errorf("store: deleting sessions for %q: %w", user, err)
	}
	return nil
}

// DeleteExpiredSessions sweeps sessions past either bound.
func (p *Postgres) DeleteExpiredSessions(ctx context.Context, createdBefore, seenBefore time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM console_sessions WHERE created_at < $1 OR last_seen_at < $2`, createdBefore, seenBefore)
	if err != nil {
		return 0, fmt.Errorf("store: sweeping sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RecordAudit stores a standalone event.
func (p *Postgres) RecordAudit(ctx context.Context, e model.AuditEvent) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error { return insertAudit(ctx, tx, e) })
}

// AuditEvents lists events newest first.
func (p *Postgres) AuditEvents(ctx context.Context, limit int, before int64) ([]model.AuditEvent, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, at, actor, action, target, detail FROM audit_events
		WHERE $2 <= 0 OR id < $2
		ORDER BY id DESC LIMIT $1`, limit, before)
	if err != nil {
		return nil, fmt.Errorf("store: listing audit events: %w", err)
	}
	defer rows.Close()
	out := make([]model.AuditEvent, 0)
	for rows.Next() {
		var (
			e      model.AuditEvent
			detail []byte
		)
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Target, &detail); err != nil {
			return nil, fmt.Errorf("store: reading an audit event: %w", err)
		}
		if err := json.Unmarshal(detail, &e.Detail); err != nil {
			return nil, fmt.Errorf("store: decoding audit detail %d: %w", e.ID, err)
		}
		if len(e.Detail) == 0 {
			e.Detail = nil
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading audit events: %w", err)
	}
	return out, nil
}
