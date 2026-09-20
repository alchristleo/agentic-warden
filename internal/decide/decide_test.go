package decide_test

import (
	"context"
	"testing"

	"github.com/acme/agent-wrapper/internal/decide"
)

func rules(t *testing.T, allow, deny []string) *decide.RulesDecider {
	t.Helper()
	d, err := decide.NewRulesDecider(allow, deny)
	if err != nil {
		t.Fatalf("NewRulesDecider: %v", err)
	}
	return d
}

func TestRulesDeniedToolCallIsDenied(t *testing.T) {
	d := rules(t, nil, []string{"Bash(curl *)"})

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Bash",
		Input: map[string]any{"command": "curl https://evil.example"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Deny {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Deny)
	}
	if got.Source != decide.SourceRules {
		t.Errorf("Source = %q, want %q", got.Source, decide.SourceRules)
	}
}

func TestRulesAllowedToolCallIsAllowed(t *testing.T) {
	d := rules(t, []string{"Bash(git status)"}, nil)

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Bash",
		Input: map[string]any{"command": "git status"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Allow {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Allow)
	}
}

func TestRulesDenyWinsWhenBothListsMatch(t *testing.T) {
	d := rules(t, []string{"Bash(*)"}, []string{"Bash(rm -rf *)"})

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Bash",
		Input: map[string]any{"command": "rm -rf /"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Deny {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Deny)
	}
}

func TestRulesUnmatchedToolCallEscalatesToAsk(t *testing.T) {
	d := rules(t, []string{"Bash(git status)"}, []string{"Bash(curl *)"})

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Bash",
		Input: map[string]any{"command": "npm install"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Ask {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Ask)
	}
}

func TestRulesBareToolNameMatchesEveryCallOfThatTool(t *testing.T) {
	d := rules(t, []string{"WebSearch"}, nil)

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "WebSearch",
		Input: map[string]any{"query": "anything at all"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Allow {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Allow)
	}
}

func TestRulesMatchTheFieldThatCarriesTheToolsSubject(t *testing.T) {
	d := rules(t, nil, []string{"Read(./.env)"})

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Read",
		Input: map[string]any{"file_path": "./.env"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Deny {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Deny)
	}
}

func TestRulesRuleForAnotherToolDoesNotMatch(t *testing.T) {
	d := rules(t, nil, []string{"Bash(git push *)"})

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Read",
		Input: map[string]any{"file_path": "git push origin"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Ask {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Ask)
	}
}

func TestRulesWildcardCrossesPathSeparators(t *testing.T) {
	d := rules(t, nil, []string{"Read(./secrets/*)"})

	got, err := d.Classify(context.Background(), decide.Subject{
		Tool:  "Read",
		Input: map[string]any{"file_path": "./secrets/prod/db.pem"},
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if got.Outcome != decide.Deny {
		t.Errorf("Outcome = %v, want %v", got.Outcome, decide.Deny)
	}
}

func TestRulesMalformedRuleIsRejectedAtConstruction(t *testing.T) {
	if _, err := decide.NewRulesDecider(nil, []string{"Bash(unclosed"}); err == nil {
		t.Error("NewRulesDecider() error = nil, want an error for a malformed rule")
	}
}

func TestRulesDeciderSatisfiesTheDeciderInterface(t *testing.T) {
	var _ decide.Decider = rules(t, nil, nil)
}
