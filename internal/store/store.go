// Package store persists the control plane's state.
//
// Every implementation must pass the conformance suite in
// internal/store/storetest, which is the real specification of this interface.
package store

import (
	"context"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// Store holds policy revisions, enrolled machines and enrollment tokens.
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

	// PutEnrollmentToken stores a token for later consumption.
	PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken) error
	// ConsumeEnrollmentToken marks the token used and returns the user it
	// enrolls. It returns model.ErrNotFound for an unknown hash and
	// model.ErrConflict for a token already used or expired at now. The
	// check and the mark are one atomic step, so two machines racing on one
	// token cannot both enroll.
	ConsumeEnrollmentToken(ctx context.Context, hash string, now time.Time) (user string, err error)

	// PutMachine stores an enrolled machine. It returns model.ErrBadInput
	// without an ID or credential hash and model.ErrConflict when either is
	// already stored.
	PutMachine(ctx context.Context, m model.Machine) error
	// MachineByCredential finds the machine holding this credential hash,
	// or model.ErrNotFound.
	MachineByCredential(ctx context.Context, hash string) (model.Machine, error)
	// TouchMachine records a successful bundle fetch, or model.ErrNotFound.
	TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion string) error
	// ListMachines returns every machine in enrollment order with its
	// credential hash cleared: a listing is for operators, and the hash is
	// the lookup key, not information.
	ListMachines(ctx context.Context) ([]model.Machine, error)
	// DeleteMachine revokes a machine, or model.ErrNotFound.
	DeleteMachine(ctx context.Context, id string) error

	// PutGroupSnapshot stores a membership snapshot; the newest is current.
	// It returns model.ErrBadInput for an empty user key or group name.
	PutGroupSnapshot(ctx context.Context, s model.GroupSnapshot) error
	// CurrentGroupSnapshot returns the newest snapshot, or model.ErrNotFound
	// when none has ever been posted.
	CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error)
}
