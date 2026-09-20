// Package merge implements the semantic merge used to combine organization
// defaults, user configuration and locked policy into one settings document.
//
// The rules are deliberately the ones Claude Code itself documents for its
// settings stack, so a merged document behaves the way a developer expects
// after reading the agent's own docs:
//
//   - Objects merge recursively, key by key.
//   - Any other value: the overlay wins outright.
//   - Arrays at paths listed in Rules.UnionArrays concatenate base-first with
//     duplicates removed, so an org allowlist and a personal allowlist coexist.
//   - A nil overlay value never erases a base value.
package merge

import (
	"encoding/json"
	"fmt"
)

// Rules controls semantic merging of settings documents.
type Rules struct {
	// UnionArrays lists dot-separated paths (e.g. "permissions.allow") whose
	// array values are concatenated and de-duplicated instead of replaced.
	UnionArrays []string
}

// JSON deep-merges overlay on top of base and returns the result. Values must
// be generic JSON: map[string]any, []any, string, float64, bool or nil.
func JSON(base, overlay any, rules Rules) any {
	union := make(map[string]bool, len(rules.UnionArrays))
	for _, path := range rules.UnionArrays {
		union[path] = true
	}
	return mergeValue(base, overlay, "", union)
}

func mergeValue(base, overlay any, path string, union map[string]bool) any {
	if overlay == nil {
		return base
	}
	if base == nil {
		return overlay
	}
	baseObj, baseIsObj := base.(map[string]any)
	overlayObj, overlayIsObj := overlay.(map[string]any)
	if baseIsObj && overlayIsObj {
		out := make(map[string]any, len(baseObj)+len(overlayObj))
		for k, v := range baseObj {
			out[k] = v
		}
		for k, v := range overlayObj {
			if prev, ok := out[k]; ok {
				out[k] = mergeValue(prev, v, childPath(path, k), union)
				continue
			}
			out[k] = v
		}
		return out
	}
	if union[path] {
		baseArr, baseIsArr := base.([]any)
		overlayArr, overlayIsArr := overlay.([]any)
		if baseIsArr && overlayIsArr {
			return unionArrays(baseArr, overlayArr)
		}
	}
	return overlay
}

// unionArrays concatenates base then overlay, dropping repeats. Order is
// stable so a merged document is byte-identical across runs.
func unionArrays(base, overlay []any) []any {
	out := make([]any, 0, len(base)+len(overlay))
	seen := make(map[string]bool, len(base)+len(overlay))
	for _, arr := range [][]any{base, overlay} {
		for _, v := range arr {
			key := canonKey(v)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, v)
		}
	}
	return out
}

// canonKey identifies a value for de-duplication. Encoding to JSON keeps
// structured entries (an object permission rule, say) comparable.
func canonKey(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func childPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
