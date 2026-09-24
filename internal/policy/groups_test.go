package policy_test

import (
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

func TestUnionGroupsSortsAndDeduplicates(t *testing.T) {
	got := policy.UnionGroups([]string{"platform", "oncall"}, []string{"oncall", "mobile"})
	if want := []string{"mobile", "oncall", "platform"}; !equal(got, want) {
		t.Errorf("UnionGroups = %v, want %v", got, want)
	}
}

func TestUnionGroupsWithOneSideNil(t *testing.T) {
	if got := policy.UnionGroups(nil, []string{"b", "a"}); !equal(got, []string{"a", "b"}) {
		t.Errorf("nil left: %v", got)
	}
	if got := policy.UnionGroups([]string{"a"}, nil); !equal(got, []string{"a"}) {
		t.Errorf("nil right: %v", got)
	}
}

func TestUnionGroupsOfNothingIsAnEmptySliceNotNil(t *testing.T) {
	got := policy.UnionGroups(nil, nil)
	if got == nil || len(got) != 0 {
		t.Errorf("UnionGroups(nil, nil) = %#v, want an empty, non-nil slice for JSON", got)
	}
}

func TestUnionGroupsOfThreeSources(t *testing.T) {
	got := policy.UnionGroups([]string{"platform"}, nil, []string{"oncall", "platform", "mobile"})
	if want := []string{"mobile", "oncall", "platform"}; !equal(got, want) {
		t.Errorf("UnionGroups = %v, want %v", got, want)
	}
}

func TestUnionGroupsOfNoListsIsAnEmptySliceNotNil(t *testing.T) {
	got := policy.UnionGroups()
	if got == nil || len(got) != 0 {
		t.Errorf("UnionGroups() = %#v, want an empty, non-nil slice", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
