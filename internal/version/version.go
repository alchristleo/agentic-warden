// Package version reports the release a binary was built as.
package version

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// Version is set at link time:
//
//	-ldflags "-X github.com/alchristleo/agentic-warden/internal/version.Version=v1.2.3"
var Version string

// pseudo matches the tail of a pseudo-version: yyyymmddhhmmss-abcdefabcdef.
var pseudo = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}$`)

// clean reports whether v is a plain module version: not empty, not "(devel)",
// no build metadata such as +dirty, and not a pseudo-version.
func clean(v string) bool {
	return v != "" && v != "(devel)" && !strings.Contains(v, "+") && !pseudo.MatchString(v)
}

// String is Version when stamped, else the clean module version recorded by
// `go install module@version`, else "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && clean(info.Main.Version) {
		return info.Main.Version
	}
	return "dev"
}
