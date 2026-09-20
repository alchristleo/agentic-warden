// Package store persists the control plane's state.
//
// Every implementation must pass the conformance suite in
// internal/store/storetest, which is the real specification of this interface.
package store

import (
	"context"

	"github.com/acme/agent-wrapper/internal/model"
)

// Store holds policy revisions.
type Store interface {
	// PutRuleSet stores a new revision. It returns model.ErrBadInput for a
	// revision without a version and model.ErrConflict when that version is
	// already stored, because a revision is immutable once written.
	PutRuleSet(ctx context.Context, r model.Revision) error
	// CurrentRuleSet returns the newest revision, or model.ErrNotFound when
	// no policy has been applied yet.
	CurrentRuleSet(ctx context.Context) (model.Revision, error)
	// Revisions returns up to limit revisions, newest first.
	Revisions(ctx context.Context, limit int) ([]model.Revision, error)
}
