// Package cache holds the files a launch generates.
//
// Generated files are content-addressed: the name carries a hash of the bytes,
// so two launches with different configuration can never overwrite each other's
// file, and two launches with identical configuration share one. Writes are
// atomic, so a reader never sees a half-written document.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// hashLength is how much of the content digest goes in a file name. Twelve
// hex characters is far past any realistic collision risk for one cache
// directory while keeping the path readable in diagnostic output.
const hashLength = 12

// Dir returns the cache directory for one agent's generated files, creating
// nothing. The agent name namespaces it so adapters cannot collide.
func Dir(agentName string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache: locating the user cache directory: %w", err)
	}
	return filepath.Join(base, "agent-wrapper", agentName), nil
}

// Write stores data in dir under a content-addressed name built from prefix
// and ext, creating dir when needed, and returns the path.
//
// Writing the same bytes again returns the same path and is not an error.
func Write(dir, prefix, ext string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("cache: creating %s: %w", dir, err)
	}
	digest := sha256.Sum256(data)
	name := fmt.Sprintf("%s-%s%s", prefix, hex.EncodeToString(digest[:])[:hashLength], ext)
	path := filepath.Join(dir, name)

	// The name already identifies the content, so an existing file is the
	// file we would write. Rewriting it would only risk disturbing a
	// concurrent reader.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if err := Replace(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// Replace writes data to path atomically, creating the parent directory when
// needed: the bytes land in a temporary file beside path and are renamed over
// it, so a concurrent reader sees the old file or the new one, never a
// partial write. The file is private to the user.
func Replace(path string, data []byte) error {
	return ReplaceMode(path, data, 0o600)
}

// MkdirMode creates path and any missing parents, then sets path's own mode
// explicitly. os.MkdirAll alone applies the process umask to every directory
// it creates, so under a strict umask (0077 is common for a root-run daemon
// under systemd hardening) a directory meant to be readable by other users
// comes out 0700 regardless of the mode passed in. Only the leaf gets the
// explicit chmod: the parents above it are system directories the caller
// does not own and must not change the mode of. On Windows Chmod only
// affects the read-only bit, which is fine: there is no umask to fight there.
func MkdirMode(path string, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("cache: creating %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("cache: setting permissions on %s: %w", path, err)
	}
	return nil
}

// ReplaceMode is Replace with an explicit file mode, for files that other
// users must read: a policy bundle that root writes and a developer's helper
// reads. The parent directory is created 0755 when the file is readable
// beyond its owner and 0700 otherwise, so a private file never lands in a
// directory that lists it and a shared file never lands in one that hides it.
func ReplaceMode(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	dirMode := fs.FileMode(0o700)
	if mode&0o044 != 0 {
		dirMode = 0o755
	}
	if err := MkdirMode(dir, dirMode); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cache: creating a temporary file in %s: %w", dir, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath) // no-op once the rename succeeds

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("cache: writing %s: %w", tempPath, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("cache: closing %s: %w", tempPath, err)
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		return fmt.Errorf("cache: setting permissions on %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("cache: publishing %s: %w", path, err)
	}
	return nil
}

// Prune deletes files in dir last modified longer ago than maxAge. A missing
// directory is not an error: there is nothing to prune.
func Prune(dir string, maxAge time.Duration) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cache: reading %s: %w", dir, err)
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // vanished under us; nothing to prune
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cache: removing %s: %w", entry.Name(), err)
		}
	}
	return nil
}
