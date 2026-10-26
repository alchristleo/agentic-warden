package sync_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
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

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/agent/claude"
	"github.com/alchristleo/agentic-warden/internal/handler"
	"github.com/alchristleo/agentic-warden/internal/model"
	"github.com/alchristleo/agentic-warden/internal/policy"
	"github.com/alchristleo/agentic-warden/internal/signing"
	"github.com/alchristleo/agentic-warden/internal/store"
	"github.com/alchristleo/agentic-warden/internal/sync"
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

// newSignedFakeAwd serves body signed by signer, checking the bearer
// credential the way the real control plane does. When announcer is
// non-nil, the response also carries a rollover statement announcing
// signer's key, signed by announcer — the shape a control plane mid-
// rotation sends.
func newSignedFakeAwd(t *testing.T, credential string, body []byte, signer ed25519.PrivateKey, announcer ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-AW-Signature", signing.Sign(signer, body))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(signer.Public().(ed25519.PublicKey)))
		if announcer != nil {
			header, _ := signing.SignRollover(r.Context(), signing.NewSeedSigner(announcer), signer.Public().(ed25519.PublicKey))
			w.Header().Set("X-AW-Key-Rollover", header)
		}
		w.Header().Set("ETag", `"e1"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// enrolledSigned is enrolled's counterpart for a machine that pins a signing
// key, so a signed test does not have to rebuild machine.json by hand.
func enrolledSigned(t *testing.T, srv *httptest.Server, reg *agent.Registry, pinned ed25519.PublicKey, agents ...string) (sync.Config, string) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	m := sync.Machine{Server: srv.URL, MachineID: "m1", Credential: "cred", Agents: agents}
	if pinned != nil {
		m.PublicKey = signing.FormatPublic(pinned)
		m.KeyID = signing.KeyID(pinned)
	}
	if err := sync.SaveMachine(stateDir, m); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "claude-root")
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	return sync.Config{
		StateDir: stateDir,
		GOOS:     "linux",
		Roots:    map[string]string{"claude": root},
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
	if res.Unchanged || res.Version != "v1" || len(res.Written) != 3 {
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

func TestRunRepairsDriftEvenWhenTheETagStillMatches(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}

	bundlePath := filepath.Join(root, claude.BundleFile)
	dropInPath := filepath.Join(root, claude.DropInFile)
	if err := os.WriteFile(bundlePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dropInPath); err != nil {
		t.Fatal(err)
	}

	// The bundle on the server has not changed, so a naive conditional fetch
	// would get a 304 and leave the tampered/missing files as they are.
	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Unchanged {
		t.Error("Run reported Unchanged although the files on disk had drifted")
	}
	if len(res.Written) != 3 {
		t.Errorf("Written = %v, want the two agent files and the state-dir bundle rewritten", res.Written)
	}

	var written policy.Bundle
	if err := json.Unmarshal(mustRead(t, bundlePath), &written); err != nil {
		t.Fatal(err)
	}
	if written.Version != "v1" {
		t.Errorf("bundle on disk = %+v, want the repaired render", written)
	}
	if _, err := os.Stat(dropInPath); err != nil {
		t.Errorf("the drop-in was not rewritten: %v", err)
	}

	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "drift detected") {
			found = true
		}
	}
	if !found {
		t.Errorf("notes = %v, want a note about the detected drift", res.Notes)
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

// escapingRenderer returns a file path that escapes the agent's root, to
// prove Run refuses to write outside it.
type escapingRenderer struct{}

func (escapingRenderer) Name() string                    { return "escaping" }
func (escapingRenderer) Locate([]string) (string, error) { return "", errors.New("not installed") }
func (escapingRenderer) Build(context.Context, agent.BuildOptions) (*agent.Launch, error) {
	return nil, errors.New("not buildable")
}
func (escapingRenderer) Render(*policy.Bundle) (agent.Rendering, error) {
	return agent.Rendering{Files: []agent.File{{Path: "../escape.json", Content: []byte("{}"), Mode: 0o644}}}, nil
}

func TestRunRejectsAnUnsafeRendererPath(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t, escapingRenderer{}), "escaping")
	cfg.Roots["escaping"] = filepath.Join(t.TempDir(), "escaping-root")
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "unsafe path") {
		t.Fatalf("err = %v, want an unsafe path error", res.Err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("root has %d entries, want none", len(entries))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Roots["escaping"]), "escape.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the escaping path was written outside the agent root")
	}
}

func TestRunWithoutARegistryFailsCleanly(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")
	cfg.Registry = nil
	res := sync.Run(context.Background(), cfg)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "no adapter registry") {
		t.Errorf("err = %v, want a clean error naming the missing registry", res.Err)
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

func TestRunDoesNotAccumulateNotesInStateAcrossFailures(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	// notingRenderer is listed before brokenRenderer so it renders (and
	// reports a note) before the cycle fails on the broken one.
	cfg, _ := enrolled(t, f, claudeRegistry(t, notingRenderer{}, brokenRenderer{}), "noting", "broken")
	cfg.Roots["noting"] = filepath.Join(t.TempDir(), "noting-root")

	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error: the broken renderer always fails")
	}
	first, _ := sync.LoadState(cfg.StateDir)

	res = sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error on the second cycle too")
	}
	second, _ := sync.LoadState(cfg.StateDir)

	if len(second.Notes) != len(first.Notes) {
		t.Errorf("state.Notes grew across failing cycles: first %v, second %v", first.Notes, second.Notes)
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "not enforceable") {
			found = true
		}
	}
	if !found {
		t.Errorf("Result.Notes = %v, want this cycle's renderer note reported", res.Notes)
	}
}

// notingRenderer renders one file and reports a note, standing in for the
// Codex renderer that drops repo-scoped rules.
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

func TestRunLeavesTheFullBundleInTheStateDirectory(t *testing.T) {
	// aw compiles per repository from this copy, so a machine that enrols
	// no Claude still has the bundle. It is a planned file like the rest:
	// hashed into state, so drift on it is repaired too.
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	path := filepath.Join(cfg.StateDir, sync.BundleFile)
	var written policy.Bundle
	if err := json.Unmarshal(mustRead(t, path), &written); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if written.Version != "v1" || len(written.Rules) != 1 {
		t.Errorf("bundle on disk = %+v", written)
	}
	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Files[path]; !ok {
		t.Errorf("state.Files lacks %s", path)
	}
	if len(res.Written) != 3 {
		t.Errorf("Written = %v; want the two Claude files and the state-dir bundle", res.Written)
	}
}

func TestRunWritesTheServedBytesAndTheSignature(t *testing.T) {
	// The served body is deliberately not what json.MarshalIndent would
	// produce (compact, no trailing newline); the point of the test is that
	// the signature still verifies against exactly what landed on disk.
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"6","user":"a@b.c","rules":[]}`)
	srv := newSignedFakeAwd(t, "cred", body, key, nil)
	cfg, _ := enrolledSigned(t, srv, claudeRegistry(t), pub, "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	onDisk, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.BundleFile))
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	if !bytes.Equal(onDisk, body) {
		t.Fatalf("the bundle on disk is a re-encoding, not the served bytes:\n%s", onDisk)
	}
	sig, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.SignatureFile))
	if err != nil {
		t.Fatalf("reading the signature: %v", err)
	}
	fields := strings.Fields(string(sig))
	if len(fields) != 3 || fields[0] != "aw-ed25519" {
		t.Fatalf("signature file = %q", sig)
	}
	if !signing.Verify(pub, onDisk, fields[2]) {
		t.Fatal("the signature on disk does not verify over the bundle on disk")
	}
	trust, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.TrustFile))
	if err != nil {
		t.Fatalf("reading the trust key: %v", err)
	}
	if strings.TrimSpace(string(trust)) != signing.FormatPublic(pub) {
		t.Fatalf("trust file = %q", trust)
	}
	state, _ := sync.LoadState(cfg.StateDir)
	for _, name := range []string{sync.BundleFile, sync.SignatureFile, sync.TrustFile} {
		if _, ok := state.Files[filepath.Join(cfg.StateDir, name)]; !ok {
			t.Errorf("%s is not recorded in state.json, so drift in it goes unnoticed", name)
		}
	}
}

