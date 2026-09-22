package policyhelper_test

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/policyhelper"
	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/sync"
)

// bundle is what aw-sync leaves for a user in the platform group: a
// baseline rule and one scoped to the payments repositories, with the
// repository matcher intact for the helper to resolve per launch.
const bundle = `{
  "version": "2026-09-21.1",
  "groups": ["platform"],
  "rules": [
    {
      "name": "baseline",
      "agents": {"claude": {
        "managed": {"permissions": {"deny": ["Read(./.env)"]}, "allowManagedPermissionRulesOnly": true},
        "env": {"CLAUDE_CODE_ENABLE_TELEMETRY": "1"}
      }}
    },
    {
      "name": "payments",
      "match": {"repos": ["github.com/acme/payments*"]},
      "agents": {"claude": {"managed": {"permissions": {"deny": ["Bash(curl *)"]}}}}
    }
  ]
}`

func writeBundle(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aw-bundle.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeSignedState builds a state directory the way aw-sync leaves one: the
// bundle, its signature, and the public key it verifies against.
func writeSignedState(t *testing.T, body string) string {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)
	dir := t.TempDir()
	writeStateFile(t, dir, sync.TrustFile, signing.FormatPublic(pub)+"\n")
	writeStateFile(t, dir, sync.BundleFile, body)
	line := fmt.Sprintf("aw-ed25519 %s %s\n", signing.KeyID(pub), signing.Sign(key, []byte(body)))
	writeStateFile(t, dir, sync.SignatureFile, line)
	return dir
}

func writeStateFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tamperByte flips one byte of path, invalidating any signature over its
// former contents without touching its length.
func tamperByte(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xff
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func config(t *testing.T, bundlePath string) policyhelper.Config {
	t.Helper()
	return policyhelper.Config{
		BundlePath: bundlePath,
		AuditDir:   t.TempDir(),
		WorkDir:    t.TempDir(), // not a repository
	}
}

func envelope(t *testing.T, r policyhelper.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Output, &out); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, r.Output)
	}
	return out
}

func managedOf(t *testing.T, r policyhelper.Result) map[string]any {
	t.Helper()
	managed, ok := envelope(t, r)["managedSettings"].(map[string]any)
	if !ok {
		t.Fatalf("output has no managedSettings object:\n%s", r.Output)
	}
	return managed
}

// denies returns permissions.deny as strings, or nothing when it is absent.
func denies(t *testing.T, managed map[string]any) []string {
	t.Helper()
	permissions, _ := managed["permissions"].(map[string]any)
	raw, _ := permissions["deny"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func hasNote(r policyhelper.Result, text string) bool {
	return strings.Contains(strings.Join(r.Notes, "\n"), text)
}

func TestTheBundleIsCompiledAndEmittedAsManagedSettings(t *testing.T) {
	r := policyhelper.Run(config(t, writeBundle(t, bundle)))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; notes %q", r.ExitCode, r.Notes)
	}
	if r.Source != policyhelper.SourceBundle || r.Version != "2026-09-21.1" {
		t.Errorf("source = %q version = %q, want bundle and 2026-09-21.1", r.Source, r.Version)
	}
	managed := managedOf(t, r)
	if got := denies(t, managed); len(got) != 1 || got[0] != "Read(./.env)" {
		t.Errorf("deny = %q; outside a repository only the baseline rule applies", got)
	}
	if managed["allowManagedPermissionRulesOnly"] != true {
		t.Errorf("managed settings lost a key: %v", managed)
	}
	env, _ := managed["env"].(map[string]any)
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("the policy's env was not carried into managed settings: %v", managed)
	}
}

func TestTheRepositorySelectsItsRules(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.RepoOverride = "github.com/acme/payments-api"

	r := policyhelper.Run(cfg)

	if got := strings.Join(denies(t, managedOf(t, r)), ","); got != "Read(./.env),Bash(curl *)" {
		t.Errorf("deny = %q; want the baseline and payments rules merged in order", got)
	}
}

func TestTheRepositoryIsDetectedFromTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = git@github.com:acme/payments-api.git\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config(t, writeBundle(t, bundle))
	cfg.WorkDir = root

	r := policyhelper.Run(cfg)

	if got := denies(t, managedOf(t, r)); len(got) != 2 {
		t.Errorf("deny = %q; the payments rule should apply inside its repository", got)
	}
}

func TestWithoutABundleTheEnvelopeIsEmpty(t *testing.T) {
	// A machine aw-sync has not reached yet is governed by the static
	// managed-settings files alone. An envelope without managedSettings
	// leaves them in force; that is the safest thing to do, and it is
	// said out loud in the notes.
	r := policyhelper.Run(config(t, filepath.Join(t.TempDir(), "absent.json")))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if r.Source != policyhelper.SourceNone || string(r.Output) != "{}\n" {
		t.Errorf("source = %q output = %q, want none and an empty envelope", r.Source, r.Output)
	}
	if !hasNote(r, "no policy available") || !hasNote(r, "absent.json") {
		t.Errorf("notes %q should say there is no policy and name the file", r.Notes)
	}
}

