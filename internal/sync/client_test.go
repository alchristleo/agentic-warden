package sync_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/store"
	"github.com/acme/agent-wrapper/internal/sync"
)

func TestEnrollPostsTheTokenAndReturnsTheCredential(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/machines/enroll" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"machineId":"m1","credential":"c1","user":"alice@acme.com"}`))
	}))
	defer srv.Close()

	c := &sync.Client{Server: srv.URL + "/"}
	e, err := c.Enroll(context.Background(), "tok", "host1", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got["token"] != "tok" || got["name"] != "host1" || got["os"] != "linux" {
		t.Errorf("request body = %v", got)
	}
	if e.MachineID != "m1" || e.Credential != "c1" || e.User != "alice@acme.com" {
		t.Errorf("enrollment = %+v", e)
	}
}

func TestEnrollDecodesASuccessBodyLargerThanTheErrorCap(t *testing.T) {
	// maxErrorBody is 4096 bytes; the enrollment payload itself is small,
	// so pad it past that with a long unknown field to prove the 201 path
	// does not truncate the body before decoding it.
	padding := strings.Repeat("x", 8192)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"machineId":"m1","credential":"c1","user":"alice@acme.com","padding":%q}`, padding)
	}))
	defer srv.Close()

	c := &sync.Client{Server: srv.URL}
	e, err := c.Enroll(context.Background(), "tok", "host1", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if e.MachineID != "m1" || e.Credential != "c1" || e.User != "alice@acme.com" {
		t.Errorf("enrollment = %+v", e)
	}
}

func TestEnrollReportsARejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"conflict"}`, http.StatusConflict)
	}))
	defer srv.Close()
	_, err := (&sync.Client{Server: srv.URL}).Enroll(context.Background(), "tok", "h", "linux")
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Errorf("err = %v, want one naming the 409", err)
	}
}

func bundleServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		if status == http.StatusOK {
			w.Header().Set("ETag", `"e2"`)
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestFetchSendsTheCredentialAndETag(t *testing.T) {
	srv, seen := bundleServer(t, http.StatusOK, `{"version":"v2","groups":[],"rules":[]}`)
	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/v1/bundle" || seen.Header.Get("Authorization") != "Bearer cred" || seen.Header.Get("If-None-Match") != `"e1"` {
		t.Errorf("request: %s %s %v", seen.Method, seen.URL.Path, seen.Header)
	}
	if got.Unchanged || got.ETag != `"e2"` || got.Bundle == nil || got.Bundle.Version != "v2" {
		t.Errorf("fetched = %+v", got)
	}
}

func TestFetchOn304IsUnchanged(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusNotModified, "")
	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Unchanged || got.Bundle != nil {
		t.Errorf("fetched = %+v, want Unchanged with no bundle", got)
	}
}

func TestFetchOn401IsErrUnauthorized(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", nil)
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestFetchOnServerErrorFails(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusInternalServerError, "boom")
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want one naming the status", err)
	}
}

func TestFetchRejectsAMalformedBundle(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusOK, `{"version":`)
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", nil)
	if err == nil {
		t.Error("want an error for a malformed bundle")
	}
}

func TestFetchWhenTheServerIsDownFails(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusOK, "{}")
	url := srv.URL
	srv.Close()
	_, err := (&sync.Client{Server: url}).Fetch(context.Background(), "cred", "", nil)
	if err == nil {
		t.Error("want an error when the control plane is unreachable")
	}
}

