package v2ray

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/approuter"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// appRouterPlan is what the rules mean for the engine: the ordered list it
// applies, and the outbound each class stands for.
type appRouterPlan struct {
	rules []approuter.Rule
	// classes maps a class number to the outbound tag it routes to, in the
	// order the rules first named them.
	classes map[int]string
	order   []int
}

// compileAppRouter reads the RoutingA text and turns every rule that selects on
// process(...) into one engine rule. It runs whether or not the rule port uses
// RoutingA: those rules are the app router's own policy, and the rest of the
// text keeps following the rule port's mode.
//
// A condition the engine cannot evaluate is an error, not something ignored:
// a rule that loses a condition matches more than its author wrote.
func (t *Template) compileAppRouter() (*appRouterPlan, error) {
	plan := &appRouterPlan{classes: map[int]string{}}
	text := expandRoutingAEnv(configure.GetRoutingA(), "compileAppRouter")
	if strings.TrimSpace(text) == "" {
		return plan, nil
	}
	rules, err := RoutingA.Parse(text)
	if err != nil {
		return nil, err
	}
	outboundTags := t.outNames()
	for _, rule := range rules {
		r, ok := rule.(RoutingA.Routing)
		if !ok {
			continue
		}
		engineRule, isAppRule, err := t.appRuleOf(r, plan, outboundTags)
		if err != nil {
			return nil, err
		}
		if isAppRule {
			plan.rules = append(plan.rules, engineRule)
		}
	}
	return plan, nil
}

// appRuleOf turns one RoutingA rule into an engine rule, and reports whether it
// was an application rule at all.
func (t *Template) appRuleOf(r RoutingA.Routing, plan *appRouterPlan, outboundTags map[string]bool) (approuter.Rule, bool, error) {
	var (
		out     approuter.Rule
		matcher *RoutingA.ProcessMatcher
	)
	for _, f := range r.And {
		switch f.Name {
		case "process":
			if matcher != nil {
				return out, true, fmt.Errorf("the rule for %q selects on process(...) twice; conditions are joined with AND, so one is enough", r.Out)
			}
			m, isProcess, err := f.ProcessMatcher()
			if err != nil {
				return out, true, err
			}
			if !isProcess {
				continue
			}
			matcher = &m
		}
	}
	if matcher == nil {
		return out, false, nil
	}
	out.Apps = *matcher

	for _, f := range r.And {
		switch f.Name {
		case "process":
			// Already read above.
		case "ip":
			v4, v6, err := ipRangesOf(f)
			if err != nil {
				return out, true, fmt.Errorf("the rule for %q: %w", r.Out, err)
			}
			out.V4 = append(out.V4, v4...)
			out.V6 = append(out.V6, v6...)
		case "port":
			ranges, err := portRangesOf(f)
			if err != nil {
				return out, true, fmt.Errorf("the rule for %q: %w", r.Out, err)
			}
			out.Ports = append(out.Ports, ranges...)
		case "network":
			if len(f.Params) == 0 {
				return out, true, fmt.Errorf("the rule for %q has an empty network(...)", r.Out)
			}
			for _, network := range f.Params {
				switch strings.TrimSpace(network) {
				case "tcp":
					out.Network |= approuter.NetworkTCP
				case "udp":
					out.Network |= approuter.NetworkUDP
				default:
					return out, true, fmt.Errorf("the rule for %q names network %q, and application rules carry tcp and udp", r.Out, network)
				}
			}
		case "domain", "domains":
			patterns, err := domainPatternsOf(f)
			if err != nil {
				return out, true, fmt.Errorf("the rule for %q: %w", r.Out, err)
			}
			out.Domains = append(out.Domains, patterns...)
		case "protocol":
			return out, true, fmt.Errorf("the rule for %q selects on the sniffed protocol, which application rules cannot carry", r.Out)
		case "source", "sourcePort", "user", "inboundTag":
			return out, true, fmt.Errorf("the rule for %q selects on %s(...), which application rules cannot carry", r.Out, f.Name)
		default:
			return out, true, fmt.Errorf("the rule for %q selects on %s(...), which application rules cannot carry", r.Out, f.Name)
		}
	}

	switch r.Out {
	case "direct":
		out.Action = approuter.ActionDirect
	case "block":
		out.Action = approuter.ActionBlock
	case "":
		return out, true, fmt.Errorf("a rule that selects on process(...) needs an outbound")
	default:
		if _, ok := outboundTags[r.Out]; !ok {
			return out, true, fmt.Errorf(`your RoutingA rules depend on the outbound "%v", thus you should select at least one server in this outbound`, r.Out)
		}
		class, ok := classOf(plan, r.Out)
		if !ok {
			return out, true, fmt.Errorf("too many outbounds are named by application rules; at most %d fit", approuter.MaxClass)
		}
		out.Action = approuter.ActionProxy
		out.Class = class
	}
	return out, true, nil
}

