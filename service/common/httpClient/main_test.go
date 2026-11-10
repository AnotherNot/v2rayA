package httpClient

import (
	"os"
	"testing"

	"github.com/v2rayA/v2rayA/common/netguard"
)

// The package's tests stand in local transports; the fetch guard is about real
// providers and would answer for loopback addresses instead of the stub.
func TestMain(m *testing.M) {
	netguard.SetAllowPrivate(true)
	os.Exit(m.Run())
}