// signingServer answers every request with body, signed by signer. When
// announcer is non-nil, the response also carries a rollover statement
// announcing signer's key, signed by announcer — the shape a control plane
// mid-rotation sends.
func signingServer(t *testing.T, signer ed25519.PrivateKey, body []byte, announcer ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-AW-Signature", signing.Sign(signer, body))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(signer.Public().(ed25519.PublicKey)))
		if announcer != nil {
			header, _ := signing.SignRollover(r.Context(), signing.NewSeedSigner(announcer), signer.Public().(ed25519.PublicKey))
			w.Header().Set("X-AW-Key-Rollover", header)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// signingServerSigningOver signs signedBody but serves servedBody, the shape
// a tampered-in-transit or misconfigured response takes: the bytes on the
// wire are not the bytes the signature covers.
func signingServerSigningOver(t *testing.T, signer ed25519.PrivateKey, servedBody, signedBody []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-AW-Signature", signing.Sign(signer, signedBody))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(signer.Public().(ed25519.PublicKey)))
		_, _ = w.Write(servedBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// unsignedServer answers with body and none of the three signing headers,
// the shape a deployment that does not sign sends.
func unsignedServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchVerifiesTheSignature(t *testing.T) {
	key, _ := signing.Generate()
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := signingServer(t, key, body, nil)
	c := &sync.Client{Server: srv.URL}

	got, err := c.Fetch(context.Background(), "cred", "", key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got.Raw, body) {
		t.Fatalf("Raw = %q, want the served bytes verbatim", got.Raw)
	}
	if got.Bundle.Version != "4" {
		t.Fatalf("Version = %q", got.Bundle.Version)
	}
}

func TestFetchRejectsATamperedBody(t *testing.T) {
	key, _ := signing.Generate()
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := signingServerSigningOver(t, key, body, []byte(`{"version":"9","user":"a@b.c","rules":[]}`))
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", key.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch accepted a body the signature does not cover")
	}
}

func TestFetchRequiresASignatureWhenAKeyIsPinned(t *testing.T) {
	key, _ := signing.Generate()
	srv := unsignedServer(t, []byte(`{"version":"4"}`))
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", key.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch accepted an unsigned bundle while a key was pinned")
	}
}

func TestFetchWithNoPinnedKeySkipsVerification(t *testing.T) {
	srv := unsignedServer(t, []byte(`{"version":"4","user":"a@b.c","rules":[]}`))
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetchFollowsARollover(t *testing.T) {
	old, _ := signing.Generate()
	next, _ := signing.Generate()
	body := []byte(`{"version":"5","user":"a@b.c","rules":[]}`)
	srv := signingServer(t, next, body, old) // signs with next, announces via old
	c := &sync.Client{Server: srv.URL}
	got, err := c.Fetch(context.Background(), "cred", "", old.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Rollover == nil || !got.Rollover.PublicKey.Equal(next.Public().(ed25519.PublicKey)) {
		t.Fatal("Fetch did not report the rollover it used")
	}
}

func TestFetchRejectsAForgedRollover(t *testing.T) {
	old, _ := signing.Generate()
	stranger, _ := signing.Generate()
	next, _ := signing.Generate()
	body := []byte(`{"version":"5"}`)
	srv := signingServer(t, next, body, stranger) // announced by a key we never pinned
	c := &sync.Client{Server: srv.URL}
	if _, err := c.Fetch(context.Background(), "cred", "", old.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch followed a rollover the pinned key did not sign")
	}
}

func TestFetchSendsThePinnedKeyID(t *testing.T) {
	key, _ := signing.Generate()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-AW-Key-Id")
		body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
		w.Header().Set("X-AW-Signature", signing.Sign(key, body))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(key.Public().(ed25519.PublicKey)))
		w.Write(body)
	}))
	defer srv.Close()
	pub := key.Public().(ed25519.PublicKey)
	if _, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", pub); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if seen != signing.KeyID(pub) {
		t.Fatalf("the request sent X-AW-Key-Id %q, want %q", seen, signing.KeyID(pub))
	}
}

