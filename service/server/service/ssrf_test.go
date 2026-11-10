package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/v2rayA/v2rayA/common/netguard"
)

// A subscription or rule source is fetched from an address its provider
// chooses. Before SEC-10 a redirect could take that fetch to loopback, the LAN
// or a metadata endpoint; the guard must refuse before the request leaves the
// process.
func TestFetchRefusesPrivateTargetsByDefault(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"version":1,"rules":[]}`))
	}))
	defer server.Close()

	netguard.SetAllowPrivate(false)
	t.Cleanup(func() { netguard.SetAllowPrivate(true) })

	if _, err := fetchRuleDocument(server.URL); !errors.Is(err, netguard.ErrPrivateAddress) {
		t.Fatalf("fetch to loopback = %v, want ErrPrivateAddress", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the loopback server received %d requests, want 0", n)
	}

	netguard.SetAllowPrivate(true)
	if _, err := fetchRuleDocument(server.URL); err != nil {
		t.Fatalf("fetch with --allow-private-fetch: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("the loopback server received %d requests with the flag, want 1", n)
	}
}
