package approuter

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/v2rayA/v2rayA/pkg/util/log"
	"golang.org/x/net/ipv4"
)

// udpIdle is how long a flow is kept after its last datagram. A UDP flow has no
// close of its own, so silence is the only end there is.
const udpIdle = 10 * time.Second

// udpFlowStore reads the destination the kernel recorded for a flow address.
// Unlike the TCP store nothing is consumed: the same address serves every
// datagram of the flow.
type udpFlowStore interface {
	flow(addr uint32) (orig, bool, error)
}

// udpRelay receives the datagrams the kernel rewrote to a flow address and
// carries each flow to the class inbound the rule names, over a SOCKS5 UDP
// association, or dials it directly. Answers are sent back from the flow
// address, which is what lets the recvmsg hook turn them into the real source.
type udpRelay struct {
	conn       *net.UDPConn
	pc         *ipv4.PacketConn
	store      udpFlowStore
	decider    decider
	classPorts map[int]int
	forwarded  atomic.Uint64
	direct     atomic.Uint64
	refused    atomic.Uint64
	decided    atomic.Uint64

	mu    sync.Mutex
	flows map[udpFlowKey]*udpFlow
}

// udpFlowKey is a flow address together with the client that owns it. The
// kernel gives every socket its own address, and the client is part of the key
// as well: if two local sockets ever name the same address, neither receives
// the other's answers.
type udpFlowKey struct {
	addr   uint32
	client string
}

// udpFlow is one (process, destination) pair. Exactly one of up and assoc is
// set: up for a flow that goes out directly, assoc for a proxied one, whose
// datagrams travel framed through the inbound.
type udpFlow struct {
	flowAddr uint32
	client   net.Addr
	dst      string
	up       *net.UDPConn
	assoc    *udpAssoc
}

func (f *udpFlow) write(payload []byte) error {
	if f.assoc != nil {
		return f.assoc.send(payload)
	}
	_, err := f.up.Write(payload)
	return err
}

func (f *udpFlow) read(buf []byte) (int, error) {
	if f.assoc != nil {
		return f.assoc.receive(buf)
	}
	return f.up.Read(buf)
}

func (f *udpFlow) setReadDeadline(t time.Time) error {
	if f.assoc != nil {
		return f.assoc.conn.SetReadDeadline(t)
	}
	return f.up.SetReadDeadline(t)
}

func (f *udpFlow) close() {
	if f.assoc != nil {
		f.assoc.close()
		return
	}
	f.up.Close()
}

func newUDPRelay(store udpFlowStore, d decider, classPorts map[int]int) *udpRelay {
	return &udpRelay{
		store: store, decider: d, classPorts: classPorts,
		flows: map[udpFlowKey]*udpFlow{},
	}
}

func (r *udpRelay) start() error {
	if err := r.listen(net.JoinHostPort("0.0.0.0", strconv.Itoa(RelayPort))); err != nil {
		return fmt.Errorf("listen for the app router UDP relay on port %d: %w", RelayPort, err)
	}
	go r.serve()
	return nil
}

// listen binds the socket the flow addresses arrive on. It cannot be
// 127.0.0.1: a socket bound to one address receives only datagrams addressed to
// that address, and the flow addresses live in 127.1.0.0/16. Binding the
// wildcard is what makes every flow address reach the same socket; a datagram
// whose destination is not a flow address finds no entry and is refused.
// Listening on one interface would also do, but the flow addresses belong to
// the loopback interface, not to it.
func (r *udpRelay) listen(addr string) error {
	ua, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", ua)
	if err != nil {
		return err
	}
	pc := ipv4.NewPacketConn(conn)
	// The destination of every datagram is its flow address; without it a
	// datagram cannot be attributed to a flow.
	if err := pc.SetControlMessage(ipv4.FlagDst, true); err != nil {
		conn.Close()
		return fmt.Errorf("read flow addresses: %w", err)
	}
	r.conn, r.pc = conn, pc
	return nil
}

