// Package decide answers one question: may this tool call proceed?
//
// The deterministic rule set is authoritative and is always consulted first.
// Anything it does not resolve escalates to Ask, which is what the agent does
// on its own today, so a Decider can only ever reduce prompts, never lower the
// floor. A semantic Decider (see the Jev section of the design) plugs in behind
// the same interface to answer the residual, and its failure mode is the same
// Ask the rules already produce.
package decide

import (
	"context"
	"fmt"
	"strings"

	"github.com/acme/agent-wrapper/internal/glob"
)

// Outcome is what should happen to a tool call.
type Outcome int

const (
	// Ask leaves the decision to the developer. It is the zero value on
	// purpose: an uninitialised or failed Decision must never read as Allow.
	Ask Outcome = iota
	Allow
	Deny
)

func (o Outcome) String() string {
	switch o {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return "ask"
	}
}

// Decision sources, recorded so an audit reader can tell a deterministic rule
// from a semantic judgment.
const (
	SourceRules = "rules"
)

// Subject is the tool call being judged.
type Subject struct {
	// Tool is the agent's tool name, e.g. "Bash" or "Read".
	Tool string
	// Input is the tool's arguments as the agent reports them.
	Input map[string]any
	// Cwd is the working directory the call was made from.
	Cwd string
}

// Decision is the answer plus enough context to audit it.
type Decision struct {
	Outcome Outcome
	// Reason is shown to the developer and written to the audit log.
	Reason string
	// Confidence is 1 for a deterministic rule match. A semantic Decider
	// reports its calibrated confidence here.
	Confidence float64
	// Source names the Decider that produced this, e.g. SourceRules.
	Source string
	// Rule is the matched rule, empty when nothing matched.
	Rule string
}

// Decider judges one tool call. Implementations must be safe for concurrent
// use and must return Ask rather than an error whenever they can, so a
// degraded Decider costs a prompt rather than a blocked session.
type Decider interface {
	Classify(ctx context.Context, s Subject) (Decision, error)
}

// RulesDecider matches a call against permission rules in the agent's own
// syntax: a bare tool name ("WebSearch") matches every call of that tool, and
// "Tool(pattern)" matches when the tool's subject field matches pattern.
//
// A '*' in a pattern matches any run of characters, path separators included.
// That is looser than the agent's own matcher and is deliberate for a deny
// list, where matching too much is the safe direction. Do not rely on it to
// express a narrow allow rule.
type RulesDecider struct {
	allow []rule
	deny  []rule
}

type rule struct {
	text    string
	tool    string
	pattern string
	// bare is true for a rule that names a tool and nothing else.
	bare bool
}

// NewRulesDecider compiles the two rule lists, rejecting malformed rules up
// front rather than silently ignoring them at decision time.
func NewRulesDecider(allow, deny []string) (*RulesDecider, error) {
	compiledAllow, err := compileRules(allow)
	if err != nil {
		return nil, fmt.Errorf("allow rules: %w", err)
	}
	compiledDeny, err := compileRules(deny)
	if err != nil {
		return nil, fmt.Errorf("deny rules: %w", err)
	}
	return &RulesDecider{allow: compiledAllow, deny: compiledDeny}, nil
}

func compileRules(texts []string) ([]rule, error) {
	out := make([]rule, 0, len(texts))
	for _, text := range texts {
		r, err := compileRule(text)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func compileRule(text string) (rule, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return rule{}, fmt.Errorf("empty rule")
	}
	open := strings.Index(trimmed, "(")
	if open < 0 {
		return rule{text: trimmed, tool: trimmed, bare: true}, nil
	}
	if !strings.HasSuffix(trimmed, ")") {
		return rule{}, fmt.Errorf("rule %q: missing closing parenthesis", text)
	}
	tool := strings.TrimSpace(trimmed[:open])
	if tool == "" {
		return rule{}, fmt.Errorf("rule %q: missing tool name", text)
	}
	return rule{text: trimmed, tool: tool, pattern: trimmed[open+1 : len(trimmed)-1]}, nil
}

// Classify reports the outcome for s. Deny is checked first, so an allow rule
// can never widen what a deny rule forbids.
func (d *RulesDecider) Classify(_ context.Context, s Subject) (Decision, error) {
	if matched, ok := match(d.deny, s); ok {
		return Decision{
			Outcome:    Deny,
			Reason:     fmt.Sprintf("denied by rule %s", matched.text),
			Confidence: 1,
			Source:     SourceRules,
			Rule:       matched.text,
		}, nil
	}
	if matched, ok := match(d.allow, s); ok {
		return Decision{
			Outcome:    Allow,
			Reason:     fmt.Sprintf("allowed by rule %s", matched.text),
			Confidence: 1,
			Source:     SourceRules,
			Rule:       matched.text,
		}, nil
	}
	return Decision{
		Outcome:    Ask,
		Reason:     "no rule matched",
		Confidence: 1,
		Source:     SourceRules,
	}, nil
}

func match(rules []rule, s Subject) (rule, bool) {
	for _, r := range rules {
		if r.tool != s.Tool {
			continue
		}
		if r.bare {
			return r, true
		}
		if glob.Match(r.pattern, subjectField(s)) {
			return r, true
		}
	}
	return rule{}, false
}

// subjectFields names the input field that carries what a rule matches on, per
// tool. A tool that is not listed matches only on a bare tool-name rule.
var subjectFields = map[string]string{
	"Bash":      "command",
	"Read":      "file_path",
	"Write":     "file_path",
	"Edit":      "file_path",
	"WebFetch":  "url",
	"WebSearch": "query",
}

func subjectField(s Subject) string {
	field, ok := subjectFields[s.Tool]
	if !ok {
		return ""
	}
	value, _ := s.Input[field].(string)
	return value
}