// classOf gives an outbound its class number, in the order the rules first name
// it, so the generated inbounds and rules agree with the engine.
func classOf(plan *appRouterPlan, outbound string) (int, bool) {
	for class, tag := range plan.classes {
		if tag == outbound {
			return class, true
		}
	}
	if len(plan.classes) >= approuter.MaxClass {
		return 0, false
	}
	class := len(plan.classes) + 1
	plan.classes[class] = outbound
	plan.order = append(plan.order, class)
	return class, true
}

// ipRangesOf reads the literal addresses and CIDRs of an ip(...) condition.
// The families are returned apart because the engine matches an IPv4
// destination against IPv4 ranges only, and an IPv6 one against IPv6 ranges.
// A colon means IPv6: an IPv4 literal never carries one.
func ipRangesOf(f RoutingA.Function) ([]approuter.IPRange, []approuter.IPRange6, error) {
	var values []string
	for key, vv := range f.NamedParams {
		for _, v := range vv {
			values = append(values, key+":"+v)
		}
	}
	values = append(values, f.Params...)
	var v4 []approuter.IPRange
	var v6 []approuter.IPRange6
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "geoip:") || strings.HasPrefix(value, "ext:") {
			return nil, nil, fmt.Errorf("ip(%s) is a database reference, and application rules read literal addresses for now", value)
		}
		if strings.Contains(value, ":") {
			r, err := ipRange6Of(value)
			if err != nil {
				return nil, nil, err
			}
			v6 = append(v6, r)
			continue
		}
		r, err := ipRangeOf(value)
		if err != nil {
			return nil, nil, err
		}
		v4 = append(v4, r)
	}
	return v4, v6, nil
}

func ipRangeOf(value string) (approuter.IPRange, error) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		prefix = prefix.Masked()
		addr := prefix.Addr()
		if !addr.Is4() {
			return approuter.IPRange{}, fmt.Errorf("ip(%s) is an IPv6 range, and a rule that names one family does not match the other", value)
		}
		v4 := addr.As4()
		lo := binary.BigEndian.Uint32(v4[:])
		hi := lo | (0xffffffff >> prefix.Bits())
		return approuter.IPRange{Lo: lo, Hi: hi}, nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return approuter.IPRange{}, fmt.Errorf("ip(%s) is not an address or a CIDR", value)
	}
	if !addr.Is4() {
		return approuter.IPRange{}, fmt.Errorf("ip(%s) is an IPv6 address, and a rule that names one family does not match the other", value)
	}
	v4 := addr.As4()
	n := binary.BigEndian.Uint32(v4[:])
	return approuter.IPRange{Lo: n, Hi: n}, nil
}

