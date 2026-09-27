package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeDispatcher records what the manager asked for and fails the dial, so
// the tests observe the routing decision without any network.
type fakeDispatcher struct {
	mu    sync.Mutex
	calls []dispatchCall
}

type dispatchCall struct {
	network  string
	addr     string
	proxyTag string
}

func (f *fakeDispatcher) Dispatch(_ context.Context, network, addr, proxyTag string) (net.Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dispatchCall{network, addr, proxyTag})
	return nil, fmt.Errorf("fake dispatcher: no network in tests")
}

func (f *fakeDispatcher) ncalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeDispatcher) last() dispatchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// A single-outbound tag goes through the dispatcher, which forces the
// outbound: the query must reach Dispatch with the tag attached.
func TestSingleTagGoesViaDispatcher(t *testing.T) {
	fake := &fakeDispatcher{}
	mgr := NewUpstreamManager([]UpstreamConfig{
		{ID: "u", Addr: "9.9.9.9:53", Protocol: "udp", ProxyTag: "proxy"},
	})
	mgr.SetDispatcher(fake)
	up, ok := mgr.GetUpstream("u")
	if !ok {
		t.Fatal("upstream not registered")
	}
	_, err := mgr.Exchange(up, &DnsQuery{Name: "example.com", QType: TypeA})
	if err == nil || !strings.Contains(err.Error(), "fake dispatcher") {
		t.Fatalf("err = %v, want the fake dispatcher's failure", err)
	}
	if fake.ncalls() == 0 {
		t.Fatal("dispatcher never called for a single-outbound tag")
	}
	if got := fake.last(); got.proxyTag != "proxy" || got.addr != "9.9.9.9:53" {
		t.Fatalf("dispatch = %+v", got)
	}
}

// A balancer group tag has no outbound handler to force, so it must go
// through the SOCKS proxies instead — even with a dispatcher set. A closed
// port fails fast and proves the SOCKS path was taken.
func TestGroupTagGoesViaSocks(t *testing.T) {
	fake := &fakeDispatcher{}
	mgr := NewUpstreamManager([]UpstreamConfig{
		{ID: "u", Addr: "9.9.9.9:53", Protocol: "udp", ProxyTag: "regional"},
	})
	mgr.SetDispatcher(fake)
	mgr.SetGroupTags([]string{"regional"})
	mgr.SetProxyAddrResolver(func(tag string) string {
		if tag == "regional" {
			return "127.0.0.1:1" // closed: fails fast, proves the path
		}
		return ""
	})
	up, ok := mgr.GetUpstream("u")
	if !ok {
		t.Fatal("upstream not registered")
	}
	_, err := mgr.Exchange(up, &DnsQuery{Name: "example.com", QType: TypeA})
	if err == nil || !strings.Contains(err.Error(), "socks5 dial") {
		t.Fatalf("err = %v, want a SOCKS dial failure", err)
	}
	if n := fake.ncalls(); n != 0 {
		t.Fatalf("dispatcher called %d times for a group tag", n)
	}
}
