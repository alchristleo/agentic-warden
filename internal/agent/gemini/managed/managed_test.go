package managed_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
)

func TestADocumentWithKnownSettingsAndAFullRuleValidates(t *testing.T) {
	doc := map[string]any{
		"settings": map[string]any{
			"tools":      map[string]any{"exclude": []any{"run_shell_command"}},
			"mcp":        map[string]any{"allowed": []any{"docs"}},
			"mcpServers": map[string]any{"docs": map[string]any{"command": "gemini-mcp"}},
			"admin":      map[string]any{"secureModeEnabled": true},
		},
		"policies": []any{
			map[string]any{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": float64(100)},
			map[string]any{"toolName": []any{"write_file", "replace"}, "decision": "ask_user", "priority": 50},
		},
	}

	if err := managed.Validate(doc); err != nil {
		t.Fatalf("Validate = %v, want nil for documented keys and complete rules", err)
	}
}

func TestPartsSplitsTheDocument(t *testing.T) {
	settings, policies, err := managed.Parts(map[string]any{
		"settings": map[string]any{"general": map[string]any{}},
		"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := settings["general"]; !ok || len(policies) != 1 {
		t.Errorf("settings = %v, policies = %v", settings, policies)
	}
}

func TestAnUnknownTopLevelKeyIsRejected(t *testing.T) {
	err := managed.Validate(map[string]any{"settings": map[string]any{}, "rules": []any{}})

	if err == nil || !strings.Contains(err.Error(), `"rules"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestSettingsMustBeAnObjectAndPoliciesAList(t *testing.T) {
	if err := managed.Validate(map[string]any{"settings": []any{}}); err == nil || !strings.Contains(err.Error(), "settings") {
		t.Errorf("settings as a list: err = %v", err)
	}
	if err := managed.Validate(map[string]any{"policies": map[string]any{}}); err == nil || !strings.Contains(err.Error(), "policies") {
		t.Errorf("policies as an object: err = %v", err)
	}
}

func TestAnUnknownSettingsKeyIsRejectedWithTheDate(t *testing.T) {
	err := managed.Validate(map[string]any{"settings": map[string]any{"tools": map[string]any{}, "toolz": map[string]any{}}})

	if err == nil || !strings.Contains(err.Error(), `"toolz"`) || !strings.Contains(err.Error(), managed.KeysAsOf) {
		t.Errorf("err = %v; want the unknown key named and the allowlist date", err)
	}
}

func TestUnknownSettingsKeysAreReportedSorted(t *testing.T) {
	err := managed.Validate(map[string]any{"settings": map[string]any{"zeta": 1, "alpha": 2}})

	if err == nil || strings.Index(err.Error(), `"alpha"`) > strings.Index(err.Error(), `"zeta"`) {
		t.Errorf("err = %v; want unknown keys in sorted order so the message is stable", err)
	}
}

func TestAPolicyRuleMustBeAnObject(t *testing.T) {
	err := managed.Validate(map[string]any{"policies": []any{"deny everything"}})

	if err == nil || !strings.Contains(err.Error(), "policies[0]") {
		t.Errorf("err = %v; want the offending entry named by index", err)
	}
}

func TestAPolicyRuleNeedsAToolName(t *testing.T) {
	for name, rule := range map[string]map[string]any{
		"missing":       {"decision": "deny", "priority": 1},
		"empty string":  {"toolName": "", "decision": "deny", "priority": 1},
		"empty list":    {"toolName": []any{}, "decision": "deny", "priority": 1},
		"list with int": {"toolName": []any{"a", 3}, "decision": "deny", "priority": 1},
	} {
		err := managed.Validate(map[string]any{"policies": []any{rule}})
		if err == nil || !strings.Contains(err.Error(), "toolName") {
			t.Errorf("%s: err = %v; want toolName named", name, err)
		}
	}
}

func TestAPolicyRuleNeedsAKnownDecision(t *testing.T) {
	for name, rule := range map[string]map[string]any{
		"missing":    {"toolName": "*", "priority": 1},
		"unknown":    {"toolName": "*", "decision": "forbid", "priority": 1},
		"wrong type": {"toolName": "*", "decision": true, "priority": 1},
	} {
		err := managed.Validate(map[string]any{"policies": []any{rule}})
		if err == nil || !strings.Contains(err.Error(), "decision") || !strings.Contains(err.Error(), "ask_user") {
			t.Errorf("%s: err = %v; want decision named with the valid values", name, err)
		}
	}
}

func TestAPolicyRuleNeedsAnIntegerPriorityInRange(t *testing.T) {
	// Gemini's loader rejects the whole file when priority is missing, a
	// fraction, or outside 0..999; the validator must catch it at apply time.
	for name, rule := range map[string]map[string]any{
		"missing":   {"toolName": "*", "decision": "deny"},
		"fraction":  {"toolName": "*", "decision": "deny", "priority": 1.5},
		"negative":  {"toolName": "*", "decision": "deny", "priority": -1},
		"too large": {"toolName": "*", "decision": "deny", "priority": 1000},
		"string":    {"toolName": "*", "decision": "deny", "priority": "10"},
	} {
		err := managed.Validate(map[string]any{"policies": []any{rule}})
		if err == nil || !strings.Contains(err.Error(), "priority") {
			t.Errorf("%s: err = %v; want priority named", name, err)
		}
	}
	for _, p := range []any{0, 999, float64(100), int64(5)} {
		if err := managed.Validate(map[string]any{"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": p}}}); err != nil {
			t.Errorf("priority %v (%T): err = %v; want nil", p, p, err)
		}
	}
}

func TestANullInsideAPolicyRuleIsRejectedWithItsPath(t *testing.T) {
	err := managed.Validate(map[string]any{"policies": []any{
		map[string]any{"toolName": "*", "decision": "deny", "priority": 1, "modes": []any{"default", nil}},
	}})

	if err == nil || !strings.Contains(err.Error(), "policies[0].modes[1]") {
		t.Errorf("err = %v; want the null's path, since TOML cannot carry it", err)
	}
}

func TestIntegerAcceptsWholeNumbersOnly(t *testing.T) {
	for v, want := range map[any]int64{5: 5, int64(7): 7, float64(9): 9} {
		if got, ok := managed.Integer(v); !ok || got != want {
			t.Errorf("Integer(%v) = %d, %v; want %d, true", v, got, ok, want)
		}
	}
	for _, v := range []any{1.5, "3", nil, true} {
		if _, ok := managed.Integer(v); ok {
			t.Errorf("Integer(%v) accepted a value that is not a whole number", v)
		}
	}
}

func TestEmptyAndNilDocumentsAreValid(t *testing.T) {
	if err := managed.Validate(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
	if err := managed.Validate(map[string]any{}); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := managed.Validate(map[string]any{"settings": map[string]any{}, "policies": []any{}}); err != nil {
		t.Errorf("empty halves: %v", err)
	}
}

func TestForAgentIgnoresOtherAgents(t *testing.T) {
	bad := map[string]any{"model": "opus"}
	if err := managed.ForAgent("claude", bad); err != nil {
		t.Errorf("claude: %v; want nil, this validator knows Gemini alone", err)
	}
	if err := managed.ForAgent("codex", bad); err != nil {
		t.Errorf("codex: %v; want nil", err)
	}
	if err := managed.ForAgent("gemini", bad); err == nil {
		t.Error("gemini: nil; want the unknown top-level key rejected")
	}
	if err := managed.ForAgent("gemini", nil); err != nil {
		t.Errorf("gemini with no managed settings: %v; want nil", err)
	}
}

func TestTheAllowlistIsSortedAndDated(t *testing.T) {
	if !sort.StringsAreSorted(managed.SettingsKeys) {
		t.Error("SettingsKeys must be sorted; lookup binary-searches it")
	}
	if !sort.StringsAreSorted(managed.Decisions) {
		t.Error("Decisions must be sorted")
	}
	if len(managed.KeysAsOf) != len("2026-09-22") {
		t.Errorf("KeysAsOf = %q; want a YYYY-MM-DD date", managed.KeysAsOf)
	}
	for _, key := range []string{"admin", "mcpServers", "policyPaths", "tools", "useWriteTodos"} {
		i := sort.SearchStrings(managed.SettingsKeys, key)
		if i == len(managed.SettingsKeys) || managed.SettingsKeys[i] != key {
			t.Errorf("SettingsKeys lacks %q", key)
		}
	}
}
