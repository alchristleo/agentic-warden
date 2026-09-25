// Package authz decides who may use the console: members of one group, as
// the identity provider says. It reads SCIM and the applied snapshot, never
// the policy's own authored groups, which an admin could edit to promote
// themselves.
package authz

import (
	"context"
	"errors"

	"github.com/acme/agent-wrapper/internal/model"
)

// ErrNoGroupData means awd has no IdP group data at all, so no one can be
// shown to be an admin.
var ErrNoGroupData = errors.New("console needs group data (SCIM or awd groups apply)")

// Groups is the part of the store authz reads.
type Groups interface {
	SCIMGroupsFor(ctx context.Context, userName string) ([]string, error)
	CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error)
	SCIMCounts(ctx context.Context) (model.SCIMCounts, error)
}

// Checker answers "is this user a console admin".
type Checker struct {
	Store Groups
	Group string
}

// Admin reports whether user is in the admin group. Matching is exact, as
// bundle resolution is.
func (c *Checker) Admin(ctx context.Context, user string) (bool, error) {
	snapshot, err := c.Store.CurrentGroupSnapshot(ctx)
	hasSnapshot := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return false, err
	}
	counts, err := c.Store.SCIMCounts(ctx)
	if err != nil {
		return false, err
	}
	if !hasSnapshot && counts.Groups == 0 {
		return false, ErrNoGroupData
	}
	if hasSnapshot {
		for _, g := range snapshot.Members[user] {
			if g == c.Group {
				return true, nil
			}
		}
	}
	scim, err := c.Store.SCIMGroupsFor(ctx, user)
	if err != nil {
		return false, err
	}
	for _, g := range scim {
		if g == c.Group {
			return true, nil
		}
	}
	return false, nil
}