// ipRange6Of reads one IPv6 value: an address, a CIDR or "first-last". A
// v4-mapped literal is refused: a v4-mapped destination is an IPv4 destination
// and is matched against IPv4 ranges, so the rule would never apply.
func ipRange6Of(value string) (approuter.IPRange6, error) {
	if first, last, ok := strings.Cut(value, "-"); ok {
		lo, err := ipv6AddrOf(strings.TrimSpace(first), value)
		if err != nil {
			return approuter.IPRange6{}, err
		}
		hi, err := ipv6AddrOf(strings.TrimSpace(last), value)
		if err != nil {
			return approuter.IPRange6{}, err
		}
		if lo.Compare(hi) > 0 {
			return approuter.IPRange6{}, fmt.Errorf("ip(%s) is reversed", value)
		}
		return approuter.IPRange6{Lo: v6Words(lo), Hi: v6Words(hi)}, nil
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		prefix = prefix.Masked()
		if prefix.Addr().Is4() {
			return approuter.IPRange6{}, fmt.Errorf("ip(%s) is an IPv4 range, and it is written without a colon", value)
		}
		if prefix.Addr().Is4In6() {
			return approuter.IPRange6{}, fmt.Errorf("ip(%s) is a v4-mapped range; write the IPv4 range instead", value)
		}
		lo := v6Words(prefix.Addr())
		hi := lo
		for i := range hi {
			switch keep := prefix.Bits() - i*32; {
			case keep <= 0:
				hi[i] = ^uint32(0)
			case keep < 32:
				hi[i] |= ^uint32(0) >> keep
			}
		}
		return approuter.IPRange6{Lo: lo, Hi: hi}, nil
	}
	addr, err := ipv6AddrOf(value, value)
	if err != nil {
		return approuter.IPRange6{}, err
	}
	w := v6Words(addr)
	return approuter.IPRange6{Lo: w, Hi: w}, nil
}

// ipv6AddrOf parses one IPv6 literal. value is the whole ip(...) parameter,
// which is what the error names.
func ipv6AddrOf(literal, value string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(literal)
	if err != nil || !addr.Is6() {
		return netip.Addr{}, fmt.Errorf("ip(%s) is not an IPv6 address, a CIDR or a range", value)
	}
	if addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("ip(%s) names an interface; a rule matches an address, not a link", value)
	}
	if addr.Is4In6() {
		return netip.Addr{}, fmt.Errorf("ip(%s) is a v4-mapped address; write the IPv4 address instead", value)
	}
	return addr, nil
}

// v6Words splits a sixteen-byte address into four host-order words, most
// significant first, which is what the kernel compares.
func v6Words(addr netip.Addr) [4]uint32 {
	b := addr.As16()
	var w [4]uint32
	for i := range w {
		w[i] = binary.BigEndian.Uint32(b[i*4:])
	}
	return w
}

// domainPatternsOf reads the values of a domain(...) or domains(...)
// condition. A named value carries its prefix ("full: a.com"); a bare one is a
// suffix match, which is what `domain: a.com` means in the editor.
func domainPatternsOf(f RoutingA.Function) ([]approuter.DomainPattern, error) {
	var values []string
	for key, vv := range f.NamedParams {
		for _, v := range vv {
			values = append(values, key+":"+v)
		}
	}
	values = append(values, f.Params...)
	var patterns []approuter.DomainPattern
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		p, err := approuter.CompileDomainPattern(value)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, nil
}

// hasDomainRules reports whether any rule selects on a name. Such a rule cannot
// be decided by the kernel, so every flow of the executables it names goes
// through the service, and the service needs what the DNS module answered.
func (p *appRouterPlan) hasDomainRules() bool {
	for i := range p.rules {
		if len(p.rules[i].Domains) > 0 {
			return true
		}
	}
	return false
}

// ValidateAppRouterDomains refuses a setting or a rule text that together would
// leave a domain rule with no way to learn a name. The config build refuses the
// same combination; this is what lets the editor and the settings page say so
// while the user is looking at what they wrote.
func ValidateAppRouterDomains(setting *configure.Setting, text string) error {
	if setting == nil || setting.TransparentType != configure.TransparentAppRouter {
		return nil
	}
	if configure.ResolveDnsMode(setting) == configure.DnsModeHijack {
		return nil
	}
	if !routingAHasDomainRules(text) {
		return nil
	}
	return fmt.Errorf("a rule selects on a domain, so the DNS module has to stay in the path of the answer: choose the hijack DNS mode, or remove the domain condition")
}

