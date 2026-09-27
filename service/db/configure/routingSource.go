package configure

import "github.com/v2rayA/v2rayA/db"

// RoutingSource is the rule source RoutingA's rules are imported from.
//
// URL points at an index of named rule sets in the v2rayN format. Version is
// that index's version, kept so an unchanged source is not re-imported.
// DomainStrategy is carried for display only: the core already defaults to
// IPOnDemand, which is what the published sets ask for.
type RoutingSource struct {
	URL            string `json:"url"`
	Version        string `json:"version"`
	Remarks        string `json:"remarks"`
	DomainStrategy string `json:"domainStrategy"`
}

func SetRoutingSource(r *RoutingSource) (err error) {
	return db.Set("system", "routingSource", r)
}

func GetRoutingSource(r *RoutingSource) {
	_ = db.Get("system", "routingSource", r)
}
