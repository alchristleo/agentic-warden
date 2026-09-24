package policy

import (
	"fmt"
	"regexp"
	"sort"
)

// FirstNull returns the path of the first nil value in v, or "" when there
// is none. TOML has no null: launch and managed documents that end up as
// TOML must reject one at apply time, and the walker names the path so the
// author can find it.
func FirstNull(v any, path string) string {
	switch t := v.(type) {
	case nil:
		return path
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if found := FirstNull(t[k], p); found != "" {
				return found
			}
		}
	case []any:
		for i, e := range t {
			if found := FirstNull(e, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

// bareKey is the character set a TOML bare key allows. Codex's -c flag
// addresses a launch document entry by joining every table level with ".",
// and an inline table (a table inside an array, which cannot be dotted)
// writes its keys the same way; a key outside this set would either break
// that syntax or silently address a different, nested key than the one the
// author wrote.
var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// FirstBadKey returns the dotted path and the offending key of the first map
// key in v that is not a bare TOML key, or ("", "") when every key is
// clean. Only map keys are checked; a leaf value is not a TOML key.
func FirstBadKey(v any, path string) (string, string) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if !bareKey.MatchString(k) {
				return p, k
			}
			if found, badKey := FirstBadKey(t[k], p); found != "" {
				return found, badKey
			}
		}
	case []any:
		for i, e := range t {
			if found, badKey := FirstBadKey(e, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found, badKey
			}
		}
	}
	return "", ""
}
