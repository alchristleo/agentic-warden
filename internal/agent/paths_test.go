package agent_test

import (
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent"
)

func TestProgramDataUsesTheEnvironmentWhenSet(t *testing.T) {
	t.Setenv("ProgramData", `D:\Data`)

	if got, want := agent.ProgramData(), `D:\Data`; got != want {
		t.Errorf("ProgramData() = %q, want %q", got, want)
	}
}

func TestProgramDataFallsBackWhenUnset(t *testing.T) {
	t.Setenv("ProgramData", "")

	if got, want := agent.ProgramData(), `C:\ProgramData`; got != want {
		t.Errorf("ProgramData() = %q, want %q", got, want)
	}
}