func (r *udpRelay) serve() {
	buf := make([]byte, 65535)
	for {
		n, cm, src, err := r.pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Warn("app router: read a UDP datagram: %v", err)
			continue
		}
		if cm == nil || cm.Dst == nil {
			r.refused.Add(1)
			log.Warn("app router: a datagram from %v carries no destination address; refusing", src)
			continue
		}
		flowIP := cm.Dst.To4()
		if flowIP == nil {
			r.refused.Add(1)
			continue
		}
		flowAddr := ip4Number(flowIP)
		if flowAddr>>16 != 0x7f01 {
			// Flow addresses live in 127.1.0.0/16. The relay listens on the
			// wildcard, so a datagram for any other address is not one the
			// kernel redirected: an exposed port must not turn every
			// stranger's probe into a log line.
			r.refused.Add(1)
			continue
		}
		o, found, err := r.store.flow(flowAddr)
		if err != nil {
			r.refused.Add(1)
			log.Warn("app router: read the flow for %v: %v; refusing", cm.Dst, err)
			continue
		}
		if !found {
			r.refused.Add(1)
			log.Warn("app router: no flow is recorded for %v, from %v; refusing", cm.Dst, src)
			continue
		}
		f, err := r.flowFor(flowAddr, src, o)
		if err != nil {
			r.refused.Add(1)
			log.Warn("app router: flow for %v: %v", o, err)
			continue
		}
		if err := f.write(buf[:n]); err != nil {
			log.Warn("app router: forward a datagram to %s: %v", f.dst, err)
		}
	}
}

// flowFor returns the flow a datagram belongs to, creating it on the first
// datagram. Creating it may need a SOCKS handshake, which is why it happens
// here rather than in the kernel.
func (r *udpRelay) flowFor(flowAddr uint32, client net.Addr, o orig) (*udpFlow, error) {
	key := udpFlowKey{addr: flowAddr, client: client.String()}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.flows[key]; ok {
		return f, nil
	}

	action, class := o.Action, o.InboundID
	if action == actionDecide {
		r.decided.Add(1)
		action, class = r.decider.decide(o)
	}
	switch action {
	case ActionBlock:
		return nil, errors.New("a rule blocked it")
	case ActionDirect, ActionProxy:
	default:
		return nil, fmt.Errorf("it carries action %d, which the relay cannot serve", action)
	}

	f := &udpFlow{flowAddr: flowAddr, client: client, dst: o.String()}
	if action == ActionProxy {
		// A proxied flow is not dialled here: it is handed to the inbound the
		// rule names, over a SOCKS5 UDP association. Dialling the destination
		// directly would make the rule's inbound decorative.
		port, ok := r.classPorts[class]
		if !ok {
			return nil, fmt.Errorf("class %d has no local inbound", class)
		}
		assoc, err := socks5UDPAssociate(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), net.IP(o.IP[:]), o.Port)
		if err != nil {
			return nil, fmt.Errorf("hand it to the class %d inbound: %w", class, err)
		}
		f.assoc = assoc
		r.forwarded.Add(1)
		log.Debug("app router: udp %v -> class %d -> %s (pid %d)", client, class, o, o.Pid)
	} else {
		up, err := net.Dial("udp4", o.String())
		if err != nil {
			return nil, err
		}
		uc, ok := up.(*net.UDPConn)
		if !ok {
			up.Close()
			return nil, errors.New("the destination socket is not UDP")
		}
		f.up = uc
		r.direct.Add(1)
		log.Debug("app router: udp %v -> direct %s (pid %d)", client, o, o.Pid)
	}
	r.flows[key] = f
	go r.replies(key, f)
	return f, nil
}

// replies carries the answers back to the application from the flow address,
// so the application sees the destination it asked for.
func (r *udpRelay) replies(key udpFlowKey, f *udpFlow) {
	defer func() {
		r.mu.Lock()
		delete(r.flows, key)
		r.mu.Unlock()
		f.close()
	}()
	flowIP := net.IPv4(byte(f.flowAddr>>24), byte(f.flowAddr>>16), byte(f.flowAddr>>8), byte(f.flowAddr))
	buf := make([]byte, 65535)
	for {
		_ = f.setReadDeadline(time.Now().Add(udpIdle))
		n, err := f.read(buf)
		if err != nil {
			// The idle deadline is the normal end of a flow; anything else is
			// worth knowing about, because it means the hand-off broke.
			if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
				log.Warn("app router: udp flow to %s ended: %v", f.dst, err)
			}
			return
		}
		if _, err := r.pc.WriteTo(buf[:n], &ipv4.ControlMessage{Src: flowIP}, f.client); err != nil {
			log.Warn("app router: answer %v: %v", f.client, err)
			return
		}
	}
}

func (r *udpRelay) close() error {
	if r == nil || r.conn == nil {
		return nil
	}
	r.mu.Lock()
	flows := r.flows
	r.flows = map[udpFlowKey]*udpFlow{}
	r.mu.Unlock()
	for _, f := range flows {
		f.close()
	}
	return r.conn.Close()
}

// ip4Number is the host-order number the kernel keys a flow address by.
func ip4Number(ip net.IP) uint32 {
	v4 := ip.To4()
	if v4 == nil {
		return 0
	}
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3])
}
