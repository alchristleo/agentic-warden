// Package repo identifies the repository a directory belongs to, for
// per-repository policy targeting.
//
// It reads the git configuration directly rather than running git: the policy
// helper runs on every launch under a timeout, and a subprocess is both the
// slow part and the part that can hang. Only the remote URL is needed, and the
// config file format for that is stable.
package repo

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Detect returns the normalized URL of the repository containing dir, or an
// empty string when dir is not inside one. Only an unreadable repository is
// an error.
//
// The "origin" remote is preferred; without one, the first remote in the
// config is used, so a repository cloned under another name still targets.
func Detect(dir string) (string, error) {
	gitDir, err := findGitDir(dir)
	if err != nil || gitDir == "" {
		return "", err
	}
	config := filepath.Join(commonDir(gitDir), "config")
	remotes, err := readRemotes(config)
	if err != nil {
		return "", err
	}
	if url, ok := remotes.byName["origin"]; ok {
		return Normalize(url), nil
	}
	if len(remotes.order) > 0 {
		return Normalize(remotes.byName[remotes.order[0]]), nil
	}
	return "", nil
}

// findGitDir walks up from dir to the nearest .git, resolving a worktree's
// .git file to the directory it names. It returns "" when there is none.
func findGitDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("repo: resolving %s: %w", dir, err)
	}
	for {
		candidate := filepath.Join(dir, ".git")
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate, nil
		case err == nil:
			return readGitFile(candidate, dir)
		case !errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("repo: reading %s: %w", candidate, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// readGitFile resolves a "gitdir: PATH" file, which is how a linked worktree
// and a submodule point at their real git directory.
func readGitFile(path, base string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("repo: reading %s: %w", path, err)
	}
	line := strings.TrimSpace(string(raw))
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", fmt.Errorf("repo: %s is not a gitdir pointer", path)
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return target, nil
}

// commonDir returns the directory holding the repository's config. For a
// linked worktree that is the main repository's git directory, named by the
// worktree's commondir file; otherwise it is gitDir itself.
func commonDir(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	common := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return common
}

type remotes struct {
	byName map[string]string
	order  []string
}

// readRemotes parses the [remote "name"] sections of a git config for their
// url. A missing config is not an error: a repository with no remotes has
// nothing to target.
func readRemotes(path string) (remotes, error) {
	out := remotes{byName: make(map[string]string)}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("repo: reading %s: %w", path, err)
	}
	defer f.Close()

	var current string // remote name of the section being read, if any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
			continue
		case line[0] == '[':
			current = remoteName(line)
		case current != "":
			key, value, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(key) != "url" {
				continue
			}
			if _, seen := out.byName[current]; !seen {
				out.order = append(out.order, current)
			}
			out.byName[current] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("repo: reading %s: %w", path, err)
	}
	return out, nil
}

// remoteName returns the name in a [remote "name"] header, or "" for any
// other section.
func remoteName(header string) string {
	body := strings.TrimSuffix(strings.TrimPrefix(header, "["), "]")
	rest, ok := strings.CutPrefix(body, "remote ")
	if !ok {
		return ""
	}
	return strings.Trim(strings.TrimSpace(rest), `"`)
}

// Normalize reduces a remote URL to host/path so that the same repository
// matches one pattern however it was cloned: scheme, credentials, port, a
// trailing .git and a trailing slash are dropped, and the host is lowercased
// while the path keeps its case, since some hosts distinguish it.
func Normalize(url string) string {
	url = strings.TrimSpace(url)

	if rest, ok := strings.CutPrefix(url, "file://"); ok {
		return strings.TrimSuffix(rest, "/")
	}
	if strings.HasPrefix(url, "/") {
		return strings.TrimSuffix(url, "/")
	}

	// scheme://[user[:pass]@]host[:port]/path
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
		if at := strings.LastIndex(url, "@"); at >= 0 && at < strings.Index(url, "/") {
			url = url[at+1:]
		}
		host, path, _ := strings.Cut(url, "/")
		if colon := strings.Index(host, ":"); colon >= 0 {
			host = host[:colon]
		}
		return strings.ToLower(host) + "/" + trimRepo(path)
	}

	// scp-like: [user@]host:path
	if at := strings.Index(url, "@"); at >= 0 && at < strings.Index(url, ":") {
		url = url[at+1:]
	}
	host, path, ok := strings.Cut(url, ":")
	if !ok {
		return trimRepo(url)
	}
	return strings.ToLower(host) + "/" + trimRepo(path)
}

func trimRepo(path string) string {
	path = strings.TrimSuffix(path, "/")
	return strings.TrimSuffix(path, ".git")
}
