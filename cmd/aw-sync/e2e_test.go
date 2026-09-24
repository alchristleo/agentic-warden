package main_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

var (
	builtSync   string
	builtAwd    string
	builtPolicy string
)

const adminToken = "e2e-admin-token"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-sync-e2e-*")
	if err != nil {
		panic(err)
	}
	builtSync = filepath.Join(dir, "aw-sync")
	builtAwd = filepath.Join(dir, "awd")
	if out, err := exec.Command("go", "build", "-o", builtSync, ".").CombinedOutput(); err != nil {
		panic("building aw-sync: " + err.Error() + "\n" + string(out))
	}
	if out, err := exec.Command("go", "build", "-o", builtAwd, "../awd").CombinedOutput(); err != nil {
		panic("building awd: " + err.Error() + "\n" + string(out))
	}
	builtPolicy = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-tags", "awtest", "-o", builtPolicy, "../aw-policy").CombinedOutput(); err != nil {
		panic("building aw-policy: " + err.Error() + "\n" + string(out))
	}
	// os.Exit does not run deferred calls, so the cleanup has to happen
	// after m.Run returns and before Exit is called, not as a defer above.
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type server struct {
	url  string
	cmd  *exec.Cmd
	done chan error
	stop func()
}

// startAwd runs the control plane on an ephemeral port with an admin token
// and the in-memory store. stop() ends it early, for the outage test.
func startAwd(t *testing.T) *server {
	t.Helper()
	cmd := exec.Command(builtAwd, "serve")
	cmd.Env = append(os.Environ(), "AWD_ADDR=127.0.0.1:0", "AWD_LOG_LEVEL=error", "AWD_ADMIN_TOKEN="+adminToken)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the listen address: %v", err)
	}
	address := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "listening on "))
	s := &server{url: "http://" + address, cmd: cmd, done: done}
	var once bool
	s.stop = func() {
		if once {
			return
		}
		once = true
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		io.Copy(io.Discard, stdout)
	}
	t.Cleanup(s.stop)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.url + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("awd did not become healthy")
	return nil
}

// admin sends one authenticated request to awd and decodes the JSON reply.
func admin(t *testing.T, s *server, method, path string, body any, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, s.url+path, reader)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s: %s: %s", method, path, resp.Status, payload)
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			t.Fatalf("decoding %s: %v\n%s", path, err, payload)
		}
	}
}

func runSync(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(builtSync, args...)
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errOut.String(), 0
	case errors.As(err, &exitErr):
		return out.String(), errOut.String(), exitErr.ExitCode()
	default:
		t.Fatalf("running aw-sync: %v", err)
		return "", "", 0
	}
}

// runPolicy executes aw-policy the way Claude Code does, from dir, against
// the bundle aw-sync rendered. It returns the managedSettings object (nil
// when the envelope has none), stderr and the exit code.
func runPolicy(t *testing.T, dir, bundlePath string) (managed map[string]any, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(builtPolicy)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"AW_POLICY_BUNDLE="+bundlePath,
		"AW_POLICY_CONFIG="+filepath.Join(dir, "absent-aw-policy.json"),
		"HOME="+t.TempDir(), "XDG_CACHE_HOME="+t.TempDir())
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("running aw-policy: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("aw-policy stdout is not one JSON object: %v\n%s", err, out.String())
	}
	managed, _ = envelope["managedSettings"].(map[string]any)
	return managed, errOut.String(), code
}

