package codex

import (
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/codex/requirements"
	"github.com/acme/agent-wrapper/internal/policy"
)

// RequirementsFile is Codex's enforced configuration, relative to its system
// directory. aw-sync owns the whole file: Codex has no drop-in directory,
// so there is nothing to share with another author.
const RequirementsFile = "requirements.toml"

// Render compiles the bundle for a session in no repository and writes the
// result as requirements.toml. A static file cannot express a repo-scoped
// rule, so those are dropped here and reported in a note; silence would
// leave an author believing a restriction applies when it does not.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	managed := bundle.Compile("").Agent(Name).Managed
	if err := requirements.Validate(managed); err != nil {
		return agent.Rendering{}, fmt.Errorf("codex: %w", err)
	}
	var body []byte
	if len(managed) > 0 {
		var err error
		body, err = toml.Marshal(managed)
		if err != nil {
			return agent.Rendering{}, fmt.Errorf("codex: encoding requirements.toml: %w", err)
		}
	}
	content := append([]byte(header(bundle)), body...)
	return agent.Rendering{
		Files: []agent.File{{Path: RequirementsFile, Content: content, Mode: 0o644}},
		Notes: dropped(bundle),
	}, nil
}

// header is the first line of the file. TOML has no sidecar convention, so
// the revision rides in a comment, where `aw-sync status` and a curious
// operator can both read it.
func header(bundle *policy.Bundle) string {
	version := "unversioned"
	if bundle != nil && bundle.Version != "" {
		version = bundle.Version
	}
	return "# Managed by aw-sync from policy revision " + version + ". Do not edit: the next sync overwrites this file.\n\n"
}

// dropped names the Codex rules Compile("") left out because they are scoped
// to repositories. Rules for other agents are not this renderer's to report.
func dropped(bundle *policy.Bundle) []string {
	if bundle == nil {
		return nil
	}
	var names []string
	for _, rule := range bundle.Rules {
		if _, forCodex := rule.Agents[Name]; forCodex && len(rule.Match.Repos) > 0 {
			names = append(names, rule.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("codex: %d repo-scoped rule(s) not enforceable in requirements.toml: %s",
		len(names), strings.Join(names, ", "))}
}

// DecodeForTest parses a rendered document back into a map. It exists so the
// package's tests can assert a round trip without importing the encoder
// themselves.
func DecodeForTest(content []byte, into *map[string]any) error {
	return toml.Unmarshal(content, into)
}
