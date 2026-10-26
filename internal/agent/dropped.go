package agent

import (
	"fmt"
	"strings"

	"github.com/alchristleo/agentic-warden/internal/policy"
)

// DroppedRepoRules names the rules for agentName that Compile("") left out
// because they are scoped to repositories. A static file cannot express those,
// so the renderer for file reports them in a note. It returns nil when none
// were dropped.
func DroppedRepoRules(bundle *policy.Bundle, agentName, file string) []string {
	if bundle == nil {
		return nil
	}
	var names []string
	for _, rule := range bundle.Rules {
		if _, forAgent := rule.Agents[agentName]; forAgent && len(rule.Match.Repos) > 0 {
			names = append(names, rule.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s: %d repo-scoped rule(s) not enforceable in %s: %s",
		agentName, len(names), file, strings.Join(names, ", "))}
}
