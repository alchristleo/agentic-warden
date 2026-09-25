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

// Store holds policy revisions, enrolled machines, enrollment tokens and
// what SCIM has provisioned.
type Store interface {
	SCIMStore
	ConsoleStore

	// PutRuleSet stores a new revision and its audit event atomically. It
	// returns model.ErrBadInput for a revision without a version or an
	// audit event without an actor or action, and model.ErrConflict when
	// that version is already stored, because a revision is immutable once
	// written. Either failure stores neither.
	PutRuleSet(ctx context.Context, r model.Revision, audit model.AuditEvent) error
	// CurrentRuleSet returns the newest revision, or model.ErrNotFound when
	// no policy has been applied yet.
	CurrentRuleSet(ctx context.Context) (model.Revision, error)
	// Revisions returns up to limit revisions, newest first.
	Revisions(ctx context.Context, limit int) ([]model.Revision, error)

	// PutEnrollmentToken stores a token and its audit event atomically.
	PutEnrollmentToken(ctx context.Context, t model.EnrollmentToken, audit model.AuditEvent) error
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
	// keyID is the signing key the machine presented, empty when it pins
	// none; it is stored as given, so a machine that stops presenting one
	// stops reporting one.
	TouchMachine(ctx context.Context, id string, seenAt time.Time, bundleVersion, keyID string) error
	// ListMachines returns every machine in enrollment order with its
	// credential hash cleared: a listing is for operators, and the hash is
	// the lookup key, not information.
	ListMachines(ctx context.Context) ([]model.Machine, error)
	// DeleteMachine revokes a machine and records audit atomically, or
	// model.ErrNotFound, in which case no event is written.
	DeleteMachine(ctx context.Context, id string, audit model.AuditEvent) error

	// PutGroupSnapshot stores a snapshot and its audit event atomically.
	PutGroupSnapshot(ctx context.Context, s model.GroupSnapshot, audit model.AuditEvent) error
	// CurrentGroupSnapshot returns the newest snapshot, or model.ErrNotFound
	// when none has ever been posted.
	CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error)
}

// ConsoleStore holds console sessions and the audit log.
type ConsoleStore interface {
	// CreateSession stores a session. model.ErrBadInput without a token
	// hash or user; model.ErrConflict when the hash exists.
	CreateSession(ctx context.Context, s model.ConsoleSession) error
	// SessionByHash returns a session, or model.ErrNotFound.
	SessionByHash(ctx context.Context, hash string) (model.ConsoleSession, error)
	// TouchSession sets LastSeenAt, or model.ErrNotFound.
	TouchSession(ctx context.Context, hash string, at time.Time) error
	// DeleteSession removes a session. Deleting one that does not exist is
	// not an error: logout is idempotent.
	DeleteSession(ctx context.Context, hash string) error
	// DeleteSessionsFor removes every session of a user.
	DeleteSessionsFor(ctx context.Context, user string) error
	// DeleteExpiredSessions removes sessions created before createdBefore
	// or last seen before seenBefore, and returns how many it removed.
	DeleteExpiredSessions(ctx context.Context, createdBefore, seenBefore time.Time) (int, error)
	// RecordAudit stores an event that accompanies no other write (login,
	// logout). model.ErrBadInput when Validate fails.
	RecordAudit(ctx context.Context, e model.AuditEvent) error
	// AuditEvents returns up to limit events, newest first. before, when
	// positive, returns only events with a smaller ID.
	AuditEvents(ctx context.Context, limit int, before int64) ([]model.AuditEvent, error)
}

// SCIMStore holds what the identity provider provisioned over SCIM.
type SCIMStore interface {
	// CreateSCIMUser stores a provisioned user. model.ErrBadInput without an
	// ID or userName; model.ErrConflict when the ID exists or another user
	// has the same userName ignoring case.
	CreateSCIMUser(ctx context.Context, u model.SCIMUser) error
	// SCIMUser returns one user, or model.ErrNotFound.
	SCIMUser(ctx context.Context, id string) (model.SCIMUser, error)
	// ReplaceSCIMUser overwrites userName, externalId, active and Modified;
	// Created is kept. Errors as CreateSCIMUser, plus model.ErrNotFound.
	ReplaceSCIMUser(ctx context.Context, u model.SCIMUser) error
	// PatchSCIMUser applies c atomically, sets Modified to at and returns the
	// result. Errors as ReplaceSCIMUser.
	PatchSCIMUser(ctx context.Context, id string, c model.SCIMUserChange, at time.Time) (model.SCIMUser, error)
	// DeleteSCIMUser removes a user and every membership it had, or
	// model.ErrNotFound.
	DeleteSCIMUser(ctx context.Context, id string) error
	// ListSCIMUsers returns one page of the users f matches, ordered by
	// Created then ID, and the total number matched. startIndex is 1-based;
	// a page past the end is empty, never nil. model.ErrBadInput for a
	// filter attribute users do not have.
	ListSCIMUsers(ctx context.Context, f model.SCIMFilter, startIndex, count int) ([]model.SCIMUser, int, error)

	// CreateSCIMGroup stores a group. model.ErrBadInput without an ID or
	// displayName, or when a member is not a stored user; model.ErrConflict
	// when the ID exists or another group has the displayName ignoring case.
	CreateSCIMGroup(ctx context.Context, g model.SCIMGroup) error
	// SCIMGroup returns one group with its members sorted, or
	// model.ErrNotFound.
	SCIMGroup(ctx context.Context, id string) (model.SCIMGroup, error)
	// ReplaceSCIMGroup overwrites displayName, externalId, members and
	// Modified. Errors as CreateSCIMGroup, plus model.ErrNotFound.
	ReplaceSCIMGroup(ctx context.Context, g model.SCIMGroup) error
	// PatchSCIMGroup applies c in one transaction, member operations in
	// order, sets Modified to at and returns the result. Errors as
	// ReplaceSCIMGroup; on any error nothing changes.
	PatchSCIMGroup(ctx context.Context, id string, c model.SCIMGroupChange, at time.Time) (model.SCIMGroup, error)
	// DeleteSCIMGroup removes a group and its memberships, or
	// model.ErrNotFound.
	DeleteSCIMGroup(ctx context.Context, id string) error
	// ListSCIMGroups is ListSCIMUsers for groups. withMembers false skips
	// reading membership entirely, since an IdP paging a large directory
	// asks for groups without members and Postgres would otherwise run a
	// member query per page for nothing; each group's Members is then an
	// empty, non-nil slice rather than what it actually holds.
	ListSCIMGroups(ctx context.Context, f model.SCIMFilter, startIndex, count int, withMembers bool) ([]model.SCIMGroup, int, error)

	// SCIMGroupsFor returns the sorted display names of the groups holding
	// the active user whose userName equals userName exactly; an empty,
	// non-nil slice for an unknown or inactive user.
	SCIMGroupsFor(ctx context.Context, userName string) ([]string, error)
	// SCIMCounts counts provisioned users, active users and groups.
	SCIMCounts(ctx context.Context) (model.SCIMCounts, error)
}
