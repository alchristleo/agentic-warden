package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func createGroup(t *testing.T, srv *httptest.Server, body string) string {
	t.Helper()
	status, out := scimDo(t, srv, http.MethodPost, "/scim/v2/Groups", body)
	if status != http.StatusCreated {
		t.Fatalf("creating group: %d %v", status, out)
	}
	return out["id"].(string)
}

func groupBody(name string, members ...string) string {
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"` + name + `","members":[`
	for i, m := range members {
		if i > 0 {
			body += ","
		}
		body += `{"value":"` + m + `"}`
	}
	return body + `]}`
}

func memberIDs(group map[string]any) []string {
	out := []string{}
	members, _ := group["members"].([]any)
	for _, m := range members {
		out = append(out, m.(map[string]any)["value"].(string))
	}
	return out
}

func TestSCIMGroupLifecycle(t *testing.T) {
	srv := newSCIMServer(t)
	alice := createUser(t, srv, aliceUser)

	status, created := scimDo(t, srv, http.MethodPost, "/scim/v2/Groups", groupBody("platform", alice))
	if status != http.StatusCreated || created["displayName"] != "platform" || !equal(memberIDs(created), []string{alice}) {
		t.Fatalf("POST = %d %v", status, created)
	}
	id := created["id"].(string)

	if status, got := scimDo(t, srv, http.MethodGet, "/scim/v2/Groups/"+id+"?excludedAttributes=members", ""); status != 200 || got["members"] != nil {
		t.Errorf("GET excluding members = %d %v", status, got)
	}

	// Entra: PATCH answers 204 when no attributes are requested.
	status, _ = scimDo(t, srv, http.MethodPatch, "/scim/v2/Groups/"+id,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Remove","path":"members","value":[{"value":"`+alice+`"}]}]}`)
	if status != http.StatusNoContent {
		t.Errorf("PATCH = %d, want 204", status)
	}
	// With attributes requested, the resource comes back.
	status, patched := scimDo(t, srv, http.MethodPatch, "/scim/v2/Groups/"+id+"?attributes=members",
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"add","path":"members","value":[{"value":"`+alice+`"}]}]}`)
	if status != 200 || !equal(memberIDs(patched), []string{alice}) {
		t.Errorf("PATCH with attributes = %d %v", status, patched)
	}

	status, replaced := scimDo(t, srv, http.MethodPut, "/scim/v2/Groups/"+id, groupBody("platform-eng"))
	if status != 200 || replaced["displayName"] != "platform-eng" || len(memberIDs(replaced)) != 0 {
		t.Errorf("PUT = %d %v", status, replaced)
	}

	if status, _ := scimDo(t, srv, http.MethodDelete, "/scim/v2/Groups/"+id, ""); status != http.StatusNoContent {
		t.Errorf("DELETE = %d", status)
	}
	if status, _ := scimDo(t, srv, http.MethodGet, "/scim/v2/Groups/"+id, ""); status != 404 {
		t.Errorf("GET after delete = %d", status)
	}
}

func TestSCIMGroupErrors(t *testing.T) {
	srv := newSCIMServer(t)
	createGroup(t, srv, groupBody("platform"))
	cases := []struct {
		name, method, path, body string
		status                   int
		scimType                 string
	}{
		{"member is not a user", "POST", "/scim/v2/Groups", groupBody("mobile", "ghost"), 400, "invalidValue"},
		{"duplicate displayName", "POST", "/scim/v2/Groups", groupBody("PLATFORM"), 409, "uniqueness"},
		{"userName filter on groups", "GET", "/scim/v2/Groups?filter=userName%20eq%20%22a%22", "", 400, "invalidFilter"},
		{"unknown id", "PATCH", "/scim/v2/Groups/nope",
			`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"add","path":"members","value":[]}]}`, 404, ""},
	}
	for _, tc := range cases {
		status, out := scimDo(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || (tc.scimType != "" && out["scimType"] != tc.scimType) {
			t.Errorf("%s: %d %v, want %d %s", tc.name, status, out, tc.status, tc.scimType)
		}
	}
}

func TestSCIMGroupListFilterAndExcludedMembers(t *testing.T) {
	srv := newSCIMServer(t)
	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("platform", alice))
	createGroup(t, srv, groupBody("mobile"))

	_, list := scimDo(t, srv, http.MethodGet, "/scim/v2/Groups?filter=displayName+eq+%22Platform%22&excludedAttributes=members", "")
	resources := list["Resources"].([]any)
	if list["totalResults"] != float64(1) || len(resources) != 1 {
		t.Fatalf("list = %v", list)
	}
	if g := resources[0].(map[string]any); g["displayName"] != "platform" || g["members"] != nil {
		t.Errorf("group = %v", g)
	}
}
