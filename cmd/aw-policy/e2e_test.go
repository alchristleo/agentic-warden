package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// built is the helper as the tests need it: with -tags awtest, so fixtures
// can be named in the environment. release is the helper as it ships.
var built, release string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-policy-e2e-*")
	if err != nil {
		panic(err)
	}
	built = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-tags", "awtest", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building aw-policy (awtest): " + err.Error() + "\n" + string(out))
	}
	release = filepath.Join(dir, "aw-policy-release")
	if out, err := exec.Command("go", "build", "-o", release, ".").CombinedOutput(); err != nil {
		panic("building aw-policy (release): " + err.Error() + "\n" + string(out))
	}
	// os.Exit does not run deferred calls, so the cleanup has to happen
	// after m.Run returns and before Exit is called.
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// bundle is a rendered aw-bundle.json with one rule everyone gets.
const bundle = `{"version":"v1","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`

// run executes the tagged helper the way Claude Code does: no arguments,
// stdout and stderr captured separately. env is added to a minimal
// environment that points every cache and home directory at temporary ones.
func run(t *testing.T, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runBinary(t, built, nil, env...)
}

// runBinary is run for a chosen binary and arguments.
func runBinary(t *testing.T, binary string, args []string, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = t.TempDir() // not a repository
	cmd.Env = append(env, "PATH="+os.Getenv("PATH"), "HOME="+t.TempDir(), "XDG_CACHE_HOME="+t.TempDir())
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
		t.Fatalf("running %s: %v", binary, err)
		return "", "", 0
	}
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func decode(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return out
}

func TestEmitsTheBundleAsManagedSettings(t *testing.T) {
	bundlePath := writeFile(t, "aw-bundle.json", bundle)

	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"))

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	managed, _ := decode(t, stdout)["managedSettings"].(map[string]any)
	if managed["model"] != "opus" {
		t.Errorf("managedSettings = %v, want the bundle's rule", managed)
	}
	if stderr != "" {
		t.Errorf("stderr = %q; a missing configuration file is not worth a note", stderr)
	}
}

func TestAMissingBundleStillExitsZero(t *testing.T) {
	// A machine aw-sync has not reached yet must still let Claude Code
	// start: an empty envelope leaves the static managed-settings files
	// in force.
	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+filepath.Join(t.TempDir(), "absent-bundle.json"), "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"))

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, has := decode(t, stdout)["managedSettings"]; has {
		t.Errorf("stdout = %s, want an envelope without managedSettings", stdout)
	}
	if !strings.Contains(stderr, "absent-bundle.json") {
		t.Errorf("stderr %q should name the missing bundle", stderr)
	}
}

func TestAnInvalidConfigIsNotedAndIgnored(t *testing.T) {
	cfg := writeFile(t, "aw-policy.json", `{"serverUrl":"https://old.example.com"}`)
	bundlePath := writeFile(t, "aw-bundle.json", bundle)

	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+cfg)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if managed, _ := decode(t, stdout)["managedSettings"].(map[string]any); managed["model"] != "opus" {
		t.Errorf("managedSettings = %v; a bad configuration must not cost the policy", managed)
	}
	if !strings.Contains(stderr, "aw-policy.json") {
		t.Errorf("stderr %q should name the invalid configuration", stderr)
	}
}

func TestRequireBundleExitsNonZeroWithoutABundle(t *testing.T) {
	cfg := writeFile(t, "aw-policy.json", `{"requireBundle":true}`)

	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+filepath.Join(t.TempDir(), "absent.json"), "AW_POLICY_CONFIG="+cfg)

	if code == 0 {
		t.Fatal("exit = 0, want non-zero: the organization asked to fail closed")
	}
	decode(t, stdout)
	if stderr == "" {
		t.Error("stderr is empty; Claude Code shows it as the reason for refusing to start")
	}
}

func TestStderrStaysShort(t *testing.T) {
	// Claude Code fails the run past 1 MiB of stderr, and it shows stderr
	// as the reason when the helper exits non-zero, so notes are capped at
	// maxStderr (16 KiB) however much goes wrong. A bundle path that is
	// itself huge gets echoed into the "no policy available" note, so it
	// is what exercises the real cap.
	bundlePath := filepath.Join(t.TempDir(), strings.Repeat("x", 20<<10), "aw-bundle.json")

	_, stderr, _ := run(t, "AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"))

	if len(stderr) > 16<<10 {
		t.Errorf("stderr is %d bytes, want <= 16 KiB", len(stderr))
	}
	if len(stderr) == 0 {
		t.Error("stderr is empty, want the truncated note")
	}
}

func TestAReleaseBuildIgnoresTheEnvironmentOverrides(t *testing.T) {
	// Claude Code hands the helper the developer's environment. A release
	// build must read the system directory whatever that environment says,
	// and tell the developer so, or the override would look like it worked.
	// The fixture's model is one no real bundle would set, so the check
	// holds even on a machine that has a real bundle in /etc/claude-code.
	bundlePath := writeFile(t, "aw-bundle.json", `{"version":"v1","rules":[{"name":"mine","agents":{"claude":{"managed":{"model":"fixture-only-model"}}}}]}`)

	stdout, stderr, code := runBinary(t, release, nil,
		"AW_POLICY_BUNDLE="+bundlePath, "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"))

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if managed, _ := decode(t, stdout)["managedSettings"].(map[string]any); managed["model"] == "fixture-only-model" {
		t.Errorf("stdout = %s; the release build read the developer's file", stdout)
	}
	for _, name := range []string{"AW_POLICY_BUNDLE", "AW_POLICY_CONFIG"} {
		if !strings.Contains(stderr, name) || !strings.Contains(stderr, "ignores it") {
			t.Errorf("stderr %q should say %s is set and ignored", stderr, name)
		}
	}
}

func TestBuildInfoTellsTheTwoBuildsApart(t *testing.T) {
	stdout, _, code := runBinary(t, release, []string{"build-info"})
	if code != 0 || stdout != "env-overrides=off\n" {
		t.Errorf("release build-info: exit %d, stdout %q", code, stdout)
	}
	stdout, _, code = runBinary(t, built, []string{"build-info"})
	if code != 0 || stdout != "env-overrides=on\n" {
		t.Errorf("awtest build-info: exit %d, stdout %q", code, stdout)
	}
}

func TestAnUnrecognisedArgumentStillRunsTheHelper(t *testing.T) {
	// If a future Claude Code passes an argument, the helper must emit an
	// envelope rather than refuse the launch over something it did not
	// understand.
	stdout, _, code := runBinary(t, release, []string{"--something-new"})

	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	decode(t, stdout)
}
