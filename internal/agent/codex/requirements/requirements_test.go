package requirements_test

import (
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/codex/requirements"
)

func TestKnownKeysValidate(t *testing.T) {
	managed := map[string]any{
		"allowed_approval_policies": []any{"on-request", "untrusted"},
		"allowed_sandbox_modes":     []any{"read-only", "workspace-write"},
		"default_permissions":       ":workspace",
		"allow_appshots":            false,
		"mcp_servers":               map[string]any{"docs": map[string]any{"identity": map[string]any{"command": "codex-mcp"}}},
		"rules": map[string]any{"prefix_rules": []any{
			map[string]any{"pattern": []any{map[string]any{"token": "rm"}}, "decision": "forbidden"},
		}},
	}

	if err := requirements.Validate(managed); err != nil {
		t.Fatalf("Validate = %v, want nil for documented keys", err)
	}
}

func TestAnUnknownTopLevelKeyIsRejected(t *testing.T) {
	err := requirements.Validate(map[string]any{"allowed_sandbox_modes": []any{"read-only"}, "sandbox_mode": "read-only"})

	if err == nil || !strings.Contains(err.Error(), `"sandbox_mode"`) || !strings.Contains(err.Error(), requirements.KeysAsOf) {
		t.Errorf("err = %v; want the unknown key named and the allowlist date, so a reader knows which reference to check", err)
	}
}

func TestUnknownKeysAreReportedSorted(t *testing.T) {
	err := requirements.Validate(map[string]any{"zeta": 1, "alpha": 2})

	if err == nil || strings.Index(err.Error(), `"alpha"`) > strings.Index(err.Error(), `"zeta"`) {
		t.Errorf("err = %v; want unknown keys in sorted order so the message is stable", err)
	}
}

func TestAValueTOMLCannotEncodeIsRejected(t *testing.T) {
	// TOML has no null: a JSON null in the policy would be dropped or
	// mis-encoded, and the author should hear about it at apply time.
	err := requirements.Validate(map[string]any{"default_permissions": nil})

	if err == nil || !strings.Contains(err.Error(), "default_permissions") {
		t.Errorf("err = %v; want an error naming the nil key", err)
	}
}

func TestANestedNullIsRejectedWithItsPath(t *testing.T) {
	err := requirements.Validate(map[string]any{"mcp_servers": map[string]any{"docs": map[string]any{"x": nil, "y": "ok"}}})

	if err == nil || !strings.Contains(err.Error(), "mcp_servers.docs.x") {
		t.Errorf("err = %v; want an error naming the nested nil path", err)
	}
}

func TestANullInsideAListIsRejected(t *testing.T) {
	err := requirements.Validate(map[string]any{"allowed_sandbox_modes": []any{"read-only", nil}})

	if err == nil || !strings.Contains(err.Error(), "allowed_sandbox_modes[1]") {
		t.Errorf("err = %v; want an error naming the null slice element", err)
	}
}

func TestEmptyAndNilDocumentsAreValid(t *testing.T) {
	if err := requirements.Validate(map[string]any{}); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := requirements.Validate(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
}

func TestForAgentIgnoresOtherAgents(t *testing.T) {
	bad := map[string]any{"not_a_key": true}

	if err := requirements.ForAgent("claude", bad); err != nil {
		t.Errorf("claude: %v; the Codex validator must not judge another agent's document", err)
	}
	if err := requirements.ForAgent("codex", bad); err == nil {
		t.Error("codex: nil; want the unknown key rejected")
	}
	if err := requirements.ForAgent("codex", nil); err != nil {
		t.Errorf("codex nil: %v", err)
	}
}

func TestTheAllowlistIsSortedAndDated(t *testing.T) {
	for i := 1; i < len(requirements.Keys); i++ {
		if requirements.Keys[i-1] >= requirements.Keys[i] {
			t.Errorf("Keys not sorted/unique at %d: %q >= %q", i, requirements.Keys[i-1], requirements.Keys[i])
		}
	}
	if len(requirements.KeysAsOf) != len("2026-09-22") {
		t.Errorf("KeysAsOf = %q, want an ISO date", requirements.KeysAsOf)
	}
}
