//go:build linux

package approuter

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/v2rayA/RoutingA"
)

// The privileged test is opt-in, following the bound-device guard: it attaches
// the engine to a disposable child cgroup, never to the host root cgroup, and
// every socket it opens is local. No firewall rule, no external destination and
// no production configuration is touched.
//
//	cd service
//	go test -c -o /tmp/v2raya-approuter-tests ./kernel/approuter
//	sudo env V2RAYA_BPF_TEST=1 /tmp/v2raya-approuter-tests -test.v
//
// It proves the paths a unit test cannot: that the kernel hook routes a
// selected executable through the relay to its class inbound, leaves a direct
// executable alone, refuses a blocked one, carries a datagram through a SOCKS5
// association, and that an executable whose rules do not fit the kernel keeps
// being decided by the service on every flow. The last one is the defect the
// live stand found, which is why every path here is taken twice.
func TestNativeRouter(t *testing.T) {
	if os.Getenv("V2RAYA_BPF_TEST") != "1" {
		t.Skip("set V2RAYA_BPF_TEST=1 and run as root to verify with the kernel")
	}
	if os.Geteuid() != 0 {
		t.Fatal("native BPF verification needs root")
	}
	cgroup := newTestCgroup(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ip := localIPv4(t)

	proxy := startNativeEcho(t, ip)
	direct := startNativeEcho(t, ip)
	blocked := startNativeEcho(t, ip)
	udp := startNativeEchoUDP(t, ip)
	socks := startTestSocks(t)

	router, err := Start(Options{
		Cgroup:     cgroup,
		ClassPorts: map[int]int{1: socks.port()},
		Rules: []Rule{
			{Apps: matcher(exe), Ports: ports(udp.port()), Network: NetworkUDP, Action: ActionProxy, Class: 1},
			{Apps: matcher(exe), Ports: ports(proxy.port()), Action: ActionProxy, Class: 1},
			{Apps: matcher(exe), Ports: ports(direct.port()), Action: ActionDirect},
			{Apps: matcher(exe), Ports: ports(blocked.port()), Action: ActionBlock},
		},
	})
	if err != nil {
		t.Fatalf("attach the engine: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	// A proxied flow reaches the class inbound, and so does the second one,
	// which the kernel decides from its cache. The stand's defect was exactly
	// here: the second flow went direct.
	runChild(t, cgroup, ip, "tcp", proxy)
	runChild(t, cgroup, ip, "tcp", proxy)
	if got := socks.connects.Load(); got != 2 {
		t.Fatalf("the class inbound saw %d proxied connections, want 2", got)
	}
	if got := proxy.hits.Load(); got != 2 {
		t.Fatalf("the destination saw %d proxied requests, want 2", got)
	}

	// A direct flow reaches the destination and never the class inbound.
	before := socks.connects.Load()
	runChild(t, cgroup, ip, "tcp", direct)
	if got := direct.hits.Load(); got != 1 {
		t.Fatalf("the direct destination saw %d requests, want 1", got)
	}
	if got := socks.connects.Load(); got != before {
		t.Fatalf("a direct flow reached the class inbound (%d -> %d)", before, got)
	}

	// A blocked flow is refused: the first time by the relay, and from then on
	// by the kernel itself. The destination must never see either attempt.
	runChildBlocked(t, cgroup, ip, blocked)
	runChildBlocked(t, cgroup, ip, blocked)
	if got := blocked.hits.Load(); got != 0 {
		t.Fatalf("a blocked destination saw %d requests", got)
	}

	// A datagram is carried through its own SOCKS5 association, twice: each
	// run opens a new socket and therefore a new flow.
	runChild(t, cgroup, ip, "udp", udp)
	runChild(t, cgroup, ip, "udp", udp)
	if got := socks.associations.Load(); got != 2 {
		t.Fatalf("the class inbound served %d UDP associations, want 2", got)
	}
	if got := udp.hits.Load(); got != 2 {
		t.Fatalf("the UDP destination saw %d datagrams, want 2", got)
	}

	snapshot, err := router.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RuleProxy == 0 || snapshot.RuleDirect == 0 || snapshot.RuleBlock == 0 {
		t.Fatalf("the kernel did not decide every action: %+v", snapshot)
	}
	if snapshot.UDPRedirected == 0 || snapshot.UDPRelayed == 0 {
		t.Fatalf("the datagrams did not take the redirected path: %+v", snapshot)
	}

	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	runDecidedEveryFlow(t, cgroup, exe, ip, proxy, socks)
}

// runDecidedEveryFlow covers the reserved decision slot: a rule that names a
// domain cannot be evaluated by the kernel, so every flow of its executable
// goes to the service. The second flow is the one that used to slip past.
func runDecidedEveryFlow(t *testing.T, cgroup, exe string, ip net.IP, proxy *nativeEcho, socks *testSocks) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "dns.sock")
	router, err := Start(Options{
		Cgroup:          cgroup,
		ClassPorts:      map[int]int{1: socks.port()},
		DNSReportSocket: socket,
		Rules: []Rule{
			{
				Apps:    matcher(exe),
				Ports:   ports(proxy.port()),
				Domains: mustDomains(t, "full: native.test"),
				Action:  ActionProxy,
				Class:   1,
			},
			{Apps: matcher(exe), Ports: ports(proxy.port()), Action: ActionDirect},
		},
	})
	if err != nil {
		t.Fatalf("attach an engine with a domain rule: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	before := socks.connects.Load()
	// The name has to be known before the flow arrives, as after a real answer.
	reportName(t, socket, "native.test", ip)
	runChild(t, cgroup, ip, "tcp", proxy)
	runChild(t, cgroup, ip, "tcp", proxy)
	if got := socks.connects.Load() - before; got != 2 {
		t.Fatalf("the service decided %d of 2 flows, want 2", got)
	}
}

// newTestCgroup makes a disposable subtree. The directory is removed by the
// cleanup; a forced interruption can leave it behind, but nothing is pinned.
func newTestCgroup(t *testing.T) string {
	t.Helper()
	path := filepath.Join("/sys/fs/cgroup", fmt.Sprintf("v2raya-approuter-test-%d", os.Getpid()))
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Errorf("remove the test cgroup: %v", err)
		}
	})
	return path
}

