package sync

import (
	"errors"
	"path/filepath"
)

// LockFile is the file in the state directory that one aw-sync cycle holds
// at a time. It is never deleted: removing a lock file races a process that
// has just opened it, and the OS releases the lock itself when a holder
// exits, so a crashed cycle never leaves it stuck.
const LockFile = "aw-sync.lock"

// ErrLocked means another aw-sync cycle holds the lock right now.
var ErrLocked = errors.New("sync: another aw-sync cycle holds the lock")

// Lock takes the state directory's cycle lock without waiting. It returns
// ErrLocked when another process holds it, and an error satisfying
// errors.Is(err, fs.ErrNotExist) when stateDir does not exist. release
// gives the lock back; it is safe to defer.
func Lock(stateDir string) (release func(), err error) {
	return lockFile(filepath.Join(stateDir, LockFile))
}
