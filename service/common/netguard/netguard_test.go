package netguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestIsBlocked(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.8.9.10", true},
		{"10.0.0.1", true},
		{"172.16.5.4", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"0.0.0.0", true},
		{"198.18.0.1", true},
		{"224.0.0.1", true},
		{"255.255.255.255", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"::", true},
		{"::ffff:127.0.0.1", true},
		{"64:ff9b::7f00:1", true},
		{"1.1.1.1", false},
		{"93.184.216.34", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		addr := netip.MustParseAddr(tc.addr)
		if got := IsBlocked(addr); got != tc.want {
			t.Errorf("IsBlocked(%s) = %v, want %v", tc.addr, got, tc.want)
		}
	}
	if !IsBlocked(netip.Addr{}) {
		t.Error("an invalid address must be blocked")
	}
}

func withResolver(t *testing.T, answers map[string][]string) {
	t.Helper()
	previous := lookupNetIP
	lookupNetIP = func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
		values, ok := answers[host]
		if !ok {
			return nil, errors.New("no such host")
		}
		addrs := make([]netip.Addr, len(values))
		for i, v := range values {
			addrs[i] = netip.MustParseAddr(v)
		}
		return addrs, nil
	}
	t.Cleanup(func() { lookupNetIP = previous })
}

func TestCheckURLBlocksPrivateTargets(t *testing.T) {
	withResolver(t, map[string][]string{
		"public.test": {"93.184.216.34"},
		"mixed.test":  {"93.184.216.34", "10.0.0.5"},
		"lan.test":    {"192.168.1.10"},
	})
	cases := []struct {
		name    string
		raw     string
		blocked bool
		private bool
	}{
		{"loopback literal", "http://127.0.0.1:8080/sub", true, true},
		{"metadata", "http://169.254.169.254/latest/meta-data/", true, true},
		{"private name", "https://lan.test/sub", true, true},
		{"mixed answer", "https://mixed.test/sub", true, true},
		{"public name", "https://public.test/sub", false, false},
		{"public literal", "https://1.1.1.1/", false, false},
		{"unsupported scheme", "file:///etc/passwd", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			err = CheckURL(context.Background(), u)
			if tc.blocked && err == nil {
				t.Fatalf("CheckURL(%s) = nil, want an error", tc.raw)
			}
			if !tc.blocked && err != nil {
				t.Fatalf("CheckURL(%s) = %v, want nil", tc.raw, err)
			}
			if tc.private && !errors.Is(err, ErrPrivateAddress) {
				t.Fatalf("CheckURL(%s) = %v, want ErrPrivateAddress", tc.raw, err)
			}
		})
	}
}

func TestCheckURLHonoursAllowPrivate(t *testing.T) {
	SetAllowPrivate(true)
	t.Cleanup(func() { SetAllowPrivate(false) })
	if err := CheckURL(context.Background(), mustURL(t, "http://127.0.0.1:2017/")); err != nil {
		t.Fatalf("allowed private URL refused: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(req *http.Request, status int, location string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}
	if location != "" {
		resp.Header.Set("Location", location)
	}
	return resp
}

func TestWrapClientChecksEveryRedirect(t *testing.T) {
	SetAllowPrivate(false)
	withResolver(t, map[string][]string{
		"public.test": {"93.184.216.34"},
	})
	calls := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return response(req, http.StatusFound, "https://127.0.0.1:2017/api/account"), nil
	})
	c := WrapClient(&http.Client{Transport: base})
	resp, err := c.Get("https://public.test/sub")
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, ErrPrivateAddress) {
		t.Fatalf("redirect to loopback: err = %v, want ErrPrivateAddress", err)
	}
	if calls != 1 {
		t.Fatalf("the fake transport ran %d times, want 1", calls)
	}
}

func TestWrapClientRefusesHTTPSDowngrade(t *testing.T) {
	SetAllowPrivate(false)
	withResolver(t, map[string][]string{
		"public.test": {"93.184.216.34"},
	})
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusFound, "http://public.test/next"), nil
	})
	c := WrapClient(&http.Client{Transport: base})
	resp, err := c.Get("https://public.test/start")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "https to http") {
		t.Fatalf("downgrade err = %v, want a refusal naming the downgrade", err)
	}
}

func TestWrapClientBoundsRedirects(t *testing.T) {
	SetAllowPrivate(false)
	withResolver(t, map[string][]string{
		"public.test": {"93.184.216.34"},
	})
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusFound, "https://public.test/again"), nil
	})
	c := WrapClient(&http.Client{Transport: base})
	resp, err := c.Get("https://public.test/start")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "stopped after") {
		t.Fatalf("redirect loop err = %v, want a bounded-redirect error", err)
	}
}

func TestGuardedDialerUsesTheCheckedAddress(t *testing.T) {
	dialed := ""
	dialer := guardedDialer{base: func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = address
		return nil, errors.New("stop")
	}}
	ctx := context.WithValue(context.Background(), targetKey{}, targetAddrs{
		host:  "public.test",
		addrs: []netip.Addr{netip.MustParseAddr("93.184.216.34")},
	})
	_, _ = dialer.DialContext(ctx, "tcp", "public.test:443")
	if dialed != "93.184.216.34:443" {
		t.Fatalf("dialed %q, want the checked address", dialed)
	}
	_, _ = dialer.DialContext(context.Background(), "tcp", "proxy.test:1080")
	if dialed != "proxy.test:1080" {
		t.Fatalf("a proxy dial was rewritten to %q", dialed)
	}
}

func TestWrapClientIdempotent(t *testing.T) {
	SetAllowPrivate(false)
	c := WrapClient(&http.Client{})
	if WrapClient(c) != c {
		t.Fatal("a guarded client must not be wrapped twice")
	}
}

func TestWrapClientNilUsesDefault(t *testing.T) {
	SetAllowPrivate(false)
	if c := WrapClient(nil); c == nil || c == http.DefaultClient {
		t.Fatal("a nil client must become a guarded copy, not the shared default")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
