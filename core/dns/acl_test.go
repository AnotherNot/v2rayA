package dns

import (
	"context"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestAllowedClientPrefixesDefaultsToLoopback(t *testing.T) {
	prefixes, err := allowedClientPrefixes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("default prefixes = %v, want loopback v4 and v6", prefixes)
	}
	listener := &DnsListener{allowed: prefixes}
	if !listener.clientAllowed(netip.MustParseAddr("127.0.0.1")) {
		t.Error("loopback v4 refused")
	}
	if !listener.clientAllowed(netip.MustParseAddr("::1")) {
		t.Error("loopback v6 refused")
	}
	if listener.clientAllowed(netip.MustParseAddr("192.168.1.5")) {
		t.Error("a LAN address was allowed without an explicit ACL")
	}
}

func TestClientAllowedUsesPrefixes(t *testing.T) {
	prefixes, err := allowedClientPrefixes([]string{"192.168.1.0/24", "10.0.0.7"})
	if err != nil {
		t.Fatal(err)
	}
	listener := &DnsListener{allowed: prefixes}
	for _, allowed := range []string{"192.168.1.1", "192.168.1.254", "10.0.0.7", "127.0.0.1"} {
		if !listener.clientAllowed(netip.MustParseAddr(allowed)) {
			t.Errorf("%s refused", allowed)
		}
	}
	for _, blocked := range []string{"192.168.2.1", "10.0.0.8", "8.8.8.8"} {
		if listener.clientAllowed(netip.MustParseAddr(blocked)) {
			t.Errorf("%s allowed", blocked)
		}
	}
}

func TestParseClientPrefix(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"192.168.1.0/24", "192.168.1.0/24", true},
		{"192.168.1.7", "192.168.1.7/32", true},
		{"2001:db8::/32", "2001:db8::/32", true},
		{"::1", "::1/128", true},
		{"", "", false},
		{"not-an-address", "", false},
		{"192.168.1.0/33", "", false},
	}
	for _, tc := range cases {
		prefix, err := ParseClientPrefix(tc.raw)
		if tc.ok {
			if err != nil {
				t.Errorf("%q: %v", tc.raw, err)
				continue
			}
			if prefix.String() != tc.want {
				t.Errorf("%q = %s, want %s", tc.raw, prefix, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q unexpectedly parsed as %s", tc.raw, prefix)
		}
	}
}

type fakeResponseWriter struct {
	remote *net.UDPAddr
	msg    *dns.Msg
}

func (w *fakeResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}
}
func (w *fakeResponseWriter) RemoteAddr() net.Addr { return w.remote }
func (w *fakeResponseWriter) WriteMsg(m *dns.Msg) error {
	w.msg = m
	return nil
}
func (w *fakeResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (w *fakeResponseWriter) Close() error              { return nil }
func (w *fakeResponseWriter) TsigStatus() error         { return nil }
func (w *fakeResponseWriter) TsigTimersOnly(bool)       {}
func (w *fakeResponseWriter) Hijack()                   {}

// countingHandler is called from the server goroutine, so its counter is
// atomic: the client receiving a response is not a happens-before edge.
type countingHandler struct{ calls atomic.Int64 }

func (h *countingHandler) HandleQuery(context.Context, *DnsQuery) (*DnsResponse, error) {
	h.calls.Add(1)
	return &DnsResponse{Rcode: dns.RcodeSuccess}, nil
}

func TestHandlePacketRefusesClientOutsideACL(t *testing.T) {
	handler := &countingHandler{}
	listener := &DnsListener{
		config:  &DnsListenerConfig{ListenAddr: "127.0.0.1:0", Timeout: 1},
		handler: handler,
		allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		ctx:     context.Background(),
	}
	query := new(dns.Msg)
	query.SetQuestion("example.com.", dns.TypeA)

	refused := &fakeResponseWriter{remote: &net.UDPAddr{IP: net.IPv4(192, 168, 1, 5), Port: 4444}}
	listener.handlePacket(refused, query)
	if got := handler.calls.Load(); got != 0 {
		t.Fatalf("handler ran %d times for a refused client", got)
	}
	if refused.msg == nil || refused.msg.Rcode != dns.RcodeRefused {
		t.Fatalf("refused response = %v, want REFUSED", refused.msg)
	}

	allowed := &fakeResponseWriter{remote: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4444}}
	listener.handlePacket(allowed, query)
	if got := handler.calls.Load(); got != 1 {
		t.Fatalf("handler ran %d times for loopback, want 1", got)
	}
	if allowed.msg == nil || allowed.msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("allowed response = %v, want the handler's answer", allowed.msg)
	}
}

func TestListenerConfigValidatesACLAndRateLimit(t *testing.T) {
	valid := &DnsListenerConfig{
		ListenAddr:     "127.0.0.1:52353",
		Timeout:        5,
		AllowedClients: []string{"127.0.0.0/8", "::1", "192.168.1.0/24"},
		RateLimit:      200,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, mutate := range map[string]func(*DnsListenerConfig){
		"bad client":    func(c *DnsListenerConfig) { c.AllowedClients = []string{"not-an-address"} },
		"empty client":  func(c *DnsListenerConfig) { c.AllowedClients = []string{" "} },
		"negative rate": func(c *DnsListenerConfig) { c.RateLimit = -1 },
		"huge rate":     func(c *DnsListenerConfig) { c.RateLimit = MaxRateLimit + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := *valid
			cfg.AllowedClients = append([]string(nil), valid.AllowedClients...)
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

// The UDP path is the one an open resolver exposes to the network; this test
// checks that the ACL check does not stop an ordinary loopback query. It goes
// through Start and Stop so that teardown exercises the same lifecycle as the
// module, not a bare Shutdown that hides a listener which never started.
func TestListenerAnswersLoopbackOverUDP(t *testing.T) {
	handler := &countingHandler{}
	listener := NewDnsListener(&DnsListenerConfig{ListenAddr: "127.0.0.1:0", Timeout: 1}, handler)
	if err := listener.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	addr := listener.servers[0].udp.PacketConn.LocalAddr().String()
	client := &dns.Client{Net: "udp", Timeout: time.Second}
	query := new(dns.Msg)
	query.SetQuestion("example.com.", dns.TypeA)
	resp, _, err := client.Exchange(query, addr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("rcode = %s, want NOERROR", dns.RcodeToString[resp.Rcode])
	}
	if got := handler.calls.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}
