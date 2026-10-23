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
	codexURL        = "https://learn.chatgpt.com/docs/config-file/config-reference.md"
	geminiLatestURL = "https://api.github.com/repos/google-gemini/gemini-cli/releases/latest"
	geminiRawURL    = "https://raw.githubusercontent.com/google-gemini/gemini-cli/%s/packages/%s"
	claudeURL       = "https://code.claude.com/docs/en/settings-reference.md"
	claudeHooksURL  = "https://code.claude.com/docs/en/hooks.md"
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

// get fetches url, trying up to attempts times with a growing pause. Its
// error starts "fetch failed:" so that an outage is not mistaken for drift.
func get(url string, attempts int, backoff time.Duration) (string, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(backoff * time.Duration(i))
		}
		var resp *http.Response
		if resp, err = c.Get(url); err != nil {
			continue
		}
		var b []byte
		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("status %s", resp.Status)
		} else {
			b, err = io.ReadAll(resp.Body)
		}
		resp.Body.Close()
		if err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("fetch failed: %s after %d attempts: %w", url, attempts, err)
}

func fetch(t *testing.T, url string) string {
	t.Helper()
	body, err := get(url, 3, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// parseLatestTag returns tag_name from a GitHub "latest release" response.
func parseLatestTag(body string) (string, error) {
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal([]byte(body), &rel); err != nil || rel.Tag == "" {
		return "", fmt.Errorf("no tag_name in the latest release response")
	}
	return rel.Tag, nil
}

// geminiURLs returns the settings schema and policy types URLs at the latest
// Gemini CLI release tag.
func geminiURLs(t *testing.T) (settings, types string) {
	t.Helper()
	tag, err := parseLatestTag(fetch(t, geminiLatestURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", geminiLatestURL, err)
	}
	return fmt.Sprintf(geminiRawURL, tag, "cli/src/config/settingsSchema.ts"),
		fmt.Sprintf(geminiRawURL, tag, "core/src/policy/types.ts")
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
	fmt.Fprintf(&b, "%s drifted: edit %s.", source, file)
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
	geminiURL, _ := geminiURLs(t)
	keys, err := parseGeminiSettingsKeys(fetch(t, geminiURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", geminiURL, err)
	}
	check(t, "Gemini settings.json ("+geminiURL+")", "internal/agent/gemini/managed/keys.go", keys, managed.SettingsKeys, true)
}

func TestGeminiDecisions(t *testing.T) {
	_, geminiTypesURL := geminiURLs(t)
	vals, err := parseGeminiDecisions(fetch(t, geminiTypesURL))
	if err != nil {
		t.Fatalf("upstream page changed shape: %s: %v", geminiTypesURL, err)
	}
	check(t, "Gemini PolicyDecision ("+geminiTypesURL+")", "internal/agent/gemini/managed/keys.go", vals, managed.Decisions, true)
}

// TestClaudeNestedKeys reports keys the settings reference documents under a
// strict block that the schema (vendored plus overlay) does not define, and
// overlay entries upstream no longer lists.
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
	upstream := append(keys, events...)
	var added []string
	for _, k := range upstream {
		if !schema.Defines(k) {
			added = append(added, k)
		}
	}
	_, removed := diffNames(upstream, schema.OverlayPaths())
	if msg := message("Claude settings ("+claudeURL+", "+claudeHooksURL+")",
		"internal/agent/claude/schema/overlay.go", added, removed); msg != "" {
		t.Error(msg)
	}
}
