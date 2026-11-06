//go:build unix

package fileperm

import "golang.org/x/sys/unix"

// SecureUmask makes every file the process creates owner-only by default.
// The explicit chmod calls still run for files that already exist.
func SecureUmask() {
	unix.Umask(0077)
}
