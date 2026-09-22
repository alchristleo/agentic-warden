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
	"strings"
	"syscall"
	"testing"
	"time"
)

var built string

// e2eAdminToken is the admin bearer token the e2e server is configured with.
const e2eAdminToken = "e2e-admin-token"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "awd-e2e-*")
	if err != nil {
		panic(err)
	}

	built = filepath.Join(dir, "awd")
	if out, err := exec.Command("go", "build", "-o", built, ".").CombinedOutput(); err != nil {
		panic("building awd: " + err.Error() + "\n" + string(out))
	}
	// os.Exit does not run deferred calls, so the cleanup has to happen
	// after m.Run returns and before Exit is called, not as a defer above.
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
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
	cmd.Env = append(os.Environ(), "AWD_ADDR=127.0.0.1:0", "AWD_LOG_LEVEL=error", "AWD_ADMIN_TOKEN="+e2eAdminToken)
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
	return runAwdEnv(t, []string{"AWD_ADMIN_TOKEN=" + e2eAdminToken}, args...)
}

// runAwdEnv runs awd with extra environment on top of the process's own. It
// keeps stdout and stderr separate and returns whichever one the command
// actually wrote to: commands report failures on stderr alone (main's error
// path) and successful output on stdout alone (apply, enroll-token,
// machines, revoke), so no awd command mixes the two in a single run.
// Preferring stdout when non-empty lets a caller assert on a clean success
// value like enroll-token's piped token, while still surfacing stderr text
// for the error-path assertions the rest of this suite relies on.
func runAwdEnv(t *testing.T, extra []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(built, args...)
	cmd.Env = append(append(os.Environ(), "AWD_LOG_LEVEL=error"), extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		out = stderr.String()
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out, 0
	case errors.As(err, &exitErr):
		return out, exitErr.ExitCode()
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

func TestApplyRejectsClaudeSettingsThatBreakTheSchema(t *testing.T) {
	// Emitted by the policy helper, these settings would make Claude Code
	// refuse to start on every machine the rule reaches. The author must
	// hear about it here, from the server as well as the local check.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      claude:\n        managed:\n          permissions:\n            deny: Read(./.env)\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for a schema violation: %s", out)
	}
	if !strings.Contains(out, "/permissions/deny") {
		t.Errorf("output %q does not name the offending setting", out)
	}

	// The server enforces the same rule for a client that skipped the check.
	body := `{"version":"v2","rules":[{"name":"b","agents":{"claude":{"managed":{"permissions":{"deny":"x"}}}}}]}`
	req, err := http.NewRequest(http.MethodPost, s.url+"/v1/policy/revisions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("server status = %d, want 422", resp.StatusCode)
	}
}

func TestApplyRejectsCodexRequirementsWithAnUnknownKey(t *testing.T) {
	// A Codex rule goes through the same gate as a Claude one: an unknown
	// top-level requirements.toml key is a typo nobody would notice until
	// Codex ignored it on every machine, so apply and the server both stop it.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      codex:\n        managed:\n          allowed_sandbox_mode: [read-only]\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for an unknown Codex key: %s", out)
	}
	if !strings.Contains(out, `"allowed_sandbox_mode"`) {
		t.Errorf("output %q does not name the offending key", out)
	}

	body := `{"version":"v2","rules":[{"name":"b","agents":{"codex":{"managed":{"allowed_sandbox_mode":["read-only"]}}}}]}`
	req, err := http.NewRequest(http.MethodPost, s.url+"/v1/policy/revisions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("server status = %d, want 422", resp.StatusCode)
	}
}

func TestApplyRejectsAGeminiPolicyRuleWithoutAPriority(t *testing.T) {
	// A Gemini rule goes through the same gate as a Claude or Codex one.
	// Gemini's own loader refuses a policy file whose rule lacks a
	// priority, so the whole file would be ignored on every machine; apply
	// and the server both stop it in front of the author.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      gemini:\n        managed:\n          policies:\n            - toolName: run_shell_command\n              decision: deny\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for a Gemini rule without a priority: %s", out)
	}
	if !strings.Contains(out, "policies[0].priority") {
		t.Errorf("output %q does not name the offending field", out)
	}

	body := `{"version":"v2","rules":[{"name":"b","agents":{"gemini":{"managed":{"policies":[{"toolName":"*","decision":"deny"}]}}}}]}`
	req, err := http.NewRequest(http.MethodPost, s.url+"/v1/policy/revisions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("server status = %d, want 422", resp.StatusCode)
	}
}

func TestApplyRejectsALaunchDocumentForGemini(t *testing.T) {
	// launch is Codex's per-launch channel; Gemini applies managed at
	// launch, so a launch entry for it would do nothing. The author hears
	// that at apply time, from the client and the server alike.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      gemini:\n        launch:\n          x: 1\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for launch on gemini: %s", out)
	}
	if !strings.Contains(out, "launch") || !strings.Contains(out, "gemini") {
		t.Errorf("output %q does not name launch and the agent", out)
	}

	body := `{"version":"v2","rules":[{"name":"b","agents":{"gemini":{"launch":{"x":1}}}}]}`
	req, err := http.NewRequest(http.MethodPost, s.url+"/v1/policy/revisions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("server status = %d, want 422", resp.StatusCode)
	}
}

func TestApplyRejectsANullLeafInALaunchDocument(t *testing.T) {
	// A null in a codex launch document has no TOML form; apply must catch
	// it before it reaches a repository and blocks every launch there.
	s := startServer(t)
	path := writePolicy(t, "version: v1\nrules:\n  - name: baseline\n    agents:\n      codex:\n        launch:\n          sandbox_mode:\n")

	out, code := runAwd(t, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0, want non-zero for a null launch leaf: %s", out)
	}
	if !strings.Contains(out, "launch.sandbox_mode") {
		t.Errorf("output %q does not name launch.sandbox_mode", out)
	}
}

