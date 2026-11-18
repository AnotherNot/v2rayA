package v2ray

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohae/deepcopy"
	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/approuter"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
	"github.com/v2rayA/v2rayA/kernel/iptables"
	"github.com/v2rayA/v2rayA/kernel/v2ray/asset"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

func (t *Template) outNames() map[string]bool {
	tags := make(map[string]bool)
	for _, o := range t.Outbounds {
		if len(o.Balancers) > 0 {
			for _, groupName := range o.Balancers {
				tags[groupName] = true
			}
		} else {
			tags[o.Tag] = false
		}
	}
	return tags
}

func (t *Template) FirstProxyOutboundName(filter func(outboundName string, isGroup bool) bool) (outboundName string, isGroup bool) {
	if filter == nil {
		filter = func(outboundName string, isGroup bool) bool {
			return true
		}
	}
	// deduplicate
	m := make(map[string]struct{})

	for _, o := range t.Outbounds {
		switch o.Tag {
		case "direct", "block", "dns-out":
			continue
		}
		if len(o.Balancers) > 0 {
			for _, v := range o.Balancers {
				if _, ok := m[v]; !ok {
					if filter(v, true) {
						return v, true
					}
					m[v] = struct{}{}
				}
			}
		} else {
			if filter(o.Tag, false) {
				return o.Tag, false
			}
		}
	}
	return
}

// dnsRuleToLines converts a configure.DnsRule to the "server -> outbound" line format
// and the domain list used for the DNS server object.
// Returns (serverLine, []domains). serverLine is empty if server is empty.
func (t *Template) AppendRoutingRuleByMode(mode configure.RulePortMode, inbounds []string) (err error) {
	firstOutboundTag, _ := t.FirstProxyOutboundName(nil)
	// apple pushing. #495 #479
	t.Routing.Rules = append(t.Routing.Rules, coreObj.RoutingRule{
		Type:        "field",
		OutboundTag: "direct",
		InboundTag:  deepcopy.Copy(inbounds).([]string),
		Domain:      []string{"domain:push-apple.com.akadns.net", "domain:push.apple.com"},
	})
	switch mode {
	case configure.AllowlistMode:
		// foreign domains with intranet IP should be proxied first rather than directly connected
		if asset.DoesV2rayAssetExist(asset.BlocklistAsset) {
			t.Routing.Rules = append(t.Routing.Rules,
				coreObj.RoutingRule{
					Type:        "field",
					OutboundTag: firstOutboundTag,
					InboundTag:  deepcopy.Copy(inbounds).([]string),
					Domain:      []string{"ext:" + asset.BlocklistAsset + ":geolocation-!cn"},
				})
		} else {
			t.Routing.Rules = append(t.Routing.Rules,
				coreObj.RoutingRule{
					Type:        "field",
					OutboundTag: firstOutboundTag,
					InboundTag:  deepcopy.Copy(inbounds).([]string),
					Domain:      []string{"geosite:geolocation-!cn"},
				})
		}
		t.Routing.Rules = append(t.Routing.Rules,
			coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "proxy",
				InboundTag:  deepcopy.Copy(inbounds).([]string),
				// https://github.com/v2rayA/v2rayA/issues/285
				Domain: []string{"geosite:google"},
			},
			coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "direct",
				InboundTag:  deepcopy.Copy(inbounds).([]string),
				Domain:      []string{"geosite:cn"},
			},
			coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "proxy",
				InboundTag:  deepcopy.Copy(inbounds).([]string),
				IP:          []string{"geoip:hk", "geoip:mo"},
			},
			coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "direct",
				InboundTag:  deepcopy.Copy(inbounds).([]string),
				IP:          []string{"geoip:private", "geoip:cn"},
			},
		)
	case configure.BlocklistMode:
		if asset.DoesV2rayAssetExist(asset.BlocklistAsset) {
			t.Routing.Rules = append(t.Routing.Rules,
				coreObj.RoutingRule{
					Type:        "field",
					OutboundTag: firstOutboundTag,
					InboundTag:  deepcopy.Copy(inbounds).([]string),
					Domain:      []string{"ext:" + asset.BlocklistAsset + ":gfw"},
				},
				coreObj.RoutingRule{
					Type:        "field",
					OutboundTag: firstOutboundTag,
					InboundTag:  deepcopy.Copy(inbounds).([]string),
					Domain:      []string{"ext:" + asset.BlocklistAsset + ":greatfire"},
				})
		} else {
			t.Routing.Rules = append(t.Routing.Rules,
				coreObj.RoutingRule{
					Type:        "field",
					OutboundTag: firstOutboundTag,
					InboundTag:  deepcopy.Copy(inbounds).([]string),
					Domain:      []string{"geosite:geolocation-!cn"},
				})
		}

		t.Routing.Rules = append(t.Routing.Rules,
			coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: firstOutboundTag,
				InboundTag:  deepcopy.Copy(inbounds).([]string),
				// From: https://github.com/Loyalsoldier/geoip/blob/release/text/telegram.txt
				IP: []string{"91.105.192.0/23", "91.108.4.0/22", "91.108.8.0/21", "91.108.16.0/21", "91.108.56.0/22",
					"95.161.64.0/20", "149.154.160.0/20", "185.76.151.0/24", "2001:67c:4e8::/48", "2001:b28:f23c::/47",
					"2001:b28:f23f::/48", "2a0a:f280:203::/48"},
			},
			coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "direct",
				InboundTag:  deepcopy.Copy(inbounds).([]string),
			},
		)
	case configure.RoutingAMode:
		if err := parseRoutingA(t, deepcopy.Copy(inbounds).([]string)); err != nil {
			return err
		}
	}
	return nil
}

