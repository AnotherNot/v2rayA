package configure

import (
	"os"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "v2raya-configure-test-*")
	if err != nil {
		panic(err)
	}
	conf.GetEnvironmentConfig().Config = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestGetLenSubscriptionServersPanicsOnMissingSubscription(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("missing subscription must report a recoverable panic")
		}
	}()
	GetLenSubscriptionServers(-1)
}

func TestSetOutboundSettingSkipsProbeValidationForRandom(t *testing.T) {
	const outbound = "probe-validation-test"
	if err := AddOutbound(outbound); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveOutbound(outbound) })
	if err := SetOutboundSetting(outbound, OutboundSetting{Type: Random}); err != nil {
		t.Fatalf("random with empty probe fields was rejected: %v", err)
	}
	if err := SetOutboundSetting(outbound, OutboundSetting{Type: LeastPing}); err == nil {
		t.Fatal("leastping with an empty probe interval was accepted")
	}
	if err := SetOutboundSetting(outbound, DefaultOutboundSetting()); err != nil {
		t.Fatalf("default leastping setting was rejected: %v", err)
	}
}

func TestSaveSpeedsStoresThroughputWithoutTouchingLatency(t *testing.T) {
	if err := AppendServers([]*ServerRaw{{}}); err != nil {
		t.Fatal(err)
	}
	servers := GetServers()
	index := len(servers) - 1
	t.Cleanup(func() { _ = RemoveServers([]int{index + 1}) })
	ws := NewWhiches([]*Which{{
		NodeRef: NodeRef{TYPE: ServerType, ID: index + 1},
		Latency: "20ms",
		Speed:   "12.4 Mbit/s",
	}})
	if err := ws.SaveLatencies(); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveSpeeds(); err != nil {
		t.Fatal(err)
	}
	got := GetServers()[index]
	if got.Latency != "20ms" {
		t.Errorf("latency = %q, want 20ms", got.Latency)
	}
	if got.Speed != "12.4 Mbit/s" {
		t.Errorf("speed = %q, want 12.4 Mbit/s", got.Speed)
	}
}

func TestSetSubscriptionAndConnectsRollsBackConnections(t *testing.T) {
	const outbound = "transaction-test"
	if err := AddOutbound(outbound); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveOutbound(outbound) })

	index := GetLenSubscriptions()
	subscription := &SubscriptionRaw{Address: "https://example.test/subscription"}
	if err := AppendSubscriptions([]*SubscriptionRaw{subscription}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveSubscriptions([]int{index}) })

	initial := &NodeRefs{Touches: []*NodeRef{{
		TYPE:     SubscriptionServerType,
		ID:       1,
		Sub:      index,
		Outbound: outbound,
	}}}
	if err := OverwriteConnects(initial); err != nil {
		t.Fatal(err)
	}
	replacement := &NodeRefs{Touches: []*NodeRef{{
		TYPE:     SubscriptionServerType,
		ID:       2,
		Sub:      index,
		Outbound: outbound,
	}}}
	if err := SetSubscriptionAndConnects(index+1, subscription, replacement); err == nil {
		t.Fatal("updating a missing subscription succeeded")
	}

	got := GetConnectedServersByOutbound(outbound)
	if got == nil || got.Len() != 1 || *got.Get()[0] != *initial.Get()[0] {
		t.Fatalf("connections changed after rollback: %+v", got)
	}

	if err := SetSubscriptionAndConnects(index, subscription, replacement); err != nil {
		t.Fatalf("replace subscription and connections: %v", err)
	}
	got = GetConnectedServersByOutbound(outbound)
	if got == nil || got.Len() != 1 || *got.Get()[0] != *replacement.Get()[0] {
		t.Fatalf("connections were not committed with subscription: %+v", got)
	}
}

func TestRuleModesNormalize(t *testing.T) {
	if got := NormalizeRulePortMode("whitelist"); got != AllowlistMode {
		t.Errorf("rule port whitelist -> %q", got)
	}
	if got := NormalizeRulePortMode("gfwlist"); got != BlocklistMode {
		t.Errorf("rule port gfwlist -> %q", got)
	}
	if got := NormalizeTransparentMode("whitelist"); got != TransparentAllowlist {
		t.Errorf("transparent whitelist -> %q", got)
	}
	if got := NormalizeTransparentMode("gfwlist"); got != TransparentBlocklist {
		t.Errorf("transparent gfwlist -> %q", got)
	}
	// canonical spellings and unknown values pass through
	for _, m := range []RulePortMode{AllowlistMode, BlocklistMode, RoutingAMode, "custom", "from-the-future"} {
		if got := NormalizeRulePortMode(m); got != m {
			t.Errorf("rule port %q became %q", m, got)
		}
	}
	for _, m := range []TransparentMode{TransparentClose, TransparentProxy, TransparentAllowlist, TransparentBlocklist, TransparentFollowRule, "from-the-future"} {
		if got := NormalizeTransparentMode(m); got != m {
			t.Errorf("transparent %q became %q", m, got)
		}
	}
}

func TestStoredRuleModesReadBackMigrated(t *testing.T) {
	s := NewSetting()
	s.RulePortMode = "gfwlist"
	s.Transparent = "whitelist"
	if err := SetSetting(s); err != nil {
		t.Fatal(err)
	}
	got := GetSettingNotNil()
	if got.RulePortMode != BlocklistMode {
		t.Errorf("stored gfwlist reads back as %q", got.RulePortMode)
	}
	if got.Transparent != TransparentAllowlist {
		t.Errorf("stored whitelist reads back as %q", got.Transparent)
	}
	// the next save persists the canonical spellings
	fresh := NewSetting()
	if err := SetSetting(fresh); err != nil {
		t.Fatal(err)
	}
}