// localIPv4 returns this host's own non-loopback IPv4 address. A loopback
// destination is never proxied, so the test cannot use 127.0.0.1.
func localIPv4(t *testing.T) net.IP {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4
		}
	}
	t.Skip("this host has no non-loopback IPv4 address to use as a destination")
	return nil
}

func matcher(exe string) RoutingA.ProcessMatcher {
	return RoutingA.ProcessMatcher{Paths: []string{exe}}
}

func ports(port int) []PortRange {
	return []PortRange{{Lo: uint16(port), Hi: uint16(port)}}
}

// nativeEcho is a local destination that answers one request and counts it.
type nativeEcho struct {
	ln    net.Listener
	hits  atomic.Int64
	bound int
}

func startNativeEcho(t *testing.T, ip net.IP) *nativeEcho {
	t.Helper()
	ln, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	s := &nativeEcho{ln: ln, bound: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4)
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				s.hits.Add(1)
				_, _ = c.Write([]byte("pong"))
			}(conn)
		}
	}()
	return s
}

func (s *nativeEcho) port() int { return s.bound }

type nativeEchoUDP struct {
	hits  atomic.Int64
	conn  net.PacketConn
	bound int
}

func startNativeEchoUDP(t *testing.T, ip net.IP) *nativeEchoUDP {
	t.Helper()
	pc, err := net.ListenPacket("udp4", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	s := &nativeEchoUDP{conn: pc, bound: pc.LocalAddr().(*net.UDPAddr).Port}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 4 {
				continue
			}
			s.hits.Add(1)
			_, _ = pc.WriteTo([]byte("pong"), addr)
		}
	}()
	return s
}

func (s *nativeEchoUDP) port() int { return s.bound }

// testSocks is the class inbound: the engine's relay dials it, and it dials
// the destination. It counts what arrived so the test can tell a proxied flow
// from a direct one even though both reach the same server.
type testSocks struct {
	ln           net.Listener
	connects     atomic.Int64
	associations atomic.Int64
}

func startTestSocks(t *testing.T) *testSocks {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &testSocks{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	return s
}

func (s *testSocks) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *testSocks) handle(c net.Conn) {
	defer c.Close()
	var greeting [2]byte
	if _, err := io.ReadFull(c, greeting[:]); err != nil || greeting[0] != 5 {
		return
	}
	if _, err := io.CopyN(io.Discard, c, int64(greeting[1])); err != nil {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return
	}
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return
	}
	host, port, err := readSocksAddress(c, head[3])
	if err != nil {
		return
	}
	switch head[1] {
	case 1: // CONNECT
		dst, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
		if err != nil {
			_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		defer dst.Close()
		if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			return
		}
		s.connects.Add(1)
		splice(c, dst)
	case 3: // UDP ASSOCIATE
		s.associate(c)
	default:
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
	}
}

// associate is the server half of what the relay's client does: one UDP socket
// per association, framed datagrams in both directions, and a lifetime bound to
// the TCP control connection.
func (s *testSocks) associate(ctrl net.Conn) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	reply := []byte{5, 0, 0, 1, 127, 0, 0, 1, byte(port >> 8), byte(port)}
	if _, err := ctrl.Write(reply); err != nil {
		return
	}
	s.associations.Add(1)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, ctrl)
		close(done)
	}()
	buf := make([]byte, 65535)
	for {
		select {
		case <-done:
			return
		default:
		}
		_ = pc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		host, dstPort, payload, err := parseUDPDatagram(buf[:n])
		if err != nil {
			continue
		}
		go forwardDatagram(host, dstPort, payload, pc, from)
	}
}

