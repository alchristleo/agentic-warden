package glob_test

import (
	"testing"

	"github.com/acme/agent-wrapper/internal/glob"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{"exact match", "git status", "git status", true},
		{"exact mismatch", "git status", "git push", false},
		{"trailing star matches a suffix", "git *", "git status", true},
		{"trailing star requires the prefix", "git *", "npm install", false},
		{"trailing star matches an empty tail", "git*", "git", true},
		{"leading star matches a prefix", "*.pem", "server.pem", true},
		{"star crosses path separators", "./secrets/*", "./secrets/prod/db.pem", true},
		{"star between literals", "npm * --prod", "npm install --prod", true},
		{"star between literals needs both ends", "npm * --prod", "npm install --dev", false},
		{"bare star matches anything", "*", "anything at all", true},
		{"bare star matches empty", "*", "", true},
		{"empty pattern matches only empty", "", "", true},
		{"empty pattern rejects content", "", "x", false},
		{"literal is not a prefix match", "git", "git status", false},
		{"overlapping literals are not double counted", "a*a", "aa", true},
		{"overlapping literals need both", "a*a", "a", false},
		{"repeated stars collapse", "a**b", "axxb", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := glob.Match(tc.pattern, tc.value); got != tc.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
			}
		})
	}
}

func TestMatchAnyReportsTheFirstPatternThatMatches(t *testing.T) {
	patterns := []string{"github.com/acme/infra*", "github.com/acme/web*"}

	got, ok := glob.MatchAny(patterns, "github.com/acme/web-frontend")

	if !ok {
		t.Fatal("MatchAny() ok = false, want true")
	}
	if got != "github.com/acme/web*" {
		t.Errorf("MatchAny() = %q, want the matching pattern", got)
	}
}

func TestMatchAnyOfNothingMatchesNothing(t *testing.T) {
	if _, ok := glob.MatchAny(nil, "anything"); ok {
		t.Error("MatchAny(nil) ok = true, want false")
	}
}
