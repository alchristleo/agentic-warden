//go:build drift

// Package drift compares the key lists this repository pins against the
// vendors' published references. It runs weekly in CI (see
// .github/workflows/drift.yml), never in the normal test run: it needs the
// network.
package drift

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/agent/codex/requirements"
	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
)

const (
	codexURL       = "https://learn.chatgpt.com/docs/config-file/config-reference.md"
	geminiURL      = "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/cli/src/config/settingsSchema.ts"
	geminiTypesURL = "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/core/src/policy/types.ts"
	claudeURL      = "https://code.claude.com/docs/en/settings-reference.md"
	claudeHooksURL = "https://code.claude.com/docs/en/hooks.md"
)

// claudeKeyedBlocks are the blocks the settings index lists dotted keys for
// today. statusLine, fileSuggestion, spinnerVerbs, spinnerTipsOverride,
// subagentStatusLine and voice have none upstream, so they are exempt; hooks
// is covered by the hook events page.
var claudeKeyedBlocks = []string{"attribution", "permissions", "sandbox", "worktree", "autoMode", "policyHelper"}

// claudeBlocks are the settings blocks that reject unknown keys.
var claudeBlocks = []string{
	"attribution", "permissions", "hooks", "statusLine", "fileSuggestion", "sandbox",
	"spinnerVerbs", "spinnerTipsOverride", "worktree", "autoMode", "subagentStatusLine",
	"voice", "policyHelper",
}

func fetch(t *testing.T, url string) string {
	t.Helper()
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("fetch %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch %s: status %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("fetch %s: %v", url, err)
	}
	return string(b)
}

