package policy_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

// slicingRuleSet has one rule of each kind: ungrouped, grouped, repo-scoped,
// and both grouped and repo-scoped.
func slicingRuleSet() *policy.RuleSet {
	claude := func(model string) map[string]policy.AgentConfig {
		return map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": model}}}
	}
	return &policy.RuleSet{
		Version: "v1",
		Groups: map[string][]string{
			"alice@acme.com": {"platform", "security"},
			"bob@acme.com":   {"mobile"},
		},
		Rules: []policy.Rule{
			{Name: "baseline", Agents: claude("sonnet")},
			{Name: "platform", Match: policy.Match{Groups: []string{"platform"}}, Agents: claude("opus")},
			{Name: "mobile", Match: policy.Match{Groups: []string{"mobile"}}, Agents: claude("haiku")},
			{Name: "payments", Match: policy.Match{Repos: []string{"github.com/acme/payments*"}}, Agents: claude("opus-payments")},
			{Name: "platform-payments",
				Match:  policy.Match{Groups: []string{"platform"}, Repos: []string{"github.com/acme/payments*"}},
				Agents: claude("opus-platform-payments")},
		},
	}
}

func ruleNames(rules []policy.Rule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Name)
	}
	return out
}

func TestGroupsForReturnsTheAuthoredMembership(t *testing.T) {
	rs := slicingRuleSet()

	if got := rs.GroupsFor("alice@acme.com"); !reflect.DeepEqual(got, []string{"platform", "security"}) {
		t.Errorf("GroupsFor(alice) = %v", got)
	}
	if got := rs.GroupsFor("nobody@acme.com"); len(got) != 0 {
		t.Errorf("GroupsFor(unknown) = %v, want empty: an unknown user gets the baseline only", got)
	}
	var nilSet *policy.RuleSet
	if got := nilSet.GroupsFor("alice@acme.com"); got == nil || len(got) != 0 {
		t.Errorf("GroupsFor on a nil rule set = %v, want an empty non-nil slice", got)
	}
}

func TestSliceKeepsUngroupedGroupMatchingAndRepoScopedRules(t *testing.T) {
	rs := slicingRuleSet()

	bundle := rs.Slice([]string{"platform"})

	want := []string{"baseline", "platform", "payments", "platform-payments"}
	if got := ruleNames(bundle.Rules); !reflect.DeepEqual(got, want) {
		t.Errorf("Slice(platform) rules = %v, want %v (repo-scoped rules stay in; the client resolves them)", got, want)
	}
	if bundle.Version != "v1" || !reflect.DeepEqual(bundle.Groups, []string{"platform"}) {
		t.Errorf("bundle = %+v, want the version and groups carried", bundle)
	}
}

func TestSliceDropsRulesForOtherGroups(t *testing.T) {
	rs := slicingRuleSet()

	bundle := rs.Slice(nil)

	want := []string{"baseline", "payments"}
	if got := ruleNames(bundle.Rules); !reflect.DeepEqual(got, want) {
		t.Errorf("Slice(no groups) rules = %v, want %v", got, want)
	}
}

func TestSliceKeepsRepoMatchersVerbatim(t *testing.T) {
	rs := slicingRuleSet()

	bundle := rs.Slice([]string{"platform"})

	for _, rule := range bundle.Rules {
		if rule.Name == "platform-payments" && !reflect.DeepEqual(rule.Match.Repos, []string{"github.com/acme/payments*"}) {
			t.Errorf("repo matcher was altered: %+v", rule.Match)
		}
	}
}

func TestBundleCompileResolvesRepoScopedRulesOnTheClient(t *testing.T) {
	bundle := slicingRuleSet().Slice([]string{"platform"})

	inPayments := bundle.Compile("github.com/acme/payments-api")
	elsewhere := bundle.Compile("github.com/acme/website")
	noRepo := bundle.Compile("")

	if got := inPayments.Agent("claude").Managed["model"]; got != "opus-platform-payments" {
		t.Errorf("in payments: model = %v, want the last matching rule's", got)
	}
	if got := elsewhere.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("elsewhere: model = %v, want the group rule's", got)
	}
	if got := noRepo.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("outside a repo: model = %v, want the group rule's", got)
	}
	if !reflect.DeepEqual(inPayments.AppliedRules, []string{"baseline", "platform", "payments", "platform-payments"}) {
		t.Errorf("applied rules = %v", inPayments.AppliedRules)
	}
}

func TestBundleCompileOnNilIsEmpty(t *testing.T) {
	var b *policy.Bundle
	if doc := b.Compile("github.com/acme/x"); doc == nil || len(doc.Agents) != 0 {
		t.Errorf("nil bundle compiled to %+v, want an empty document", doc)
	}
}

func TestSliceThenCompileEqualsCompile(t *testing.T) {
	// The split must not change what a subject receives. This runs the
	// shipped example policy plus the synthetic one through both paths for
	// every combination of group and repo the rules mention.
	example, err := policy.LoadRuleSet("../../examples/org-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, rs := range []*policy.RuleSet{example, slicingRuleSet()} {
		for _, groups := range [][]string{nil, {"platform"}, {"mobile"}, {"platform", "mobile"}} {
			for _, repo := range []string{"", "github.com/acme/payments-api", "github.com/acme/website"} {
				subject := policy.Subject{Groups: groups, Repo: repo}
				direct := rs.Compile(subject)
				split := rs.Slice(groups).Compile(repo)
				if !reflect.DeepEqual(direct, split) {
					t.Errorf("groups=%v repo=%q:\n direct = %+v\n split  = %+v", groups, repo, direct, split)
				}
			}
		}
	}
}

func TestValidateRejectsAnEmptyUserInGroups(t *testing.T) {
	rs := &policy.RuleSet{Version: "v1", Groups: map[string][]string{"": {"platform"}}}
	if err := rs.Validate(); err == nil {
		t.Error("Validate() = nil, want an error for a group entry with no user")
	}
}

func TestNilRuleSetSlicesToEmptyLists(t *testing.T) {
	var rs *policy.RuleSet
	b := rs.Slice(nil)

	if b.Groups == nil || len(b.Groups) != 0 {
		t.Errorf("Groups = %v, want non-nil empty slice", b.Groups)
	}
	if b.Rules == nil || len(b.Rules) != 0 {
		t.Errorf("Rules = %v, want non-nil empty slice", b.Rules)
	}

	// Verify JSON serialization contains empty arrays, not null
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"groups":[]`) {
		t.Errorf("JSON missing \"groups\":[], got %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"rules":[]`) {
		t.Errorf("JSON missing \"rules\":[], got %s", jsonStr)
	}
}
