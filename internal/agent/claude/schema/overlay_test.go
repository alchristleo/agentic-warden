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

func TestOverlayEntriesAllApply(t *testing.T) {
	if _, err := load(); err != nil {
		t.Fatalf("load() = %v, want nil", err)
	}
}
