package policy

import "sort"

// UnionGroups merges the authored memberships with the synced ones, sorted
// and without duplicates. Union, not replacement: the authored map is the
// manual override and nothing an author wrote disappears when the first
// snapshot lands. The result is never nil, since it is JSON-encoded as a
// list.
func UnionGroups(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, g := range list {
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	sort.Strings(out)
	return out
}
