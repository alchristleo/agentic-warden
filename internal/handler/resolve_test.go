package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBundleGroupsIncludeActiveSCIMGroups(t *testing.T) {
	srv := newSCIMServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	before := fetchBundle(t, srv, credential, nil)
	beforeETag := before.Header.Get("ETag")

	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("mobile", alice))

	resp := fetchBundle(t, srv, credential, http.Header{"If-None-Match": {beforeETag}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; a SCIM membership must change the ETag", resp.StatusCode)
	}
	if b := decodeBundle(t, resp); !equal(b.Groups, []string{"mobile", "platform"}) {
		t.Errorf("groups = %v, want authored platform ∪ scim mobile", b.Groups)
	}

	// Deactivation drops the SCIM group; the authored one stays.
	scimDo(t, srv, http.MethodPatch, "/scim/v2/Users/"+alice,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}`)
	if b := decodeBundle(t, fetchBundle(t, srv, credential, nil)); !equal(b.Groups, []string{"platform"}) {
		t.Errorf("groups after deactivation = %v, want [platform]", b.Groups)
	}
}

func TestBundleWithoutSCIMDataIsUnchanged(t *testing.T) {
	plain := newServer(t)
	withSCIM := newSCIMServer(t)
	var etags []string
	for _, srv := range []*httptest.Server{plain, withSCIM} {
		post(t, srv, "/v1/policy/revisions", groupedRuleSet)
		_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
		etags = append(etags, fetchBundle(t, srv, credential, nil).Header.Get("ETag"))
	}
	if etags[0] != etags[1] {
		t.Errorf("ETags differ (%v): enabling SCIM with no data must not change any bundle", etags)
	}
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestResolveShowsEverySource(t *testing.T) {
	srv := newSCIMServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	putGroups(t, srv, `{"members":{"alice@acme.com":["oncall"]}}`, adminToken)
	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("mobile", alice))

	resp := getAs(t, srv, "/v1/groups/resolve?user=alice@acme.com", adminToken)
	var got struct {
		User          string   `json:"user"`
		Authored      []string `json:"authored"`
		Snapshot      []string `json:"snapshot"`
		SCIM          []string `json:"scim"`
		Effective     []string `json:"effective"`
		SCIMNearMatch *string  `json:"scimNearMatch"`
	}
	decodeJSON(t, resp, &got)
	if resp.StatusCode != 200 || !equal(got.Authored, []string{"platform"}) || !equal(got.Snapshot, []string{"oncall"}) ||
		!equal(got.SCIM, []string{"mobile"}) || !equal(got.Effective, []string{"mobile", "oncall", "platform"}) || got.SCIMNearMatch != nil {
		t.Errorf("resolve = %d %+v", resp.StatusCode, got)
	}
}

func TestResolveFlagsACaseMismatch(t *testing.T) {
	srv := newSCIMServer(t)
	createUser(t, srv, `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"Alice@acme.com"}`)
	resp := getAs(t, srv, "/v1/groups/resolve?user=alice@acme.com", adminToken)
	var got struct {
		SCIM          []string `json:"scim"`
		SCIMNearMatch *string  `json:"scimNearMatch"`
	}
	decodeJSON(t, resp, &got)
	if got.SCIMNearMatch == nil || *got.SCIMNearMatch != "Alice@acme.com" || len(got.SCIM) != 0 || got.SCIM == nil {
		t.Errorf("resolve = %+v; want scim [] and the near match named", got)
	}
}

func TestResolveNeedsAdminAndAUser(t *testing.T) {
	srv := newSCIMServer(t)
	if resp := getAs(t, srv, "/v1/groups/resolve?user=a@acme.com", ""); resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	if resp := getAs(t, srv, "/v1/groups/resolve", adminToken); resp.StatusCode != 400 {
		t.Errorf("no user: %d, want 400", resp.StatusCode)
	}
}

func TestGetGroupsReportsSCIMCounts(t *testing.T) {
	srv := newSCIMServer(t)
	if resp := getAs(t, srv, "/v1/groups", adminToken); resp.StatusCode != 404 {
		t.Errorf("nothing at all: %d, want 404", resp.StatusCode)
	}
	alice := createUser(t, srv, aliceUser)
	createGroup(t, srv, groupBody("mobile", alice))

	resp := getAs(t, srv, "/v1/groups", adminToken)
	var got struct {
		HasSnapshot bool `json:"hasSnapshot"`
		SCIM        *struct {
			Users, ActiveUsers, Groups int
		} `json:"scim"`
		Source *string `json:"source"`
	}
	decodeJSON(t, resp, &got)
	if resp.StatusCode != 200 || got.HasSnapshot || got.SCIM == nil || got.SCIM.Users != 1 || got.SCIM.Groups != 1 || got.Source != nil {
		t.Errorf("groups = %d %+v", resp.StatusCode, got)
	}

	putGroups(t, srv, `{"source":"okta-export","members":{}}`, adminToken)
	resp = getAs(t, srv, "/v1/groups", adminToken)
	decodeJSON(t, resp, &got)
	if !got.HasSnapshot || got.Source == nil || *got.Source != "okta-export" || got.SCIM == nil {
		t.Errorf("groups with a snapshot = %+v", got)
	}
}

func TestGetGroupsWithoutSCIMHasNoSCIMField(t *testing.T) {
	srv := newServer(t)
	putGroups(t, srv, `{"members":{}}`, adminToken)
	resp := getAs(t, srv, "/v1/groups", adminToken)
	var got map[string]any
	decodeJSON(t, resp, &got)
	if _, ok := got["scim"]; ok || got["hasSnapshot"] != true {
		t.Errorf("groups = %v", got)
	}
}