// paymentsRepo makes a directory that repo.Detect resolves to
// github.com/acme/payments-api, without needing git.
func paymentsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[remote \"origin\"]\n\turl = git@github.com:acme/payments-api.git\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const e2ePolicy = `{
  "version": "2026-09-21.e2e",
  "groups": {"alice@acme.com": ["platform"]},
  "rules": [
    {"name": "baseline", "agents": {
      "claude": {"managed": {"model": "sonnet"}},
      "codex": {"managed": {"allowed_sandbox_modes": ["read-only", "workspace-write"]}},
      "gemini": {"managed": {
        "settings": {"admin": {"secureModeEnabled": true}},
        "policies": [{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": 100}]
      }}
    }},
    {"name": "platform", "match": {"groups": ["platform"]}, "agents": {"claude": {"managed": {"model": "opus"}}}},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {
      "claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}},
      "codex": {"managed": {"allowed_sandbox_modes": ["read-only"]}},
      "gemini": {"managed": {"policies": [{"toolName": "*", "decision": "deny", "priority": 999}]}}
    }}
  ]
}`

func applyPolicy(t *testing.T, s *server) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(e2ePolicy), &doc); err != nil {
		t.Fatal(err)
	}
	// The body is the bare rule set; awd rejects unknown fields.
	admin(t, s, http.MethodPost, "/v1/policy/revisions", doc, nil)
}

func mintToken(t *testing.T, s *server, user string) string {
	t.Helper()
	var out struct{ Token string }
	admin(t, s, http.MethodPost, "/v1/enrollment-tokens", map[string]string{"user": user}, &out)
	if out.Token == "" {
		t.Fatal("no token minted")
	}
	return out.Token
}

func TestEnrollOnceStatusAndOutage(t *testing.T) {
	s := startAwd(t)
	applyPolicy(t, s)
	stateDir := filepath.Join(t.TempDir(), "state")
	claudeRoot := filepath.Join(t.TempDir(), "claude-root")
	codexRoot := filepath.Join(t.TempDir(), "codex-root")
	geminiRoot := filepath.Join(t.TempDir(), "gemini-root")

	// Enroll, with the token in the environment so it never hits argv.
	token := mintToken(t, s, "alice@acme.com")
	stdout, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=" + token},
		"enroll", "--server", s.url, "--name", "e2e-host", "--agents", "claude,codex,gemini", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("enroll exited %d: %s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "alice@acme.com") {
		t.Errorf("enroll output does not name the user: %q", stdout)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(stateDir, "machine.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("machine.json mode = %o, want 0600", info.Mode().Perm())
		}
	}

	// status works for a developer right after enroll, before any `once`
	// has run: enroll wrote the non-secret enrollment facts into
	// state.json, since machine.json is 0600 and a developer cannot read
	// it.
	stdout, stderr, code = runSync(t, nil, "status", "--json", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("status right after enroll exited %d: %s%s", code, stdout, stderr)
	}
	var freshReport struct {
		Enrolled  bool
		MachineID string
		Agents    []string
	}
	if err := json.Unmarshal([]byte(stdout), &freshReport); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, stdout)
	}
	if !freshReport.Enrolled || freshReport.MachineID == "" || len(freshReport.Agents) != 3 || freshReport.Agents[0] != "claude" || freshReport.Agents[1] != "codex" || freshReport.Agents[2] != "gemini" {
		t.Errorf("status right after enroll = %+v", freshReport)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		machinePath := filepath.Join(stateDir, "machine.json")
		if err := os.Chmod(machinePath, 0o000); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code = runSync(t, nil, "status", "--json", "--state-dir", stateDir)
		if code != 0 {
			t.Fatalf("status with an unreadable machine.json exited %d: %s%s", code, stdout, stderr)
		}
		if err := os.Chmod(machinePath, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A second enrollment is refused without --force, and the token is
	// single-use anyway.
	_, stderr, code = runSync(t, []string{"AW_SYNC_TOKEN=" + token},
		"enroll", "--server", s.url, "--agents", "claude", "--state-dir", stateDir)
	if code == 0 || !strings.Contains(stderr, "--force") {
		t.Errorf("re-enroll: exit %d, stderr %q; want a refusal naming --force", code, stderr)
	}

	// The first cycle renders both Claude files.
	stdout, stderr, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot, "--root", "codex="+codexRoot, "--root", "gemini="+geminiRoot)
	if code != 0 {
		t.Fatalf("once exited %d: %s%s", code, stdout, stderr)
	}
	bundlePath := filepath.Join(claudeRoot, "aw-bundle.json")
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Version string `json:"version"`
		Groups  []string
		Rules   []struct{ Name string }
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Version != "2026-09-21.e2e" || len(bundle.Rules) != 3 || len(bundle.Groups) != 1 {
		t.Errorf("bundle = %+v; want the platform group resolved and all three Claude rules with repo matchers intact", bundle)
	}
	if _, err := os.Stat(filepath.Join(claudeRoot, "managed-settings.d", "50-agent-wrapper.json")); err != nil {
		t.Errorf("drop-in not written: %v", err)
	}
	if got, _ := os.ReadFile(bundlePath + ".aw-revision"); string(got) != "2026-09-21.e2e\n" {
		t.Errorf("aw-revision = %q", got)
	}

	// Codex gets a whole requirements.toml: the baseline allowlist, the
	// header naming the revision, and no trace of the repo-scoped payments
	// rule, which a static file cannot express and the note reports.
	reqs, err := os.ReadFile(filepath.Join(codexRoot, "requirements.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(reqs), "# Managed by aw-sync from policy revision 2026-09-21.e2e.") ||
		!strings.Contains(string(reqs), "allowed_sandbox_modes = ['read-only', 'workspace-write']") {
		t.Errorf("requirements.toml =\n%s", reqs)
	}
	if _, err := os.Stat(filepath.Join(codexRoot, "requirements.toml.aw-revision")); err == nil {
		t.Error("a TOML file carries its revision in the header; no .aw-revision sibling is written")
	}

	// Gemini gets both files: settings.json with its revision sibling, and
	// the admin policy file with the revision in its header. The repo-scoped
	// payments rule is absent from both and named in the note.
	settings, err := os.ReadFile(filepath.Join(geminiRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"secureModeEnabled": true`) {
		t.Errorf("settings.json =\n%s", settings)
	}
	if got, _ := os.ReadFile(filepath.Join(geminiRoot, "settings.json.aw-revision")); string(got) != "2026-09-21.e2e\n" {
		t.Errorf("settings.json.aw-revision = %q; a JSON file carries its revision in a sibling", got)
	}
	policies, err := os.ReadFile(filepath.Join(geminiRoot, "policies", "50-agent-wrapper.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(policies), "# Managed by aw-sync from policy revision 2026-09-21.e2e.") ||
		!strings.Contains(string(policies), "priority = 100\n") ||
		strings.Contains(string(policies), "priority = 999") {
		t.Errorf("50-agent-wrapper.toml =\n%s", policies)
	}

	// aw-policy resolves the rendered bundle per launch and needs no
	// server: outside a repository the platform rule sets the model and
	// the payments deny stays out; inside a payments repository it joins.
	managed, stderr, code := runPolicy(t, t.TempDir(), bundlePath)
	if code != 0 {
		t.Fatalf("aw-policy exited %d: %s", code, stderr)
	}
	if managed["model"] != "opus" {
		t.Errorf("outside a repository managed = %v; want model opus from the platform rule", managed)
	}
	if _, has := managed["permissions"]; has {
		t.Errorf("outside a repository the payments rule leaked in: %v", managed)
	}
	managed, stderr, code = runPolicy(t, paymentsRepo(t), bundlePath)
	if code != 0 {
		t.Fatalf("aw-policy in the payments repository exited %d: %s", code, stderr)
	}
	permissions, _ := managed["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	if managed["model"] != "opus" || len(deny) != 1 || deny[0] != "Bash(curl *)" {
		t.Errorf("in the payments repository managed = %v; want the platform model and the repo-scoped deny", managed)
	}

	// A second cycle is a 304 no-op.
	stdout, _, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot, "--root", "codex="+codexRoot, "--root", "gemini="+geminiRoot)
	if code != 0 || !strings.Contains(stdout, "unchanged") {
		t.Errorf("second once: exit %d, stdout %q; want an unchanged report", code, stdout)
	}

	// Status is clean and machine-readable.
	stdout, _, code = runSync(t, nil, "status", "--json", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("status exited %d", code)
	}
	var report struct {
		Enrolled bool
		Version  string
		Drift    bool
		Error    string
		Notes    []string
		Files    []struct{ Path, State string }
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, stdout)
	}
	if !report.Enrolled || report.Version != "2026-09-21.e2e" || report.Drift || report.Error != "" || len(report.Files) != 6 {
		t.Errorf("report = %+v", report)
	}
	if notes := strings.Join(report.Notes, "\n"); !strings.Contains(notes, "repo-scoped rule") || !strings.Contains(notes, "payments") ||
		!strings.Contains(notes, "codex:") || !strings.Contains(notes, "gemini:") {
		t.Errorf("report.Notes = %q; want the dropped payments rule reported by both static-file renderers", report.Notes)
	}

	// Drift is reported, then repaired by the next cycle even though the
	// bundle on the server has not changed: a naive conditional fetch
	// would get a 304 for an unchanged bundle and leave the tampered file
	// as it is, which is the bug C1 fixes.
	if err := os.WriteFile(bundlePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ = runSync(t, nil, "status", "--state-dir", stateDir)
	if !strings.Contains(stdout, "drift") {
		t.Errorf("status does not report drift:\n%s", stdout)
	}
	stdout, stderr, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot, "--root", "codex="+codexRoot, "--root", "gemini="+geminiRoot)
	if code != 0 {
		t.Fatalf("once after drift exited %d: %s%s", code, stdout, stderr)
	}
	if got, _ := os.ReadFile(bundlePath); string(got) == "{}\n" {
		t.Error("once did not repair the tampered bundle")
	}
	if _, err := os.Stat(filepath.Join(claudeRoot, "managed-settings.d", "50-agent-wrapper.json")); err != nil {
		t.Errorf("drop-in not rewritten by the repair cycle: %v", err)
	}

	// Tamper again so the outage check below proves a failed fetch leaves
	// a tampered file alone, not that the repair cycle above already fixed
	// it.
	if err := os.WriteFile(bundlePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The server goes away: the cycle fails, the files stay.
	s.stop()
	stdout, stderr, code = runSync(t, nil, "once", "--state-dir", stateDir, "--root", "claude="+claudeRoot, "--root", "codex="+codexRoot, "--root", "gemini="+geminiRoot)
	if code != 1 {
		t.Errorf("once during an outage exited %d, want 1: %s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(claudeRoot, "managed-settings.d", "50-agent-wrapper.json")); err != nil {
		t.Error("an outage removed the drop-in")
	}
	if _, err := os.Stat(filepath.Join(codexRoot, "requirements.toml")); err != nil {
		t.Error("an outage removed requirements.toml")
	}
	if _, err := os.Stat(filepath.Join(geminiRoot, "policies", "50-agent-wrapper.toml")); err != nil {
		t.Error("an outage removed the Gemini policy file")
	}
	if got, _ := os.ReadFile(bundlePath); string(got) != "{}\n" {
		t.Error("an outage rewrote the bundle; a failed fetch must leave files as they are")
	}
	stdout, _, _ = runSync(t, nil, "status", "--json", "--state-dir", stateDir)
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Error == "" || report.Version != "2026-09-21.e2e" {
		t.Errorf("after the outage report = %+v; want the error recorded and the version kept", report)
	}

	// The helper still answers during the outage, and from what is on
	// disk: the bundle was tampered to "{}" above, so a helper that read
	// the file emits an empty managedSettings object, and one that served
	// anything stale (the platform model, say) would show it here. Exit 0
	// with a valid envelope is all Claude Code requires to start.
	managed, stderr, code = runPolicy(t, t.TempDir(), bundlePath)
	if code != 0 {
		t.Errorf("aw-policy during the outage exited %d: %s", code, stderr)
	}
	if managed == nil || len(managed) != 0 {
		t.Errorf("during the outage managed = %v; want an empty object compiled from the tampered bundle on disk", managed)
	}
}

func TestOnceResolvesARelativeRootAgainstTheWorkingDirectory(t *testing.T) {
	s := startAwd(t)
	applyPolicy(t, s)
	stateDir := filepath.Join(t.TempDir(), "state")
	token := mintToken(t, s, "alice@acme.com")
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=" + token},
		"enroll", "--server", s.url, "--agents", "claude", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("enroll exited %d: %s", code, stderr)
	}

	// --root is resolved with filepath.Abs before use, so a relative value
	// must land under the process's working directory, not wherever the
	// renderer happens to be invoked from.
	workDir := t.TempDir()
	cmd := exec.Command(builtSync, "once", "--state-dir", stateDir, "--root", "claude=relative-claude-root")
	cmd.Dir = workDir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("once exited: %v: %s%s", err, out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, "relative-claude-root", "aw-bundle.json")); err != nil {
		t.Errorf("a relative --root was not resolved against the working directory: %v", err)
	}

	// state.json must have recorded an absolute path: `status`, run here
	// from a different working directory than `once` used, must still find
	// the file and report it clean, not "missing".
	stdout, stderr, code := runSync(t, nil, "status", "--json", "--state-dir", stateDir)
	if code != 0 {
		t.Fatalf("status exited %d: %s%s", code, stdout, stderr)
	}
	var report struct {
		Files []struct{ Path, State string }
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, stdout)
	}
	for _, f := range report.Files {
		if !filepath.IsAbs(f.Path) {
			t.Errorf("state.json recorded a relative path %q", f.Path)
		}
		if f.State != "ok" {
			t.Errorf("file %s reports %q from a different working directory; --root was not made absolute", f.Path, f.State)
		}
	}
	if len(report.Files) != 3 {
		t.Errorf("report.Files = %v, want 3 entries", report.Files)
	}
}

