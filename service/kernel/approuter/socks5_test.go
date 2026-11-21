package approuter

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// fakeSocks5 answers a CONNECT with the reply the test asks for.
func fakeSocks5(t *testing.T, reply []byte) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var greeting [3]byte
		if _, err := io.ReadFull(conn, greeting[:]); err != nil {
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		var head [10]byte // version, command, reserved, type, 4-byte address, port
		if _, err := io.ReadFull(conn, head[:]); err != nil {
			return
		}
		got <- net.JoinHostPort(net.IPv4(head[4], head[5], head[6], head[7]).String(),
			strconv.Itoa(int(head[8])<<8|int(head[9])))
		_, _ = conn.Write(reply)
		_, _ = conn.Write([]byte("payload"))
	}()
	return ln.Addr().String(), got
}

func TestSocks5DialAsksForTheDestinationTheApplicationWanted(t *testing.T) {
	addr, got := fakeSocks5(t, []byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	conn, err := socks5Dial(addr, net.IPv4(203, 0, 113, 7), 8443)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case dst := <-got:
		if dst != "203.0.113.7:8443" {
			t.Fatalf("the inbound was asked for %s", dst)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no request reached the inbound")
	}
	payload := make([]byte, 7)
	if _, err := io.ReadFull(conn, payload); err != nil {
		t.Fatalf("read through the class inbound: %v", err)
	}
	if string(payload) != "payload" {
		t.Fatalf("read %q", payload)
	}
}

// A refusal from the inbound is reported, not swallowed: the relay has to
// count it and say why.
func TestSocks5DialReportsARefusal(t *testing.T) {
	addr, _ := fakeSocks5(t, []byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
	conn, err := socks5Dial(addr, net.IPv4(203, 0, 113, 7), 443)
	if err == nil {
		conn.Close()
		t.Fatal("a refused CONNECT was reported as success")
	}
}

func TestSocks5DialReportsAnAuthenticationRefusal(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var greeting [3]byte
		_, _ = io.ReadFull(conn, greeting[:])
		_, _ = conn.Write([]byte{5, 0xff})
	}()
	if _, err := socks5Dial(ln.Addr().String(), net.IPv4(1, 1, 1, 1), 80); err == nil {
		t.Fatal("a refused greeting was reported as success")
	}
}