func TestAnUnparseableBundleIsNotedAndNotEmitted(t *testing.T) {
	r := policyhelper.Run(config(t, writeBundle(t, "{not json")))

	if r.ExitCode != 0 || string(r.Output) != "{}\n" {
		t.Errorf("exit = %d output = %q, want 0 and an empty envelope", r.ExitCode, r.Output)
	}
	if !hasNote(r, "not valid JSON") {
		t.Errorf("notes %q should say the bundle does not parse", r.Notes)
	}
}

func TestASchemaViolationIsNeverEmitted(t *testing.T) {
	// aw-sync validated every rule when it wrote the file, so this guards
	// a newer schema in this binary; the answer is still no settings,
	// never settings that make Claude Code refuse to start.
	bad := `{"version":"v2","groups":[],"rules":[{"name":"bad","agents":{"claude":{"managed":{"permissions":{"deny":"Read(./.env)"}}}}}]}`

	r := policyhelper.Run(config(t, writeBundle(t, bad)))

	if r.ExitCode != 0 || r.Source != policyhelper.SourceNone || string(r.Output) != "{}\n" {
		t.Errorf("exit = %d source = %q output = %q, want 0, none and an empty envelope", r.ExitCode, r.Source, r.Output)
	}
	if !hasNote(r, "/permissions/deny") {
		t.Errorf("notes %q should say what was wrong", r.Notes)
	}
}

func TestRequireBundleFailsClosedWithoutABundle(t *testing.T) {
	cfg := config(t, filepath.Join(t.TempDir(), "absent.json"))
	cfg.RequireBundle = true

	r := policyhelper.Run(cfg)

	if r.ExitCode != 1 {
		t.Errorf("exit = %d, want 1: the organization asked to fail closed", r.ExitCode)
	}
	if string(r.Output) != "{}\n" {
		t.Errorf("output = %q; a valid envelope is printed even when refusing", r.Output)
	}
}

func TestRequireBundleIsSatisfiedByAUsableBundle(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.RequireBundle = true

	if r := policyhelper.Run(cfg); r.ExitCode != 0 {
		t.Errorf("exit = %d, want 0; notes %q", r.ExitCode, r.Notes)
	}
}

func TestOutputStaysUnderClaudeCodesLimit(t *testing.T) {
	// Claude Code reads at most 1 MiB from stdout; more fails the run.
	big := strings.Repeat("x", 2<<20)
	oversized := `{"version":"v1","groups":[],"rules":[{"name":"big","agents":{"claude":{"managed":{"model":"` + big + `"}}}}]}`

	r := policyhelper.Run(config(t, writeBundle(t, oversized)))

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", r.ExitCode)
	}
	if len(r.Output) >= 1<<20 {
		t.Errorf("output is %d bytes; an oversized policy must degrade, not brick", len(r.Output))
	}
	if !hasNote(r, "1 MiB") {
		t.Errorf("notes %q should say the policy was too large", r.Notes)
	}
}

