package merge_test

import (
	"reflect"
	"testing"

	"github.com/acme/agent-wrapper/internal/merge"
)

func TestEnvSoftDefaultKeepsTheValueAlreadyInTheEnvironment(t *testing.T) {
	base := []string{"ANTHROPIC_BASE_URL=https://personal.example"}
	defaults := map[string]string{"ANTHROPIC_BASE_URL": "https://gateway.acme.com"}

	out, notes := merge.Env(base, defaults, false)

	if want := []string{"ANTHROPIC_BASE_URL=https://personal.example"}; !reflect.DeepEqual(out, want) {
		t.Errorf("out = %q, want %q", out, want)
	}
	want := []string{`env ANTHROPIC_BASE_URL kept from environment (org default "https://gateway.acme.com" ignored)`}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}
}

func TestEnvPolicyForcesOverAValueAlreadyInTheEnvironment(t *testing.T) {
	base := []string{"PATH=/usr/bin", "ANTHROPIC_BASE_URL=https://personal.example"}
	defaults := map[string]string{"ANTHROPIC_BASE_URL": "https://gateway.acme.com"}

	out, notes := merge.Env(base, defaults, true)

	want := []string{"PATH=/usr/bin", "ANTHROPIC_BASE_URL=https://gateway.acme.com"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("out = %q, want %q", out, want)
	}
	wantNotes := []string{`env ANTHROPIC_BASE_URL forced to "https://gateway.acme.com" (policy)`}
	if !reflect.DeepEqual(notes, wantNotes) {
		t.Errorf("notes = %q, want %q", notes, wantNotes)
	}
}

func TestEnvInjectsVariablesTheEnvironmentDoesNotDefine(t *testing.T) {
	out, notes := merge.Env([]string{"PATH=/usr/bin"}, map[string]string{"CLAUDE_CODE_ENABLE_TELEMETRY": "1"}, false)

	want := []string{"PATH=/usr/bin", "CLAUDE_CODE_ENABLE_TELEMETRY=1"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("out = %q, want %q", out, want)
	}
	wantNotes := []string{`env CLAUDE_CODE_ENABLE_TELEMETRY="1" injected`}
	if !reflect.DeepEqual(notes, wantNotes) {
		t.Errorf("notes = %q, want %q", notes, wantNotes)
	}
}

func TestEnvNotesAreOrderedByVariableNameSoLaunchesAreReproducible(t *testing.T) {
	defaults := map[string]string{"ZULU": "z", "ALPHA": "a", "MIKE": "m"}

	_, notes := merge.Env(nil, defaults, false)

	want := []string{
		`env ALPHA="a" injected`,
		`env MIKE="m" injected`,
		`env ZULU="z" injected`,
	}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}
}

func TestEnvDoesNotMutateTheCallersEnvironmentSlice(t *testing.T) {
	base := []string{"PATH=/usr/bin", "ANTHROPIC_BASE_URL=https://personal.example"}

	merge.Env(base, map[string]string{"ANTHROPIC_BASE_URL": "https://gateway.acme.com"}, true)

	if base[1] != "ANTHROPIC_BASE_URL=https://personal.example" {
		t.Errorf("caller's slice was mutated: base[1] = %q", base[1])
	}
}
