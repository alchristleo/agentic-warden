package model

import (
	"fmt"
	"time"
)

// ConsoleSession is one signed-in console user. The store holds only the
// hash of the session token, as it does for machine credentials, so a
// database read never yields a usable cookie.
type ConsoleSession struct {
	TokenHash  string
	User       string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Audit actions. Each admin change writes exactly one event, in the same
// transaction as the change.
const (
	AuditRevisionCreate = "policy.revision.create"
	AuditTokenCreate    = "enrollment_token.create"
	AuditMachineRevoke  = "machine.revoke"
	AuditGroupsApply    = "groups.snapshot.apply"
	AuditLogin          = "console.login"
	AuditLoginDenied    = "console.login_denied"
	AuditLogout         = "console.logout"
)

// AuditEvent records who changed what. Actor is the SSO user, or
// "token:<applied-by>" for a request made with the admin token.
type AuditEvent struct {
	ID     int64          `json:"id"`
	At     time.Time      `json:"at"`
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	Target string         `json:"target,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// Validate reports whether the event names an actor and an action. An
// event without either would be an audit row nobody can read.
func (e AuditEvent) Validate() error {
	if e.Actor == "" {
		return fmt.Errorf("audit event has no actor: %w", ErrBadInput)
	}
	if e.Action == "" {
		return fmt.Errorf("audit event has no action: %w", ErrBadInput)
	}
	return nil
}
