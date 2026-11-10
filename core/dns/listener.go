package dns

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Handler is the interface for processing DNS queries.
// Implementations must be safe for concurrent use.
type Handler interface {
	// HandleQuery processes a DNS query and returns a response.
	// The context may carry a deadline for timeout control.
	HandleQuery(ctx context.Context, query *DnsQuery) (*DnsResponse, error)
}

// serverPair holds UDP and TCP server instances for a single listen address.
type serverPair struct {
	udp *dns.Server
	tcp *dns.Server
}

// DnsListener listens for DNS queries on one or more addresses (UDP + TCP per address).
type DnsListener struct {
	config  *DnsListenerConfig
	handler Handler
	servers []serverPair
	allowed []netip.Prefix
	limiter *clientLimiter
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewDnsListener creates a new DnsListener with the given config and handler.
func NewDnsListener(config *DnsListenerConfig, handler Handler) *DnsListener {
	return &DnsListener{
		config:  config,
		handler: handler,
	}
}

// Start starts the primary listener strictly and every extra listener on a
// best-effort basis: an extra address that cannot be bound (a privileged
// port without root, a resolver already owning it) is skipped with a warning
// instead of failing the whole module. A module whose primary listener is
// down serves nothing, so the primary stays fatal.
func (l *DnsListener) Start() error {
	if l.config == nil {
		return fmt.Errorf("dns listener: config is nil")
	}
	if l.handler == nil {
		return fmt.Errorf("dns listener: handler is nil")
	}
	if err := l.config.Validate(); err != nil {
		return fmt.Errorf("dns listener: invalid config: %w", err)
	}

	l.ctx, l.cancel = context.WithCancel(context.Background())
	allowed, err := allowedClientPrefixes(l.config.AllowedClients)
	if err != nil {
		l.Stop()
		return fmt.Errorf("dns listener: allowed clients: %w", err)
	}
	l.allowed = allowed
	if l.config.RateLimit > 0 {
		l.limiter = newClientLimiter(l.config.RateLimit)
	}

	// The primary listener is mandatory: without it the module serves nothing.
	primary, err := l.startPair(l.config.ListenAddr)
	if err != nil {
		l.Stop()
		return fmt.Errorf("dns listener: %s: %w", l.config.ListenAddr, err)
	}
	l.servers = append(l.servers, *primary)

	// Extra listeners are best-effort: one the process cannot bind must not
	// take down the primary. A loud warning names the loser.
	for _, addr := range l.config.ExtraListenAddrs {
		pair, err := l.startPair(addr)
		if err != nil {
			log.Printf("[dns] warning: extra listener %s not started, continuing without it: %v", addr, err)
			continue
		}
		l.servers = append(l.servers, *pair)
	}

	return nil
}

// startWaitTimeout bounds the wait for the bound sockets to report started.
// The sockets are already bound, so only a stalled scheduler can exhaust it.
const startWaitTimeout = 5 * time.Second

// startPair starts UDP and TCP listeners for a single address. It returns once
// both are serving. miekg/dns marks a server started inside ActivateAndServe,
// so a Start that returned earlier would let Stop miss a server that has not
// started yet and then wait forever for the goroutine that starts it later.
func (l *DnsListener) startPair(addr string) (*serverPair, error) {
	timeout := time.Duration(l.config.Timeout) * time.Second

	udpConn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}
	udpStarted := make(chan struct{})
	udpSrv := &dns.Server{
		Addr:              addr,
		Net:               "udp",
		PacketConn:        udpConn,
		Handler:           dns.HandlerFunc(l.handlePacket),
		UDPSize:           1452,
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		NotifyStartedFunc: func() { close(udpStarted) },
	}

	tcpListener, err := net.Listen("tcp", addr)
	if err != nil {
		_ = udpConn.Close()
		return nil, fmt.Errorf("listen tcp: %w", err)
	}
	tcpStarted := make(chan struct{})
	tcpSrv := &dns.Server{
		Addr:              addr,
		Net:               "tcp",
		Listener:          tcpListener,
		Handler:           dns.HandlerFunc(l.handlePacket),
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		NotifyStartedFunc: func() { close(tcpStarted) },
	}

	serveErr := make(chan error, 2)
	l.wg.Add(2)
	go func() {
		defer l.wg.Done()
		log.Printf("[dns] UDP listener starting on %s", addr)
		if err := udpSrv.ActivateAndServe(); err != nil {
			log.Printf("[dns] UDP listener stopped: %v", err)
			serveErr <- fmt.Errorf("udp: %w", err)
		}
	}()
	go func() {
		defer l.wg.Done()
		log.Printf("[dns] TCP listener starting on %s", addr)
		if err := tcpSrv.ActivateAndServe(); err != nil {
			log.Printf("[dns] TCP listener stopped: %v", err)
			serveErr <- fmt.Errorf("tcp: %w", err)
		}
	}()

	if err := waitStarted(udpStarted, tcpStarted, serveErr); err != nil {
		// The sockets are still ours: closing them unblocks a server that did
		// start, so its goroutine ends and Stop is not left waiting for a
		// listener no Shutdown call can reach.
		_ = udpConn.Close()
		_ = tcpListener.Close()
		return nil, err
	}

	return &serverPair{udp: udpSrv, tcp: tcpSrv}, nil
}

