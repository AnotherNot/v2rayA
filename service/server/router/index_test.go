package router

import (
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerTimeouts(t *testing.T) {
	server := newHTTPServer(http.NotFoundHandler())
	if server.ReadHeaderTimeout != 10*time.Second || server.IdleTimeout != 120*time.Second {
		t.Fatalf("timeouts = read header %v, idle %v", server.ReadHeaderTimeout, server.IdleTimeout)
	}
	// A body is capped by limitBody, and the read deadline stops a client
	// from holding a connection open by never finishing one.
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("read timeout = %v, want 30s", server.ReadTimeout)
	}
	// Speed and latency probes are long; the write deadline still bounds a
	// client that does not read the response.
	if server.WriteTimeout != 10*time.Minute {
		t.Fatalf("write timeout = %v, want 10m", server.WriteTimeout)
	}
}