func (t *Template) setRulePortRouting() error {
	// append rule-http and rule-socks no mather if they are enabled
	// because "The Same as the Rule Port" may need them
	return t.AppendRoutingRuleByMode(t.Setting.RulePortMode, []string{"rule-http", "rule-socks"})
}
// ValidateRoutingA parses RoutingA source and reports conditions this build
// cannot enforce, before the text is stored or handed to the core.
func ValidateRoutingA(text string) error {
	rules, err := RoutingA.Parse(text)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		r, ok := rule.(RoutingA.Routing)
		if !ok {
			continue
		}
		for _, f := range r.And {
			if f.Name != "process" {
				continue
			}
			// The condition is the app router's, so its shape is checked here
			// rather than by the core: a parameter nothing can enforce is
			// refused while the user is looking at it.
			if _, isProcess, err := f.ProcessMatcher(); isProcess && err != nil {
				return fmt.Errorf("the rule for %q: %w", r.Out, err)
			}
		}
	}
	return nil
}

// ValidateCustomInboundRoutingA checks the rules of a custom inbound, which go
// straight to the core's router: an executable condition there would be dropped
// and the rule would match more than written.
func ValidateCustomInboundRoutingA(text string) error {
	if err := ValidateRoutingA(text); err != nil {
		return err
	}
	return validateCoreRoutingA(text)
}

// validateCoreRoutingA refuses a condition the core's router has no matcher
// for. Dropping one is not harmless: the rule keeps its other conditions and
// then matches more traffic than its author wrote. It is used where the text
// goes straight to the core — a custom inbound's own rules — and not for the
// main editor, whose process(...) rules the app router applies.
func validateCoreRoutingA(text string) error {
	rules, err := RoutingA.Parse(text)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		r, ok := rule.(RoutingA.Routing)
		if !ok {
			continue
		}
		for _, f := range r.And {
			if f.Name == "process" {
				return fmt.Errorf("RoutingA: the rule for %q selects on process(...), which a custom inbound cannot enforce; application rules belong in the main rule set, with the app router on", r.Out)
			}
		}
	}
	return nil
}

// hasProcessCondition reports whether a rule selects on the executable, which
// is the app router's business rather than the core's.
func hasProcessCondition(r RoutingA.Routing) bool {
	for _, f := range r.And {
		if f.Name == "process" {
			return true
		}
	}
	return false
}

// expandRoutingAEnv replaces $$NAME$$ placeholders with the environment's value,
// the way a deployment can keep a rule set's addresses out of the database.
func expandRoutingAEnv(text, who string) string {
	lines := strings.Split(text, "\n")
	hardcodeReplacement := regexp.MustCompile(`\$\$.+?\$\$`)
	for i := range lines {
		hardcodes := hardcodeReplacement.FindAllString(lines[i], -1)
		for _, hardcode := range hardcodes {
			env := strings.TrimSuffix(strings.TrimPrefix(hardcode, "$$"), "$$")
			val, ok := os.LookupEnv(env)
			if !ok {
				log.Error("%s: Environment variable \"%v\" is not found", who, env)
			} else {
				log.Info("%s: Environment variable %v=%v", who, env, strconv.Quote(val))
			}
			lines[i] = strings.Replace(lines[i], hardcode, val, 1)
		}
	}
	return strings.Join(lines, "\n")
}

