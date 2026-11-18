package approuter

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeStore stands in for the kernel's map: a flow arrives with a destination
// and an action recorded for its port, or it does not.
type fakeStore struct {
	mu    sync.Mutex
	flows map[portKey]orig
}

func newFakeStore() *fakeStore { return &fakeStore{flows: map[portKey]orig{}} }

func (s *fakeStore) record(key portKey, o orig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows[key] = o
}

func (s *fakeStore) take(key portKey) (orig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.flows[key]
	delete(s.flows, key)
	return o, ok, nil
}

// fakeDecider answers the flows the kernel could not, the way a planner does.
type fakeDecider struct {
	action Action
	class  int
}

func (d fakeDecider) decide(orig) (Action, int) { return d.action, d.class }

// fakeClassInbound answers SOCKS5 the way the core's inbound does: no
// authentication, CONNECT, then it echoes what it reads.
type fakeClassInbound struct {
	ln   net.Listener
	seen chan string
}

func newFakeClassInbound(t *testing.T) *fakeClassInbound {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeClassInbound{ln: ln, seen: make(chan string, 8)}
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

func (f *fakeClassInbound) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeClassInbound) serve(conn net.Conn) {
	defer conn.Close()
	var greeting [3]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return
	}
	var host string
	switch head[3] {
	case 1:
		var v4 [4]byte
		if _, err := io.ReadFull(conn, v4[:]); err != nil {
			return
		}
		host = net.IP(v4[:]).String()
	case 4:
		var v16 [16]byte
		if _, err := io.ReadFull(conn, v16[:]); err != nil {
			return
		}
		host = net.IP(v16[:]).String()
	default:
		return
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes[:])
	f.seen <- net.JoinHostPort(host, strconv.Itoa(int(port)))
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	_, _ = io.Copy(conn, conn)
}

// echoServer is a plain TCP echo, the destination a direct flow reaches.
func echoServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln
}

// relayAround starts the relay's per-connection handler on a real socket so the
// peer address is a real one, which is what the lookup is keyed on.
func relayAround(t *testing.T, store flowStore, d decider, classPorts map[int]int) (net.Conn, *relay) {
	t.Helper()
	r := newRelay(store, d, classPorts)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	handled := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(handled)
			return
		}
		r.handle(conn)
		close(handled)
	}()
	client, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// Cleanups run last-in first-out: the client has to be closed before the
	// handler is awaited, or the handler waits for a conversation that is
	// still open.
	t.Cleanup(func() {
		select {
		case <-handled:
		case <-time.After(10 * time.Second):
			t.Error("the relay handler did not finish")
		}
	})
	t.Cleanup(func() { _ = client.Close() })
	return client, r
}

func peerKey(t *testing.T, conn net.Conn) portKey {
	t.Helper()
	addr := conn.LocalAddr().(*net.TCPAddr)
	return portKey{IP: binary.BigEndian.Uint32(addr.IP.To4()), Port: uint32(addr.Port)}
}

