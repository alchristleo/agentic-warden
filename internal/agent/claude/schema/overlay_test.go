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
	err := apply(doc, []Extra{{Path: "sandbox.enabled", Schema: map[string]any{"type": "boolean"}}})
	if err == nil || !strings.Contains(err.Error(), "sandbox.enabled") {
		t.Fatalf("apply() = %v, want an error naming sandbox.enabled", err)
	}
}

func TestApplyRejectsUnresolvableParent(t *testing.T) {
	doc := map[string]any{"properties": map[string]any{"sandbox": map[string]any{"type": "object"}}}
	for _, path := range []string{"nosuch.key", "sandbox.key"} {
		err := apply(doc, []Extra{{Path: path, Schema: map[string]any{"type": "boolean"}}})
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
	if err := apply(doc, []Extra{{Path: "b.k", Schema: map[string]any{"type": "string"}}}); err != nil {
		t.Fatalf("apply() = %v, want nil", err)
	}
	props := doc["$defs"].(map[string]any)["box"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["k"]; !ok {
		t.Fatal("k was not added to the referenced definition")
	}
}

func TestOverlayEntriesValidate(t *testing.T) {
	hook := []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": "true"}}}}
	creds := func(k string, v any) map[string]any {
		return map[string]any{"sandbox": map[string]any{"credentials": map[string]any{k: v}}}
	}
	pair := func(extra string) []any {
		return []any{map[string]any{"accessKeyIdVar": "A", "secretAccessKeyVar": "B", extra: "C"}}
	}
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
		{"sigv4 ok", creds("sigv4", map[string]any{"streaming": "passthrough"}), false},
		{"sigv4 bad enum", creds("sigv4", map[string]any{"streaming": "allow"}), true},
		{"sigv4 typo", creds("sigv4", map[string]any{"sigv4A": "deny"}), true},
		{"awsPairs ok", creds("awsPairs", pair("sessionTokenVar")), false},
		{"awsPairs missing required", creds("awsPairs", []any{map[string]any{"accessKeyIdVar": "A"}}), true},
		{"awsPairs typo", creds("awsPairs", pair("sessionTokenVr")), true},
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
