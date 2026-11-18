package approuter

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// Limits mirrored from bpf/router.bpf.c. A rule that does not fit them is not
// dropped: the executables it matches are decided in user space instead.
const (
	maxRules        = 256
	maxExeRules     = 256
	rulesPerExe     = 16
	maxHostsPerRule = 2
	maxPortsPerRule = 2
)

// Slots reserved in the exec_rules map.
const (
	slotEmptyRules = 0 // this executable matches no rule
	slotDecide     = 1 // its rules do not fit: decide every flow in user space
	firstSlot      = 2
)

// rulesDecide mirrors RULES_DECIDE: the length that tells the kernel to send
// every flow of an executable to the relay.
const rulesDecide = ^uint32(0)

// bpfRule mirrors struct router_rule. The padding is explicit because
// encoding/binary sizes a Go struct by summing its fields.
type bpfRule struct {
	Action    uint8
	AnyHost   uint8
	AnyPort   uint8
	NV4       uint8
	NV6       uint8
	NPort     uint8
	InboundID uint8
	Network   uint8
	V4        [maxHostsPerRule]IPRange
	V6        [maxHostsPerRule]IPRange6
	Port      [maxPortsPerRule]PortRange
}

// compiled is one rule set in the two shapes it is needed in: what the kernel
// can walk, and what a flow whose executable is not cached needs in user space.
type compiled struct {
	rules  []Rule
	kernel []bpfRule
	fits   []bool
}

func compileRules(rules []Rule) (*compiled, error) {
	if len(rules) > maxRules {
		return nil, fmt.Errorf("%d application rules, and at most %d fit in the kernel; split them or narrow them", len(rules), maxRules)
	}
	c := &compiled{
		rules:  rules,
		kernel: make([]bpfRule, len(rules)),
		fits:   make([]bool, len(rules)),
	}
	for i, r := range rules {
		if r.Action == ActionProxy && (r.Class < 1 || r.Class > MaxClass) {
			return nil, fmt.Errorf("rule %d asks for class %d, and classes are 1 to %d", i+1, r.Class, MaxClass)
		}
		br := bpfRule{
			Action:    uint8(r.Action),
			InboundID: uint8(r.Class),
			NV4:       uint8(len(r.V4)),
			NV6:       uint8(len(r.V6)),
			NPort:     uint8(len(r.Ports)),
			Network:   uint8(r.Network),
		}
		if len(r.V4) == 0 && len(r.V6) == 0 {
			br.AnyHost = 1
		}
		if len(r.Ports) == 0 {
			br.AnyPort = 1
		}
		fits := len(r.V4) <= maxHostsPerRule && len(r.V6) <= maxHostsPerRule && len(r.Ports) <= maxPortsPerRule
		// A rule that does not fit keeps whatever the kernel can see only if
		// that is still exact; anything else is marked and decided in user
		// space. Copying a truncated list would make the rule match more.
		if fits {
			copy(br.V4[:], r.V4)
			copy(br.V6[:], r.V6)
			copy(br.Port[:], r.Ports)
		}
		c.kernel[i] = br
		c.fits[i] = fits
	}
	return c, nil
}

