// Package storetest holds the behavior every store.Store must exhibit.
//
// It lives outside the store package so any implementation, in this module or
// another, can run the same suite and prove it behaves identically.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

// Factory builds a fresh, empty store for one test.
type Factory func(t *testing.T) store.Store

// Run executes the conformance suite against the store the factory builds.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(t *testing.T, s store.Store)
	}{
		{"current rule set on an empty store reports not found", currentOnEmpty},
		{"a stored rule set can be read back", putThenCurrent},
		{"the newest revision is the current one", newestWins},
		{"storing the same version twice is a conflict", duplicateVersion},
		{"a revision without a version is rejected", versionRequired},
		{"revisions list newest first", listNewestFirst},
		{"listing is limited", listLimit},
		{"listing an empty store yields an empty slice", listEmpty},
		{"revisions stored in the same instant keep their applied order", sameInstantOrder},
		{"the store assigns an increasing sequence number", sequenceAssigned},
		{"an enrollment token is consumed once", tokenConsumedOnce},
		{"an unknown enrollment token is not found", tokenUnknown},
		{"an expired enrollment token is a conflict", tokenExpired},
		{"a machine can be found by its credential hash", machineByCredential},
		{"a machine needs an id and a credential hash", machineRequiresIDAndHash},
		{"a machine id is unique", machineIDUnique},
		{"a credential hash is unique", machineHashUnique},
		{"an enrollment token hash is unique", tokenHashUnique},
		{"touching a machine records the fetch", machineTouch},
		{"machines list in enrollment order", machinesList},
		{"listing no machines yields an empty slice", machinesListEmpty},
		{"a deleted machine is gone", machineDelete},
		{"current group snapshot on an empty store reports not found", snapshotOnEmpty},
		{"a stored group snapshot can be read back", snapshotPutThenCurrent},
		{"the newest group snapshot is the current one", snapshotNewestWins},
		{"a group snapshot with an empty user key is rejected", snapshotUserKeyRequired},
		{"a group snapshot with an empty group name is rejected", snapshotGroupNameRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
	RunSCIM(t, func(t *testing.T) store.SCIMStore { return newStore(t) })
	RunConsole(t, newStore)
}

func revision(version string, at time.Time) model.Revision {
	return model.Revision{
		Version:   version,
		CreatedAt: at,
		CreatedBy: "tester",
		RuleSet: policy.RuleSet{
			Version: version,
			Rules: []policy.Rule{{
				Name:   "baseline",
				Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "sonnet"}}},
			}},
		},
	}
}

var epoch = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func currentOnEmpty(t *testing.T, s store.Store) {
	_, err := s.CurrentRuleSet(context.Background())
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("CurrentRuleSet() error = %v, want ErrNotFound", err)
	}
}

func putThenCurrent(t *testing.T, s store.Store) {
	ctx := context.Background()
	want := revision("v1", epoch)
	if err := s.PutRuleSet(ctx, want, audit(model.AuditRevisionCreate, want.Version)); err != nil {
		t.Fatalf("PutRuleSet: %v", err)
	}

	got, err := s.CurrentRuleSet(ctx)
	if err != nil {
		t.Fatalf("CurrentRuleSet: %v", err)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if len(got.RuleSet.Rules) != 1 || got.RuleSet.Rules[0].Name != "baseline" {
		t.Errorf("RuleSet = %+v, want the stored rules", got.RuleSet)
	}
	if got.CreatedBy != "tester" {
		t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, "tester")
	}
}

func newestWins(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutRuleSet(ctx, revision("v1", epoch), audit(model.AuditRevisionCreate, "v1")); err != nil {
		t.Fatalf("PutRuleSet v1: %v", err)
	}
	if err := s.PutRuleSet(ctx, revision("v2", epoch.Add(time.Hour)), audit(model.AuditRevisionCreate, "v2")); err != nil {
		t.Fatalf("PutRuleSet v2: %v", err)
	}

	got, err := s.CurrentRuleSet(ctx)
	if err != nil {
		t.Fatalf("CurrentRuleSet: %v", err)
	}
	if got.Version != "v2" {
		t.Errorf("Version = %q, want the newest revision", got.Version)
	}
}

