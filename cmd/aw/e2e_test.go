package main_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func baseEnv(t *testing.T, binDir string) []string {
	t.Helper()
	return []string{"PATH=" + binDir, "HOME=" + t.TempDir(), "XDG_CACHE_HOME=" + t.TempDir()}
}

func TestAgentsListsWhatTheBinaryCanLaunch(t *testing.T) {
	binDir := t.TempDir()

	out, code := run(t, baseEnv(t, binDir), "agents")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("output %q does not list claude", out)
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
