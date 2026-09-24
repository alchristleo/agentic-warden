package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

// groupSources is a user's groups by where each came from. The bundle
// serves Effective; /v1/groups/resolve shows all of it, so an operator can
// answer "why does alice get this policy" without reading three tables.
type groupSources struct {
	User      string   `json:"user"`
	Authored  []string `json:"authored"`
	Snapshot  []string `json:"snapshot"`
	SCIM      []string `json:"scim"`
	Effective []string `json:"effective"`
	// SCIMNearMatch names a SCIM userName equal to User ignoring case but
	// not exactly — an IdP attribute mapping to fix. Resolution does not
	// fold case, so without this the mismatch would only show up as a user
	// quietly missing their groups.
	SCIMNearMatch *string `json:"scimNearMatch"`
}

// resolveGroups gathers a user's groups from every source. With no
// snapshot and no SCIM data, Effective is the authored list alone, exactly
// as before either source existed.
func (h *Handler) resolveGroups(ctx context.Context, ruleSet *policy.RuleSet, user string) (groupSources, error) {
	s := groupSources{User: user, Authored: policy.UnionGroups(ruleSet.GroupsFor(user))}

	snapshot, err := h.store.CurrentGroupSnapshot(ctx)
	switch {
	case err == nil:
		s.Snapshot = policy.UnionGroups(snapshot.Members[user])
	case errors.Is(err, model.ErrNotFound):
		s.Snapshot = []string{}
	default:
		return groupSources{}, err
	}

	if s.SCIM, err = h.store.SCIMGroupsFor(ctx, user); err != nil {
		return groupSources{}, err
	}
	s.Effective = policy.UnionGroups(s.Authored, s.Snapshot, s.SCIM)
	return s, nil
}

// getGroupsResolve shows one user's resolution, per source.
func (h *Handler) getGroupsResolve(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		writeError(w, http.StatusBadRequest, "the user query parameter is required")
		return
	}
	ruleSet := &policy.RuleSet{}
	revision, err := h.store.CurrentRuleSet(r.Context())
	switch {
	case err == nil:
		ruleSet = &revision.RuleSet
	case errors.Is(err, model.ErrNotFound):
	default:
		h.fail(w, r, err)
		return
	}
	s, err := h.resolveGroups(r.Context(), ruleSet, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	near, _, err := h.store.ListSCIMUsers(r.Context(), model.SCIMFilter{Attribute: model.SCIMAttrUserName, Value: user}, 1, 1)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if len(near) == 1 && near[0].UserName != user {
		s.SCIMNearMatch = &near[0].UserName
	}
	writeJSON(w, http.StatusOK, s)
}