func duplicateVersion(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutRuleSet(ctx, revision("v1", epoch), audit(model.AuditRevisionCreate, "v1")); err != nil {
		t.Fatalf("PutRuleSet: %v", err)
	}

	err := s.PutRuleSet(ctx, revision("v1", epoch.Add(time.Hour)), audit(model.AuditRevisionCreate, "v1"))

	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("PutRuleSet() error = %v, want ErrConflict", err)
	}
}

func versionRequired(t *testing.T, s store.Store) {
	err := s.PutRuleSet(context.Background(), revision("", epoch), audit(model.AuditRevisionCreate, ""))

	if !errors.Is(err, model.ErrBadInput) {
		t.Errorf("PutRuleSet() error = %v, want ErrBadInput", err)
	}
}

func listNewestFirst(t *testing.T, s store.Store) {
	ctx := context.Background()
	for i, version := range []string{"v1", "v2", "v3"} {
		if err := s.PutRuleSet(ctx, revision(version, epoch.Add(time.Duration(i)*time.Hour)), audit(model.AuditRevisionCreate, version)); err != nil {
			t.Fatalf("PutRuleSet %s: %v", version, err)
		}
	}

	got, err := s.Revisions(ctx, 10)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d revisions, want 3", len(got))
	}
	for i, want := range []string{"v3", "v2", "v1"} {
		if got[i].Version != want {
			t.Errorf("revision %d = %q, want %q", i, got[i].Version, want)
		}
	}
}

func listLimit(t *testing.T, s store.Store) {
	ctx := context.Background()
	for i, version := range []string{"v1", "v2", "v3"} {
		if err := s.PutRuleSet(ctx, revision(version, epoch.Add(time.Duration(i)*time.Hour)), audit(model.AuditRevisionCreate, version)); err != nil {
			t.Fatalf("PutRuleSet %s: %v", version, err)
		}
	}

	got, err := s.Revisions(ctx, 2)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}

	if len(got) != 2 || got[0].Version != "v3" {
		t.Errorf("Revisions(2) = %v, want the two newest", versions(got))
	}
}

// sameInstantOrder pins the tiebreak. A timestamp alone cannot order two
// revisions applied within the same clock tick, and getting this wrong means
// a rollback serves the revision it was rolling back from.
func sameInstantOrder(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, version := range []string{"v1", "v2", "v3"} {
		if err := s.PutRuleSet(ctx, revision(version, epoch), audit(model.AuditRevisionCreate, version)); err != nil {
			t.Fatalf("PutRuleSet %s: %v", version, err)
		}
	}

	got, err := s.Revisions(ctx, 10)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d revisions, want 3", len(got))
	}
	for i, want := range []string{"v3", "v2", "v1"} {
		if got[i].Version != want {
			t.Errorf("revision %d = %q, want %q (order = %v)", i, got[i].Version, want, versions(got))
		}
	}

	current, err := s.CurrentRuleSet(ctx)
	if err != nil {
		t.Fatalf("CurrentRuleSet: %v", err)
	}
	if current.Version != "v3" {
		t.Errorf("CurrentRuleSet() = %q, want the last one applied", current.Version)
	}
}

func sequenceAssigned(t *testing.T, s store.Store) {
	ctx := context.Background()
	for _, version := range []string{"v1", "v2"} {
		if err := s.PutRuleSet(ctx, revision(version, epoch), audit(model.AuditRevisionCreate, version)); err != nil {
			t.Fatalf("PutRuleSet %s: %v", version, err)
		}
	}

	got, err := s.Revisions(ctx, 10)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}

	if got[0].Seq <= got[1].Seq {
		t.Errorf("sequences = %d, %d; want the newer revision to carry the higher one", got[0].Seq, got[1].Seq)
	}
	if got[1].Seq == 0 {
		t.Error("the store left Seq unassigned")
	}
}

