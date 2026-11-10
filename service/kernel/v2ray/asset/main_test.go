package asset

import (
	"os"
	"testing"

	"github.com/v2rayA/v2rayA/common/netguard"
)

// The tests stand in a local transport for the download; the guard is about
// real providers and would try to resolve the fixture host.
func TestMain(m *testing.M) {
	netguard.SetAllowPrivate(true)
	os.Exit(m.Run())
}
