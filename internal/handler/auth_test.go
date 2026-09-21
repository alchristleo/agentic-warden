package handler_test

import (
	"net/http"
	"net/http/httptest"
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
}

func TestReadingPolicyNeedsNoToken(t *testing.T) {
	srv := newServer(t)
	applyBaseline(t, srv)

	resp := get(t, srv, "/v1/policy", nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the preview route stays open", resp.StatusCode)
	}
}
