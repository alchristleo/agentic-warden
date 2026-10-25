package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminCLIRefusesPlainHTTP(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKEN", "secret")
	const remote = "http://awd.example.com"
	for name, call := range map[string]func() error{
		"enroll-token": func() error { return enrollToken([]string{"--url", remote, "alice"}) },
		"machines":     func() error { return machines([]string{"--url", remote}) },
		"revoke":       func() error { return revoke([]string{"--url", remote, "m1"}) },
		"apply":        func() error { return apply([]string{"--url", remote, filepath.Join(t.TempDir(), "p.yaml")}) },
	} {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("%s: err = %v, want a refusal naming https", name, err)
		}
	}
}

func TestAdminCLIRefusesRedirectToPlainHTTP(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKEN", "secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/x", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	for name, call := range map[string]func() error{
		"machines": func() error { return machines([]string{"--url", srv.URL}) },
		"apply": func() error {
			p := filepath.Join(t.TempDir(), "p.yaml")
			if err := os.WriteFile(p, []byte("version: 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return apply([]string{"--url", srv.URL, p})
		},
	} {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("%s: err = %v, want a refusal naming https", name, err)
		}
	}
}
