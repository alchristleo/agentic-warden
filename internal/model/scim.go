package model

import (
	"fmt"
	"time"
)

// SCIMUser is a user as the identity provider provisioned it over SCIM.
// Resolution matches UserName exactly against the enrolled user; an
// inactive user keeps its memberships but contributes no groups, so a
// reactivation restores them without the IdP re-pushing anything.
type SCIMUser struct {
	ID         string    `json:"id"`
	UserName   string    `json:"userName"`
	ExternalID string    `json:"externalId,omitempty"`
	Active     bool      `json:"active"`
	Created    time.Time `json:"created"`
	Modified   time.Time `json:"modified"`
}

// Validate reports whether the user can be stored.
func (u SCIMUser) Validate() error {
	if u.ID == "" {
		return fmt.Errorf("scim user has no id: %w", ErrBadInput)
	}
	if u.UserName == "" {
		return fmt.Errorf("scim user %q has no userName: %w", u.ID, ErrBadInput)
	}
	return nil
}

// SCIMGroup is a provisioned group. DisplayName is the group name policies
// target; Members are SCIMUser IDs, sorted when read from a store.
type SCIMGroup struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	ExternalID  string    `json:"externalId,omitempty"`
	Members     []string  `json:"members"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
}

// Validate reports whether the group can be stored. Whether each member
// exists is the store's check, since only it can see the users.
func (g SCIMGroup) Validate() error {
	if g.ID == "" {
		return fmt.Errorf("scim group has no id: %w", ErrBadInput)
	}
	if g.DisplayName == "" {
		return fmt.Errorf("scim group %q has no displayName: %w", g.ID, ErrBadInput)
	}
	for _, m := range g.Members {
		if m == "" {
			return fmt.Errorf("scim group %q has an empty member id: %w", g.ID, ErrBadInput)
		}
	}
	return nil
}

// The attributes a SCIM list may be filtered on.
const (
	SCIMAttrUserName    = "userName"
	SCIMAttrDisplayName = "displayName"
	SCIMAttrExternalID  = "externalId"
)

// SCIMFilter selects list results by one attribute equal to one value. The
// zero value matches everything. Names compare case-insensitively, as the
// core schema declares them; externalId compares exactly.
type SCIMFilter struct {
	Attribute string
	Value     string
}

// SCIMUserChange is a PATCH on a user folded into the fields it sets; a nil
// field is left as it is.
type SCIMUserChange struct {
	UserName   *string
	ExternalID *string
	Active     *bool
}

// The kinds of member operation a group PATCH carries.
const (
	SCIMMembersAdd     = "add"
	SCIMMembersRemove  = "remove"
	SCIMMembersReplace = "replace"
)

// SCIMMemberOp is one member operation, applied in the order the IdP sent
// it: a replace followed by an add is not the same as the reverse.
type SCIMMemberOp struct {
	Kind  string
	Users []string
}

// SCIMGroupChange is a PATCH on a group. The store applies it in one
// transaction, so two concurrent member changes cannot lose a write.
type SCIMGroupChange struct {
	DisplayName *string
	ExternalID  *string
	Members     []SCIMMemberOp
}

// SCIMCounts summarises what SCIM has provisioned, for an operator.
type SCIMCounts struct {
	Users       int `json:"users"`
	ActiveUsers int `json:"activeUsers"`
	Groups      int `json:"groups"`
}
