//go:build !windows

package claude

import (
	"io/fs"
	"syscall"
)

// ownerUID is the numeric owner of the file info describes, and whether the
// question could be answered at all. The type assertion is the only route to
// the uid the kernel reported, and syscall.Stat_t does not exist on Windows,
// which is why this lives in a build-tagged file rather than in inspect.go
// beside its caller.
func ownerUID(info fs.FileInfo) (uid int, known bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// A filesystem or a test double that reports no Stat_t is not an
		// owner this check can judge, and guessing would defeat the point.
		return 0, false
	}
	return int(st.Uid), true
}
