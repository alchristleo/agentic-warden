package policy

// removedAgents maps an agent key agent-wrapper no longer supports to the hint shown to the user.
var removedAgents = map[string]string{
	"gemini": "Gemini CLI is no longer supported by agent-wrapper; use agy (Antigravity CLI): aw agy",
}

// RemovedDocs is where the docs explain what to do about a removed agent.
const RemovedDocs = "docs/site/reference.md#gemini-cli-removed"

// RemovedHint returns the migration hint for an agent key agent-wrapper no longer supports.
func RemovedHint(name string) (string, bool) {
	hint, ok := removedAgents[name]
	return hint, ok
}
