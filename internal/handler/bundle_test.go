package handler_test

import (
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/store"
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

func TestBundleGroupsAreTheUnionOfAuthoredAndSynced(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	before := fetchBundle(t, srv, alice, nil)
	beforeETag := before.Header.Get("ETag")

	// The IdP says alice is also mobile; the authored map keeps platform.
	if resp := putGroups(t, srv, `{"members":{"alice@acme.com":["mobile","platform"]}}`, adminToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("put groups: %d", resp.StatusCode)
	}

	resp := fetchBundle(t, srv, alice, http.Header{"If-None-Match": {beforeETag}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; a changed membership must change the ETag and defeat the 304", resp.StatusCode)
	}
	b := decodeBundle(t, resp)
	if !equal(b.Groups, []string{"mobile", "platform"}) {
		t.Errorf("groups = %v, want the sorted union", b.Groups)
	}
	names := make([]string, 0)
	for _, r := range b.Rules {
		names = append(names, r.Name)
	}
	if want := []string{"baseline", "platform", "mobile", "payments"}; !equal(names, want) {
		t.Errorf("rules = %v, want %v", names, want)
	}
}

// newSignedServer builds a server the way newServer does, but returns it
// already armed with the given Signer so a test can exercise the signing
// headers without reaching past the package boundary the other tests use.
func newSignedServer(t *testing.T, signer *handler.Signer) *httptest.Server {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.AdminToken = adminToken
	h.Now = func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }
	h.Signer = signer
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func TestBundleCarriesASignature(t *testing.T) {
	key, _ := signing.Generate()
	srv := newSignedServer(t, &handler.Signer{Key: key})
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	if got := resp.Header.Get("X-AW-Key-Id"); got != signing.KeyID(pub) {
		t.Fatalf("X-AW-Key-Id = %q, want %q", got, signing.KeyID(pub))
	}
	if !signing.Verify(pub, body, resp.Header.Get("X-AW-Signature")) {
		t.Fatal("the signature does not verify over the response body")
	}
	if resp.Header.Get("X-AW-Key-Rollover") != "" {
		t.Fatal("a rollover header appeared with no previous key configured")
	}
}

func TestUnsignedDeploymentSendsNoSignatureHeaders(t *testing.T) {
	srv := newServer(t)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	for _, name := range []string{"X-AW-Signature", "X-AW-Key-Id", "X-AW-Key-Rollover"} {
		if resp.Header.Get(name) != "" {
			t.Errorf("%s was sent by an unsigned deployment", name)
		}
	}
}

func TestNotModifiedCarriesNoSignature(t *testing.T) {
	key, _ := signing.Generate()
	srv := newSignedServer(t, &handler.Signer{Key: key})
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	first := fetchBundle(t, srv, alice, nil)

	second := fetchBundle(t, srv, alice, http.Header{"If-None-Match": {first.Header.Get("ETag")}})

	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.StatusCode)
	}
	if second.Header.Get("X-AW-Signature") != "" {
		t.Fatal("a 304 carried a signature; there is no body to sign")
	}
}

func TestRolloverHeaderIsSignedByThePreviousKey(t *testing.T) {
	previous, _ := signing.Generate()
	current, _ := signing.Generate()
	srv := newSignedServer(t, &handler.Signer{Key: current, Previous: previous})
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	resp := fetchBundle(t, srv, alice, nil)

	got, err := signing.VerifyRollover(previous.Public().(ed25519.PublicKey), resp.Header.Get("X-AW-Key-Rollover"))
	if err != nil {
		t.Fatalf("VerifyRollover: %v", err)
	}
	if !got.PublicKey.Equal(current.Public().(ed25519.PublicKey)) {
		t.Fatal("the rollover announces the wrong key")
	}
}

func TestBundleWithAnEmptySnapshotIsTheAuthoredBundle(t *testing.T) {
	srv := newServer(t)
	post(t, srv, "/v1/policy/revisions", groupedRuleSet)
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	authored := decodeBundle(t, fetchBundle(t, srv, alice, nil))

	putGroups(t, srv, `{"members":{}}`, adminToken)

	synced := decodeBundle(t, fetchBundle(t, srv, alice, nil))
	if !equal(synced.Groups, authored.Groups) || len(synced.Rules) != len(authored.Rules) {
		t.Errorf("bundle changed under an empty snapshot: %+v vs %+v", synced, authored)
	}
}
