package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/acme/agent-wrapper/internal/model"
)

// foreignKeyViolation is the Postgres code for a reference to a missing
// row, which here means a group member that is not a user.
const foreignKeyViolation = "23503"

// scimWriteError turns a constraint violation into the store's vocabulary.
func scimWriteError(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return fmt.Errorf("store: %s: name or id is taken: %w", what, model.ErrConflict)
		case foreignKeyViolation:
			return fmt.Errorf("store: %s: a member is not a user: %w", what, model.ErrBadInput)
		}
	}
	return fmt.Errorf("store: %s: %w", what, err)
}

const scimUserColumns = `id, user_name, external_id, active, created, modified`

func scanSCIMUser(row pgx.Row) (model.SCIMUser, error) {
	var u model.SCIMUser
	err := row.Scan(&u.ID, &u.UserName, &u.ExternalID, &u.Active, &u.Created, &u.Modified)
	return u, err
}

// CreateSCIMUser stores a provisioned user.
func (p *Postgres) CreateSCIMUser(ctx context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	const query = `INSERT INTO scim_users (` + scimUserColumns + `) VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := p.pool.Exec(ctx, query, u.ID, u.UserName, u.ExternalID, u.Active, u.Created, u.Modified); err != nil {
		return scimWriteError(err, "creating scim user")
	}
	return nil
}

// SCIMUser returns one user.
func (p *Postgres) SCIMUser(ctx context.Context, id string) (model.SCIMUser, error) {
	u, err := scanSCIMUser(p.pool.QueryRow(ctx, `SELECT `+scimUserColumns+` FROM scim_users WHERE id = $1`, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return model.SCIMUser{}, fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	case err != nil:
		return model.SCIMUser{}, fmt.Errorf("store: reading scim user: %w", err)
	}
	return u, nil
}

// ReplaceSCIMUser overwrites a user, keeping its Created time.
func (p *Postgres) ReplaceSCIMUser(ctx context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	const query = `UPDATE scim_users SET user_name = $2, external_id = $3, active = $4, modified = $5 WHERE id = $1`
	tag, err := p.pool.Exec(ctx, query, u.ID, u.UserName, u.ExternalID, u.Active, u.Modified)
	if err != nil {
		return scimWriteError(err, "replacing scim user")
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: scim user %q not found: %w", u.ID, model.ErrNotFound)
	}
	return nil
}

// PatchSCIMUser locks the row, applies the change and writes it back.
func (p *Postgres) PatchSCIMUser(ctx context.Context, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error) {
	var out model.SCIMUser
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		u, err := scanSCIMUser(tx.QueryRow(ctx, `SELECT `+scimUserColumns+` FROM scim_users WHERE id = $1 FOR UPDATE`, id))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
		case err != nil:
			return fmt.Errorf("store: reading scim user: %w", err)
		}
		u = applyUserChange(u, c, at)
		if err := u.Validate(); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		const query = `UPDATE scim_users SET user_name = $2, external_id = $3, active = $4, modified = $5 WHERE id = $1`
		if _, err := tx.Exec(ctx, query, u.ID, u.UserName, u.ExternalID, u.Active, u.Modified); err != nil {
			return scimWriteError(err, "patching scim user")
		}
		out = u
		return nil
	})
	return out, err
}

// DeleteSCIMUser removes a user; memberships go with it by cascade.
func (p *Postgres) DeleteSCIMUser(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scim_users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: deleting scim user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

// limitOffset turns a 1-based page into SQL LIMIT and OFFSET.
func limitOffset(startIndex, count int) (int, int) {
	if startIndex < 1 {
		startIndex = 1
	}
	if count < 0 {
		count = 0
	}
	return count, startIndex - 1
}

// ListSCIMUsers returns one page of matching users.
func (p *Postgres) ListSCIMUsers(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error) {
	var where string
	switch f.Attribute {
	case "":
		where = `$1::text = $1::text` // always true; keeps one parameter for every branch
	case model.SCIMAttrUserName:
		where = `lower(user_name) = lower($1)`
	case model.SCIMAttrExternalID:
		where = `external_id = $1`
	default:
		return nil, 0, fmt.Errorf("store: users cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	var total int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM scim_users WHERE `+where, f.Value).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: counting scim users: %w", err)
	}
	limit, offset := limitOffset(startIndex, count)
	rows, err := p.pool.Query(ctx,
		`SELECT `+scimUserColumns+` FROM scim_users WHERE `+where+` ORDER BY created, id COLLATE "C" LIMIT $2 OFFSET $3`,
		f.Value, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("store: listing scim users: %w", err)
	}
	defer rows.Close()
	users := make([]model.SCIMUser, 0)
	for rows.Next() {
		u, err := scanSCIMUser(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("store: listing scim users: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: listing scim users: %w", err)
	}
	return users, total, nil
}

