package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterRejectsBeyondBurstAndRefills(t *testing.T) {
	limiter := newRateLimiter(60, 1) // one token per second, burst of one
	if !limiter.allow("a") {
		t.Fatal("the first request was refused")
	}
	if limiter.allow("a") {
		t.Fatal("a second immediate request was allowed")
	}
	if !limiter.allow("b") {
		t.Fatal("one client consumed another client's bucket")
	}

	limiter.mu.Lock()
	bucket, ok := limiter.buckets.Get("a")
	if ok {
		bucket.last = time.Now().Add(-2 * time.Second)
	}
	limiter.mu.Unlock()
	if !ok {
		t.Fatal("bucket disappeared")
	}
	if !limiter.allow("a") {
		t.Fatal("the bucket did not refill")
	}
}

func TestRateLimitMiddlewareReturns429(t *testing.T) {
	limiter := newRateLimiter(1, 1)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(limiter.middleware())
	r.GET("/", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.5:1234"
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		return recorder
	}

	if got := request().Code; got != http.StatusOK {
		t.Fatalf("first status = %d", got)
	}
	recorder := request()
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q", got)
	}
	if got := errorCode(t, recorder); got != "RATE_LIMITED" {
		t.Errorf("errorCode = %q, want RATE_LIMITED", got)
	}
}

func TestClientKeyUsesTheTransportAddress(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientKey(req); got != "198.51.100.7" {
		t.Fatalf("clientKey = %q, want the transport peer", got)
	}
}
