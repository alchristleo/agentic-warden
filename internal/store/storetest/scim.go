package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

// RunSCIM executes the SCIM conformance cases against the store the
// factory builds. It is separate from Run while SCIMStore is its own
// interface; once Store embeds it, Run calls it too.
func RunSCIM(t *testing.T, newStore func(t *testing.T) store.SCIMStore) {
	t.Helper()
	for _, tc := range scimCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
}

// scimCases is the SCIM part of the conformance suite.
var scimCases = []struct {
	name string
	fn   func(t *testing.T, s store.SCIMStore)
}{
	{"a scim user can be created and read back", scimUserRoundTrip},
	{"a scim user needs an id and a userName", scimUserRequired},
	{"a scim userName is unique ignoring case", scimUserNameUnique},
	{"replacing a scim user keeps its created time", scimUserReplace},
	{"patching a scim user changes only what it names", scimUserPatch},
	{"a patch cannot take another user's userName", scimUserPatchConflict},
	{"an unknown scim user is not found", scimUserUnknown},
	{"deleting a scim user removes its memberships", scimUserDeleteCascades},
	{"scim users list by filter and page stably", scimUserList},
	{"a scim group can be created with members and read back", scimGroupRoundTrip},
	{"a scim group member must be a stored user", scimGroupMemberMustExist},
	{"a scim displayName is unique ignoring case", scimGroupNameUnique},
	{"replacing a scim group replaces its members and keeps created", scimGroupReplace},
	{"a group patch applies member operations in order", scimGroupPatchOrder},
	{"a failed group patch changes nothing", scimGroupPatchAtomic},
	{"concurrent member patches lose nothing", scimGroupPatchConcurrent},
	{"deleting a scim group removes its memberships", scimGroupDelete},
	{"scim groups list by filter", scimGroupList},
	{"scim groups list without members omits them", scimGroupListWithoutMembers},
	{"scim groups for a user are those of the active user only", scimGroupsForActiveOnly},
	{"scim groups for match the userName exactly", scimGroupsForExact},
	{"scim counts", scimCountsCase},
}

var scimAt = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

func scimUser(id, name string, created time.Time) model.SCIMUser {
	return model.SCIMUser{ID: id, UserName: name, ExternalID: "ext-" + id, Active: true, Created: created, Modified: created}
}

func scimGroup(id, name string, members ...string) model.SCIMGroup {
	return model.SCIMGroup{ID: id, DisplayName: name, Members: members, Created: scimAt, Modified: scimAt}
}

