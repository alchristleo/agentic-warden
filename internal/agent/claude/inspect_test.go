package claude_test

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/signing"
)

// signedBundle is a minimal bundle body: the signature check cares only
// about the bytes, not the schema, so this is enough to exercise it.
const signedBundle = `{"version":"2026-09-22.sig","groups":["platform"],"rules":[]}`

// writeSignedState builds a state directory the way aw-sync leaves one: the
// bundle, its signature, and the public key it verifies against.
func (in *inspection) writeSignedState(t *testing.T, stateDir, body string) {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	in.write(t, filepath.Join(stateDir, "aw-trust.pub"), signing.FormatPublic(pub)+"\n")
	in.write(t, filepath.Join(stateDir, "aw-bundle.json"), body)
	line := fmt.Sprintf("aw-ed25519 %s %s\n", signing.KeyID(pub), signing.Sign(key, []byte(body)))
	in.write(t, filepath.Join(stateDir, "aw-bundle.json.sig"), line)
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

// inspection sets up a managed system directory and a Claude config
// directory under the test's control and returns an adapter aimed at them.
type inspection struct {
	adapter   *claude.Adapter
	systemDir string
	configDir string
}

func newInspection(t *testing.T) *inspection {
	t.Helper()
	a := claude.New()
	a.SystemDir = t.TempDir()
	a.ConfigDir = t.TempDir()
	return &inspection{adapter: a, systemDir: a.SystemDir, configDir: a.ConfigDir}
}

func (in *inspection) write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (in *inspection) helperBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aw-policy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// helperSaying is a fake helper whose `build-info` answer is script; the
// no-argument path still prints an envelope so the other checks hold.
func (in *inspection) helperSaying(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "aw-policy")
	body := "#!/bin/sh\nif [ \"$1\" = build-info ]; then " + script + "; fi\necho '{}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func TestInspectReportsAWorkingPolicyHelper(t *testing.T) {
	in := newInspection(t)
	helper := in.helperBinary(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.OK, helper) {
		t.Errorf("findings %v should confirm the helper at %s", findings, helper)
	}
	if !findingsWith(findings, agent.OK, "50-agent-wrapper.json") {
		t.Errorf("findings %v should name the file that configures it", findings)
	}
}

func TestInspectWarnsWhenNoPolicyHelperIsConfigured(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"), `{"model":"opus"}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "no policyHelper") {
		t.Errorf("findings %v should warn that bare `claude` is ungoverned", findings)
	}
}

func TestInspectFailsWhenTheHelperPathIsNotExecutable(t *testing.T) {
	// This is the configuration that makes Claude Code refuse to start on
	// every launch: worth the loudest level doctor has.
	in := newInspection(t)
	missing := filepath.Join(t.TempDir(), "aw-policy")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"),
		`{"policyHelper":{"path":"`+missing+`"}}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Error, missing) {
		t.Errorf("findings %v should report the missing helper as an error", findings)
	}
}

func TestInspectDetectsServerManagedSettingsShadowingTheHelper(t *testing.T) {
	in := newInspection(t)
	helper := in.helperBinary(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)
	in.write(t, filepath.Join(in.configDir, "remote-settings.json"), `{"model":"sonnet"}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v should warn that a server-managed payload shadows the helper", findings)
	}
}

func TestInspectHonoursTheConfigDirFromTheEnvironment(t *testing.T) {
	in := newInspection(t)
	in.adapter.ConfigDir = ""
	other := t.TempDir()
	in.write(t, filepath.Join(other, "remote-settings.json"), `{"model":"sonnet"}`)

	findings := in.adapter.Inspect([]string{"CLAUDE_CONFIG_DIR=" + other, "HOME=" + t.TempDir()})

	if !findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v should look in CLAUDE_CONFIG_DIR", findings)
	}
}

func TestInspectNotesWhenTheServerManagedFetchIsSkipped(t *testing.T) {
	// A custom base URL makes Claude Code skip the server-managed fetch, so
	// a stale remote-settings.json cannot shadow the helper on this machine.
	in := newInspection(t)
	in.write(t, filepath.Join(in.configDir, "remote-settings.json"), `{"model":"sonnet"}`)

	findings := in.adapter.Inspect([]string{"ANTHROPIC_BASE_URL=https://gateway.example.com"})

	if !findingsWith(findings, agent.OK, "ANTHROPIC_BASE_URL") {
		t.Errorf("findings %v should note that the fetch is skipped", findings)
	}
	if findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v should not warn about shadowing when the fetch is skipped", findings)
	}
}

