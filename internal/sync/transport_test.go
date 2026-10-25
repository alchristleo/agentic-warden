package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent/claude"
	"github.com/alchristleo/agentic-warden/internal/sync"
)

func TestCheckTransport(t *testing.T) {
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://awd.example.com", true},
		{"http://localhost:8080", true},
		{"http://LOCALHOST:8080", true},
		{"http://127.0.0.1:8080", true},
		{"http://127.5.5.5", true},
		{"http://[::1]:8080", true},
		{"http://awd.example.com", false},
		{"http://localhost.evil.com", false},
		{"http://user@evil.com", false},
		{"http://localhost@evil.com", false},
		{"http://10.0.0.5:8080", false},
	} {
		err := sync.CheckTransport(tc.url)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.url, err)
		}
		if !tc.ok && (err == nil || !strings.Contains(err.Error(), tc.url) || !strings.Contains(err.Error(), "https")) {
			t.Errorf("%s: want an error naming the URL and https, got %v", tc.url, err)
		}
	}
	if err := sync.CheckTransport("http://admin:s3cret@evil.com"); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("want a refusal that hides the password, got %v", err)
	}
}

func TestRunRefusesASavedPlainHTTPServer(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	before, err := os.ReadFile(filepath.Join(root, claude.BundleFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := sync.SaveMachine(cfg.StateDir, sync.Machine{Server: "http://awd.example.com", MachineID: "m1", Credential: "cred", Agents: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "https") {
		t.Fatalf("err = %v, want a refusal naming https", res.Err)
	}
	after, err := os.ReadFile(filepath.Join(root, claude.BundleFile))
	if err != nil || string(after) != string(before) {
		t.Errorf("a refused cycle changed the files (err %v)", err)
	}
}
