package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/store"
)

func TestApplyWithoutTheAdminTokenIs401(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/policy/revisions", baselineRuleSet, "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestApplyWithTheWrongAdminTokenIs401(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/policy/revisions", baselineRuleSet, "not-it")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminRoutesAre503WhenNoTokenIsConfigured(t *testing.T) {
	// An unset token must never mean "open": it means the route is off.
	h := handler.New(store.NewMemory(), nil)
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)

	resp := postAs(t, srv, "/v1/policy/revisions", baselineRuleSet, "anything")

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}

	if resp := putGroups(t, srv, `{"members":{}}`, "anything"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("PUT /v1/groups status = %d, want 503", resp.StatusCode)
	}
	if resp := getGroups(t, srv, "anything"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET /v1/groups status = %d, want 503", resp.StatusCode)
	}
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	srv := newServer(t)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/policy/revisions", strings.NewReader(baselineRuleSet))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201: RFC 7235 makes the auth scheme case-insensitive", resp.StatusCode)
	}
}

func TestReadingPolicyNeedsNoToken(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	resp := get(t, srv, "/v1/policy", nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the preview route stays open", resp.StatusCode)
	}
}