// querier is what both a pool and a transaction offer, so a group can be
// read inside or outside one.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const scimGroupColumns = `id, display_name, external_id, created, modified`

// readSCIMGroup reads one group and its sorted members.
func readSCIMGroup(ctx context.Context, q querier, id string, lock bool) (model.SCIMGroup, error) {
	query := `SELECT ` + scimGroupColumns + ` FROM scim_groups WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var g model.SCIMGroup
	err := q.QueryRow(ctx, query, id).Scan(&g.ID, &g.DisplayName, &g.ExternalID, &g.Created, &g.Modified)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return model.SCIMGroup{}, fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	case err != nil:
		return model.SCIMGroup{}, fmt.Errorf("store: reading scim group: %w", err)
	}
	members, err := readMembers(ctx, q, []string{id})
	if err != nil {
		return model.SCIMGroup{}, err
	}
	g.Members = members[id]
	if g.Members == nil {
		g.Members = []string{}
	}
	return g, nil
}

// readMembers returns each group's sorted member ids.
func readMembers(ctx context.Context, q querier, groupIDs []string) (map[string][]string, error) {
	rows, err := q.Query(ctx,
		`SELECT group_id, user_id FROM scim_members WHERE group_id = ANY($1) ORDER BY group_id, user_id COLLATE "C"`, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("store: reading scim members: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var gid, uid string
		if err := rows.Scan(&gid, &uid); err != nil {
			return nil, fmt.Errorf("store: reading scim members: %w", err)
		}
		out[gid] = append(out[gid], uid)
	}
	return out, rows.Err()
}

// insertMembers adds members, ignoring ones already present.
func insertMembers(ctx context.Context, tx pgx.Tx, groupID string, users []string) error {
	for _, u := range users {
		const query = `INSERT INTO scim_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		if _, err := tx.Exec(ctx, query, groupID, u); err != nil {
			return scimWriteError(err, "adding scim member")
		}
	}
	return nil
}

// CreateSCIMGroup stores a group and its members in one transaction.
func (p *Postgres) CreateSCIMGroup(ctx context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		const query = `INSERT INTO scim_groups (` + scimGroupColumns + `) VALUES ($1, $2, $3, $4, $5)`
		if _, err := tx.Exec(ctx, query, g.ID, g.DisplayName, g.ExternalID, g.Created, g.Modified); err != nil {
			return scimWriteError(err, "creating scim group")
		}
		return insertMembers(ctx, tx, g.ID, g.Members)
	})
}

// SCIMGroup returns one group.
func (p *Postgres) SCIMGroup(ctx context.Context, id string) (model.SCIMGroup, error) {
	return readSCIMGroup(ctx, p.pool, id, false)
}