func TestRunWritesNothingWhenVerificationFails(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"6","user":"a@b.c","rules":[]}`)
	// Signed by a key nobody pinned: the pinned key below cannot verify it.
	srv := newSignedFakeAwd(t, "cred", body, wrong, nil)
	cfg, root := enrolledSigned(t, srv, claudeRegistry(t), pub, "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err == nil {
		t.Fatal("want an error when the bundle is not signed by the pinned key")
	}
	if _, err := os.Stat(filepath.Join(root, claude.BundleFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("an agent file was written despite failed verification")
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, sync.BundleFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("the bundle file was written despite failed verification")
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, sync.SignatureFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("the signature file was written despite failed verification")
	}
}

func TestRunRepinsOnARollover(t *testing.T) {
	old, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	next, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	oldPub := old.Public().(ed25519.PublicKey)
	nextPub := next.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"7","user":"a@b.c","rules":[]}`)
	// Signs with the new key, announces it via the one already pinned.
	srv := newSignedFakeAwd(t, "cred", body, next, old)
	cfg, _ := enrolledSigned(t, srv, claudeRegistry(t), oldPub, "claude")

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	machine, err := sync.LoadMachine(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if machine.PublicKey != signing.FormatPublic(nextPub) || machine.KeyID != signing.KeyID(nextPub) {
		t.Errorf("machine = %+v, want repinned to the new key", machine)
	}
	trust, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.TrustFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(trust)) != signing.FormatPublic(nextPub) {
		t.Fatalf("trust file = %q, want the new key", trust)
	}
}

