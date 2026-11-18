package v2ray

import (
	"strings"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/approuter"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
)

// process(...) is the app router's condition. The core's router has no matcher
// for an executable, so the rule must not reach it with the condition dropped.
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

func TestValidateRoutingARejectsABadProcessParameter(t *testing.T) {
	if err := ValidateRoutingA("process(uid: 1000)->proxy"); err == nil {
		t.Fatal("an unknown process parameter was accepted")
	}
	if err := ValidateRoutingA("process()->proxy"); err == nil {
		t.Fatal("an empty process(...) was accepted")
	}
}

func TestValidateRoutingAAcceptsAProcessRule(t *testing.T) {
	for _, text := range []string{
		"process(firefox)->proxy",
		`process(name: "chrom*", folder: /opt/apps)->direct`,
		"process(firefox) && port(443)->work",
	} {
		if err := ValidateRoutingA(text); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
}

func TestValidateRoutingAAcceptsTheConditionsTheRouterHas(t *testing.T) {
	text := `domain(domain: example.com) && ip(1.1.1.1) && port(443) && network(tcp)->proxy`
	if err := ValidateRoutingA(text); err != nil {
		t.Fatalf("a rule without process(...) was refused: %v", err)
	}
}

// A custom inbound's rules go straight to the core, so an executable condition
// there has nowhere to be applied.
func TestValidateCustomInboundRoutingARejectsAProcessCondition(t *testing.T) {
	if err := ValidateCustomInboundRoutingA("process(firefox)->direct"); err == nil {
		t.Fatal("a custom inbound accepted a process(...) condition")
	}
}

func withRoutingAText(t *testing.T, text string) {
	t.Helper()
	previous := configure.GetRoutingA()
	t.Cleanup(func() { _ = configure.SetRoutingA(&previous) })
	if err := configure.SetRoutingA(&text); err != nil {
		t.Fatal(err)
	}
}

// The core's rules must not carry a rule whose executable condition it cannot
// enforce; with the engine off that rule applies to nothing, and the log says
// so instead of the rule silently matching everything.
func TestParseRoutingASkipsAProcessRuleForTheCore(t *testing.T) {
	withRoutingAText(t, "process(firefox)->direct")
	tmpl := baseTemplate(t, configure.NewSetting())
	withProxyOutbound(tmpl)
	if err := parseRoutingA(tmpl, []string{"rule-http", "rule-socks"}); err != nil {
		t.Fatal(err)
	}
	for _, rule := range tmpl.Routing.Rules {
		if rule.OutboundTag == "direct" {
			t.Fatalf("the process rule reached the core: %+v", rule)
		}
	}
	if len(tmpl.Routing.Rules) != 1 {
		t.Fatalf("%d rules, want only the catch-all: %+v", len(tmpl.Routing.Rules), tmpl.Routing.Rules)
	}
}

func TestAppRouterModeGeneratesOneInboundPerOutbound(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, strings.Join([]string{
		"process(firefox)->work",
		"process(curl) && port(443)->proxy",
		"process(wget)->direct",
		"process(bitcoin)->block",
	}, "\n"))
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	tmpl.Outbounds = []coreObj.OutboundObject{
		{Tag: "proxy", Protocol: "vmess"},
		{Tag: "work", Protocol: "vmess"},
	}
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
		t.Fatal(err)
	}
	plan := tmpl.appRouter
	if plan == nil {
		t.Fatal("no plan was built")
	}
	if len(plan.rules) != 4 {
		t.Fatalf("%d engine rules, want 4", len(plan.rules))
	}
	if got := plan.classes[1]; got != "work" {
		t.Errorf("class 1 is %q, want the outbound named first", got)
	}
	if got := plan.classes[2]; got != "proxy" {
		t.Errorf("class 2 is %q", got)
	}
	if plan.rules[2].Action != approuter.ActionDirect || plan.rules[3].Action != approuter.ActionBlock {
		t.Errorf("direct/block were not kept: %+v", plan.rules)
	}

	class1 := findClassInbound(tmpl, AppRouterClassTag(1))
	class2 := findClassInbound(tmpl, AppRouterClassTag(2))
	if class1 == nil || class2 == nil {
		t.Fatalf("class inbounds missing: %+v", tmpl.Inbounds)
	}
	if class1.Port != approuter.ClassPort(1) || class2.Port != approuter.ClassPort(2) {
		t.Errorf("ports %d and %d", class1.Port, class2.Port)
	}
	if class1.Listen != "127.0.0.1" || class1.Protocol != "socks" {
		t.Errorf("class 1: %+v", class1)
	}
	// One rule per class, each naming its own outbound.
	routed := map[string]string{}
	for _, rule := range tmpl.Routing.Rules {
		if len(rule.InboundTag) == 1 && strings.HasPrefix(rule.InboundTag[0], "approuter-") {
			routed[rule.InboundTag[0]] = rule.OutboundTag
		}
	}
	if len(routed) != 2 || routed[AppRouterClassTag(1)] != "work" || routed[AppRouterClassTag(2)] != "proxy" {
		t.Fatalf("class rules: %+v", routed)
	}
}

