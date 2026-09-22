package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// overrides turns the launch document into the key=value strings Codex's
// -c flag takes, one per leaf, sorted so two launches from the same policy
// produce the same command line. Nested tables become dotted keys, which
// is how Codex addresses them; a table inside an array cannot be dotted
// and is written inline instead. Values are TOML: Codex parses the value
// as TOML and falls back to a raw string, so strings are quoted to keep
// "1.0" a string and "true" a string.
func overrides(launch map[string]any) ([]string, error) {
	leaves := make(map[string]any)
	flatten("", launch, leaves)
	keys := make([]string, 0, len(leaves))
	for k := range leaves {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		value, err := tomlValue(leaves[k])
		if err != nil {
			return nil, fmt.Errorf("codex: launch %s: %w", k, err)
		}
		out = append(out, k+"="+value)
	}
	return out, nil
}

// flatten walks tables into dotted keys and stops at anything else.
func flatten(prefix string, table map[string]any, leaves map[string]any) {
	for k, v := range table {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			flatten(key, sub, leaves)
			continue
		}
		leaves[key] = v
	}
}

// tomlValue writes one value as a TOML literal. JSON's string escaping is a
// subset of TOML's basic-string escaping, so encoding/json does the quoting.
// Numbers arrive as float64 from JSON; a whole one is written as an integer
// because Codex's integer settings reject 8080.0.
func tomlValue(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", fmt.Errorf("is null, which TOML cannot represent")
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case bool:
		return strconv.FormatBool(t), nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), nil
		}
		return strconv.FormatFloat(t, 'g', -1, 64), nil
	case []any:
		parts := make([]string, 0, len(t))
		for i, e := range t {
			s, err := tomlValue(e)
			if err != nil {
				return "", fmt.Errorf("[%d] %w", i, err)
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, err := tomlValue(t[k])
			if err != nil {
				return "", fmt.Errorf(".%s %w", k, err)
			}
			parts = append(parts, k+" = "+s)
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	default:
		return "", fmt.Errorf("has type %T, which has no TOML form", v)
	}
}
