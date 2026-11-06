package router

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// prepareUnixSocket makes sure the path is either free or a socket this
// process owns. Removing an arbitrary file as root would let a local user
// turn a mistaken --socket path into a file deletion.
func prepareUnixSocket(path string) error {
	if path == "" {
		return nil
	}
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("unix socket directory %v: %w", parent, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("unix socket directory %v is not a directory", parent)
	}
	if info.Mode().Perm()&0002 != 0 && info.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("unix socket directory %v is world-writable; use a private directory for --socket", parent)
	}

	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unix socket %v: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace %v: it is not a socket", path)
	}
	if !socketOwnedByCurrentUser(info) {
		return fmt.Errorf("refusing to replace %v: it belongs to another user", path)
	}
	return os.Remove(path)
}

// unixSocketMode returns the file mode and the group that may use the
// socket. Without --socket-group the socket is owner-only; with a group it
// is group-readable and writable. The peer-credential check is disabled in
// the group case, because the kernel already limits access to the group.
func unixSocketMode(group string) (os.FileMode, int, error) {
	if strings.TrimSpace(group) == "" {
		return 0600, -1, nil
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, -1, fmt.Errorf("unix socket group %q: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, -1, fmt.Errorf("unix socket group %q has a non-numeric gid %q", group, g.Gid)
	}
	return 0660, gid, nil
}

func secureUnixSocket(path, group string) error {
	mode, gid, err := unixSocketMode(group)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod unix socket %v: %w", path, err)
	}
	if gid >= 0 {
		if err := os.Chown(path, -1, gid); err != nil {
			return fmt.Errorf("chown unix socket %v: %w", path, err)
		}
	}
	log.Alert("unix socket %v is ready with mode %04o", path, mode.Perm())
	return nil
}

// peerCred is the kernel's view of the process on the other end of a unix
// socket (SO_PEERCRED on Linux).
type peerCred struct {
	uid uint32
	gid uint32
}

type peerCredKey struct{}

func peerCredFromContext(ctx context.Context) (peerCred, bool) {
	cred, ok := ctx.Value(peerCredKey{}).(peerCred)
	return cred, ok
}
