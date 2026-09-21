package sync_test

import (
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestStateDirPerOS(t *testing.T) {
	cases := map[string]string{
		"linux":   "/var/lib/agent-wrapper",
		"darwin":  "/Library/Application Support/agent-wrapper",
		"windows": `C:\ProgramData\agent-wrapper`,
	}
	t.Setenv("ProgramData", "")
	for goos, want := range cases {
		if got := sync.StateDir(goos); got != want {
			t.Errorf("StateDir(%s) = %q, want %q", goos, got, want)
		}
	}
}

func TestAgentRootTable(t *testing.T) {
	t.Setenv("ProgramData", "")
	cases := []struct{ goos, agent, want string }{
		{"linux", "claude", "/etc/claude-code"},
		{"darwin", "claude", "/Library/Application Support/ClaudeCode"},
		{"windows", "claude", `C:\Program Files\ClaudeCode`},
		{"linux", "codex", "/etc/codex"},
		{"darwin", "codex", "/etc/codex"},
		{"windows", "codex", `C:\ProgramData\OpenAI\Codex`},
		{"linux", "gemini", "/etc/gemini-cli"},
		{"darwin", "gemini", "/Library/Application Support/GeminiCli"},
		{"windows", "gemini", `C:\ProgramData\gemini-cli`},
	}
	for _, tc := range cases {
		got, err := sync.AgentRoot(tc.goos, tc.agent)
		if err != nil {
			t.Errorf("AgentRoot(%s, %s): %v", tc.goos, tc.agent, err)
			continue
		}
		if got != tc.want {
			t.Errorf("AgentRoot(%s, %s) = %q, want %q", tc.goos, tc.agent, got, tc.want)
		}
	}
}

func TestAgentRootHonoursProgramData(t *testing.T) {
	t.Setenv("ProgramData", `D:\PD`)
	got, err := sync.AgentRoot("windows", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if got != `D:\PD\OpenAI\Codex` {
		t.Errorf("got %q", got)
	}
}

func TestAgentRootRejectsAnUnknownAgent(t *testing.T) {
	_, err := sync.AgentRoot("linux", "copilot")
	if err == nil || !strings.Contains(err.Error(), "copilot") {
		t.Errorf("err = %v, want one naming the agent", err)
	}
}
