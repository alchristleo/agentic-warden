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
	// ErrUnauthorized means the caller presented no valid credential.
	ErrUnauthorized = errors.New("unauthorized")
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

// Machine is a device enrolled for one user. It fetches that user's bundle
// with a credential shown once at enrollment; only the credential's hash is
// stored.
type Machine struct {
	ID   string `json:"id"`
	User string `json:"user"`
	// Name is what the machine called itself at enrollment, for listings.
	Name string `json:"name,omitempty"`
	// OS is the machine's GOOS, so an operator can see which files aw-sync
	// writes there.
	OS             string    `json:"os,omitempty"`
	CredentialHash string    `json:"-"`
	EnrolledAt     time.Time `json:"enrolledAt"`
	// LastSeenAt is the last successful bundle fetch. Zero until the first
	// fetch, and serialised as the zero time then; readers test IsZero.
	LastSeenAt time.Time `json:"lastSeenAt"`
	// LastBundleVersion is the policy version served at that fetch.
	LastBundleVersion string `json:"lastBundleVersion,omitempty"`
}

// EnrollmentToken lets one machine enroll for one user, once, before it
// expires. Only the token's hash is stored.
type EnrollmentToken struct {
	Hash      string
	User      string
	ExpiresAt time.Time
	// UsedAt is zero until the token is consumed.
	UsedAt time.Time
}