// waitStarted blocks until both servers report that they are serving, one of
// them returns an error, or the start deadline expires.
func waitStarted(udp, tcp <-chan struct{}, serveErr <-chan error) error {
	timer := time.NewTimer(startWaitTimeout)
	defer timer.Stop()
	for udp != nil || tcp != nil {
		select {
		case <-udp:
			udp = nil
		case <-tcp:
			tcp = nil
		case err := <-serveErr:
			return fmt.Errorf("start listener: %w", err)
		case <-timer.C:
			return fmt.Errorf("listener did not start within %s", startWaitTimeout)
		}
	}
	return nil
}

// Stop gracefully shuts down all listeners. A pair that never finished
// starting reports "server not started"; that is the normal residue of a
// failed bind, not an error worth logging.
func (l *DnsListener) Stop() error {
	if l.cancel != nil {
		l.cancel()
	}

	var lastErr error
	for _, pair := range l.servers {
		if pair.udp != nil {
			if err := pair.udp.Shutdown(); err != nil && !isNotStarted(err) {
				log.Printf("[dns] UDP shutdown error: %v", err)
				lastErr = err
			}
		}
		if pair.tcp != nil {
			if err := pair.tcp.Shutdown(); err != nil && !isNotStarted(err) {
				log.Printf("[dns] TCP shutdown error: %v", err)
				lastErr = err
			}
		}
	}

	l.wg.Wait()
	l.servers = nil

	if lastErr != nil {
		return fmt.Errorf("dns listener: shutdown error: %w", lastErr)
	}
	return nil
}

// handlePacket is the callback invoked by miekg/dns for each incoming query.
func (l *DnsListener) handlePacket(w dns.ResponseWriter, msg *dns.Msg) {
	if msg == nil {
		return
	}
	if len(msg.Question) == 0 {
		log.Printf("[dns] received message with no questions from %s", w.RemoteAddr())
		return
	}

	q := msg.Question[0]
	clientAddr := w.RemoteAddr()
	clientIP, hasIP := addressIP(clientAddr)

	// The listener is a resolver, not a public service. A wildcard bind is
	// how port sharing and IP forwarding reach LAN clients, and this check is
	// what keeps the same wildcard from answering the rest of the network.
	if !hasIP || !l.clientAllowed(clientIP) {
		dnsLogf("[dns] refused query from %s: not in allowed_clients", clientAddr)
		m := new(dns.Msg)
		m.SetRcode(msg, dns.RcodeRefused)
		_ = w.WriteMsg(m)
		return
	}
	if l.limiter != nil && !l.limiter.Allow(clientIP) {
		// Rate-limited UDP queries are dropped rather than refused: a REFUSED
		// reply still amplifies a spoofed source.
		if _, udp := clientAddr.(*net.UDPAddr); !udp {
			m := new(dns.Msg)
			m.SetRcode(msg, dns.RcodeRefused)
			_ = w.WriteMsg(m)
		}
		return
	}
	// ANY against a recursive resolver is a reflection amplifier; a local
	// client that needs it can ask the upstream directly.
	if q.Qtype == dns.TypeANY && !clientIP.IsLoopback() {
		m := new(dns.Msg)
		m.SetRcode(msg, dns.RcodeRefused)
		_ = w.WriteMsg(m)
		return
	}

	// A query carrying this process's own token has been sent back to us by an
	// upstream — a local resolver that forwards to v2rayA while a rule points
	// back at that resolver. Answering it would start the loop again.
	if carriesOwnToken(msg) {
		loopDetected(q, clientAddr)
		m := new(dns.Msg)
		m.SetRcode(msg, dns.RcodeServerFailure)
		_ = w.WriteMsg(m)
		return
	}

	query := &DnsQuery{
		Name:  q.Name,
		QType: QueryType(q.Qtype),
	}

	switch addr := clientAddr.(type) {
	case *net.UDPAddr:
		query.ClientIP = addr.IP
	case *net.TCPAddr:
		query.ClientIP = addr.IP
	}

	dnsLogf("[dns] query: %s %s from %s", dns.Type(q.Qtype).String(), q.Name, clientAddr.String())

	ctx, cancel := context.WithTimeout(l.ctx, time.Duration(l.config.Timeout)*time.Second)
	defer cancel()

	resp, err := l.handler.HandleQuery(ctx, query)
	if err != nil {
		log.Printf("[dns] error handling query %s %s: %v", dns.Type(q.Qtype).String(), q.Name, err)
		m := new(dns.Msg)
		m.SetReply(msg)
		m.Rcode = dns.RcodeServerFailure
		_ = w.WriteMsg(m)
		return
	}

	m := new(dns.Msg)
	m.SetReply(msg)

	if resp != nil && resp.RawMsg != nil {
		// Deep-copy the upstream response so we don't modify the cached RawMsg in-place.
		// This is critical: resp.RawMsg may be shared with the cache (via cache.Set),
		// and m.SetReply(msg) below would corrupt the cached message.
		rawBytes, err := resp.RawMsg.Pack()
		if err != nil {
			log.Printf("[dns] error packing response for %s %s: %v", dns.Type(q.Qtype).String(), q.Name, err)
			m := new(dns.Msg)
			m.SetReply(msg)
			m.Rcode = dns.RcodeServerFailure
			_ = w.WriteMsg(m)
			return
		}
		m = new(dns.Msg)
		if err := m.Unpack(rawBytes); err != nil {
			log.Printf("[dns] error unpacking response for %s %s: %v", dns.Type(q.Qtype).String(), q.Name, err)
			m := new(dns.Msg)
			m.SetReply(msg)
			m.Rcode = dns.RcodeServerFailure
			_ = w.WriteMsg(m)
			return
		}
		// Set the response header to match the client's request (ID, flags, question).
		// Preserve the upstream Rcode — SetReply always sets Rcode=RcodeSuccess,
		// but we need to keep NXDOMAIN etc. from the upstream.
		origRcode := m.Rcode
		m.SetReply(msg)
		m.Rcode = origRcode
	} else if resp != nil {
		m.Rcode = resp.Rcode
		m.Answer = resp.Answer
		m.Ns = resp.Authority
		m.Extra = resp.Additional
	}

	sanitizeForClient(m, msg)
	if err := w.WriteMsg(m); err != nil {
		log.Printf("[dns] error writing response to %s: %v", clientAddr, err)
	}
}

