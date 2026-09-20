package main_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var built string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "awd-e2e-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	built = filepath.Join(dir, "awd")
	if out, err := exec.Command("go", "build", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building awd: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

// server starts awd on an ephemeral port and returns its base URL. The server
// is stopped when the test ends.
type server struct {
	url  string
	cmd  *exec.Cmd
	done chan error
}

func startServer(t *testing.T) *server {
	t.Helper()
	cmd := exec.Command(built, "serve")
	cmd.Env = append(os.Environ(), "AWD_ADDR=127.0.0.1:0", "AWD_LOG_LEVEL=error")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start awd: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// awd announces the address it actually bound, which is the only way to
	// learn the port when the configured one is zero.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the listen address: %v", err)
	}
	address := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "listening on "))
	if address == "" {
		t.Fatalf("awd did not announce a listen address, got %q", line)
	}

	s := &server{url: "http://" + address, cmd: cmd, done: done}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		io.Copy(io.Discard, stdout)
	})
	waitReady(t, s.url)
	return s
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("awd did not become healthy in time")
}

func runAwd(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(built, args...)
	cmd.Env = append(os.Environ(), "AWD_LOG_LEVEL=error")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running awd %v: %v", args, err)
		return "", 0
	}
}

const policyYAML = `
version: "e2e-1"
rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny: [Read(./.env)]
  - name: platform
    match:
      groups: [platform]
    agents:
      claude:
        managed:
          model: opus
`

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func TestServeAnnouncesWhereItIsListening(t *testing.T) {
	s := startServer(t)

	if !strings.HasPrefix(s.url, "http://127.0.0.1:") {
		t.Errorf("url = %q, want a loopback address with the bound port", s.url)
	}
}

func TestApplyingAPolicyThenFetchingItReturnsTheCompiledDocument(t *testing.T) {
	s := startServer(t)
	path := writePolicy(t, policyYAML)

	out, code := runAwd(t, "apply", path, "--url", s.url)
	if code != 0 {
		t.Fatalf("apply exited %d: %s", code, out)
	}

	resp, err := http.Get(s.url + "/v1/policy?group=platform")
	if err != nil {
		t.Fatalf("GET policy: %v", err)
	}
	defer resp.Body.Close()

	var doc struct {
		Version string `json:"version"`
		Agents  map[string]struct {
			Managed map[string]any `json:"managed"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Version != "e2e-1" {
		t.Errorf("version = %q, want %q", doc.Version, "e2e-1")
	}
	if got := doc.Agents["claude"].Managed["model"]; got != "opus" {
		t.Errorf("model = %v, want the platform rule applied", got)
	}
}

func TestApplyRejectsAnInvalidPolicyBeforeSendingIt(t *testing.T) {
	s := startServer(t)
	path := writePolicy(t, "rules:\n  - name: baseline\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for a policy with no version: %s", out)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("output %q does not explain the problem", out)
	}
}

func TestApplyReportsAConflictOnAResubmittedVersion(t *testing.T) {
	s := startServer(t)
	path := writePolicy(t, policyYAML)
	if out, code := runAwd(t, "apply", path, "--url", s.url); code != 0 {
		t.Fatalf("first apply exited %d: %s", code, out)
	}

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("second apply exited 0, want non-zero: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "conflict") && !strings.Contains(out, "e2e-1") {
		t.Errorf("output %q does not explain that the version already exists", out)
	}
}

func TestApplyAgainstAnUnreachableControlPlaneFails(t *testing.T) {
	path := writePolicy(t, policyYAML)

	out, code := runAwd(t, "apply", path, "--url", "http://127.0.0.1:1")

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero when the control plane is unreachable: %s", out)
	}
}

func TestServeShutsDownCleanlyOnSIGTERM(t *testing.T) {
	s := startServer(t)

	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}

	select {
	case err := <-s.done:
		if err != nil {
			t.Errorf("awd exited with %v, want a clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("awd did not exit within the shutdown timeout")
	}
}

func TestAnUnknownCommandFails(t *testing.T) {
	out, code := runAwd(t, "frobnicate")

	if code == 0 {
		t.Fatalf("exited 0, want non-zero: %s", out)
	}
}