func TestRunWritesAV2SignatureLine(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	_, srv, e := realSignedServer(t, &handler.Signer{Current: signing.NewSeedSigner(key)})
	pub := key.Public().(ed25519.PublicKey)

	stateDir := filepath.Join(t.TempDir(), "state")
	m := sync.Machine{Server: srv.URL, MachineID: e.MachineID, Credential: e.Credential, Agents: []string{"claude"}, PublicKey: e.PublicKey, KeyID: e.KeyID}
	if err := sync.SaveMachine(stateDir, m); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "claude-root")
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cfg := sync.Config{
		StateDir: stateDir,
		GOOS:     "linux",
		Roots:    map[string]string{"claude": root},
		Registry: claudeRegistry(t),
		Now:      func() time.Time { return now },
	}

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	sig, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.SignatureFile))
	if err != nil {
		t.Fatalf("reading the signature: %v", err)
	}
	wantPrefix := "aw-ed25519-v2 " + signing.KeyID(pub) + " "
	if !strings.HasPrefix(string(sig), wantPrefix) {
		t.Fatalf("signature file = %q, want it to start with %q", sig, wantPrefix)
	}

	_, format, _, trustMissing, err := signing.VerifyFiles(
		filepath.Join(cfg.StateDir, sync.TrustFile),
		filepath.Join(cfg.StateDir, sync.BundleFile),
		filepath.Join(cfg.StateDir, sync.SignatureFile),
	)
	if err != nil || trustMissing {
		t.Fatalf("VerifyFiles: format=%q trustMissing=%v err=%v", format, trustMissing, err)
	}
	if format != signing.FormatV2 {
		t.Fatalf("format = %q, want v2", format)
	}
}

// Review Focus 4.
func TestUpgradedClientKeepsAV1SignatureOnA304(t *testing.T) {
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"6","user":"a@b.c","rules":[]}`)

	const etag = `"e1"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)

	cfg, _ := enrolledSigned(t, srv, claudeRegistry(t), pub, "claude")

	// Seed the state dir the way an old aw-sync, from before v2 existed,
	// would have left it: a v1 .sig line, the trust key, the bundle it
	// signs over, and an etag matching what the server answers, so this
	// cycle is a 304 that touches none of them.
	if err := os.WriteFile(filepath.Join(cfg.StateDir, sync.BundleFile), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, sync.TrustFile), []byte(signing.FormatPublic(pub)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sigLine := signing.SignatureLine(signing.FormatV1, signing.KeyID(pub), signing.Sign(key, body))
	if err := os.WriteFile(filepath.Join(cfg.StateDir, sync.SignatureFile), []byte(sigLine), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	state.ETag = etag
	if err := sync.SaveState(cfg.StateDir, state); err != nil {
		t.Fatal(err)
	}

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !res.Unchanged {
		t.Fatalf("result = %+v, want Unchanged: the server answered 304", res)
	}

	after, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.SignatureFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != sigLine {
		t.Fatalf(".sig file changed on a 304:\nbefore %q\nafter  %q", sigLine, after)
	}

	_, format, _, trustMissing, err := signing.VerifyFiles(
		filepath.Join(cfg.StateDir, sync.TrustFile),
		filepath.Join(cfg.StateDir, sync.BundleFile),
		filepath.Join(cfg.StateDir, sync.SignatureFile),
	)
	if err != nil || trustMissing {
		t.Fatalf("VerifyFiles: format=%q trustMissing=%v err=%v", format, trustMissing, err)
	}
	if format != signing.FormatV1 {
		t.Fatalf("format = %q, want v1: an upgraded client must still read an old client's signature", format)
	}
}

func TestUnsignedDeploymentWritesNeitherNewFile(t *testing.T) {
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude") // machine.json pins no key

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, sync.SignatureFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("an unsigned deployment wrote a signature file")
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, sync.TrustFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("an unsigned deployment wrote a trust file")
	}
	body, err := json.Marshal(testBundle())
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.BundleFile))
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	if !bytes.Equal(onDisk, body) {
		t.Errorf("bundle on disk = %s, want the served bytes verbatim", onDisk)
	}
}

