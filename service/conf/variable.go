package conf

import (
	"time"
)

var (
	Version                  = "debug"
	FoundNew                 = false
	RemoteVersion            = ""
	TickerUpdateGFWList      *time.Ticker
	TickerUpdateSubscription *time.Ticker
	// TickerUpdateRoutingSource re-reads the configured rule source so an
	// updated upstream set reaches a running core. It is never Reset: a rule
	// source is a URL, not an interval the user sets, so it follows the same
	// daily schedule as the subscription update.
	TickerUpdateRoutingSource *time.Ticker
	// TickerUpdateDnsSource re-reads the configured DNS rule source, on the
	// same schedule and for the same reason as the routing one.
	TickerUpdateDnsSource *time.Ticker
)

func IsDebug() bool {
	return Version == "debug"
}