func parseRoutingA(t *Template, routingInboundTags []string) error {
	// A rule source names the domain strategy its set was written for. It is
	// applied before the default is filled in, so a set asking for AsIs gets it
	// and a set asking for IPOnDemand — what the core defaults to anyway —
	// changes nothing.
	var src configure.RoutingSource
	configure.GetRoutingSource(&src)
	if src.DomainStrategy != "" {
		t.Routing.DomainStrategy = src.DomainStrategy
	}
	rules, err := RoutingA.Parse(expandRoutingAEnv(configure.GetRoutingA(), "parseRoutingA"))
	if err != nil {
		log.Warn("parseRoutingA: %v", err)
		return err
	}
	defaultOutbound, _ := t.FirstProxyOutboundName(nil)
	for _, rule := range rules {
		switch rule := rule.(type) {
		case RoutingA.Define:
			switch rule.Name {
			case "inbound", "outbound":
				switch o := rule.Value.(type) {
				case RoutingA.Bound:
					proto := o.Value
					switch proto.Name {
					case "http", "socks":
						if len(proto.NamedParams["address"]) < 1 ||
							len(proto.NamedParams["port"]) < 1 {
							continue
						}
						port, err := strconv.Atoi(proto.NamedParams["port"][0])
						if err != nil {
							continue
						}
						server := coreObj.Server{
							Port:    port,
							Address: proto.NamedParams["address"][0],
						}
						if unames := proto.NamedParams["user"]; len(unames) > 0 {
							passwords := proto.NamedParams["pass"]
							levels := proto.NamedParams["level"]
							for i, uname := range unames {
								u := coreObj.OutboundUser{
									User: uname,
								}
								if i < len(passwords) {
									u.Pass = passwords[i]
								}
								if i < len(levels) {
									level, err := strconv.Atoi(levels[i])
									if err == nil {
										u.Level = level
									}
								}
								server.Users = append(server.Users, u)
							}
						}
						switch rule.Name {
						case "outbound":
							t.Outbounds = append(t.Outbounds, coreObj.OutboundObject{
								Tag:      o.Name,
								Protocol: o.Value.Name,
								Settings: coreObj.Settings{
									Servers: []coreObj.Server{
										server,
									},
								},
							})
						case "inbound":
							// DEPRECATED: inbound definition in RoutingA is no longer supported.
							// Log a warning and skip creating the inbound.
							log.Warn("parseRoutingA: inbound definition '%s' is deprecated and will be ignored. "+
								"Use custom inbound settings with RoutingA rules instead.", o.Name)
						}
					case "freedom":
						settings := coreObj.Settings{}
						if len(proto.NamedParams["domainStrategy"]) > 0 {
							settings.DomainStrategy = proto.NamedParams["domainStrategy"][0]
						}
						if len(proto.NamedParams["redirect"]) > 0 {
							settings.Redirect = proto.NamedParams["redirect"][0]
						}
						if len(proto.NamedParams["userLevel"]) > 0 {
							level, err := strconv.Atoi(proto.NamedParams["userLevel"][0])
							if err == nil {
								settings.UserLevel = &level
							}
						}
						t.Outbounds = append(t.Outbounds, coreObj.OutboundObject{
							Tag:      o.Name,
							Protocol: o.Value.Name,
							Settings: settings,
						})
					}
				}
			}
		}
	}
	outboundTags := t.outNames()
	for _, rule := range rules {
		switch rule := rule.(type) {
		case RoutingA.Define:
			switch rule.Name {
			case "default":
				switch v := rule.Value.(type) {
				case string:
					defaultOutbound = v
					if _, ok := outboundTags[v]; !ok {
						return fmt.Errorf(`your RoutingA rules depend on the outbound "%v", thus you should select at least one server in this outbound`, v)
					}
				}
			}
		case RoutingA.Routing:
			if hasProcessCondition(rule) {
				// The app router applies these, not the core's router: the
				// core has no matcher for the executable, and a rule that kept
				// its other conditions would match more than it says.
				continue
			}
			rr := deepcopy.Copy(coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: rule.Out,
				InboundTag:  routingInboundTags,
			}).(coreObj.RoutingRule)
			for _, f := range rule.And {
				switch f.Name {
				case "domain", "domains":
					for k, vv := range f.NamedParams {
						for _, v := range vv {
							if k == "contains" {
								rr.Domain = append(rr.Domain, v)
								continue
							}
							if k == "ext" {
								datFilenameAndTag := strings.SplitN(v, ":", 2)
								if len(datFilenameAndTag) < 2 {
									return fmt.Errorf("RoutingA ext value %q is missing its tag; expected ext:\"file.dat:tag\"", v)
								}
								if !asset.DoesV2rayAssetExist(datFilenameAndTag[0]) {
									return fmt.Errorf("RoutingA ext file %q is not in the asset directory", datFilenameAndTag[0])
								}
							}
							rr.Domain = append(rr.Domain, fmt.Sprintf("%v:%v", k, v))
						}
					}
					// unnamed param is not recommended
					rr.Domain = append(rr.Domain, f.Params...)
				case "ip":
					for k, vv := range f.NamedParams {
						for _, v := range vv {
							if k == "ext" {
								datFilenameAndTag := strings.SplitN(v, ":", 2)
								if len(datFilenameAndTag) < 2 {
									return fmt.Errorf("RoutingA ext value %q is missing its tag; expected ext:\"file.dat:tag\"", v)
								}
								if !asset.DoesV2rayAssetExist(datFilenameAndTag[0]) {
									return fmt.Errorf("RoutingA ext file %q is not in the asset directory", datFilenameAndTag[0])
								}
							}
							rr.IP = append(rr.IP, fmt.Sprintf("%v:%v", k, v))
						}
					}
					rr.IP = append(rr.IP, f.Params...)
				case "network":
					rr.Network = strings.Join(f.Params, ",")
				case "port":
					rr.Port = strings.Join(f.Params, ",")
				case "sourcePort":
					rr.SourcePort = strings.Join(f.Params, ",")
				case "protocol":
					rr.Protocol = f.Params
				case "source":
					rr.Source = f.Params
				case "user":
					rr.User = f.Params
				case "inboundTag":
					rr.InboundTag = f.Params
				}
			}
			if rr.OutboundTag != "" {
				if _, ok := outboundTags[rr.OutboundTag]; !ok {
					return fmt.Errorf(`your RoutingA rules depend on the outbound "%v", thus you should select at least one server in this outbound`, rr.OutboundTag)
				}
			}
			t.Routing.Rules = append(t.Routing.Rules, rr)
		}
	}
	t.Routing.Rules = append(t.Routing.Rules, coreObj.RoutingRule{
		Type:        "field",
		OutboundTag: defaultOutbound,
		InboundTag:  []string{"rule-http", "rule-socks"},
	})
	return nil
}

