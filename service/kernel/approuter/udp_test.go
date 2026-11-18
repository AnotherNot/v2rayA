package approuter

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeUDPStore is the kernel's flows map: a flow address names a destination
// and an action, and every datagram of that flow reads it.
type fakeUDPStore struct {
	mu    sync.Mutex
	flows map[uint32]orig
}

func newFakeUDPStore() *fakeUDPStore { return &fakeUDPStore{flows: map[uint32]orig{}} }

func (s *fakeUDPStore) record(addr uint32, o orig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows[addr] = o
}

func (s *fakeUDPStore) flow(addr uint32) (orig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.flows[addr]
	return o, ok, nil
}

type fakeUDPDecider struct {
	action Action
	class  int
}

func (d fakeUDPDecider) decide(orig) (Action, int) { return d.action, d.class }

// udpEcho answers every datagram with the same bytes, the destination a direct
// flow reaches.
func udpEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(buf[:n], from)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

// fakeSocksUDP is a SOCKS5 server that answers UDP ASSOCIATE and relays every
// framed datagram to the destination its header names, the way the class
// inbound does.
type fakeSocksUDP struct {
	ln   net.Listener
	seen chan string
}

func newFakeSocksUDP(t *testing.T) *fakeSocksUDP {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSocksUDP{ln: ln, seen: make(chan string, 16)}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSocksUDP) addr() string { return f.ln.Addr().String() }

func (f *fakeSocksUDP) serve(ctrl net.Conn) {
	defer ctrl.Close()
	var greeting [2]byte
	if _, err := io.ReadFull(ctrl, greeting[:]); err != nil {
		return
	}
	if _, err := io.ReadFull(ctrl, make([]byte, int(greeting[1]))); err != nil {
		return
	}
	if _, err := ctrl.Write([]byte{5, 0}); err != nil {
		return
	}
	// The request: CMD, RSV, ATYP, address, port. The address is the one the
	// client will send from, which the relay names as 0.0.0.0:0.
	var head [4]byte
	if _, err := io.ReadFull(ctrl, head[:]); err != nil {
		return
	}
	if head[1] != 3 || head[2] != 0 || head[3] != 1 {
		return
	}
	var addr [4]byte
	if _, err := io.ReadFull(ctrl, addr[:]); err != nil {
		return
	}
	var port [2]byte
	if _, err := io.ReadFull(ctrl, port[:]); err != nil {
		return
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return
	}
	defer udp.Close()
	bound := udp.LocalAddr().(*net.UDPAddr)
	reply := []byte{5, 0, 0, 1}
	reply = append(reply, bound.IP.To4()...)
	reply = binary.BigEndian.AppendUint16(reply, uint16(bound.Port))
	if _, err := ctrl.Write(reply); err != nil {
		return
	}
	buf := make([]byte, 65535)
	for {
		n, from, err := udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		host, dstPort, payload, err := splitUDP(buf[:n])
		if err != nil {
			continue
		}
		f.seen <- net.JoinHostPort(host, strconv.Itoa(dstPort))
		up, err := net.Dial("udp4", net.JoinHostPort(host, strconv.Itoa(dstPort)))
		if err != nil {
			continue
		}
		_ = up.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := up.Write(payload); err != nil {
			up.Close()
			continue
		}
		answer := make([]byte, 65535)
		an, err := up.Read(answer)
		up.Close()
		if err != nil {
			continue
		}
		out := make([]byte, 0, len(answer)+10)
		out = append(out, 0, 0, 0, 1)
		out = append(out, net.ParseIP(host).To4()...)
		out = binary.BigEndian.AppendUint16(out, uint16(dstPort))
		out = append(out, answer[:an]...)
		if _, err := udp.WriteToUDP(out, from); err != nil {
			return
		}
	}
}