// routingAHasDomainRules reports whether a rule that selects on process(...)
// also selects on a name.
func routingAHasDomainRules(text string) bool {
	rules, err := RoutingA.Parse(text)
	if err != nil {
		return false
	}
	for _, rule := range rules {
		r, ok := rule.(RoutingA.Routing)
		if !ok {
			continue
		}
		var process, domain bool
		for _, f := range r.And {
			switch f.Name {
			case "process":
				process = true
			case "domain", "domains":
				domain = true
			}
		}
		if process && domain {
			return true
		}
	}
	return false
}

// dnsReportSocket is where the DNS module reports the names it answered, and
// the engine listens. A Unix socket in the configuration directory needs no
// port, and the file's mode keeps other users out.
func dnsReportSocket() string {
	return filepath.Join(conf.GetEnvironmentConfig().Config, "approuter-dns.sock")
}

// appRouterDNSReport is the socket the DNS module has to report to, or "" when
// no rule selects on a name. A name is not in the packet, so the module that
// answered it has to say what it answered; that only happens when the system's
// queries actually reach the module, which is the hijack mode.
func (t *Template) appRouterDNSReport() (string, error) {
	if t.Setting == nil || t.Setting.TransparentType != configure.TransparentAppRouter {
		return "", nil
	}
	plan, err := t.compileAppRouter()
	if err != nil {
		return "", err
	}
	if !plan.hasDomainRules() {
		return "", nil
	}
	if configure.ResolveDnsMode(t.Setting) != configure.DnsModeHijack {
		return "", fmt.Errorf("a rule selects on a domain, so the DNS module has to stay in the path of the answer: choose the hijack DNS mode, or remove the domain condition")
	}
	return dnsReportSocket(), nil
}

// portRangesOf reads port(80, 443, 1000-2000).
func portRangesOf(f RoutingA.Function) ([]approuter.PortRange, error) {
	var ranges []approuter.PortRange
	for _, value := range f.Params {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			lo, hi, err := portRangeOf(part)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, approuter.PortRange{Lo: lo, Hi: hi})
		}
	}
	return ranges, nil
}

func portRangeOf(value string) (uint16, uint16, error) {
	if i := strings.IndexByte(value, '-'); i >= 0 {
		lo, err := strconv.Atoi(strings.TrimSpace(value[:i]))
		if err != nil {
			return 0, 0, fmt.Errorf("port(%s) is not a range", value)
		}
		hi, err := strconv.Atoi(strings.TrimSpace(value[i+1:]))
		if err != nil {
			return 0, 0, fmt.Errorf("port(%s) is not a range", value)
		}
		if lo < 0 || hi > 65535 || lo > hi {
			return 0, 0, fmt.Errorf("port(%s) is outside 0-65535 or reversed", value)
		}
		return uint16(lo), uint16(hi), nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 0 || port > 65535 {
		return 0, 0, fmt.Errorf("port(%s) is not a port", value)
	}
	return uint16(port), uint16(port), nil
}

// logInactiveAppRules names rules the engine will not see, so a user who wrote
// them is not left wondering why nothing happens.
func logInactiveAppRules() {
	text := expandRoutingAEnv(configure.GetRoutingA(), "logInactiveAppRules")
	rules, err := RoutingA.Parse(text)
	if err != nil {
		return
	}
	inactive := 0
	for _, rule := range rules {
		r, ok := rule.(RoutingA.Routing)
		if !ok {
			continue
		}
		for _, f := range r.And {
			if f.Name == "process" {
				inactive++
				break
			}
		}
	}
	if inactive > 0 {
		log.Warn("RoutingA: %d rule(s) select on process(...) and are inactive while the transparent proxy is not in app_router mode", inactive)
	}
}