func TestAppRouterRuleKeepsItsDestinationConditions(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, "process(curl) && ip(10.0.0.0/8, 192.0.2.1) && port(80, 443, 8000-8100)->proxy")
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
		t.Fatal(err)
	}
	rule := tmpl.appRouter.rules[0]
	wantHosts := []approuter.IPRange{{Lo: 0x0a000000, Hi: 0x0affffff}, {Lo: 0xc0000201, Hi: 0xc0000201}}
	if len(rule.V4) != 2 || rule.V4[0] != wantHosts[0] || rule.V4[1] != wantHosts[1] {
		t.Fatalf("hosts: %+v", rule.V4)
	}
	if len(rule.Ports) != 3 {
		t.Fatalf("ports: %+v", rule.Ports)
	}
	if rule.Ports[2] != (approuter.PortRange{Lo: 8000, Hi: 8100}) {
		t.Fatalf("the port range was not read: %+v", rule.Ports[2])
	}
}

func TestAppRouterRefusesConditionsItCannotEnforce(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"domain", "process(curl) && domain(example.com)->proxy"},
		{"sctp", "process(curl) && network(sctp)->proxy"},
		{"geoip", "process(curl) && ip(geoip:cn)->proxy"},
		{"protocol", "process(curl) && protocol(tls)->proxy"},
		{"source", "process(curl) && source(10.0.0.1)->proxy"},
		{"ipv6", `process(curl) && ip("2001:db8::/32")->proxy`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPorts(t)
			withRoutingAText(t, tc.text)
			setting := appRouterSetting()
			tmpl := baseTemplate(t, setting)
			withProxyOutbound(tmpl)
			if err := tmpl.setInbound(setting); err != nil {
				t.Fatal(err)
			}
			if err := tmpl.setTransparentRouting(); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

func TestAppRouterRefusesAnOutboundThatIsNotConnected(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, "process(curl)->nowhere")
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err == nil {
		t.Fatal("an outbound that does not exist was accepted")
	}
}

// With no process rule the mode routes nothing, and it must say so by not
// generating inbounds rather than by generating an unused one.
func TestAppRouterModeWithNoProcessRulesRoutesNothing(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, "default: proxy\ndomain(geosite:cn)->direct")
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
		t.Fatal(err)
	}
	if len(tmpl.appRouter.rules) != 0 {
		t.Fatalf("%d engine rules", len(tmpl.appRouter.rules))
	}
	for _, ib := range tmpl.Inbounds {
		if _, ok := appRouterClassOf(ib.Tag); ok {
			t.Fatalf("an inbound was generated for nothing: %+v", ib)
		}
	}
}

// The class inbound carries the destination the application asked for, read
// from the socket. Sniffing would replace it with a name from the payload (an
// HTTP Host header, say), and the outbound would dial that name instead.
func TestAppRouterClassInboundDoesNotSniff(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, "process(curl)->proxy")
	setting := appRouterSetting()
	setting.InboundSniffing = configure.InboundSniffingHttpTLS
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
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

// network(...) says which transport a rule is about; a rule that does not name
// one covers both, which is what a rule written before UDP existed means.
func TestAppRouterReadsTheNetworkCondition(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, `process(curl) && network(udp)->proxy
process(wget) && network(tcp, udp) && port(53)->direct
process(nc) && port(443)->direct`)
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
		t.Fatal(err)
	}
	plan := tmpl.appRouter
	if len(plan.rules) != 3 {
		t.Fatalf("%d rules", len(plan.rules))
	}
	if plan.rules[0].Network != approuter.NetworkUDP {
		t.Errorf("the udp rule carries %d", plan.rules[0].Network)
	}
	if plan.rules[1].Network != approuter.NetworkBoth {
		t.Errorf("tcp, udp carries %d", plan.rules[1].Network)
	}
	if plan.rules[2].Network != 0 {
		t.Errorf("a rule without network(...) carries %d, and zero means both", plan.rules[2].Network)
	}
}

// UDP reaches the class over a SOCKS5 association, so the generated inbound
// has to accept one.
func TestAppRouterClassInboundAcceptsUDP(t *testing.T) {
	withPorts(t)
	withRoutingAText(t, "process(curl)->proxy")
	setting := appRouterSetting()
	tmpl := baseTemplate(t, setting)
	withProxyOutbound(tmpl)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.setTransparentRouting(); err != nil {
		t.Fatal(err)
	}
	class := findClassInbound(tmpl, AppRouterClassTag(1))
	if class == nil {
		t.Fatal("no class inbound was generated")
	}
	if class.Settings == nil || !class.Settings.UDP {
		t.Fatalf("the class inbound does not accept UDP: %+v", class.Settings)
	}
}