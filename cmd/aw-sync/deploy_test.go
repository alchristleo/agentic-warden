package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The timer units are what an organization installs on every machine. One
// that does not run `aw-sync once` syncs nothing, silently, fleet-wide.
func TestEveryTimerUnitRunsOnce(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "aw-sync")
	units := map[string]string{
		"systemd/aw-sync.service":                 "aw-sync once",
		"systemd/aw-sync.timer":                   "OnUnitActiveSec",
		"launchd/com.agent-wrapper.aw-sync.plist": "<string>once</string>",
		"windows/register-task.ps1":               "once",
	}
	for rel, want := range units {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s does not contain %q", rel, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Errorf("deploy/aw-sync/README.md: %v", err)
	}
}
