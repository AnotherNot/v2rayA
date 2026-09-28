package v2ray

import (
	"net"
	"os"
	"runtime"
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
)

func TestDirectDnsServersFollowTheRules(t *testing.T) {
	// the default rules: localhost direct (skipped: loopback), 1.0.0.1 through the proxy
	got := directDnsServers()
	if len(got) != 0 {
		t.Fatalf("directDnsServers() = %v", got)
	}
	if fallbackResolver() != "1.0.0.1" {
		t.Fatalf("fallbackResolver() = %q", fallbackResolver())
	}
}

// A non-root service can never bind port 53, so asking the DNS module to
// listen on 127.2.0.17:53 guarantees the core exits on start. The generator
// must not emit the listener without privilege.
func TestExtraListenAddrsSkipPrivilegedPortWithoutRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs a non-root user: root can bind 53")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the 127.2.0.17 listener is Linux-only")
	}
	setting := configure.GetSettingNotNil()
	for _, addr := range dnsModuleExtraListenAddrs(setting) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("addr %q does not parse: %v", addr, err)
		}
		if port == "53" {
			t.Fatalf("a non-root generator emitted %q", addr)
		}
	}
}
