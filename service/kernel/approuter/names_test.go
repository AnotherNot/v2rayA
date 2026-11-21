package approuter

import (
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestNameStoreKeepsEveryNameOfAnAddress(t *testing.T) {
	s := newNameStore()
	addr := netip.MustParseAddr("10.0.0.1")
	s.learn("Example.COM.", addr, time.Minute)
	s.learn("cdn.test", addr, time.Minute)

	got := s.lookup(net.IPv4(10, 0, 0, 1))
	if len(got) != 2 || got[0] != "example.com" || got[1] != "cdn.test" {
		t.Fatalf("lookup gave %v", got)
	}
	// An IPv6 socket that named an IPv4 destination still looks up the IPv4
	// address: the kernel normalizes it before the relay sees it.
	if got := s.lookup(net.ParseIP("::ffff:10.0.0.1")); len(got) != 2 {
		t.Fatalf("the mapped form gave %v", got)
	}
	if got := s.lookup(net.IPv4(10, 0, 0, 2)); len(got) != 0 {
		t.Fatalf("an address no name points at gave %v", got)
	}
}

func TestNameTTLIsClamped(t *testing.T) {
	if got := nameTTL(time.Second); got != dnsMinTTL {
		t.Fatalf("a one-second TTL became %v", got)
	}
	if got := nameTTL(24 * time.Hour); got != dnsMaxTTL {
		t.Fatalf("a one-day TTL became %v", got)
	}
	if got := nameTTL(time.Minute); got != time.Minute {
		t.Fatalf("a one-minute TTL became %v", got)
	}
}

func TestNameStoreForgetsAnExpiredName(t *testing.T) {
	s := newNameStore()
	s.learn("live.test", netip.MustParseAddr("10.0.0.3"), time.Minute)
	s.mu.Lock()
	s.byAddr["10.0.0.3"] = append(s.byAddr["10.0.0.3"], nameEntry{
		name: "gone.test", expiry: time.Now().Add(-time.Second),
	})
	s.mu.Unlock()
	got := s.lookup(net.IPv4(10, 0, 0, 3))
	if len(got) != 1 || got[0] != "live.test" {
		t.Fatalf("lookup gave %v", got)
	}
}

func TestNameStoreKeepsAtMostItsLimitPerAddress(t *testing.T) {
	s := newNameStore()
	addr := netip.MustParseAddr("10.0.0.4")
	for i := 0; i < dnsMaxNamesPerAddr+2; i++ {
		s.learn(fmt.Sprintf("n%d.test", i), addr, time.Minute)
	}
	got := s.lookup(net.IPv4(10, 0, 0, 4))
	if len(got) != dnsMaxNamesPerAddr {
		t.Fatalf("%d names were kept", len(got))
	}
	if got[0] != "n2.test" {
		t.Fatalf("the oldest names were not the ones dropped: %v", got)
	}
}

// A name learned for one address replaces its previous entry rather than
// growing the list.
func TestNameStoreReplacesTheSameName(t *testing.T) {
	s := newNameStore()
	addr := netip.MustParseAddr("10.0.0.5")
	for i := 0; i < 5; i++ {
		s.learn("same.test", addr, time.Minute)
	}
	if got := s.lookup(net.IPv4(10, 0, 0, 5)); len(got) != 1 {
		t.Fatalf("lookup gave %v", got)
	}
}
