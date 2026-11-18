package approuter

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/v2rayA/RoutingA"
)

func apps(names ...string) RoutingA.ProcessMatcher {
	return RoutingA.ProcessMatcher{Names: names}
}

func TestCompileRulesRefusesMoreThanTheKernelHolds(t *testing.T) {
	rules := make([]Rule, maxRules+1)
	for i := range rules {
		rules[i] = Rule{Apps: apps("x"), Action: ActionDirect}
	}
	if _, err := compileRules(rules); err == nil {
		t.Fatal("more rules than the kernel can hold were accepted")
	}
}

func TestCompileRulesRefusesAnUnknownClass(t *testing.T) {
	_, err := compileRules([]Rule{{Apps: apps("x"), Action: ActionProxy, Class: MaxClass + 1}})
	if err == nil {
		t.Fatal("a class beyond the limit was accepted")
	}
}

// A rule with more destinations than the kernel holds is not truncated: it is
// marked, and the executables it matches are decided in user space.
func TestCompileMarksARuleThatDoesNotFit(t *testing.T) {
	c, err := compileRules([]Rule{{
		Apps:   apps("x"),
		Action: ActionDirect,
		V4:     []IPRange{{0, 1}, {2, 3}, {4, 5}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if c.fits[0] {
		t.Fatal("a rule with three address ranges was reported as fitting")
	}
	if c.kernel[0].AnyHost != 0 {
		t.Fatal("the truncated rule was widened to any host")
	}
}

func TestCompileKeepsTheDestinationConditions(t *testing.T) {
	c, err := compileRules([]Rule{{
		Apps:   apps("x"),
		Action: ActionProxy,
		Class:  2,
		V4:     []IPRange{{0x0a000000, 0x0affffff}},
		Ports:  []PortRange{{443, 443}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !c.fits[0] {
		t.Fatal("a rule that fits was marked")
	}
	k := c.kernel[0]
	if k.Action != uint8(ActionProxy) || k.InboundID != 2 {
		t.Fatalf("%+v", k)
	}
	if k.AnyHost != 0 || k.AnyPort != 0 || k.NV4 != 1 || k.NPort != 1 {
		t.Fatalf("%+v", k)
	}
	if k.V4[0] != (IPRange{0x0a000000, 0x0affffff}) || k.Port[0] != (PortRange{443, 443}) {
		t.Fatalf("%+v", k)
	}
}

func TestCompiledDecideTakesTheFirstMatch(t *testing.T) {
	c, err := compileRules([]Rule{
		{Apps: apps("x"), Action: ActionBlock, Ports: []PortRange{{443, 443}}},
		{Apps: apps("x"), Action: ActionProxy, Class: 1},
		{Apps: apps("x"), Action: ActionDirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	indexes := []int{0, 1, 2}
	if action, _ := c.decide(indexes, at(0x01010101), 443, NetworkTCP, nil); action != ActionBlock {
		t.Fatalf("port 443 got %v", action)
	}
	if action, class := c.decide(indexes, at(0x01010101), 80, NetworkTCP, nil); action != ActionProxy || class != 1 {
		t.Fatalf("port 80 got %v class %d", action, class)
	}
	// A destination no rule mentions is left alone.
	if action, _ := c.decide([]int{2}, at(0x01010101), 80, NetworkTCP, nil); action != ActionDirect {
		t.Fatalf("no matching rule gave %v", action)
	}
}

func TestMatchesDestinationNeedsBothHalves(t *testing.T) {
	r := Rule{V4: []IPRange{{0x0a000000, 0x0affffff}}, Ports: []PortRange{{80, 80}}}
	if !matchesFlow(r, at(0x0a000001), 80, nil) {
		t.Fatal("a destination inside both ranges did not match")
	}
	if matchesFlow(r, at(0x0a000001), 81, nil) {
		t.Fatal("the wrong port matched")
	}
	if matchesFlow(r, at(0x0b000001), 80, nil) {
		t.Fatal("the wrong address matched")
	}
}

// fakeCache records what the planner writes, so the test can assert on the slot
// an executable ends up with.
type fakeCache struct {
	slots map[exeKey]uint32
	lists map[uint32][]uint32
}

func newFakeCache() *fakeCache {
	return &fakeCache{slots: map[exeKey]uint32{}, lists: map[uint32][]uint32{}}
}

func (c *fakeCache) putExe(key exeKey, slot uint32) error {
	c.slots[key] = slot
	return nil
}

func (c *fakeCache) putExecRules(slot uint32, indexes []uint32) error {
	c.lists[slot] = indexes
	return nil
}

// plannerFor builds a planner whose rules are about the test binary itself, so
// the /proc lookup resolves to a name the rules can match.
func plannerFor(t *testing.T, rules []Rule) (*planner, *fakeCache, exeKey) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.EvalSymlinks(self)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rules {
		rules[i].Apps = RoutingA.ProcessMatcher{Paths: []string{path}}
	}
	c, err := compileRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	cache := newFakeCache()
	// The identity the kernel would report is not what stat() says on some
	// filesystems, so the test supplies one rather than computing it.
	return newPlanner(c, cache, ActionDirect, nil), cache, exeKey{Dev: 42, Ino: 4242}
}

func TestPlannerCachesTheRuleListOfAnExecutable(t *testing.T) {
	p, cache, key := plannerFor(t, []Rule{
		{Action: ActionBlock, Ports: []PortRange{{443, 443}}},
		{Action: ActionDirect},
	})
	o := orig{Pid: uint32(os.Getpid()), ExeDev: key.Dev, ExeIno: key.Ino, Port: 443}
	o.Addr = v4Addr(1, 1, 1, 1)
	if action, _ := p.decide(o); action != ActionBlock {
		t.Fatalf("port 443 got %v", action)
	}
	slot, ok := cache.slots[key]
	if !ok {
		t.Fatal("the executable was not cached")
	}
	if slot < firstSlot {
		t.Fatalf("slot %d is a reserved one", slot)
	}
	if got := cache.lists[slot]; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("cached rule list %v", got)
	}
}

// A rule the kernel cannot evaluate exactly leaves its executable on the slow
// path instead of on a wrong one.
func TestPlannerSendsAnUnfittableRuleToUserSpace(t *testing.T) {
	p, cache, key := plannerFor(t, []Rule{{
		Action: ActionBlock,
		V4:     []IPRange{{0, 1}, {2, 3}, {4, 5}},
	}})
	o := orig{Pid: uint32(os.Getpid()), ExeDev: key.Dev, ExeIno: key.Ino, Port: 80}
	o.Addr = v4Addr(4, 4, 4, 4)
	p.decide(o)
	if slot := cache.slots[key]; slot != slotDecide {
		t.Fatalf("slot %d, want the decide slot", slot)
	}
}

func TestPlannerCachesAnExecutableThatMatchesNothing(t *testing.T) {
	p, cache, key := plannerFor(t, nil)
	o := orig{Pid: uint32(os.Getpid()), ExeDev: key.Dev, ExeIno: key.Ino, Port: 80}
	o.Addr = v4Addr(1, 1, 1, 1)
	if action, _ := p.decide(o); action != ActionDirect {
		t.Fatalf("an executable with no rules got %v", action)
	}
	if slot := cache.slots[key]; slot != slotEmptyRules {
		t.Fatalf("slot %d, want the empty slot", slot)
	}
}

// Without an identity there is nothing an entry could be keyed by, so the
// planner must not write one.
func TestPlannerDoesNotCacheWithoutAnIdentity(t *testing.T) {
	p, cache, _ := plannerFor(t, []Rule{{Action: ActionDirect}})
	o := orig{Pid: uint32(os.Getpid()), Port: 80}
	o.Addr = v4Addr(1, 1, 1, 1)
	p.decide(o)
	if len(cache.slots) != 0 {
		t.Fatalf("an entry was written without an identity: %+v", cache.slots)
	}
}

// A process that cannot be identified is left alone rather than guessed at.
func TestPlannerLeavesAnUnidentifiableProcessAlone(t *testing.T) {
	p, cache, _ := plannerFor(t, []Rule{{Action: ActionBlock}})
	o := orig{Pid: 0x7ffffff0, Port: 80} // a pid that does not exist
	o.Addr = v4Addr(1, 1, 1, 1)
	if action, _ := p.decide(o); action != ActionDirect {
		t.Fatalf("an unidentified process got %v", action)
	}
	if len(cache.slots) != 0 {
		t.Fatal("an unidentified process was cached")
	}
}

// The policy for a connection nobody can attribute is a choice: leaving it
// alone, or refusing it so a blocking rule does not fail open.
func TestPlannerAppliesTheUnknownOwnerPolicy(t *testing.T) {
	for _, policy := range []Action{ActionDirect, ActionBlock} {
		compiled, err := compileRules([]Rule{{Action: ActionProxy, Class: 1}})
		if err != nil {
			t.Fatal(err)
		}
		p := newPlanner(compiled, newFakeCache(), policy, nil)
		o := orig{Pid: 0x7ffffff0, Port: 80}
		o.Addr = v4Addr(1, 1, 1, 1)
		action, _ := p.decide(o)
		if action != policy {
			t.Errorf("policy %v gave %v", policy, action)
		}
	}
}

// A rule that names a transport leaves the other one alone; a rule with no
// network(...) covers both.
func TestCompiledDecideRespectsTheNetwork(t *testing.T) {
	c, err := compileRules([]Rule{
		{Apps: apps("x"), Action: ActionBlock, Network: NetworkUDP},
		{Apps: apps("x"), Action: ActionProxy, Class: 1, Network: NetworkTCP},
		{Apps: apps("x"), Action: ActionDirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	indexes := []int{0, 1, 2}
	if action, _ := c.decide(indexes, at(0x01010101), 443, NetworkUDP, nil); action != ActionBlock {
		t.Fatalf("the UDP rule gave %v for a UDP flow", action)
	}
	if action, class := c.decide(indexes, at(0x01010101), 443, NetworkTCP, nil); action != ActionProxy || class != 1 {
		t.Fatalf("the TCP rule gave %v class %d for a TCP flow", action, class)
	}
	if action, _ := c.decide([]int{2}, at(0x01010101), 443, NetworkUDP, nil); action != ActionDirect {
		t.Fatalf("a rule without a network gave %v", action)
	}
	if !(NetworkBoth.Covers(NetworkTCP) && NetworkBoth.Covers(NetworkUDP)) {
		t.Fatal("NetworkBoth does not cover both transports")
	}
}

// The kernel reads the transport as a bit it can test without a branch table.
func TestCompileCarriesTheNetworkToTheKernel(t *testing.T) {
	c, err := compileRules([]Rule{{Apps: apps("x"), Action: ActionDirect, Network: NetworkUDP}})
	if err != nil {
		t.Fatal(err)
	}
	if c.kernel[0].Network != uint8(NetworkUDP) {
		t.Fatalf("the kernel rule carries %d", c.kernel[0].Network)
	}
}

// at and at6 are destinations in the shape a rule is compared against.
func at(ip uint32) destination { return destination{v4: ip} }

func at6(a, b, c, d uint32) destination {
	return destination{v6: [4]uint32{a, b, c, d}, isIPv6: true}
}

// An IPv4 rule must not catch an IPv6 flow, and an IPv6 rule must not catch an
// IPv4 one, even when the address happens to be all zeroes on one side.
func TestMatchesDestinationSeparatesTheFamilies(t *testing.T) {
	v4any := Rule{V4: []IPRange{{0x0a000000, 0x0affffff}}} // 10.0.0.0/8
	if !matchesFlow(v4any, at(0x0a000001), 80, nil) {
		t.Fatal("an IPv4 destination inside the range did not match")
	}
	if matchesFlow(v4any, at6(0, 0, 0, 0), 80, nil) {
		t.Fatal("an IPv4 rule caught an IPv6 flow")
	}
	v6any := Rule{V6: []IPRange6{{Lo: [4]uint32{0, 0, 0, 0}, Hi: [4]uint32{0, 0, 0, 0}}}}
	if !matchesFlow(v6any, at6(0, 0, 0, 0), 80, nil) {
		t.Fatal("an IPv6 destination inside the range did not match")
	}
	if matchesFlow(v6any, at(0), 80, nil) {
		t.Fatal("an IPv6 rule caught an IPv4 flow")
	}
	mixed := Rule{V4: []IPRange{{0x0a000000, 0x0affffff}}, V6: []IPRange6{
		{Lo: [4]uint32{0xfd00, 0, 0, 1}, Hi: [4]uint32{0xfd00, 0, 0, 0xffff}},
	}}
	if !matchesFlow(mixed, at(0x0a000001), 80, nil) {
		t.Fatal("the IPv4 half of a mixed rule did not match")
	}
	if !matchesFlow(mixed, at6(0xfd00, 0, 0, 2), 80, nil) {
		t.Fatal("the IPv6 half of a mixed rule did not match")
	}
}

// The kernel needs the same split: one count per family, and a rule that names
// only one family is not "any host".
func TestCompileKeepsTheIPv6Condition(t *testing.T) {
	c, err := compileRules([]Rule{{
		Apps:   apps("x"),
		Action: ActionProxy,
		Class:  1,
		V6:     []IPRange6{{Lo: [4]uint32{0x2001, 0xdb8, 0, 0}, Hi: [4]uint32{0x2001, 0xdb8, 0xffff, 0xffff}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	k := c.kernel[0]
	if !c.fits[0] || k.AnyHost != 0 || k.NV4 != 0 || k.NV6 != 1 {
		t.Fatalf("%+v fits=%v", k, c.fits[0])
	}
	if k.V6[0].Lo != [4]uint32{0x2001, 0xdb8, 0, 0} {
		t.Fatalf("%+v", k.V6[0])
	}
}

func TestCompileMarksTooManyIPv6Ranges(t *testing.T) {
	c, err := compileRules([]Rule{{
		Apps:   apps("x"),
		Action: ActionDirect,
		V6: []IPRange6{
			{Lo: [4]uint32{1}, Hi: [4]uint32{2}},
			{Lo: [4]uint32{3}, Hi: [4]uint32{4}},
			{Lo: [4]uint32{5}, Hi: [4]uint32{6}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if c.fits[0] {
		t.Fatal("a rule with three IPv6 ranges was reported as fitting")
	}
	if c.kernel[0].AnyHost != 0 {
		t.Fatal("the truncated rule was widened to any host")
	}
}

// 128-bit comparison, word by word: the ranges of the PoC's O2-O4 checks are
// the ones that would silently match the wrong address if the order were
// reversed.
func TestCompiledDecideComparesIPv6AsA128BitNumber(t *testing.T) {
	c, err := compileRules([]Rule{
		{Apps: apps("x"), Action: ActionBlock, V6: []IPRange6{
			{Lo: [4]uint32{0x2001, 0xdb8, 0, 0}, Hi: [4]uint32{0x2001, 0xdb8, 0xffff, 0xffff}},
		}},
		{Apps: apps("x"), Action: ActionProxy, Class: 1, V6: []IPRange6{
			{Lo: [4]uint32{0, 0, 0, 0x10}, Hi: [4]uint32{0, 0, 0, 0x10}},
		}},
		{Apps: apps("x"), Action: ActionDirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	indexes := []int{0, 1, 2}
	if action, _ := c.decide(indexes, at6(0x2001, 0xdb8, 0, 1), 443, NetworkTCP, nil); action != ActionBlock {
		t.Fatalf("the CIDR range gave %v", action)
	}
	if action, _ := c.decide(indexes, at6(0x2001, 0xdb9, 0, 1), 443, NetworkTCP, nil); action != ActionDirect {
		t.Fatalf("an address past the range gave %v", action)
	}
	if action, _ := c.decide(indexes, at6(0, 0, 0, 0x10), 443, NetworkTCP, nil); action != ActionProxy {
		t.Fatalf("the single-address range gave %v", action)
	}
	if action, _ := c.decide(indexes, at6(0, 0, 0, 1), 443, NetworkTCP, nil); action != ActionDirect {
		t.Fatalf("::1 matched a range that starts at ::10: %v", action)
	}
}

// mustDomains compiles patterns the way the editor writes them.
func mustDomains(t *testing.T, values ...string) []DomainPattern {
	t.Helper()
	var out []DomainPattern
	for _, value := range values {
		p, err := CompileDomainPattern(value)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// A rule that names a domain cannot be evaluated by the kernel: it would match
// on the address alone, which is not what it says. Its executables are decided
// here instead, which is what an unfittable rule already means.
func TestCompileMarksADomainRuleAsUnfittable(t *testing.T) {
	c, err := compileRules([]Rule{{
		Apps:    apps("x"),
		Action:  ActionProxy,
		Class:   1,
		Domains: mustDomains(t, "example.com"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if c.fits[0] {
		t.Fatal("a rule that names a domain was reported as fitting the kernel")
	}
}

// The name decides, and the rule order still decides which name wins. With no
// name at all the domain rules do not match; with a name that matches neither
// they do not match either.
func TestCompiledDecideMatchesTheNameOfAnAddress(t *testing.T) {
	c, err := compileRules([]Rule{
		{Apps: apps("x"), Action: ActionBlock, Domains: mustDomains(t, "full: ads.example.com")},
		{Apps: apps("x"), Action: ActionProxy, Class: 1, Domains: mustDomains(t, "example.com")},
		{Apps: apps("x"), Action: ActionDirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	indexes := []int{0, 1, 2}
	if action, _ := c.decide(indexes, at(0x01010101), 443, NetworkTCP, []string{"ads.example.com"}); action != ActionBlock {
		t.Fatalf("the exact name gave %v", action)
	}
	if action, class := c.decide(indexes, at(0x01010101), 443, NetworkTCP, []string{"www.example.com"}); action != ActionProxy || class != 1 {
		t.Fatalf("the suffix name gave %v class %d", action, class)
	}
	if action, _ := c.decide(indexes, at(0x01010101), 443, NetworkTCP, nil); action != ActionDirect {
		t.Fatalf("no name gave %v", action)
	}
	if action, _ := c.decide(indexes, at(0x01010101), 443, NetworkTCP, []string{"other.test"}); action != ActionDirect {
		t.Fatalf("an unrelated name gave %v", action)
	}
}

// The planner asks the table the DNS module reports into, and the executable
// with a domain rule is cached for the slow path, not for the kernel.
func TestPlannerMatchesTheNameTheDNSAnswered(t *testing.T) {
	p, cache, key := plannerFor(t, []Rule{
		{Action: ActionProxy, Class: 1, Domains: mustDomains(t, "full: name.test")},
		{Action: ActionDirect},
	})
	store := newNameStore()
	store.learn("name.test", netip.MustParseAddr("10.0.0.1"), time.Minute)
	p.names = store

	o := orig{Pid: uint32(os.Getpid()), ExeDev: key.Dev, ExeIno: key.Ino, Port: 443, Addr: v4Addr(10, 0, 0, 1)}
	if action, class := p.decide(o); action != ActionProxy || class != 1 {
		t.Fatalf("the named address gave %v class %d", action, class)
	}
	if slot := cache.slots[key]; slot != slotDecide {
		t.Fatalf("slot %d, want the decide slot", slot)
	}

	o.Addr = v4Addr(10, 0, 0, 2)
	if action, _ := p.decide(o); action != ActionDirect {
		t.Fatalf("an address no name points at gave %v", action)
	}
}

// The report socket is how the module's answers reach the table.
func TestDNSReporterLearnsWhatTheModuleAnswered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.sock")
	store := newNameStore()
	r, err := startDNSReporter(path, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.close() })

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	send := func(line string) byte {
		t.Helper()
		if _, err := conn.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
		var ack [1]byte
		if _, err := conn.Read(ack[:]); err != nil {
			t.Fatal(err)
		}
		return ack[0]
	}
	if ack := send(`{"name":"Name.TEST.","addr":"10.0.0.9","ttl":60}`); ack != dnsReportAck {
		t.Fatalf("ack %q", ack)
	}
	// The ack comes after the entry is stored, so it is readable now.
	got := store.lookup(net.IPv4(10, 0, 0, 9))
	if len(got) != 1 || got[0] != "name.test" {
		t.Fatalf("lookup gave %v", got)
	}
	// A report that cannot be read is acknowledged too, and the connection
	// stays usable: a broken line must not stall the resolver.
	if ack := send("not json"); ack != dnsReportAck {
		t.Fatalf("ack %q for a malformed report", ack)
	}
	if ack := send(`{"name":"other.test","addr":"10.0.0.10","ttl":60}`); ack != dnsReportAck {
		t.Fatalf("ack %q after a malformed report", ack)
	}
	if got := store.lookup(net.IPv4(10, 0, 0, 10)); len(got) != 1 || got[0] != "other.test" {
		t.Fatalf("lookup after a malformed report gave %v", got)
	}
}
