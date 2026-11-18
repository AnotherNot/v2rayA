package approuter

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// actionDecide is what the kernel sets on a flow whose executable it cannot
// decide. It never leaves the relay: the relay resolves it into one of the
// actions above.
const actionDecide Action = 3

// orig is the destination the application asked for, with what the kernel knew
// about the socket when it redirected it.
type orig struct {
	IP        [4]byte // network order
	Port      int
	Pid       uint32
	ExeDev    uint32
	ExeIno    uint64
	Action    Action
	InboundID int
}

func (o orig) String() string {
	return net.JoinHostPort(net.IP(o.IP[:]).String(), strconv.Itoa(o.Port))
}

// ipOf reads the destination as the host-order number the rules compare.
func ipOf(o orig) uint32 { return binary.BigEndian.Uint32(o.IP[:]) }

// portKey is the client's own address: what the relay sees as its peer.
type portKey struct {
	IP   uint32
	Port uint32
}

// flowStore hands the relay the destination recorded for a socket. It is an
// interface so the relay can be exercised without a kernel.
type flowStore interface {
	take(key portKey) (orig, bool, error)
}

// decider turns a flow the kernel could not decide into an action.
type decider interface {
	decide(o orig) (Action, int)
}

// relay accepts what the kernel rewrote to it and hands each flow to the local
// inbound of its class, dials it, or drops it.
type relay struct {
	store      flowStore
	decider    decider
	classPorts map[int]int
	ln         net.Listener
	relayed    atomic.Uint64
	refused    atomic.Uint64
	direct     atomic.Uint64
	decided    atomic.Uint64
}

func newRelay(store flowStore, d decider, classPorts map[int]int) *relay {
	return &relay{store: store, decider: d, classPorts: classPorts}
}

func (r *relay) start() error {
	ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(RelayPort)))
	if err != nil {
		return fmt.Errorf("listen for the app router relay on port %d: %w", RelayPort, err)
	}
	r.ln = ln
	go r.serve()
	return nil
}

func (r *relay) serve() {
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			return
		}
		go r.handle(conn)
	}
}

func (r *relay) close() error {
	if r == nil || r.ln == nil {
		return nil
	}
	return r.ln.Close()
}

func (r *relay) handle(conn net.Conn) {
	defer conn.Close()
	peer, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok || peer.IP.To4() == nil {
		r.refused.Add(1)
		log.Warn("app router: peer %v is not IPv4; refusing", conn.RemoteAddr())
		return
	}
	key := portKey{
		IP:   binary.BigEndian.Uint32(peer.IP.To4()),
		Port: uint32(peer.Port),
	}
	// The mapping is written when the client's connect() reaches the sock-ops
	// callback, which runs before the handshake, so the entry is normally
	// already there. The short retry only covers a scheduling delay.
	var (
		o     orig
		found bool
	)
	for attempt := 0; attempt < 25; attempt++ {
		var err error
		o, found, err = r.store.take(key)
		if err != nil {
			r.refused.Add(1)
			log.Warn("app router: read the destination for %v: %v; refusing", conn.RemoteAddr(), err)
			return
		}
		if found {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !found {
		r.refused.Add(1)
		log.Warn("app router: no destination recorded for %v; refusing", conn.RemoteAddr())
		return
	}

	action, class := o.Action, o.InboundID
	if action == actionDecide {
		r.decided.Add(1)
		action, class = r.decider.decide(o)
	}
	switch action {
	case ActionProxy:
		r.handToInbound(conn, o, class)
	case ActionBlock:
		r.refused.Add(1)
		log.Debug("app router: %s -> blocked (pid %d)", o, o.Pid)
	case ActionDirect:
		r.dialDirect(conn, o)
	default:
		r.refused.Add(1)
		log.Warn("app router: %s carries action %d, which the relay cannot serve; refusing", o, action)
	}
}

func (r *relay) handToInbound(conn net.Conn, o orig, class int) {
	port, ok := r.classPorts[class]
	if !ok {
		// A flow whose class has no inbound is refused: sending it anywhere
		// else would apply a decision nobody made.
		r.refused.Add(1)
		log.Warn("app router: class %d has no local inbound; refusing %s", class, o)
		return
	}
	inbound := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	up, err := socks5Dial(inbound, net.IP(o.IP[:]), o.Port)
	if err != nil {
		r.refused.Add(1)
		log.Warn("app router: hand %s to the class %d inbound at %s: %v", o, class, inbound, err)
		return
	}
	defer up.Close()
	r.relayed.Add(1)
	log.Debug("app router: %v -> class %d -> %s (pid %d)", conn.RemoteAddr(), class, o, o.Pid)
	splice(conn, up)
}

// dialDirect serves a flow the engine decided to leave alone but had already
// redirected to the relay.
func (r *relay) dialDirect(conn net.Conn, o orig) {
	up, err := net.DialTimeout("tcp", o.String(), 10*time.Second)
	if err != nil {
		r.refused.Add(1)
		log.Warn("app router: direct dial %s: %v", o, err)
		return
	}
	defer up.Close()
	r.direct.Add(1)
	log.Debug("app router: %s -> direct (pid %d)", o, o.Pid)
	splice(conn, up)
}