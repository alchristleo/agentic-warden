package handler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.Now = func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) }
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func get(t *testing.T, srv *httptest.Server, path string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, values := range header {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeDocument(t *testing.T, resp *http.Response) policy.Document {
	t.Helper()
	var doc policy.Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	return doc
}

const baselineRuleSet = `{
  "version": "v1",
  "rules": [
    {
      "name": "baseline",
      "agents": {"claude": {"managed": {"permissions": {"deny": ["Read(./.env)"]}}}}
    },
    {
      "name": "platform",
      "match": {"groups": ["platform"]},
      "agents": {"claude": {"managed": {"model": "opus"}}}
    },
    {
      "name": "payments",
      "match": {"repos": ["github.com/acme/payments*"]},
      "agents": {"claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}}}
    }
  ]
}`

func applyBaseline(t *testing.T, srv *httptest.Server) {
	t.Helper()
	if resp := post(t, srv, "/v1/policy/revisions", baselineRuleSet); resp.StatusCode != http.StatusCreated {
		t.Fatalf("applying the baseline rule set: status = %d, want 201", resp.StatusCode)
	}
}

func TestHealthzReportsTheProcessIsUp(t *testing.T) {
	srv := newServer(t)

	if resp := get(t, srv, "/healthz", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestReadyzReportsTheStoreIsReachable(t *testing.T) {
	srv := newServer(t)

	if resp := get(t, srv, "/readyz", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPolicyBeforeAnyRevisionIsAnEmptyDocumentRatherThanAnError(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, "/v1/policy", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: a client must never be pushed into its fail-safe path by an org that has no policy yet", resp.StatusCode)
	}
	if doc := decodeDocument(t, resp); len(doc.Agents) != 0 {
		t.Errorf("Agents = %v, want none", doc.Agents)
	}
}

func TestApplyingARuleSetThenFetchingItCompilesTheBaseline(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	doc := decodeDocument(t, get(t, srv, "/v1/policy", nil))

	managed := doc.Agent("claude").Managed
	permissions, _ := managed["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Read(./.env)" {
		t.Errorf("permissions.deny = %v, want only the baseline rule", deny)
	}
	if _, targeted := managed["model"]; targeted {
		t.Error("a group-targeted key reached a subject with no groups")
	}
}

func TestPolicyAppliesGroupTargeting(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	doc := decodeDocument(t, get(t, srv, "/v1/policy?group=platform", nil))

	if got := doc.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("model = %v, want the platform rule to apply", got)
	}
}

func TestPolicyAppliesRepositoryTargeting(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	doc := decodeDocument(t, get(t, srv, "/v1/policy?repo=github.com/acme/payments-api", nil))

	permissions, _ := doc.Agent("claude").Managed["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	if len(deny) != 2 {
		t.Errorf("permissions.deny = %v, want the baseline and payments rules unioned", deny)
	}
}

func TestPolicyNamesTheRulesThatApplied(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	doc := decodeDocument(t, get(t, srv, "/v1/policy?group=platform", nil))

	if len(doc.AppliedRules) != 2 || doc.AppliedRules[0] != "baseline" {
		t.Errorf("AppliedRules = %v, want the audit trail of applied rules", doc.AppliedRules)
	}
}

func TestPolicyIsCacheableWithAnETag(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	first := get(t, srv, "/v1/policy?group=platform", nil)
	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the response, want one so a client can revalidate cheaply")
	}

	again := get(t, srv, "/v1/policy?group=platform", http.Header{"If-None-Match": {etag}})

	if again.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304 for a matching ETag", again.StatusCode)
	}
}

func TestADifferentSubjectGetsADifferentETag(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	platform := get(t, srv, "/v1/policy?group=platform", nil).Header.Get("ETag")
	design := get(t, srv, "/v1/policy?group=design", nil).Header.Get("ETag")

	if platform == design {
		t.Errorf("both subjects got ETag %q; a cached policy for one group would be served to another", platform)
	}
}

func TestApplyingTheSameVersionTwiceIsRejected(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	resp := post(t, srv, "/v1/policy/revisions", baselineRuleSet)

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409: a revision is immutable once applied", resp.StatusCode)
	}
}

func TestApplyingARuleSetWithoutAVersionIsRejected(t *testing.T) {
	srv := newServer(t)

	resp := post(t, srv, "/v1/policy/revisions", `{"rules":[]}`)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestApplyingMalformedJSONIsRejected(t *testing.T) {
	srv := newServer(t)

	resp := post(t, srv, "/v1/policy/revisions", `{"version":`)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRevisionsListNewestFirst(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)
	if resp := post(t, srv, "/v1/policy/revisions", `{"version":"v2","rules":[]}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("applying v2: status = %d, want 201", resp.StatusCode)
	}

	resp := get(t, srv, "/v1/policy/revisions", nil)

	var revisions []struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&revisions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(revisions) != 2 || revisions[0].Version != "v2" {
		t.Errorf("revisions = %v, want v2 first", revisions)
	}
}

func TestAnUnknownPathIs404(t *testing.T) {
	srv := newServer(t)

	if resp := get(t, srv, "/v1/nothing-here", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTheWrongMethodIsRejected(t *testing.T) {
	srv := newServer(t)

	resp := post(t, srv, "/v1/policy", `{}`)

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestResponsesAreJSON(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, "/v1/policy", nil)

	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
}

func TestApplyingARuleSetThatFailsManagedValidationIsRejected(t *testing.T) {
	h := handler.New(store.NewMemory(), nil)
	h.ManagedValidator = func(agentName string, managed map[string]any) error {
		if agentName == "claude" && managed["model"] == 42.0 {
			return errors.New("model must be a string")
		}
		return nil
	}
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)

	resp := post(t, srv, "/v1/policy/revisions",
		`{"version":"v1","rules":[{"name":"bad","agents":{"claude":{"managed":{"model":42}}}}]}`)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "model must be a string") {
		t.Errorf("error %q should carry the validator's message to the author", body["error"])
	}
}