// parseRoutingARules parses RoutingA rule text and returns routing rules
// filtered by the given inboundTag. This is used for custom inbound RoutingA rules.
// It does NOT create any inbounds or outbounds - only routing rules.
func parseRoutingARules(rulesText string, inboundTag string) (rules []coreObj.RoutingRule, err error) {
	lines := strings.Split(rulesText, "\n")
	hardcodeReplacement := regexp.MustCompile(`\$\$.+?\$\$`)
	for i := range lines {
		hardcodes := hardcodeReplacement.FindAllString(lines[i], -1)
		for _, hardcode := range hardcodes {
			env := strings.TrimSuffix(strings.TrimPrefix(hardcode, "$$"), "$$")
			val, ok := os.LookupEnv(env)
			if !ok {
				log.Error("parseRoutingARules: Environment variable \"%v\" is not found", env)
			} else {
				log.Info("parseRoutingARules: Environment variable %v=%v", env, strconv.Quote(val))
			}
			lines[i] = strings.Replace(lines[i], hardcode, val, 1)
		}
	}
	parsedRules, err := RoutingA.Parse(strings.Join(lines, "\n"))
	if err != nil {
		return nil, err
	}
	for _, rule := range parsedRules {
		switch rule := rule.(type) {
		case RoutingA.Routing:
			if hasProcessCondition(rule) {
				return nil, fmt.Errorf("the custom inbound %q selects on process(...), which it cannot enforce; application rules belong in the main rule set", inboundTag)
			}
			rr := deepcopy.Copy(coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: rule.Out,
				InboundTag:  []string{inboundTag},
			}).(coreObj.RoutingRule)
			for _, f := range rule.And {
				switch f.Name {
				case "domain", "domains":
					for k, vv := range f.NamedParams {
						for _, v := range vv {
							if k == "contains" {
								rr.Domain = append(rr.Domain, v)
								continue
							}
							if k == "ext" {
								datFilenameAndTag := strings.SplitN(v, ":", 2)
								if len(datFilenameAndTag) < 2 {
									return nil, fmt.Errorf("RoutingA ext value %q is missing its tag; expected ext:\"file.dat:tag\"", v)
								}
								if !asset.DoesV2rayAssetExist(datFilenameAndTag[0]) {
									return nil, fmt.Errorf("RoutingA ext file %q is not in the asset directory", datFilenameAndTag[0])
								}
							}
							rr.Domain = append(rr.Domain, fmt.Sprintf("%v:%v", k, v))
						}
					}
					rr.Domain = append(rr.Domain, f.Params...)
				case "ip":
					for k, vv := range f.NamedParams {
						for _, v := range vv {
							if k == "ext" {
								datFilenameAndTag := strings.SplitN(v, ":", 2)
								if len(datFilenameAndTag) < 2 {
									return nil, fmt.Errorf("RoutingA ext value %q is missing its tag; expected ext:\"file.dat:tag\"", v)
								}
								if !asset.DoesV2rayAssetExist(datFilenameAndTag[0]) {
									return nil, fmt.Errorf("RoutingA ext file %q is not in the asset directory", datFilenameAndTag[0])
								}
							}
							rr.IP = append(rr.IP, fmt.Sprintf("%v:%v", k, v))
						}
					}
					rr.IP = append(rr.IP, f.Params...)
				case "network":
					rr.Network = strings.Join(f.Params, ",")
				case "port":
					rr.Port = strings.Join(f.Params, ",")
				case "sourcePort":
					rr.SourcePort = strings.Join(f.Params, ",")
				case "protocol":
					rr.Protocol = f.Params
				case "source":
					rr.Source = f.Params
				case "user":
					rr.User = f.Params
				case "inboundTag":
					rr.InboundTag = f.Params
				}
			}
			if rr.OutboundTag != "" {
				rules = append(rules, rr)
			}
		}
	}
	return rules, nil
}