func TestOnceWithoutEnrollmentExits1(t *testing.T) {
	_, stderr, code := runSync(t, nil, "once", "--state-dir", t.TempDir())
	if code != 1 || !strings.Contains(stderr, "enroll") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestOnceWithAMissingStateDirSaysNotEnrolled(t *testing.T) {
	_, stderr, code := runSync(t, nil, "once", "--state-dir", filepath.Join(t.TempDir(), "absent"))
	if code != 1 || !strings.Contains(stderr, "enroll") {
		t.Errorf("exit %d, stderr %q; a missing state dir is 'not enrolled', not a lock error", code, stderr)
	}
}

func TestOnceSkipsWhileAnotherCycleHoldsTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the lock test holds the lock from this process; covered by the sync package test on Windows")
	}
	stateDir := t.TempDir()
	release, err := sync.Lock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	stdout, stderr, code := runSync(t, nil, "once", "--state-dir", stateDir)
	if code != 0 || !strings.Contains(stdout, "another aw-sync cycle is running; skipped") {
		t.Errorf("exit %d, stdout %q, stderr %q; want a clean skip", code, stdout, stderr)
	}
}

func TestEnrollRefusesWhileAnotherCycleHoldsTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("see TestOnceSkipsWhileAnotherCycleHoldsTheLock")
	}
	stateDir := t.TempDir()
	release, err := sync.Lock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// A closed port: the refusal must come before any network call.
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "another aw-sync is running; retry") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "machine.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("machine.json written while locked: %v", err)
	}
}

