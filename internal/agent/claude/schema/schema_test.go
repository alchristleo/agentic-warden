package schema_test

import (
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
)

func TestValidateAcceptsWellFormedSettings(t *testing.T) {
	settings := map[string]any{
		"permissions": map[string]any{
			"deny": []any{"Read(./.env)"},
		},
		"allowManagedPermissionRulesOnly": true,
		"model":                           "opus",
	}
	if err := schema.Validate(settings); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateAcceptsEmptySettings(t *testing.T) {
	if err := schema.Validate(map[string]any{}); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsWrongType(t *testing.T) {
	// A permission list that is a string, not an array, is the kind of
	// mistake Claude Code cannot repair; emitted by a helper it refuses to
	// start.
	settings := map[string]any{
		"permissions": map[string]any{"deny": "Read(./.env)"},
	}
	err := schema.Validate(settings)
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	if !strings.Contains(err.Error(), "permissions") {
		t.Errorf("error %q should name the offending field", err)
	}
}

func TestValidateRejectsBadEnum(t *testing.T) {
	settings := map[string]any{
		"permissions": map[string]any{"defaultMode": "yolo"},
	}
	if err := schema.Validate(settings); err == nil {
		t.Fatal("Validate() = nil, want an error for an unknown permission mode")
	}
}

func TestValidateToleratesUnknownTopLevelKeys(t *testing.T) {
	// The published schema lags the CLI. A key the schema has not learned
	// yet must not brick a fleet, and the schema itself allows additional
	// properties at the top level; this pins that assumption.
	settings := map[string]any{"someFutureKey": true}
	if err := schema.Validate(settings); err != nil {
		t.Fatalf("Validate() = %v, want nil for an unknown top-level key", err)
	}
}

func TestValidateRejectsNonObject(t *testing.T) {
	if err := schema.Validate([]any{"not", "an", "object"}); err == nil {
		t.Fatal("Validate() = nil, want an error for a non-object document")
	}
}

func TestValidateErrorIsCompact(t *testing.T) {
	// The error reaches an administrator's terminal and a helper's audit
	// log. It should name each violation by path, and it should not leak the
	// build machine's filesystem layout.
	settings := map[string]any{
		"permissions": map[string]any{"deny": "x", "defaultMode": "yolo"},
	}
	err := schema.Validate(settings)
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	msg := err.Error()
	for _, want := range []string{"/permissions/deny", "/permissions/defaultMode"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %s", msg, want)
		}
	}
	if strings.Contains(msg, "file://") {
		t.Errorf("error %q leaks a local path", msg)
	}
}

func TestForAgentIgnoresOtherAgents(t *testing.T) {
	broken := map[string]any{"permissions": "not for claude"}
	if err := schema.ForAgent("codex", broken); err != nil {
		t.Errorf("ForAgent(codex) = %v, want nil: another agent's schema is not ours to judge", err)
	}
	if err := schema.ForAgent("claude", broken); err == nil {
		t.Error("ForAgent(claude) = nil, want an error")
	}
	if err := schema.ForAgent("claude", nil); err != nil {
		t.Errorf("ForAgent(claude, nil) = %v, want nil: a rule with only env is valid", err)
	}
}