// matchesDestination applies the destination half of a rule. An IPv4
// destination is matched only by IPv4 ranges and an IPv6 one only by IPv6
// ranges, so a rule that names one family never catches the other.
func matchesDestination(r Rule, d destination, port uint16) bool {
	if len(r.Ports) > 0 {
		ok := false
		for _, pr := range r.Ports {
			if port >= pr.Lo && port <= pr.Hi {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.V4) == 0 && len(r.V6) == 0 {
		return true
	}
	if d.isIPv6 {
		for _, ir := range r.V6 {
			if !less6(d.v6, ir.Lo) && !less6(ir.Hi, d.v6) {
				return true
			}
		}
		return false
	}
	for _, ir := range r.V4 {
		if d.v4 >= ir.Lo && d.v4 <= ir.Hi {
			return true
		}
	}
	return false
}

// less6 reports whether a is below b, word by word from the most significant,
// which is what makes the word comparison an address comparison.
func less6(a, b [4]uint32) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// decide returns the action of the first rule that matches this flow, and the
// class a proxy action goes to.
func (c *compiled) decide(indexes []int, d destination, port uint16, network Network) (Action, int) {
	for _, i := range indexes {
		if !c.rules[i].Network.Covers(network) {
			continue
		}
		if !matchesDestination(c.rules[i], d, port) {
			continue
		}
		return c.rules[i].Action, c.rules[i].Class
	}
	return ActionDirect, 0
}

// executableOf names the program behind a process. The kernel's own identity
// is the cache key; this is what a rule matches on.
func executableOf(pid uint32) (name, path string, err error) {
	link := "/proc/" + strconv.Itoa(int(pid)) + "/exe"
	if path, err = os.Readlink(link); err != nil {
		return "", "", err
	}
	// A binary that was replaced or deleted keeps its open file, and the link
	// says so; the name is still the one the rule means.
	path = strings.TrimSuffix(path, " (deleted)")
	if path == "" {
		return "", "", errors.New("the executable link is empty")
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		name = path[i+1:]
	} else {
		name = path
	}
	return name, path, nil
}

// ruleCache is the part of the kernel's maps the slow path writes.
type ruleCache interface {
	putExe(key exeKey, slot uint32) error
	putExecRules(slot uint32, ruleIndexes []uint32) error
}

// planner decides the flows the kernel could not, and remembers what it
// learned so later flows take the fast path.
type planner struct {
	compiled   *compiled
	cache      ruleCache
	unknown    Action
	mu         sync.Mutex
	nextSlot   uint32
	remembered map[exeKey][]int
}

func newPlanner(c *compiled, cache ruleCache, unknown Action) *planner {
	if unknown != ActionBlock {
		unknown = ActionDirect
	}
	return &planner{
		compiled: c, cache: cache, unknown: unknown,
		nextSlot: firstSlot, remembered: map[exeKey][]int{},
	}
}

// decide handles one flow. The executable's rule list is cached in the kernel
// the first time it is seen; until then every flow of that executable comes
// through here, which is why the answer for this flow is computed rather than
// read back.
func (p *planner) decide(o orig) (Action, int) {
	d := o.destination()
	name, path, err := executableOf(o.Pid)
	if err != nil {
		// The process that opened this socket is not identifiable: it exited
		// before the lookup, or /proc does not show it. Applying a rule would
		// be a guess, so the configured policy answers instead.
		log.Warn("app router: cannot identify the process behind pid %d (%v); %s the flow",
			o.Pid, err, policyName(p.unknown))
		return p.unknown, 0
	}
	indexes := p.rulesFor(name, path)
	p.remember(o, name, path, indexes)
	return p.compiled.decide(indexes, d, uint16(o.Port), o.Network)
}

// rulesFor lists the rules that are about this executable, in order.
func (p *planner) rulesFor(name, path string) []int {
	var indexes []int
	for i := range p.compiled.rules {
		if p.compiled.rules[i].Apps.Match(name, path) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// remember caches the executable's rule list in the kernel. A list that does
// not fit, or a rule the kernel cannot evaluate exactly, leaves the executable
// on the slow path rather than on a wrong one.
func (p *planner) remember(o orig, name, path string, indexes []int) {
	key := exeKey{Dev: o.ExeDev, Pad: 0, Ino: o.ExeIno}
	if key.Ino == 0 {
		// The kernel could not read the identity, so an entry could never be
		// hit. Decide this flow and leave the rest to the next one.
		return
	}
	p.mu.Lock()
	if _, done := p.remembered[key]; done {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	slot := uint32(slotEmptyRules)
	switch {
	case len(indexes) == 0:
		slot = slotEmptyRules
	case !p.fitsKernel(indexes) || len(indexes) > rulesPerExe:
		slot = slotDecide
	default:
		p.mu.Lock()
		slot = p.nextSlot
		if slot >= maxExeRules {
			slot = slotDecide
		} else {
			p.nextSlot++
			ruleIndexes := make([]uint32, len(indexes))
			for i, index := range indexes {
				ruleIndexes[i] = uint32(index)
			}
			if err := p.cache.putExecRules(slot, ruleIndexes); err != nil {
				log.Warn("app router: cache the rules of %s: %v", o, err)
				slot = slotDecide
			}
		}
		p.mu.Unlock()
	}
	if err := p.cache.putExe(key, slot); err != nil {
		log.Warn("app router: cache the executable behind %s: %v", o, err)
		return
	}
	// One line per executable, not per flow: this is the point where the
	// kernel takes over, and a reader needs to see which program it took over
	// for, how much of its rule list fit, and what happens to the rest.
	switch slot {
	case slotEmptyRules:
		log.Info("app router: %s (%s) matches no rule; its connections are left alone", name, path)
	case slotDecide:
		log.Info("app router: %s (%s) matched %d rule(s) that do not fit the kernel; every connection of it is decided here",
			name, path, len(indexes))
	default:
		log.Info("app router: %s (%s) matched %d rule(s); the kernel decides from slot %d", name, path, len(indexes), slot)
	}
	p.mu.Lock()
	if len(p.remembered) > 4096 {
		p.remembered = map[exeKey][]int{}
	}
	p.remembered[key] = indexes
	p.mu.Unlock()
}

func (p *planner) fitsKernel(indexes []int) bool {
	for _, i := range indexes {
		if !p.compiled.fits[i] {
			return false
		}
	}
	return true
}

// policyName is how the policy reads in the log.
func policyName(a Action) string {
	if a == ActionBlock {
		return "refusing"
	}
	return "leaving alone"
}

// networkOfProto reads the transport the kernel recorded. A TCP entry is the
// only one the maps held before UDP existed, so anything else is UDP.
func networkOfProto(proto uint8) Network {
	if proto == 17 { // IPPROTO_UDP
		return NetworkUDP
	}
	return NetworkTCP
}
