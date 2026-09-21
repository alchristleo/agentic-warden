package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
)

// The install templates under deploy/managed-settings are what an
// organization pushes to every machine. A template that Claude Code rejects
// is a fleet-wide outage, so each one is held to the schema here.
func TestTheInstallTemplatesAreValidManagedSettings(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "deploy", "managed-settings")
	var checked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") || d.Name() == "aw-policy.json" {
			return nil
		}
		checked++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var settings map[string]any
		if err := json.Unmarshal(raw, &settings); err != nil {
			t.Errorf("%s: not a JSON object: %v", path, err)
			return nil
		}
		if err := schema.Validate(settings); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		helper, _ := settings["policyHelper"].(map[string]any)
		if helper == nil {
			t.Errorf("%s: an install template exists to set policyHelper", path)
			return nil
		}
		if p, _ := helper["path"].(string); !strings.HasPrefix(p, "/") && !strings.HasSuffix(p, ".exe") {
			t.Errorf("%s: policyHelper.path %q must be absolute (and end in .exe on Windows)", path, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 2 {
		t.Errorf("checked %d templates, want at least the Unix and Windows drop-ins", checked)
	}
}