// splitUDP reads a framed datagram back into its destination and payload. It is
// the test's own reader, so a mistake in unwrapUDP cannot hide here.
func splitUDP(datagram []byte) (string, int, []byte, error) {
	if len(datagram) < 4 || datagram[3] != 1 {
		return "", 0, nil, errors.New("not an IPv4 datagram")
	}
	if len(datagram) < 10 {
		return "", 0, nil, errors.New("truncated")
	}
	host := net.IP(datagram[4:8]).String()
	port := int(binary.BigEndian.Uint16(datagram[8:10]))
	return host, port, datagram[10:], nil
}

// udpRelayAround starts a relay on a real socket, so a flow address is a real
// address and a reply really travels from it.
func udpRelayAround(t *testing.T, store udpFlowStore, d decider, classPorts map[int]int) (*udpRelay, *net.UDPAddr) {
	t.Helper()
	r := newUDPRelay(store, d, classPorts)
	if err := r.listen("0.0.0.0:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.close() })
	go r.serve()
	return r, r.conn.LocalAddr().(*net.UDPAddr)
}

// exchange sends one datagram to a flow address through the relay and waits for
// the answer, which must come back from the same flow address.
func exchange(t *testing.T, relay *net.UDPAddr, flowAddr uint32, payload string) (string, net.IP) {
	t.Helper()
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.WriteToUDP([]byte(payload), &net.UDPAddr{
		IP:   net.IPv4(byte(flowAddr>>24), byte(flowAddr>>16), byte(flowAddr>>8), byte(flowAddr)),
		Port: relay.Port,
	}); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 65535)
	n, from, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	return string(buf[:n]), from.IP
}

// A direct UDP flow is dialled by the relay, and the answer reaches the
// application from the flow address it sent to.
func TestUDPRelayDialsADirectFlow(t *testing.T) {
	echo := udpEcho(t)
	store := newFakeUDPStore()
	_, relay := udpRelayAround(t, store, fakeUDPDecider{}, map[int]int{})
	const flow = 0x7f010203
	store.record(flow, orig{
		Addr: v4Addr(127, 0, 0, 1), Family: familyInet, Port: echo.Port, Action: ActionDirect, Network: NetworkUDP,
	})
	got, from := exchange(t, relay, flow, "ping")
	if got != "ping" {
		t.Fatalf("the answer was %q", got)
	}
	if !from.Equal(net.IPv4(127, 1, 2, 3)) {
		t.Fatalf("the answer came from %v, not the flow address", from)
	}
}

// A proxied UDP flow travels framed through the class inbound, which sees the
// destination the application asked for.
func TestUDPRelayHandsAProxiedFlowToTheClassInbound(t *testing.T) {
	echo := udpEcho(t)
	inbound := newFakeSocksUDP(t)
	store := newFakeUDPStore()
	_, relay := udpRelayAround(t, store, fakeUDPDecider{}, map[int]int{1: portOf(t, inbound.addr())})
	const flow = 0x7f010405
	store.record(flow, orig{
		Addr: v4Addr(127, 0, 0, 1), Family: familyInet, Port: echo.Port, Action: ActionProxy, InboundID: 1, Network: NetworkUDP,
	})
	got, _ := exchange(t, relay, flow, "through")
	if got != "through" {
		t.Fatalf("the answer was %q", got)
	}
	select {
	case dst := <-inbound.seen:
		if dst != net.JoinHostPort("127.0.0.1", strconv.Itoa(echo.Port)) {
			t.Fatalf("the inbound was asked for %s", dst)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the class inbound saw no datagram")
	}
}

// A UDP flow the kernel could not decide is decided in user space, and the
// answer for the first datagram is used for the flow.
func TestUDPRelayDecidesAFlowTheKernelCouldNot(t *testing.T) {
	echo := udpEcho(t)
	store := newFakeUDPStore()
	r, relay := udpRelayAround(t, store, fakeUDPDecider{action: ActionDirect}, map[int]int{})
	const flow = 0x7f010607
	store.record(flow, orig{
		Addr: v4Addr(127, 0, 0, 1), Family: familyInet, Port: echo.Port, Action: actionDecide, Network: NetworkUDP,
	})
	got, _ := exchange(t, relay, flow, "decide")
	if got != "decide" {
		t.Fatalf("the answer was %q", got)
	}
	if r.decided.Load() != 1 {
		t.Fatalf("decided=%d", r.decided.Load())
	}
}

// A blocked datagram is dropped: no answer, and the relay says it refused one.
func TestUDPRelayDropsABlockedFlow(t *testing.T) {
	echo := udpEcho(t)
	store := newFakeUDPStore()
	r, relay := udpRelayAround(t, store, fakeUDPDecider{action: ActionBlock}, map[int]int{})
	const flow = 0x7f010809
	store.record(flow, orig{
		Addr: v4Addr(127, 0, 0, 1), Family: familyInet, Port: echo.Port, Action: actionDecide, Network: NetworkUDP,
	})
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, _ = client.WriteToUDP([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 1, 8, 9), Port: relay.Port})
	_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 16)
	if _, _, err := client.ReadFromUDP(buf); err == nil {
		t.Fatal("a blocked datagram was answered")
	}
	if r.refused.Load() == 0 {
		t.Fatal("the refused datagram was not counted")
	}
}

