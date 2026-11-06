//go:build !linux

package router

import "net"

func peerCredFromConn(net.Conn) (peerCred, bool, error) {
	return peerCred{}, false, nil
}