func TestEveryRunIsAudited(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.RepoOverride = "github.com/acme/payments-api"
	cfg.Now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

	policyhelper.Run(cfg)

	raw, err := os.ReadFile(filepath.Join(cfg.AuditDir, "aw-policy.log"))
	if err != nil {
		t.Fatalf("no audit log: %v", err)
	}
	var entry struct {
		Time     time.Time `json:"time"`
		Source   string    `json:"source"`
		Version  string    `json:"version"`
		Groups   []string  `json:"groups"`
		Repo     string    `json:"repo"`
		ExitCode int       `json:"exitCode"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &entry); err != nil {
		t.Fatalf("audit line is not JSON: %v\n%s", err, raw)
	}
	if entry.Source != "bundle" || entry.Version != "2026-09-21.1" || entry.Repo != "github.com/acme/payments-api" ||
		len(entry.Groups) != 1 || entry.Groups[0] != "platform" || entry.ExitCode != 0 || !entry.Time.Equal(cfg.Now()) {
		t.Errorf("audit entry = %+v; want the bundle's groups and version, the repository and the time", entry)
	}
}

func TestNoAuditDirMeansNoAuditAndNoFailure(t *testing.T) {
	cfg := config(t, writeBundle(t, bundle))
	cfg.AuditDir = ""

	if r := policyhelper.Run(cfg); r.ExitCode != 0 || r.Source != policyhelper.SourceBundle {
		t.Errorf("exit = %d source = %q; auditing is best effort", r.ExitCode, r.Source)
	}
}

func TestRunCompilesFromTheSignedBundle(t *testing.T) {
	cfg := config(t, filepath.Join(t.TempDir(), "unused-fallback.json"))
	cfg.StateDir = writeSignedState(t, bundle)
	// RepoOverride sidesteps repo detection so the only notes possible are
	// about the signature, which is what this test is checking.
	cfg.RepoOverride = "github.com/acme/other"

	r := policyhelper.Run(cfg)

	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; notes %q", r.ExitCode, r.Notes)
	}
	if r.Source != policyhelper.SourceBundle {
		t.Errorf("source = %q, want bundle", r.Source)
	}
	if got := denies(t, managedOf(t, r)); len(got) != 1 || got[0] != "Read(./.env)" {
		t.Errorf("deny = %q; the signed bundle's baseline rule should apply", got)
	}
	if len(r.Notes) != 0 {
		t.Errorf("notes = %q, want none: the signature checks out", r.Notes)
	}
}

func TestRunRefusesATamperedBundle(t *testing.T) {
	stateDir := writeSignedState(t, bundle)
	tamperByte(t, filepath.Join(stateDir, sync.BundleFile))
	cfg := config(t, filepath.Join(t.TempDir(), "unused-fallback.json"))
	cfg.StateDir = stateDir

	r := policyhelper.Run(cfg)

	if r.ExitCode != 0 {
		t.Errorf("exit = %d, want 0: a bad signature must not brick an otherwise unpoliced launch", r.ExitCode)
	}
	if string(r.Output) != "{}\n" {
		t.Errorf("output = %q, want the empty envelope", r.Output)
	}
	if !hasNote(r, filepath.Join(stateDir, sync.BundleFile)) {
		t.Errorf("notes %q should name the bundle that failed to verify", r.Notes)
	}
}

func TestRunRefusesAMissingSignature(t *testing.T) {
	stateDir := writeSignedState(t, bundle)
	if err := os.Remove(filepath.Join(stateDir, sync.SignatureFile)); err != nil {
		t.Fatal(err)
	}
	cfg := config(t, filepath.Join(t.TempDir(), "unused-fallback.json"))
	cfg.StateDir = stateDir

	r := policyhelper.Run(cfg)

	if r.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", r.ExitCode)
	}
	if string(r.Output) != "{}\n" {
		t.Errorf("output = %q, want the empty envelope", r.Output)
	}
	if !hasNote(r, filepath.Join(stateDir, sync.BundleFile)) {
		t.Errorf("notes %q should name the bundle left without a signature", r.Notes)
	}
}

func TestRequireBundleTurnsAFailureIntoAnError(t *testing.T) {
	stateDir := writeSignedState(t, bundle)
	tamperByte(t, filepath.Join(stateDir, sync.BundleFile))
	cfg := config(t, filepath.Join(t.TempDir(), "unused-fallback.json"))
	cfg.StateDir = stateDir
	cfg.RequireBundle = true

	r := policyhelper.Run(cfg)

	if r.ExitCode != 1 {
		t.Errorf("exit = %d, want 1: a failed signature is not a usable bundle", r.ExitCode)
	}
	if string(r.Output) != "{}\n" {
		t.Errorf("output = %q; a valid envelope is printed even when refusing", r.Output)
	}
}

func TestRunRefusesAnUnreadableTrustFile(t *testing.T) {
	// A trust file that exists but cannot be read (permission denied, here)
	// is a broken deployment, not an absent one; folding it into the
	// unsigned case would make VerifyBundle fail open exactly where its job
	// is to fail closed.
	stateDir := writeSignedState(t, bundle)
	trustPath := filepath.Join(stateDir, sync.TrustFile)
	if err := os.Chmod(trustPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(trustPath, 0o644) }) // TempDir cleanup needs to read it back
	cfg := config(t, filepath.Join(t.TempDir(), "unused-fallback.json"))
	cfg.StateDir = stateDir

	r := policyhelper.Run(cfg)

	if r.ExitCode != 0 {
		t.Errorf("exit = %d, want 0: an unreadable trust file must not brick an otherwise unpoliced launch", r.ExitCode)
	}
	if string(r.Output) != "{}\n" {
		t.Errorf("output = %q, want the empty envelope", r.Output)
	}
	if !hasNote(r, trustPath) {
		t.Errorf("notes %q should name the unreadable trust file", r.Notes)
	}
}

func TestNoTrustFileBehavesExactlyAsBefore(t *testing.T) {
	bundlePath := writeBundle(t, bundle)
	without := policyhelper.Run(config(t, bundlePath))

	cfg := config(t, bundlePath)
	cfg.StateDir = t.TempDir() // no trust file: an unsigned deployment

	with := policyhelper.Run(cfg)

	if string(with.Output) != string(without.Output) {
		t.Errorf("output = %q, want byte-identical to the pre-signing path %q", with.Output, without.Output)
	}
	if with.Source != without.Source || with.ExitCode != without.ExitCode {
		t.Errorf("source/exit = %q/%d, want %q/%d", with.Source, with.ExitCode, without.Source, without.ExitCode)
	}
}
