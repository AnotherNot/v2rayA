package dns

import (
	"net/netip"
	"sync"
	"time"
)

// maxRateLimitClients bounds the per-client state an unauthenticated UDP
// listener can make the process allocate. A spoofed-source flood must cost
// memory proportional to this constant, not to the flood.
const maxRateLimitClients = 4096

type rateBucket struct {
	tokens float64
	last   time.Time
}

// clientLimiter is a token bucket per source address. A client receives
// RateLimit queries per second in a burst of the same size; anything above is
// dropped. It answers the amplification part of an open-resolver report
// without changing how a normal host resolves names.
type clientLimiter struct {
	mu      sync.Mutex
	limit   float64
	burst   float64
	buckets map[netip.Addr]*rateBucket
	now     func() time.Time
}

func newClientLimiter(limit int) *clientLimiter {
	perSecond := float64(limit)
	return &clientLimiter{
		limit:   perSecond,
		burst:   perSecond,
		buckets: make(map[netip.Addr]*rateBucket),
		now:     time.Now,
	}
}

// Allow reports whether the client may send one query now.
func (l *clientLimiter) Allow(ip netip.Addr) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	ip = ip.Unmap()
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[ip]
	if !ok {
		if len(l.buckets) >= maxRateLimitClients {
			l.evict(now)
		}
		bucket = &rateBucket{tokens: l.burst, last: now}
		l.buckets[ip] = bucket
	}
	if elapsed := now.Sub(bucket.last).Seconds(); elapsed > 0 {
		bucket.tokens += elapsed * l.limit
		if bucket.tokens > l.burst {
			bucket.tokens = l.burst
		}
		bucket.last = now
	}
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// evict drops idle buckets first, then one arbitrary entry. The second step
// keeps the map bounded even when every entry is active, which is exactly the
// case a spoofed flood creates.
func (l *clientLimiter) evict(now time.Time) {
	cutoff := now.Add(-10 * time.Second)
	for ip, bucket := range l.buckets {
		if bucket.last.Before(cutoff) {
			delete(l.buckets, ip)
		}
	}
	if len(l.buckets) >= maxRateLimitClients {
		for ip := range l.buckets {
			delete(l.buckets, ip)
			break
		}
	}
}
