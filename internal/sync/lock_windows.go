//go:build windows

package sync

import (
	"errors"
	"fmt"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION, which the syscall package
// does not name.
const errSharingViolation syscall.Errno = 32

// lockFile opens path with no sharing at all, so any other open fails until
// this handle is closed; Windows closes it when the process exits.
func lockFile(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("sync: lock file path: %w", err)
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errSharingViolation) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("sync: opening the lock file: %w", err)
	}
	return func() { _ = syscall.CloseHandle(h) }, nil
}
