package dns

import (
	"fmt"
	"strings"

	"github.com/miekg/dns"
)

// FilterBailiwick removes records that are not part of the answer to the
// question. A recursive upstream can append an A record for a name the client
// never asked about; storing it in the cache would hand that record to the
// next client. The filter keeps the question's name, every CNAME target that
// is reachable from it, authority records for the question's ancestors, and
// the additional addresses those records name. It returns the number of
// records it dropped.
func FilterBailiwick(msg *dns.Msg) int {
	if msg == nil || len(msg.Question) == 0 {
		return 0
	}
	qname := canonicalName(msg.Question[0].Name)
	allowed := map[string]struct{}{qname: {}}

	// A CNAME chain can arrive out of order; close over it before deciding
	// which records belong to the answer.
	for {
		added := false
		for _, rr := range msg.Answer {
			cname, ok := rr.(*dns.CNAME)
			if !ok {
				continue
			}
			if _, known := allowed[canonicalName(cname.Hdr.Name)]; !known {
				continue
			}
			target := canonicalName(cname.Target)
			if _, known := allowed[target]; !known {
				allowed[target] = struct{}{}
				added = true
			}
		}
		if !added {
			break
		}
	}

	removed := 0
	answer := msg.Answer[:0]
	referenced := make(map[string]struct{})
	for _, rr := range msg.Answer {
		if _, ok := allowed[canonicalName(rr.Header().Name)]; !ok {
			removed++
			continue
		}
		answer = append(answer, rr)
		switch typed := rr.(type) {
		case *dns.MX:
			referenced[canonicalName(typed.Mx)] = struct{}{}
		case *dns.SRV:
			referenced[canonicalName(typed.Target)] = struct{}{}
		case *dns.NAPTR:
			referenced[canonicalName(typed.Replacement)] = struct{}{}
		}
	}
	msg.Answer = answer

	authority := msg.Ns[:0]
	for _, rr := range msg.Ns {
		name := canonicalName(rr.Header().Name)
		// A zone apex is an ancestor of the question, so a SOA or NS there is
		// a legitimate authority record. An unrelated zone's record is not.
		if !dns.IsSubDomain(name, qname) {
			removed++
			continue
		}
		authority = append(authority, rr)
		if ns, ok := rr.(*dns.NS); ok {
			referenced[canonicalName(ns.Ns)] = struct{}{}
		}
	}
	msg.Ns = authority

	extra := msg.Extra[:0]
	for _, rr := range msg.Extra {
		if rr.Header().Rrtype == dns.TypeOPT {
			extra = append(extra, rr)
			continue
		}
		name := canonicalName(rr.Header().Name)
		_, inAnswer := allowed[name]
		_, isReferenced := referenced[name]
		// Glue is useful only for the names the kept answer and authority
		// records point at, and only as an address.
		if isReferenced && (rr.Header().Rrtype == dns.TypeA || rr.Header().Rrtype == dns.TypeAAAA) {
			extra = append(extra, rr)
			continue
		}
		if inAnswer {
			extra = append(extra, rr)
			continue
		}
		removed++
	}
	msg.Extra = extra

	return removed
}

func canonicalName(name string) string {
	return strings.ToLower(dns.Fqdn(name))
}

