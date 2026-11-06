//go:build !linux

package router

import "os"

func socketOwnedByCurrentUser(info os.FileInfo) bool {
	return true
}
