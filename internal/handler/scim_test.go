package handler_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/store"
)

const scimToken = "test-scim-token"

// newSCIMServer is newServer with SCIM enabled.
func newSCIMServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.AdminToken = adminToken
	h.SCIMToken = scimToken
	// The same clock as newServer, so a bundle from either server is
	// byte-comparable.
	h.Now = func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

// scimDo sends a SCIM request with the SCIM token and decodes a JSON
// answer into a generic map (nil for an empty body).
func scimDo(t *testing.T, srv *httptest.Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	return scimDoAs(t, srv, method, path, body, scimToken)
}

func scimDoAs(t *testing.T, srv *httptest.Server, method, path, body, token string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/scim+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) == 0 {
		return resp.StatusCode, nil
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/scim+json") {
		t.Errorf("%s %s: Content-Type = %q", method, path, ct)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s %s: decoding %s: %v", method, path, raw, err)
	}
	return resp.StatusCode, out
}

const aliceUser = `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@acme.com","externalId":"00u1","active":true}`

func createUser(t *testing.T, srv *httptest.Server, body string) string {
	t.Helper()
	status, out := scimDo(t, srv, http.MethodPost, "/scim/v2/Users", body)
	if status != http.StatusCreated {
		t.Fatalf("creating user: %d %v", status, out)
	}
	return out["id"].(string)
}

func TestSCIMIsDisabledWithoutAToken(t *testing.T) {
	srv := newServer(t) // no SCIMToken
	status, out := scimDoAs(t, srv, http.MethodGet, "/scim/v2/Users", "", "anything")
	if status != http.StatusServiceUnavailable || out["status"] != "503" {
		t.Errorf("status = %d, body = %v; want a 503 SCIM error", status, out)
	}
}

func TestSCIMRefusesAWrongToken(t *testing.T) {
	srv := newSCIMServer(t)
	for _, token := range []string{"", "wrong", adminToken} {
		if status, _ := scimDoAs(t, srv, http.MethodGet, "/scim/v2/Users", "", token); status != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", token, status)
		}
	}
}

func TestSCIMDiscoveryDocuments(t *testing.T) {
	srv := newSCIMServer(t)
	status, spc := scimDo(t, srv, http.MethodGet, "/scim/v2/ServiceProviderConfig", "")
	if status != 200 || spc["patch"].(map[string]any)["supported"] != true {
		t.Errorf("ServiceProviderConfig = %d %v", status, spc)
	}
	for _, path := range []string{"/scim/v2/ResourceTypes", "/scim/v2/Schemas"} {
		status, out := scimDo(t, srv, http.MethodGet, path, "")
		if status != 200 || out["totalResults"] != float64(2) {
			t.Errorf("%s = %d %v", path, status, out)
		}
	}
}

func TestSCIMUserLifecycle(t *testing.T) {
	srv := newSCIMServer(t)
	status, created := scimDo(t, srv, http.MethodPost, "/scim/v2/Users", aliceUser)
	if status != http.StatusCreated {
		t.Fatalf("POST = %d %v", status, created)
	}
	id := created["id"].(string)
	meta := created["meta"].(map[string]any)
	if created["userName"] != "alice@acme.com" || created["active"] != true || meta["resourceType"] != "User" ||
		!strings.HasSuffix(meta["location"].(string), "/scim/v2/Users/"+id) {
		t.Errorf("created = %v", created)
	}

	if status, got := scimDo(t, srv, http.MethodGet, "/scim/v2/Users/"+id, ""); status != 200 || got["externalId"] != "00u1" {
		t.Errorf("GET = %d %v", status, got)
	}

	status, patched := scimDo(t, srv, http.MethodPatch, "/scim/v2/Users/"+id,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}`)
	if status != 200 || patched["active"] != false {
		t.Errorf("PATCH = %d %v", status, patched)
	}

	status, replaced := scimDo(t, srv, http.MethodPut, "/scim/v2/Users/"+id,
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice@acme.io","active":true}`)
	if status != 200 || replaced["userName"] != "alice@acme.io" || replaced["active"] != true {
		t.Errorf("PUT = %d %v", status, replaced)
	}

	if status, _ := scimDo(t, srv, http.MethodDelete, "/scim/v2/Users/"+id, ""); status != http.StatusNoContent {
		t.Errorf("DELETE = %d", status)
	}
	if status, out := scimDo(t, srv, http.MethodGet, "/scim/v2/Users/"+id, ""); status != 404 || out["status"] != "404" {
		t.Errorf("GET after delete = %d %v", status, out)
	}
}