func listEmpty(t *testing.T, s store.Store) {
	got, err := s.Revisions(context.Background(), 10)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if got == nil {
		t.Error("Revisions() = nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("Revisions() = %v, want none", versions(got))
	}
}

func versions(revisions []model.Revision) []string {
	out := make([]string, 0, len(revisions))
	for _, r := range revisions {
		out = append(out, r.Version)
	}
	return out
}

func token(hash, user string, expires time.Time) model.EnrollmentToken {
	return model.EnrollmentToken{Hash: hash, User: user, ExpiresAt: expires}
}

func machine(id, user, hash string, at time.Time) model.Machine {
	return model.Machine{ID: id, User: user, Name: "laptop-" + id, OS: "linux", CredentialHash: hash, EnrolledAt: at}
}

func tokenConsumedOnce(t *testing.T, s store.Store) {
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutEnrollmentToken(ctx, token("h1", "alice@acme.com", now.Add(time.Hour)), audit(model.AuditTokenCreate, "alice@acme.com")); err != nil {
		t.Fatalf("PutEnrollmentToken: %v", err)
	}

	user, err := s.ConsumeEnrollmentToken(ctx, "h1", now)
	if err != nil || user != "alice@acme.com" {
		t.Fatalf("first consume = %q, %v; want alice and nil", user, err)
	}
	_, err = s.ConsumeEnrollmentToken(ctx, "h1", now)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("second consume error = %v, want ErrConflict: a token enrolls one machine", err)
	}
}

func tokenUnknown(t *testing.T, s store.Store) {
	_, err := s.ConsumeEnrollmentToken(context.Background(), "nope", time.Now())
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func tokenExpired(t *testing.T, s store.Store) {
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutEnrollmentToken(ctx, token("h1", "alice@acme.com", now), audit(model.AuditTokenCreate, "alice@acme.com")); err != nil {
		t.Fatal(err)
	}
	_, err := s.ConsumeEnrollmentToken(ctx, "h1", now)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("consuming at the expiry instant: error = %v, want ErrConflict", err)
	}
}

// tokenHashUnique pins that a second token minted with a colliding hash is a
// conflict, not a silent overwrite: overwriting would let the first token's
// plaintext, already handed to an administrator, enroll the second token's
// user instead.
func tokenHashUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutEnrollmentToken(ctx, token("h1", "alice@acme.com", now.Add(time.Hour)), audit(model.AuditTokenCreate, "alice@acme.com")); err != nil {
		t.Fatalf("PutEnrollmentToken: %v", err)
	}

	err := s.PutEnrollmentToken(ctx, token("h1", "bob@acme.com", now.Add(time.Hour)), audit(model.AuditTokenCreate, "bob@acme.com"))

	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("PutEnrollmentToken() error = %v, want ErrConflict", err)
	}

	user, err := s.ConsumeEnrollmentToken(ctx, "h1", now)
	if err != nil || user != "alice@acme.com" {
		t.Errorf("consume after the duplicate = %q, %v; want alice, the first token's user", user, err)
	}
}

func machineByCredential(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutMachine(ctx, machine("m1", "alice@acme.com", "c1", at)); err != nil {
		t.Fatalf("PutMachine: %v", err)
	}

	got, err := s.MachineByCredential(ctx, "c1")
	if err != nil {
		t.Fatalf("MachineByCredential: %v", err)
	}
	if got.ID != "m1" || got.User != "alice@acme.com" || got.Name != "laptop-m1" || got.OS != "linux" || !got.EnrolledAt.Equal(at) {
		t.Errorf("machine = %+v", got)
	}
	if _, err := s.MachineByCredential(ctx, "c2"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("unknown credential: error = %v, want ErrNotFound", err)
	}
}

func machineRequiresIDAndHash(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutMachine(ctx, model.Machine{User: "a", CredentialHash: "c"}); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("no id: error = %v, want ErrBadInput", err)
	}
	if err := s.PutMachine(ctx, model.Machine{ID: "m", User: "a"}); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("no hash: error = %v, want ErrBadInput", err)
	}
}

func machineIDUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Now()
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", at)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMachine(ctx, machine("m1", "b", "c2", at)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("error = %v, want ErrConflict", err)
	}
}

func machineHashUnique(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Now()
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", at)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMachine(ctx, machine("m2", "b", "c1", at)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("error = %v, want ErrConflict: two machines must never share a credential", err)
	}
}

func machineTouch(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", at)); err != nil {
		t.Fatal(err)
	}
	seen := at.Add(time.Hour)
	if err := s.TouchMachine(ctx, "m1", seen, "v7", "3f9a1c22b0d41e77"); err != nil {
		t.Fatalf("TouchMachine: %v", err)
	}
	got, _ := s.MachineByCredential(ctx, "c1")
	if !got.LastSeenAt.Equal(seen) || got.LastBundleVersion != "v7" || got.LastKeyID != "3f9a1c22b0d41e77" {
		t.Errorf("after touch: %+v", got)
	}
	// An unsigned deployment sends no key ID, and a machine that stops
	// sending one must not appear to be still pinning the old key.
	if err := s.TouchMachine(ctx, "m1", seen, "v8", ""); err != nil {
		t.Fatalf("TouchMachine: %v", err)
	}
	if got, _ := s.MachineByCredential(ctx, "c1"); got.LastKeyID != "" {
		t.Fatalf("LastKeyID = %q, want it cleared", got.LastKeyID)
	}
	if err := s.TouchMachine(ctx, "missing", seen, "v7", ""); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("touching an unknown machine: error = %v, want ErrNotFound", err)
	}
}

