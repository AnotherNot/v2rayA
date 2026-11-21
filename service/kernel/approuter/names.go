package approuter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// The DNS module answers a name with one or more addresses, and the kernel sees
// only the address. This table is the bridge: the module reports each pair it
// answered, and a rule that names a domain is matched against the names the
// destination is known by.
//
// The table is keyed by address because that is what a flow arrives with. One
// address can carry several names (a CNAME chain, a shared CDN edge), and a
// rule matches if any of them does.

const (
	// dnsMaxNamesPerAddr bounds what one address may accumulate. A shared
	// address would otherwise collect names until the entry says nothing.
	dnsMaxNamesPerAddr = 8
	// dnsMaxAddrs bounds the table. Every report is kept until it expires, and
	// a resolver that answers for a lot of names must not grow the service
	// without end; when the table is full, room is made by dropping one entry.
	dnsMaxAddrs = 16384
	// dnsMinTTL keeps a name long enough for the connection that follows the
	// query; dnsMaxTTL keeps a server from tying an address to a name for a
	// week.
	dnsMinTTL = 10 * time.Second
	dnsMaxTTL = 30 * time.Minute
)

type nameEntry struct {
	name   string
	expiry time.Time
}

type nameStore struct {
	mu     sync.RWMutex
	byAddr map[string][]nameEntry
}

func newNameStore() *nameStore { return &nameStore{byAddr: map[string][]nameEntry{}} }

// nameTTL clamps what a server said into the range a table entry lives for.
func nameTTL(ttl time.Duration) time.Duration {
	if ttl < dnsMinTTL {
		return dnsMinTTL
	}
	if ttl > dnsMaxTTL {
		return dnsMaxTTL
	}
	return ttl
}

// learn records one name for one address, replacing the name's previous entry.
func (s *nameStore) learn(name string, addr netip.Addr, ttl time.Duration) {
	name = canonicalName(name)
	if s == nil || name == "" {
		return
	}
	key := addr.Unmap().String()
	entry := nameEntry{name: name, expiry: time.Now().Add(nameTTL(ttl))}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.byAddr[key]
	for i := range list {
		if list[i].name == name {
			list[i] = entry
			s.byAddr[key] = list
			return
		}
	}
	if len(list) == 0 && len(s.byAddr) >= dnsMaxAddrs {
		for k := range s.byAddr {
			delete(s.byAddr, k)
			break
		}
	}
	if len(list) >= dnsMaxNamesPerAddr {
		list = list[1:]
	}
	s.byAddr[key] = append(list, entry)
}

// lookup returns the names the address is known by right now.
func (s *nameStore) lookup(ip net.IP) []string {
	if s == nil {
		return nil
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return nil
	}
	key := addr.Unmap().String()
	now := time.Now()
	// The slice is copied under the lock: learn replaces an entry in place,
	// and reading it after the lock was dropped would race with that.
	s.mu.RLock()
	list := append([]nameEntry(nil), s.byAddr[key]...)
	s.mu.RUnlock()
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		if e.expiry.After(now) {
			out = append(out, e.name)
		}
	}
	return out
}

func canonicalName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// A report is one line of JSON: the name the module answered and one address it
// answered with. The module waits for the ack byte before it hands the answer
// to the application, so a connection that follows the query cannot be decided
// before the name is stored.
type dnsReport struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	TTL  int    `json:"ttl"`
}

const dnsReportAck = '\n'

// dnsReporter listens for what the DNS module answered. It is started with the
// engine, so a rule that names a domain has something to match against.
type dnsReporter struct {
	ln    net.Listener
	path  string
	names *nameStore
	done  chan struct{}
}

// startDNSReporter listens on path. An empty path means no rule selects on a
// name, and no listener is started.
func startDNSReporter(path string, names *nameStore) (*dnsReporter, error) {
	if path == "" {
		return nil, nil
	}
	// A socket left by a previous run would make Listen fail with an address
	// in use, and nothing else removes it.
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen for DNS reports on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("restrict the DNS report socket %s: %w", path, err)
	}
	r := &dnsReporter{ln: ln, path: path, names: names, done: make(chan struct{})}
	go r.serve()
	return r, nil
}

func (r *dnsReporter) serve() {
	defer close(r.done)
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			return
		}
		go r.handle(conn)
	}
}

func (r *dnsReporter) handle(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1024), 64*1024)
	for scanner.Scan() {
		r.learnLine(scanner.Bytes())
		// Always answer: the module is waiting to hand the DNS answer on, and
		// a report this side cannot read must not stall the resolver.
		if _, err := conn.Write([]byte{dnsReportAck}); err != nil {
			return
		}
	}
}

func (r *dnsReporter) learnLine(line []byte) {
	var report dnsReport
	if err := json.Unmarshal(line, &report); err != nil {
		log.Warn("app router: a DNS report is not readable: %v", err)
		return
	}
	addr, err := netip.ParseAddr(report.Addr)
	if err != nil {
		log.Warn("app router: DNS report for %q carries %q: %v", report.Name, report.Addr, err)
		return
	}
	r.names.learn(report.Name, addr, time.Duration(report.TTL)*time.Second)
	log.Debug("app router: dns %s -> %s", canonicalName(report.Name), addr)
}

func (r *dnsReporter) close() error {
	if r == nil {
		return nil
	}
	err := r.ln.Close()
	_ = os.Remove(r.path)
	<-r.done
	return err
}
