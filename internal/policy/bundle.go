package policy

// Bundle is the part of a policy that applies to one user: every rule that
// is not scoped to a group the user is outside of, with repository matchers
// left in place. The control plane resolves groups, because only it knows
// them; the client resolves the repository, because only it knows where the
// session runs. The same JSON shape is served over HTTP and written to disk.
type Bundle struct {
	// Version is the policy revision the bundle was cut from.
	Version string `json:"version,omitempty"`
	// User is who the bundle was resolved for, for diagnostics.
	User string `json:"user,omitempty"`
	// Groups are the memberships the bundle was resolved with.
	Groups []string `json:"groups"`
	// Rules are the surviving rules, in their authored order.
	Rules []Rule `json:"rules"`
}

// GroupsFor returns the authored group memberships of user. An unknown user
// has none, and receives only the rules that target everyone. The result is
// never nil, so a caller can hand it to JSON and get a list, not null.
func (rs *RuleSet) GroupsFor(user string) []string {
	if rs == nil {
		return make([]string, 0)
	}
	groups := rs.Groups[user]
	out := make([]string, len(groups))
	copy(out, groups)
	return out
}

// Slice keeps the rules that could apply to a subject with these groups:
// those with no group constraint and those whose groups intersect. Rules
// scoped to a repository are kept whatever the repository, since that is
// resolved later. The rules are shared with the rule set, not copied; nothing
// downstream mutates a rule.
func (rs *RuleSet) Slice(groups []string) *Bundle {
	if rs == nil {
		return &Bundle{Groups: make([]string, 0), Rules: make([]Rule, 0)}
	}
	if groups == nil {
		groups = make([]string, 0)
	}
	bundle := &Bundle{Version: rs.Version, Groups: groups, Rules: make([]Rule, 0, len(rs.Rules))}
	for _, rule := range rs.Rules {
		if len(rule.Match.Groups) > 0 && !anyIn(rule.Match.Groups, groups) {
			continue
		}
		bundle.Rules = append(bundle.Rules, rule)
	}
	return bundle
}

// Compile resolves the bundle for a session in repo (empty outside a
// repository) into the document a client applies. Repository matching is the
// only decision left; group matching was made when the bundle was cut, and
// re-checking it here is harmless because the bundle carries its groups.
func (b *Bundle) Compile(repo string) *Document {
	if b == nil {
		return &Document{}
	}
	subject := Subject{Groups: b.Groups, Repo: repo}
	doc := &Document{Version: b.Version}
	for _, rule := range b.Rules {
		if !rule.Match.Matches(subject) {
			continue
		}
		doc.AppliedRules = append(doc.AppliedRules, rule.Name)
		for agentName, config := range rule.Agents {
			doc.mergeAgent(agentName, config)
		}
	}
	return doc
}
