package main

import (
	"context"
	"errors"
	"testing"

	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/transport"
)

// captureDispatcher records the context the adapter built and fails the
// dial, so the tests observe the routing decision without any network.
type captureDispatcher struct {
	ctx context.Context
}

func (d *captureDispatcher) Type() interface{} { return (*captureDispatcher)(nil) }
func (d *captureDispatcher) Start() error      { return nil }
func (d *captureDispatcher) Close() error      { return nil }
func (d *captureDispatcher) Dispatch(ctx context.Context, _ net.Destination) (*transport.Link, error) {
	d.ctx = ctx
	return nil, errors.New("capture only")
}
func (d *captureDispatcher) DispatchLink(_ context.Context, _ net.Destination, _ *transport.Link) error {
	return errors.New("capture only")
}

// A proxy tag forces that outbound directly: the dispatcher must see the
// forced tag, the DNS content marker, and no inbound tag to misroute on.
func TestDispatchForcesOutbound(t *testing.T) {
	inner := &captureDispatcher{}
	a := &xrayDispatcherAdapter{dispatcher: inner}
	_, err := a.Dispatch(context.Background(), "tcp", "1.0.0.1:53", "proxy")
	if err == nil {
		t.Fatal("expected the capture error")
	}
	if got := session.GetForcedOutboundTagFromContext(inner.ctx); got != "proxy" {
		t.Fatalf("forced tag = %q, want proxy", got)
	}
	content := session.ContentFromContext(inner.ctx)
	if content == nil || content.Protocol != "dns" || !content.SkipDNSResolve {
		t.Fatalf("content = %+v, want dns with SkipDNSResolve", content)
	}
	if in := session.InboundFromContext(inner.ctx); in != nil {
		t.Fatalf("inbound = %+v, want none: an inbound tag matches no rule and falls through to direct", in)
	}
}

// "direct" (and empty) force nothing and use the default routing.
func TestDispatchDirectForcesNothing(t *testing.T) {
	for _, tag := range []string{"direct", ""} {
		inner := &captureDispatcher{}
		a := &xrayDispatcherAdapter{dispatcher: inner}
		_, err := a.Dispatch(context.Background(), "tcp", "223.5.5.5:53", tag)
		if err == nil {
			t.Fatal("expected the capture error")
		}
		if got := session.GetForcedOutboundTagFromContext(inner.ctx); got != "" {
			t.Fatalf("tag %q: forced = %q, want none", tag, got)
		}
	}
}
