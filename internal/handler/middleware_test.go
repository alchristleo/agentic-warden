package handler_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acme/agent-wrapper/internal/handler"
)

func panickingHandler(http.ResponseWriter, *http.Request) {
	panic("boom")
}

// TestRecoveryAnswersSCIMErrorUnderTheSCIMPrefix checks that a panic inside a
// /scim/v2 route is turned into the SCIM error body an IdP's provisioning
// log expects, not the control plane's plain {"error": ...} JSON.
func TestRecoveryAnswersSCIMErrorUnderTheSCIMPrefix(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)

	handler.Recovery(slog.Default())(http.HandlerFunc(panickingHandler)).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/scim+json" {
		t.Errorf("Content-Type = %q, want application/scim+json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "500" {
		t.Errorf(`body["status"] = %v, want "500"`, body["status"])
	}
}

// TestRecoveryKeepsThePlainErrorBodyOutsideSCIM checks that every other
// route is unaffected by the SCIM special case.
func TestRecoveryKeepsThePlainErrorBodyOutsideSCIM(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/policy", nil)

	handler.Recovery(slog.Default())(http.HandlerFunc(panickingHandler)).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "internal error" {
		t.Errorf("error = %q, want %q", body["error"], "internal error")
	}
}
