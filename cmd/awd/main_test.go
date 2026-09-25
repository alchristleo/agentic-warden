package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGroupsDoesNotTreatAnArbitrary500AsNoSnapshot guards against
// classifying a non-404 error as "no snapshot" merely because its message
// happens to contain the digits 404 (a real risk when a payload echoes an
// unrelated port number or similar).
func TestGroupsDoesNotTreatAnArbitrary500AsNoSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "port 404 unreachable"})
	}))
	defer srv.Close()

	if err := groups([]string{"--url", srv.URL}); err == nil {
		t.Fatal("groups() returned nil for a 500 response, want an error")
	}
}

func TestLoadSignerRejectsAMalformedKMSValue(t *testing.T) {
	if _, err := loadSigner(context.Background(), "awskms:not-an-arn"); err == nil || !strings.Contains(err.Error(), "not-an-arn") {
		t.Fatalf("err = %v", err)
	}
}
