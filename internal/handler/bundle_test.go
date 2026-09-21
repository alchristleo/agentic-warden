package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

const groupedRuleSet = `{
  "version": "g1",
  "groups": {"alice@acme.com": ["platform"], "bob@acme.com": ["mobile"]},
  "rules": [
    {"name": "baseline", "agents": {"claude": {"managed": {"model": "sonnet"}}}},
    {"name": "platform", "match": {"groups": ["platform"]}, "agents": {"claude": {"managed": {"model": "opus"}}}},
    {"name": "mobile", "match": {"groups": ["mobile"]}, "agents": {"claude": {"managed": {"model": "haiku"}}}},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {"claude": {"managed": {"model": "opus-payments"}}}}
  ]
}`

func fetchBundle(t *testing.T, srv *httptest.Server, credential string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/bundle", nil)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBundle(t *testing.T, resp *http.Response) policy.Bundle {
	t.Helper()
	var b policy.Bundle
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBundleWithoutACredentialIs401(t *testing.T) {
	srv := newServer(t)

	if resp := fetchBundle(t, srv, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if resp := fetchBundle(t, srv, "not-a-credential", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong credential: status = %d, want 401", resp.StatusCode)
	}
}

func TestBundleIsResolvedForTheMachinesUser(t *testing.T) {
	srv := newServer(t)
	if resp := post(t, srv, "/v1/policy/revisions", groupedRuleSet); resp.StatusCode != http.StatusCreated {
		t.Fatalf("apply: %d", resp.StatusCode)
	}
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	b := decodeBundle(t, resp)
	if b.User != "alice@acme.com" || len(b.Groups) != 1 || b.Groups[0] != "platform" || b.Version != "g1" {
		t.Errorf("bundle header = %+v", b)
	}
	names := make([]string, 0)
	for _, r := range b.Rules {
		names = append(names, r.Name)
	}
	if want := []string{"baseline", "platform", "payments"}; !equal(names, want) {
		t.Errorf("rules = %v, want %v: mobile is dropped, payments kept for the client", names, want)
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("no ETag")
	}
}

func TestBundleIs304WhenUnchanged(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	first := fetchBundle(t, srv, alice, nil)

	second := fetchBundle(t, srv, alice, http.Header{"If-None-Match": {first.Header.Get("ETag")}})

	if second.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", second.StatusCode)
	}
}

func TestBundleForAnUnknownUserIsTheBaseline(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, carol := enroll(t, srv, mintToken(t, srv, "carol@acme.com"))

	b := decodeBundle(t, fetchBundle(t, srv, carol, nil))

	if len(b.Groups) != 0 {
		t.Errorf("groups = %v, want none", b.Groups)
	}
	if len(b.Rules) != 2 || b.Rules[0].Name != "baseline" || b.Rules[1].Name != "payments" {
		t.Errorf("rules = %+v, want baseline and the repo-scoped rule only", b.Rules)
	}
}

func TestBundleBeforeAnyPolicyIsEmptyNotAnError(t *testing.T) {
	srv := newServer(t)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: no policy yet is the ordinary case, not a failure", resp.StatusCode)
	}
	b := decodeBundle(t, resp)
	if b.User != "alice@acme.com" || b.Rules == nil || len(b.Rules) != 0 {
		t.Errorf("bundle = %+v, want the user and an empty rules list", b)
	}
}

func TestBundleFetchTouchesTheMachine(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	fetchBundle(t, srv, alice, nil)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/machines: %v", err)
	}
	defer resp.Body.Close()
	var machines []struct {
		LastBundleVersion string `json:"lastBundleVersion"`
		LastSeenAt        string `json:"lastSeenAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		t.Fatal(err)
	}
	if len(machines) != 2 || machines[1].LastBundleVersion != "g1" || machines[1].LastSeenAt == "" {
		t.Errorf("machines = %+v, want the second one touched with g1", machines)
	}
	if machines[0].LastBundleVersion != "" {
		t.Error("the machine that never fetched was touched")
	}
}

func TestARevokedMachineIs401(t *testing.T) {
	srv := newServer(t)
	id, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/machines/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}

	if resp := fetchBundle(t, srv, alice, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 after revocation", resp.StatusCode)
	}
}

func equal(a, b []string) bool {
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
