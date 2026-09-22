package policy

import (
	"fmt"
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
