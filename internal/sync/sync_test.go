package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/sync"
)

// fakeAwd serves one bundle with an ETag and honours If-None-Match. Its
// status can be forced to simulate an outage or a revocation.
type fakeAwd struct {
	srv    *httptest.Server
	bundle atomic.Value // *policy.Bundle
	status atomic.Int32 // 0 means behave normally
	hits   atomic.Int32
}

func newFakeAwd(t *testing.T, b *policy.Bundle) *fakeAwd {
	t.Helper()
	f := &fakeAwd{}
	f.bundle.Store(b)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer cred" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if s := f.status.Load(); s != 0 {
			http.Error(w, `{"error":"forced"}`, int(s))
			return
		}
		body, _ := json.Marshal(f.bundle.Load().(*policy.Bundle))
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func testBundle() *policy.Bundle {
	return &policy.Bundle{Version: "v1", User: "alice@acme.com", Groups: []string{"platform"}, Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}}
}

// enrolled sets up a state directory enrolled against f for the given
// agents and returns a Config pointing rendered files at a temp root.
func enrolled(t *testing.T, f *fakeAwd, reg *agent.Registry, agents ...string) (sync.Config, string) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := sync.SaveMachine(stateDir, sync.Machine{Server: f.srv.URL, MachineID: "m1", Credential: "cred", Agents: agents}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "claude-root")
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	return sync.Config{
		StateDir: stateDir,
		GOOS:     "linux",
		Roots:    map[string]string{"claude": root, "broken": filepath.Join(t.TempDir(), "broken-root")},
		Registry: reg,
		Now:      func() time.Time { return now },
	}, root
}

func claudeRegistry(t *testing.T, extra ...agent.Adapter) *agent.Registry {
	t.Helper()
	reg := &agent.Registry{}
	if err := reg.Register(&claude.Adapter{GOOS: "linux"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range extra {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// brokenRenderer is an adapter whose renderer always fails, to prove that
// one agent's failure keeps every agent's files unwritten.
type brokenRenderer struct{}

func (brokenRenderer) Name() string                    { return "broken" }
func (brokenRenderer) Locate([]string) (string, error) { return "", errors.New("not installed") }
func (brokenRenderer) Build(context.Context, agent.BuildOptions) (*agent.Launch, error) {
	return nil, errors.New("not buildable")
}
func (brokenRenderer) Render(*policy.Bundle) (agent.Rendering, error) {
	return agent.Rendering{}, errors.New("broken: cannot render")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunWritesEveryRenderedFileAndRecordsState(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Unchanged || res.Version != "v1" || len(res.Written) != 2 {
		t.Errorf("result = %+v", res)
	}

	bundlePath := filepath.Join(root, claude.BundleFile)
	dropInPath := filepath.Join(root, claude.DropInFile)
	var written policy.Bundle
	if err := json.Unmarshal(mustRead(t, bundlePath), &written); err != nil {
		t.Fatal(err)
	}
	if written.Version != "v1" || len(written.Rules) != 1 {
		t.Errorf("bundle on disk = %+v", written)
	}
	if !strings.Contains(string(mustRead(t, dropInPath)), "policyHelper") {
		t.Error("drop-in not written")
	}
	for _, p := range []string{bundlePath, dropInPath} {
		if got := string(mustRead(t, p+".aw-revision")); got != "v1\n" {
			t.Errorf("%s.aw-revision = %q, want \"v1\\n\"", p, got)
		}
	}

	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != "v1" || state.ETag == "" || state.Error != "" || !state.SyncedAt.Equal(cfg.Now()) {
		t.Errorf("state = %+v", state)
	}
	sum := sha256.Sum256(mustRead(t, bundlePath))
	if state.Files[bundlePath] != hex.EncodeToString(sum[:]) {
		t.Errorf("state.Files[%s] = %q, want the file's sha256", bundlePath, state.Files[bundlePath])
	}
	if _, ok := state.Files[dropInPath]; !ok {
		t.Error("the drop-in is not in state.Files")
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, sync.AuditFile)); err != nil {
		t.Errorf("no audit log: %v", err)
	}
}

func TestRunIsANoOpOn304(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	before := mustRead(t, filepath.Join(root, claude.BundleFile))
	first, _ := sync.LoadState(cfg.StateDir)

	later := time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return later }
	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !res.Unchanged || len(res.Written) != 0 || res.Version != "v1" {
		t.Errorf("result = %+v, want Unchanged with nothing written", res)
	}
	if string(mustRead(t, filepath.Join(root, claude.BundleFile))) != string(before) {
		t.Error("the bundle was rewritten on a 304")
	}
	second, _ := sync.LoadState(cfg.StateDir)
	if second.ETag != first.ETag || second.Version != first.Version || len(second.Files) != len(first.Files) {
		t.Errorf("state changed on 304: before %+v after %+v", first, second)
	}
	if !second.SyncedAt.Equal(later) {
		t.Errorf("syncedAt = %v, want %v: a 304 is a successful cycle", second.SyncedAt, later)
	}
}

func TestRunKeepsFilesAndStateWhenTheServerFails(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	before := mustRead(t, filepath.Join(root, claude.BundleFile))
	good, _ := sync.LoadState(cfg.StateDir)

	f.status.Store(http.StatusInternalServerError)
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error when the control plane fails")
	}
	if string(mustRead(t, filepath.Join(root, claude.BundleFile))) != string(before) {
		t.Error("a failed fetch changed the bundle on disk")
	}
	if _, err := os.Stat(filepath.Join(root, claude.DropInFile)); err != nil {
		t.Error("a failed fetch removed the drop-in")
	}
	after, _ := sync.LoadState(cfg.StateDir)
	if after.Error == "" || !strings.Contains(after.Error, "500") {
		t.Errorf("state.Error = %q, want the failure recorded", after.Error)
	}
	if after.ETag != good.ETag || after.Version != good.Version || len(after.Files) != len(good.Files) {
		t.Errorf("state lost its last good record: %+v", after)
	}
	if !after.SyncedAt.Equal(good.SyncedAt) {
		t.Error("syncedAt moved on a failed cycle")
	}
}

func TestRunRecordsWhatItWroteWhenALaterWriteFails(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}

	// Block the drop-in's directory with a regular file, so its write
	// fails after the bundle's write has already landed on disk.
	dropInDir := filepath.Join(root, "managed-settings.d")
	if err := os.RemoveAll(dropInDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropInDir, []byte("blocking"), 0o644); err != nil {
		t.Fatal(err)
	}

	v2 := testBundle()
	v2.Version = "v2"
	f.bundle.Store(v2)

	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error when a later write fails")
	}

	bundlePath := filepath.Join(root, claude.BundleFile)
	found := false
	for _, w := range res.Written {
		if w == bundlePath {
			found = true
		}
	}
	if !found {
		t.Errorf("Written = %v, want it to include %s", res.Written, bundlePath)
	}

	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(mustRead(t, bundlePath))
	if state.Files[bundlePath] != hex.EncodeToString(sum[:]) {
		t.Errorf("state.Files[%s] = %q, want the sha256 of what is on disk now", bundlePath, state.Files[bundlePath])
	}
	if state.Version != "v1" {
		t.Errorf("state.Version = %q, want the last completed cycle's version", state.Version)
	}
	if state.Error == "" {
		t.Error("state.Error is empty, want the write failure recorded")
	}
}

