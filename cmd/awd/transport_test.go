package main

import (
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