func mustCreateUsers(t *testing.T, s store.SCIMStore, users ...model.SCIMUser) {
	t.Helper()
	for _, u := range users {
		if err := s.CreateSCIMUser(context.Background(), u); err != nil {
			t.Fatalf("creating %s: %v", u.ID, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func scimUserRoundTrip(t *testing.T, s store.SCIMStore) {
	want := scimUser("u1", "alice@acme.com", scimAt)
	mustCreateUsers(t, s, want)
	got, err := s.SCIMUser(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.UserName != want.UserName || got.ExternalID != want.ExternalID || !got.Active ||
		!got.Created.Equal(scimAt) || !got.Modified.Equal(scimAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func scimUserRequired(t *testing.T, s store.SCIMStore) {
	for _, u := range []model.SCIMUser{scimUser("", "a@acme.com", scimAt), scimUser("u1", "", scimAt)} {
		if err := s.CreateSCIMUser(context.Background(), u); !errors.Is(err, model.ErrBadInput) {
			t.Errorf("create %+v: err = %v, want ErrBadInput", u, err)
		}
	}
}

func scimUserNameUnique(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	if err := s.CreateSCIMUser(context.Background(), scimUser("u2", "Alice@ACME.com", scimAt)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("same name in another case: err = %v, want ErrConflict", err)
	}
	if err := s.CreateSCIMUser(context.Background(), scimUser("u1", "bob@acme.com", scimAt)); !errors.Is(err, model.ErrConflict) {
		t.Errorf("same id: err = %v, want ErrConflict", err)
	}
}

func scimUserReplace(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	later := scimAt.Add(time.Hour)
	replacement := model.SCIMUser{ID: "u1", UserName: "alice@acme.io", Active: false, Created: later, Modified: later}
	if err := s.ReplaceSCIMUser(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	got, _ := s.SCIMUser(context.Background(), "u1")
	if got.UserName != "alice@acme.io" || got.Active || got.ExternalID != "" ||
		!got.Created.Equal(scimAt) || !got.Modified.Equal(later) {
		t.Errorf("got %+v", got)
	}
	if err := s.ReplaceSCIMUser(context.Background(), scimUser("nope", "x@acme.com", scimAt)); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("replace unknown: err = %v, want ErrNotFound", err)
	}
}

func scimUserPatch(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	later := scimAt.Add(time.Minute)
	got, err := s.PatchSCIMUser(context.Background(), "u1", model.SCIMUserChange{Active: ptr(false)}, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Active || got.UserName != "alice@acme.com" || got.ExternalID != "ext-u1" || !got.Modified.Equal(later) {
		t.Errorf("got %+v", got)
	}
	if _, err := s.PatchSCIMUser(context.Background(), "nope", model.SCIMUserChange{}, later); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("patch unknown: err = %v, want ErrNotFound", err)
	}
}

func scimUserPatchConflict(t *testing.T, s store.SCIMStore) {
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt), scimUser("u2", "bob@acme.com", scimAt))
	_, err := s.PatchSCIMUser(context.Background(), "u2", model.SCIMUserChange{UserName: ptr("ALICE@acme.com")}, scimAt)
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
	// Renaming a user to its own name in another case is not a clash.
	if _, err := s.PatchSCIMUser(context.Background(), "u1", model.SCIMUserChange{UserName: ptr("Alice@acme.com")}, scimAt); err != nil {
		t.Errorf("recasing one's own name: %v", err)
	}
}

func scimUserUnknown(t *testing.T, s store.SCIMStore) {
	if _, err := s.SCIMUser(context.Background(), "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("get: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSCIMUser(context.Background(), "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("delete: err = %v, want ErrNotFound", err)
	}
}

func scimUserDeleteCascades(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt), scimUser("u2", "bob@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1", "u2")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSCIMUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	g, _ := s.SCIMGroup(ctx, "g1")
	if !equalStrings(g.Members, []string{"u2"}) {
		t.Errorf("members = %v, want [u2]", g.Members)
	}
	if _, err := s.SCIMUser(ctx, "u1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("deleted user still readable: %v", err)
	}
}

func scimUserList(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	// Created out of ID order, and two sharing an instant, to pin the order.
	mustCreateUsers(t, s,
		scimUser("u3", "carol@acme.com", scimAt),
		scimUser("u1", "alice@acme.com", scimAt),
		scimUser("u2", "bob@acme.com", scimAt.Add(-time.Hour)),
	)
	all, total, err := s.ListSCIMUsers(ctx, model.SCIMFilter{}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || !equalStrings(userIDs(all), []string{"u2", "u1", "u3"}) {
		t.Errorf("all = %v (total %d), want u2,u1,u3 by created then id", userIDs(all), total)
	}
	second, total, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{}, 2, 1)
	if total != 3 || !equalStrings(userIDs(second), []string{"u1"}) {
		t.Errorf("page 2 of 1 = %v (total %d)", userIDs(second), total)
	}
	past, _, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{}, 10, 5)
	if past == nil || len(past) != 0 {
		t.Errorf("past the end = %#v, want empty non-nil", past)
	}
	byName, total, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrUserName, Value: "BOB@acme.com"}, 1, 100)
	if total != 1 || !equalStrings(userIDs(byName), []string{"u2"}) {
		t.Errorf("userName filter = %v", userIDs(byName))
	}
	byExt, _, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrExternalID, Value: "ext-u3"}, 1, 100)
	if !equalStrings(userIDs(byExt), []string{"u3"}) {
		t.Errorf("externalId filter = %v", userIDs(byExt))
	}
	caseExt, _, _ := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrExternalID, Value: "EXT-U3"}, 1, 100)
	if len(caseExt) != 0 {
		t.Errorf("externalId compares exactly; got %v", userIDs(caseExt))
	}
	if _, _, err := s.ListSCIMUsers(ctx, model.SCIMFilter{Attribute: model.SCIMAttrDisplayName, Value: "x"}, 1, 100); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("displayName filter on users: err = %v, want ErrBadInput", err)
	}
}

func userIDs(users []model.SCIMUser) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.ID)
	}
	return out
}

func scimGroupRoundTrip(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u2", "bob@acme.com", scimAt), scimUser("u1", "alice@acme.com", scimAt))
	want := scimGroup("g1", "platform", "u2", "u1")
	want.ExternalID = "okta-g1"
	if err := s.CreateSCIMGroup(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.SCIMGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "platform" || got.ExternalID != "okta-g1" || !equalStrings(got.Members, []string{"u1", "u2"}) ||
		!got.Created.Equal(scimAt) {
		t.Errorf("got %+v; members must come back sorted", got)
	}
	empty := scimGroup("g2", "empty")
	if err := s.CreateSCIMGroup(ctx, empty); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroup(ctx, "g2"); got.Members == nil {
		t.Error("an empty group's members must be an empty slice, not nil")
	}
}

func scimGroupMemberMustExist(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "ghost")); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("create with ghost: err = %v, want ErrBadInput", err)
	}
	if _, err := s.SCIMGroup(ctx, "g1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("a rejected group must not be stored: %v", err)
	}
}

