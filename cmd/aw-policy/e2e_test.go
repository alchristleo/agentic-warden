package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	defer os.RemoveAll(dir)

	built = filepath.Join(dir, "aw-policy")
	if out, err := exec.Command("go", "build", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building aw-policy: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

// run executes the helper the way Claude Code does: no arguments, the
// version in the environment, stdout and stderr captured separately.
func run(t *testing.T, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(built)
	cmd.Env = append(env, "PATH="+os.Getenv("PATH"), "CLAUDE_CODE_VERSION=2.1.300")
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

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aw-policy.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func controlPlane(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func decode(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return out
}

func TestEmitsTheManagedSettingsEnvelope(t *testing.T) {
	cp := controlPlane(t, `{"version":"v1","agents":{"claude":{"managed":{"model":"opus"}}}}`)
	cfg := writeConfig(t, `{"serverUrl":"`+cp.URL+`","groups":["platform"]}`)

	stdout, stderr, code := run(t, "AW_POLICY_CONFIG="+cfg, "XDG_CACHE_HOME="+t.TempDir(), "HOME="+t.TempDir())

	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	managed, _ := decode(t, stdout)["managedSettings"].(map[string]any)
	if managed["model"] != "opus" {
		t.Errorf("managedSettings = %v, want the served policy", managed)
	}
}

func TestAMissingConfigStillExitsZero(t *testing.T) {
	// Deployed wrong, the helper must still let Claude Code start: an
	// empty envelope leaves the static managed-settings file in force.
	stdout, stderr, code := run(t, "AW_POLICY_CONFIG="+filepath.Join(t.TempDir(), "absent.json"),
		"XDG_CACHE_HOME="+t.TempDir(), "HOME="+t.TempDir())

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, has := decode(t, stdout)["managedSettings"]; has {
		t.Errorf("stdout = %s, want an envelope without managedSettings", stdout)
	}
	if !strings.Contains(stderr, "absent.json") {
		t.Errorf("stderr %q should name the missing configuration", stderr)
	}
}

func TestAnUnreachableControlPlaneStillExitsZero(t *testing.T) {
	cfg := writeConfig(t, `{"serverUrl":"http://127.0.0.1:1","timeoutMs":500}`)

	stdout, _, code := run(t, "AW_POLICY_CONFIG="+cfg, "XDG_CACHE_HOME="+t.TempDir(), "HOME="+t.TempDir())

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	decode(t, stdout)
}

func TestRequireFreshExitsNonZeroWithoutAPolicy(t *testing.T) {
	cfg := writeConfig(t, `{"serverUrl":"http://127.0.0.1:1","timeoutMs":500,"requireFresh":true}`)

	_, stderr, code := run(t, "AW_POLICY_CONFIG="+cfg, "XDG_CACHE_HOME="+t.TempDir(), "HOME="+t.TempDir())

	if code == 0 {
		t.Fatal("exit = 0, want non-zero: the organization asked to fail closed")
	}
	if stderr == "" {
		t.Error("stderr is empty; Claude Code shows it as the reason for refusing to start")
	}
}

func TestStderrStaysShort(t *testing.T) {
	// Claude Code fails the run past 1 MiB of stderr, so notes are bounded
	// however much goes wrong.
	cfg := writeConfig(t, `{"serverUrl":"http://127.0.0.1:1","timeoutMs":500}`)

	_, stderr, _ := run(t, "AW_POLICY_CONFIG="+cfg, "XDG_CACHE_HOME="+t.TempDir(), "HOME="+t.TempDir())

	if len(stderr) > 64<<10 {
		t.Errorf("stderr is %d bytes", len(stderr))
	}
}
