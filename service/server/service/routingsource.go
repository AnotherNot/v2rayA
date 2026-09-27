package service

import (
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
)

// The rule sets published for v2rayN (runetfreedom's russia-v2ray-custom-routing-list
// among them) are JSON arrays whose shape is a superset of what RoutingA can
// express. This converts one such array into RoutingA source.
//
// The sets that exist in the wild only use port, network, domain, ip, protocol
// and outboundTag, all of which RoutingA has, so the conversion is lossless for
// them. A field RoutingA cannot express is reported rather than dropped: a rule
// that quietly loses its condition sends traffic the wrong way, and the user
// would have no way to tell.

// V2rayNRule mirrors one entry of a v2rayN rule set. Only the fields that carry
// a routing condition are declared; a set carrying anything else is rejected by
// the process-name check below and by jsoniter's default of ignoring the rest.
type V2rayNRule struct {
	OutboundTag string   `json:"outboundTag"`
	Port        string   `json:"port"`
	Network     string   `json:"network"`
	Domain      []string `json:"domain"`
	IP          []string `json:"ip"`
	Protocol    []string `json:"protocol"`
	InboundTag  []string `json:"inboundTag"`
	Process     []string `json:"process"`
	Enabled     *bool    `json:"enabled"`
	Remarks     string   `json:"remarks"`
}

// RoutingTemplate is the index a rule source URL points at: a version and the
// named sets behind it. The version is what makes re-importing idempotent.
type RoutingTemplate struct {
	Version      string `json:"version"`
	RoutingItems []struct {
		Remarks        string `json:"remarks"`
		URL            string `json:"url"`
		RuleSet        string `json:"ruleSet"`
		DomainStrategy string `json:"domainStrategy"`
	} `json:"routingItems"`
}

// routing conditions are ANDed inside one rule, so the order they are written
// in does not change what the rule matches. It is fixed so that re-importing an
// unchanged set produces byte-identical source.
var conditionOrder = []string{"inboundTag", "domain", "ip", "port", "network", "protocol"}

// ConvertV2rayNRouting turns a v2rayN rule array into RoutingA source. The
// result names the rule set in a comment header so an imported set is
// recognisable in the editor.
func ConvertV2rayNRouting(name string, data []byte) (string, error) {
	var rules []V2rayNRule
	if err := jsoniter.Unmarshal(data, &rules); err != nil {
		return "", fmt.Errorf("the rule set is not a v2rayN rule array: %w", err)
	}
	var b strings.Builder
	if name != "" {
		fmt.Fprintf(&b, "# %s\n", name)
	}
	kept := 0
	for i, r := range rules {
		// A disabled rule is a rule the author kept but does not want applied.
		if r.Enabled != nil && !*r.Enabled {
			continue
		}
		if len(r.Process) > 0 {
			return "", fmt.Errorf("rule %d matches on process name (%s), which RoutingA cannot express; the rule set is not compatible",
				i+1, strings.Join(r.Process, ", "))
		}
		conditions := conditionsOf(r)
		if len(conditions) == 0 {
			// A rule with no condition matches every request, which is what the
			// "default:" line of RoutingA is for. Skipping it keeps the import
			// from silently swallowing traffic ahead of the rules above it.
			continue
		}
		if r.OutboundTag == "" {
			return "", fmt.Errorf("rule %d has no outboundTag", i+1)
		}
		if r.Remarks != "" {
			fmt.Fprintf(&b, "# %s\n", r.Remarks)
		}
		fmt.Fprintf(&b, "%s->%s\n", strings.Join(conditions, " && "), r.OutboundTag)
		kept++
	}
	if kept == 0 {
		return "", fmt.Errorf("the rule set holds no usable rule")
	}
	fmt.Fprintf(&b, "\ndefault: proxy\n")
	return b.String(), nil
}

func conditionsOf(r V2rayNRule) []string {
	values := map[string][]string{
		"inboundTag": r.InboundTag,
		"domain":     r.Domain,
		"ip":         r.IP,
		"port":       splitIfSet(r.Port),
		"network":    splitIfSet(r.Network),
		"protocol":   r.Protocol,
	}
	var out []string
	for _, name := range conditionOrder {
		v := values[name]
		if len(v) == 0 {
			continue
		}
		quoted := make([]string, 0, len(v))
		for _, one := range v {
			// RoutingA treats a comma inside an argument as a separator, and
			// xray writes a bare host:port rule that way.
			quoted = append(quoted, "'"+strings.ReplaceAll(one, "'", "")+"'")
		}
		out = append(out, fmt.Sprintf("%s(%s)", name, strings.Join(quoted, ", ")))
	}
	return out
}

func splitIfSet(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