func scimGroupNameUnique(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSCIMGroup(ctx, scimGroup("g2", "Platform")); !errors.Is(err, model.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

func scimGroupReplace(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "a@acme.com", scimAt), scimUser("u2", "b@acme.com", scimAt), scimUser("u3", "c@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	later := scimAt.Add(time.Hour)
	replacement := model.SCIMGroup{ID: "g1", DisplayName: "platform-eng", ExternalID: "x", Members: []string{"u2", "u3"}, Created: later, Modified: later}
	if err := s.ReplaceSCIMGroup(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	got, err := s.SCIMGroup(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "platform-eng" || got.ExternalID != "x" || !equalStrings(got.Members, []string{"u2", "u3"}) ||
		!got.Created.Equal(scimAt) || !got.Modified.Equal(later) {
		t.Errorf("got %+v, want platform-eng/x with [u2 u3], created unchanged, modified updated", got)
	}

	// A replace with a member that is not a stored user is rejected and
	// leaves the group exactly as it was.
	ghost := model.SCIMGroup{ID: "g1", DisplayName: "ghosted", Members: []string{"ghost"}, Created: later, Modified: later}
	if err := s.ReplaceSCIMGroup(ctx, ghost); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("replace with ghost member: err = %v, want ErrBadInput", err)
	}
	unchanged, _ := s.SCIMGroup(ctx, "g1")
	if unchanged.DisplayName != "platform-eng" || unchanged.ExternalID != "x" || !equalStrings(unchanged.Members, []string{"u2", "u3"}) ||
		!unchanged.Created.Equal(scimAt) || !unchanged.Modified.Equal(later) {
		t.Errorf("got %+v; a rejected replace must change nothing", unchanged)
	}

	// A replace that collides with another group's name, ignoring case, is
	// a conflict, not silently accepted.
	if err := s.CreateSCIMGroup(ctx, scimGroup("g2", "mobile")); err != nil {
		t.Fatal(err)
	}
	clash := model.SCIMGroup{ID: "g2", DisplayName: "PLATFORM-ENG", Created: later, Modified: later}
	if err := s.ReplaceSCIMGroup(ctx, clash); !errors.Is(err, model.ErrConflict) {
		t.Errorf("replace onto another group's name: err = %v, want ErrConflict", err)
	}

	if err := s.ReplaceSCIMGroup(ctx, model.SCIMGroup{ID: "nope", DisplayName: "x", Created: later, Modified: later}); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("replace unknown: err = %v, want ErrNotFound", err)
	}
}

func scimGroupPatchOrder(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "a@acme.com", scimAt), scimUser("u2", "b@acme.com", scimAt), scimUser("u3", "c@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	later := scimAt.Add(time.Minute)
	got, err := s.PatchSCIMGroup(ctx, "g1", model.SCIMGroupChange{
		DisplayName: ptr("platform-eng"),
		Members: []model.SCIMMemberOp{
			{Kind: model.SCIMMembersReplace, Users: []string{"u2"}},
			{Kind: model.SCIMMembersAdd, Users: []string{"u3", "u2"}},
			{Kind: model.SCIMMembersRemove, Users: []string{"u2", "u1"}},
		},
	}, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "platform-eng" || !equalStrings(got.Members, []string{"u3"}) || !got.Modified.Equal(later) {
		t.Errorf("got %+v, want platform-eng with [u3]", got)
	}
}

func scimGroupPatchAtomic(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "a@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	_, err := s.PatchSCIMGroup(ctx, "g1", model.SCIMGroupChange{
		DisplayName: ptr("renamed"),
		Members:     []model.SCIMMemberOp{{Kind: model.SCIMMembersAdd, Users: []string{"u1", "ghost"}}},
	}, scimAt)
	if !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("err = %v, want ErrBadInput", err)
	}
	got, _ := s.SCIMGroup(ctx, "g1")
	if got.DisplayName != "platform" || len(got.Members) != 0 {
		t.Errorf("got %+v; a failed patch must change nothing", got)
	}
}

