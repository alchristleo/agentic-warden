package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// provisioningStep is one IdP request and the groups alice's bundle must
// show afterwards. Bodies use {alice} and {group} placeholders filled in
// from earlier responses.
type provisioningStep struct {
	name, method, path, body string
	wantStatus               int
	wantGroups               []string
}

// replay runs an IdP's provisioning sequence against a fresh server with
// alice enrolled, checking her bundle's groups after every step.
func replay(t *testing.T, steps []provisioningStep) {
	t.Helper()
	srv := newSCIMServer(t)
	post(t, srv, "/v1/policy/revisions", `{"version":"v","rules":[{"name":"base","agents":{"claude":{"managed":{"model":"sonnet"}}}}]}`)
	_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	ids := map[string]string{}
	fill := func(s string) string {
		for placeholder, id := range ids {
			s = strings.ReplaceAll(s, placeholder, id)
		}
		return s
	}
	for _, step := range steps {
		status, out := scimDo(t, srv, step.method, fill(step.path), fill(step.body))
		if status != step.wantStatus {
			t.Fatalf("%s: status %d %v, want %d", step.name, status, out, step.wantStatus)
		}
		if id, ok := out["id"].(string); ok && step.method == http.MethodPost {
			if _, isUser := out["userName"]; isUser {
				ids["{alice}"] = id
			} else {
				ids["{group}"] = id
			}
		}
		if b := decodeBundle(t, fetchBundle(t, srv, credential, nil)); !equal(b.Groups, step.wantGroups) {
			t.Errorf("%s: bundle groups = %v, want %v", step.name, b.Groups, step.wantGroups)
		}
	}
}

const (
	patchOp  = `"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"]`
	userURN  = `urn:ietf:params:scim:schemas:core:2.0:User`
	groupURN = `urn:ietf:params:scim:schemas:core:2.0:Group`
)

func TestOktaProvisioningSequence(t *testing.T) {
	q := url.QueryEscape(`userName eq "alice@acme.com"`)
	replay(t, []provisioningStep{
		{"look up before create", "GET", "/scim/v2/Users?filter=" + q + "&startIndex=1&count=100", "", 200, []string{}},
		{"create user", "POST", "/scim/v2/Users", `{"schemas":["` + userURN + `"],"userName":"alice@acme.com",
		  "name":{"givenName":"Alice","familyName":"Anders"},"emails":[{"primary":true,"value":"alice@acme.com","type":"work"}],
		  "displayName":"Alice Anders","locale":"en-US","externalId":"00u1abcd","groups":[],"password":"xY9!","active":true}`, 201, []string{}},
		{"push group", "POST", "/scim/v2/Groups", `{"schemas":["` + groupURN + `"],"displayName":"platform","members":[]}`, 201, []string{}},
		{"add member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"add","path":"members",
		  "value":[{"value":"{alice}","display":"alice@acme.com"}]}]}`, 204, []string{"platform"}},
		{"rename group", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"replace",
		  "value":{"id":"{group}","displayName":"platform-eng"}}]}`, 204, []string{"platform-eng"}},
		{"deactivate", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"replace","value":{"active":false}}]}`, 200, []string{}},
		{"reactivate", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"replace","value":{"active":true}}]}`, 200, []string{"platform-eng"}},
		{"remove member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"remove",
		  "path":"members[value eq \"{alice}\"]"}]}`, 204, []string{}},
		{"delete group", "DELETE", "/scim/v2/Groups/{group}", "", 204, []string{}},
	})
}

func TestEntraProvisioningSequence(t *testing.T) {
	q := url.QueryEscape(`userName eq "alice@acme.com"`)
	replay(t, []provisioningStep{
		{"look up before create", "GET", "/scim/v2/Users?filter=" + q, "", 200, []string{}},
		{"create user", "POST", "/scim/v2/Users", `{"schemas":["` + userURN + `","urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"],
		  "externalId":"alice","userName":"alice@acme.com","active":true,"displayName":"Alice Anders",
		  "emails":[{"primary":true,"type":"work","value":"alice@acme.com"}],"meta":{"resourceType":"User"},
		  "name":{"formatted":"Alice Anders","familyName":"Anders","givenName":"Alice"},"roles":[],
		  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User":{"department":"Eng"}}`, 201, []string{}},
		{"look up group", "GET", "/scim/v2/Groups?excludedAttributes=members&filter=" + url.QueryEscape(`displayName eq "oncall"`), "", 200, []string{}},
		{"create group", "POST", "/scim/v2/Groups", `{"schemas":["` + groupURN + `"],"externalId":"8aa1a5c0","displayName":"oncall","meta":{"resourceType":"Group"},"members":[]}`, 201, []string{}},
		{"add member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"Add","path":"members","value":[{"value":"{alice}"}]}]}`, 204, []string{"oncall"}},
		{"update attributes", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[
		  {"op":"Replace","path":"displayName","value":"Alice A."},
		  {"op":"Add","path":"emails[type eq \"work\"].value","value":"alice@acme.com"}]}`, 200, []string{"oncall"}},
		{"disable", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"Replace","path":"active","value":"False"}]}`, 200, []string{}},
		{"enable", "PATCH", "/scim/v2/Users/{alice}", `{` + patchOp + `,"Operations":[{"op":"Replace","path":"active","value":"True"}]}`, 200, []string{"oncall"}},
		{"remove member", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"Remove","path":"members","value":[{"value":"{alice}"}]}]}`, 204, []string{}},
		{"add back", "PATCH", "/scim/v2/Groups/{group}", `{` + patchOp + `,"Operations":[{"op":"Add","path":"members","value":[{"value":"{alice}"}]}]}`, 204, []string{"oncall"}},
		{"delete user", "DELETE", "/scim/v2/Users/{alice}", "", 204, []string{}},
	})
}
