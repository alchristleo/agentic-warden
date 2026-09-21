// Package sync keeps a machine's agent configuration files in step with the
// control plane. It is the one network client on a machine: it fetches the
// enrolled user's bundle with the machine credential, renders every agent's
// files, and writes them all or none, so the machine never carries a policy
// that is half applied.
package sync

import (
	"fmt"
	"os"
)

// StateDir is where aw-sync keeps machine.json and state.json on goos. It is
// root-owned and readable by everyone, because `aw doctor` runs as the
// developer and reports from it; only machine.json is private.
func StateDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/agent-wrapper"
	case "windows":
		return programData() + `\agent-wrapper`
	default:
		return "/var/lib/agent-wrapper"
	}
}

// agentRoots is the one table of where each agent reads its enforced
// configuration, in the order linux, darwin, windows. A Windows entry that
// starts with %ProgramData% is resolved at call time.
var agentRoots = map[string][3]string{
	"claude": {"/etc/claude-code", "/Library/Application Support/ClaudeCode", `C:\Program Files\ClaudeCode`},
	"codex":  {"/etc/codex", "/etc/codex", `%ProgramData%\OpenAI\Codex`},
	"gemini": {"/etc/gemini-cli", "/Library/Application Support/GeminiCli", `%ProgramData%\gemini-cli`},
}

// AgentRoot is the directory agentName's rendered files live under on goos.
// An agent without a row cannot be governed by files, and saying so is
// better than writing them somewhere nothing reads.
func AgentRoot(goos, agentName string) (string, error) {
	roots, ok := agentRoots[agentName]
	if !ok {
		return "", fmt.Errorf("sync: no system directory is known for agent %q", agentName)
	}
	var root string
	switch goos {
	case "darwin":
		root = roots[1]
	case "windows":
		root = roots[2]
	default:
		root = roots[0]
	}
	if len(root) > 13 && root[:13] == "%ProgramData%" {
		root = programData() + root[13:]
	}
	return root, nil
}

// programData is Windows' machine-wide data directory, which an installation
// can relocate; the environment says where.
func programData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}
