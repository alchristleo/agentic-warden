package agy

import (
	"encoding/json"
	"fmt"

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/agent/agy/managed"
	"github.com/alchristleo/agentic-warden/internal/policy"
)

// Render compiles the bundle for a session in no repository and writes the
// result as admin_settings.json. The file is written even when empty ({}),
// so a file from an earlier revision never outlives the policy that produced
// it. A static file cannot express a repo-scoped rule, so those are dropped
// here and reported in a note.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	doc := bundle.Compile("").Agent(Name).Managed
	if err := managed.Validate(doc); err != nil {
		return agent.Rendering{}, fmt.Errorf("agy: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("agy: encoding %s: %w", AdminFile, err)
	}
	return agent.Rendering{
		Files: []agent.File{{Path: AdminFile, Content: append(encoded, '\n'), Mode: 0o644}},
		Notes: agent.DroppedRepoRules(bundle, Name, AdminFile),
	}, nil
}