func (t *Template) setTransparentRouting() (err error) {
	// The app router decides per application and has already named the class
	// when the flow reaches an inbound, so the transparent policy has nothing
	// to add: a class goes to the outbound it stands for.
	if t.Setting.TransparentType == configure.TransparentAppRouter {
		plan, err := t.compileAppRouter()
		if err != nil {
			return err
		}
		t.appRouter = plan
		if len(plan.rules) == 0 {
			log.Warn("app router: no rule selects on process(...), so no traffic is routed; write one in the RoutingA editor")
			return nil
		}
		for _, class := range plan.order {
			tag := AppRouterClassTag(class)
			t.Inbounds = append(t.Inbounds, coreObj.Inbound{
				Port:     approuter.ClassPort(class),
				Protocol: "socks",
				Listen:   "127.0.0.1",
				Settings: &coreObj.InboundSettings{UDP: false},
				Tag:      tag,
			})
			t.Routing.Rules = append(t.Routing.Rules, coreObj.RoutingRule{
				Type:        "field",
				InboundTag:  []string{tag},
				OutboundTag: plan.classes[class],
			})
		}
		return nil
	}
	// With DNS interception off in tun mode the core no longer relays port
	// 53, so a query that still reaches the device must leave the host
	// directly instead of joining the transparent proxy chain, which would
	// send it to a proxy's port 53. The rule goes first so it wins over the
	// mode's own rules.
	if t.Setting.TransparentType == configure.TransparentTun && !dnsInterceptionEnabled(t.Setting) {
		t.Routing.Rules = append([]coreObj.RoutingRule{{
			Type:        "field",
			InboundTag:  []string{"transparent"},
			OutboundTag: "direct",
			Port:        "53",
		}}, t.Routing.Rules...)
	}
	defaultOutbound, _ := t.FirstProxyOutboundName(nil)
	switch t.Setting.Transparent {
	case configure.TransparentProxy:
		// Global transparent: route all transparent inbound to default outbound
		t.Routing.Rules = append(t.Routing.Rules, coreObj.RoutingRule{
			Type:        "field",
			InboundTag:  []string{"transparent"},
			OutboundTag: defaultOutbound,
		})
	case configure.TransparentAllowlist:
		return t.AppendRoutingRuleByMode(configure.AllowlistMode, []string{"transparent"})
	case configure.TransparentBlocklist:
		return t.AppendRoutingRuleByMode(configure.BlocklistMode, []string{"transparent"})
	case configure.TransparentFollowRule:
		// transparent mode is the same as rule
		for i := range t.Routing.Rules {
			ok := false
			for _, in := range t.Routing.Rules[i].InboundTag {
				if in == "rule-http" {
					ok = true
					break
				}
			}
			if ok {
				t.Routing.Rules[i].InboundTag = append(t.Routing.Rules[i].InboundTag, "transparent")
			}
		}
	}
	return nil
}
func (t *Template) AppendDokodemoTProxy(tproxy string, port int, tag string) {
	// Follow the "Allow connections from LAN" (PortSharing) toggle, the same way
	// the socks/http inbounds and dns-in already do. Redirected/forwarded traffic
	// (e.g. docker bridge containers, which the PREROUTING transparent rules
	// REDIRECT to the bridge interface address) can only reach the inbound when
	// it listens on 0.0.0.0; since v2.4 the loopback-only default broke those
	// flows entirely.
	listenAddr := "127.0.0.1"
	if t.Setting != nil && t.Setting.PortSharing {
		listenAddr = "0.0.0.0"
	}
	dokodemo := coreObj.Inbound{
		Listen:   listenAddr,
		Port:     port,
		Protocol: "dokodemo-door",
		Sniffing: coreObj.Sniffing{
			Enabled:      true,
			DestOverride: []string{"http", "tls"},
		},
		Settings: &coreObj.InboundSettings{Network: "tcp,udp"},
		Tag:      tag,
	}
	dokodemo.StreamSettings = &coreObj.StreamSettings{Sockopt: &coreObj.Sockopt{Tproxy: &tproxy}}
	dokodemo.Settings.FollowRedirect = true
	t.Inbounds = append(t.Inbounds, dokodemo)
}