// ValidateResponse validates a DNS response message for integrity and consistency.
// It checks:
//   - Message is not nil
//   - Question section is present and valid
//   - Response code (RCODE) is a valid value
//   - Message is marked as a response (QR bit)
//   - Question matches the query (checked externally via ID matching by miekg/dns)
//
// Returns nil if the response is valid, or an error describing the issue.
func ValidateResponse(msg *dns.Msg) error {
	if msg == nil {
		return fmt.Errorf("dns validator: nil response message")
	}

	// Verify the message is a response (QR bit = 1).
	if !msg.Response {
		return fmt.Errorf("dns validator: message is not a response (QR bit not set)")
	}

	// Verify question section is present.
	if len(msg.Question) == 0 {
		return fmt.Errorf("dns validator: response has no question section")
	}

	// Verify question is well-formed.
	q := msg.Question[0]
	if q.Name == "" {
		return fmt.Errorf("dns validator: empty question name")
	}
	if q.Qtype == 0 {
		return fmt.Errorf("dns validator: zero query type in question")
	}
	if q.Qclass == 0 {
		return fmt.Errorf("dns validator: zero query class in question")
	}

	// Validate response code (RCODE).
	if err := validateRcode(msg.Rcode); err != nil {
		return fmt.Errorf("dns validator: %w", err)
	}

	// Validate answer section consistency.
	for i, rr := range msg.Answer {
		if rr == nil {
			return fmt.Errorf("dns validator: nil answer record at index %d", i)
		}
		if rr.Header().Rrtype == 0 {
			return fmt.Errorf("dns validator: answer record %d has zero type", i)
		}
		// Validate record name is non-empty and fully qualified.
		if rr.Header().Name == "" {
			return fmt.Errorf("dns validator: answer record %d has empty name", i)
		}
	}

	// Validate authority section.
	for i, rr := range msg.Ns {
		if rr == nil {
			return fmt.Errorf("dns validator: nil authority record at index %d", i)
		}
		if rr.Header().Rrtype == 0 {
			return fmt.Errorf("dns validator: authority record %d has zero type", i)
		}
	}

	// Validate additional section (excluding OPT pseudo-records which are metadata).
	for i, rr := range msg.Extra {
		if rr == nil {
			return fmt.Errorf("dns validator: nil additional record at index %d", i)
		}
		if rr.Header().Rrtype == dns.TypeOPT {
			// OPT records are valid; skip detailed validation.
			continue
		}
		if rr.Header().Rrtype == 0 {
			return fmt.Errorf("dns validator: additional record %d has zero type", i)
		}
	}

	return nil
}

// validateRcode checks if the DNS response code (RCODE) is valid.
// Standard RCODEs are 0-15; extended RCODEs may be higher but should be
// checked for known values.
func validateRcode(rcode int) error {
	switch rcode {
	case dns.RcodeSuccess: // 0
	case dns.RcodeFormatError: // 1
	case dns.RcodeServerFailure: // 2
	case dns.RcodeNameError: // 3
	case dns.RcodeNotImplemented: // 4
	case dns.RcodeRefused: // 5
	case dns.RcodeYXDomain: // 6
	case dns.RcodeYXRrset: // 7
	case dns.RcodeNXRrset: // 8
	case dns.RcodeNotAuth: // 9
	case dns.RcodeNotZone: // 10
	case dns.RcodeBadSig: // 16 (also RcodeBadVers)
	case dns.RcodeBadKey: // 17
	case dns.RcodeBadTime: // 18
	case dns.RcodeBadMode: // 19
	case dns.RcodeBadName: // 20
	case dns.RcodeBadAlg: // 21
	case dns.RcodeBadTrunc: // 22
	case dns.RcodeBadCookie: // 23
	default:
		// Allow extended RCODEs (higher than 23) as they may be valid
		// in specific contexts, but warn about unexpected values.
		if rcode < 0 || rcode > 0xFFF {
			return fmt.Errorf("invalid rcode %d: out of valid range (0-4095)", rcode)
		}
	}
	return nil
}

// ValidateAnswerCount checks that the response contains at least one
// answer record when the RCODE is success.
func ValidateAnswerCount(msg *dns.Msg) error {
	if msg == nil {
		return fmt.Errorf("dns validator: nil message")
	}
	if msg.Rcode == dns.RcodeSuccess && len(msg.Answer) == 0 {
		return fmt.Errorf("dns validator: success response with zero answers")
	}
	return nil
}

// ValidateQuestionMatch verifies that the response question matches the
// expected query name and type. This is an additional safety check beyond
// the ID matching that miekg/dns performs internally.
func ValidateQuestionMatch(msg *dns.Msg, expectedName string, expectedQType QueryType) error {
	if msg == nil {
		return fmt.Errorf("dns validator: nil message")
	}
	if len(msg.Question) == 0 {
		return fmt.Errorf("dns validator: no question in response")
	}

	q := msg.Question[0]
	if q.Name != dns.Fqdn(expectedName) {
		return fmt.Errorf("dns validator: question name mismatch: got %q, expected %q",
			q.Name, dns.Fqdn(expectedName))
	}
	if q.Qtype != uint16(expectedQType) {
		return fmt.Errorf("dns validator: question type mismatch: got %d, expected %d",
			q.Qtype, uint16(expectedQType))
	}
	return nil
}
