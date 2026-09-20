package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveBinary resolves name against the PATH entries in env, or os.Environ
// when env is nil. A name containing a separator is treated as a path.
//
// Entries that resolve to the running executable are skipped. Organizations
// routinely install the wrapper on PATH under the agent's own name, and
// without this the wrapper would exec itself forever.
func ResolveBinary(name string, env []string) (string, error) {
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		abs, err := filepath.Abs(name)
		if err != nil {
			abs = name
		}
		if executable(abs) {
			return abs, nil
		}
		return "", fmt.Errorf("agent: %s: not found or not executable", name)
	}
	if env == nil {
		env = os.Environ()
	}
	self := selfPath()

	var searched []string
	for _, dir := range SearchPath(env) {
		searched = append(searched, dir)
		candidate := filepath.Join(dir, name)
		if !executable(candidate) {
			continue
		}
		if self != "" && resolvesTo(candidate, self) {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("agent: %q not found on PATH (searched: %s)", name,
		strings.Join(searched, string(filepath.ListSeparator)))
}

// SearchPath extracts the PATH directories from a KEY=VALUE environment.
func SearchPath(env []string) []string {
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && key == "PATH" {
			return filepath.SplitList(value)
		}
	}
	return nil
}

// selfPath returns the running executable with symlinks resolved, or "" when
// it cannot be determined. An empty result disables the recursion guard rather
// than failing the launch.
func selfPath() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		return resolved
	}
	return self
}

func resolvesTo(candidate, self string) bool {
	resolved, err := filepath.EvalSymlinks(candidate)
	return err == nil && resolved == self
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