func scimGroupPatchConcurrent(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	const n = 20
	for i := 0; i < n; i++ {
		mustCreateUsers(t, s, scimUser(fmt.Sprintf("u%02d", i), fmt.Sprintf("user%02d@acme.com", i), scimAt))
	}
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := s.PatchSCIMGroup(ctx, "g1", model.SCIMGroupChange{
				Members: []model.SCIMMemberOp{{Kind: model.SCIMMembersAdd, Users: []string{id}}},
			}, scimAt)
			errs <- err
		}(fmt.Sprintf("u%02d", i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.SCIMGroup(ctx, "g1")
	if len(got.Members) != n {
		t.Errorf("members = %d, want %d: a concurrent patch lost a write", len(got.Members), n)
	}
}

func scimGroupDelete(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSCIMGroup(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if groups, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); len(groups) != 0 {
		t.Errorf("groups after delete = %v", groups)
	}
	if err := s.DeleteSCIMGroup(ctx, "g1"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("second delete: err = %v, want ErrNotFound", err)
	}
	// The name is free again.
	if err := s.CreateSCIMGroup(ctx, scimGroup("g2", "platform")); err != nil {
		t.Errorf("recreating a deleted name: %v", err)
	}
}

func scimGroupList(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	for _, g := range []model.SCIMGroup{scimGroup("g2", "mobile"), scimGroup("g1", "platform", "u1")} {
		if err := s.CreateSCIMGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	all, total, err := s.ListSCIMGroups(ctx, model.SCIMFilter{}, 1, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(all) != 2 || all[0].ID != "g1" || !equalStrings(all[0].Members, []string{"u1"}) {
		t.Errorf("all = %+v (total %d)", all, total)
	}
	byName, _, _ := s.ListSCIMGroups(ctx, model.SCIMFilter{Attribute: model.SCIMAttrDisplayName, Value: "MOBILE"}, 1, 100, true)
	if len(byName) != 1 || byName[0].ID != "g2" {
		t.Errorf("displayName filter = %+v", byName)
	}
	if _, _, err := s.ListSCIMGroups(ctx, model.SCIMFilter{Attribute: model.SCIMAttrUserName, Value: "x"}, 1, 100, true); !errors.Is(err, model.ErrBadInput) {
		t.Errorf("userName filter on groups: err = %v, want ErrBadInput", err)
	}
}

// scimGroupListWithoutMembers is the conformance case for Group 5's part A:
// a page listed with withMembers false must not report a member of a group
// that actually has one, but the empty slice must still be non-nil so the
// handler's JSON encoding behaves the same as an empty group.
func scimGroupListWithoutMembers(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	without, _, err := s.ListSCIMGroups(ctx, model.SCIMFilter{}, 1, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(without) != 1 || without[0].Members == nil || len(without[0].Members) != 0 {
		t.Errorf("without members = %#v, want one group with an empty, non-nil Members", without)
	}
	with, _, err := s.ListSCIMGroups(ctx, model.SCIMFilter{}, 1, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(with) != 1 || !equalStrings(with[0].Members, []string{"u1"}) {
		t.Errorf("with members = %+v, want [u1]", with)
	}
}

func scimGroupsForActiveOnly(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt))
	for _, g := range []model.SCIMGroup{scimGroup("g1", "platform", "u1"), scimGroup("g2", "oncall", "u1"), scimGroup("g3", "mobile")} {
		if err := s.CreateSCIMGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SCIMGroupsFor(ctx, "alice@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(got, []string{"oncall", "platform"}) {
		t.Errorf("groups = %v, want sorted [oncall platform]", got)
	}
	if _, err := s.PatchSCIMUser(ctx, "u1", model.SCIMUserChange{Active: ptr(false)}, scimAt); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); got == nil || len(got) != 0 {
		t.Errorf("inactive user's groups = %#v, want empty non-nil", got)
	}
	if _, err := s.PatchSCIMUser(ctx, "u1", model.SCIMUserChange{Active: ptr(true)}, scimAt); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); !equalStrings(got, []string{"oncall", "platform"}) {
		t.Errorf("reactivated user's groups = %v; memberships must survive deactivation", got)
	}
	if got, err := s.SCIMGroupsFor(ctx, "nobody@acme.com"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("unknown user: %#v, %v", got, err)
	}
}

func scimGroupsForExact(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	mustCreateUsers(t, s, scimUser("u1", "Alice@acme.com", scimAt))
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform", "u1")); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMGroupsFor(ctx, "alice@acme.com"); len(got) != 0 {
		t.Errorf("groups = %v; resolution must not fold case", got)
	}
}

func scimCountsCase(t *testing.T, s store.SCIMStore) {
	ctx := context.Background()
	if got, err := s.SCIMCounts(ctx); err != nil || got != (model.SCIMCounts{}) {
		t.Errorf("empty counts = %+v, %v", got, err)
	}
	inactive := scimUser("u2", "bob@acme.com", scimAt)
	inactive.Active = false
	mustCreateUsers(t, s, scimUser("u1", "alice@acme.com", scimAt), inactive)
	if err := s.CreateSCIMGroup(ctx, scimGroup("g1", "platform")); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SCIMCounts(ctx); got != (model.SCIMCounts{Users: 2, ActiveUsers: 1, Groups: 1}) {
		t.Errorf("counts = %+v", got)
	}
}
