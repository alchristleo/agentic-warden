package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRedirectCap(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKEN", "secret")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	policy := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(policy, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"apply":    func() error { return apply([]string{"--url", srv.URL, policy}) },
		"machines": func() error { return machines([]string{"--url", srv.URL}) },
	} {
		hits.Store(0)
		err := call()
		if err == nil || !strings.Contains(err.Error(), "10 redirects") {
			t.Errorf("%s: err = %v, want the redirect cap", name, err)
		}
		if hits.Load() != 10 {
			t.Errorf("%s: %d requests, want 10", name, hits.Load())
		}
	}
}
