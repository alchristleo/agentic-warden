package handler_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	srv := newSignedServer(t, &handler.Signer{Current: signing.NewSeedSigner(key)})
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
	srv := newSignedServer(t, &handler.Signer{Current: signing.NewSeedSigner(key)})
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
	srv := newSignedServer(t, &handler.Signer{Current: signing.NewSeedSigner(current), Previous: signing.NewSeedSigner(previous)})
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

func TestNotModifiedStillCarriesTheRollover(t *testing.T) {
	// A fleet whose policy is stable answers 304 to every cycle. If the
	// rollover rode only on a changed bundle, no machine there would ever
	// repin, and the operator who then retires the previous key strands the
	// lot of them. The statement is signed by the outgoing key and says
	// nothing about the body, so a 304 can carry it honestly.
	previous, _ := signing.Generate()
	current, _ := signing.Generate()
	srv := newSignedServer(t, &handler.Signer{Current: signing.NewSeedSigner(current), Previous: signing.NewSeedSigner(previous)})
	_, alice := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	first := fetchBundle(t, srv, alice, nil)

	second := fetchBundle(t, srv, alice, http.Header{"If-None-Match": {first.Header.Get("ETag")}})

	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.StatusCode)
	}
	got, err := signing.VerifyRollover(previous.Public().(ed25519.PublicKey), second.Header.Get("X-AW-Key-Rollover"))
	if err != nil {
		t.Fatalf("VerifyRollover on a 304: %v", err)
	}
	if !got.PublicKey.Equal(current.Public().(ed25519.PublicKey)) {
		t.Fatal("the rollover on the 304 announces the wrong key")
	}
	if second.Header.Get("X-AW-Signature") != "" {
		t.Error("a 304 carried a signature; there is still no body to sign")
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

// newSignedServerWithMachine is newSignedServer plus an enrolled machine: it
// mints an enrollment token as admin and enrolls it, returning the
// credential. A nil signer is an unsigned server.
func newSignedServerWithMachine(t *testing.T, signer *handler.Signer) (*httptest.Server, string) {
	t.Helper()
	srv := newSignedServer(t, signer)
	_, credential := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	return srv, credential
}

// postLargePolicy posts a revision whose baseline rule's claude managed
// settings hold one env value of n bytes, so the compiled bundle is at
// least n bytes. newServer sets no ManagedValidator, so any key is accepted.
func postLargePolicy(t *testing.T, srv *httptest.Server, n int) {
	t.Helper()
	pad := strings.Repeat("x", n)
	body := fmt.Sprintf(`{"version":"big","rules":[{"name":"baseline","agents":{"claude":{"managed":{"env":{"PAD":%q}}}}}]}`, pad)
	if resp := post(t, srv, "/v1/policy/revisions", body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("postLargePolicy: status = %d", resp.StatusCode)
	}
}

// v2OnlySigner stands in for KMS: a seed that only speaks v2, fails on
// demand, and refuses messages over 4096 bytes like the real one.
type v2OnlySigner struct {
	signing.Signer
	fail bool
}

func (v *v2OnlySigner) Formats() []string { return []string{signing.FormatV2} }
func (v *v2OnlySigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	if len(msg) > 4096 {
		return nil, fmt.Errorf("message of %d bytes exceeds 4096", len(msg))
	}
	if v.fail {
		return nil, errors.New("KMSInternalException: boom")
	}
	return v.Signer.Sign(ctx, msg)
}

func bundleReq(t *testing.T, srv *httptest.Server, credential, formats, etag string) *http.Response {
	t.Helper()
	h := http.Header{"Authorization": {"Bearer " + credential}}
	if formats != "" {
		h.Set("X-AW-Signature-Formats", formats)
	}
	if etag != "" {
		h.Set("If-None-Match", etag)
	}
	return get(t, srv, "/v1/bundle", h)
}

func TestNegotiationTable(t *testing.T) {
	key, _ := signing.Generate()
	seed := signing.NewSeedSigner(key)
	kms := &v2OnlySigner{Signer: seed}
	cases := []struct {
		name       string
		signer     signing.Signer
		formats    string
		wantStatus int
		wantFormat string
	}{
		{"seed, new client", seed, "v1, v2", 200, "v2"},
		{"seed, old client", seed, "", 200, "v1"},
		{"seed, v1 only", seed, "v1", 200, "v1"},
		{"seed, unknown only", seed, "v9", 200, "v1"},
		{"kms, new client", kms, "v1, v2", 200, "v2"},
		{"kms, old client", kms, "", 426, ""},
		{"kms, unknown only", kms, "v9", 426, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: tc.signer})
			resp := bundleReq(t, srv, credential, tc.formats, "")
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == 426 {
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), "upgrade aw-sync to a build that supports signature format v2") {
					t.Fatalf("426 body %s", body)
				}
				return
			}
			got := resp.Header.Get("X-AW-Signature-Format")
			if got != tc.wantFormat {
				t.Fatalf("format %q, want %q", got, tc.wantFormat)
			}
			body, _ := io.ReadAll(resp.Body)
			if !signing.VerifyBundle(got, tc.signer.Public(), body, resp.Header.Get("X-AW-Signature")) {
				t.Fatal("signature does not verify")
			}
		})
	}
}

