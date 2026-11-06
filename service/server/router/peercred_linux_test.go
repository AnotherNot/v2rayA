//go:build linux

package router

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestPeerCredFromConn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peer.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var server net.Conn
	select {
	case server = <-accepted:
	case err := <-acceptErr:
		t.Fatal(err)
	}
	defer server.Close()

	cred, isUnix, err := peerCredFromConn(server)
	if err != nil {
		t.Fatal(err)
	}
	if !isUnix {
		t.Fatal("a unix connection was not detected")
	}
	if cred.uid != uint32(os.Geteuid()) {
		t.Fatalf("peer uid = %d, want %d", cred.uid, os.Geteuid())
	}
}
