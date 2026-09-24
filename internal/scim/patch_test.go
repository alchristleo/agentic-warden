package scim_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

const patchSchema = `"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"]`

func userPatch(t *testing.T, ops string) model.SCIMUserChange {
	t.Helper()
	c, err := scim.UserPatch(strings.NewReader(`{` + patchSchema + `,"Operations":` + ops + `}`))
	if err != nil {
		t.Fatalf("UserPatch(%s): %v", ops, err)
	}
	return c
}

func TestUserPatchOktaDeactivate(t *testing.T) {
	c := userPatch(t, `[{"op":"replace","value":{"active":false}}]`)
	if c.Active == nil || *c.Active || c.UserName != nil || c.ExternalID != nil {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchEntraStringBooleanAndCapitalOp(t *testing.T) {
	c := userPatch(t, `[{"op":"Replace","path":"active","value":"False"}]`)
	if c.Active == nil || *c.Active {
		t.Errorf("change = %+v", c)
	}
	c = userPatch(t, `[{"op":"Replace","path":"active","value":"True"}]`)
	if c.Active == nil || !*c.Active {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchEntraAttributeUpdates(t *testing.T) {
	c := userPatch(t, `[
	  {"op":"Replace","path":"userName","value":"alice@acme.io"},
	  {"op":"Add","path":"externalId","value":"alice"},
	  {"op":"Add","path":"emails[type eq \"work\"].value","value":"alice@acme.io"},
	  {"op":"Replace","path":"name.givenName","value":"Alice"},
	  {"op":"Add","path":"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department","value":"Eng"}
	]`)
	if c.UserName == nil || *c.UserName != "alice@acme.io" || c.ExternalID == nil || *c.ExternalID != "alice" || c.Active != nil {
		t.Errorf("change = %+v; unheld paths must be no-ops", c)
	}
}

func TestUserPatchCoreURNPath(t *testing.T) {
	c := userPatch(t, `[{"op":"replace","path":"urn:ietf:params:scim:schemas:core:2.0:User:active","value":false}]`)
	if c.Active == nil || *c.Active {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchNoPathIgnoresUnheldKeys(t *testing.T) {
	c := userPatch(t, `[{"op":"replace","value":{"id":"u1","active":true,"name":{"givenName":"A"},"userName":"b@acme.com"}}]`)
	if c.Active == nil || !*c.Active || c.UserName == nil || *c.UserName != "b@acme.com" {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchRemoveExternalID(t *testing.T) {
	c := userPatch(t, `[{"op":"remove","path":"externalId"}]`)
	if c.ExternalID == nil || *c.ExternalID != "" {
		t.Errorf("change = %+v", c)
	}
}

func TestUserPatchRejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":        `{`,
		"no schema":       `{"Operations":[{"op":"replace","path":"active","value":false}]}`,
		"no operations":   `{` + patchSchema + `,"Operations":[]}`,
		"unknown op":      `{` + patchSchema + `,"Operations":[{"op":"move","path":"active","value":false}]}`,
		"bad boolean":     `{` + patchSchema + `,"Operations":[{"op":"replace","path":"active","value":"maybe"}]}`,
		"remove userName": `{` + patchSchema + `,"Operations":[{"op":"remove","path":"userName"}]}`,
		"empty userName":  `{` + patchSchema + `,"Operations":[{"op":"replace","path":"userName","value":""}]}`,
		"non-string name": `{` + patchSchema + `,"Operations":[{"op":"replace","path":"userName","value":7}]}`,
	} {
		_, err := scim.UserPatch(strings.NewReader(body))
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 {
			t.Errorf("%s: err = %v, want a 400 scim error", name, err)
		}
	}
}

func groupPatch(t *testing.T, ops string) model.SCIMGroupChange {
	t.Helper()
	c, err := scim.GroupPatch(strings.NewReader(`{` + patchSchema + `,"Operations":` + ops + `}`))
	if err != nil {
		t.Fatalf("GroupPatch(%s): %v", ops, err)
	}
	return c
}

func TestGroupPatchOktaAddAndFilteredRemove(t *testing.T) {
	c := groupPatch(t, `[
	  {"op":"add","path":"members","value":[{"value":"u1","display":"alice@acme.com"},{"value":"u2"}]},
	  {"op":"remove","path":"members[value eq \"u3\"]"}
	]`)
	want := []model.SCIMMemberOp{{Kind: "add", Users: []string{"u1", "u2"}}, {Kind: "remove", Users: []string{"u3"}}}
	if !equalOps(c.Members, want) || c.DisplayName != nil {
		t.Errorf("change = %+v, want %+v", c, want)
	}
}

func TestGroupPatchOktaRenameWithIDInValue(t *testing.T) {
	c := groupPatch(t, `[{"op":"replace","value":{"id":"g1","displayName":"platform-eng"}}]`)
	if c.DisplayName == nil || *c.DisplayName != "platform-eng" || len(c.Members) != 0 {
		t.Errorf("change = %+v", c)
	}
}

func TestGroupPatchEntraCapitalOpsAndValueListRemove(t *testing.T) {
	c := groupPatch(t, `[
	  {"op":"Add","path":"members","value":[{"value":"u1"}]},
	  {"op":"Remove","path":"members","value":[{"value":"u2"}]},
	  {"op":"Replace","path":"displayName","value":"oncall"},
	  {"op":"Replace","path":"externalId","value":"8aa1"}
	]`)
	want := []model.SCIMMemberOp{{Kind: "add", Users: []string{"u1"}}, {Kind: "remove", Users: []string{"u2"}}}
	if !equalOps(c.Members, want) || c.DisplayName == nil || *c.DisplayName != "oncall" || c.ExternalID == nil || *c.ExternalID != "8aa1" {
		t.Errorf("change = %+v", c)
	}
}

func TestGroupPatchReplaceMembersAndRemoveAll(t *testing.T) {
	c := groupPatch(t, `[{"op":"replace","path":"members","value":[{"value":"u9"}]},{"op":"remove","path":"members"}]`)
	want := []model.SCIMMemberOp{{Kind: "replace", Users: []string{"u9"}}, {Kind: "replace", Users: []string{}}}
	if !equalOps(c.Members, want) {
		t.Errorf("members = %+v, want %+v", c.Members, want)
	}
}

func TestGroupPatchNoPathMembers(t *testing.T) {
	c := groupPatch(t, `[{"op":"replace","value":{"members":[{"value":"u1"}]}}]`)
	if !equalOps(c.Members, []model.SCIMMemberOp{{Kind: "replace", Users: []string{"u1"}}}) {
		t.Errorf("members = %+v", c.Members)
	}
}

func TestGroupPatchRejects(t *testing.T) {
	for name, ops := range map[string]string{
		"member without value": `[{"op":"add","path":"members","value":[{"display":"x"}]}]`,
		"members not a list":   `[{"op":"add","path":"members","value":"u1"}]`,
		"bad member filter":    `[{"op":"remove","path":"members[display eq \"x\"]"}]`,
		"empty displayName":    `[{"op":"replace","path":"displayName","value":""}]`,
		"remove displayName":   `[{"op":"remove","path":"displayName"}]`,
		"unknown op":           `[{"op":"copy","path":"members","value":[]}]`,
	} {
		_, err := scim.GroupPatch(strings.NewReader(`{` + patchSchema + `,"Operations":` + ops + `}`))
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 {
			t.Errorf("%s: err = %v, want a 400 scim error", name, err)
		}
	}
}

func equalOps(a, b []model.SCIMMemberOp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || strings.Join(a[i].Users, ",") != strings.Join(b[i].Users, ",") {
			return false
		}
	}
	return true
}
