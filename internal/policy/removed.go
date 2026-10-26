package policy

// RemovedAgents maps an agent key agent-wrapper no longer supports to the hint shown to the user.
var RemovedAgents = map[string]string{
	"gemini": "Gemini CLI is no longer supported by agent-wrapper; use agy (Antigravity CLI)",
}