func TestInspectAnEmptyRemoteSettingsCacheDoesNotShadow(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.configDir, "remote-settings.json"), `{}`)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v: an empty payload delivers no policy key and shadows nothing", findings)
	}
}

func TestInspectWarnsWhenThereIsNoBundle(t *testing.T) {
	in := newInspection(t)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Warn, "aw-bundle.json") {
		t.Errorf("findings %+v should warn that no bundle has been synced", findings)
	}
}

func TestInspectReportsTheBundleVersion(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"),
		`{"version":"2026-09-21.1","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.OK, "version 2026-09-21.1") || !findingsWith(findings, agent.OK, "1 rule") {
		t.Errorf("findings %+v should report the bundle's version and rule count", findings)
	}
}

func TestInspectWarnsWhenTheBundleHasNoRules(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"), `{"version":"v1","groups":[],"rules":[]}`)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Warn, "no rules") {
		t.Errorf("findings %+v should warn that a rule-less bundle enforces nothing", findings)
	}
}

func TestInspectFlagsAnUnparseableBundle(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"), "{not json")

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Error, "not valid JSON") {
		t.Errorf("findings %+v should flag the bundle aw-policy cannot read", findings)
	}
}

func TestInspectWarnsWhenTheHelperWasBuiltWithEnvOverrides(t *testing.T) {
	in := newInspection(t)
	helper := in.helperSaying(t, "echo env-overrides=on; exit 0")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "-tags awtest") {
		t.Errorf("findings %v should warn that the helper honours a developer's AW_POLICY_BUNDLE", findings)
	}
}

func TestInspectSaysNothingAboutAReleaseHelper(t *testing.T) {
	in := newInspection(t)
	helper := in.helperSaying(t, "echo env-overrides=off; exit 0")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "awtest") {
		t.Errorf("findings %v warn about a release build", findings)
	}
}

func TestInspectSaysNothingWhenTheHelperCannotAnswerBuildInfo(t *testing.T) {
	// The drop-in may name a helper that is not ours; failing on an unknown
	// argument is not evidence of anything.
	in := newInspection(t)
	helper := in.helperSaying(t, "echo unknown argument >&2; exit 1")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "awtest") {
		t.Errorf("findings %v warn on a probe that failed", findings)
	}
	if !findingsWith(findings, agent.OK, helper) {
		t.Errorf("findings %v should still confirm the helper binary", findings)
	}
}

func TestInspectReportsAnUnsignedDeploymentWhenThereIsNoStateDir(t *testing.T) {
	// The zero value (no StateDir configured) is what every other Inspect
	// test above already relies on, and it must read as "signing was never
	// turned on here", exactly like a real machine with no trust file.
	in := newInspection(t)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.OK, "bundle signature: unsigned deployment") {
		t.Errorf("findings %+v should report an unsigned deployment", findings)
	}
}

func TestInspectReportsAnUnsignedDeploymentWhenThereIsNoTrustFile(t *testing.T) {
	in := newInspection(t)
	in.adapter.StateDir = t.TempDir()

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.OK, "bundle signature: unsigned deployment") {
		t.Errorf("findings %+v should report an unsigned deployment", findings)
	}
}

func TestInspectReportsAVerifiedBundleSignature(t *testing.T) {
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.OK, "bundle signature: verified (v1, key ") {
		t.Errorf("findings %+v should report a verified signature", findings)
	}
}

func TestInspectFlagsAFailedBundleSignature(t *testing.T) {
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)
	tamperByte(t, filepath.Join(stateDir, "aw-bundle.json"))

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Error, "bundle signature: FAILED") {
		t.Errorf("findings %+v should flag the tampered bundle as a failure", findings)
	}
	if !findingsWith(findings, agent.Error, "aw-bundle.json") {
		t.Errorf("findings %+v should name the file that failed to verify", findings)
	}
}

func TestInspectFlagsAnUnparseableTrustFile(t *testing.T) {
	// The trust file itself did not check out, so there is no key ID to
	// report a mismatch against; the message must still say something
	// sensible rather than naming a key it never got.
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.write(t, filepath.Join(stateDir, "aw-trust.pub"), "not a real key\n")
	in.write(t, filepath.Join(stateDir, "aw-bundle.json"), signedBundle)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Error, "bundle signature: FAILED") {
		t.Errorf("findings %+v should flag the unparseable trust file", findings)
	}
	if !findingsWith(findings, agent.Error, "aw-trust.pub") {
		t.Errorf("findings %+v should name the trust file that failed to parse", findings)
	}
	if findingsWith(findings, agent.Error, "does not match key") {
		// The old bug: with no key ID to show, the message fell back to
		// "does not match key " with a trailing space and nothing after it.
		t.Errorf("findings %+v should not claim a key mismatch when there is no key to compare", findings)
	}
}

func TestInspectWarnsWhenTheBundleIsWorldWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits carry no meaning on Windows")
	}
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)
	bundlePath := filepath.Join(stateDir, "aw-bundle.json")
	if err := os.Chmod(bundlePath, 0o646); err != nil {
		t.Fatal(err)
	}

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, bundlePath) {
		t.Errorf("findings %+v should warn that %s is writable by more than its owner", findings, bundlePath)
	}
}

func TestInspectWarnsWhenTheTrustFileIsWorldWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits carry no meaning on Windows")
	}
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)
	trustPath := filepath.Join(stateDir, "aw-trust.pub")
	if err := os.Chmod(trustPath, 0o664); err != nil {
		t.Fatal(err)
	}

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, trustPath) {
		t.Errorf("findings %+v should warn that %s is writable by more than its owner", findings, trustPath)
	}
}

func TestInspectDoesNotWarnWhenTheStateFilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits carry no meaning on Windows")
	}
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)
	for _, name := range []string{"aw-bundle.json", "aw-trust.pub"} {
		if err := os.Chmod(filepath.Join(stateDir, name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "writable") {
		t.Errorf("findings %+v should not warn about ordinary owner-writable, world-readable files", findings)
	}
}

func TestInspectWarnsWhenTheTrustFileIsNotOwnedByRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("there are no uids on Windows; ownership there is reported as unchecked")
	}
	if os.Getuid() == 0 {
		t.Skip("this test needs a non-root owner, and everything this run writes is root's")
	}
	// The mode bits are impeccable — 0644, writable only by its owner — and
	// the signature verifies. The problem is who the owner is: a deploy that
	// chowned the state directory lets that account delete all three files,
	// generate a key of its own and write a bundle that verifies perfectly
	// against it, which doctor would otherwise report as "verified".
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)
	trustPath := filepath.Join(stateDir, "aw-trust.pub")
	if err := os.Chmod(trustPath, 0o644); err != nil {
		t.Fatal(err)
	}

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.OK, "bundle signature: verified (v1, key ") {
		t.Fatalf("findings %+v should still verify; the point of the test is that verification alone is not enough", findings)
	}
	if !findingsWith(findings, agent.Warn, trustPath) || !findingsWith(findings, agent.Warn, "not root") {
		t.Errorf("findings %+v should warn that %s is owned by someone other than root", findings, trustPath)
	}
}

func TestInspectReportsOwnershipAsUncheckedOnWindows(t *testing.T) {
	// Windows has no uid behind the file and an ACL this build cannot read,
	// so the honest answer is "unknown". Reporting nothing would read as a
	// check that ran and passed, which on a C:\ProgramData subtree with
	// inherited ACLs is exactly the wrong thing to tell an operator.
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.adapter.GOOS = "windows"
	in.writeSignedState(t, stateDir, signedBundle)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "unchecked on Windows") {
		t.Errorf("findings %+v should say plainly that ownership is unchecked on Windows", findings)
	}
}

func TestInspectWarnsWhenNoKeyIsPinned(t *testing.T) {
	// machine.json is 0600 and doctor runs as the developer, so the empty
	// pin is derived from what a developer can see: aw-sync has left a
	// bundle in the state directory but no trust key beside it.
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.write(t, filepath.Join(stateDir, "aw-bundle.json"), signedBundle)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "enrolled before bundle signing; re-enroll to pin a key") {
		t.Errorf("findings %+v should warn about a machine that pins no signing key", findings)
	}
}

func TestInspectDoesNotWarnAboutThePinOnASignedMachine(t *testing.T) {
	in := newInspection(t)
	stateDir := t.TempDir()
	in.adapter.StateDir = stateDir
	in.writeSignedState(t, stateDir, signedBundle)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "re-enroll to pin a key") {
		t.Errorf("findings %+v warn about the pin on a machine that has one", findings)
	}
}

func TestInspectDoesNotWarnAboutThePinBeforeAwSyncHasRun(t *testing.T) {
	// No bundle either: that is a machine aw-sync has not reached, which
	// inspectBundle already reports, and nothing yet says anything about
	// whether it pinned a key.
	in := newInspection(t)
	in.adapter.StateDir = t.TempDir()

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "re-enroll to pin a key") {
		t.Errorf("findings %+v warn about a pin on a machine aw-sync has never run on", findings)
	}
}
