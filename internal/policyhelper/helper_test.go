package policyhelper_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/policyhelper"
)

// compiledPolicy is what GET /v1/policy returns for the test subject.
const compiledPolicy = `{
  "version": "2026-09-21.1",
  "agents": {
    "claude": {
      "managed": {
        "permissions": {"deny": ["Read(./.env)"]},
        "allowManagedPermissionRulesOnly": true
      },
      "env": {"CLAUDE_CODE_ENABLE_TELEMETRY": "1"}
    }
  },
  "appliedRules": ["baseline"]
}`

// fakeControlPlane serves one compiled policy and can be broken between
// runs, which is how an outage after a good launch is simulated. The cache
// is keyed on the server URL, so the outage has to come from the same URL.
type fakeControlPlane struct {
	*httptest.Server
	status atomic.Int32 // 0 hangs until the request is cancelled
	body   atomic.Value // string
	hits   atomic.Int32
}

func newControlPlane(t *testing.T, status int, body string) *fakeControlPlane {
	t.Helper()
	cp := &fakeControlPlane{}
	cp.set(status, body)
	cp.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cp.hits.Add(1)
		status := int(cp.status.Load())
		if status == 0 {
			<-r.Context().Done()
			return
		}
		body := cp.body.Load().(string)
		etag := fmt.Sprintf("%q", fmt.Sprintf("%x", sha256.Sum256([]byte(body))))
		w.Header().Set("ETag", etag)
		if status == http.StatusOK && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(cp.Close)
	return cp
}

func (cp *fakeControlPlane) set(status int, body string) {
	cp.status.Store(int32(status))
	cp.body.Store(body)
}

// primed returns a config whose cache already holds compiledPolicy from cp.
func primed(t *testing.T, cp *fakeControlPlane) policyhelper.Config {
	t.Helper()
	cfg := config(t, cp.URL)
	if r := policyhelper.Run(context.Background(), cfg); r.Source != policyhelper.SourceServer {
		t.Fatalf("priming run: source = %q, notes = %q", r.Source, r.Notes)
	}
	return cfg
}

func config(t *testing.T, url string) policyhelper.Config {
	t.Helper()
	return policyhelper.Config{
		ServerURL: url,
		CacheDir:  t.TempDir(),
		Groups:    []string{"platform"},
		WorkDir:   t.TempDir(), // not a repository
		Timeout:   2 * time.Second,
	}
}

func envelope(t *testing.T, r policyhelper.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Output, &out); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, r.Output)
	}
	return out
}

func managedOf(t *testing.T, r policyhelper.Result) map[string]any {
	t.Helper()
	managed, ok := envelope(t, r)["managedSettings"].(map[string]any)
	if !ok {
		t.Fatalf("output has no managedSettings object:\n%s", r.Output)
	}
	return managed
}

func TestAFreshPolicyIsEmittedAsManagedSettings(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)

	r := policyhelper.Run(context.Background(), config(t, cp.URL))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if r.Source != policyhelper.SourceServer {
		t.Errorf("source = %q, want server", r.Source)
	}
	managed := managedOf(t, r)
	if managed["allowManagedPermissionRulesOnly"] != true {
		t.Errorf("managed settings lost a key: %v", managed)
	}
	env, _ := managed["env"].(map[string]any)
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("the policy's env should land in managedSettings.env, got %v", managed["env"])
	}
}

