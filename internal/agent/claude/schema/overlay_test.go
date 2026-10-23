package schema

import (
	"strings"
	"testing"
)

func TestOverlayAcceptsNestedUserNamespaces(t *testing.T) {
	settings := map[string]any{"sandbox": map[string]any{"allowNestedUserNamespaces": true}}
	if err := Validate(settings); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestOverlayRejectsWrongValueType(t *testing.T) {
	settings := map[string]any{"sandbox": map[string]any{"allowNestedUserNamespaces": "yes"}}
	err := Validate(settings)
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	if !strings.Contains(err.Error(), "/sandbox/allowNestedUserNamespaces") {
		t.Fatalf("Validate() = %v, want it to name /sandbox/allowNestedUserNamespaces", err)
	}
}

func TestStrictBlockStillRejectsTypo(t *testing.T) {
	settings := map[string]any{"permissions": map[string]any{"denny": []any{"Read(./.env)"}}}
	err := Validate(settings)
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	if !strings.Contains(err.Error(), "denny") {
		t.Fatalf("Validate() = %v, want it to name denny", err)
	}
}

func TestApplyRejectsExistingKey(t *testing.T) {
	doc := map[string]any{
		"properties": map[string]any{
			"sandbox": map[string]any{
				"properties": map[string]any{"enabled": map[string]any{"type": "boolean"}},
			},
		},
	}
	err := apply(doc, []extra{{Path: "sandbox.enabled", Schema: map[string]any{"type": "boolean"}}})
	if err == nil || !strings.Contains(err.Error(), "sandbox.enabled") {
		t.Fatalf("apply() = %v, want an error naming sandbox.enabled", err)
	}
}

func TestApplyRejectsUnresolvableParent(t *testing.T) {
	doc := map[string]any{"properties": map[string]any{"sandbox": map[string]any{"type": "object"}}}
	for _, path := range []string{"nosuch.key", "sandbox.key"} {
		err := apply(doc, []extra{{Path: path, Schema: map[string]any{"type": "boolean"}}})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("apply(%s) = %v, want an error naming the path", path, err)
		}
	}
}

func TestApplyFollowsLocalRef(t *testing.T) {
	doc := map[string]any{
		"$defs":      map[string]any{"box": map[string]any{"properties": map[string]any{}}},
		"properties": map[string]any{"b": map[string]any{"$ref": "#/$defs/box"}},
	}
	if err := apply(doc, []extra{{Path: "b.k", Schema: map[string]any{"type": "string"}}}); err != nil {
		t.Fatalf("apply() = %v, want nil", err)
	}
	props := doc["$defs"].(map[string]any)["box"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["k"]; !ok {
		t.Fatal("k was not added to the referenced definition")
	}
}

func TestOverlayEntriesValidate(t *testing.T) {
	hook := []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": "true"}}}}
	cases := []struct {
		name     string
		settings map[string]any
		wantErr  bool
	}{
		{"hook event ok", map[string]any{"hooks": map[string]any{"PreModelSwitch": hook}}, false},
		{"hook event wrong type", map[string]any{"hooks": map[string]any{"PostModelSwitch": "x"}}, true},
		{"blockReads ok", map[string]any{"permissions": map[string]any{"blockReadsOutsideWorkingDirectories": true}}, false},
		{"blockReads wrong type", map[string]any{"permissions": map[string]any{"blockReadsOutsideWorkingDirectories": "yes"}}, true},
		{"location ok", map[string]any{"worktree": map[string]any{"location": "~/worktrees"}}, false},
		{"location wrong type", map[string]any{"worktree": map[string]any{"location": true}}, true},
	}
	for _, c := range cases {
		err := Validate(c.settings)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: Validate() = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestOverlayEntriesAllApply(t *testing.T) {
	if _, err := load(); err != nil {
		t.Fatalf("load() = %v, want nil", err)
	}
}

func TestDefines(t *testing.T) {
	cases := map[string]bool{
		"sandbox.enabled":                   true,
		"sandbox.allowNestedUserNamespaces": true,
		"hooks.PreModelSwitch":              true,
		"sandbox.credentials.files":         true,
		"sandbox.credentials.awsPairs":      false,
		"sandbox.nosuch":                    false,
		"nosuch.key":                        false,
		"permissions":                       true,
	}
	for path, want := range cases {
		if got := Defines(path); got != want {
			t.Errorf("Defines(%q) = %v, want %v", path, got, want)
		}
	}
}