func (t *Template) updatePrivateRouting() {
	privateAddrs, _ := iptables.GetLocalCIDR()
	if len(privateAddrs) == 0 {
		return
	}
	for i := range t.Routing.Rules {
		for j := range t.Routing.Rules[i].IP {
			if t.Routing.Rules[i].IP[j] == "geoip:private" {
				t.Routing.Rules[i].IP = append(t.Routing.Rules[i].IP, privateAddrs...)
				break
			}
		}
	}
}

func (t *Template) optimizeGeoipMemoryOccupation() {
	if asset.DoesV2rayAssetExist("geoip-only-cn-private.dat") {
		for i := range t.Routing.Rules {
			for j := range t.Routing.Rules[i].IP {
				switch t.Routing.Rules[i].IP[j] {
				case "geoip:private", "geoip:cn":
					t.Routing.Rules[i].IP[j] = "ext:geoip-only-cn-private.dat:" + strings.TrimPrefix(t.Routing.Rules[i].IP[j], "geoip:")
				}
			}
		}
	}
}

func (t *Template) setWhitelistRouting(whitelist []Addr) {
	var rules []coreObj.RoutingRule
	for _, addr := range whitelist {
		if net.ParseIP(addr.host) != nil {
			rules = append(rules, coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "direct",
				IP:          []string{addr.host},
				Port:        addr.port,
			})
		} else {
			rules = append(rules, coreObj.RoutingRule{
				Type:        "field",
				OutboundTag: "direct",
				Domain:      []string{addr.host},
				Port:        addr.port,
			})
		}
	}
	if len(rules) > 0 {
		t.Routing.Rules = append(rules, t.Routing.Rules...)
	}
}

func (t *Template) setGroupRouting() {
	outbounds := t.outNames()
	for i := range t.Routing.Rules {
		if t.Routing.Rules[i].OutboundTag != "" && outbounds[t.Routing.Rules[i].OutboundTag] == true {
			t.Routing.Rules[i].BalancerTag, t.Routing.Rules[i].OutboundTag = t.Routing.Rules[i].OutboundTag, ""
		}
	}
}

func (t *Template) setVmessInboundRouting() {
	if configure.GetPortsNotNil().Vmess <= 0 {
		return
	}
	for i := range t.Routing.Rules {
		var bHasRule bool
		for _, tag := range t.Routing.Rules[i].InboundTag {
			if tag == "rule-http" {
				bHasRule = true
			}
		}
		if bHasRule {
			t.Routing.Rules[i].InboundTag = append(t.Routing.Rules[i].InboundTag, "vmess")
		}
	}
}
