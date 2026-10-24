// Package version reports the release a binary was built as.
package version

import "runtime/debug"

// Version is set at link time:
//
//	-ldflags "-X github.com/alchristleo/agentic-warden/internal/version.Version=v1.2.3"
var Version string

// String is Version when stamped, else the module version recorded by
// `go install module@version`, else "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
