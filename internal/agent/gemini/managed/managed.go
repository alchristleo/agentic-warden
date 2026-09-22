// Package managed validates the document aw-sync renders for Gemini CLI:
// {settings: {...}, policies: [{...}]}. settings becomes the system
// settings.json and policies becomes an admin policy TOML file, so the
// checks are the ones each format needs: a vendored, dated allowlist of
// top-level settings keys plus a JSON round trip, and for every policy
// rule the three fields Gemini's own loader refuses to do without.
package managed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Parts splits a Gemini managed document into its settings object and its
// policy list, rejecting any other top-level key. Both halves are optional;
// a missing half comes back nil.
func Parts(doc map[string]any) (map[string]any, []any, error) {
	var unknown []string
	for key := range doc {
		if key != "settings" && key != "policies" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, nil, fmt.Errorf("managed: unknown top-level key(s) %s; a Gemini document has settings and policies alone", quoted(unknown))
	}
	var settings map[string]any
	if raw, present := doc["settings"]; present && raw != nil {
		var ok bool
		if settings, ok = raw.(map[string]any); !ok {
			return nil, nil, fmt.Errorf("managed: settings must be an object, not %T", raw)
		}
	}
	var policies []any
	if raw, present := doc["policies"]; present && raw != nil {
		var ok bool
		if policies, ok = raw.([]any); !ok {
			return nil, nil, fmt.Errorf("managed: policies must be a list, not %T", raw)
		}
	}
	return settings, policies, nil
}

// Validate reports whether doc can be rendered for Gemini: the document has
// the right shape, every top-level settings key is documented, settings
// survive a JSON round trip, and every policy rule carries a toolName, a
// known decision and an integer priority from 0 to 999, with no null
// anywhere in it, because TOML cannot represent one. A nil or empty
// document is valid; it renders to an empty settings file and a header-only
// policy file, which is a policy that requires nothing.
func Validate(doc map[string]any) error {
	settings, policies, err := Parts(doc)
	if err != nil {
		return err
	}
	if err := validateSettings(settings); err != nil {
		return err
	}
	for i, entry := range policies {
		if err := validateRule(entry, fmt.Sprintf("policies[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

// ForAgent is a policy.ManagedValidator: it validates the managed document of
// rules aimed at Gemini and ignores every other agent, whose format it does
// not know.
func ForAgent(agentName string, doc map[string]any) error {
	if agentName != "gemini" || doc == nil {
		return nil
	}
	return Validate(doc)
}

// Integer reports v as an int64 when it is a whole number: a Go int from a
// literal, or a float64 with no fraction, which is how every number in a
// bundle arrives after JSON decoding. The renderer uses it to write
// priority as a TOML integer, since Gemini rejects 100.0.
func Integer(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
	}
	return 0, false
}

func validateSettings(settings map[string]any) error {
	var unknown []string
	for key := range settings {
		if !knownKey(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("managed: unknown settings key(s) %s; the allowlist is from the reference as of %s", quoted(unknown), KeysAsOf)
	}
	if len(settings) == 0 {
		return nil
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("managed: settings cannot be written as JSON: %w", err)
	}
	var back map[string]any
	if err := json.Unmarshal(encoded, &back); err != nil {
		return fmt.Errorf("managed: settings do not read back as JSON: %w", err)
	}
	return nil
}

// validateRule checks one policy entry against what Gemini's TOML loader
// requires. path names the entry in errors, as policies[i].
func validateRule(entry any, path string) error {
	rule, ok := entry.(map[string]any)
	if !ok {
		return fmt.Errorf("managed: %s must be an object, not %T", path, entry)
	}
	if p := firstNull(rule, path); p != "" {
		return fmt.Errorf("managed: %s is null, which TOML cannot represent", p)
	}
	switch name := rule["toolName"].(type) {
	case string:
		if name == "" {
			return fmt.Errorf(`managed: %s.toolName is empty; use "*" to match every tool`, path)
		}
	case []any:
		if len(name) == 0 {
			return fmt.Errorf("managed: %s.toolName is an empty list", path)
		}
		for i, n := range name {
			if s, ok := n.(string); !ok || s == "" {
				return fmt.Errorf("managed: %s.toolName[%d] must be a non-empty string", path, i)
			}
		}
	default:
		return fmt.Errorf("managed: %s.toolName is required and must be a string or a list of strings", path)
	}
	decision, _ := rule["decision"].(string)
	if !knownDecision(decision) {
		return fmt.Errorf("managed: %s.decision must be one of %s, not %q", path, strings.Join(Decisions, ", "), decision)
	}
	if priority, ok := Integer(rule["priority"]); !ok || priority < 0 || priority > 999 {
		return fmt.Errorf("managed: %s.priority is required and must be an integer from 0 to 999; Gemini rejects the whole policy file otherwise", path)
	}
	return nil
}

// firstNull returns the path of the first nil value in v, or "" when there
// is none. TOML has no null: the encoder drops a nil map value silently and
// refuses a nil slice element, and neither is what an author meant.
func firstNull(v any, path string) string {
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
			if found := firstNull(t[k], path+"."+k); found != "" {
				return found
			}
		}
	case []any:
		for i, e := range t {
			if found := firstNull(e, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

func knownKey(key string) bool {
	i := sort.SearchStrings(SettingsKeys, key)
	return i < len(SettingsKeys) && SettingsKeys[i] == key
}

func knownDecision(d string) bool {
	i := sort.SearchStrings(Decisions, d)
	return i < len(Decisions) && Decisions[i] == d
}

func quoted(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(q, ", ")
}
