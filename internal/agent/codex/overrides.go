package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// bareKey is the character set a TOML bare key allows. flatten dots every
// table level together to address it from -c, and tomlValue writes an
// inline table's keys the same way; a key outside this set would either
// break that syntax or silently address a different, nested key than the
// one the author wrote. The policy package rejects this before a launch
// document ever reaches this adapter (see policy.FirstBadKey); the check
// here is defence in depth for a launch document built some other way.
var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// overrides turns the launch document into the key=value strings Codex's
// -c flag takes, one per leaf, sorted so two launches from the same policy
// produce the same command line. Nested tables become dotted keys, which
// is how Codex addresses them; a table inside an array cannot be dotted
// and is written inline instead. Values are TOML: Codex parses the value
// as TOML and falls back to a raw string, so strings are quoted to keep
// "1.0" a string and "true" a string.
func overrides(launch map[string]any) ([]string, error) {
	leaves := make(map[string]any)
	if err := flatten("", launch, leaves); err != nil {
		return nil, fmt.Errorf("codex: launch: %w", err)
	}
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

// flatten walks tables into dotted keys and stops at anything else. It
// rejects a key segment codex -c cannot address as a bare TOML key.
func flatten(prefix string, table map[string]any, leaves map[string]any) error {
	for k, v := range table {
		if !bareKey.MatchString(k) {
			return fmt.Errorf("key %q must use only letters, digits, _ and - to reach codex -c", k)
		}
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			if err := flatten(key, sub, leaves); err != nil {
				return err
			}
			continue
		}
		leaves[key] = v
	}
	return nil
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
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return "", err
		}
		return strings.TrimSuffix(buf.String(), "\n"), nil
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
			if !bareKey.MatchString(k) {
				return "", fmt.Errorf("key %q must use only letters, digits, _ and - to reach codex -c", k)
			}
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
