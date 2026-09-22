package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// maxGroupsBody bounds a membership snapshot. An organization of tens of
// thousands of users with a handful of groups each is a few megabytes; the
// cap is for a runaway exporter, not a real fleet.
const maxGroupsBody = 8 << 20

// groupsRequest is the body of PUT /v1/groups. Unknown fields are rejected
// so a misspelled key fails loudly rather than posting an empty snapshot.
type groupsRequest struct {
	// Source names the exporter that produced this snapshot.
	Source string `json:"source"`
	// Members maps a user to their groups, exactly as the IdP exported them.
	Members map[string][]string `json:"members"`
}

// groupsSummary is what PUT /v1/groups answers with: enough for an
// operator to confirm what the server stored without reading the members.
type groupsSummary struct {
	// Source names the exporter that produced the current snapshot.
	Source string `json:"source"`
	// AppliedBy is who posted the current snapshot, from the client's environment.
	AppliedBy string `json:"appliedBy,omitempty"`
	// SyncedAt is when the server stored the current snapshot.
	SyncedAt time.Time `json:"syncedAt"`
	// Users is the number of distinct member keys in the snapshot.
	Users int `json:"users"`
	// Groups is the number of distinct group names across all members.
	Groups int `json:"groups"`
}

// groupsDetail is what GET /v1/groups answers with: the summary fields
// plus the full membership map, for an operator checking exactly what the
// server resolves against.
type groupsDetail struct {
	// groupsSummary's fields are inlined into the same JSON object; GET
	// answers with everything PUT does, plus members.
	groupsSummary
	// Members holds the full membership map and is never nil: an absent
	// map would be indistinguishable from an omitted field by a caller
	// checking the raw JSON, so an empty snapshot still serialises
	// "members":{}.
	Members map[string][]string `json:"members"`
}

// summarise turns a stored snapshot into the wire summary used by PUT,
// counting distinct users and groups without exposing the membership map.
func summarise(s model.GroupSnapshot) groupsSummary {
	distinct := make(map[string]bool)
	for _, groups := range s.Members {
		for _, g := range groups {
			distinct[g] = true
		}
	}
	return groupsSummary{Source: s.Source, AppliedBy: s.AppliedBy, SyncedAt: s.SyncedAt, Users: len(s.Members), Groups: len(distinct)}
}

// detail turns a stored snapshot into the wire detail used by GET, adding
// the full membership map and guaranteeing it is never nil.
func detail(s model.GroupSnapshot) groupsDetail {
	members := s.Members
	if members == nil {
		members = map[string][]string{}
	}
	return groupsDetail{groupsSummary: summarise(s), Members: members}
}

// putGroups stores an IdP membership snapshot. It replaces the previous
// snapshot whole; the authored groups map in the policy still unions with
// it, so a snapshot can only add memberships an author did not write.
func (h *Handler) putGroups(w http.ResponseWriter, r *http.Request) {
	var req groupsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGroupsBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the snapshot exceeds %d bytes", maxGroupsBody))
			return
		}
		writeError(w, http.StatusBadRequest, "the request body is not a valid group snapshot: "+err.Error())
		return
	}
	if req.Members == nil {
		writeError(w, http.StatusUnprocessableEntity, "the snapshot has no members object")
		return
	}
	snapshot := model.GroupSnapshot{
		Source:    req.Source,
		AppliedBy: r.Header.Get("X-Applied-By"),
		SyncedAt:  h.Now(),
		Members:   req.Members,
	}
	if err := snapshot.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.store.PutGroupSnapshot(r.Context(), snapshot); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, summarise(snapshot))
}

// getGroups shows the current snapshot, members included, for an operator
// checking what the server resolves against.
func (h *Handler) getGroups(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.store.CurrentGroupSnapshot(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail(snapshot))
}
