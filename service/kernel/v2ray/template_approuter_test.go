package v2ray

import (
	"testing"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/approuter"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
)

func appRouterSetting() *configure.Setting {
	s := configure.NewSetting()
	s.Transparent = configure.TransparentProxy
	s.TransparentType = configure.TransparentAppRouter
	return s
}

// withProxyOutbound gives the template one outbound a class can stand for;
// without one there is no outbound name to route to.
func withProxyOutbound(tmpl *Template) {
	tmpl.Outbounds = []coreObj.OutboundObject{{Tag: "proxy", Protocol: "vmess"}}
}

func findClassInbound(tmpl *Template, tag string) *coreObj.Inbound {
	for i := range tmpl.Inbounds {
		if tmpl.Inbounds[i].Tag == tag {
			return &tmpl.Inbounds[i]
		}
	}
	return nil
}

// A class is a loopback SOCKS inbound: the relay dials it, and the core decides
// from the inbound which outbound the class means.
func TestAppRouterModeAddsALoopbackClassInbound(t *testing.T) {
	withPorts(t)
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	inbound := findClassInbound(tmpl, AppRouterClassTag(1))
	if inbound == nil {
		t.Fatal("no class inbound was generated")
	}
	if inbound.Protocol != "socks" {
		t.Errorf("protocol %q, want socks", inbound.Protocol)
	}
	if inbound.Listen != "127.0.0.1" {
		t.Errorf("listen %q, want loopback: the class must not be reachable from the network", inbound.Listen)
	}
	if inbound.Port != approuter.ClassPort(1) {
		t.Errorf("port %d, want %d", inbound.Port, approuter.ClassPort(1))
	}
	if inbound.Settings == nil || inbound.Settings.UDP {
		t.Errorf("the first stage carries TCP only, settings %+v", inbound.Settings)
	}
}

// The class inbound is fed by the engine, which has already decided: the rule
// carries the class to the proxy outbound, and the transparent policy adds
// nothing.
func TestAppRouterModeRoutesTheClassToTheDefaultOutbound(t *testing.T) {
	withPorts(t)
	setting := appRouterSetting()
	setting.Transparent = configure.TransparentAllowlist
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
		t.Fatal(err)
	}
	var found *coreObj.RoutingRule
	for i := range tmpl.Routing.Rules {
		for _, tag := range tmpl.Routing.Rules[i].InboundTag {
			if tag == AppRouterClassTag(1) {
				found = &tmpl.Routing.Rules[i]
			}
		}
	}
	if found == nil {
		t.Fatalf("no rule routes the class inbound: %+v", tmpl.Routing.Rules)
	}
	if found.OutboundTag != "proxy" {
		t.Errorf("class routed to %q, want the proxy outbound", found.OutboundTag)
	}
	// The policy is the rules' business in this mode; an allowlist of IPs is
	// not what the class inbound should be filtered by.
	if len(tmpl.Routing.Rules) > 1 {
		for _, rule := range tmpl.Routing.Rules {
			if rule.InboundTag == nil || len(rule.InboundTag) == 0 {
				continue
			}
			for _, tag := range rule.InboundTag {
				if tag == "transparent" {
					t.Errorf("a rule for the transparent inbound was generated in app_router mode: %+v", rule)
				}
			}
		}
	}
}

// The mode needs root and a cgroup v2 mount; in lite mode it must not claim to
// be on, or the interface would offer a switch that cannot work.
func TestAppRouterModeIsOffInLiteMode(t *testing.T) {
	previous := *conf.GetEnvironmentConfig()
	config := previous
	config.Lite = true
	conf.SetConfig(config)
	t.Cleanup(func() { conf.SetConfig(previous) })

	if IsTransparentOn(appRouterSetting()) {
		t.Fatal("app_router reported itself on without root")
	}
}

func TestAppRouterClassTagRoundTrips(t *testing.T) {
	tag := AppRouterClassTag(3)
	class, ok := appRouterClassOf(tag)
	if !ok || class != 3 {
		t.Fatalf("%q -> %d, %v", tag, class, ok)
	}
	// setDualStack renames loopback inbounds; the class must survive it, or the
	// engine cannot find the port of its own class.
	for _, spelling := range []string{"approuter-3_ipv4", "approuter-3_ipv6"} {
		class, ok := appRouterClassOf(spelling)
		if !ok || class != 3 {
			t.Errorf("%q -> %d, %v", spelling, class, ok)
		}
	}
	for _, other := range []string{"approuter-", "approuter-x", "approuter-0", "approuter-1_ipv4x", "transparent", ""} {
		if _, ok := appRouterClassOf(other); ok {
			t.Errorf("%q was read as a class", other)
		}
	}
}
// The class inbound carries the destination the application asked for, read
// from the socket. Sniffing would replace it with a name from the payload (an
// HTTP Host header, say), and the outbound would dial that name instead.
func TestAppRouterClassInboundDoesNotSniff(t *testing.T) {
	withPorts(t)
	setting := appRouterSetting()
	setting.InboundSniffing = configure.InboundSniffingHttpTLS
	tmpl := baseTemplate(t, setting)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	class := findClassInbound(tmpl, AppRouterClassTag(1))
	if class == nil {
		t.Fatal("no class inbound was generated")
	}
	if class.Sniffing.Enabled || len(class.Sniffing.DestOverride) != 0 {
		t.Fatalf("the class inbound sniffs: %+v", class.Sniffing)
	}
	// Every other inbound keeps the behaviour the setting asks for.
	var socks *coreObj.Inbound
	for i := range tmpl.Inbounds {
		if tmpl.Inbounds[i].Tag == "socks" {
			socks = &tmpl.Inbounds[i]
		}
	}
	if socks == nil {
		t.Fatal("no socks inbound")
	}
	if !socks.Sniffing.Enabled {
		t.Fatal("the ordinary socks inbound lost its sniffing")
	}
}
