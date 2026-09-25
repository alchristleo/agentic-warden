package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

// RunConsole checks console sessions and the audit log.
func RunConsole(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(t *testing.T, s store.Store)
	}{
		{"a session can be read back by its hash", sessionRoundTrip},
		{"a session needs a hash and a user", sessionRequired},
		{"a session hash is unique", sessionHashUnique},
		{"an unknown session is not found", sessionUnknown},
		{"touching a session moves last seen", sessionTouch},
		{"deleting a session is idempotent", sessionDeleteIdempotent},
		{"deleting a user's sessions leaves others", sessionDeleteFor},
		{"expired sessions are swept by either bound", sessionSweep},
		{"audit events list newest first with a before cursor", auditList},
		{"an audit event needs an actor and an action", auditRequired},
		{"each audited write stores exactly one event", auditedWrites},
		{"a bad audit event leaves the paired write undone", auditRollback},
		{"revoking an unknown machine writes no event", auditNotFoundWritesNothing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.fn(t, newStore(t)) })
	}
}

func audit(action, target string) model.AuditEvent {
	return model.AuditEvent{At: epoch, Actor: "token:tester", Action: action, Target: target}
}

func session(hash, user string, created, seen time.Time) model.ConsoleSession {
	return model.ConsoleSession{TokenHash: hash, User: user, CreatedAt: created, LastSeenAt: seen}
}

func sessionRoundTrip(t *testing.T, s store.Store) {
	ctx := context.Background()
	want := session("h1", "alice@example.com", epoch, epoch)
	if err := s.CreateSession(ctx, want); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.SessionByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("SessionByHash: %v", err)
	}
	if got.User != want.User || !got.CreatedAt.Equal(epoch) || !got.LastSeenAt.Equal(epoch) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func sessionRequired(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, bad := range []model.ConsoleSession{session("", "a", epoch, epoch), session("h", "", epoch, epoch)} {
		if err := s.CreateSession(ctx, bad); !errors.Is(err, model.ErrBadInput) {
			t.Errorf("CreateSession(%+v) = %v, want ErrBadInput", bad, err)
		}
	}
}

func sessionHashUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.CreateSession(ctx, session("h1", "a", epoch, epoch)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, session("h1", "b", epoch, epoch)); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second CreateSession = %v, want ErrConflict", err)
	}
}

func sessionUnknown(t *testing.T, s store.Store) {
	ctx := context.Background()
	if _, err := s.SessionByHash(ctx, "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("SessionByHash = %v, want ErrNotFound", err)
	}
	if err := s.TouchSession(ctx, "nope", epoch); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("TouchSession = %v, want ErrNotFound", err)
	}
}

func sessionTouch(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.CreateSession(ctx, session("h1", "a", epoch, epoch)); err != nil {
		t.Fatal(err)
	}
	later := epoch.Add(5 * time.Minute)
	if err := s.TouchSession(ctx, "h1", later); err != nil {
		t.Fatal(err)
	}
	got, _ := s.SessionByHash(ctx, "h1")
	if !got.LastSeenAt.Equal(later) || !got.CreatedAt.Equal(epoch) {
		t.Fatalf("got %+v, want last seen %v and created %v", got, later, epoch)
	}
}

func sessionDeleteIdempotent(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.CreateSession(ctx, session("h1", "a", epoch, epoch)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.DeleteSession(ctx, "h1"); err != nil {
			t.Fatalf("DeleteSession #%d: %v", i+1, err)
		}
	}
	if _, err := s.SessionByHash(ctx, "h1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("after delete = %v, want ErrNotFound", err)
	}
}

func sessionDeleteFor(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, sess := range []model.ConsoleSession{
		session("a1", "alice", epoch, epoch), session("a2", "alice", epoch, epoch), session("b1", "bob", epoch, epoch),
	} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteSessionsFor(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"a1", "a2"} {
		if _, err := s.SessionByHash(ctx, h); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s survived: %v", h, err)
		}
	}
	if _, err := s.SessionByHash(ctx, "b1"); err != nil {
		t.Errorf("bob's session went too: %v", err)
	}
}

func sessionSweep(t *testing.T, s store.Store) {
	ctx := context.Background()
	old := epoch.Add(-9 * time.Hour)
	for _, sess := range []model.ConsoleSession{
		session("fresh", "a", epoch, epoch),
		session("too-old", "a", old, epoch),                  // past the absolute bound
		session("idle", "a", epoch, epoch.Add(-2*time.Hour)), // past the idle bound
	} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteExpiredSessions(ctx, epoch.Add(-8*time.Hour), epoch.Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("DeleteExpiredSessions = %d, %v; want 2, nil", n, err)
	}
	if _, err := s.SessionByHash(ctx, "fresh"); err != nil {
		t.Fatalf("fresh session swept: %v", err)
	}
}

