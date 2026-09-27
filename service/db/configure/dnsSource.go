package configure

import "github.com/v2rayA/v2rayA/db"

// DnsSource is the source the DNS rules are imported from.
//
// A DNS source publishes rules in the shape this project stores them, one
// upstream and one outbound each. That is deliberately not the raw core DNS
// object other clients exchange: there, the per-domain mapping lives inside a
// configuration fragment this project never sees, so importing one would mean
// guessing which upstream answers which name.
type DnsSource struct {
	URL     string `json:"url"`
	Version string `json:"version"`
	Remarks string `json:"remarks"`
}

func SetDnsSource(s *DnsSource) error {
	return db.Set("system", "dnsSource", s)
}

func GetDnsSource(s *DnsSource) {
	_ = db.Get("system", "dnsSource", s)
}
