package dns

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// A rule can select on the name a connection was resolved from, and the kernel
// only ever sees the address. The service matches such a rule, and it learns
// the pairs from here: this module is the one that answered, so it is the one
// that knows what it answered.
//
// A pair is reported before the answer is returned to the application, and the
// service acknowledges it, so a connection the application makes right after
// the answer cannot arrive at the service before the name is stored.

const (
	// reportDefaultTimeout bounds one exchange with the service.
	reportDefaultTimeout = 250 * time.Millisecond
	// reportRetryDelay keeps a service that is not there yet — the app router
	// attaches after the core starts — from costing every query a failed dial.
	reportRetryDelay = time.Second
)

// reportPair is one line of the report: a name the module answered and one
// address it answered with.
type reportPair struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	TTL  int    `json:"ttl"`
}

// Reporter sends answered pairs to the service over a Unix socket.
type Reporter struct {
	socket  string
	timeout time.Duration

	mu      sync.Mutex
	conn    net.Conn
	retryAt time.Time
	failing bool
}

// NewReporter builds a reporter from the module config. A nil or empty config
// means no rule needs a name.
func NewReporter(cfg *ReportConfig) *Reporter {
	if cfg == nil || cfg.Socket == "" {
		return nil
	}
	timeout := reportDefaultTimeout
	if cfg.AckTimeoutMS > 0 {
		timeout = time.Duration(cfg.AckTimeoutMS) * time.Millisecond
	}
	return &Reporter{socket: cfg.Socket, timeout: timeout}
}

// report sends the pairs one at a time. A pair that cannot be delivered is
// dropped: DNS must keep working when the service does not.
func (r *Reporter) report(pairs []reportPair) {
	if r == nil || len(pairs) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.retryAt) {
		return
	}
	for _, pair := range pairs {
		line, err := json.Marshal(pair)
		if err != nil {
			continue
		}
		line = append(line, '\n')
		if err := r.exchange(line); err != nil {
			// The dial failed before the service was listening, or it stopped
			// answering: wait before trying again, and say so once.
			r.retryAt = time.Now().Add(reportRetryDelay)
			if !r.failing {
				r.failing = true
				log.Printf("[dns] report to %s: %v; names are not learned until it recovers", r.socket, err)
			}
			return
		}
		r.failing = false
	}
}

// exchange writes one line and waits for the acknowledgement byte.
func (r *Reporter) exchange(line []byte) error {
	if r.conn == nil {
		conn, err := net.DialTimeout("unix", r.socket, r.timeout)
		if err != nil {
			return err
		}
		r.conn = conn
	}
	_ = r.conn.SetDeadline(time.Now().Add(r.timeout))
	if _, err := r.conn.Write(line); err != nil {
		r.drop()
		return err
	}
	var ack [1]byte
	if _, err := io.ReadFull(r.conn, ack[:]); err != nil {
		r.drop()
		return err
	}
	_ = r.conn.SetDeadline(time.Time{})
	return nil
}

// Close ends the connection, if any.
func (r *Reporter) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drop()
}

func (r *Reporter) drop() error {
	if r.conn == nil {
		return nil
	}
	err := r.conn.Close()
	r.conn = nil
	return err
}

// reportingHandler puts the reporter in the path of every answer.
type reportingHandler struct {
	inner    Handler
	reporter *Reporter
}

func (h *reportingHandler) HandleQuery(ctx context.Context, query *DnsQuery) (*DnsResponse, error) {
	resp, err := h.inner.HandleQuery(ctx, query)
	if err == nil {
		h.reporter.report(reportPairs(resp))
	}
	return resp, err
}

// reportPairs turns one answer into the pairs the service needs. Only A and
// AAAA records count, and only those reachable from the question through the
// CNAME chain: an answer section can carry records for other names, and tying
// their addresses to this question would route traffic the rule never asked
// for. Both the name the application asked for and the name the answer pointed
// at are reported, because a rule may name either.
func reportPairs(resp *DnsResponse) []reportPair {
	if resp == nil || len(resp.Answer) == 0 {
		return nil
	}
	question := canonicalDNSName(resp.Query.Name)
	chain := map[string]bool{question: true}
	for changed := true; changed; {
		changed = false
		for _, rr := range resp.Answer {
			cname, ok := rr.(*dns.CNAME)
			if !ok || !chain[canonicalDNSName(cname.Hdr.Name)] {
				continue
			}
			target := canonicalDNSName(cname.Target)
			if !chain[target] {
				chain[target] = true
				changed = true
			}
		}
	}
	var pairs []reportPair
	for _, rr := range resp.Answer {
		var addr netip.Addr
		switch rr := rr.(type) {
		case *dns.A:
			a, ok := netip.AddrFromSlice(rr.A)
			if !ok {
				continue
			}
			addr = a.Unmap()
		case *dns.AAAA:
			a, ok := netip.AddrFromSlice(rr.AAAA)
			if !ok {
				continue
			}
			addr = a.Unmap()
		default:
			continue
		}
		owner := canonicalDNSName(rr.Header().Name)
		if !chain[owner] {
			continue
		}
		ttl := int(rr.Header().Ttl)
		pairs = append(pairs, reportPair{Name: question, Addr: addr.String(), TTL: ttl})
		if owner != question {
			pairs = append(pairs, reportPair{Name: owner, Addr: addr.String(), TTL: ttl})
		}
	}
	return pairs
}

// canonicalDNSName is the form names are compared in: lower case, without the
// root label's trailing dot.
func canonicalDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}