// ReplaceSCIMGroup overwrites a group and its member set.
func (p *Postgres) ReplaceSCIMGroup(ctx context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		const query = `UPDATE scim_groups SET display_name = $2, external_id = $3, modified = $4 WHERE id = $1`
		tag, err := tx.Exec(ctx, query, g.ID, g.DisplayName, g.ExternalID, g.Modified)
		if err != nil {
			return scimWriteError(err, "replacing scim group")
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("store: scim group %q not found: %w", g.ID, model.ErrNotFound)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM scim_members WHERE group_id = $1`, g.ID); err != nil {
			return fmt.Errorf("store: replacing scim members: %w", err)
		}
		return insertMembers(ctx, tx, g.ID, g.Members)
	})
}

// PatchSCIMGroup locks the group row, so concurrent patches on one group
// queue behind each other instead of losing writes.
func (p *Postgres) PatchSCIMGroup(ctx context.Context, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error) {
	var out model.SCIMGroup
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		g, err := readSCIMGroup(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if c.DisplayName != nil {
			g.DisplayName = *c.DisplayName
		}
		if c.ExternalID != nil {
			g.ExternalID = *c.ExternalID
		}
		g.Modified = at
		if err := g.Validate(); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		const update = `UPDATE scim_groups SET display_name = $2, external_id = $3, modified = $4 WHERE id = $1`
		if _, err := tx.Exec(ctx, update, g.ID, g.DisplayName, g.ExternalID, g.Modified); err != nil {
			return scimWriteError(err, "patching scim group")
		}
		for _, op := range c.Members {
			switch op.Kind {
			case model.SCIMMembersReplace:
				if _, err := tx.Exec(ctx, `DELETE FROM scim_members WHERE group_id = $1`, id); err != nil {
					return fmt.Errorf("store: replacing scim members: %w", err)
				}
				fallthrough
			case model.SCIMMembersAdd:
				if err := insertMembers(ctx, tx, id, op.Users); err != nil {
					return err
				}
			case model.SCIMMembersRemove:
				if _, err := tx.Exec(ctx, `DELETE FROM scim_members WHERE group_id = $1 AND user_id = ANY($2)`, id, op.Users); err != nil {
					return fmt.Errorf("store: removing scim members: %w", err)
				}
			default:
				return fmt.Errorf("store: unknown member operation %q: %w", op.Kind, model.ErrBadInput)
			}
		}
		out, err = readSCIMGroup(ctx, tx, id, false)
		return err
	})
	return out, err
}

// DeleteSCIMGroup removes a group; memberships go with it by cascade.
func (p *Postgres) DeleteSCIMGroup(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM scim_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: deleting scim group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	return nil
}

// ListSCIMGroups returns one page of matching groups with their members.
func (p *Postgres) ListSCIMGroups(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMGroup, int, error) {
	var where string
	switch f.Attribute {
	case "":
		where = `$1::text = $1::text` // always true; keeps one parameter for every branch
	case model.SCIMAttrDisplayName:
		where = `lower(display_name) = lower($1)`
	case model.SCIMAttrExternalID:
		where = `external_id = $1`
	default:
		return nil, 0, fmt.Errorf("store: groups cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	var total int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM scim_groups WHERE `+where, f.Value).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: counting scim groups: %w", err)
	}
	limit, offset := limitOffset(startIndex, count)
	rows, err := p.pool.Query(ctx,
		`SELECT `+scimGroupColumns+` FROM scim_groups WHERE `+where+` ORDER BY created, id COLLATE "C" LIMIT $2 OFFSET $3`,
		f.Value, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("store: listing scim groups: %w", err)
	}
	groups := make([]model.SCIMGroup, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var g model.SCIMGroup
		if err := rows.Scan(&g.ID, &g.DisplayName, &g.ExternalID, &g.Created, &g.Modified); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("store: listing scim groups: %w", err)
		}
		groups = append(groups, g)
		ids = append(ids, g.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: listing scim groups: %w", err)
	}
	members, err := readMembers(ctx, p.pool, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range groups {
		groups[i].Members = members[groups[i].ID]
		if groups[i].Members == nil {
			groups[i].Members = []string{}
		}
	}
	return groups, total, nil
}

// SCIMGroupsFor resolves an enrolled user's SCIM groups.
func (p *Postgres) SCIMGroupsFor(ctx context.Context, userName string) ([]string, error) {
	const query = `
		SELECT g.display_name
		FROM scim_users u
		JOIN scim_members m ON m.user_id = u.id
		JOIN scim_groups g ON g.id = m.group_id
		WHERE u.user_name = $1 AND u.active
		ORDER BY g.display_name COLLATE "C"`
	rows, err := p.pool.Query(ctx, query, userName)
	if err != nil {
		return nil, fmt.Errorf("store: resolving scim groups: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: resolving scim groups: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// SCIMCounts counts what SCIM has provisioned.
func (p *Postgres) SCIMCounts(ctx context.Context) (model.SCIMCounts, error) {
	const query = `
		SELECT (SELECT count(*) FROM scim_users),
		       (SELECT count(*) FROM scim_users WHERE active),
		       (SELECT count(*) FROM scim_groups)`
	var c model.SCIMCounts
	if err := p.pool.QueryRow(ctx, query).Scan(&c.Users, &c.ActiveUsers, &c.Groups); err != nil {
		return model.SCIMCounts{}, fmt.Errorf("store: counting scim rows: %w", err)
	}
	return c, nil
}
