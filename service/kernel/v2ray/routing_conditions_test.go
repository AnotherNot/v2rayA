package v2ray

import (
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
)

func routingTemplateWithBuiltins(t *testing.T, text string) *Template {
	t.Helper()
	withRoutingAText(t, text)
	tmpl := baseTemplate(t, configure.NewSetting())
	tmpl.Outbounds = []coreObj.OutboundObject{
		{Tag: "proxy", Protocol: "vmess"},
		{Tag: "direct", Protocol: "freedom"},
		{Tag: "block", Protocol: "blackhole"},
	}
	return tmpl
}

// extern(ip, geoip, private) is the older spelling of ip(geoip: private). The
// parser accepts both, so a translator that knows only one drops the condition
// and the rule then matches more than its author wrote.
func TestExternBecomesTheConditionTheCoreKnows(t *testing.T) {
	tmpl := routingTemplateWithBuiltins(t, "extern(ip, geoip, private)->direct\nextern(domain, geosite, category-ads)->block")
	if err := parseRoutingA(tmpl, []string{"rule-http", "rule-socks"}); err != nil {
		t.Fatal(err)
	}
	var ipRule, domainRule *coreObj.RoutingRule
	for i := range tmpl.Routing.Rules {
		switch tmpl.Routing.Rules[i].OutboundTag {
		case "direct":
			ipRule = &tmpl.Routing.Rules[i]
		case "block":
			domainRule = &tmpl.Routing.Rules[i]
		}
	}
	if ipRule == nil || len(ipRule.IP) != 1 || ipRule.IP[0] != "geoip:private" {
		t.Fatalf("the ip extern became %+v", ipRule)
	}
	if domainRule == nil || len(domainRule.Domain) != 1 || domainRule.Domain[0] != "geosite:category-ads" {
		t.Fatalf("the domain extern became %+v", domainRule)
	}
}

// A file of its own is named the way the core reads it, and has to exist.
func TestExternRefusesAFileTheCoreCannotFind(t *testing.T) {
	tmpl := routingTemplateWithBuiltins(t, "extern(domain, missing.dat, tag)->direct")
	if err := parseRoutingA(tmpl, []string{"rule-http", "rule-socks"}); err == nil {
		t.Fatal("an extern file that is not in the asset directory was accepted")
	}
}

func TestExternRefusesANonsenseKind(t *testing.T) {
	tmpl := routingTemplateWithBuiltins(t, "extern(port, geoip, private)->direct")
	if err := parseRoutingA(tmpl, []string{"rule-http", "rule-socks"}); err == nil {
		t.Fatal("extern(port, ...) was accepted")
	}
}

// The custom-inbound translator shares the conditions, so it must handle
// extern the same way rather than dropping it.
func TestCustomInboundRulesUnderstandExtern(t *testing.T) {
	rules, err := parseRoutingARules("extern(ip, geoip, private)->direct", "app-in")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || len(rules[0].IP) != 1 || rules[0].IP[0] != "geoip:private" {
		t.Fatalf("%+v", rules)
	}
}