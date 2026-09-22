package main

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/sync"
)

// signedTestBundle is a minimal, schema-valid bundle: enough for
// resolvePolicy to compile it once its signature checks out.
const signedTestBundle = `{"version":"2026-09-22.sig","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`

// writeSignedBundle builds a state directory the way aw-sync leaves one: the
// bundle, its signature, and the public key it verifies against.
func writeSignedBundle(t *testing.T, dir, body string) {
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

func TestResolvePolicyAcceptsAVerifiedBundle(t *testing.T) {
	stateDir := t.TempDir()
	writeSignedBundle(t, stateDir, signedTestBundle)
	t.Setenv("AW_SYNC_STATE_DIR", stateDir)

	doc, src, err := resolvePolicy(options{}, t.TempDir())

	if err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}
	if src.Kind != "bundle" || src.Version != "2026-09-22.sig" {
		t.Errorf("src = %+v; want the verified bundle's version", src)
	}
	if doc == nil {
		t.Fatal("resolvePolicy returned a nil document for a verified bundle")
	}
}

func TestResolvePolicyRefusesATamperedBundle(t *testing.T) {
	stateDir := t.TempDir()
	writeSignedBundle(t, stateDir, signedTestBundle)
	tamperByte(t, filepath.Join(stateDir, sync.BundleFile))
	t.Setenv("AW_SYNC_STATE_DIR", stateDir)

	_, _, err := resolvePolicy(options{}, t.TempDir())

	if err == nil {
		t.Fatal("resolvePolicy accepted a bundle whose signature does not match")
	}
	if !strings.Contains(err.Error(), sync.BundleFile) {
		t.Errorf("error %q should name the bundle file", err)
	}
}

func TestResolvePolicyUnchangedWithNoTrustFile(t *testing.T) {
	// A machine that has never seen a signature must behave exactly as it
	// did before signing existed: the bundle compiles with no verification.
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, sync.BundleFile), []byte(signedTestBundle), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AW_SYNC_STATE_DIR", stateDir)

	doc, src, err := resolvePolicy(options{}, t.TempDir())

	if err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}
	if src.Kind != "bundle" {
		t.Errorf("src = %+v; an unsigned deployment should still compile the bundle", src)
	}
	if doc == nil {
		t.Fatal("resolvePolicy returned a nil document for an unsigned bundle")
	}
}

func TestSyncSectionReadsAwSyncState(t *testing.T) {
	dir := t.TempDir()
	if err := sync.SaveMachine(dir, sync.Machine{Server: "http://awd", MachineID: "m1", Credential: "secret", Agents: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "aw-bundle.json")
	if err := os.WriteFile(bundle, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := sync.State{
		Server: "http://awd", MachineID: "m1", Agents: []string{"claude"},
		Version:  "v1",
		SyncedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		Files:    map[string]string{bundle: "not-the-hash-on-disk"},
		Error:    "boom",
	}
	if err := sync.SaveState(dir, state); err != nil {
		t.Fatal(err)
	}

	r, errText := syncSection(dir)

	if errText != "" {
		t.Fatalf("syncSection error = %q", errText)
	}
	if !r.Enrolled || r.MachineID != "m1" || r.Version != "v1" || r.Error != "boom" || !r.SyncedAt.Equal(state.SyncedAt) {
		t.Errorf("report = %+v; want the enrollment and last-cycle facts from state.json", r)
	}
	if !r.Drift || len(r.Files) != 1 || r.Files[0].State != "drift" {
		t.Errorf("report = %+v; want the tampered bundle reported as drift", r)
	}
}

func TestSyncSectionWithoutAStateDirectoryIsNotEnrolled(t *testing.T) {
	r, errText := syncSection(filepath.Join(t.TempDir(), "absent"))

	if errText != "" || r == nil || r.Enrolled {
		t.Errorf("report = %+v error = %q; a machine without aw-sync is not enrolled, and that is not an error", r, errText)
	}
}

func TestSyncSectionReportsAnUnreadableState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, errText := syncSection(dir)

	if r != nil || errText == "" {
		t.Errorf("report = %+v error = %q; a broken state.json is worth telling the developer about", r, errText)
	}
}

func TestAgeIsHumanReadable(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cases := map[time.Time]string{
		now.Add(-30 * time.Second): "30s ago",
		now.Add(-5 * time.Minute):  "5m0s ago",
		now.Add(-3 * time.Hour):    "3h0m0s ago",
		now.Add(-49 * time.Hour):   "2d1h ago",
		now.Add(30 * time.Second):  "0s ago",
		{}:                         "never",
	}
	for at, want := range cases {
		if got := age(at, now); got != want {
			t.Errorf("age(%v) = %q, want %q", at, got, want)
		}
	}
}

func TestTheClaudeAdapterLooksForTheFilenamesAwSyncWrites(t *testing.T) {
	// internal/agent/claude cannot import internal/sync — sync imports it
	// for SystemDir — so it carries its own copies of the three state-
	// directory filenames. Nothing in either package would notice them
	// drifting apart; what would notice is a fleet where doctor reports an
	// unsigned deployment on a machine that has been signing all along. This
	// package imports both, so it is where the two can be held together:
	// writeSignedBundle lays the files down under sync's names, and the
	// adapter has to find all three under its own.
	stateDir := t.TempDir()
	writeSignedBundle(t, stateDir, signedTestBundle)
	adapter := claude.New()
	adapter.SystemDir = t.TempDir()
	adapter.ConfigDir = t.TempDir()
	adapter.StateDir = stateDir

	findings := adapter.Inspect([]string{})

	var verified bool
	for _, f := range findings {
		if f.Level == agent.OK && strings.Contains(f.Message, "bundle signature: verified (key ") {
			verified = true
		}
	}
	if !verified {
		t.Errorf("findings %+v; the adapter's filenames have drifted from sync.TrustFile, sync.BundleFile or sync.SignatureFile", findings)
	}
}

func TestDoctorNotesThatAwSyncStateDirRedirectsTheReport(t *testing.T) {
	// The variable stays ungated in `aw` on purpose, but aw-policy ignores
	// it, so a report from a directory of the developer's choosing must not
	// read as a report about what is enforced.
	t.Setenv("AW_SYNC_STATE_DIR", t.TempDir())

	warn := redirectedStateDir()

	if warn == nil {
		t.Fatal("doctor said nothing about reporting on a redirected state directory")
	}
	if warn.Level != agent.Warn || !strings.Contains(warn.Message, "AW_SYNC_STATE_DIR") || !strings.Contains(warn.Message, "aw-policy") {
		t.Errorf("finding = %+v; it should name the variable and say aw-policy reads elsewhere", warn)
	}
}

func TestDoctorSaysNothingAboutTheStateDirWhenItIsNotRedirected(t *testing.T) {
	t.Setenv("AW_SYNC_STATE_DIR", "")

	if warn := redirectedStateDir(); warn != nil {
		t.Errorf("finding = %+v on a machine using the OS default; that is the ordinary case and needs no note", warn)
	}
}
