package v2ray

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/v2rayA/RoutingA"
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
			ranges, err := ipRangesOf(f)
			if err != nil {
				return out, true, fmt.Errorf("the rule for %q: %w", r.Out, err)
			}
			out.V4 = append(out.V4, ranges...)
		case "port":
			ranges, err := portRangesOf(f)
			if err != nil {
				return out, true, fmt.Errorf("the rule for %q: %w", r.Out, err)
			}
			out.Ports = append(out.Ports, ranges...)
		case "network":
			for _, network := range f.Params {
				if strings.TrimSpace(network) != "tcp" {
					return out, true, fmt.Errorf("the rule for %q names network %q, and application rules carry TCP only for now", r.Out, network)
				}
			}
		case "domain", "domains":
			return out, true, fmt.Errorf("the rule for %q selects on a domain, which application rules cannot carry yet; use an address or port", r.Out)
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
func ipRangesOf(f RoutingA.Function) ([]approuter.IPRange, error) {
	var values []string
	for key, vv := range f.NamedParams {
		for _, v := range vv {
			values = append(values, key+":"+v)
		}
	}
	values = append(values, f.Params...)
	var ranges []approuter.IPRange
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "geoip:") || strings.HasPrefix(value, "ext:") {
			return nil, fmt.Errorf("ip(%s) is a database reference, and application rules read literal addresses for now", value)
		}
		r, err := ipRangeOf(value)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

func ipRangeOf(value string) (approuter.IPRange, error) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		prefix = prefix.Masked()
		addr := prefix.Addr()
		if !addr.Is4() {
			return approuter.IPRange{}, fmt.Errorf("ip(%s) is an IPv6 range, and application rules carry IPv4 only for now", value)
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
		return approuter.IPRange{}, fmt.Errorf("ip(%s) is an IPv6 address, and application rules carry IPv4 only for now", value)
	}
	v4 := addr.As4()
	n := binary.BigEndian.Uint32(v4[:])
	return approuter.IPRange{Lo: n, Hi: n}, nil
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