func auditList(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, target := range []string{"one", "two", "three"} {
		if err := s.RecordAudit(ctx, audit(model.AuditLogin, target)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.AuditEvents(ctx, 10, 0)
	if err != nil || len(all) != 3 || all[0].Target != "three" || all[2].Target != "one" {
		t.Fatalf("AuditEvents = %+v, %v", all, err)
	}
	if all[0].ID <= all[1].ID {
		t.Fatalf("ids not decreasing: %d, %d", all[0].ID, all[1].ID)
	}
	page, err := s.AuditEvents(ctx, 1, all[0].ID)
	if err != nil || len(page) != 1 || page[0].Target != "two" {
		t.Fatalf("page after %d = %+v, %v", all[0].ID, page, err)
	}
	empty, err := s.AuditEvents(ctx, 10, all[2].ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("past the end = %#v, %v; want empty non-nil slice", empty, err)
	}
}

func auditRequired(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, bad := range []model.AuditEvent{{Action: "x"}, {Actor: "x"}} {
		if err := s.RecordAudit(ctx, bad); !errors.Is(err, model.ErrBadInput) {
			t.Errorf("RecordAudit(%+v) = %v, want ErrBadInput", bad, err)
		}
	}
}

func auditedWrites(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutRuleSet(ctx, revision("v1", epoch), audit(model.AuditRevisionCreate, "v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutEnrollmentToken(ctx, token("t1", "alice", epoch.Add(time.Hour)), audit(model.AuditTokenCreate, "alice")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMachine(ctx, machine("m1", "alice", "c1", epoch)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, "m1", audit(model.AuditMachineRevoke, "m1")); err != nil {
		t.Fatal(err)
	}
	withDetail := audit(model.AuditGroupsApply, "")
	withDetail.Detail = map[string]any{"source": "okta", "users": float64(1)}
	if err := s.PutGroupSnapshot(ctx, snapshot("okta", epoch, map[string][]string{"alice": {"devs"}}), withDetail); err != nil {
		t.Fatal(err)
	}
	events, err := s.AuditEvents(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	want := []string{model.AuditGroupsApply, model.AuditMachineRevoke, model.AuditTokenCreate, model.AuditRevisionCreate}
	if !equalStrings(actions, want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	if events[0].Detail["source"] != "okta" || events[0].Detail["users"] != float64(1) {
		t.Fatalf("detail round trip = %#v", events[0].Detail)
	}
}

// auditRollback proves the pairing is atomic. Postgres must not pre-check
// the event in Go: the CHECK constraint fails the insert inside the
// transaction, so this case exercises a real rollback.
func auditRollback(t *testing.T, s store.Store) {
	ctx := context.Background()
	bad := model.AuditEvent{Actor: "token:tester"} // no action
	if err := s.PutRuleSet(ctx, revision("v1", epoch), bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("PutRuleSet with bad audit = %v, want ErrBadInput", err)
	}
	if _, err := s.CurrentRuleSet(ctx); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("revision stored despite failed audit: %v", err)
	}
	if err := s.PutMachine(ctx, machine("m1", "alice", "c1", epoch)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, "m1", bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("DeleteMachine with bad audit = %v, want ErrBadInput", err)
	}
	if _, err := s.MachineByCredential(ctx, "c1"); err != nil {
		t.Fatalf("machine deleted despite failed audit: %v", err)
	}
	if err := s.PutEnrollmentToken(ctx, token("t1", "alice", epoch.Add(time.Hour)), bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("PutEnrollmentToken with bad audit = %v", err)
	}
	if _, err := s.ConsumeEnrollmentToken(ctx, "t1", epoch); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("token stored despite failed audit: %v", err)
	}
	if err := s.PutGroupSnapshot(ctx, snapshot("okta", epoch, map[string][]string{"a": {"g"}}), bad); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("PutGroupSnapshot with bad audit = %v", err)
	}
	if _, err := s.CurrentGroupSnapshot(ctx); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("snapshot stored despite failed audit: %v", err)
	}
	if events, _ := s.AuditEvents(ctx, 10, 0); len(events) != 0 {
		t.Fatalf("events written: %+v", events)
	}
}

func auditNotFoundWritesNothing(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.DeleteMachine(ctx, "ghost", audit(model.AuditMachineRevoke, "ghost")); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("DeleteMachine = %v, want ErrNotFound", err)
	}
	if events, _ := s.AuditEvents(ctx, 10, 0); len(events) != 0 {
		t.Fatalf("events written for a missing machine: %+v", events)
	}
}