// notModifiedServer answers every request 304, carrying rollover when that
// is non-empty — the shape a control plane mid-rotation sends to a machine
// whose policy has not changed, which on a stable fleet is every machine.
func notModifiedServer(t *testing.T, rollover string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rollover != "" {
			w.Header().Set("X-AW-Key-Rollover", rollover)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchReadsARolloverOnA304(t *testing.T) {
	old, _ := signing.Generate()
	next, _ := signing.Generate()
	nextPub := next.Public().(ed25519.PublicKey)
	header, err := signing.SignRollover(context.Background(), signing.NewSeedSigner(old), nextPub)
	if err != nil {
		t.Fatal(err)
	}
	srv := notModifiedServer(t, header)

	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`, old.Public().(ed25519.PublicKey))

	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !got.Unchanged || got.Bundle != nil {
		t.Errorf("fetched = %+v, want Unchanged with no bundle", got)
	}
	if got.Rollover == nil || !got.Rollover.PublicKey.Equal(nextPub) {
		t.Fatal("Fetch ignored the rollover on a 304, so a rotation could never finish on a fleet whose policy is stable")
	}
}

func TestFetchRejectsAForgedRolloverOnA304(t *testing.T) {
	old, _ := signing.Generate()
	stranger, _ := signing.Generate()
	next, _ := signing.Generate()
	// Announced by a key this machine never pinned. There is no bundle on a
	// 304 whose own check would catch it later, so it has to be caught here.
	header, err := signing.SignRollover(context.Background(), signing.NewSeedSigner(stranger), next.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	srv := notModifiedServer(t, header)

	if _, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`, old.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("Fetch followed a 304's rollover that the pinned key did not sign")
	}
}

func TestFetchAdvertisesBothFormats(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-AW-Signature-Formats")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	_, _ = (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "etag", nil)
	if got != "v1, v2" {
		t.Fatalf("header %q", got)
	}
}

// mintAndEnroll mints an enrollment token as an administrator and enrolls a
// machine for user against srv, using the same sync.Client production code
// a real machine would, so these tests exercise the real request/response
// shape rather than a hand-built stand-in.
func mintAndEnroll(t *testing.T, srv *httptest.Server, adminToken, user string) sync.Enrollment {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/enrollment-tokens", strings.NewReader(fmt.Sprintf(`{"user":%q}`, user)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("minting a token: status = %d", resp.StatusCode)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatal(err)
	}
	e, err := (&sync.Client{Server: srv.URL}).Enroll(context.Background(), tok.Token, "laptop", "linux")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// realSignedServer starts a real handler.Handler backed by an in-memory
// store, with signer wired on, and enrolls one machine against it. It
// returns the server (so a test can still reach the *handler.Handler to
// change Signer mid-test, e.g. to simulate a rotation) and the enrollment,
// which carries the key this machine pinned.
const testAdminToken = "test-admin-token"

func realSignedServer(t *testing.T, signer *handler.Signer) (*handler.Handler, *httptest.Server, sync.Enrollment) {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.AdminToken = testAdminToken
	h.Signer = signer
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	e := mintAndEnroll(t, srv, testAdminToken, "alice@acme.com")
	return h, srv, e
}

// v2OnlySigner stands in for a KMS-backed signer the way the handler tests
// do: it can speak only v2, so an old aw-sync — or one that negotiates
// nothing — gets a 426 from the real handler rather than a response.
type v2OnlySigner struct{ signing.Signer }

func (v2OnlySigner) Formats() []string { return []string{signing.FormatV2} }

func TestFetchVerifiesV2AgainstASeedServer(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, srv, e := realSignedServer(t, &handler.Signer{Current: signing.NewSeedSigner(key)})
	pinned, err := signing.ParsePublic(e.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), e.Credential, "", pinned)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Format != signing.FormatV2 {
		t.Fatalf("Format = %q, want v2", got.Format)
	}
	if !signing.VerifyBundle(signing.FormatV2, pinned, got.Raw, got.Signature) {
		t.Fatal("the signature does not verify as v2 over the served bytes")
	}
}

