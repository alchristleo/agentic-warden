package policy

import "sort"

// UnionGroups merges a user's memberships from every source — the authored
// map, the IdP snapshot, SCIM — sorted and without duplicates. Union, not
// replacement: the authored map is the manual override and nothing an
// author wrote disappears when an IdP starts feeding the server. The result
// is never nil, since it is JSON-encoded as a list.
func UnionGroups(lists ...[]string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, list := range lists {
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
