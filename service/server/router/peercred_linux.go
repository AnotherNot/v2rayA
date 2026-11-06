//go:build linux

package router

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

func peerCredFromConn(c net.Conn) (peerCred, bool, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return peerCred{}, false, nil
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{}, true, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return peerCred{}, true, err
	}
	if credErr != nil {
		return peerCred{}, true, credErr
	}
	if cred == nil {
		return peerCred{}, true, errors.New("no peer credentials")
	}
	return peerCred{uid: cred.Uid, gid: cred.Gid}, true, nil
}
