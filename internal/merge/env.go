package merge

import (
	"fmt"
	"sort"
	"strings"
)

// Env overlays defaults onto base (a KEY=VALUE list, as os.Environ returns)
// and returns the combined environment plus a note for every decision.
//
// With force false the environment the developer already has wins, and org
// defaults only fill gaps. With force true (the policy layer) org values
// replace what is there. Notes are ordered by variable name so two launches
// with the same inputs produce byte-identical output.
//
// base is never modified.
func Env(base []string, defaults map[string]string, force bool) (out, notes []string) {
	out = make([]string, len(base))
	copy(out, base)

	// Duplicate keys can appear in a real environment; the first occurrence is
	// the one the process sees, so that is the one to inspect and replace.
	index := make(map[string]int, len(base))
	for i, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, seen := index[key]; !seen {
			index[key] = i
		}
	}

	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := defaults[key]
		i, present := index[key]
		switch {
		case present && force:
			out[i] = key + "=" + value
			notes = append(notes, fmt.Sprintf("env %s forced to %q (policy)", key, value))
		case present:
			notes = append(notes, fmt.Sprintf("env %s kept from environment (org default %q ignored)", key, value))
		default:
			out = append(out, key+"="+value)
			notes = append(notes, fmt.Sprintf("env %s=%q injected", key, value))
		}
	}
	return out, notes
}