func TestUnsignedServerSendsNoSignatureHeaders(t *testing.T) {
	srv, credential := newSignedServerWithMachine(t, nil)
	resp := bundleReq(t, srv, credential, "v1, v2", "")
	for _, h := range []string{"X-AW-Signature", "X-AW-Signature-Format", "X-AW-Key-Id"} {
		if resp.Header.Get(h) != "" {
			t.Fatalf("%s set on an unsigned server", h)
		}
	}
}

// Review Focus 3.
func TestKMSStyleSignerSignsALargeBundle(t *testing.T) {
	key, _ := signing.Generate()
	kms := &v2OnlySigner{Signer: signing.NewSeedSigner(key)}
	srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: kms})
	postLargePolicy(t, srv, 100<<10) // 100 KiB of managed settings
	resp := bundleReq(t, srv, credential, "v1, v2", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(body) < 100<<10 || !signing.VerifyBundle("v2", kms.Public(), body, resp.Header.Get("X-AW-Signature")) {
		t.Fatalf("status %d, len %d", resp.StatusCode, len(body))
	}
}

func TestSignerFailureIs503NeverUnsigned(t *testing.T) {
	key, _ := signing.Generate()
	kms := &v2OnlySigner{Signer: signing.NewSeedSigner(key), fail: true}
	srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: kms})
	resp := bundleReq(t, srv, credential, "v1, v2", "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 || !strings.Contains(string(body), "signing unavailable") || strings.Contains(string(body), `"version"`) {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
}

func TestRolloverOn304UsesCacheAndFailsClosedWhenUncached(t *testing.T) {
	cur, _ := signing.Generate()
	prevKey, _ := signing.Generate()
	prev := &v2OnlySigner{Signer: signing.NewSeedSigner(prevKey)}
	srv, credential := newSignedServerWithMachine(t, &handler.Signer{Current: signing.NewSeedSigner(cur), Previous: prev})
	first := bundleReq(t, srv, credential, "v1, v2", "")
	etag := first.Header.Get("ETag")
	if first.Header.Get("X-AW-Key-Rollover") == "" {
		t.Fatal("no rollover on the 200")
	}
	prev.fail = true
	cached := bundleReq(t, srv, credential, "v1, v2", etag)
	if cached.StatusCode != 304 || cached.Header.Get("X-AW-Key-Rollover") == "" {
		t.Fatalf("cached 304 = %d rollover %q", cached.StatusCode, cached.Header.Get("X-AW-Key-Rollover"))
	}
	// A fresh server has an empty cache: the same failure is a 503.
	srv2, credential2 := newSignedServerWithMachine(t, &handler.Signer{Current: signing.NewSeedSigner(cur), Previous: prev})
	if resp := bundleReq(t, srv2, credential2, "v1, v2", etag); resp.StatusCode != 503 {
		t.Fatalf("uncached rollover failure = %d, want 503", resp.StatusCode)
	}
}
