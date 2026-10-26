package claude_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent/claude"
)

func TestReadConfig(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), claude.PolicyConfigFile)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := claude.ReadConfig(write(`{"requireBundle":false}`))
	if err != nil || cfg.RequireBundle == nil || *cfg.RequireBundle {
		t.Errorf("false: %+v, %v", cfg, err)
	}
	cfg, err = claude.ReadConfig(write(`{"requireBundle":null}`))
	if err != nil || cfg.RequireBundle != nil {
		t.Errorf("null: %+v, %v", cfg, err)
	}
	if _, err := claude.ReadConfig(write(`{"serverUrl":"https://x"}`)); err == nil {
		t.Error("unknown key accepted")
	}
	if _, err := claude.ReadConfig(filepath.Join(t.TempDir(), "absent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v, want ErrNotExist", err)
	}
}
