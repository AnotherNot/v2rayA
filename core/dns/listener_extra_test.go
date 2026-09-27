package dns

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// stubHandler answers every query with 192.0.2.1.
type stubHandler struct{}

func (stubHandler) HandleQuery(_ context.Context, q *DnsQuery) (*DnsResponse, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(q.Name), uint16(q.QType))
	m.Response = true
	m.RecursionAvailable = true
	a := &dns.A{
		Hdr: dns.RR_Header{Name: dns.Fqdn(q.Name), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   net.ParseIP("192.0.2.1"),
	}
	m.Answer = []dns.RR{a}
	return &DnsResponse{Query: *q, RawMsg: m, Rcode: dns.RcodeSuccess, Answer: m.Answer}, nil
}

func queryPrimary(t *testing.T, l *DnsListener, name string) string {
	t.Helper()
	if len(l.servers) == 0 {
		t.Fatal("no servers started")
	}
	addr := l.servers[0].udp.PacketConn.LocalAddr().String()
	c := new(dns.Client)
	c.Timeout = 3 * time.Second
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("answer = %v", resp.Answer)
	}
	return resp.Answer[0].(*dns.A).A.String()
}

// An extra address that cannot be bound must not take down the primary:
// the module starts, warns, and serves on the primary.
func TestExtraBindFailureKeepsPrimary(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	l := NewDnsListener(&DnsListenerConfig{
		ListenAddr:       "127.0.0.1:0",
		ExtraListenAddrs: []string{occupied.LocalAddr().String()},
		Timeout:          5,
	}, stubHandler{})
	if err := l.Start(); err != nil {
		t.Fatalf("Start with a dead extra: %v", err)
	}
	defer l.Stop()
	if got := queryPrimary(t, l, "example.com"); got != "192.0.2.1" {
		t.Fatalf("answer = %q", got)
	}
}

// The primary listener is mandatory: without it the module serves nothing,
// so its failure stays fatal.
func TestPrimaryBindFailureAborts(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	l := NewDnsListener(&DnsListenerConfig{
		ListenAddr: occupied.LocalAddr().String(),
		Timeout:    5,
	}, stubHandler{})
	if err := l.Start(); err == nil {
		l.Stop()
		t.Fatal("Start on a dead primary succeeded")
	}
}

// Stop is idempotent and quiet: shutting down never-started servers must not
// surface "server not started" as an error. The query first waits out the
// goroutine scheduling: miekg marks a server started inside ActivateAndServe,
// and stopping before that is a race with its internals, not our code.
func TestStopIsQuiet(t *testing.T) {
	l := NewDnsListener(&DnsListenerConfig{ListenAddr: "127.0.0.1:0", Timeout: 5}, stubHandler{})
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if got := queryPrimary(t, l, "example.com"); got != "192.0.2.1" {
		t.Fatalf("answer = %q", got)
	}
	if err := l.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := l.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if !isNotStarted(errors.New("dns: server not started")) {
		t.Fatal("isNotStarted does not recognise the miekg/dns residue error")
	}
	if isNotStarted(errors.New("listen udp: permission denied")) {
		t.Fatal("isNotStarted swallows a real bind error")
	}
}
