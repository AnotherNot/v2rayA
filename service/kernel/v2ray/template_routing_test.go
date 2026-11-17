package v2ray

import (
	"strings"
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
)

// process(...) names the executable, which the core's router cannot match. A
// translator that ignores the condition leaves the rule matching on whatever
// remains — every process when process(...) was the only one.
func TestValidateRoutingARejectsAProcessCondition(t *testing.T) {
	err := ValidateRoutingA("process(firefox) && port(443)->proxy")
	if err == nil {
		t.Fatal("a process(...) condition was accepted")
	}
	if !strings.Contains(err.Error(), "process(...)") {
		t.Fatalf("the error does not name the condition: %v", err)
	}
}

func TestValidateRoutingAAcceptsTheConditionsTheRouterHas(t *testing.T) {
	text := `domain(domain: example.com) && ip(1.1.1.1) && port(443) && network(tcp)->proxy`
	if err := ValidateRoutingA(text); err != nil {
		t.Fatalf("a rule without process(...) was refused: %v", err)
	}
}

// Stored text reaches the core through config generation, and generation is
// the only check when the core is stopped — so it must refuse the condition
// itself, not rely on the endpoint having done so.
func TestParseRoutingARejectsAProcessCondition(t *testing.T) {
	previous := configure.GetRoutingA()
	t.Cleanup(func() { _ = configure.SetRoutingA(&previous) })
	text := "default: proxy\nprocess(firefox)->direct"
	if err := configure.SetRoutingA(&text); err != nil {
		t.Fatal(err)
	}
	tmpl := baseTemplate(t, configure.NewSetting())
	if err := parseRoutingA(tmpl, []string{"rule-http", "rule-socks"}); err == nil {
		t.Fatal("parseRoutingA dropped a process(...) condition")
	}
	if len(tmpl.Routing.Rules) != 0 {
		t.Fatalf("a rule was generated from the refused text: %+v", tmpl.Routing.Rules)
	}
}

// The custom-inbound editor has its own translator; the refusal has to reach
// it too.
func TestParseRoutingARulesRejectsAProcessCondition(t *testing.T) {
	rules, err := parseRoutingARules("process(firefox)->direct", "app-in")
	if err == nil {
		t.Fatalf("parseRoutingARules dropped a process(...) condition: %+v", rules)
	}
}