func TestTheSubjectIsSentToTheServer(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"v1"}`))
	}))
	t.Cleanup(srv.Close)
	cfg := config(t, srv.URL)
	cfg.Groups = []string{"platform", "security"}
	if err := os.MkdirAll(filepath.Join(cfg.WorkDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.WorkDir, ".git", "config"),
		[]byte("[remote \"origin\"]\n\turl = git@github.com:acme/payments-api.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policyhelper.Run(context.Background(), cfg)

	for _, want := range []string{"group=platform", "group=security", "repo=github.com%2Facme%2Fpayments-api"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q should contain %s", query, want)
		}
	}
}

func TestAServerErrorFallsBackToTheCachedPolicy(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := primed(t, cp)
	cp.set(http.StatusInternalServerError, "boom")

	r := policyhelper.Run(context.Background(), cfg)

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0: a server outage must not brick the agent", r.ExitCode)
	}
	if r.Source != policyhelper.SourceCache {
		t.Errorf("source = %q, want cache", r.Source)
	}
	if managedOf(t, r)["allowManagedPermissionRulesOnly"] != true {
		t.Error("the cached policy was not the one emitted")
	}
}

func TestATimeoutFallsBackToTheCachedPolicy(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := primed(t, cp)
	cp.set(0, "")
	cfg.Timeout = 100 * time.Millisecond

	start := time.Now()
	r := policyhelper.Run(context.Background(), cfg)

	if r.ExitCode != 0 || r.Source != policyhelper.SourceCache {
		t.Errorf("exit = %d source = %q, want 0 and cache", r.ExitCode, r.Source)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v; the helper must give up well inside Claude Code's own timeout", elapsed)
	}
}

func TestMalformedJSONFallsBackToTheCachedPolicy(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := primed(t, cp)
	cp.set(http.StatusOK, `{"version": `)

	r := policyhelper.Run(context.Background(), cfg)

	if r.ExitCode != 0 || r.Source != policyhelper.SourceCache {
		t.Errorf("exit = %d source = %q, want 0 and cache", r.ExitCode, r.Source)
	}
}

func TestASchemaViolationIsNeverEmitted(t *testing.T) {
	// The server is trusted for policy but not for Claude Code's schema:
	// a stale server, or a hand-edited store, must not produce an object
	// that makes every launch on the fleet refuse to start.
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := primed(t, cp)
	cp.set(http.StatusOK, `{"version":"v2","agents":{"claude":{"managed":{"permissions":{"deny":"Read(./.env)"}}}}}`)

	r := policyhelper.Run(context.Background(), cfg)

	if r.ExitCode != 0 || r.Source != policyhelper.SourceCache {
		t.Errorf("exit = %d source = %q, want 0 and cache", r.ExitCode, r.Source)
	}
	if deny, _ := managedOf(t, r)["permissions"].(map[string]any); deny["deny"] == "Read(./.env)" {
		t.Error("the invalid document was emitted")
	}
	if !strings.Contains(strings.Join(r.Notes, "\n"), "/permissions/deny") {
		t.Errorf("notes %q should say what was wrong", r.Notes)
	}
}

func TestWithNoCacheAndNoServerTheEnvelopeIsEmpty(t *testing.T) {
	// An envelope without managedSettings contributes nothing, so the
	// static managed-settings file the organization deployed still applies.
	// That is the safest thing a first launch during an outage can do.
	cp := newControlPlane(t, http.StatusInternalServerError, "boom")

	r := policyhelper.Run(context.Background(), config(t, cp.URL))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if r.Source != policyhelper.SourceNone {
		t.Errorf("source = %q, want none", r.Source)
	}
	if _, has := envelope(t, r)["managedSettings"]; has {
		t.Errorf("output should omit managedSettings entirely, got %s", r.Output)
	}
}

func TestRequireFreshFailsClosedWhenTheServerIsDown(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := primed(t, cp)
	cp.set(http.StatusInternalServerError, "boom")
	cfg.RequireFresh = true

	r := policyhelper.Run(context.Background(), cfg)

	if r.ExitCode == 0 {
		t.Error("exit = 0, want non-zero: the organization opted into refusing to start on stale policy")
	}
}

func TestNotModifiedServesTheCacheWithoutReparsing(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := primed(t, cp)

	r := policyhelper.Run(context.Background(), cfg)

	if cp.hits.Load() != 2 {
		t.Fatalf("server hits = %d, want 2", cp.hits.Load())
	}
	if r.Source != policyhelper.SourceServer {
		t.Errorf("source = %q, want server: a 304 confirms the cache is current", r.Source)
	}
	if managedOf(t, r)["allowManagedPermissionRulesOnly"] != true {
		t.Error("the confirmed cache was not emitted")
	}
}

func TestOutputStaysUnderClaudeCodesLimit(t *testing.T) {
	// Claude Code reads at most 1 MiB from stdout; more fails the run.
	big := strings.Repeat("x", 2<<20)
	cp := newControlPlane(t, http.StatusOK, `{"version":"v1","agents":{"claude":{"managed":{"model":"`+big+`"}}}}`)

	r := policyhelper.Run(context.Background(), config(t, cp.URL))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if len(r.Output) >= 1<<20 {
		t.Errorf("output is %d bytes; an oversized policy must degrade, not brick", len(r.Output))
	}
}

func TestTheCacheIsPerSubject(t *testing.T) {
	// Two repositories can compile to different policies. The cache for
	// one must never be served for the other.
	var served atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"v1","agents":{"claude":{"managed":{"model":"` + r.URL.Query().Get("repo") + `"}}}}`))
	}))
	t.Cleanup(srv.Close)
	cfg := config(t, srv.URL)
	cfg.Groups = nil
	cfg.RepoOverride = "github.com/acme/a"
	policyhelper.Run(context.Background(), cfg)
	srv.Close()

	cfg.RepoOverride = "github.com/acme/b"
	r := policyhelper.Run(context.Background(), cfg)

	if r.Source != policyhelper.SourceNone {
		t.Errorf("source = %q, want none: repository b has no cache of its own", r.Source)
	}
}

func TestEveryRunIsAudited(t *testing.T) {
	cp := newControlPlane(t, http.StatusOK, compiledPolicy)
	cfg := config(t, cp.URL)

	policyhelper.Run(context.Background(), cfg)

	raw, err := os.ReadFile(filepath.Join(cfg.CacheDir, "aw-policy.log"))
	if err != nil {
		t.Fatalf("no audit log: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &entry); err != nil {
		t.Fatalf("audit line is not JSON: %v\n%s", err, raw)
	}
	if entry["source"] != "server" || entry["version"] != "2026-09-21.1" {
		t.Errorf("audit entry = %v, want source and version", entry)
	}
}
