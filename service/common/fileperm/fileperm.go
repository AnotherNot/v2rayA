// Package fileperm keeps the service's own files and directories private.
// A v2rayA configuration directory holds the session secret, subscription
// tokens and node passwords, so it must not be readable by other local
// users.
package fileperm

import (
	"errors"
	"fmt"
	"os"
)

// OwnedByCurrentUser reports whether the file belongs to the process's
// effective user. It is exported for callers that decide whether a shell
// binary is safe to execute as root.
func OwnedByCurrentUser(info os.FileInfo) bool {
	return ownedByCurrentUser(info)
}

// EnsurePrivateDir creates path when missing and forces mode on an existing
// directory. A symlink is refused: chmod would otherwise follow it.
func EnsurePrivateDir(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, mode); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%v is a symlink, refusing to change its permissions", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%v is not a directory", path)
	}
	if !ownedByCurrentUser(info) && os.Geteuid() != 0 {
		return fmt.Errorf("%v belongs to another user", path)
	}
	return os.Chmod(path, mode)
}

// EnsurePrivateFile forces mode on an existing regular file. A missing
// file is not an error: WAL and shared-memory files appear after SQLite
// starts writing.
func EnsurePrivateFile(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%v is a symlink, refusing to change its permissions", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%v is not a regular file", path)
	}
	if !ownedByCurrentUser(info) && os.Geteuid() != 0 {
		return fmt.Errorf("%v belongs to another user", path)
	}
	return os.Chmod(path, mode)
}
