package v2ray

import (
	"testing"
)

// The custom-inbound editor has its own translator; an executable condition
// there has nowhere to be applied, so it is refused rather than dropped.
func TestParseRoutingARulesRejectsAProcessCondition(t *testing.T) {
	rules, err := parseRoutingARules("process(firefox)->direct", "app-in")
	if err == nil {
		t.Fatalf("parseRoutingARules dropped a process(...) condition: %+v", rules)
	}
}
