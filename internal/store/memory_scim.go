package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// CreateSCIMUser stores a provisioned user.
func (m *Memory) CreateSCIMUser(_ context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimUsers[u.ID]; ok {
		return fmt.Errorf("store: scim user %q already exists: %w", u.ID, model.ErrConflict)
	}
	if err := m.userNameFreeLocked(u.UserName, ""); err != nil {
		return err
	}
	m.scimUsers[u.ID] = u
	return nil
}

// userNameFreeLocked reports a conflict when a user other than except
// holds name ignoring case.
func (m *Memory) userNameFreeLocked(name, except string) error {
	for id, u := range m.scimUsers {
		if id != except && strings.ToLower(u.UserName) == strings.ToLower(name) {
			return fmt.Errorf("store: scim userName %q is taken: %w", name, model.ErrConflict)
		}
	}
	return nil
}

// SCIMUser returns one user.
func (m *Memory) SCIMUser(_ context.Context, id string) (model.SCIMUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.scimUsers[id]
	if !ok {
		return model.SCIMUser{}, fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	return u, nil
}

// ReplaceSCIMUser overwrites a user, keeping its Created time.
func (m *Memory) ReplaceSCIMUser(_ context.Context, u model.SCIMUser) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.scimUsers[u.ID]
	if !ok {
		return fmt.Errorf("store: scim user %q not found: %w", u.ID, model.ErrNotFound)
	}
	if err := m.userNameFreeLocked(u.UserName, u.ID); err != nil {
		return err
	}
	u.Created = old.Created
	m.scimUsers[u.ID] = u
	return nil
}

