// Package model holds the control plane's domain types and the errors its
// layers agree on.
//
// The sentinel errors are the vocabulary a store speaks and a handler
// translates into HTTP status codes. Keeping them here means no layer below
// the handler has to know what an HTTP status is.
package model

import (
	"errors"
	"time"

	"github.com/acme/agent-wrapper/internal/policy"
)

var (
	// ErrNotFound means the requested thing does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means the request collides with what is already stored.
	ErrConflict = errors.New("conflict")
	// ErrBadInput means the request is malformed or incomplete.
	ErrBadInput = errors.New("bad input")
	// ErrForbidden means the caller is known but not allowed.
	ErrForbidden = errors.New("forbidden")
	// ErrOverBudget means the caller's team has spent its allowance.
	ErrOverBudget = errors.New("over budget")
)

// Revision is one stored, immutable version of the organization's policy.
//
// Policy is append-only: applying a change stores a new revision rather than
// editing the last one, so a rollback is a lookup and an audit reader can see
// exactly what was in force at any point.
type Revision struct {
	// Seq orders revisions. The store assigns it, increasing with every
	// revision written. A timestamp cannot order two revisions applied within
	// the same clock tick, and that tie decides which policy is current.
	Seq int64 `json:"seq"`
	// Version identifies the revision and is what clients cache on.
	Version string `json:"version"`
	// RuleSet is the authored policy this revision captured.
	RuleSet policy.RuleSet `json:"ruleSet"`
	// CreatedAt is when the revision was stored.
	CreatedAt time.Time `json:"createdAt"`
	// CreatedBy identifies who applied it.
	CreatedBy string `json:"createdBy,omitempty"`
}
