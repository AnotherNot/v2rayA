package v2ray

import (
	"fmt"
	"strings"

	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
	"github.com/v2rayA/v2rayA/kernel/v2ray/asset"
)

// applyConditions writes a rule's destination conditions onto the core's rule
// shape. One copy serves the main rule set and a custom inbound's own rules:
// when these ran twice, a condition handled in one place and forgotten in the
// other silently widened the rule that lost it.
func applyConditions(rr *coreObj.RoutingRule, rule RoutingA.Routing) error {
	for _, f := range rule.And {
		switch f.Name {
		case "domain", "domains":
			if err := appendDomains(&rr.Domain, f); err != nil {
				return err
			}
		case "ip":
			if err := appendIPs(&rr.IP, f); err != nil {
				return err
			}
		case "extern":
			if err := appendExtern(rr, f); err != nil {
				return err
			}
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
	return nil
}

// appendDomains reads domain(...) into the core's domain list.
func appendDomains(domain *[]string, f RoutingA.Function) error {
	for k, vv := range f.NamedParams {
		for _, v := range vv {
			if k == "contains" {
				*domain = append(*domain, v)
				continue
			}
			if k == "ext" {
				if err := checkExtValue(v); err != nil {
					return err
				}
			}
			*domain = append(*domain, fmt.Sprintf("%v:%v", k, v))
		}
	}
	// unnamed param is not recommended
	*domain = append(*domain, f.Params...)
	return nil
}

// appendIPs reads ip(...) into the core's address list.
func appendIPs(ip *[]string, f RoutingA.Function) error {
	for k, vv := range f.NamedParams {
		for _, v := range vv {
			if k == "ext" {
				if err := checkExtValue(v); err != nil {
					return err
				}
			}
			*ip = append(*ip, fmt.Sprintf("%v:%v", k, v))
		}
	}
	*ip = append(*ip, f.Params...)
	return nil
}

// appendExtern reads extern(kind, file, tag), the older spelling of
// ip(geoip: private) and domain(geosite: category-ads). The parser accepts it,
// so leaving it unhandled dropped the condition and widened the rule.
func appendExtern(rr *coreObj.RoutingRule, f RoutingA.Function) error {
	if len(f.Params) != 3 {
		return fmt.Errorf("extern(...) takes a kind, a file and a tag, as in extern(ip, geoip, private); got %d value(s)", len(f.Params))
	}
	kind, file, tag := f.Params[0], f.Params[1], f.Params[2]
	var value string
	switch file {
	case "geoip", "geosite":
		value = file + ":" + tag
	default:
		// A file of its own: the core reads it as ext:"name:tag", and the
		// name has to be one the asset directory holds.
		if !asset.DoesV2rayAssetExist(file) {
			return fmt.Errorf("RoutingA extern file %q is not in the asset directory", file)
		}
		value = fmt.Sprintf("ext:%q", file+":"+tag)
	}
	switch kind {
	case "ip":
		rr.IP = append(rr.IP, value)
	case "domain":
		rr.Domain = append(rr.Domain, value)
	default:
		return fmt.Errorf("extern(%s, ...) is not an ip or a domain rule", kind)
	}
	return nil
}

// checkExtValue refuses an ext:"file.dat:tag" whose file is not in the asset
// directory: the core would fail to start, after the text was stored.
func checkExtValue(v string) error {
	datFilenameAndTag := strings.SplitN(v, ":", 2)
	if len(datFilenameAndTag) < 2 {
		return fmt.Errorf("RoutingA ext value %q is missing its tag; expected ext:\"file.dat:tag\"", v)
	}
	if !asset.DoesV2rayAssetExist(datFilenameAndTag[0]) {
		return fmt.Errorf("RoutingA ext file %q is not in the asset directory", datFilenameAndTag[0])
	}
	return nil
}