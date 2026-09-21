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

var built string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aw-policy-e2e-*")
	if err != nil {
		panic(err)
	}
	built = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building aw-policy: " + err.Error() + "\n" + string(out))
	}
	// os.Exit does not run deferred calls, so the cleanup has to happen
	// after m.Run returns and before Exit is called.
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// bundle is a rendered aw-bundle.json with one rule everyone gets.
const bundle = `{"version":"v1","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`

// run executes the helper the way Claude Code does: no arguments, stdout
// and stderr captured separately. env is added to a minimal environment
// that points every cache and home directory at temporary ones.
func run(t *testing.T, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(built)
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
		t.Fatalf("running aw-policy: %v", err)
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
	stdout, stderr, code := run(t, "AW_POLICY_BUNDLE="+filepath.Join(t.TempDir(), "absent-bundle.json"))

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
	// Claude Code fails the run past 1 MiB of stderr, so notes are bounded
	// however much goes wrong.
	_, stderr, _ := run(t, "AW_POLICY_BUNDLE="+writeFile(t, "aw-bundle.json", "{"+strings.Repeat("x", 100<<10)))

	if len(stderr) > 64<<10 {
		t.Errorf("stderr is %d bytes", len(stderr))
	}
}
