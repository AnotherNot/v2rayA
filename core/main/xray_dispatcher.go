package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/net/cnc"
	"github.com/xtls/xray-core/common/session"
	xcore "github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
)

// xrayDispatcherAdapter implements dns.RouteDispatcher using xray-core's
// internal routing dispatcher. This allows the DNS module to route queries
// through xray-core's proxy outbounds (VMESS, VLESS, Trojan, etc.) just like
// xray-core's built-in DNS module does.
//
// The outbound is forced directly on the dispatch context, bypassing the
// routing rules: a proxy tag names an outbound, not an inbound, and no
// routing rule matches the DNS module's traffic. Setting the tag as an
// inbound tag instead (as done before) matches nothing and the query falls
// through to the default route — usually direct — so a "proxy" upstream
// silently resolves directly. Forced dispatch is how xray itself dials a
// tagged outbound (see DialTaggedOutbound).
type xrayDispatcherAdapter struct {
	dispatcher routing.Dispatcher
}

// newXrayDispatcherAdapter creates a new adapter from an xray core instance.
func newXrayDispatcherAdapter(srv *xcore.Instance) (*xrayDispatcherAdapter, error) {
	var dispatcher routing.Dispatcher
	if err := srv.RequireFeatures(func(d routing.Dispatcher) {
		dispatcher = d
	}, false); err != nil {
		return nil, fmt.Errorf("xray dispatcher not available: %w", err)
	}
	return &xrayDispatcherAdapter{dispatcher: dispatcher}, nil
}

// Dispatch implements dns.RouteDispatcher.
// It creates a TCP connection to addr through xray-core's routing engine.
// A non-direct proxyTag forces that outbound directly, bypassing the routing
// rules; "direct" (and empty) use the default routing. Balancer group tags
// have no outbound handler and cannot be forced — the DNS module keeps those
// on the SOCKS path, which flows through the normal routing with balancers,
// so the dispatcher never sees them.
func (a *xrayDispatcherAdapter) Dispatch(ctx context.Context, network, addr, proxyTag string) (net.Conn, error) {
	// Parse the destination address.
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		portStr = "53"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 53
	}

	var dest xnet.Destination
	switch strings.ToLower(network) {
	case "tcp", "tcp4", "tcp6":
		dest = xnet.TCPDestination(xnet.ParseAddress(host), xnet.Port(port))
	case "udp", "udp4", "udp6":
		dest = xnet.UDPDestination(xnet.ParseAddress(host), xnet.Port(port))
	default:
		dest = xnet.TCPDestination(xnet.ParseAddress(host), xnet.Port(port))
	}

	// Mark as DNS traffic to prevent recursive DNS resolution. The content
	// must be set before the forced tag: the tag lives on the content as an
	// attribute, and replacing the content afterwards would drop it.
	dnsCtx := session.ContextWithContent(ctx, &session.Content{
		Protocol:       "dns",
		SkipDNSResolve: true,
	})
	// Force the named outbound directly. For "direct" nothing is forced and
	// the default routing applies.
	if proxyTag != "" && proxyTag != "direct" {
		dnsCtx = session.SetForcedOutboundTagToContext(dnsCtx, proxyTag)
	}

	// Dispatch through xray-core's routing engine.
	link, err := a.dispatcher.Dispatch(dnsCtx, dest)
	if err != nil {
		return nil, fmt.Errorf("xray dispatch: %w", err)
	}

	// Wrap the transport link into a standard net.Conn.
	return cnc.NewConnection(
		cnc.ConnectionInputMulti(link.Writer),
		cnc.ConnectionOutputMulti(link.Reader),
	), nil
}
