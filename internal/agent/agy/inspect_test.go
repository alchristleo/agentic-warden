package agy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/agent/agy"
)

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func inspected(t *testing.T) *agy.Adapter {
	t.Helper()
	a := agy.New()
	a.SystemDir = t.TempDir()
	return a
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const quiet = "PATH=/nowhere"

func TestInspectWarnsWhenTheAdminFileIsMissing(t *testing.T) {
	f := inspected(t).Inspect([]string{quiet})
	if !findingsWith(f, agent.Warn, "admin_settings.json") || !findingsWith(f, agent.Warn, "until aw-sync has run") {
		t.Errorf("findings %v", f)
	}
}

func TestInspectErrorsOnUnparseableJSON(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile), `{"gateway":`)
	f := a.Inspect([]string{quiet})
	if !findingsWith(f, agent.Error, "not valid JSON, so agy ignores it") {
		t.Errorf("findings %v", f)
	}
}

func TestInspectErrorsWhenValidateFails(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile), `{"bogus":1}`)
	f := a.Inspect([]string{quiet})
	if !findingsWith(f, agent.Error, "bogus") {
		t.Errorf("findings %v", f)
	}
}

func TestInspectWarnsWithoutARevisionSibling(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile), `{}`)
	f := a.Inspect([]string{quiet})
	if !findingsWith(f, agent.Warn, "not written by aw-sync") {
		t.Errorf("findings %v", f)
	}
}

func TestInspectReportsThePresentFileAndItsRevision(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile), `{}`)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile+".aw-revision"), "2026-10-26.3\n")
	f := a.Inspect([]string{quiet})
	if !findingsWith(f, agent.OK, "admin file present, revision 2026-10-26.3") {
		t.Errorf("findings %v", f)
	}
	for _, x := range f {
		if strings.Contains(x.Message, "enforced") {
			t.Errorf("message %q claims enforcement", x.Message)
		}
	}
}

func gatewayOn(t *testing.T) *agy.Adapter {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile), `{"gateway":{"enabled":true,"url":"https://gw.example.com"}}`)
	write(t, filepath.Join(a.SystemDir, agy.AdminFile+".aw-revision"), "r1\n")
	return a
}

func TestInspectWarnsWhenTheGatewayIsOnWithoutAnAPIKey(t *testing.T) {
	f := gatewayOn(t).Inspect([]string{quiet})
	if !findingsWith(f, agent.Warn, "agy sends gateway requests with no API key") {
		t.Errorf("findings %v", f)
	}
	f = gatewayOn(t).Inspect([]string{quiet, "AGY_LLM_GATEWAY_API_KEY=k"})
	if findingsWith(f, agent.Warn, "no API key") {
		t.Errorf("findings %v warn despite a key", f)
	}
}

func TestInspectNotesThatTheAdminFileOutranksGatewayEnv(t *testing.T) {
	f := gatewayOn(t).Inspect([]string{quiet, "AGY_LLM_GATEWAY_PROXY_URL=http://p"})
	if !findingsWith(f, agent.OK, "AGY_LLM_GATEWAY_PROXY_URL is set: the admin file outranks it") {
		t.Errorf("findings %v", f)
	}
	// The API key variable is the developer's own credential, not an override.
	f = gatewayOn(t).Inspect([]string{quiet, "AGY_LLM_GATEWAY_API_KEY=k"})
	if findingsWith(f, agent.OK, "outranks") {
		t.Errorf("findings %v note the API key as overridden", f)
	}
}