func TestRunRepinsOnARolloverCarriedByA304(t *testing.T) {
	// The fleet this matters on is the ordinary one: policy that does not
	// change, so every cycle is a 304. A rotation that only moved the pin on
	// changed bundle bytes would never finish here, and the operator who
	// then retires the previous key freezes every machine still on it.
	old, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	next, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	oldPub := old.Public().(ed25519.PublicKey)
	nextPub := next.Public().(ed25519.PublicKey)
	body := []byte(`{"version":"8","user":"a@b.c","rules":[]}`)
	var rotating atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const etag = `"e1"`
		w.Header().Set("ETag", etag)
		if rotating.Load() {
			header, _ := signing.SignRollover(r.Context(), signing.NewSeedSigner(old), nextPub)
			w.Header().Set("X-AW-Key-Rollover", header)
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("X-AW-Signature", signing.Sign(old, body))
		w.Header().Set("X-AW-Key-Id", signing.KeyID(oldPub))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	cfg, _ := enrolledSigned(t, srv, claudeRegistry(t), oldPub, "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatalf("the first cycle failed: %v", res.Err)
	}

	rotating.Store(true)
	res := sync.Run(context.Background(), cfg)

	if res.Err != nil {
		t.Fatalf("the rotating cycle failed: %v", res.Err)
	}
	if !res.Unchanged {
		t.Fatalf("the second cycle was not a 304: %+v", res)
	}
	machine, err := sync.LoadMachine(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if machine.PublicKey != signing.FormatPublic(nextPub) || machine.KeyID != signing.KeyID(nextPub) {
		t.Errorf("machine = %+v; a rollover on a 304 must move the pin, or the rotation never finishes", machine)
	}
	// The bundle on disk is still the one the outgoing key signed, so the
	// trust file beside it must still name that key: rewriting it here would
	// break the check it exists for. The next changed bundle rewrites both.
	trust, err := os.ReadFile(filepath.Join(cfg.StateDir, sync.TrustFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(trust)) != signing.FormatPublic(oldPub) {
		t.Errorf("trust file = %q; a 304 wrote no new bundle, so the key its signature was made with must stay", trust)
	}
}

func TestRunRemovesTheSigningFilesWhenNoKeyIsPinned(t *testing.T) {
	// Signing turned off on the control plane, or a machine re-enrolled
	// against one that never signed: machine.json pins nothing, a fresh
	// bundle is written, and the trust key and signature left over from
	// before would otherwise make aw-policy fail every session forever.
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	sigPath := filepath.Join(cfg.StateDir, sync.SignatureFile)
	trustPath := filepath.Join(cfg.StateDir, sync.TrustFile)
	writeFile(t, trustPath, signing.FormatPublic(pub)+"\n")
	writeFile(t, sigPath, "aw-ed25519 "+signing.KeyID(pub)+" "+signing.Sign(key, []byte("some older bundle"))+"\n")

	res := sync.Run(context.Background(), cfg)

	if res.Err != nil {
		t.Fatalf("the cycle failed: %v", res.Err)
	}
	for _, path := range []string{sigPath, trustPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived a cycle on a machine that pins no key: aw-policy will keep checking against it", path)
		}
	}
	state, err := sync.LoadState(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{sigPath, trustPath} {
		if _, ok := state.Files[path]; ok {
			t.Errorf("state.json still lists %s, so the next cycle reports it as drift forever", path)
		}
	}
}

func TestRunRemovesTheSigningFilesEvenWhenTheBundleIsUnchanged(t *testing.T) {
	// The removal has to survive a server that would answer 304. On a fleet
	// whose policy is stable that is every cycle, so a removal that only
	// happened on changed bytes would never happen at all.
	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatalf("the first cycle failed: %v", res.Err)
	}
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	trustPath := filepath.Join(cfg.StateDir, sync.TrustFile)
	writeFile(t, trustPath, signing.FormatPublic(key.Public().(ed25519.PublicKey))+"\n")

	res := sync.Run(context.Background(), cfg)

	if res.Err != nil {
		t.Fatalf("the second cycle failed: %v", res.Err)
	}
	if _, err := os.Stat(trustPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s survived a cycle the server would have answered 304", trustPath)
	}
}

// writeFile puts body at path, creating the directory, for tests that set up
// leftovers an earlier deployment would have written.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// revisedFixture is a real handler behind a proxy that can replay a recorded
// response or serve a chosen body, so a test can act as the network attacker
// who hands a pinned machine an older (still validly signed) bundle.
type revisedFixture struct {
	h      *handler.Handler
	srv    *httptest.Server
	cfg    sync.Config
	now    time.Time
	last   *httptest.ResponseRecorder
	replay *httptest.ResponseRecorder
	key    ed25519.PrivateKey
	body   []byte // when set, served signed with key instead of the handler's answer
}

func newRevisedFixture(t *testing.T) *revisedFixture {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	f := &revisedFixture{key: key, now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	f.h = handler.New(store.NewMemory(), nil)
	f.h.AdminToken = testAdminToken
	f.h.Signer = &handler.Signer{Current: signing.NewSeedSigner(key)}
	f.h.Now = func() time.Time { return f.now }
	routes := f.h.Routes()
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bundle" {
			routes.ServeHTTP(w, r)
			return
		}
		switch {
		case f.body != nil:
			w.Header().Set("X-AW-Signature", signing.Sign(key, f.body))
			w.Header().Set("X-AW-Key-Id", signing.KeyID(key.Public().(ed25519.PublicKey)))
			w.Header().Set("ETag", `"custom"`)
			_, _ = w.Write(f.body)
		case f.replay != nil:
			for k, v := range f.replay.Header() {
				w.Header()[k] = v
			}
			_, _ = w.Write(f.replay.Body.Bytes())
		default:
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, r)
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			if rec.Code == http.StatusOK {
				f.last = rec
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	e := mintAndEnroll(t, f.srv, testAdminToken, "alice@acme.com")
	stateDir := filepath.Join(t.TempDir(), "state")
	m := sync.Machine{Server: f.srv.URL, MachineID: e.MachineID, Credential: e.Credential, Agents: []string{"claude"}, PublicKey: e.PublicKey, KeyID: e.KeyID}
	if err := sync.SaveMachine(stateDir, m); err != nil {
		t.Fatal(err)
	}
	f.cfg = sync.Config{
		StateDir: stateDir,
		GOOS:     "linux",
		Roots:    map[string]string{"claude": filepath.Join(t.TempDir(), "claude-root")},
		Registry: claudeRegistry(t),
		Now:      func() time.Time { return f.now },
	}
	return f
}

// apply stores a revision at the fixture's current clock.
func (f *revisedFixture) apply(t *testing.T, version string) {
	t.Helper()
	body := `{"version":"` + version + `","rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/v1/policy/revisions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("applying %s: status %d", version, resp.StatusCode)
	}
}

func (f *revisedFixture) revisedAt(t *testing.T) int64 {
	t.Helper()
	state, err := sync.LoadState(f.cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	return state.RevisedAt
}

const (
	revisedT1 = int64(1789905600000) // 2026-09-20T12:00:00Z
	revisedT2 = revisedT1 + 60000
)

// twoRevisions leaves the machine synced to the second revision, with the
// first revision's signed response recorded for replay.
func (f *revisedFixture) twoRevisions(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	f.apply(t, "v1")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	old := f.last
	f.now = f.now.Add(time.Minute)
	f.apply(t, "v2")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := f.revisedAt(t); got != revisedT2 {
		t.Fatalf("state.RevisedAt = %d, want %d", got, revisedT2)
	}
	return old
}

func TestRunRefusesAnOlderRevision(t *testing.T) {
	f := newRevisedFixture(t)
	old := f.twoRevisions(t)
	before := f.snapshot(t)

	f.replay = old
	res := sync.Run(context.Background(), f.cfg)

	if res.Err == nil || !strings.Contains(res.Err.Error(), "policy revision time 2026-09-20T12:00:00Z is older than last rendered 2026-09-20T12:01:00Z; apply the policy again on the control plane") {
		t.Fatalf("err = %v, want the older-revision refusal", res.Err)
	}
	after := f.snapshot(t)
	for path, content := range before {
		if got, ok := after[path]; !ok || got != content {
			t.Errorf("%s changed or vanished after the refusal", path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("%s appeared after the refusal", path)
		}
	}
	if got := f.revisedAt(t); got != revisedT2 {
		t.Errorf("state.RevisedAt = %d, want it unchanged at %d", got, revisedT2)
	}
}

// snapshot reads every file under the state directory and the rendered root.
// state.json and the audit log are left out: a refused cycle records its
// error in them by design.
func (f *revisedFixture) snapshot(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{f.cfg.StateDir, f.cfg.Roots["claude"]} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() == sync.StateFile || d.Name() == sync.AuditFile {
				return err
			}
			raw, err := os.ReadFile(path)
			out[path] = string(raw)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// An unpinned machine has no signature to trust, so the revision time is not
// checked: an older bundle is rendered. The stored value is still max(), so
// it stays at the higher time it already held.
func TestRunDoesNotCheckRevisedAtWithoutAPinnedKey(t *testing.T) {
	f := newRevisedFixture(t)
	old := f.twoRevisions(t)
	machine, err := sync.LoadMachine(f.cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	machine.PublicKey, machine.KeyID = "", ""
	if err := sync.SaveMachine(f.cfg.StateDir, machine); err != nil {
		t.Fatal(err)
	}

	f.replay = old
	res := sync.Run(context.Background(), f.cfg)

	if res.Err != nil {
		t.Fatalf("an unpinned machine refused an older revision: %v", res.Err)
	}
	if res.Version != "v1" {
		t.Errorf("version = %q, want the older v1 rendered", res.Version)
	}
	if got := f.revisedAt(t); got != revisedT2 {
		t.Errorf("state.RevisedAt = %d, want max() to keep %d", got, revisedT2)
	}
}

func TestRunAcceptsSameOrNewerRevision(t *testing.T) {
	f := newRevisedFixture(t)
	f.apply(t, "v1")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	same := f.last

	// The same revision served again; the replayed 200 is rendered as is.
	f.replay = same
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatalf("an equal revision time was refused: %v", res.Err)
	}
	if got := f.revisedAt(t); got != revisedT1 {
		t.Errorf("state.RevisedAt = %d, want %d", got, revisedT1)
	}

	f.replay = nil
	f.now = f.now.Add(time.Minute)
	f.apply(t, "v2")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatalf("a newer revision was refused: %v", res.Err)
	}
	if got := f.revisedAt(t); got != revisedT2 {
		t.Errorf("state.RevisedAt = %d, want %d", got, revisedT2)
	}
}

func TestRunRefusesMissingRevisedAtOnceStored(t *testing.T) {
	f := newRevisedFixture(t)
	f.apply(t, "v1")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatal(res.Err)
	}

	// What an older control plane would sign: a bundle with no revisedAt.
	f.body = []byte(`{"version":"v0","user":"alice@acme.com","groups":[],"rules":[]}`)
	res := sync.Run(context.Background(), f.cfg)

	if res.Err == nil || !strings.Contains(res.Err.Error(), "policy revision time (none) is older than last rendered 2026-09-20T12:00:00Z") {
		t.Fatalf("err = %v, want the older-revision refusal", res.Err)
	}
	if got := f.revisedAt(t); got != revisedT1 {
		t.Errorf("state.RevisedAt = %d, want %d", got, revisedT1)
	}
}

func TestRolloverKeepsRevisedAt(t *testing.T) {
	f := newRevisedFixture(t)
	f.apply(t, "v1")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatal(res.Err)
	}

	next, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	f.h.Signer = &handler.Signer{Current: signing.NewSeedSigner(next), Previous: signing.NewSeedSigner(f.key)}

	// A 304 cycle that repins keeps the stored value ...
	res := sync.Run(context.Background(), f.cfg)
	if res.Err != nil || !res.Unchanged {
		t.Fatalf("res = %+v, want an unchanged cycle", res)
	}
	machine, err := sync.LoadMachine(f.cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if machine.KeyID != signing.KeyID(next.Public().(ed25519.PublicKey)) {
		t.Fatalf("machine.KeyID = %q, want it repinned", machine.KeyID)
	}
	if got := f.revisedAt(t); got != revisedT1 {
		t.Errorf("after the 304 rollover state.RevisedAt = %d, want %d", got, revisedT1)
	}

	// ... and so does one that renders a newer revision under the new key.
	f.h.Signer = &handler.Signer{Current: signing.NewSeedSigner(next)}
	f.now = f.now.Add(time.Minute)
	f.apply(t, "v2")
	if res := sync.Run(context.Background(), f.cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := f.revisedAt(t); got != revisedT2 {
		t.Errorf("after the rendering cycle state.RevisedAt = %d, want %d", got, revisedT2)
	}
}
