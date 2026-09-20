package merge_test

import (
	"reflect"
	"testing"

	"github.com/acme/agent-wrapper/internal/merge"
)

func TestJSONOverlayWinsPerKeyAndRecursesIntoObjects(t *testing.T) {
	base := map[string]any{
		"model": "sonnet",
		"permissions": map[string]any{
			"defaultMode": "ask",
			"allow":       []any{"Bash(git status)"},
		},
	}
	overlay := map[string]any{
		"model": "opus",
		"permissions": map[string]any{
			"defaultMode": "acceptEdits",
		},
	}

	got := merge.JSON(base, overlay, merge.Rules{})

	want := map[string]any{
		"model": "opus",
		"permissions": map[string]any{
			"defaultMode": "acceptEdits",
			"allow":       []any{"Bash(git status)"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON() = %#v, want %#v", got, want)
	}
}

func TestJSONUnionsArraysAtListedPathsBaseFirstWithoutDuplicates(t *testing.T) {
	rules := merge.Rules{UnionArrays: []string{"permissions.allow"}}
	base := map[string]any{
		"permissions": map[string]any{"allow": []any{"Bash(git status)", "WebSearch"}},
	}
	overlay := map[string]any{
		"permissions": map[string]any{"allow": []any{"WebSearch", "Bash(npm run *)"}},
	}

	got := merge.JSON(base, overlay, rules)

	want := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash(git status)", "WebSearch", "Bash(npm run *)"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON() = %#v, want %#v", got, want)
	}
}

func TestJSONReplacesArraysAtPathsNotListedForUnion(t *testing.T) {
	rules := merge.Rules{UnionArrays: []string{"permissions.allow"}}
	base := map[string]any{"additionalDirectories": []any{"/srv/a"}}
	overlay := map[string]any{"additionalDirectories": []any{"/srv/b"}}

	got := merge.JSON(base, overlay, rules)

	want := map[string]any{"additionalDirectories": []any{"/srv/b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON() = %#v, want %#v", got, want)
	}
}

func TestJSONOverlayNullNeverErasesBaseValue(t *testing.T) {
	base := map[string]any{"model": "sonnet"}
	overlay := map[string]any{"model": nil}

	got := merge.JSON(base, overlay, merge.Rules{})

	want := map[string]any{"model": "sonnet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON() = %#v, want %#v", got, want)
	}
}