// A datagram addressed to no flow is refused rather than guessed at.
func TestUDPRelayRefusesAnUnknownFlow(t *testing.T) {
	store := newFakeUDPStore()
	r, relay := udpRelayAround(t, store, fakeUDPDecider{}, map[int]int{})
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, _ = client.WriteToUDP([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 1, 9, 9), Port: relay.Port})
	deadline := time.Now().Add(2 * time.Second)
	for r.refused.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.refused.Load() == 0 {
		t.Fatal("a datagram for no flow was not refused")
	}
}

// The header RFC 1928 puts in front of a datagram: two reserved bytes, the
// fragment number and the destination.
func TestUDPAssociationFraming(t *testing.T) {
	framed := wrapUDP(net.IPv4(1, 2, 3, 4), 443, []byte("payload"))
	want := []byte{0, 0, 0, 1, 1, 2, 3, 4, 1, 187}
	if len(framed) != len(want)+len("payload") {
		t.Fatalf("framed %d bytes", len(framed))
	}
	for i := range want {
		if framed[i] != want[i] {
			t.Fatalf("header %v", framed[:10])
		}
	}
	payload, err := unwrapUDP(framed)
	if err != nil || string(payload) != "payload" {
		t.Fatalf("unwrapped %q, %v", payload, err)
	}
	// A fragment is never something the relay asks for, so it cannot be an
	// answer to what it sent.
	fragmented := append([]byte(nil), framed...)
	fragmented[2] = 1
	if _, err := unwrapUDP(fragmented); err == nil {
		t.Fatal("a fragmented datagram was accepted")
	}
	if _, err := unwrapUDP([]byte{0, 0}); err == nil {
		t.Fatal("a truncated datagram was accepted")
	}
}

// The relay asks the inbound for an association, and the datagrams it sends
// name the destination the flow stands for.
func TestUDPAssociationReachesTheInbound(t *testing.T) {
	echo := udpEcho(t)
	inbound := newFakeSocksUDP(t)
	assoc, err := socks5UDPAssociate(inbound.addr(), net.IPv4(127, 0, 0, 1), echo.Port)
	if err != nil {
		t.Fatal(err)
	}
	defer assoc.close()
	if err := assoc.send([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := assoc.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := assoc.receive(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("received %q", buf[:n])
	}
	select {
	case dst := <-inbound.seen:
		if dst != net.JoinHostPort("127.0.0.1", strconv.Itoa(echo.Port)) {
			t.Fatalf("the inbound was asked for %s", dst)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the inbound saw no datagram")
	}
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
