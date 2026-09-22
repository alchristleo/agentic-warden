// Package model holds the control plane's domain types and the errors its
// layers agree on.
//
// The sentinel errors are the vocabulary a store speaks and a handler
// translates into HTTP status codes. Keeping them here means no layer below
// the handler has to know what an HTTP status is.
package model

import (
	"errors"
	"fmt"
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

// GroupSnapshot is one export of identity-provider memberships. It replaces
// the previous snapshot whole: a user absent from it has no synced groups,
// which is what a cron export means and what makes a snapshot auditable.
type GroupSnapshot struct {
	// Seq orders snapshots, assigned by the store as for revisions.
	Seq int64 `json:"seq"`
	// Source names the exporter, for the operator reading a listing.
	Source string `json:"source"`
	// AppliedBy is who posted it, from the client's environment.
	AppliedBy string `json:"appliedBy,omitempty"`
	// SyncedAt is when the server stored it.
	SyncedAt time.Time `json:"syncedAt"`
	// Members maps a user, exactly as the enrollment names them, to their
	// groups. No case folding: an export whose keys differ from the
	// enrolled emails is an export to fix, not to paper over.
	Members map[string][]string `json:"members"`
}

// Validate reports whether every user key and group name is present. A
// snapshot with an empty key would silently apply to nobody.
func (s GroupSnapshot) Validate() error {
	for user, groups := range s.Members {
		if user == "" {
			return fmt.Errorf("group snapshot has a member with an empty user: %w", ErrBadInput)
		}
		for _, g := range groups {
			if g == "" {
				return fmt.Errorf("group snapshot: user %q has an empty group name: %w", user, ErrBadInput)
			}
		}
	}
	return nil
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
	// LastKeyID is the signing key the machine presented at that fetch, so
	// an operator can see which machines have picked up a rotation. Empty
	// for a machine that pins no key.
	LastKeyID string `json:"lastKeyId,omitempty"`
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