func TestApplyWithoutTheAdminTokenIsRefused(t *testing.T) {
	s := startServer(t)
	path := writePolicy(t, policyYAML)

	out, code := runAwdEnv(t, []string{"AWD_ADMIN_TOKEN="}, "apply", path, "--url", s.url)

	if code == 0 {
		t.Fatalf("apply exited 0 without a token: %s", out)
	}
	if !strings.Contains(out, "401") {
		t.Errorf("output %q should show the 401", out)
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

func TestEnrollTokenThenEnrollThenBundle(t *testing.T) {
	s := startServer(t)
	if out, code := runAwd(t, "apply", writePolicy(t, groupedPolicyYAML), "--url", s.url); code != 0 {
		t.Fatalf("apply: %s", out)
	}

	out, code := runAwd(t, "enroll-token", "alice@acme.com", "--url", s.url)
	if code != 0 {
		t.Fatalf("enroll-token exited %d: %s", code, out)
	}
	token := strings.TrimSpace(out)
	if token == "" || strings.ContainsAny(token, " \n") {
		t.Fatalf("stdout should be the token alone, got %q", out)
	}

	body := `{"token":"` + token + `","name":"e2e-host","os":"linux"}`
	resp, err := http.Post(s.url+"/v1/machines/enroll", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enroll status = %d", resp.StatusCode)
	}
	var enrolled struct {
		MachineID  string `json:"machineId"`
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&enrolled); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, s.url+"/v1/bundle", nil)
	req.Header.Set("Authorization", "Bearer "+enrolled.Credential)
	bundleResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer bundleResp.Body.Close()
	var bundle struct {
		User  string `json:"user"`
		Rules []struct{ Name string }
	}
	if err := json.NewDecoder(bundleResp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.User != "alice@acme.com" || len(bundle.Rules) != 2 {
		t.Errorf("bundle = %+v, want alice's baseline and platform rules", bundle)
	}

	list, code := runAwd(t, "machines", "--url", s.url)
	if code != 0 || !strings.Contains(list, enrolled.MachineID) || !strings.Contains(list, "e2e-host") {
		t.Errorf("machines output %q should list the enrolled machine", list)
	}

	if out, code := runAwd(t, "revoke", enrolled.MachineID, "--url", s.url); code != 0 || !strings.Contains(out, "revoked") {
		t.Errorf("revoke: %d %q", code, out)
	}
	after, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("bundle after revoke: %d, want 401", after.StatusCode)
	}
}

func TestEnrollTokenNeedsAUser(t *testing.T) {
	s := startServer(t)

	out, code := runAwd(t, "enroll-token", "--url", s.url)

	if code == 0 || !strings.Contains(out, "user") {
		t.Errorf("exit %d, output %q", code, out)
	}
}

const groupedPolicyYAML = `
version: "e2e-groups"
groups:
  alice@acme.com: [platform]
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
  - name: mobile
    match:
      groups: [mobile]
    agents:
      claude:
        managed:
          model: haiku
`

const groupsYAML = `
source: okta-export
members:
  alice@acme.com: [mobile]
  carol@acme.com: [platform]
`

func TestGroupsApplyThenTheBundleShowsTheUnion(t *testing.T) {
	s := startServer(t)
	if out, code := runAwd(t, "apply", writePolicy(t, groupedPolicyYAML), "--url", s.url); code != 0 {
		t.Fatalf("apply: %s", out)
	}
	path := writePolicy(t, groupsYAML)

	out, code := runAwd(t, "groups", "apply", path, "--url", s.url)
	if code != 0 || !strings.Contains(out, "okta-export") || !strings.Contains(out, "2 users") {
		t.Fatalf("groups apply exited %d: %s", code, out)
	}

	// alice is platform by the policy and mobile by the IdP: both rules apply.
	out, code = runAwd(t, "enroll-token", "alice@acme.com", "--url", s.url)
	if code != 0 {
		t.Fatal(out)
	}
	resp, err := http.Post(s.url+"/v1/machines/enroll", "application/json",
		strings.NewReader(`{"token":"`+strings.TrimSpace(out)+`","name":"h","os":"linux"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var enrolled struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&enrolled); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, s.url+"/v1/bundle", nil)
	req.Header.Set("Authorization", "Bearer "+enrolled.Credential)
	bundleResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer bundleResp.Body.Close()
	var bundle struct {
		Groups []string `json:"groups"`
		Rules  []struct{ Name string }
	}
	if err := json.NewDecoder(bundleResp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	if strings.Join(bundle.Groups, ",") != "mobile,platform" || len(bundle.Rules) != 3 {
		t.Errorf("bundle = %+v, want groups mobile,platform and baseline+platform+mobile rules", bundle)
	}

	summary, code := runAwd(t, "groups", "--url", s.url)
	if code != 0 || !strings.Contains(summary, "okta-export") || !strings.Contains(summary, "users: 2") {
		t.Errorf("groups exited %d: %s", code, summary)
	}
}

func TestGroupsWithoutASnapshotSaysSo(t *testing.T) {
	s := startServer(t)
	out, code := runAwd(t, "groups", "--url", s.url)
	if code != 0 || !strings.Contains(out, "no group snapshot") {
		t.Errorf("exit %d, output %q", code, out)
	}
	if out, code := runAwdEnv(t, []string{"AWD_ADMIN_TOKEN="}, "groups", "apply", writePolicy(t, groupsYAML), "--url", s.url); code == 0 {
		t.Errorf("groups apply without the admin token exited 0: %s", out)
	}
}