func TestSCIMUserErrors(t *testing.T) {
	srv := newSCIMServer(t)
	createUser(t, srv, aliceUser)
	cases := []struct {
		name, method, path, body string
		status                   int
		scimType                 string
	}{
		{"duplicate userName ignoring case", "POST", "/scim/v2/Users",
			`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"ALICE@acme.com"}`, 409, "uniqueness"},
		{"not json", "POST", "/scim/v2/Users", `{`, 400, "invalidSyntax"},
		{"no userName", "POST", "/scim/v2/Users", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"]}`, 400, "invalidValue"},
		{"null active", "POST", "/scim/v2/Users",
			`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"null-active@acme.com","active":null}`, 400, "invalidValue"},
		{"unsupported filter", "GET", "/scim/v2/Users?filter=userName%20co%20%22a%22", "", 400, "invalidFilter"},
		{"bad count", "GET", "/scim/v2/Users?count=many", "", 400, "invalidValue"},
		{"unknown id", "PATCH", "/scim/v2/Users/nope",
			`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}`, 404, ""},
		{"unknown id on PUT", "PUT", "/scim/v2/Users/nope", aliceUser, 404, ""},
	}
	for _, tc := range cases {
		status, out := scimDo(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || (tc.scimType != "" && out["scimType"] != tc.scimType) {
			t.Errorf("%s: %d %v, want %d %s", tc.name, status, out, tc.status, tc.scimType)
		}
	}
}

func TestSCIMBodyOverTheCapIs413(t *testing.T) {
	srv := newSCIMServer(t)
	big := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"a@acme.com","x":"` + strings.Repeat("a", 1<<20) + `"}`
	if status, _ := scimDo(t, srv, http.MethodPost, "/scim/v2/Users", big); status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", status)
	}
}

func TestSCIMUserListFiltersAndPages(t *testing.T) {
	srv := newSCIMServer(t)
	createUser(t, srv, aliceUser)
	createUser(t, srv, `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"bob@acme.com"}`)

	status, list := scimDo(t, srv, http.MethodGet, "/scim/v2/Users?filter=userName+eq+%22Alice%40acme.com%22", "")
	resources := list["Resources"].([]any)
	if status != 200 || list["totalResults"] != float64(1) || len(resources) != 1 ||
		resources[0].(map[string]any)["userName"] != "alice@acme.com" {
		t.Errorf("filtered list = %d %v", status, list)
	}
	schemas := list["schemas"].([]any)
	if schemas[0] != "urn:ietf:params:scim:api:messages:2.0:ListResponse" {
		t.Errorf("schemas = %v", schemas)
	}

	_, page := scimDo(t, srv, http.MethodGet, "/scim/v2/Users?startIndex=2&count=1", "")
	if page["totalResults"] != float64(2) || page["startIndex"] != float64(2) || page["itemsPerPage"] != float64(1) {
		t.Errorf("page = %v", page)
	}

	_, none := scimDo(t, srv, http.MethodGet, "/scim/v2/Users?filter=userName+eq+%22nobody%40acme.com%22", "")
	if none["totalResults"] != float64(0) || len(none["Resources"].([]any)) != 0 {
		t.Errorf("empty list = %v; Resources must be [] not null", none)
	}
}

func TestSCIMUnknownRouteIsASCIMError(t *testing.T) {
	srv := newSCIMServer(t)
	if status, out := scimDo(t, srv, http.MethodPatch, "/scim/v2/Users", ""); status != 404 || out["status"] != "404" {
		t.Errorf("PATCH /scim/v2/Users = %d %v, want a 404 SCIM error", status, out)
	}
	if status, out := scimDo(t, srv, http.MethodGet, "/scim/v2/Nope", ""); status != 404 || out["status"] != "404" {
		t.Errorf("GET /scim/v2/Nope = %d %v, want a 404 SCIM error", status, out)
	}

	if status, _ := scimDoAs(t, srv, http.MethodGet, "/scim/v2/Nope", "", ""); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated unknown route: status = %d, want 401", status)
	}
}
