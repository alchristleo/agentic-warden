package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/credential"
	"github.com/alchristleo/agentic-warden/internal/handler"
	"github.com/alchristleo/agentic-warden/internal/store"
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

func TestReadingPolicyNeedsAdmin(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	if resp := get(t, srv, "/v1/policy", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", resp.StatusCode)
	}
	if resp := getAdmin(t, srv, "/v1/policy", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("admin token status = %d, want 200", resp.StatusCode)
	}
}

func serverWithTokens(t *testing.T, tokens map[string]string) *httptest.Server {
	t.Helper()
	h := handler.New(store.NewMemory(), nil)
	h.AdminTokens = tokens
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func TestAdminTokensBothNamesPass(t *testing.T) {
	srv := serverWithTokens(t, map[string]string{
		"alice": credential.Hash("alice-secret"),
		"bob":   credential.Hash("bob-secret"),
	})

	for _, token := range []string{"alice-secret", "bob-secret"} {
		if resp := getAs(t, srv, "/v1/audit", token); resp.StatusCode != http.StatusOK {
			t.Errorf("token %q: status = %d, want 200", token, resp.StatusCode)
		}
	}
	if resp := getAs(t, srv, "/v1/audit", "carol-secret"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown token: status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminTokensActorIgnoresAppliedBy(t *testing.T) {
	srv := serverWithTokens(t, map[string]string{"alice": credential.Hash("alice-secret")})
	req, _ := http.NewRequest("POST", srv.URL+"/v1/enrollment-tokens", strings.NewReader(`{"user":"bob@example.com"}`))
	req.Header.Set("Authorization", "Bearer alice-secret")
	req.Header.Set("X-Applied-By", "mallory")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	events := auditEvents(t, getAs(t, srv, "/v1/audit", "alice-secret"))

	if len(events) != 1 || events[0].Actor != "token:alice" {
		t.Errorf("events = %+v, want one with actor token:alice", events)
	}
}

func TestAdminTokensRemovedNameFails(t *testing.T) {
	both := map[string]string{"alice": credential.Hash("alice-secret"), "bob": credential.Hash("bob-secret")}
	if resp := getAs(t, serverWithTokens(t, both), "/v1/audit", "bob-secret"); resp.StatusCode != http.StatusOK {
		t.Fatalf("before removal: status = %d, want 200", resp.StatusCode)
	}

	onlyAlice := map[string]string{"alice": credential.Hash("alice-secret")}
	resp := getAs(t, serverWithTokens(t, onlyAlice), "/v1/audit", "bob-secret")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("after removal: status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminTokensSingleLegacyIsAdmin(t *testing.T) {
	srv := serverWithTokens(t, map[string]string{"admin": credential.Hash("legacy")})
	req, _ := http.NewRequest("POST", srv.URL+"/v1/enrollment-tokens", strings.NewReader(`{"user":"bob@example.com"}`))
	req.Header.Set("Authorization", "Bearer legacy")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	events := auditEvents(t, getAs(t, srv, "/v1/audit", "legacy"))

	if len(events) != 1 || events[0].Actor != "token:admin" {
		t.Errorf("events = %+v, want one with actor token:admin", events)
	}
}

func TestNoAdminTokensIs503(t *testing.T) {
	srv := serverWithTokens(t, nil)

	if resp := getAs(t, srv, "/v1/audit", "anything"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestAdminTokensEmptyBearerNeverAuthenticates(t *testing.T) {
	srv := serverWithTokens(t, map[string]string{"empty": credential.Hash("")})

	for _, header := range []string{"", "Bearer ", "Bearer"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/audit", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", header, resp.StatusCode)
		}
	}
}
