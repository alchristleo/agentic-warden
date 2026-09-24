package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
	"github.com/acme/agent-wrapper/internal/policy"
)

// SettingsFile is Gemini's system settings, relative to its system
// directory. aw-sync owns the whole file: it has no drop-in directory, so
// there is nothing to share with another author. Its revision rides in the
// .aw-revision sibling that sync writes for every JSON file, since JSON has
// no comments.
const SettingsFile = "settings.json"

// PoliciesFile is aw-sync's one file in Gemini's admin policy directory,
// which is a drop-in directory: other administrators' .toml files beside it
// are left alone. The revision rides in a header comment.
const PoliciesFile = "policies/50-agent-wrapper.toml"

// Render compiles the bundle for a session in no repository and writes the
// result as settings.json and the admin policy file. Both are written even
// when empty, so a file from an earlier revision never outlives the policy
// that produced it. A static file cannot express a repo-scoped rule, so
// those are dropped here and reported in a note; silence would leave an
// author believing a restriction applies when it does not.
func (a *Adapter) Render(bundle *policy.Bundle) (agent.Rendering, error) {
	doc := bundle.Compile("").Agent(Name).Managed
	if err := managed.Validate(doc); err != nil {
		return agent.Rendering{}, fmt.Errorf("gemini: %w", err)
	}
	settings, policies, err := managed.Parts(doc)
	if err != nil {
		return agent.Rendering{}, fmt.Errorf("gemini: %w", err)
	}

	settingsBytes, err := settingsJSON(settings)
	if err != nil {
		return agent.Rendering{}, err
	}
	policiesBytes, err := policiesTOML(header(bundle), policies)
	if err != nil {
		return agent.Rendering{}, err
	}
	// SettingsFile is listed first so its write creates the agent root:
	// under a strict umask, cache.MkdirMode fixes only the leaf directory
	// it creates, so the root must come from the root-level file, not from
	// MkdirAll creating it as an unfixed parent of policies/.
	return agent.Rendering{
		Files: []agent.File{
			{Path: SettingsFile, Content: settingsBytes, Mode: 0o644},
			{Path: PoliciesFile, Content: policiesBytes, Mode: 0o644},
		},
		Notes: dropped(bundle),
	}, nil
}

// settingsJSON encodes the settings object as Gemini reads it: indented,
// keys sorted by the encoder, trailing newline, {} for nothing.
func settingsJSON(settings map[string]any) ([]byte, error) {
	if settings == nil {
		settings = map[string]any{}
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("gemini: encoding settings.json: %w", err)
	}
	return append(encoded, '\n'), nil
}

// policiesTOML encodes the policy list as one [[rule]] table per entry
// under the given header; the header alone when there are none.
func policiesTOML(header string, policies []any) ([]byte, error) {
	var rules []byte
	if len(policies) > 0 {
		var err error
		rules, err = toml.Marshal(map[string]any{"rule": integerPriorities(policies)})
		if err != nil {
			return nil, fmt.Errorf("gemini: encoding policies: %w", err)
		}
	}
	return append([]byte(header), rules...), nil
}

// integerPriorities copies each rule with its priority as an int64. Numbers
// reach a renderer as float64 after JSON decoding, and go-toml writes
// float64(100) as 100.0, which Gemini's loader rejects as a non-integer.
// Validate has already established every priority is a whole number.
func integerPriorities(policies []any) []any {
	out := make([]any, 0, len(policies))
	for _, entry := range policies {
		rule, _ := entry.(map[string]any)
		copied := make(map[string]any, len(rule))
		for k, v := range rule {
			copied[k] = v
		}
		if n, ok := managed.Integer(rule["priority"]); ok {
			copied["priority"] = n
		}
		out = append(out, copied)
	}
	return out
}

// header is the first line of the policy file. TOML has no sidecar
// convention, so the revision rides in a comment, where `aw-sync status`
// and a curious operator can both read it.
func header(bundle *policy.Bundle) string {
	version := "unversioned"
	if bundle != nil && bundle.Version != "" {
		version = bundle.Version
	}
	return "# Managed by aw-sync from policy revision " + version + ". Do not edit: the next sync overwrites this file.\n\n"
}

// dropped names the Gemini rules Compile("") left out because they are
// scoped to repositories. Rules for other agents are not this renderer's to
// report.
func dropped(bundle *policy.Bundle) []string {
	if bundle == nil {
		return nil
	}
	var names []string
	for _, rule := range bundle.Rules {
		if _, forGemini := rule.Agents[Name]; forGemini && len(rule.Match.Repos) > 0 {
			names = append(names, rule.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("gemini: %d repo-scoped rule(s) not enforceable in settings.json or policies: %s",
		len(names), strings.Join(names, ", "))}
}

// DecodeTOMLForTest parses a rendered policy file back into a map. It exists
// so the package's tests can assert a round trip without importing the
// encoder themselves.
func DecodeTOMLForTest(content []byte, into *map[string]any) error {
	return toml.Unmarshal(content, into)
}
