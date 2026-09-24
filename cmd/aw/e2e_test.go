package main_test

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/sync"
)

// awBinary builds the CLI once and returns its path.
func awBinary(t *testing.T) string {
	t.Helper()
	if built != "" {
		return built
	}
	t.Fatal("binary was not built; TestMain should have built it")
	return ""
}

var built string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-e2e-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	built = filepath.Join(dir, "aw")
	build := exec.Command("go", "build", "-o", built, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic("building aw: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

// fakeAgent writes an executable named name into dir running the given script.
func fakeAgent(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	return path
}

// run invokes the CLI with a controlled environment and returns its output and
// exit code.
func run(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(awBinary(t), args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running aw %v: %v", args, err)
		return "", 0
	}
}

// baseEnv is the environment every e2e test starts from. It sets
// AW_SYNC_STATE_DIR to a fresh, empty temporary directory so a test that
// does not care about aw-sync state never reads the real machine's
// /var/lib/agent-wrapper/aw-bundle.json; a test that needs its own state
// directory appends its own AW_SYNC_STATE_DIR after this slice, and the
// later entry in exec.Cmd.Env wins over this default.
func baseEnv(t *testing.T, binDir string) []string {
	t.Helper()
	return []string{"PATH=" + binDir, "HOME=" + t.TempDir(), "XDG_CACHE_HOME=" + t.TempDir(), "AW_SYNC_STATE_DIR=" + t.TempDir()}
}

func TestAgentsListsWhatTheBinaryCanLaunch(t *testing.T) {
	binDir := t.TempDir()

	out, code := run(t, baseEnv(t, binDir), "agents")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	for _, name := range []string{"claude", "codex", "gemini"} {
		if !strings.Contains(out, name) {
			t.Errorf("output %q does not list %s", out, name)
		}
	}
}

func TestDoctorReportsTheComputedLaunchWithoutRunningTheAgent(t *testing.T) {
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "agent-ran")
	fakeAgent(t, binDir, "claude", "touch "+marker)

	out, code := run(t, baseEnv(t, binDir), "doctor")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("doctor ran the agent; it must only report")
	}
	if !strings.Contains(out, "--settings") {
		t.Errorf("doctor output does not show the injected flags:\n%s", out)
	}
}

func TestDoctorJSONIsMachineReadable(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")

	out, code := run(t, baseEnv(t, binDir), "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	var report struct {
		Agents []struct {
			Name   string `json:"name"`
			Binary string `json:"binary"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", err, out)
	}
	if len(report.Agents) == 0 || report.Agents[0].Binary == "" {
		t.Errorf("report does not locate any agent: %+v", report)
	}
}

func TestDoctorReportsHowTheAgentIsGoverned(t *testing.T) {
	// Whatever this machine's state, doctor says something about the
	// enforcement path: a helper it found, or a warning that there is none.
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")

	out, code := run(t, baseEnv(t, binDir), "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	var report struct {
		Agents []struct {
			Findings []struct {
				Level   string `json:"level"`
				Message string `json:"message"`
			} `json:"findings"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", err, out)
	}
	if len(report.Agents) == 0 || len(report.Agents[0].Findings) == 0 {
		t.Fatalf("report has no findings for the agent:\n%s", out)
	}
	for _, f := range report.Agents[0].Findings {
		if f.Level == "" || f.Message == "" {
			t.Errorf("finding %+v is missing a level or message", f)
		}
	}

	text, _ := run(t, baseEnv(t, binDir), "doctor")
	if !strings.Contains(text, "policyHelper") {
		t.Errorf("plain doctor output should mention the policyHelper status:\n%s", text)
	}
}

func TestRunningAnAgentPassesItsExitCodeThrough(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 3")

	out, code := run(t, baseEnv(t, binDir), "claude")

	if code != 3 {
		t.Errorf("exit code = %d, want 3. output:\n%s", code, out)
	}
}

func TestRunningAnAgentPassesTheDevelopersArgumentsThrough(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", `printf '%s\n' "$@"`)

	out, code := run(t, baseEnv(t, binDir), "claude", "--model", "opus")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "--model\nopus") {
		t.Errorf("agent did not receive the developer's arguments:\n%s", out)
	}
}

func TestTheWrapperOnPathUnderTheAgentsNameDoesNotRecurse(t *testing.T) {
	wrapperDir, realDir := t.TempDir(), t.TempDir()
	if err := os.Symlink(awBinary(t), filepath.Join(wrapperDir, "claude")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	fakeAgent(t, realDir, "claude", "echo real-agent")

	env := []string{
		"PATH=" + wrapperDir + string(filepath.ListSeparator) + realDir,
		"HOME=" + t.TempDir(),
		"XDG_CACHE_HOME=" + t.TempDir(),
	}
	out, code := run(t, env, "claude")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "real-agent") {
		t.Errorf("output %q does not come from the real agent", out)
	}
}

