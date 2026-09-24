// Package sync keeps a machine's agent configuration files in step with the
// control plane. It is the one network client on a machine: it fetches the
// enrolled user's bundle with the machine credential, renders every agent's
// files, and writes them all or none, so the machine never carries a policy
// that is half applied.
package sync

import (
	"fmt"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/agent/codex"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
)

// StateDir is where aw-sync keeps machine.json and state.json on goos. It is
// root-owned and readable by everyone, because `aw doctor` runs as the
// developer and reports from it; only machine.json is private.
func StateDir(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/agent-wrapper"
	case "windows":
		return agent.ProgramData() + `\agent-wrapper`
	default:
		return "/var/lib/agent-wrapper"
	}
}

// AgentRoot is the directory agentName's rendered files live under on goos.
// Each adapter owns its row, so the place aw-sync writes and the place the
// adapter's Inspect reads cannot drift apart. An agent without a row cannot
// be governed by files, and saying so is better than writing them somewhere
// nothing reads.
func AgentRoot(goos, agentName string) (string, error) {
	switch agentName {
	case claude.Name:
		return claude.SystemDir(goos), nil
	case codex.Name:
		return codex.SystemDir(goos), nil
	case gemini.Name:
		return gemini.SystemDir(goos), nil
	}
	return "", fmt.Errorf("sync: no system directory is known for agent %q", agentName)
}
