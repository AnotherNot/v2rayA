package approuter

import (
	"fmt"
	"regexp"
	"strings"
)

// A rule can name a domain as well as an address, and the kernel sees only the
// address. The name comes from what the DNS module answered (see names.go);
// matching it happens here, in Go, because the patterns include regular
// expressions, and a kernel that could evaluate only a subset would silently
// stop applying the rest.

// domainKind is how a pattern compares a name.
type domainKind uint8

const (
	domainSuffix domainKind = iota
	domainFull
	domainKeyword
	domainRegexp
)

// DomainPattern is one value of a domain(...) condition. Its fields are not
// exported: the only way to make one is CompileDomainPattern, so a pattern
// cannot reach the matcher with an uncompiled regular expression.
type DomainPattern struct {
	kind domainKind
	raw  string
	val  string
	re   *regexp.Regexp
}

// CompileDomainPattern reads one value the way the editor writes it. A bare
// value and "suffix:" are suffixes, "domain:" is the older spelling of the
// same; "full:" is the whole name, "keyword:" and "contains:" are substrings,
// "regexp:" is a regular expression. A prefix the matcher does not know is an
// error: read as a suffix it would widen the rule.
func CompileDomainPattern(value string) (DomainPattern, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DomainPattern{}, fmt.Errorf("a domain(...) condition has an empty value")
	}
	kind, val := domainSuffix, value
	if head, tail, ok := strings.Cut(value, ":"); ok {
		switch strings.ToLower(strings.TrimSpace(head)) {
		case "suffix", "domain":
			kind, val = domainSuffix, tail
		case "full":
			kind, val = domainFull, tail
		case "keyword", "contains":
			kind, val = domainKeyword, tail
		case "regexp":
			kind, val = domainRegexp, tail
		case "geosite", "ext":
			return DomainPattern{}, fmt.Errorf("domain(%s) is a database reference, and application rules read literal names for now", value)
		default:
			return DomainPattern{}, fmt.Errorf("domain(%s) names %q, and application rules read suffix:, full:, keyword:, contains: and regexp:", value, head)
		}
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return DomainPattern{}, fmt.Errorf("domain(%s) has an empty value", value)
	}
	p := DomainPattern{kind: kind, raw: value}
	if kind == domainRegexp {
		re, err := regexp.Compile(val)
		if err != nil {
			return DomainPattern{}, fmt.Errorf("domain(%s): %w", value, err)
		}
		p.re = re
		return p, nil
	}
	p.val = strings.ToLower(strings.TrimSuffix(val, "."))
	return p, nil
}

// matches reports whether the pattern covers a name a DNS answer carried. A
// trailing dot belongs to the wire format, not to the name.
func (p DomainPattern) matches(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	switch p.kind {
	case domainFull:
		return name == p.val
	case domainSuffix:
		// "example.com" covers "example.com" and "a.example.com", but not
		// "notexample.com": the comparison is on a label boundary.
		return name == p.val || strings.HasSuffix(name, "."+p.val)
	case domainKeyword:
		return strings.Contains(name, p.val)
	default:
		return p.re.MatchString(name)
	}
}

// matchDomains reports whether any name the destination is known by matches any
// of the rule's patterns. A rule with no patterns has nothing to check; a rule
// with patterns and no known name does not match. A name condition is a
// requirement, not a hint: a rule that has not seen its name must not route
// traffic anyway.
func matchDomains(patterns []DomainPattern, names []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		for _, name := range names {
			if p.matches(name) {
				return true
			}
		}
	}
	return false
}
