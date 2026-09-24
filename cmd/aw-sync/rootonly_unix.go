//go:build unix

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// statOwner reports path's owner and mode, following symlinks. Tests
// replace it to simulate ownership they cannot create without root.
var statOwner = func(path string) (uid uint32, mode fs.FileMode, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("%s: no owner information", path)
	}
	return uint32(st.Uid), info.Mode(), nil
}

// rootOnly reports why a user other than root could replace path: path,
// with symlinks resolved, and every directory above it up to / must be
// owned by uid 0 and not writable by group or others. A root timer that
// runs a file, or reads a directory, that someone else can swap hands
// that someone root. A nil error means only root can change what path
// names.
func rootOnly(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	for p := resolved; ; p = filepath.Dir(p) {
		uid, mode, err := statOwner(p)
		if err != nil {
			return err
		}
		switch {
		case uid != 0:
			return fmt.Errorf("%s is owned by uid %d", p, uid)
		case mode.Perm()&0o022 != 0:
			return fmt.Errorf("%s is writable by group or others (mode %s)", p, mode.Perm())
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}
