package agy

import (
	"encoding/json"
	"fmt"
	"strings"

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
		Notes: dropped(bundle),
	}, nil
}

// dropped names the agy rules Compile("") left out because they are scoped
// to repositories. Rules for other agents are not this renderer's to report.
func dropped(bundle *policy.Bundle) []string {
	if bundle == nil {
		return nil
	}
	var names []string
	for _, rule := range bundle.Rules {
		if _, forAgy := rule.Agents[Name]; forAgy && len(rule.Match.Repos) > 0 {
			names = append(names, rule.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("agy: %d repo-scoped rule(s) not enforceable in %s: %s",
		len(names), AdminFile, strings.Join(names, ", "))}
}
