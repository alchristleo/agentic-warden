package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

func writeDoc(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadReadsTheAgentsManagedSettingsAndEnvironment(t *testing.T) {
	path := writeDoc(t, `{
	  "agents": {
	    "claude": {
	      "managed": {"model": "opus"},
	      "env": {"ANTHROPIC_BASE_URL": "https://gateway.acme.com"},
	      "forceEnv": true
	    }
	  }
	}`)

	doc, err := policy.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	claude := doc.Agent("claude")
	if claude.Managed["model"] != "opus" {
		t.Errorf("managed model = %v, want %q", claude.Managed["model"], "opus")
	}
	if claude.Env["ANTHROPIC_BASE_URL"] != "https://gateway.acme.com" {
		t.Errorf("env = %v, want the gateway URL", claude.Env)
	}
	if !claude.ForceEnv {
		t.Error("ForceEnv = false, want true")
	}
}

func TestAgentReturnsAnEmptyConfigForAnAgentThePolicyDoesNotMention(t *testing.T) {
	path := writeDoc(t, `{"agents": {"claude": {"managed": {"model": "opus"}}}}`)
	doc, err := policy.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := doc.Agent("codex")

	if len(got.Managed) != 0 || len(got.Env) != 0 || got.ForceEnv {
		t.Errorf("Agent(%q) = %+v, want a zero config", "codex", got)
	}
}

func TestAgentOnANilDocumentIsSafe(t *testing.T) {
	var doc *policy.Document

	got := doc.Agent("claude")

	if len(got.Managed) != 0 {
		t.Errorf("Agent() on a nil document = %+v, want a zero config", got)
	}
}

func TestLoadNamesTheFileItCouldNotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")

	_, err := policy.Load(missing)

	if err == nil {
		t.Fatal("Load() error = nil, want an error for a missing file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestLoadRejectsAMalformedDocument(t *testing.T) {
	path := writeDoc(t, `{"agents": `)

	if _, err := policy.Load(path); err == nil {
		t.Error("Load() error = nil, want an error for malformed JSON")
	}
}

func TestLoadAcceptsADocumentWithNoAgents(t *testing.T) {
	path := writeDoc(t, `{}`)

	doc, err := policy.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(doc.Agent("claude").Managed) != 0 {
		t.Error("an empty document produced settings")
	}
}
