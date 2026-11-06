//go:build !linux

package fileperm

import "os"

func ownedByCurrentUser(os.FileInfo) bool {
	return true
}