// PatchSCIMUser applies a change under the lock.
func (m *Memory) PatchSCIMUser(_ context.Context, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.scimUsers[id]
	if !ok {
		return model.SCIMUser{}, fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	u = applyUserChange(u, c, at)
	if err := u.Validate(); err != nil {
		return model.SCIMUser{}, fmt.Errorf("store: %w", err)
	}
	if err := m.userNameFreeLocked(u.UserName, id); err != nil {
		return model.SCIMUser{}, err
	}
	m.scimUsers[id] = u
	return u, nil
}

// applyUserChange sets the fields c names. Both stores use it, so a patch
// means the same thing whichever one runs it.
func applyUserChange(u model.SCIMUser, c model.SCIMUserChange, at time.Time) model.SCIMUser {
	if c.UserName != nil {
		u.UserName = *c.UserName
	}
	if c.ExternalID != nil {
		u.ExternalID = *c.ExternalID
	}
	if c.Active != nil {
		u.Active = *c.Active
	}
	u.Modified = at
	return u
}

// DeleteSCIMUser removes a user and its memberships.
func (m *Memory) DeleteSCIMUser(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimUsers[id]; !ok {
		return fmt.Errorf("store: scim user %q not found: %w", id, model.ErrNotFound)
	}
	delete(m.scimUsers, id)
	for _, members := range m.scimMembers {
		delete(members, id)
	}
	return nil
}

// ListSCIMUsers returns one page of matching users.
func (m *Memory) ListSCIMUsers(_ context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error) {
	var match func(model.SCIMUser) bool
	switch f.Attribute {
	case "":
		match = func(model.SCIMUser) bool { return true }
	case model.SCIMAttrUserName:
		match = func(u model.SCIMUser) bool { return strings.ToLower(u.UserName) == strings.ToLower(f.Value) }
	case model.SCIMAttrExternalID:
		match = func(u model.SCIMUser) bool { return u.ExternalID == f.Value }
	default:
		return nil, 0, fmt.Errorf("store: users cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]model.SCIMUser, 0)
	for _, u := range m.scimUsers {
		if match(u) {
			all = append(all, u)
		}
	}
	sort.Slice(all, func(i, j int) bool { return createdThenID(all[i].Created, all[i].ID, all[j].Created, all[j].ID) })
	return page(all, startIndex, count), len(all), nil
}

// createdThenID is the list order both stores use, so paging is stable.
func createdThenID(ci time.Time, idi string, cj time.Time, idj string) bool {
	if !ci.Equal(cj) {
		return ci.Before(cj)
	}
	return idi < idj
}

// page cuts one 1-based page out of all; past the end is empty, never nil.
func page[T any](all []T, startIndex, count int) []T {
	if startIndex < 1 {
		startIndex = 1
	}
	from := startIndex - 1
	if count <= 0 || from >= len(all) {
		return []T{}
	}
	return append([]T{}, all[from:min(from+count, len(all))]...)
}

// CreateSCIMGroup stores a group.
func (m *Memory) CreateSCIMGroup(_ context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimGroups[g.ID]; ok {
		return fmt.Errorf("store: scim group %q already exists: %w", g.ID, model.ErrConflict)
	}
	if err := m.displayNameFreeLocked(g.DisplayName, ""); err != nil {
		return err
	}
	members, err := m.memberSetLocked(g.Members)
	if err != nil {
		return err
	}
	g.Members = nil
	m.scimGroups[g.ID] = g
	m.scimMembers[g.ID] = members
	return nil
}

func (m *Memory) displayNameFreeLocked(name, except string) error {
	for id, g := range m.scimGroups {
		if id != except && strings.ToLower(g.DisplayName) == strings.ToLower(name) {
			return fmt.Errorf("store: scim displayName %q is taken: %w", name, model.ErrConflict)
		}
	}
	return nil
}

// memberSetLocked builds a member set, refusing an id that is not a user.
func (m *Memory) memberSetLocked(ids []string) (map[string]bool, error) {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, ok := m.scimUsers[id]; !ok {
			return nil, fmt.Errorf("store: scim member %q is not a user: %w", id, model.ErrBadInput)
		}
		set[id] = true
	}
	return set, nil
}

// groupLocked assembles a stored group with its sorted members.
func (m *Memory) groupLocked(id string) (model.SCIMGroup, bool) {
	g, ok := m.scimGroups[id]
	if !ok {
		return model.SCIMGroup{}, false
	}
	g.Members = make([]string, 0, len(m.scimMembers[id]))
	for member := range m.scimMembers[id] {
		g.Members = append(g.Members, member)
	}
	sort.Strings(g.Members)
	return g, true
}

// SCIMGroup returns one group.
func (m *Memory) SCIMGroup(_ context.Context, id string) (model.SCIMGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groupLocked(id)
	if !ok {
		return model.SCIMGroup{}, fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	return g, nil
}

// ReplaceSCIMGroup overwrites a group, keeping its Created time.
func (m *Memory) ReplaceSCIMGroup(_ context.Context, g model.SCIMGroup) error {
	if err := g.Validate(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.scimGroups[g.ID]
	if !ok {
		return fmt.Errorf("store: scim group %q not found: %w", g.ID, model.ErrNotFound)
	}
	if err := m.displayNameFreeLocked(g.DisplayName, g.ID); err != nil {
		return err
	}
	members, err := m.memberSetLocked(g.Members)
	if err != nil {
		return err
	}
	g.Created = old.Created
	g.Members = nil
	m.scimGroups[g.ID] = g
	m.scimMembers[g.ID] = members
	return nil
}

// PatchSCIMGroup applies a change to a copy and commits it only if every
// step succeeds, which is the memory store's transaction.
func (m *Memory) PatchSCIMGroup(_ context.Context, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.scimGroups[id]
	if !ok {
		return model.SCIMGroup{}, fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	if c.DisplayName != nil {
		g.DisplayName = *c.DisplayName
	}
	if c.ExternalID != nil {
		g.ExternalID = *c.ExternalID
	}
	g.Modified = at
	if err := g.Validate(); err != nil {
		return model.SCIMGroup{}, fmt.Errorf("store: %w", err)
	}
	if err := m.displayNameFreeLocked(g.DisplayName, id); err != nil {
		return model.SCIMGroup{}, err
	}
	members := make(map[string]bool, len(m.scimMembers[id]))
	for member := range m.scimMembers[id] {
		members[member] = true
	}
	for _, op := range c.Members {
		switch op.Kind {
		case model.SCIMMembersReplace:
			members = make(map[string]bool, len(op.Users))
			fallthrough
		case model.SCIMMembersAdd:
			for _, u := range op.Users {
				if _, ok := m.scimUsers[u]; !ok {
					return model.SCIMGroup{}, fmt.Errorf("store: scim member %q is not a user: %w", u, model.ErrBadInput)
				}
				members[u] = true
			}
		case model.SCIMMembersRemove:
			for _, u := range op.Users {
				delete(members, u)
			}
		default:
			return model.SCIMGroup{}, fmt.Errorf("store: unknown member operation %q: %w", op.Kind, model.ErrBadInput)
		}
	}
	m.scimGroups[id] = g
	m.scimMembers[id] = members
	out, _ := m.groupLocked(id)
	return out, nil
}

// DeleteSCIMGroup removes a group and its memberships.
func (m *Memory) DeleteSCIMGroup(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scimGroups[id]; !ok {
		return fmt.Errorf("store: scim group %q not found: %w", id, model.ErrNotFound)
	}
	delete(m.scimGroups, id)
	delete(m.scimMembers, id)
	return nil
}

// ListSCIMGroups returns one page of matching groups. withMembers false
// skips assembling each group's member set, matching Postgres skipping its
// member query for the same case.
func (m *Memory) ListSCIMGroups(_ context.Context, f model.SCIMFilter, startIndex, count int, withMembers bool) ([]model.SCIMGroup, int, error) {
	var match func(model.SCIMGroup) bool
	switch f.Attribute {
	case "":
		match = func(model.SCIMGroup) bool { return true }
	case model.SCIMAttrDisplayName:
		match = func(g model.SCIMGroup) bool { return strings.ToLower(g.DisplayName) == strings.ToLower(f.Value) }
	case model.SCIMAttrExternalID:
		match = func(g model.SCIMGroup) bool { return g.ExternalID == f.Value }
	default:
		return nil, 0, fmt.Errorf("store: groups cannot be filtered on %q: %w", f.Attribute, model.ErrBadInput)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]model.SCIMGroup, 0)
	for id, g := range m.scimGroups {
		if withMembers {
			g, _ = m.groupLocked(id)
		} else {
			g.Members = []string{}
		}
		if match(g) {
			all = append(all, g)
		}
	}
	sort.Slice(all, func(i, j int) bool { return createdThenID(all[i].Created, all[i].ID, all[j].Created, all[j].ID) })
	return page(all, startIndex, count), len(all), nil
}

// SCIMGroupsFor resolves an enrolled user's SCIM groups.
func (m *Memory) SCIMGroupsFor(_ context.Context, userName string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0)
	for id, u := range m.scimUsers {
		if u.UserName != userName || !u.Active {
			continue
		}
		for gid, members := range m.scimMembers {
			if members[id] {
				out = append(out, m.scimGroups[gid].DisplayName)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// SCIMCounts counts what SCIM has provisioned.
func (m *Memory) SCIMCounts(_ context.Context) (model.SCIMCounts, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := model.SCIMCounts{Users: len(m.scimUsers), Groups: len(m.scimGroups)}
	for _, u := range m.scimUsers {
		if u.Active {
			c.ActiveUsers++
		}
	}
	return c, nil
}
