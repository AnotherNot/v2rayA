// Package netguard decides which addresses the service may fetch.
//
// Subscription, rule-source, DNS-source and speed-test addresses are supplied
// by a user or by a provider the user trusts only for its own content. A
// redirect from such an address must not turn the service into a probe of the
// host's loopback, its LAN, a cloud metadata endpoint or any other private
// range. The guard resolves the target before a request and refuses a name
// that maps to a blocked address; a direct connection then dials the address
// it already checked, so a second DNS answer cannot move the request.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// MaxRedirects is the number of redirects a guarded client follows. A source
// that keeps redirecting is probing the fetch path, not serving a document.
const MaxRedirects = 3

var allowPrivate atomic.Bool

// SetAllowPrivate permits fetches to private addresses. It is the runtime form
// of --allow-private-fetch: an operator who keeps subscriptions on a LAN NAS
// must ask for that reach explicitly.
func SetAllowPrivate(v bool) { allowPrivate.Store(v) }

// AllowPrivate reports whether the operator allowed private fetches.
func AllowPrivate() bool { return allowPrivate.Load() }

// ErrPrivateAddress is wrapped by every refusal, so a caller can tell a
// blocked target from a transport failure without parsing the message.
var ErrPrivateAddress = errors.New("private network address")

// BlockedError names the host and the address that the policy refused. The
// host is safe to show; the request URL is not, because it can carry a token.
type BlockedError struct {
	Host string
	Addr netip.Addr
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("%s resolves to the private address %s", e.Host, e.Addr)
}

func (e *BlockedError) Unwrap() error { return ErrPrivateAddress }

// blockedPrefixes are the ranges that a public source can never legitimately
// occupy. netip's IsPrivate/IsLoopback cover the common ones; these are the
// remainder that still route inside an operator's network or are reserved.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space (CGNAT)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking, fake-IP range
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64, maps IPv4
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
}

// lookupNetIP is a variable so tests can answer without a resolver.
var lookupNetIP = net.DefaultResolver.LookupNetIP

// IsBlocked reports whether addr is one a public fetch must not reach. An
// invalid address is blocked: a failure to parse must not widen the policy.
func IsBlocked(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// CheckHost resolves a host and refuses it when any answer is blocked. A
// literal address is checked without a lookup.
func CheckHost(ctx context.Context, host string) error {
	_, err := resolveHost(ctx, host)
	return err
}

// CheckURL refuses a URL whose host resolves to a blocked address. It also
// rejects a scheme other than HTTP or HTTPS: the fetch path speaks only those.
func CheckURL(ctx context.Context, u *url.URL) error {
	_, _, err := resolveURL(ctx, u)
	return err
}

func resolveURL(ctx context.Context, u *url.URL) (string, []netip.Addr, error) {
	if AllowPrivate() {
		return "", nil, nil
	}
	if u == nil {
		return "", nil, errors.New("netguard: nil URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", nil, fmt.Errorf("netguard: unsupported URL scheme %q", u.Scheme)
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	addrs, err := resolveHost(ctx, host)
	if err != nil {
		return "", nil, err
	}
	return host, addrs, nil
}

func resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, errors.New("netguard: URL has no host")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if IsBlocked(addr) {
			return nil, &BlockedError{Host: host, Addr: addr}
		}
		return []netip.Addr{addr}, nil
	}
	addrs, err := lookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("netguard: cannot resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("netguard: %s has no address", host)
	}
	for _, addr := range addrs {
		if IsBlocked(addr) {
			return nil, &BlockedError{Host: host, Addr: addr}
		}
	}
	return addrs, nil
}

// guardedTransport checks every request, including each redirect, before the
// underlying transport sees it.
type guardedTransport struct {
	base http.RoundTripper
}

type targetKey struct{}

type targetAddrs struct {
	host  string
	addrs []netip.Addr
}

func (t guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if AllowPrivate() {
		return t.base.RoundTrip(req)
	}
	host, addrs, err := resolveURL(req.Context(), req.URL)
	if err != nil {
		return nil, err
	}
	if len(addrs) > 0 {
		req = req.WithContext(context.WithValue(req.Context(), targetKey{}, targetAddrs{host: host, addrs: addrs}))
	}
	return t.base.RoundTrip(req)
}

// guardedDialer dials the addresses the request already checked. A proxied
// request dials its proxy instead; the target was still checked in RoundTrip,
// and the proxy's own resolution is outside this process's control.
type guardedDialer struct {
	base func(context.Context, string, string) (net.Conn, error)
}

func (d guardedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return d.base(ctx, network, address)
	}
	target, ok := ctx.Value(targetKey{}).(targetAddrs)
	if !ok || !strings.EqualFold(strings.TrimSuffix(target.host, "."), strings.TrimSuffix(host, ".")) {
		return d.base(ctx, network, address)
	}
	var lastErr error
	for _, addr := range target.addrs {
		conn, err := d.base(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("netguard: no address for %s", target.host)
	}
	return nil, lastErr
}

// WrapClient returns a copy of c that refuses blocked targets and bounds
// redirects. The copy shares the connection pool of the original transport,
// so wrapping does not open a second pool. A nil client becomes
// http.DefaultClient's client without modifying it.
func WrapClient(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	if AllowPrivate() {
		return c
	}
	if _, ok := c.Transport.(guardedTransport); ok {
		return c
	}
	clone := *c
	base := clone.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if tr, ok := base.(*http.Transport); ok {
		tr = tr.Clone()
		dial := tr.DialContext
		if dial == nil {
			dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			dial = dialer.DialContext
		}
		tr.DialContext = guardedDialer{base: dial}.DialContext
		base = tr
	}
	clone.Transport = guardedTransport{base: base}
	previous := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return fmt.Errorf("netguard: stopped after %d redirects", MaxRedirects)
		}
		if len(via) > 0 {
			prev := via[len(via)-1].URL
			if prev != nil && strings.EqualFold(prev.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("netguard: refusing the redirect from https to %s", req.URL.Scheme)
			}
		}
		if err := CheckURL(req.Context(), req.URL); err != nil {
			return err
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	return &clone
}
