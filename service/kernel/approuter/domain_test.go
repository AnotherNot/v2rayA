package approuter

import (
	"testing"
)

// The patterns are matched the way RoutingA writes them, and a suffix is not a
// bare string suffix: "example.com" must not catch "notexample.com".
func TestDomainPatternsMatchWhatTheEditorMeans(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		name    string
		want    bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "a.example.com", true},
		{"example.com", "notexample.com", false},
		{"example.com", "example.com.evil", false},
		{"Example.COM", "A.example.com", true},
		{"example.com.", "example.com", true},
		{"suffix:example.com", "a.example.com", true},
		{"domain:example.com", "a.example.com", true},
		{"full:example.com", "example.com", true},
		{"full:example.com", "a.example.com", false},
		{"keyword:cdn", "my-cdn-edge.test", true},
		{"contains:cdn", "my-cdn-edge.test", true},
		{`regexp:^ads\.`, "ads.example.com", true},
		{`regexp:^ads\.`, "no-ads.example.com", false},
	} {
		p, err := CompileDomainPattern(tc.pattern)
		if err != nil {
			t.Errorf("%s: %v", tc.pattern, err)
			continue
		}
		if got := p.matches(tc.name); got != tc.want {
			t.Errorf("%s matches %s: %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// What the matcher cannot enforce is refused rather than read as something
// else: a bare "geosite:cn" or a typo in a prefix would otherwise widen or
// quietly stop applying the rule.
func TestCompileDomainPatternRefusesWhatItCannotEnforce(t *testing.T) {
	for _, value := range []string{
		"",
		"geosite:cn",
		"ext:ads.dat:tag",
		"nope:example.com",
		"regexp:[",
		"full:",
	} {
		if _, err := CompileDomainPattern(value); err == nil {
			t.Errorf("%q was accepted", value)
		}
	}
}

// A domain condition is a requirement: with no name known the rule does not
// match, and a rule that names no domain is not blocked by the missing name.
func TestMatchDomainsNeedsAKnownName(t *testing.T) {
	patterns := mustDomains(t, "example.com")
	if matchDomains(patterns, nil) {
		t.Fatal("a rule matched without any known name")
	}
	if matchDomains(patterns, []string{"other.test"}) {
		t.Fatal("a rule matched a name it does not name")
	}
	if !matchDomains(nil, nil) {
		t.Fatal("a rule with no domain condition was blocked by the missing name")
	}
	if !matchDomains(nil, []string{"anything"}) {
		t.Fatal("a rule with no domain condition did not match")
	}
}
