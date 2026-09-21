package claude

import (
	"encoding/json"
	"fmt"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/policy"
)

const (
	// BundleFile is where aw-sync leaves the user's bundle for aw-policy,
	// relative to the system directory. It is the organization's policy, not
	// a secret, so it is readable by every developer on the machine.
	BundleFile = "aw-bundle.json"
	// DropInFile is the managed-settings drop-in that names the helper. It
	// never changes, but aw-sync owning it means installing governance is
	// "install the binaries, enroll" and nothing else.
	DropInFile = "managed-settings.d/50-agent-wrapper.json"
)

// helperPath is where the install procedure puts aw-policy on this OS. The
// drop-in must name the same path deploy/managed-settings documents, and a
// test holds the two together.
func helperPath(goos string) string {
	if goos == "windows" {
		return `C:\Program Files\AgentWrapper\aw-policy.exe`
	}
	return "/usr/local/bin/aw-policy"
}

// Render produces the two files Claude Code's governance rests on: the
// bundle narrowed to this agent, with repository matchers intact for
// aw-policy to resolve per launch, and the static drop-in that points
// Claude Code at aw-policy. Nothing is compiled here: matching a rule to a
// repository is the helper's job, because only it knows where a session
// runs.
//
// Every rule's managed settings are validated against the schema this
// binary carries, so a bundle aw-policy would refuse is never written.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	narrowed := narrow(bundle)
	for _, rule := range narrowed.Rules {
		if err := schema.ForAgent(Name, rule.Agents[Name].Managed); err != nil {
			return agent.Rendering{}, fmt.Errorf("claude: rule %q: %w", rule.Name, err)
		}
	}
	bundleJSON, err := json.MarshalIndent(narrowed, "", "  ")
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("claude: encoding the bundle: %w", err)
	}

	dropIn := map[string]any{"policyHelper": map[string]any{
		"path":              helperPath(a.goos()),
		"timeoutMs":         5000,
		"refreshIntervalMs": 300000,
	}}
	if err := schema.Validate(dropIn); err != nil {
		return agent.Rendering{}, fmt.Errorf("claude: the policyHelper drop-in: %w", err)
	}
	dropInJSON, err := json.MarshalIndent(dropIn, "", "  ")
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("claude: encoding the drop-in: %w", err)
	}

	return agent.Rendering{Files: []agent.File{
		{Path: BundleFile, Content: append(bundleJSON, '\n'), Mode: 0o644},
		{Path: DropInFile, Content: append(dropInJSON, '\n'), Mode: 0o644},
	}}, nil
}

// narrow keeps the rules that configure Claude, each reduced to its Claude
// entry, with matchers untouched. Other agents' settings are written to
// their own system files by their own renderers; carrying them here would
// only widen what a reader of this file learns.
func narrow(bundle *policy.Bundle) *policy.Bundle {
	out := &policy.Bundle{Groups: make([]string, 0), Rules: make([]policy.Rule, 0)}
	if bundle == nil {
		return out
	}
	out.Version = bundle.Version
	out.User = bundle.User
	out.Groups = append(out.Groups, bundle.Groups...)
	for _, rule := range bundle.Rules {
		config, ok := rule.Agents[Name]
		if !ok {
			continue
		}
		out.Rules = append(out.Rules, policy.Rule{
			Name:   rule.Name,
			Match:  rule.Match,
			Agents: map[string]policy.AgentConfig{Name: config},
		})
	}
	return out
}
