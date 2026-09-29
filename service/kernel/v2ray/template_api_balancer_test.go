package v2ray

import (
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/coreObj"
)

// A random group must reach the core as a balancer with the random strategy
// and no observatory: the core picks a member per connection and never
// probes, so an observer would only burn probe traffic for nothing.
func TestRandomGroupGeneratesBalancerWithoutObserver(t *testing.T) {
	tmpl := baseTemplate(t, configure.NewSetting())
	tmpl.Outbounds = []coreObj.OutboundObject{
		{Tag: "proxy", Protocol: "vless"},
		{Tag: "direct", Protocol: "freedom"},
		{Tag: "member", Protocol: "vless", Balancers: []string{"regional"}},
	}
	data := &ServerData{
		OutboundName2Setting: map[string]configure.OutboundSetting{
			"regional": {
				ProbeURL:      "",
				ProbeInterval: "10s",
				Type:          configure.Random,
			},
		},
	}
	if _, err := tmpl.SetAPI(data); err != nil {
		t.Fatal(err)
	}
	var balancer *coreObj.Balancer
	for i := range tmpl.Routing.Balancers {
		if tmpl.Routing.Balancers[i].Tag == "regional" {
			balancer = &tmpl.Routing.Balancers[i]
		}
	}
	if balancer == nil {
		t.Fatal("no balancer for the random group")
	}
	if balancer.Strategy.Type != "random" {
		t.Fatalf("balancer strategy = %q, want random", balancer.Strategy.Type)
	}
	if tmpl.MultiObservatory != nil {
		for _, o := range tmpl.MultiObservatory.Observers {
			if o.Tag == "regional" {
				t.Fatal("the random group got an observatory observer")
			}
		}
	}
}
