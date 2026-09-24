package main_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync/timer"
)

var update = flag.Bool("update", false, "rewrite deploy/aw-sync's units from the embedded templates")

// The deploy units are what an organization ships when it pushes files
// instead of running install-timer. They must be exactly what
// install-timer would install with the default binary path and interval,
// so the two ways of installing cannot drift apart.
func TestDeployUnitsMatchTheTemplates(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "aw-sync")
	for _, goos := range []string{"linux", "darwin", "windows"} {
		units, err := timer.Render(goos, timer.Default(goos))
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		for _, u := range units {
			path := filepath.Join(root, timer.DeployDir(goos), u.Name)
			// aw-sync.timer only schedules aw-sync.service; the command
			// itself lives in the service unit's ExecStart, so the timer
			// unit alone is exempt from this check.
			if u.Name != "aw-sync.timer" && !bytes.Contains(u.Content, []byte("once")) {
				t.Errorf("%s does not run `aw-sync once`; it would sync nothing, fleet-wide", path)
			}
			if *update {
				if err := os.WriteFile(path, u.Content, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				continue
			}
			if !bytes.Equal(got, u.Content) {
				t.Errorf("%s differs from timer.Render(%q, Default); regenerate with: go test ./cmd/aw-sync -run TestDeployUnitsMatchTheTemplates -update", path, goos)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Errorf("deploy/aw-sync/README.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "windows", "register-task.ps1")); err == nil {
		t.Error("windows/register-task.ps1 still ships; aw-sync-task.xml replaced it")
	}
}