func TestRunKeepsFilesWhenTheServerIsUnreachable(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	f.srv.Close()
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error when the control plane is down")
	}
	if _, err := os.Stat(filepath.Join(root, claude.BundleFile)); err != nil {
		t.Error("an outage removed the bundle")
	}
}

func TestRunReportsARevokedCredential(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")
	f.status.Store(http.StatusUnauthorized)
	res := sync.Run(context.Background(), cfg)
	if !errors.Is(res.Err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", res.Err)
	}
}

func TestRunWritesNothingForAnyAgentWhenOneRendererFails(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t, brokenRenderer{}), "claude", "broken")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "broken") {
		t.Fatalf("err = %v, want the broken renderer's error", res.Err)
	}
	if _, err := os.Stat(filepath.Join(root, claude.BundleFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("Claude's bundle was written although another agent failed to render")
	}
	state, _ := sync.LoadState(cfg.StateDir)
	if state.Error == "" || state.Version != "" {
		t.Errorf("state = %+v, want the error recorded and no version", state)
	}
}

func TestRunWritesNothingWhenTheBundleFailsValidation(t *testing.T) {
	bad := testBundle()
	bad.Rules[0].Agents["claude"] = policy.AgentConfig{Managed: map[string]any{"permissions": "nope"}}
	f := newFakeAwd(t, bad)
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error for a bundle the schema rejects")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("root has %d entries, want none", len(entries))
	}
}

func TestRunRequiresEnrollment(t *testing.T) {
	cfg := sync.Config{StateDir: t.TempDir(), GOOS: "linux", Registry: claudeRegistry(t)}
	res := sync.Run(context.Background(), cfg)
	if !errors.Is(res.Err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want ErrNotEnrolled", res.Err)
	}
}

func TestRunRejectsAnAgentWithoutARenderer(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "codex")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "codex") {
		t.Errorf("err = %v, want one naming codex", res.Err)
	}
}

func TestRunCarriesRendererNotesIntoState(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t, notingRenderer{}), "claude", "noting")
	cfg.Roots["noting"] = filepath.Join(t.TempDir(), "noting-root")
	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	state, _ := sync.LoadState(cfg.StateDir)
	if len(state.Notes) != 1 || !strings.Contains(state.Notes[0], "not enforceable") {
		t.Errorf("notes = %v", state.Notes)
	}
}

// notingRenderer renders one file and reports a note, standing in for the
// Codex and Gemini renderers that drop repo-scoped rules.
type notingRenderer struct{}

func (notingRenderer) Name() string                    { return "noting" }
func (notingRenderer) Locate([]string) (string, error) { return "", errors.New("not installed") }
func (notingRenderer) Build(context.Context, agent.BuildOptions) (*agent.Launch, error) {
	return nil, errors.New("not buildable")
}
func (notingRenderer) Render(*policy.Bundle) (agent.Rendering, error) {
	return agent.Rendering{
		Files: []agent.File{{Path: "requirements.toml", Content: []byte("# ok\n"), Mode: 0o644}},
		Notes: []string{"1 repo-scoped rule is not enforceable for noting"},
	}, nil
}
