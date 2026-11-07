package router

import (
	"math"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	lru "github.com/hashicorp/golang-lru/v2"
)

// rateLimiter is a per-client token bucket. The bucket map is bounded so a
// client cycling source addresses cannot turn the limiter into a memory leak;
// an evicted bucket simply starts full again.
type rateLimiter struct {
	mu      sync.Mutex
	buckets *lru.Cache[string, *rateBucket]
	rate    float64
	burst   float64
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMinute, burst int) *rateLimiter {
	buckets, err := lru.New[string, *rateBucket](4096)
	if err != nil {
		// New only fails on a non-positive size; 4096 is a constant.
		panic(err)
	}
	return &rateLimiter{
		buckets: buckets,
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
	}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	bucket, ok := l.buckets.Get(key)
	if !ok {
		bucket = &rateBucket{tokens: l.burst, last: now}
		l.buckets.Add(key, bucket)
	}
	elapsed := now.Sub(bucket.last).Seconds()
	bucket.last = now
	bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.rate)
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func (l *rateLimiter) middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !l.allow(clientKey(ctx.Request)) {
			ctx.Header("Retry-After", "60")
			abortSecurity(ctx, http.StatusTooManyRequests, "RATE_LIMITED",
				"too many requests; retry shortly")
			return
		}
		ctx.Next()
	}
}

// clientKey uses the transport peer address. X-Forwarded-For is
// client-controlled unless a trusted proxy rewrites it, so it is not used:
// behind a reverse proxy every client shares the proxy's bucket, which is
// worse for throughput but cannot be bypassed with a header.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}
