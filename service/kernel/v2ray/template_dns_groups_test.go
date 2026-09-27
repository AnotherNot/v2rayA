package v2ray

import (
	"encoding/json"
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
)

// The generator must name the balancer groups: a group tag has no outbound
// handler, so the DNS module sends those upstreams through the local SOCKS
// inbound instead of forcing them on the dispatcher.
func TestDnsModuleConfigNamesGroups(t *testing.T) {
	tmpl := baseTemplate(t, configure.NewSetting())
	tmpl.Outbounds = []coreObj.OutboundObject{
		{Tag: "proxy", Protocol: "vless"},
		{Tag: "direct", Protocol: "freedom"},
		{Tag: "member", Protocol: "vless", Balancers: []string{"regional"}},
	}
	if err := tmpl.generateDnsModuleConfig(nil); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		GroupTags []string `json:"group_tags"`
	}
	if err := json.Unmarshal(tmpl.DnsModuleConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.GroupTags) != 1 || cfg.GroupTags[0] != "regional" {
		t.Fatalf("group_tags = %v, want [regional]", cfg.GroupTags)
	}
}

// Without groups the field stays empty: every tag forces cleanly.
func TestDnsModuleConfigWithoutGroups(t *testing.T) {
	tmpl := baseTemplate(t, configure.NewSetting())
	tmpl.Outbounds = []coreObj.OutboundObject{
		{Tag: "proxy", Protocol: "vless"},
		{Tag: "direct", Protocol: "freedom"},
	}
	if err := tmpl.generateDnsModuleConfig(nil); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		GroupTags []string `json:"group_tags"`
	}
	if err := json.Unmarshal(tmpl.DnsModuleConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.GroupTags) != 0 {
		t.Fatalf("group_tags = %v, want empty", cfg.GroupTags)
	}
}
