package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/config"
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

func TestConsoleClientSecret(t *testing.T) {
	file := func(content string) string {
		p := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		name    string
		in      config.ConsoleConfig
		want    string
		wantErr string
	}{
		{"env value", config.ConsoleConfig{ClientSecret: "s3cret"}, "s3cret", ""},
		{"padded env value", config.ConsoleConfig{ClientSecret: "  s3cret\n"}, "s3cret", ""},
		{"whitespace-only env value", config.ConsoleConfig{ClientSecret: "   "}, "", "AWD_CONSOLE_CLIENT_SECRET is empty"},
		{"file", config.ConsoleConfig{ClientSecretFile: file("from-file\n")}, "from-file", ""},
		{"empty file", config.ConsoleConfig{ClientSecretFile: file("\n")}, "", "is empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := consoleClientSecret(tc.in)
			if tc.wantErr == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got %q, %v; want %q", got, err, tc.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
			if tc.in.ClientSecret != "" && strings.Contains(err.Error(), tc.in.ClientSecret) {
				t.Errorf("error %q echoes the secret", err)
			}
		})
	}
}