// TestEnrollChecksEnrollmentUnderTheLock pins the fix for a TOCTOU: the
// already-enrolled check must run under the same lock as the write, so a
// second concurrent enroll without --force sees the first one's
// machine.json instead of racing it. While the lock is held, a second
// enroll must be refused for holding the lock, before it ever reads
// machine.json; once released, the same enroll must be refused for already
// being enrolled, with machine.json unchanged.
func TestEnrollChecksEnrollmentUnderTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("see TestOnceSkipsWhileAnotherCycleHoldsTheLock")
	}
	stateDir := t.TempDir()
	machine := []byte(`{"server":"http://awd","machineId":"m1","credential":"c","agents":["claude"]}` + "\n")
	machinePath := filepath.Join(stateDir, "machine.json")
	if err := os.WriteFile(machinePath, machine, 0o600); err != nil {
		t.Fatal(err)
	}

	release, err := sync.Lock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	// A closed port: if the lock check did not come first, this would reach
	// the network (or the already-enrolled check) instead.
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "another aw-sync is running; retry") {
		t.Errorf("while locked: exit %d, stderr %q; want the lock refusal first", code, stderr)
	}
	release()

	_, stderr, code = runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "--force") {
		t.Errorf("after release: exit %d, stderr %q; want the already-enrolled refusal", code, stderr)
	}
	got, err := os.ReadFile(machinePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, machine) {
		t.Errorf("machine.json changed: %q", got)
	}
}

