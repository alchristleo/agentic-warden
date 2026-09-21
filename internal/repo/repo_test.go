package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acme/agent-wrapper/internal/repo"
)

func gitDir(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const originConfig = `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:acme/payments-api.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "fork"]
	url = https://github.com/leo/payments-api.git
`

func TestDetectReadsTheOriginRemote(t *testing.T) {
	root := gitDir(t, originConfig)

	got, err := repo.Detect(root)

	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if want := "github.com/acme/payments-api"; got != want {
		t.Errorf("Detect() = %q, want %q", got, want)
	}
}

func TestDetectWalksUpFromASubdirectory(t *testing.T) {
	root := gitDir(t, originConfig)
	sub := filepath.Join(root, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Detect(sub)

	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if want := "github.com/acme/payments-api"; got != want {
		t.Errorf("Detect() = %q, want %q", got, want)
	}
}

func TestDetectOutsideARepositoryIsEmptyNotAnError(t *testing.T) {
	// Outside a repository there is no repo to target; a policy for "no
	// repo" is the baseline, and the helper must not treat that as failure.
	got, err := repo.Detect(t.TempDir())

	if err != nil {
		t.Fatalf("Detect() error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("Detect() = %q, want empty", got)
	}
}

func TestDetectFollowsAWorktreeGitFile(t *testing.T) {
	// A linked worktree has a .git file naming the real gitdir; the config
	// lives in the common directory above it.
	main := gitDir(t, originConfig)
	worktreeGit := filepath.Join(main, ".git", "worktrees", "feature")
	if err := os.MkdirAll(worktreeGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeGit, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+worktreeGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Detect(linked)

	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if want := "github.com/acme/payments-api"; got != want {
		t.Errorf("Detect() = %q, want %q", got, want)
	}
}

func TestDetectWithNoOriginFallsBackToTheFirstRemote(t *testing.T) {
	root := gitDir(t, "[remote \"upstream\"]\n\turl = https://gitlab.com/acme/x.git\n")

	got, err := repo.Detect(root)

	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if want := "gitlab.com/acme/x"; got != want {
		t.Errorf("Detect() = %q, want %q", got, want)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/payments-api.git":     "github.com/acme/payments-api",
		"https://github.com/acme/payments-api.git": "github.com/acme/payments-api",
		"https://github.com/acme/payments-api":     "github.com/acme/payments-api",
		"https://user:token@github.com/acme/x.git": "github.com/acme/x",
		"ssh://git@github.com:22/acme/x.git":       "github.com/acme/x",
		"ssh://git@github.com/acme/x/":             "github.com/acme/x",
		"HTTPS://GitHub.com/Acme/X.git":            "github.com/Acme/X",
		"git://github.com/acme/x.git":              "github.com/acme/x",
		"gitlab.example.com:group/sub/project.git": "gitlab.example.com/group/sub/project",
		"/home/leo/src/local-only":                 "/home/leo/src/local-only",
		"file:///home/leo/src/local-only":          "/home/leo/src/local-only",
		"  https://github.com/acme/spaced.git \n":  "github.com/acme/spaced",
	}
	for in, want := range cases {
		if got := repo.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
