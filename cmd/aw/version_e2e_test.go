package main_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const versionVar = "github.com/alchristleo/agentic-warden/internal/version.Version"

// buildVersioned builds aw with ldflags (empty for an unstamped build) and
// returns the output of `aw "version"`.
func buildVersioned(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aw")
	args := []string{"build", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	if out, err := exec.Command("go", append(args, ".")...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("aw version: %v", err)
	}
	return string(out)
}

func TestVersionCommandStamped(t *testing.T) {
	if got := buildVersioned(t, "-X "+versionVar+"=v9.9.9"); got != "v9.9.9\n" {
		t.Errorf("output = %q, want v9.9.9", got)
	}
}

func TestVersionCommandUnstamped(t *testing.T) {
	want := wantUnstamped(t) + "\n"
	if got := buildVersioned(t, ""); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// wantUnstamped is what an unstamped build reports: the tag when the checkout
// is clean and exactly at one (Go stamps it from VCS), otherwise "dev".
func wantUnstamped(t *testing.T) string {
	t.Helper()
	tag, err := exec.Command("git", "describe", "--tags", "--exact-match").Output()
	if err != nil {
		return "dev"
	}
	if dirty, err := exec.Command("git", "status", "--porcelain").Output(); err != nil || len(dirty) > 0 {
		return "dev"
	}
	return strings.TrimSpace(string(tag))
}