// The whole point of the relay: the flow reaches the class inbound, which sees
// the destination the application asked for, and the bytes come back.
func TestRelayHandsAProxiedFlowToTheClassInbound(t *testing.T) {
	inbound := newFakeClassInbound(t)
	store := newFakeStore()
	client, r := relayAround(t, store, fakeDecider{}, map[int]int{1: inbound.port()})
	store.record(peerKey(t, client), orig{
		IP: [4]byte{93, 184, 216, 34}, Port: 443, Pid: 42,
		Action: ActionProxy, InboundID: 1,
	})

	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("read the echoed bytes back: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("echoed %q", got)
	}
	select {
	case dst := <-inbound.seen:
		if dst != "93.184.216.34:443" {
			t.Fatalf("the class inbound was asked for %s", dst)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the class inbound saw no request")
	}
	if r.relayed.Load() != 1 {
		t.Fatalf("relayed=%d", r.relayed.Load())
	}
}

// A flow the kernel could not decide arrives marked; the relay asks the planner
// and follows the answer.
func TestRelayDecidesAFlowTheKernelCouldNot(t *testing.T) {
	inbound := newFakeClassInbound(t)
	store := newFakeStore()
	client, r := relayAround(t, store, fakeDecider{action: ActionProxy, class: 1}, map[int]int{1: inbound.port()})
	store.record(peerKey(t, client), orig{
		IP: [4]byte{1, 2, 3, 4}, Port: 80, Action: actionDecide,
	})
	if _, err := client.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case dst := <-inbound.seen:
		if dst != "1.2.3.4:80" {
			t.Fatalf("the class inbound was asked for %s", dst)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the class inbound saw no request")
	}
	if r.decided.Load() != 1 {
		t.Fatalf("decided=%d", r.decided.Load())
	}
}

// A direct decision on a flow the kernel already redirected is served by the
// relay dialling the destination itself.
func TestRelayDialsADirectFlow(t *testing.T) {
	echo := echoServer(t)
	store := newFakeStore()
	client, r := relayAround(t, store, fakeDecider{action: ActionDirect}, map[int]int{})
	addr := echo.Addr().(*net.TCPAddr)
	store.record(peerKey(t, client), orig{
		IP: [4]byte{127, 0, 0, 1}, Port: addr.Port, Action: actionDecide,
	})
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("read the echo: %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("echoed %q", got)
	}
	if r.direct.Load() != 1 {
		t.Fatalf("direct=%d", r.direct.Load())
	}
}

// A blocked flow is dropped without a connection to anywhere.
func TestRelayDropsABlockedFlow(t *testing.T) {
	store := newFakeStore()
	client, r := relayAround(t, store, fakeDecider{action: ActionBlock}, map[int]int{})
	store.record(peerKey(t, client), orig{
		IP: [4]byte{1, 2, 3, 4}, Port: 80, Action: actionDecide,
	})
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := client.Write([]byte("x")); err != nil {
		t.Logf("write: %v", err)
	}
	if _, err := io.ReadAll(client); err != nil {
		t.Logf("read: %v", err)
	}
	if r.refused.Load() == 0 {
		t.Fatal("a blocked flow was not counted as refused")
	}
	if r.relayed.Load() != 0 || r.direct.Load() != 0 {
		t.Fatalf("a blocked flow went somewhere: relayed=%d direct=%d", r.relayed.Load(), r.direct.Load())
	}
}

// The mapping is consumed: one connection, one destination. A reused port must
// not inherit the previous connection's destination.
func TestRelayConsumesTheMapping(t *testing.T) {
	inbound := newFakeClassInbound(t)
	store := newFakeStore()
	client, _ := relayAround(t, store, fakeDecider{}, map[int]int{1: inbound.port()})
	key := peerKey(t, client)
	store.record(key, orig{IP: [4]byte{1, 1, 1, 1}, Port: 80, Action: ActionProxy, InboundID: 1})

	if _, err := client.Write([]byte("x")); err != nil {
		t.Logf("write: %v", err)
	}
	<-inbound.seen
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		_, still := store.flows[key]
		store.mu.Unlock()
		if !still {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the destination stayed in the map after the flow was served")
}

// A flow with no recorded destination is refused, not guessed.
func TestRelayRefusesAFlowWithoutADestination(t *testing.T) {
	inbound := newFakeClassInbound(t)
	store := newFakeStore()
	client, r := relayAround(t, store, fakeDecider{}, map[int]int{1: inbound.port()})
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.ReadAll(client)
	if r.refused.Load() == 0 {
		t.Fatal("the flow was not refused")
	}
	if r.relayed.Load() != 0 {
		t.Fatal("a flow without a destination was relayed")
	}
}

// A class without an inbound is refused rather than sent somewhere else.
func TestRelayRefusesAClassWithoutAnInbound(t *testing.T) {
	store := newFakeStore()
	client, r := relayAround(t, store, fakeDecider{}, map[int]int{})
	store.record(peerKey(t, client), orig{
		IP: [4]byte{1, 1, 1, 1}, Port: 80, Action: ActionProxy, InboundID: 1,
	})
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.ReadAll(client)
	if r.refused.Load() == 0 {
		t.Fatal("a class with no inbound was not refused")
	}
	if r.relayed.Load() != 0 {
		t.Fatal("a flow with no inbound was relayed")
	}
}