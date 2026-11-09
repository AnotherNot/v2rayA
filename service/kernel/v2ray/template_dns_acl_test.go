package v2ray

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
)

func TestDNSAllowedClientsFollowsTheListenerReach(t *testing.T) {
	local := &configure.Setting{}
	clients := dnsAllowedClients(local, dnsModuleListenAddr(local))
	if len(clients) != 2 || clients[0] != "127.0.0.0/8" || clients[1] != "::1/128" {
		t.Fatalf("loopback listener ACL = %v, want loopback only", clients)
	}
	if rate := dnsRateLimit(local, dnsModuleListenAddr(local)); rate != 0 {
		t.Fatalf("loopback listener rate limit = %d, want 0", rate)
	}

	shared := &configure.Setting{PortSharing: true}
	clients = dnsAllowedClients(shared, dnsModuleListenAddr(shared))
	if len(clients) < 2 {
		t.Fatalf("port-sharing listener ACL = %v", clients)
	}
	seenPrivate := false
	for _, raw := range clients {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			t.Fatalf("ACL entry %q: %v", raw, err)
		}
		if !prefix.Addr().IsLoopback() && (prefix.Addr().IsPrivate() || prefix.Addr().IsLinkLocalUnicast()) {
			seenPrivate = true
		}
		if !prefix.Addr().IsLoopback() && !prefix.Addr().IsPrivate() && !prefix.Addr().IsLinkLocalUnicast() {
			t.Fatalf("public address %s was put in the DNS ACL", prefix)
		}
	}
	if !seenPrivate {
		t.Fatalf("port-sharing ACL = %v, want at least one private interface prefix", clients)
	}
	if rate := dnsRateLimit(shared, dnsModuleListenAddr(shared)); rate != 200 {
		t.Fatalf("port-sharing rate limit = %d, want 200", rate)
	}
}

func TestDNSAllowedClientsForAnExplicitAddress(t *testing.T) {
	setting := &configure.Setting{DnsListenAddr: "192.168.50.2:5353"}
	clients := dnsAllowedClients(setting, dnsModuleListenAddr(setting))
	found := false
	for _, raw := range clients {
		if raw == "192.168.50.2/32" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ACL = %v, want the configured address", clients)
	}
	if rate := dnsRateLimit(setting, dnsModuleListenAddr(setting)); rate != 200 {
		t.Fatalf("rate limit = %d, want 200 for a non-loopback listener", rate)
	}
}

func TestGeneratedDNSConfigCarriesACLAndRateLimit(t *testing.T) {
	setting := configure.NewSetting()
	setting.PortSharing = true
	tmpl := baseTemplate(t, setting)
	if err := tmpl.generateDnsModuleConfig(nil); err != nil {
		t.Fatal(err)
	}
	b := []byte(tmpl.DnsModuleConfig)
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	listener, ok := decoded["listener"].(map[string]interface{})
	if !ok {
		t.Fatalf("listener missing from %s", b)
	}
	t.Logf("generated listener config: %v", listener)
	if _, ok := listener["allowed_clients"]; !ok {
		t.Fatalf("allowed_clients missing from %s", b)
	}
	if got, ok := listener["rate_limit"].(float64); !ok || got != 200 {
		t.Fatalf("rate_limit = %v, want 200", listener["rate_limit"])
	}
}

func TestDNSAllowedClientsDoesNotLeakPublicText(t *testing.T) {
	for _, raw := range dnsAllowedClients(&configure.Setting{PortSharing: true}, "0.0.0.0:52353") {
		if strings.Contains(raw, "0.0.0.0/0") || strings.Contains(raw, "::/0") {
			t.Fatalf("ACL contains a catch-all prefix: %s", raw)
		}
	}
}