// diffNames returns the names upstream has that pinned lacks (added) and the
// names pinned has that upstream lacks (removed), both sorted.
func diffNames(upstream, pinned []string) (added, removed []string) {
	up, pin := map[string]bool{}, map[string]bool{}
	for _, n := range upstream {
		up[n] = true
	}
	for _, n := range pinned {
		pin[n] = true
	}
	for n := range up {
		if !pin[n] {
			added = append(added, n)
		}
	}
	for n := range pin {
		if !up[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// message describes a difference, or returns "" when there is none.
func message(source, file string, added, removed []string) string {
	if len(added) == 0 && len(removed) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s drifted from the pinned list; edit %s and its KeysAsOf date.", source, file)
	if len(added) > 0 {
		fmt.Fprintf(&b, "\n  added upstream: %s", strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		fmt.Fprintf(&b, "\n  removed upstream: %s", strings.Join(removed, ", "))
	}
	return b.String()
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// section returns the text after the line equal to heading, up to the next
// line that starts with level (for example "## "), or to the end of the page.
func section(page, heading, level string) (string, bool) {
	i := strings.Index("\n"+page, "\n"+heading+"\n")
	if i < 0 {
		return "", false
	}
	rest := page[i+len(heading):]
	if j := strings.Index(rest, "\n"+level); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

var (
	codexKeyRe    = regexp.MustCompile(`(?m)^\s*key:\s*"([^"]+)"`)
	geminiKeyRe   = regexp.MustCompile(`(?m)^  (\w+): (?:\{|\w+\()`)
	geminiEnumRe  = regexp.MustCompile(`(?m)^\s+\w+ = '([^']+)'`)
	claudeKeyRe   = regexp.MustCompile("(?m)^\\| \\[`([^`]+)`\\]")
	claudeEventRe = regexp.MustCompile(`(?m)^### (\w+)\s*$`)
)

// parseCodexKeys returns the top-level keys of the requirements.toml section.
func parseCodexKeys(page string) ([]string, error) {
	sec, ok := section(page, "## `requirements.toml`", "## ")
	if !ok {
		return nil, fmt.Errorf("no requirements.toml section")
	}
	var keys []string
	for _, m := range codexKeyRe.FindAllStringSubmatch(sec, -1) {
		name := m[1]
		if i := strings.IndexAny(name, ".["); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			return nil, fmt.Errorf("key %q has no name", m[1])
		}
		keys = append(keys, name)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no key entries in the requirements.toml section")
	}
	return sortedUnique(keys), nil
}

// parseGeminiSettingsKeys returns the top-level keys of SETTINGS_SCHEMA.
func parseGeminiSettingsKeys(src string) ([]string, error) {
	const start = "const SETTINGS_SCHEMA = {"
	i := strings.Index(src, start)
	if i < 0 {
		return nil, fmt.Errorf("no SETTINGS_SCHEMA")
	}
	body := src[i+len(start):]
	j := strings.Index(body, "\n}")
	if j < 0 {
		return nil, fmt.Errorf("SETTINGS_SCHEMA has no end")
	}
	var keys []string
	for _, m := range geminiKeyRe.FindAllStringSubmatch(body[:j], -1) {
		keys = append(keys, m[1])
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no keys in SETTINGS_SCHEMA")
	}
	return sortedUnique(keys), nil
}

// parseGeminiDecisions returns the values of the PolicyDecision enum.
func parseGeminiDecisions(src string) ([]string, error) {
	i := strings.Index(src, "enum PolicyDecision {")
	if i < 0 {
		return nil, fmt.Errorf("no PolicyDecision enum")
	}
	body := src[i:]
	j := strings.Index(body, "\n}")
	if j < 0 {
		return nil, fmt.Errorf("PolicyDecision has no end")
	}
	var vals []string
	for _, m := range geminiEnumRe.FindAllStringSubmatch(body[:j], -1) {
		vals = append(vals, m[1])
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("PolicyDecision has no values")
	}
	return sortedUnique(vals), nil
}

// parseClaudeSettingsKeys returns the dotted keys under the strict blocks
// listed in the settings index table.
func parseClaudeSettingsKeys(page string) ([]string, error) {
	var keys []string
	for _, m := range claudeKeyRe.FindAllStringSubmatch(page, -1) {
		for _, b := range claudeBlocks {
			if strings.HasPrefix(m[1], b+".") {
				keys = append(keys, m[1])
			}
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no dotted keys in the settings index")
	}
	return sortedUnique(keys), nil
}

// parseClaudeHookEvents returns "hooks.<Event>" for each event heading under
// "## Hook events".
func parseClaudeHookEvents(page string) ([]string, error) {
	sec, ok := section(page, "## Hook events", "## ")
	if !ok {
		return nil, fmt.Errorf("no Hook events section")
	}
	var keys []string
	for _, m := range claudeEventRe.FindAllStringSubmatch(sec, -1) {
		keys = append(keys, "hooks."+m[1])
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no event headings in the Hook events section")
	}
	return sortedUnique(keys), nil
}

// vendoredPaths lists every dotted property path under the strict blocks in
// the vendored schema, following local $ref links.
func vendoredPaths(t *testing.T) []string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(schema.Source, &doc); err != nil {
		t.Fatalf("vendored schema: %v", err)
	}
	var out []string
	var walk func(node map[string]any, path string, depth int)
	walk = func(node map[string]any, path string, depth int) {
		for hops := 0; hops < 16; hops++ {
			ref, ok := node["$ref"].(string)
			if !ok || !strings.HasPrefix(ref, "#/") {
				break
			}
			var cur any = doc
			for _, part := range strings.Split(ref[2:], "/") {
				m, _ := cur.(map[string]any)
				cur = m[part]
			}
			next, ok := cur.(map[string]any)
			if !ok {
				return
			}
			node = next
		}
		props, _ := node["properties"].(map[string]any)
		for name, v := range props {
			child, ok := v.(map[string]any)
			if !ok {
				continue
			}
			p := path + "." + name
			out = append(out, p)
			if depth < 5 {
				walk(child, p, depth+1)
			}
		}
	}
	top, _ := doc["properties"].(map[string]any)
	for _, b := range claudeBlocks {
		if blk, ok := top[b].(map[string]any); ok {
			walk(blk, b, 0)
		}
	}
	return out
}

func check(t *testing.T, source, file string, upstream, pinned []string, bothWays bool) {
	t.Helper()
	added, removed := diffNames(upstream, pinned)
	if !bothWays {
		removed = nil
	}
	if msg := message(source, file, added, removed); msg != "" {
		t.Error(msg)
	}
}

func TestCodexRequirementsKeys(t *testing.T) {
	keys, err := parseCodexKeys(fetch(t, codexURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", codexURL, err)
	}
	check(t, "Codex requirements.toml ("+codexURL+")", "internal/agent/codex/requirements/keys.go", keys, requirements.Keys, true)
}

func TestGeminiSettingsKeys(t *testing.T) {
	keys, err := parseGeminiSettingsKeys(fetch(t, geminiURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", geminiURL, err)
	}
	check(t, "Gemini settings.json ("+geminiURL+")", "internal/agent/gemini/managed/keys.go", keys, managed.SettingsKeys, true)
}

func TestGeminiDecisions(t *testing.T) {
	vals, err := parseGeminiDecisions(fetch(t, geminiTypesURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", geminiTypesURL, err)
	}
	check(t, "Gemini PolicyDecision ("+geminiTypesURL+")", "internal/agent/gemini/managed/keys.go", vals, managed.Decisions, true)
}

// TestClaudeNestedKeys reports keys the settings reference documents under a
// strict block that neither the vendored schema nor the overlay knows. The
// reverse direction is not reported: removals are noisy.
func TestClaudeNestedKeys(t *testing.T) {
	keys, err := parseClaudeSettingsKeys(fetch(t, claudeURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", claudeURL, err)
	}
	for _, b := range claudeKeyedBlocks {
		found := false
		for _, k := range keys {
			found = found || strings.HasPrefix(k, b+".")
		}
		if !found {
			t.Fatalf("upstream page changed shape: %s lists no keys under %q", claudeURL, b)
		}
	}
	events, err := parseClaudeHookEvents(fetch(t, claudeHooksURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", claudeHooksURL, err)
	}
	pinned := vendoredPaths(t)
	for _, e := range schema.Overlay {
		pinned = append(pinned, e.Path)
	}
	check(t, "Claude settings ("+claudeURL+", "+claudeHooksURL+")",
		"internal/agent/claude/schema/overlay.go (schema.Overlay)", append(keys, events...), pinned, false)
}