func forwardDatagram(host string, port int, payload []byte, pc net.PacketConn, from net.Addr) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return
	}
	out, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return
	}
	defer out.Close()
	_ = out.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := out.Write(payload); err != nil {
		return
	}
	answer := make([]byte, 2048)
	n, err := out.Read(answer)
	if err != nil {
		return
	}
	_, _ = pc.WriteTo(wrapUDP(net.ParseIP(host), port, answer[:n]), from)
}

func parseUDPDatagram(d []byte) (string, int, []byte, error) {
	if len(d) < 4 || d[2] != 0 {
		return "", 0, nil, fmt.Errorf("not one unfragmented datagram")
	}
	offset := 4
	var host string
	switch d[3] {
	case 1:
		if len(d) < offset+4 {
			return "", 0, nil, io.ErrUnexpectedEOF
		}
		host = net.IP(d[offset : offset+4]).String()
		offset += 4
	case 4:
		if len(d) < offset+16 {
			return "", 0, nil, io.ErrUnexpectedEOF
		}
		host = net.IP(d[offset : offset+16]).String()
		offset += 16
	default:
		return "", 0, nil, fmt.Errorf("address type %d", d[3])
	}
	if len(d) < offset+2 {
		return "", 0, nil, io.ErrUnexpectedEOF
	}
	port := int(binary.BigEndian.Uint16(d[offset:]))
	offset += 2
	return host, port, d[offset:], nil
}

// reportName writes one answered pair to the engine's report socket and waits
// for the acknowledgement, the way the DNS module does.
func reportName(t *testing.T, socket, name string, ip net.IP) {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	line := fmt.Sprintf("{\"name\":%q,\"addr\":%q,\"ttl\":60}\n", name, ip.String())
	if _, err := conn.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	var ack [1]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		t.Fatal(err)
	}
}

// runChild makes one flow from a fresh subprocess in the test cgroup.
func runChild(t *testing.T, cgroup string, ip net.IP, mode string, dst interface{ port() int }) {
	t.Helper()
	cmd := childCmd(cgroup, ip, mode, dst.port())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child (%s): %v\n%s", mode, err, output)
	}
}

// runChildBlocked expects the child to report that the flow failed at the dial,
// the write or the read: the first attempt is refused by the relay and later
// ones by the kernel. The marker is in the output so that a child that fails
// for another reason does not count as a block.
func runChildBlocked(t *testing.T, cgroup string, ip net.IP, dst interface{ port() int }) {
	t.Helper()
	cmd := childCmd(cgroup, ip, "block", dst.port())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the blocked child failed before making its flow: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "BLOCKED") {
		t.Fatalf("the child did not report a blocked flow\n%s", output)
	}
}

func childCmd(cgroup string, ip net.IP, mode string, port int) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeRouterChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		"V2RAYA_APPROUTER_CHILD="+mode,
		"V2RAYA_APPROUTER_CGROUP="+cgroup,
		"V2RAYA_APPROUTER_DEST="+net.JoinHostPort(ip.String(), strconv.Itoa(port)),
	)
	return cmd
}

// TestNativeRouterChild is the subprocess half. It moves itself into the test
// cgroup first, so the program attached there is the one that sees its
// connect, and then makes the flow whose outcome the parent asserts.
func TestNativeRouterChild(t *testing.T) {
	mode := os.Getenv("V2RAYA_APPROUTER_CHILD")
	if mode == "" {
		t.Skip("run by the privileged app router test")
	}
	cgroup := os.Getenv("V2RAYA_APPROUTER_CGROUP")
	if err := os.WriteFile(filepath.Join(cgroup, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := os.Getenv("V2RAYA_APPROUTER_DEST")
	if dest == "" {
		t.Fatal("the child was not given a destination")
	}
	switch mode {
	case "tcp":
		conn, err := net.DialTimeout("tcp", dest, 5*time.Second)
		if err != nil {
			t.Fatalf("connect to %s: %v", dest, err)
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("ping")); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		answer := make([]byte, 4)
		if _, err := io.ReadFull(conn, answer); err != nil {
			t.Fatalf("read the answer through %s: %v", dest, err)
		}
		if string(answer) != "pong" {
			t.Fatalf("answer %q", answer)
		}
	case "block":
		conn, err := net.DialTimeout("tcp", dest, 5*time.Second)
		if err != nil {
			fmt.Println("BLOCKED")
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = conn.Write([]byte("ping"))
		answer := make([]byte, 4)
		if _, err := conn.Read(answer); err != nil {
			fmt.Println("BLOCKED")
			return
		}
		t.Fatalf("a blocked destination answered %q", answer)
	case "udp":
		conn, err := net.Dial("udp", dest)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("ping")); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		answer := make([]byte, 4)
		if _, err := io.ReadFull(conn, answer); err != nil {
			t.Fatalf("read the answer through %s: %v", dest, err)
		}
		if string(answer) != "pong" {
			t.Fatalf("answer %q", answer)
		}
	default:
		t.Fatalf("unknown child mode %q", mode)
	}
}
