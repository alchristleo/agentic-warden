package authz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

var t0 = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

func audit() model.AuditEvent { return model.AuditEvent{Actor: "token:t", Action: "x"} }

func TestNoGroupData(t *testing.T) {
	c := &authz.Checker{Store: store.NewMemory(), Group: "console-admins"}
	if _, err := c.Admin(context.Background(), "alice"); !errors.Is(err, authz.ErrNoGroupData) {
		t.Fatalf("err = %v, want ErrNoGroupData", err)
	}
}

func TestSnapshotMembership(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	_ = s.PutGroupSnapshot(ctx, model.GroupSnapshot{Source: "t", SyncedAt: t0,
		Members: map[string][]string{"alice": {"console-admins"}, "bob": {"devs"}}}, audit())
	c := &authz.Checker{Store: s, Group: "console-admins"}
	for user, want := range map[string]bool{"alice": true, "bob": false, "Alice": false, "carol": false} {
		got, err := c.Admin(ctx, user)
		if err != nil || got != want {
			t.Errorf("Admin(%q) = %v, %v; want %v", user, got, err, want)
		}
	}
}

func TestSCIMMembership(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	_ = s.CreateSCIMUser(ctx, model.SCIMUser{ID: "u1", UserName: "alice", Active: true, Created: t0, Modified: t0})
	_ = s.CreateSCIMUser(ctx, model.SCIMUser{ID: "u2", UserName: "gone", Active: false, Created: t0, Modified: t0})
	_ = s.CreateSCIMGroup(ctx, model.SCIMGroup{ID: "g1", DisplayName: "console-admins", Members: []string{"u1", "u2"}, Created: t0, Modified: t0})
	c := &authz.Checker{Store: s, Group: "console-admins"}
	if ok, err := c.Admin(ctx, "alice"); !ok || err != nil {
		t.Fatalf("alice = %v, %v", ok, err)
	}
	if ok, err := c.Admin(ctx, "gone"); ok || err != nil {
		t.Fatalf("deactivated user admitted: %v, %v", ok, err)
	}
}

// Authored groups live in the policy an admin edits; counting them would let
// a revision grant its own author console access.
func TestAuthoredGroupsNeverCount(t *testing.T) {
	s := store.NewMemory()
	ctx := context.Background()
	_ = s.PutGroupSnapshot(ctx, model.GroupSnapshot{Source: "t", SyncedAt: t0, Members: map[string][]string{"bob": {"devs"}}}, audit())
	_ = s.PutRuleSet(ctx, model.Revision{Version: "v1", RuleSet: policy.RuleSet{Version: "v1",
		Groups: map[string][]string{"console-admins": {"mallory"}}}}, audit())
	c := &authz.Checker{Store: s, Group: "console-admins"}
	if ok, err := c.Admin(ctx, "mallory"); ok || err != nil {
		t.Fatalf("authored membership admitted: %v, %v", ok, err)
	}
}