func TestAnUnknownAgentFailsWithAUsefulMessage(t *testing.T) {
	out, code := run(t, baseEnv(t, t.TempDir()), "nosuchagent")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero. output:\n%s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("error %q does not say which agents are available", out)
	}
}

func TestAPolicyDocumentReachesTheLaunch(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	body := `{"agents":{"claude":{"managed":{"model":"opus"},"env":{"ACME_MARKER":"set"},"forceEnv":true}}}`
	if err := os.WriteFile(policyPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	out, code := run(t, baseEnv(t, binDir), "--policy", policyPath, "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "ACME_MARKER") {
		t.Errorf("policy environment did not reach the launch:\n%s", out)
	}
}

func TestAMissingPolicyFileIsReportedRatherThanIgnored(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	missing := filepath.Join(t.TempDir(), "absent.json")

	out, code := run(t, baseEnv(t, binDir), "--policy", missing, "doctor")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a missing policy. output:\n%s", out)
	}
	if !strings.Contains(out, missing) {
		t.Errorf("error %q does not name the missing policy file", out)
	}
}

// gitRepo makes dir a repository whose origin is url, so repo.Detect finds it.
func gitRepo(t *testing.T, dir, url string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", url}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

const e2eBundle = `{
  "version": "2026-09-22.e2e",
  "groups": ["platform"],
  "rules": [
    {"name": "baseline", "agents": {
      "claude": {"managed": {"model": "sonnet"}},
      "codex": {"launch": {"sandbox_mode": "workspace-write"}},
      "gemini": {"managed": {"settings": {"admin": {"secureModeEnabled": true}}}}
    }},
    {"name": "payments", "match": {"repos": ["github.com/acme/payments*"]}, "agents": {
      "codex": {"launch": {"sandbox_mode": "read-only"}},
      "gemini": {"managed": {"policies": [{"toolName": "run_shell_command", "decision": "deny", "priority": 100}]}}
    }}
  ]
}`

func TestDoctorCompilesTheBundleForTheRepositoryItRunsIn(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	fakeAgent(t, binDir, "codex", "exit 0")
	fakeAgent(t, binDir, "gemini", "exit 0")
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "aw-bundle.json"), []byte(e2eBundle), 0o644); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	gitRepo(t, work, "https://github.com/acme/payments-api.git")

	cmd := exec.Command(awBinary(t), "doctor", "--json")
	cmd.Dir = work
	cmd.Env = append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}

	var report struct {
		Policy string `json:"policy"`
		Agents []struct {
			Name   string `json:"name"`
			Launch struct {
				Args  []string `json:"args"`
				Notes []string `json:"notes"`
				Files []string `json:"files"`
			} `json:"launch"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	if !strings.Contains(report.Policy, "2026-09-22.e2e") || !strings.Contains(report.Policy, "github.com/acme/payments-api") {
		t.Errorf("policy = %q; want the bundle version and the detected repository", report.Policy)
	}
	names := make([]string, 0, 3)
	for _, a := range report.Agents {
		names = append(names, a.Name)
		switch a.Name {
		case "codex":
			if args := strings.Join(a.Launch.Args, " "); args != `-c sandbox_mode="read-only"` {
				t.Errorf("codex args = %q; want the payments rule's override", args)
			}
		case "gemini":
			// The bundle's version is the policy revision Gemini's own
			// session policy file should name in its header, so a reader
			// of the cache can tell which bundle produced it.
			policyFile := ""
			for _, f := range a.Launch.Files {
				if strings.Contains(f, "policies") {
					policyFile = f
				}
			}
			if policyFile == "" {
				t.Fatalf("gemini launch files %v lack a policies file", a.Launch.Files)
			}
			raw, err := os.ReadFile(policyFile)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(raw), "# Written by aw for one session from policy revision 2026-09-22.e2e.") {
				t.Errorf("policy file %s =\n%s\nwant the header to name the bundle version", policyFile, raw)
			}
		}
		if !strings.Contains(strings.Join(a.Launch.Notes, "\n"), "policy compiled for github.com/acme/payments-api") {
			t.Errorf("%s notes %q lack the compiled-for note", a.Name, a.Launch.Notes)
		}
	}
	if strings.Join(names, ",") != "claude,codex,gemini" {
		t.Errorf("agents = %v", names)
	}
}

func TestDoctorSaysWhenThereIsNoBundle(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")

	out, code := run(t, append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+t.TempDir()), "doctor")

	if code != 0 {
		t.Fatalf("exit code = %d\n%s", code, out)
	}
	if !strings.Contains(out, "policy:  none") || !strings.Contains(out, "aw-sync has not written") {
		t.Errorf("output should report no policy and why:\n%s", out)
	}
}

func TestAnUnreadableBundleRefusesToLaunch(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "aw-bundle.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir), "claude")

	if code == 0 || !strings.Contains(out, "aw-bundle.json") {
		t.Errorf("exit %d, output %q; a bundle that was meant to apply and cannot be read must refuse the launch", code, out)
	}
}

func TestDoctorReportsAwSyncState(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	stateDir := t.TempDir()
	state := `{"server":"http://awd.example","machineId":"m1","agents":["claude"],"etag":"","version":"v7","syncedAt":"2026-09-21T12:00:00Z","files":{},"error":"control plane unreachable","notes":[]}`
	if err := os.WriteFile(filepath.Join(stateDir, "machine.json"), []byte(`{"server":"http://awd.example","machineId":"m1","credential":"c","agents":["claude"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir)

	out, code := run(t, env, "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	var report struct {
		SyncStateDir string `json:"syncStateDir"`
		Sync         struct {
			Enrolled bool   `json:"enrolled"`
			Version  string `json:"version"`
			Error    string `json:"error"`
		} `json:"sync"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", err, out)
	}
	if report.SyncStateDir != stateDir || !report.Sync.Enrolled || report.Sync.Version != "v7" || report.Sync.Error != "control plane unreachable" {
		t.Errorf("sync section = %+v; want aw-sync's last cycle from state.json", report)
	}

	text, _ := run(t, env, "doctor")
	if !strings.Contains(text, "version v7") || !strings.Contains(text, "control plane unreachable") {
		t.Errorf("plain doctor output should show the last sync and its error:\n%s", text)
	}
}

// writeSignedState builds a state directory the way aw-sync leaves one on a
// machine whose enrollment pinned a signing key: the bundle, its signature,
// and the public key it verifies against.
func writeSignedState(t *testing.T, dir, body string) {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	if err := os.WriteFile(filepath.Join(dir, sync.TrustFile), []byte(signing.FormatPublic(pub)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sync.BundleFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("aw-ed25519 %s %s\n", signing.KeyID(pub), signing.Sign(key, []byte(body)))
	if err := os.WriteFile(filepath.Join(dir, sync.SignatureFile), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorReportsAVerifiedBundleSignature(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "claude", "exit 0")
	stateDir := t.TempDir()
	writeSignedState(t, stateDir, e2eBundle)

	out, code := run(t, append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir), "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	var report struct {
		Policy string `json:"policy"`
		Agents []struct {
			Name     string `json:"name"`
			Findings []struct {
				Level   string `json:"level"`
				Message string `json:"message"`
			} `json:"findings"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", err, out)
	}
	if !strings.Contains(report.Policy, "2026-09-22.e2e") {
		t.Errorf("policy = %q; want the verified bundle's version", report.Policy)
	}
	var sawVerified, sawRedirect bool
	for _, a := range report.Agents {
		if a.Name != "claude" {
			continue
		}
		for _, f := range a.Findings {
			if f.Level == "ok" && strings.Contains(f.Message, "bundle signature: verified (key ") {
				sawVerified = true
			}
			if f.Level == "warn" && strings.Contains(f.Message, "AW_SYNC_STATE_DIR") {
				sawRedirect = true
			}
		}
	}
	if !sawVerified {
		t.Errorf("claude's findings should report a verified bundle signature:\n%s", out)
	}
	// This very report is the shape the note is for: a "verified" about a
	// directory the environment pointed doctor at, while aw-policy goes on
	// reading the OS default.
	if !sawRedirect {
		t.Errorf("doctor reported on a redirected state directory without saying so:\n%s", out)
	}
}

func TestATamperedBundleRefusesToLaunch(t *testing.T) {
	binDir := t.TempDir()
	fakeAgent(t, binDir, "codex", "echo should-not-run")
	stateDir := t.TempDir()
	writeSignedState(t, stateDir, e2eBundle)
	tamperByte(t, filepath.Join(stateDir, sync.BundleFile))

	out, code := run(t, append(baseEnv(t, binDir), "AW_SYNC_STATE_DIR="+stateDir), "codex")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a bundle whose signature does not check out. output:\n%s", out)
	}
	if strings.Contains(out, "should-not-run") {
		t.Errorf("codex ran despite the unverifiable bundle:\n%s", out)
	}
	if !strings.Contains(out, sync.BundleFile) {
		t.Errorf("error %q should name the bundle file that failed to verify", out)
	}
}

// tamperByte changes one digit of path, invalidating any signature over its
// former contents while leaving the file valid JSON and valid UTF-8, so a
// test using it exercises the signature check rather than a parse failure.
func tamperByte(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range raw {
		if b >= '0' && b <= '9' {
			raw[i] = '0' + (b-'0'+1)%10
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("tamperByte: %s has no digit to change", path)
}
