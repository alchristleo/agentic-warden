package main_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const versionVar = "github.com/alchristleo/agentic-warden/internal/version.Version"

// buildVersioned builds awd with ldflags (empty for an unstamped build) and
// returns the output of `awd "version"`.
func buildVersioned(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "awd")
	args := []string{"build", "-buildvcs=false", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	if out, err := exec.Command("go", append(args, ".")...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("awd version: %v", err)
	}
	return string(out)
}

func TestVersionCommandStamped(t *testing.T) {
	if got := buildVersioned(t, "-X "+versionVar+"=v9.9.9"); got != "v9.9.9\n" {
		t.Errorf("output = %q, want v9.9.9", got)
	}
}

func TestVersionCommandUnstamped(t *testing.T) {
	if got := buildVersioned(t, ""); got != "dev\n" {
		t.Errorf("output = %q, want dev", got)
	}
}