func machinesList(t *testing.T, s store.Store) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"m3", "m1", "m2"} {
		if err := s.PutMachine(ctx, machine(id, "a", "c"+id, at.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListMachines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.ID)
		if m.CredentialHash != "" {
			t.Errorf("listing leaks a credential hash for %s", m.ID)
		}
	}
	if want := []string{"m3", "m1", "m2"}; !equalStrings(ids, want) {
		t.Errorf("ids = %v, want enrollment order %v", ids, want)
	}
}

func machinesListEmpty(t *testing.T, s store.Store) {
	got, err := s.ListMachines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("ListMachines on an empty store = %v, want an empty non-nil slice", got)
	}
}

func machineDelete(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.PutMachine(ctx, machine("m1", "a", "c1", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMachine(ctx, "m1", audit(model.AuditMachineRevoke, "m1")); err != nil {
		t.Fatalf("DeleteMachine: %v", err)
	}
	if _, err := s.MachineByCredential(ctx, "c1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("after delete: error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteMachine(ctx, "m1", audit(model.AuditMachineRevoke, "m1")); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("deleting twice: error = %v, want ErrNotFound", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func snapshot(source string, at time.Time, members map[string][]string) model.GroupSnapshot {
	return model.GroupSnapshot{Source: source, AppliedBy: "ops", SyncedAt: at, Members: members}
}

func snapshotOnEmpty(t *testing.T, s store.Store) {
	_, err := s.CurrentGroupSnapshot(context.Background())
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound before any snapshot is posted", err)
	}
}

func snapshotPutThenCurrent(t *testing.T, s store.Store) {
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	want := snapshot("okta", at, map[string][]string{"alice@acme.com": {"platform", "oncall"}})
	if err := s.PutGroupSnapshot(context.Background(), want, audit(model.AuditGroupsApply, "")); err != nil {
		t.Fatal(err)
	}

	got, err := s.CurrentGroupSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "okta" || got.AppliedBy != "ops" || !got.SyncedAt.Equal(at) {
		t.Errorf("snapshot header = %+v", got)
	}
	if !equalStrings(got.Members["alice@acme.com"], []string{"platform", "oncall"}) {
		t.Errorf("members = %v", got.Members)
	}
}

func snapshotNewestWins(t *testing.T, s store.Store) {
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	first := snapshot("okta", at, map[string][]string{"alice@acme.com": {"platform"}})
	second := snapshot("okta", at.Add(time.Hour), map[string][]string{"bob@acme.com": {"mobile"}})
	for _, snap := range []model.GroupSnapshot{first, second} {
		if err := s.PutGroupSnapshot(context.Background(), snap, audit(model.AuditGroupsApply, "")); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.CurrentGroupSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, alice := got.Members["alice@acme.com"]; alice {
		t.Error("a snapshot replaces the previous one whole; alice should be gone")
	}
	if !equalStrings(got.Members["bob@acme.com"], []string{"mobile"}) {
		t.Errorf("members = %v", got.Members)
	}
}

func snapshotUserKeyRequired(t *testing.T, s store.Store) {
	err := s.PutGroupSnapshot(context.Background(), snapshot("okta", time.Now(), map[string][]string{"": {"platform"}}), audit(model.AuditGroupsApply, ""))
	if !errors.Is(err, model.ErrBadInput) {
		t.Errorf("err = %v, want ErrBadInput", err)
	}
}

func snapshotGroupNameRequired(t *testing.T, s store.Store) {
	err := s.PutGroupSnapshot(context.Background(), snapshot("okta", time.Now(), map[string][]string{"alice@acme.com": {"platform", ""}}), audit(model.AuditGroupsApply, ""))
	if !errors.Is(err, model.ErrBadInput) {
		t.Errorf("err = %v, want ErrBadInput", err)
	}
}