// sanitizeForClient makes the response match what the client asked for.
// The upstream answer carries the module's own OPT record — with the loop
// token and whatever DO flag the upstream echoed — and, when the upstream
// was asked with DO, RRSIG records. A client that sent no OPT must get
// none back, and one that did gets a fresh OPT with its own DO bit;
// macOS's resolver waits out its full timeout on a response whose EDNS
// does not match its query.
func sanitizeForClient(m, query *dns.Msg) {
	clientOpt := query.IsEdns0()
	extra := m.Extra[:0:0]
	for _, rr := range m.Extra {
		if rr.Header().Rrtype != dns.TypeOPT {
			extra = append(extra, rr)
		}
	}
	m.Extra = extra
	wantSigs := clientOpt != nil && clientOpt.Do()
	if !wantSigs {
		m.Answer = dropSigs(m.Answer)
		m.Ns = dropSigs(m.Ns)
		m.Extra = dropSigs(m.Extra)
	}
	if clientOpt != nil {
		size := clientOpt.UDPSize()
		if size < dns.MinMsgSize {
			size = dns.MinMsgSize
		}
		m.SetEdns0(size, clientOpt.Do())
	}
}

func dropSigs(rrs []dns.RR) []dns.RR {
	out := rrs[:0:0]
	for _, rr := range rrs {
		if rr.Header().Rrtype != dns.TypeRRSIG {
			out = append(out, rr)
		}
	}
	return out
}

// isNotStarted reports the miekg/dns shutdown error of a server that never
// finished starting. Callers see it after a failed bind; it carries no signal.
func isNotStarted(err error) bool {
	return err != nil && strings.Contains(err.Error(), "server not started")
}

var defaultAllowedClients = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// allowedClientPrefixes turns the configured strings into prefixes. An empty
// list is loopback only: a config written before this field existed must not
// silently keep answering the whole network.
func allowedClientPrefixes(raw []string) ([]netip.Prefix, error) {
	// Loopback is never removed: the module's own hijacked resolv.conf and a
	// local client both arrive from there, and an ACL that omits it turns the
	// host's own resolver off.
	prefixes := append([]netip.Prefix(nil), defaultAllowedClients...)
	for _, value := range raw {
		prefix, err := ParseClientPrefix(value)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func (l *DnsListener) clientAllowed(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	for _, prefix := range l.allowed {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func addressIP(addr net.Addr) (netip.Addr, bool) {
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip, ok := netip.AddrFromSlice(a.IP)
		return ip.Unmap(), ok
	case *net.TCPAddr:
		ip, ok := netip.AddrFromSlice(a.IP)
		return ip.Unmap(), ok
	default:
		return netip.Addr{}, false
	}
}

// Healthy returns true if all listeners are active.
func (l *DnsListener) Healthy() bool {
	if len(l.servers) == 0 {
		return false
	}
	select {
	case <-l.ctx.Done():
		return false
	default:
		return true
	}
}
