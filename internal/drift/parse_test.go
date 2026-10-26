//go:build drift

package drift

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDiffNames(t *testing.T) {
	added, removed := diffNames([]string{"a", "b", "c"}, []string{"a", "c", "d"})
	if !reflect.DeepEqual(added, []string{"b"}) || !reflect.DeepEqual(removed, []string{"d"}) {
		t.Fatalf("diffNames = %v, %v; want [b], [d]", added, removed)
	}
	msg := message("Codex requirements.toml", "internal/agent/codex/requirements/keys.go", nil, []string{"d"})
	for _, want := range []string{"d", "removed", "internal/agent/codex/requirements/keys.go"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if message("x", "f.go", nil, nil) != "" {
		t.Error("message with no difference should be empty")
	}
}

func TestParseCodexKeys(t *testing.T) {
	page := "## `other`\n\n key: \"nope\",\n\n## `requirements.toml`\n\nprose\n\n" +
		"    {\n      key: \"allowed_sandbox_modes\",\n    },\n" +
		"    {\n      key: \"remote_sandbox_config[].hostname_patterns\",\n    },\n" +
		"    {\n      key: \"remote_sandbox_config\",\n    },\n" +
		"    {\n      key: \"features.web_search\",\n    },\n"
	got, err := parseCodexKeys(page)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"allowed_sandbox_modes", "features", "remote_sandbox_config"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := parseCodexKeys("# nothing here"); err == nil {
		t.Fatal("want an error for a page without the section")
	}
}

func TestParseClaudeSettingsKeys(t *testing.T) {
	page := "| Key | Desc |\n| [`model`](#model) | x |\n| [`sandbox.enabled`](#a) | x |\n" +
		"| [`sandbox.network.allowedDomains`](#b) | x |\n| [`env.FOO`](#c) | x |\n| [`worktree.location`](#d) | x |\n"
	got, err := parseClaudeSettingsKeys(page)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sandbox.enabled", "sandbox.network.allowedDomains", "worktree.location"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := parseClaudeSettingsKeys("nothing"); err == nil {
		t.Fatal("want an error")
	}
}

func TestParseClaudeHookEvents(t *testing.T) {
	page := "## Configuration\n\n### Hook locations\n\n## Hook events\n\nintro\n\n### SessionStart\n\n#### Matcher\n\n### PreToolUse\n\n## Next\n\n### Elsewhere\n"
	got, err := parseClaudeHookEvents(page)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"hooks.PreToolUse", "hooks.SessionStart"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := parseClaudeHookEvents("nothing"); err == nil {
		t.Fatal("want an error")
	}
}

func TestParsersRejectEmptySections(t *testing.T) {
	cases := map[string]func() error{
		"codex": func() error { _, err := parseCodexKeys("## `requirements.toml`\n\nprose only\n"); return err },
		"codex empty name": func() error {
			_, err := parseCodexKeys("## `requirements.toml`\n\n      key: \"[]\",\n")
			return err
		},
		"claude events": func() error {
			_, err := parseClaudeHookEvents("## Hook events\n\nprose only\n\n## Next\n")
			return err
		},
	}
	for name, f := range cases {
		if f() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestDiffFetchRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "busy", http.StatusBadGateway)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	got, err := get(srv.URL, 3, time.Millisecond)
	if err != nil || got != "ok" || calls != 3 {
		t.Fatalf("get = %q, %v after %d calls; want ok after 3", got, err, calls)
	}
	calls = -10
	_, err = get(srv.URL, 3, time.Millisecond)
	if err == nil || !strings.HasPrefix(err.Error(), "fetch failed:") {
		t.Fatalf("get error = %v; want a message starting \"fetch failed:\"", err)
	}
}
