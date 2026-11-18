package approuter

import (
	"os"
	"path/filepath"
	"testing"

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
	if action, _ := c.decide(indexes, 0x01010101, 443, NetworkTCP); action != ActionBlock {
		t.Fatalf("port 443 got %v", action)
	}
	if action, class := c.decide(indexes, 0x01010101, 80, NetworkTCP); action != ActionProxy || class != 1 {
		t.Fatalf("port 80 got %v class %d", action, class)
	}
	// A destination no rule mentions is left alone.
	if action, _ := c.decide([]int{2}, 0x01010101, 80, NetworkTCP); action != ActionDirect {
		t.Fatalf("no matching rule gave %v", action)
	}
}

func TestMatchesDestinationNeedsBothHalves(t *testing.T) {
	r := Rule{V4: []IPRange{{0x0a000000, 0x0affffff}}, Ports: []PortRange{{80, 80}}}
	if !matchesDestination(r, 0x0a000001, 80) {
		t.Fatal("a destination inside both ranges did not match")
	}
	if matchesDestination(r, 0x0a000001, 81) {
		t.Fatal("the wrong port matched")
	}
	if matchesDestination(r, 0x0b000001, 80) {
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
	return newPlanner(c, cache, ActionDirect), cache, exeKey{Dev: 42, Ino: 4242}
}

func TestPlannerCachesTheRuleListOfAnExecutable(t *testing.T) {
	p, cache, key := plannerFor(t, []Rule{
		{Action: ActionBlock, Ports: []PortRange{{443, 443}}},
		{Action: ActionDirect},
	})
	o := orig{Pid: uint32(os.Getpid()), ExeDev: key.Dev, ExeIno: key.Ino, Port: 443}
	o.IP = [4]byte{1, 1, 1, 1}
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
	o.IP = [4]byte{4, 4, 4, 4}
	p.decide(o)
	if slot := cache.slots[key]; slot != slotDecide {
		t.Fatalf("slot %d, want the decide slot", slot)
	}
}

func TestPlannerCachesAnExecutableThatMatchesNothing(t *testing.T) {
	p, cache, key := plannerFor(t, nil)
	o := orig{Pid: uint32(os.Getpid()), ExeDev: key.Dev, ExeIno: key.Ino, Port: 80}
	o.IP = [4]byte{1, 1, 1, 1}
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
	o.IP = [4]byte{1, 1, 1, 1}
	p.decide(o)
	if len(cache.slots) != 0 {
		t.Fatalf("an entry was written without an identity: %+v", cache.slots)
	}
}

// A process that cannot be identified is left alone rather than guessed at.
func TestPlannerLeavesAnUnidentifiableProcessAlone(t *testing.T) {
	p, cache, _ := plannerFor(t, []Rule{{Action: ActionBlock}})
	o := orig{Pid: 0x7ffffff0, Port: 80} // a pid that does not exist
	o.IP = [4]byte{1, 1, 1, 1}
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
		p := newPlanner(compiled, newFakeCache(), policy)
		o := orig{Pid: 0x7ffffff0, Port: 80}
		o.IP = [4]byte{1, 1, 1, 1}
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
	if action, _ := c.decide(indexes, 0x01010101, 443, NetworkUDP); action != ActionBlock {
		t.Fatalf("the UDP rule gave %v for a UDP flow", action)
	}
	if action, class := c.decide(indexes, 0x01010101, 443, NetworkTCP); action != ActionProxy || class != 1 {
		t.Fatalf("the TCP rule gave %v class %d for a TCP flow", action, class)
	}
	if action, _ := c.decide([]int{2}, 0x01010101, 443, NetworkUDP); action != ActionDirect {
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
