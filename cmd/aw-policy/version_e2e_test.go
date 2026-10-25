package main_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const versionVar = "github.com/alchristleo/agentic-warden/internal/version.Version"

// buildVersioned builds aw-policy with ldflags (empty for an unstamped build) and
// returns the output of `aw-policy "build-info"`.
func buildVersioned(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aw-policy")
	args := []string{"build", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	if out, err := exec.Command("go", append(args, ".")...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "build-info").Output()
	if err != nil {
		t.Fatalf("aw-policy build-info: %v", err)
	}
	return string(out)
}

func TestVersionCommandStamped(t *testing.T) {
	if got := buildVersioned(t, "-X "+versionVar+"=v9.9.9"); got != "env-overrides=off\nversion v9.9.9\n" {
		t.Errorf("output = %q, want version line", got)
	}
}

func TestVersionCommandUnstamped(t *testing.T) {
	if got := buildVersioned(t, ""); got != "env-overrides=off\nversion dev\n" {
		t.Errorf("output = %q, want version dev line", got)
	}
}