func TestEnrollRejectsAnAgentWithoutARenderer(t *testing.T) {
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude,copilot", "--state-dir", t.TempDir())
	if code != 1 || !strings.Contains(stderr, "copilot") {
		t.Errorf("exit %d, stderr %q; want a refusal naming copilot before any network call", code, stderr)
	}
}

func TestEnrollRefusesToOverwriteACorruptEnrollment(t *testing.T) {
	stateDir := t.TempDir()
	corrupt := []byte(`{"server":"http://awd"}`)
	machinePath := filepath.Join(stateDir, "machine.json")
	if err := os.WriteFile(machinePath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	// --server points at a closed port so the test fails loudly if the
	// refusal doesn't come before any network call.
	_, stderr, code := runSync(t, []string{"AW_SYNC_TOKEN=x"},
		"enroll", "--server", "http://127.0.0.1:1", "--agents", "claude", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "--force") {
		t.Errorf("exit %d, stderr %q; want a refusal naming --force", code, stderr)
	}
	got, err := os.ReadFile(machinePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Errorf("machine.json changed: %q", got)
	}
}

func TestHelpListsTheCommands(t *testing.T) {
	stdout, _, code := runSync(t, nil, "help")
	if code != 0 {
		t.Fatalf("help exited %d", code)
	}
	for _, want := range []string{"enroll", "once", "status", "install-timer", "uninstall-timer", "AW_SYNC_TOKEN"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestInstallTimerRefusesAnUnenrolledMachine(t *testing.T) {
	stateDir := t.TempDir()
	_, stderr, code := runSync(t, nil, "install-timer", "--state-dir", stateDir)
	if code != 1 || !strings.Contains(stderr, "not enrolled; run aw-sync enroll first") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if entries, _ := os.ReadDir(stateDir); len(entries) != 0 {
		t.Errorf("wrote into the state dir: %v", entries)
	}
}

func TestInstallTimerRejectsBadIntervalsFirst(t *testing.T) {
	for _, interval := range []string{"30s", "90s", "25h", "soon"} {
		// Even on an unenrolled dir the interval is the reported problem:
		// it is checked before anything else.
		_, stderr, code := runSync(t, nil, "install-timer", "--interval", interval, "--state-dir", t.TempDir())
		if code != 1 || !strings.Contains(stderr, "interval") {
			t.Errorf("--interval %s: exit %d, stderr %q", interval, code, stderr)
		}
	}
}

func TestInstallTimerRejectsStrayArguments(t *testing.T) {
	_, stderr, code := runSync(t, nil, "install-timer", "now")
	if code != 1 || !strings.Contains(stderr, "no arguments") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	_, stderr, code = runSync(t, nil, "uninstall-timer", "now")
	if code != 1 || !strings.Contains(stderr, "no arguments") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