func TestFetchV2KeyIDMismatchNamesBothIDs(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	const servedKeyID = "0000000000000000"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-AW-Signature-Format", signing.FormatV2)
		w.Header().Set("X-AW-Key-Id", servedKeyID)
		w.Header().Set("X-AW-Signature", signing.Sign(key, signing.BundleStatement(signing.KeyID(pub), body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	_, err = (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", pub)
	if err == nil || !strings.Contains(err.Error(), servedKeyID) || !strings.Contains(err.Error(), signing.KeyID(pub)) {
		t.Fatalf("err = %v, want it to name both %s and %s", err, servedKeyID, signing.KeyID(pub))
	}
}

func TestFetchV2BadSignatureFails(t *testing.T) {
	pinned, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	other, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := pinned.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-AW-Signature-Format", signing.FormatV2)
		w.Header().Set("X-AW-Key-Id", signing.KeyID(pub))
		w.Header().Set("X-AW-Signature", signing.Sign(other, signing.BundleStatement(signing.KeyID(pub), body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	_, err = (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", pub)
	want := "not signed by key " + signing.KeyID(pub)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want it to contain %q", err, want)
	}
}

func TestFetchUnknownFormatIsNamed(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-AW-Signature-Format", "v7")
		w.Header().Set("X-AW-Key-Id", signing.KeyID(pub))
		w.Header().Set("X-AW-Signature", signing.Sign(key, body))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	_, err = (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", pub)
	if err == nil || !strings.Contains(err.Error(), "v7") {
		t.Fatalf("err = %v, want it to name v7", err)
	}
}

func TestFetchNoFormatHeaderIsV1(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"4","user":"a@b.c","rules":[]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An awd from before v2: no X-AW-Signature-Format header at all.
		w.Header().Set("X-AW-Signature", signing.Sign(key, body))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(pub))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", pub)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Format != signing.FormatV1 {
		t.Fatalf("Format = %q, want v1", got.Format)
	}
}

// Review Focus 1.
func TestFetchRepinsFromSeedOntoAV2OnlySigner(t *testing.T) {
	seedKey, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	seed := signing.NewSeedSigner(seedKey)
	kmsKey, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	kms := v2OnlySigner{signing.NewSeedSigner(kmsKey)}

	// Enrolled while the server still signs with the seed, so this machine
	// pins the seed's key exactly as a machine enrolled before the KMS
	// rotation would have.
	h, srv, e := realSignedServer(t, &handler.Signer{Current: seed})
	pinned, err := signing.ParsePublic(e.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	// The control plane rotates onto KMS; the seed now only signs the
	// rollover.
	h.Signer = &handler.Signer{Current: kms, Previous: seed}

	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), e.Credential, "", pinned)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	kmsPub := kmsKey.Public().(ed25519.PublicKey)
	if got.Rollover == nil || !got.Rollover.PublicKey.Equal(kmsPub) {
		t.Fatalf("Rollover = %+v, want it to announce the KMS key", got.Rollover)
	}
	if got.Format != signing.FormatV2 {
		t.Fatalf("Format = %q, want v2", got.Format)
	}
	if !signing.VerifyBundle(signing.FormatV2, kmsPub, got.Raw, got.Signature) {
		t.Fatal("the signature does not verify with the new KMS-style key")
	}
}

func TestFetchAgainstV2OnlyServerWithOldClientBehaviour(t *testing.T) {
	// Fetch always advertises both formats, so it cannot itself produce the
	// request an old aw-sync would send. This documents what that old
	// client sees instead: a hand-crafted 426, as a KMS-only signer answers
	// a client that never offered v2.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"aw-sync too old for this control plane's signing; upgrade aw-sync to a build that supports signature format v2"}`, http.StatusUpgradeRequired)
	}))
	defer srv.Close()

	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "", nil)
	if err == nil || !strings.Contains(err.Error(), "upgrade aw-sync") {
		t.Fatalf("err = %v, want it to mention upgrading aw-sync; the machine keeps its current bundle either way", err)
	}
}
