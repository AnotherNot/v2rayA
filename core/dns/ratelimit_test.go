package dns

import (
	"net/netip"
	"testing"
	"time"
)

func TestClientLimiterAllowsBurstThenRefills(t *testing.T) {
	limiter := newClientLimiter(2)
	now := time.Unix(1000, 0)
	limiter.now = func() time.Time { return now }
	ip := netip.MustParseAddr("192.168.1.5")

	if !limiter.Allow(ip) || !limiter.Allow(ip) {
		t.Fatal("the burst was refused")
	}
	if limiter.Allow(ip) {
		t.Fatal("a query above the burst was allowed")
	}
	now = now.Add(time.Second)
	if !limiter.Allow(ip) || !limiter.Allow(ip) {
		t.Fatal("the bucket did not refill to its burst")
	}
	if limiter.Allow(ip) {
		t.Fatal("refill granted more than the burst")
	}
}

func TestClientLimiterBoundsItsState(t *testing.T) {
	limiter := newClientLimiter(1)
	now := time.Unix(1000, 0)
	limiter.now = func() time.Time { return now }
	for i := 0; i < maxRateLimitClients+100; i++ {
		addr := netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
		limiter.Allow(addr)
		if len(limiter.buckets) > maxRateLimitClients {
			t.Fatalf("bucket map grew to %d", len(limiter.buckets))
		}
	}
}

func TestNilClientLimiterAllows(t *testing.T) {
	var limiter *clientLimiter
	if !limiter.Allow(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("a disabled limiter refused a query")
	}
}
