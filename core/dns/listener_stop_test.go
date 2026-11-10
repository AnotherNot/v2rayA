package dns

import (
	"testing"
	"time"
)

// Stop must return even when it lands in the window between startPair
// launching its goroutines and miekg/dns marking each server started. Before
// startPair waited for its servers, Shutdown saw "server not started" and
// closed nothing; the TCP goroutine then entered Accept and kept the
// WaitGroup in Stop waiting forever. The deadline turns that hang into a
// failure the test runner can report.
func TestStopImmediatelyAfterStart(t *testing.T) {
	const (
		iterations = 200
		deadline   = 5 * time.Second
	)

	for i := 0; i < iterations; i++ {
		l := NewDnsListener(&DnsListenerConfig{ListenAddr: "127.0.0.1:0", Timeout: 1}, stubHandler{})
		if err := l.Start(); err != nil {
			t.Fatalf("iteration %d: Start: %v", i, err)
		}

		done := make(chan error, 1)
		go func() { done <- l.Stop() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("iteration %d: Stop: %v", i, err)
			}
		case <-time.After(deadline):
			t.Fatalf("iteration %d: Stop blocked for %s", i, deadline)
		}
	}
}
